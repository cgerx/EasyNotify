package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestBarkIndependentDeliveryAndRetry(t *testing.T) {
	received := make(chan map[string]any, 3)
	release := make(chan struct{})
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		received <- payload
		if calls.Add(1) == 1 {
			<-release
			w.WriteHeader(503)
			return
		}
		reply(w, 200, map[string]int{"code": 200})
	}))
	defer upstream.Close()
	relay, err := NewWithConfig(Config{Key: testKey, Bark: BarkConfig{Enabled: true, Endpoint: upstream.URL, DeviceKey: "dummy-bark-key"}})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	srv := httptest.NewServer(relay)
	defer srv.Close()
	conn := connect(t, srv.URL)
	status, result := send(t, srv.URL, Input{"完成", "## 结果\n**成功**"})
	if status != 200 {
		t.Fatal(status, result)
	}
	notice := readNotice(t, conn)
	ack(t, conn, notice.ID)
	waitQueue(t, relay, 0)
	close(release)
	for i := 0; i < 2; i++ {
		select {
		case payload := <-received:
			if payload["device_key"] != "dummy-bark-key" || payload["markdown"] != notice.Description || payload["title"] != notice.Title || payload["id"] != notice.ID || payload["group"] != "EasyNotify" {
				t.Fatal(payload)
			}
		case <-time.After(4 * time.Second):
			t.Fatal("Bark delivery did not retry independently of Mac ACK")
		}
	}
}

func TestBarkResponseAndConfiguration(t *testing.T) {
	for _, cfg := range []BarkConfig{{Enabled: true}, {Enabled: true, DeviceKey: "dummy", Endpoint: "file:///tmp/push"}, {Enabled: true, DeviceKey: "dummy", Endpoint: "https://user:secret@example.com/push"}} {
		if _, err := newBarkSender(cfg); err == nil {
			t.Fatal("accepted invalid config")
		}
	}
	if b, err := newBarkSender(BarkConfig{}); b != nil || err != nil {
		t.Fatal("disabled Bark must remain optional")
	}
	for _, test := range []struct {
		body   string
		status int
		retry  bool
	}{
		{`{"code":400}`, 200, false}, {`{"code":500}`, 200, true}, {`bad-json`, 200, true}, {``, 401, false}, {``, 429, true},
	} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.status); w.Write([]byte(test.body)) }))
		b, _ := newBarkSender(BarkConfig{Enabled: true, DeviceKey: "dummy", Endpoint: upstream.URL})
		retry, err := b.send(Message{})
		b.cancel()
		<-b.done
		upstream.Close()
		if err == nil || retry != test.retry {
			t.Fatalf("response %s: retry=%v err=%v", test.body, retry, err)
		}
	}
}

func TestSaveKeyPreservesBark(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, []byte(`{"key":"old","bark":{"enabled":true,"device_key":"dummy"},"future":42}`), 0600)
	if err := SaveKey(path, "new"); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(path)
	if err != nil || config.Key != "new" || config.Bark.DeviceKey != "dummy" || !config.Bark.Enabled {
		t.Fatal(config, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("configuration must be private")
	}
}
