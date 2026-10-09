// Package httpapi — HTTP-FLV streaming handlers.
package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// StreamingService is the HTTP-layer interface to the streaming gateway.
type StreamingService interface {
	Subscribe(ctx context.Context, nodeID model.NodeID, channelID string) (<-chan []byte, error)
	Subscribers(nodeID model.NodeID, channelID string) int
	Close() error
}

// StreamingServer handles HTTP-FLV requests.
type StreamingServer struct {
	streaming StreamingService
	log       *slog.Logger
}

// NewStreamingServer builds a StreamingServer.
func NewStreamingServer(streaming StreamingService, log *slog.Logger) *StreamingServer {
	if log == nil {
		log = slog.Default()
	}
	return &StreamingServer{streaming: streaming, log: log}
}

// HandleFLV is the HTTP-FLV endpoint: GET /v1/flv/:nodeID/:channelID
// Returns the FLV stream for the given node + channel.
//
// Response headers:
//   - Content-Type: video/x-flv
//   - Transfer-Encoding: chunked (implicit via Write)
//
// The response is FLV header + first PreviousTagSize + first tag,
// followed by N video tags, terminated when the source closes.
func (s *StreamingServer) HandleFLV(c echo.Context) error {
	nodeIDStr := c.Param("nodeID")
	channelID := c.Param("channelID")
	if nodeIDStr == "" || channelID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "nodeID and channelID are required")
	}
	nodeID, err := model.ParseNodeID(nodeIDStr)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	ctx := c.Request().Context()
	ch, err := s.streaming.Subscribe(ctx, nodeID, channelID)
	if err != nil {
		// All Subscribe errors translate to 404 (unsupported, not found, no source).
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	}

	c.Response().Header().Set(echo.HeaderContentType, "video/x-flv")
	c.Response().Header().Set("Access-Control-Allow-Origin", "*")
	c.Response().WriteHeader(http.StatusOK)

	// Stream chunks: each chunk is a FLV tag (header + body + PreviousTagSize).
	flusher, _ := c.Response().Writer.(http.Flusher)
	for {
		select {
		case <-ctx.Done():
			return nil
		case data, ok := <-ch:
			if !ok {
				return nil
			}
			if _, err := c.Response().Write(data); err != nil {
				if errors.Is(err, io.ErrClosedPipe) {
					return nil
				}
				return err
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}
