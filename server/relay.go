// Package server queues notifications in memory until an authenticated receiver acknowledges them.
package server

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

type Input struct {
	Title       string `json:"title" jsonschema:"Notification title, 1–30 Unicode characters"`
	Description string `json:"description" jsonschema:"Markdown description, 1–500 Unicode characters"`
}

func (in Input) Validate() error {
	for _, f := range []struct {
		name, value string
		limit       int
	}{{"title", in.Title, 30}, {"description", in.Description, 500}} {
		if strings.TrimSpace(f.value) == "" || !utf8.ValidString(f.value) || utf8.RuneCountInString(f.value) > f.limit {
			return fmt.Errorf("%s must contain 1–%d Unicode characters and cannot be blank", f.name, f.limit)
		}
	}
	return nil
}
func ValidateKey(key string) error {
	if len(key) == 0 || len(key) > 4096 {
		return errors.New("key must contain 1–4096 printable ASCII characters without spaces")
	}
	for _, c := range []byte(key) {
		if c < 33 || c > 126 {
			return errors.New("key must be printable ASCII without spaces")
		}
	}
	return nil
}

type Message struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	CreatedAt   string `json:"createdAt"`
}
type receiver struct {
	conn     *websocket.Conn
	wake     chan struct{}
	sentID   string
	lastSent time.Time
}
type Relay struct {
	key           string
	mu            sync.Mutex
	clients       map[*receiver]struct{}
	closed        bool
	pingInterval  time.Duration
	retryInterval time.Duration
	queue         []Message
	maxQueue      int
	bark          *barkSender
}

func New(key string) (*Relay, error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}
	return &Relay{key: key, clients: make(map[*receiver]struct{}), pingInterval: 20 * time.Second, retryInterval: 5 * time.Second, maxQueue: 10000}, nil
}

// NewWithConfig enables optional independent Bark delivery.
func NewWithConfig(config Config) (*Relay, error) {
	r, err := New(config.Key)
	if err != nil {
		return nil, err
	}
	r.bark, err = newBarkSender(config.Bark)
	if err != nil {
		return nil, err
	}
	return r, nil
}
func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func failure(w http.ResponseWriter, status int, message string) {
	reply(w, status, map[string]any{"error": message})
}
func (r *Relay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path == "/health" && req.Method == http.MethodGet {
		reply(w, 200, map[string]bool{"ok": true})
		return
	}
	if req.URL.Path != "/notify" && req.URL.Path != "/ws" {
		failure(w, 404, "Not found.")
		return
	}
	if req.URL.Path == "/ws" {
		if subtle.ConstantTimeCompare([]byte(req.Header.Get("Authorization")), []byte("Bearer "+r.key)) != 1 {
			failure(w, 401, "Invalid or missing Bearer key.")
			return
		}
		r.subscribe(w, req)
		return
	}
	if req.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		failure(w, 405, "Use POST /notify.")
		return
	}
	mediaType, _, _ := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if mediaType != "application/json" {
		failure(w, 415, "Use Content-Type: application/json.")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, 16384))
	if err != nil {
		var sizeError *http.MaxBytesError
		if errors.As(err, &sizeError) {
			failure(w, 413, "Request body exceeds 16 KB.")
		} else {
			failure(w, 400, "Unable to read request.")
		}
		return
	}
	var input Input
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&input); err != nil {
		failure(w, 400, "Invalid JSON. Only title and description are accepted.")
		return
	}
	if decoder.Decode(new(any)) != io.EOF {
		failure(w, 400, "Expected one JSON object.")
		return
	}
	if err = input.Validate(); err != nil {
		failure(w, 400, err.Error())
		return
	}
	idBytes := make([]byte, 16)
	if _, err = rand.Read(idBytes); err != nil {
		failure(w, 500, "Unable to create message ID.")
		return
	}
	idBytes[6] = (idBytes[6] & 0x0f) | 0x40
	idBytes[8] = (idBytes[8] & 0x3f) | 0x80
	id := fmt.Sprintf("%x-%x-%x-%x-%x", idBytes[:4], idBytes[4:6], idBytes[6:8], idBytes[8:10], idBytes[10:])
	r.mu.Lock()
	if r.closed || len(r.queue) >= r.maxQueue || (r.bark != nil && len(r.bark.queue) == cap(r.bark.queue)) {
		r.mu.Unlock()
		failure(w, 503, "Queue unavailable or full. Message was not accepted.")
		return
	}
	message := Message{id, input.Title, input.Description, time.Now().UTC().Format("2006-01-02T15:04:05.000Z")}
	r.queue = append(r.queue, message)
	if r.bark != nil {
		r.bark.queue <- message
	}
	r.wakeLocked()
	r.mu.Unlock()
	reply(w, 200, map[string]any{"id": id, "accepted": true})
}

