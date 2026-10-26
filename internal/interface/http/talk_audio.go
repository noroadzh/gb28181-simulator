package httpapi

// WebSocket audio for a voice-intercom session: down-link frames are the
// device's PCMU payloads, up-link frames are the browser's. Closing the socket
// only unsubscribes — ending the dialog is talk/stop's job.

import (
	"net/http"

	gws "github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

// handleTalkAudio serves /v1/nodes/:id/channels/:ch/talk/audio.
func (s *Server) handleTalkAudio(c echo.Context) error {
	if s.channels == nil {
		return c.JSON(http.StatusNotImplemented, errorBody{Error: "channels not implemented"})
	}
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	channelID := c.Param("ch")
	sessionID := c.QueryParam("session")
	if channelID == "" || sessionID == "" {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "channel and session are required"})
	}
	ctx := c.Request().Context()
	audio, unsubscribe, err := s.channels.SubscribeTalkAudio(ctx, id, channelID, sessionID)
	if err != nil {
		return c.JSON(http.StatusNotFound, errorBody{Error: "unknown talk session " + sessionID})
	}
	defer unsubscribe()

	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	// Up-link: one RTP packet per binary frame.
	go func() {
		for {
			kind, data, readErr := conn.ReadMessage()
			if readErr != nil {
				return
			}
			if kind != gws.BinaryMessage {
				continue
			}
			_ = s.channels.SendTalkAudio(ctx, id, channelID, sessionID, data)
		}
	}()

	// Down-link: publish the device's payloads until the client goes away.
	for payload := range audio {
		if err := conn.WriteMessage(gws.BinaryMessage, payload); err != nil {
			return nil
		}
	}
	return nil
}
