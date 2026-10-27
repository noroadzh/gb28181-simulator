package httpapi

// HTTP-FLV for a historical playback session: GET /v1/nodes/:id/channels/:ch/playback/flv?sn=.
//
// The body is produced by the same gateway the live endpoint uses: the
// simulator's "recording" is the channel's media source read from its
// beginning, and the session's window decides how long it runs.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

// handlePlaybackFLV streams an active playback session as FLV.
func (s *Server) handlePlaybackFLV(c echo.Context) error {
	if s.channels == nil || s.streaming == nil {
		return c.JSON(http.StatusNotImplemented, errorBody{Error: "playback streaming not available"})
	}
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	channelID := c.Param("ch")
	sn := c.QueryParam("sn")
	if channelID == "" || sn == "" {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "channel and sn are required"})
	}

	_, end, ok := s.channels.PlaybackWindow(sn)
	if !ok {
		return c.JSON(http.StatusNotFound, errorBody{Error: "unknown playback session " + sn})
	}
	window := time.Until(end)
	if window <= 0 {
		return c.JSON(http.StatusNotFound, errorBody{Error: "playback session " + sn + " has ended"})
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), window)
	defer cancel()
	// A recording preview is the local source by definition: preferring a
	// live pull here would show the live picture under a playback URL.
	ch, err := s.streaming.streaming.SubscribeSource(ctx, id, channelID, "local")
	if err != nil {
		return c.JSON(http.StatusNotFound, errorBody{Error: err.Error()})
	}

	c.Response().Header().Set(echo.HeaderContentType, "video/x-flv")
	c.Response().Header().Set("Access-Control-Allow-Origin", "*")
	c.Response().WriteHeader(http.StatusOK)

	flusher, _ := c.Response().Writer.(http.Flusher)
	for {
		select {
		case <-ctx.Done():
			return nil
		case data, ok := <-ch:
			if !ok {
				return nil
			}
			if _, werr := c.Response().Write(data); werr != nil {
				if errors.Is(werr, io.ErrClosedPipe) {
					return nil
				}
				return werr
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}
