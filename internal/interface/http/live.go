package httpapi

// Live-pull control endpoints:
//
//	POST /v1/nodes/:id/channels/:ch/live        start pulling the channel
//	POST /v1/nodes/:id/channels/:ch/live/stop   stop it (idempotent)
//
// The pull itself is a SIP dialog the platform opens with the downstream
// device; these endpoints only start and stop it.

import (
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

type liveStartInput struct {
	// Transport is "udp" (default) or "tcp" (GB/T 28181 附录 D).
	Transport string `json:"transport"`
}

type liveStartResponse struct {
	SessionID string `json:"session_id"`
	Transport string `json:"transport"`
	LocalPort int    `json:"local_port"`
	SSRC      uint32 `json:"ssrc,omitempty"`
}

func (s *Server) handleLiveStart(c echo.Context) error {
	if s.channels == nil {
		return c.JSON(http.StatusNotImplemented, errorBody{Error: "channels not implemented"})
	}
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	channelID := strings.TrimSpace(c.Param("ch"))
	if channelID == "" {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "channel id is required"})
	}
	if !s.nodeExists(c.Request().Context(), id) {
		return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
	}

	var in liveStartInput
	// An empty body means UDP; only malformed JSON is an error.
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid JSON: " + err.Error()})
	}
	transport := strings.ToLower(strings.TrimSpace(in.Transport))
	switch transport {
	case "", "udp":
		transport = "udp"
	case "tcp":
	default:
		return c.JSON(http.StatusBadRequest, errorBody{Error: `transport must be "udp" or "tcp"`})
	}

	info, err := s.channels.StartLive(c.Request().Context(), id, channelID, transport)
	if err != nil {
		return liveError(c, err)
	}
	return c.JSON(http.StatusOK, liveStartResponse{
		SessionID: info.SessionID,
		Transport: info.Transport,
		LocalPort: info.LocalPort,
		SSRC:      info.SSRC,
	})
}

func (s *Server) handleLiveStop(c echo.Context) error {
	if s.channels == nil {
		return c.JSON(http.StatusNotImplemented, errorBody{Error: "channels not implemented"})
	}
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	channelID := strings.TrimSpace(c.Param("ch"))
	if channelID == "" {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "channel id is required"})
	}
	if !s.nodeExists(c.Request().Context(), id) {
		return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
	}
	if err := s.channels.StopLive(c.Request().Context(), id, channelID); err != nil {
		return liveError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// liveError maps the live-pull vocabulary onto HTTP. The distinction that
// matters to a caller: 404 means "there is nothing to pull", 501 means "this
// node cannot pull at all", 502/504 mean "the device did not deliver".
func liveError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, model.ErrUnknownNode), errors.Is(err, model.ErrUnknownDevice):
		return c.JSON(http.StatusNotFound, errorBody{Error: err.Error()})
	case errors.Is(err, port.ErrLiveUnsupported):
		return c.JSON(http.StatusNotImplemented, errorBody{Error: err.Error()})
	case errors.Is(err, port.ErrLiveNoTarget):
		return c.JSON(http.StatusNotFound, errorBody{Error: err.Error()})
	case errors.Is(err, port.ErrLiveRefused), errors.Is(err, port.ErrControlTargetUnreachable):
		return c.JSON(http.StatusBadGateway, errorBody{Error: err.Error()})
	case errors.Is(err, port.ErrControlTimeout):
		return c.JSON(http.StatusGatewayTimeout, errorBody{Error: err.Error()})
	default:
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
}
