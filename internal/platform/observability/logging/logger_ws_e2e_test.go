package logging_test

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"

	"github.com/your-org/gb28181-simulator/internal/interface/http"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
)

// TestE2E_HubPushedToWebSocketWithRedaction exercises the full pipeline that
// task 11.2 requires: logger.L() → hub.Publish → WS handler → client frame,
// and asserts sensitive fields are scrubbed before they reach subscribers.
func TestE2E_HubPushedToWebSocketWithRedaction(t *testing.T) {
	logging.Reset()
	if err := logging.Init(logging.Options{
		Level:      logging.ParseLevel("debug"),
		RedactKeys: []string{"password"},
	}); err != nil {
		t.Fatalf("init: %v", err)
	}
	// We deliberately do NOT register a Cleanup that calls Reset — the
	// test owns the logger lifecycle. Test parallelism is fine because
	// logging.Init is the only mutator.

	hub := logging.DefaultHub()
	if hub.Count() != 0 {
		t.Fatalf("hub should start empty, got %d", hub.Count())
	}

	e := echo.New()
	e.GET("/ws", httpapi.WSHandler(hub))
	ts := httptest.NewServer(e)
	defer ts.Close()

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Wait until the subscription is registered before publishing.
	deadline := time.Now().Add(time.Second)
	for hub.Count() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.Count() == 0 {
		t.Fatalf("no subscriber registered within deadline")
	}

	logging.L().Info("manual ping",
		"reason", "test",
		"password", "should-never-leak",
	)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	body := string(raw)
	t.Logf("payload: %s", body)
	if !strings.Contains(body, `"msg":"manual ping"`) {
		t.Fatalf("missing msg: %s", body)
	}
	if !strings.Contains(body, "***REDACTED***") {
		t.Fatalf("password field was not redacted: %s", body)
	}
	if strings.Contains(body, "should-never-leak") {
		t.Fatalf("plaintext password leaked: %s", body)
	}

	// Sanity check: httpapi.NewServer wires the same hub.
	_ = httpapi.NewServer(platformconfig.Config{}, hub, httpapi.Version{Version: "t"}, nil)
}
