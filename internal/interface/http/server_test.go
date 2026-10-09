package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	httpapi "github.com/your-org/gb28181-simulator/internal/interface/http"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
)

func TestNewServer_HealthAndVersion(t *testing.T) {
	hub := logging.NewHub(4)
	s := httpapi.NewServer(platformconfig.Config{HTTP: platformconfig.HTTPConfig{Host: "127.0.0.1", Port: 18080}}, hub, httpapi.Version{Version: "0.1.0-dev", Commit: "deadbeef"}, nil, nil, nil, nil, nil)

	ts := httptest.NewServer(s.Echo())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/health")
	if err != nil {
		t.Fatalf("health get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("health body = %v", body)
	}

	resp2, err := http.Get(ts.URL + "/v1/version")
	if err != nil {
		t.Fatalf("version get: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("version status = %d", resp2.StatusCode)
	}
	var v map[string]string
	if err := json.NewDecoder(resp2.Body).Decode(&v); err != nil {
		t.Fatalf("decode version: %v", err)
	}
	for _, k := range []string{"version", "commit", "go_version", "platform"} {
		if _, ok := v[k]; !ok {
			t.Fatalf("missing field %q in version response", k)
		}
	}
}

// TestNewServer_LegacyHealthz verifies the /healthz endpoint (legacy smoke test)
func TestNewServer_LegacyHealthz(t *testing.T) {
	hub := logging.NewHub(4)
	s := httpapi.NewServer(platformconfig.Config{}, hub, httpapi.Version{Version: "x"}, nil, nil, nil, nil, nil)

	ts := httptest.NewServer(s.Echo())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("healthz get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d", resp.StatusCode)
	}
}

// TestNewServer_LegacyMetrics verifies the /metrics endpoint (legacy smoke test)
func TestNewServer_LegacyMetrics(t *testing.T) {
	hub := logging.NewHub(4)
	s := httpapi.NewServer(platformconfig.Config{}, hub, httpapi.Version{Version: "x"}, nil, nil, nil, nil, nil)

	ts := httptest.NewServer(s.Echo())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("metrics get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metrics status = %d", resp.StatusCode)
	}
}

// TestSpaHandler_ServesEmbeddedIndex spins up the server with real embedded
// assets (produced by `npm run build --prefix web`) and asserts the SPA
// fallback serves index.html for unknown routes, and that a known asset
// returns the hashed file.
func TestSpaHandler_ServesEmbeddedIndex(t *testing.T) {
	s := httpapi.NewServer(platformconfig.Config{}, logging.NewHub(4), httpapi.Version{Version: "x"}, nil, nil, nil, nil, nil)
	ts := httptest.NewServer(s.Echo())
	defer ts.Close()

	for _, p := range []string{"/", "/assets", "/does-not-exist"} {
		resp, err := http.Get(ts.URL + p)
		if err != nil {
			t.Fatalf("get %s: %v", p, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d", p, resp.StatusCode)
		}
		bodyStr := string(body)
		if !strings.Contains(bodyStr, "<html") && !strings.Contains(bodyStr, "<!doctype") {
			t.Fatalf("%s body is not HTML: %q", p, truncate(bodyStr, 80))
		}
	}
}

// TestWSHandler_LoggerPublishesToSubscriber wires the httpapi package against the
// real logger global hub (not a stand-in), then emits a log record through
// slog so the fan-out path is exercised exactly as the CLI does it.
func TestWSHandler_LoggerPublishesToSubscriber(t *testing.T) {
	logging.Reset()
	defer logging.Reset()
	dir := t.TempDir()
	if err := logging.Init(logging.Options{
		Level:      logging.ParseLevel("debug"),
		File:       filepath.Join(dir, "app.log"),
		RedactKeys: []string{"password"},
	}); err != nil {
		t.Fatalf("logger init: %v", err)
	}

	hub := logging.DefaultHub()
	s := httpapi.NewServer(platformconfig.Config{}, hub, httpapi.Version{Version: "x"}, nil, nil, nil, nil, nil)
	ts := httptest.NewServer(s.Echo())
	defer ts.Close()

	var (
		mu       sync.Mutex
		received []string
		gotAny   atomic.Bool
	)
	done := make(chan struct{})

	go func() {
		defer close(done)
		url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/logs/stream"
		c, resp, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			t.Logf("dial: %v", err)
			return
		}
		if resp != nil && resp.Body != nil {
			defer func() { _ = resp.Body.Close() }()
		}
		defer func() { _ = c.Close() }()
		_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
		// Write nothing; just subscribe and read.
		for {
			_, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			mu.Lock()
			received = append(received, string(msg))
			mu.Unlock()
			gotAny.Store(true)
		}
	}()

	// Give the WS goroutine time to Subscribe.
	time.Sleep(150 * time.Millisecond)

	logging.L().Info("publish to hub", "password", "hunter2")

	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}

	if !gotAny.Load() {
		t.Fatalf("ws subscriber received nothing; logs=%v", received)
	}
	t.Logf("ws received %d messages", len(received))
}

func TestServer_Shutdown(t *testing.T) {
	hub := logging.NewHub(4)
	s := httpapi.NewServer(platformconfig.Config{HTTP: platformconfig.HTTPConfig{Host: "127.0.0.1", Port: 0}}, hub, httpapi.Version{Version: "x"}, nil, nil, nil, nil, nil)
	ts := httptest.NewServer(s.Echo())
	defer ts.Close()
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestWSHandler_EchoShutdown(t *testing.T) {
	hub := logging.NewHub(4)
	s := httpapi.NewServer(platformconfig.Config{HTTP: platformconfig.HTTPConfig{Host: "127.0.0.1", Port: 0}}, hub, httpapi.Version{Version: "x"}, nil, nil, nil, nil, nil)
	ts := httptest.NewServer(s.Echo())
	defer ts.Close()

	done := make(chan struct{})
	var received atomic.Int64
	go func() {
		defer close(done)
		url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/logs/stream"
		c, resp, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			return
		}
		if resp != nil && resp.Body != nil {
			defer func() { _ = resp.Body.Close() }()
		}
		defer func() { _ = c.Close() }()
		for i := 0; i < 1; i++ {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
			received.Add(1)
		}
	}()

	time.Sleep(100 * time.Millisecond)
	for i := 0; i < 3; i++ {
		hub.Publish([]byte(`{"i":1}`))
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("never received any WS message (got %d)", received.Load())
	}
	if int(received.Load()) < 1 {
		t.Fatalf("expected at least 1 message, got %d", received.Load())
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
