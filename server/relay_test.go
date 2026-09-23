package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const testKey = "test-key"

func fixture(t *testing.T) (*Relay, string) {
	t.Helper()
	relay, err := New(testKey)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(relay)
	t.Cleanup(func() { relay.Close(); httpServer.Close() })
	return relay, httpServer.URL
}
func connect(t *testing.T, base string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(strings.Replace(base, "http", "ws", 1)+"/ws", http.Header{"Authorization": []string{"Bearer " + testKey}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}
func post(t *testing.T, base string, body any, key, contentType string) (int, map[string]any) {
	t.Helper()
	var data []byte
	if text, ok := body.(string); ok {
		data = []byte(text)
	} else {
		data, _ = json.Marshal(body)
	}
	req, _ := http.NewRequest("POST", base+"/notify", bytes.NewReader(data))
	req.Header.Set("Authorization", key)
	req.Header.Set("Content-Type", contentType)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, result
}
func send(t *testing.T, base string, input any) (int, map[string]any) {
	return post(t, base, input, "", "application/json")
}
func TestBroadcastAndNoHistory(t *testing.T) {
	_, base := fixture(t)
	a, b := connect(t, base), connect(t, base)
	status, result := send(t, base, Input{"完成", "# 结果\n\n**成功**"})
	if status != 200 || result["accepted"] != true {
		t.Fatal(status, result)
	}
	for _, conn := range []*websocket.Conn{a, b} {
		conn.SetReadDeadline(time.Now().Add(time.Second))
		var message Message
		if err := conn.ReadJSON(&message); err != nil {
			t.Fatal(err)
		}
		if message.ID != result["id"] || message.Title != "完成" || message.Description != "# 结果\n\n**成功**" {
			t.Fatal(message)
		}
		if _, err := time.Parse(time.RFC3339Nano, message.CreatedAt); err != nil {
			t.Fatal(err)
		}
	}
	response, err := http.Get(base + "/messages")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatal(response.StatusCode)
	}
}
func readNotice(t *testing.T, conn *websocket.Conn) Message {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var m Message
	if err := conn.ReadJSON(&m); err != nil {
		t.Fatal(err)
	}
	return m
}
func ack(t *testing.T, conn *websocket.Conn, id string) {
	t.Helper()
	if err := conn.WriteJSON(map[string]string{"type": "ack", "id": id}); err != nil {
		t.Fatal(err)
	}
}
func waitQueue(t *testing.T, r *Relay, count int) {
	t.Helper()
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		r.mu.Lock()
		n := len(r.queue)
		r.mu.Unlock()
		if n == count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("queue did not reach %d", count)
}
func TestOfflineReplayAndAck(t *testing.T) {
	r, base := fixture(t)
	for _, title := range []string{"first", "second"} {
		if status, result := send(t, base, Input{title, "queued"}); status != 200 || result["accepted"] != true {
			t.Fatal(status, result)
		}
	}
	conn := connect(t, base)
	first := readNotice(t, conn)
	if first.Title != "first" {
		t.Fatal(first)
	}
	waitQueue(t, r, 2) // Writing to the socket must not dequeue.
	conn.Close()
	conn = connect(t, base)
	replay := readNotice(t, conn)
	if replay.ID != first.ID {
		t.Fatal("missing replay")
	}
	ack(t, conn, replay.ID)
	second := readNotice(t, conn)
	if second.Title != "second" {
		t.Fatal(second)
	}
	ack(t, conn, second.ID)
	waitQueue(t, r, 0)
	conn.Close()
	conn = connect(t, base)
	send(t, base, Input{"third", "new"})
	if message := readNotice(t, conn); message.Title != "third" {
		t.Fatal("ACKed message replayed", message)
	}
}
func TestRetryAndInvalidAck(t *testing.T) {
	r, _ := New(testKey)
	r.retryInterval = 25 * time.Millisecond
	srv := httptest.NewServer(r)
	defer srv.Close()
	defer r.Close()
	conn := connect(t, srv.URL)
	send(t, srv.URL, Input{"retry", "pending"})
	first := readNotice(t, conn)
	ack(t, conn, "unknown")
	waitQueue(t, r, 1)
	if again := readNotice(t, conn); again.ID != first.ID {
		t.Fatal("retry changed ID")
	}
	ack(t, conn, first.ID)
	ack(t, conn, first.ID)
	waitQueue(t, r, 0)
}
func TestQueueCapacityDoesNotEvict(t *testing.T) {
	r, base := fixture(t)
	r.maxQueue = 1
	_, first := send(t, base, Input{"keep", "pending"})
	if status, _ := send(t, base, Input{"overflow", "pending"}); status != 503 {
		t.Fatal(status)
	}
	conn := connect(t, base)
	m := readNotice(t, conn)
	if m.ID != first["id"] {
		t.Fatal("accepted message evicted")
	}
	ack(t, conn, m.ID)
	waitQueue(t, r, 0)
	if status, _ := send(t, base, Input{"next", "pending"}); status != 200 {
		t.Fatal(status)
	}
}
func TestSendWithoutAuthentication(t *testing.T) {
	_, base := fixture(t)
	conn := connect(t, base)
	for _, header := range []string{"", "Bearer wrong"} {
		status, result := post(t, base, Input{"无需鉴权", "**收到**"}, header, "application/json")
		if status != 200 || result["accepted"] != true {
			t.Fatal(status, result)
		}
		conn.SetReadDeadline(time.Now().Add(time.Second))
		var message Message
		if err := conn.ReadJSON(&message); err != nil {
			t.Fatal(err)
		}
		if message.Title != "无需鉴权" {
			t.Fatal(message)
		}
		ack(t, conn, message.ID)
	}
}
func TestAuthorizationBeforeUpgrade(t *testing.T) {
	_, base := fixture(t)
	for _, key := range []string{"", "Bearer wrong"} {
		conn, response, err := websocket.DefaultDialer.Dial(strings.Replace(base, "http", "ws", 1)+"/ws", http.Header{"Authorization": []string{key}})
		if conn != nil {
			conn.Close()
		}
		if err == nil || response == nil || response.StatusCode != 401 {
			t.Fatal(err, response)
		}
		response.Body.Close()
	}
}
func TestValidation(t *testing.T) {
	_, base := fixture(t)
	connect(t, base)
	if status, _ := send(t, base, Input{strings.Repeat("😀", 30), strings.Repeat("字", 500)}); status != 200 {
		t.Fatal(status)
	}
	for _, body := range []any{nil, []string{}, map[string]any{}, `{"title":"x","description":"x","extra":1}`, `{"title":1,"description":"x"}`, `{"title":"x","description":null}`, Input{strings.Repeat("字", 31), "x"}, Input{"x", strings.Repeat("😀", 501)}, Input{" ", "x"}, Input{"x", ""}, `{"title":"x"}`, `{`, `{} {}`} {
		if status, result := send(t, base, body); status != 400 {
			t.Fatalf("%v: %d %v", body, status, result)
		}
	}
	if status, _ := post(t, base, Input{"x", "x"}, "Bearer "+testKey, "text/plain"); status != 415 {
		t.Fatal(status)
	}
	if status, _ := send(t, base, strings.Repeat("x", 17000)); status != 413 {
		t.Fatal(status)
	}
	req, _ := http.NewRequest("GET", base+"/notify", nil)
	req.Header.Set("Authorization", "Bearer "+testKey)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 405 {
		t.Fatal(response.StatusCode)
	}
	response, err = http.Get(base + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
}
func TestHeartbeatClosesDeadReceiver(t *testing.T) {
	relay, _ := New(testKey)
	relay.pingInterval = 15 * time.Millisecond
	httpServer := httptest.NewServer(relay)
	defer httpServer.Close()
	defer relay.Close()
	conn := connect(t, httpServer.URL)
	conn.SetPingHandler(func(string) error { return nil })
	conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("dead connection stayed open")
	}
	deadline := time.Now().Add(time.Second)
	for len(relay.snapshot()) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(relay.snapshot()) != 0 {
		t.Fatal("receiver not removed")
	}
}
func TestReceiveOnly(t *testing.T) {
	_, base := fixture(t)
	conn := connect(t, base)
	conn.WriteMessage(websocket.TextMessage, []byte("unexpected"))
	conn.SetReadDeadline(time.Now().Add(time.Second))
	_, _, err := conn.ReadMessage()
	if !websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
		t.Fatal(err)
	}
}
func TestConcurrentSend(t *testing.T) {
	_, base := fixture(t)
	conn := connect(t, base)
	const count = 40
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			status, _ := send(t, base, Input{fmt.Sprint(i), "concurrent"})
			if status != 200 {
				t.Error(status)
			}
		}(i)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	ids := map[string]bool{}
	for i := 0; i < count; i++ {
		var message Message
		if err := conn.ReadJSON(&message); err != nil {
			t.Fatal(err)
		}
		ids[message.ID] = true
		ack(t, conn, message.ID)
	}
	wg.Wait()
	if len(ids) != count {
		t.Fatal("duplicate IDs")
	}
}
func TestPrivateConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for _, key := range []string{"first", "second"} {
		if err := SaveKey(path, key); err != nil {
			t.Fatal(err)
		}
		actual, err := LoadKey(path)
		if err != nil || actual != key {
			t.Fatal(actual, err)
		}
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	for _, key := range []string{"", "bad key", "中文", "a\nb"} {
		if err := SaveKey(path, key); err == nil {
			t.Fatal("invalid key saved")
		}
	}
}
func BenchmarkRelay(b *testing.B) {
	relay, _ := New(testKey)
	defer relay.Close()
	srv := httptest.NewServer(relay)
	defer srv.Close()
	conn, _, err := websocket.DefaultDialer.Dial(strings.Replace(srv.URL, "http", "ws", 1)+"/ws", http.Header{"Authorization": []string{"Bearer " + testKey}})
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()
	go func() {
		for {
			var m Message
			if err := conn.ReadJSON(&m); err != nil {
				return
			}
			conn.WriteJSON(map[string]string{"type": "ack", "id": m.ID})
		}
	}()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("POST", srv.URL+"/notify", strings.NewReader(`{"title":"done","description":"**ok**"}`))
		req.Header.Set("Authorization", "Bearer "+testKey)
		req.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != 200 {
			b.Fatal(response.StatusCode)
		}
	}
}