// Call with mu held. Buffered signals coalesce concurrent enqueue/ACK events.
func (r *Relay) wakeLocked() {
	for client := range r.clients {
		select {
		case client.wake <- struct{}{}:
		default:
		}
	}
}
func (r *Relay) deliver(client *receiver) error {
	r.mu.Lock()
	if len(r.queue) == 0 {
		r.mu.Unlock()
		return nil
	}
	message := r.queue[0]
	if client.sentID == message.ID && time.Since(client.lastSent) < r.retryInterval {
		r.mu.Unlock()
		return nil
	}
	client.sentID, client.lastSent = message.ID, time.Now()
	r.mu.Unlock()
	_ = client.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	return client.conn.WriteJSON(message)
}
func (r *Relay) acknowledge(client *receiver, id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.queue) == 0 || r.queue[0].ID != id || client.sentID != id {
		return
	}
	r.queue[0] = Message{}
	r.queue = r.queue[1:]
	if len(r.queue) == 0 {
		r.queue = nil
	}
	r.wakeLocked()
}

func (r *Relay) snapshot() []*receiver {
	r.mu.Lock()
	defer r.mu.Unlock()
	clients := make([]*receiver, 0, len(r.clients))
	for client := range r.clients {
		clients = append(clients, client)
	}
	return clients
}
func (r *Relay) subscribe(w http.ResponseWriter, req *http.Request) {
	upgrader := websocket.Upgrader{HandshakeTimeout: 5 * time.Second, ReadBufferSize: 1024, WriteBufferSize: 2048}
	conn, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		return
	}
	client := &receiver{conn: conn, wake: make(chan struct{}, 1)}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		_ = conn.Close()
		return
	}
	r.clients[client] = struct{}{}
	client.wake <- struct{}{}
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.clients, client); r.mu.Unlock(); _ = conn.Close() }()
	conn.SetReadLimit(1024)
	_ = conn.SetReadDeadline(time.Now().Add(r.pingInterval * 3))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(r.pingInterval * 3)) })
	done := make(chan struct{})
	defer close(done)
	go func() {
		ping := time.NewTicker(r.pingInterval)
		defer ping.Stop()
		retry := time.NewTicker(r.retryInterval)
		defer retry.Stop()
		for {
			select {
			case <-done:
				return
			case <-ping.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second)); err != nil {
					_ = conn.Close()
					return
				}
				continue
			case <-retry.C:
			case <-client.wake:
			}
			if err := r.deliver(client); err != nil {
				_ = conn.Close()
				return
			}
		}
	}()
	for {
		kind, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var ack struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if kind != websocket.TextMessage || json.Unmarshal(data, &ack) != nil || ack.Type != "ack" || ack.ID == "" {
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "Expected {type:ack,id:message-id}"), time.Now().Add(time.Second))
			return
		}
		r.acknowledge(client, ack.ID)
	}
}

func (r *Relay) Close() {
	r.mu.Lock()
	r.closed = true
	if r.bark != nil {
		r.bark.cancel()
	}
	for client := range r.clients {
		_ = client.conn.Close()
	}
	r.mu.Unlock()
}
