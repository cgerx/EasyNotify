package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"
)

// BarkConfig is optional. Credentials belong in the private runtime config.
type BarkConfig struct {
	Enabled   bool   `json:"enabled"`
	Endpoint  string `json:"endpoint,omitempty"`
	DeviceKey string `json:"device_key,omitempty"`
	Group     string `json:"group,omitempty"`
}

type barkSender struct {
	config BarkConfig
	client *http.Client
	queue  chan Message
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

func newBarkSender(config BarkConfig) (*barkSender, error) {
	if !config.Enabled {
		return nil, nil
	}
	if ValidateKey(config.DeviceKey) != nil {
		return nil, errors.New("bark.device_key is required and must be printable ASCII without spaces")
	}
	if config.Endpoint == "" {
		config.Endpoint = "https://api.day.app/push"
	}
	u, err := url.Parse(config.Endpoint)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("bark.endpoint must be an HTTP(S) push URL without credentials, query or fragment")
	}
	if config.Group == "" {
		config.Group = "EasyNotify"
	}
	ctx, cancel := context.WithCancel(context.Background())
	b := &barkSender{config: config, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, queue: make(chan Message, 10000), ctx: ctx, cancel: cancel, done: make(chan struct{})}
	go b.run()
	return b, nil
}

func (b *barkSender) run() {
	defer close(b.done)
	for {
		select {
		case <-b.ctx.Done():
			return
		case message := <-b.queue:
			for attempt := 0; attempt < 3; attempt++ {
				if b.ctx.Err() != nil {
					return
				}
				retry, err := b.send(message)
				if err == nil {
					log.Printf("Bark accepted message %s", message.ID)
					break
				}
				// Never log response bodies, URLs or transport errors: they may contain secrets.
				log.Printf("Bark delivery failed for message %s (attempt %d): %s", message.ID, attempt+1, err)
				if !retry || attempt == 2 {
					break
				}
				timer := time.NewTimer(time.Duration(attempt+1) * time.Second)
				select {
				case <-b.ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}
	}
}

func (b *barkSender) send(message Message) (bool, error) {
	payload, _ := json.Marshal(map[string]any{"device_key": b.config.DeviceKey, "title": message.Title, "markdown": message.Description, "group": b.config.Group, "isArchive": 1, "id": message.ID})
	req, err := http.NewRequestWithContext(b.ctx, http.MethodPost, b.config.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return false, errors.New("invalid request")
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := b.client.Do(req)
	if err != nil {
		return true, errors.New("network request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode == 429 || response.StatusCode >= 500, errors.New("HTTP request rejected")
	}
	var result struct {
		Code int `json:"code"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&result) != nil {
		return true, errors.New("invalid response")
	}
	if result.Code != 200 {
		return result.Code == 429 || result.Code >= 500, errors.New("push request rejected")
	}
	return false, nil
}
