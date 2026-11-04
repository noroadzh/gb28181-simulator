package httpapi

// Cascade media-relay observability:
//
//	GET /v1/nodes/:id/cascade/sessions

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

type cascadeSessionResponse struct {
	UpstreamCallID string `json:"upstream_call_id"`
	ChannelID      string `json:"channel_id"`
	Peer           string `json:"peer"`
	Transport      string `json:"transport"`
	LocalPort      int    `json:"local_port"`
	StartedAt      string `json:"started_at"`
}

type cascadeSessionListResponse struct {
	Sessions []cascadeSessionResponse `json:"sessions"`
}

func (s *Server) handleCascadeSessions(c echo.Context) error {
	if s.channels == nil {
		return c.JSON(http.StatusNotImplemented, errorBody{Error: "channels not implemented"})
	}
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	if !s.nodeExists(c.Request().Context(), id) {
		return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
	}
	sessions := s.channels.ListCascadeSessions(c.Request().Context(), id)
	out := cascadeSessionListResponse{Sessions: make([]cascadeSessionResponse, 0, len(sessions))}
	for _, sess := range sessions {
		started := ""
		if !sess.StartedAt.IsZero() {
			started = sess.StartedAt.UTC().Format(time.RFC3339)
		}
		out.Sessions = append(out.Sessions, cascadeSessionResponse{
			UpstreamCallID: sess.UpstreamCallID,
			ChannelID:      sess.ChannelID,
			Peer:           sess.Peer,
			Transport:      sess.Transport,
			LocalPort:      sess.LocalPort,
			StartedAt:      started,
		})
	}
	return c.JSON(http.StatusOK, out)
}
