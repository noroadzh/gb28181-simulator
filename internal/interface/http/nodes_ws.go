package httpapi

// WebSocket fan-out for /v1/nodes/stream.
//
// Why this is a separate file from the existing /v1/logs/stream handler: the
// logs handler subscribes to a logging.Hub whose payload is already a
// []byte (the rendered slog record). The events hub publishes typed values
// that need JSON encoding, and a subscriber that fails JSON encoding must
// not be able to wedge the publish path. Keeping the two handlers in their
// own files makes the difference in payload shape and error policy obvious
// at the call site.

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	gws "github.com/gorilla/websocket"
)

// nodeStreamPingInterval bounds how long a silent connection may sit idle
// before we ask the client to confirm. Two minutes is long enough that a
// quiet network passes the deadline, short enough that half-open sockets
// surface within a human's attention span rather than at the next deploy.
const nodeStreamPingInterval = 2 * time.Minute

// handleNodeStatusStream serves the live node-state feed. It upgrades the
// HTTP request to a WebSocket and pipes events.Hub publications to the
// client as text frames.
//
// When the server was built without an events hub (legacy bootstrap, tests
// that exercise the REST surface only) the endpoint reports 503 rather
// than 101: a WebUI connecting to a node stream should not silently hold
// an open socket that will never see a frame.
func (s *Server) handleNodeStatusStream(c echo.Context) error {
	if s.events == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": "node event hub is not configured on this server",
		})
	}
	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	sub := s.events.Subscribe()
	defer s.events.Unsubscribe(sub)

	// Read loop only to detect client close and to answer pongs; the wire
	// protocol is server-push only for now, so client frames are ignored
	// once we know the socket is alive.
	clientGone := make(chan struct{})
	go func() {
		defer close(clientGone)
		conn.SetReadLimit(64)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ping := time.NewTicker(nodeStreamPingInterval)
	defer ping.Stop()

	for {
		select {
		case <-clientGone:
			return nil
		case <-ping.C:
			// WriteMessage(ping) is not in gorilla/websocket; we use the
			// control-message variant so the client receives a ping frame
			// without us having to invent a payload format.
			if err := conn.WriteControl(gws.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				return nil
			}
		case ev, ok := <-sub.Chan():
			if !ok {
				return nil
			}
			payload, err := json.Marshal(ev)
			if err != nil {
				// Encoding a fixed-shape value should not fail; if it
				// does, drop the event rather than closing the socket,
				// so one malformed event cannot silence the whole feed.
				s.log.Warn("node event encode failed", "err", err, "type", ev.Type)
				continue
			}
			if err := conn.WriteMessage(gws.TextMessage, payload); err != nil {
				return nil
			}
		}
	}
}
