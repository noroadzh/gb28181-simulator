package httpapi

// Platform recording library endpoints:
//
//	GET /v1/nodes/:id/channels/:ch/recordings?start=&end=       list
//	GET /v1/nodes/:id/channels/:ch/recordings/:recording/flv   play as FLV

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

type recordingResponse struct {
	ID        string `json:"id"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	Size      int64  `json:"size"`
	FilePath  string `json:"file_path"`
}

type recordingListResponse struct {
	Recordings []recordingResponse `json:"recordings"`
}

func (s *Server) handleRecordingList(c echo.Context) error {
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
	start, err := parseOptionalTime(c.QueryParam("start"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid start: " + err.Error()})
	}
	end, err := parseOptionalTime(c.QueryParam("end"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid end: " + err.Error()})
	}

	recs, err := s.channels.Recordings(c.Request().Context(), id, channelID, start, end)
	if err != nil {
		return recordingError(c, err)
	}
	out := recordingListResponse{Recordings: make([]recordingResponse, 0, len(recs))}
	for _, rec := range recs {
		out.Recordings = append(out.Recordings, recordingResponse{
			ID:        rec.ID,
			StartTime: rec.Start.Format(time.RFC3339),
			EndTime:   rec.End.Format(time.RFC3339),
			Size:      rec.Size,
			FilePath:  rec.Path,
		})
	}
	return c.JSON(http.StatusOK, out)
}

func (s *Server) handleRecordingFLV(c echo.Context) error {
	if s.channels == nil || s.streaming == nil {
		return c.JSON(http.StatusNotImplemented, errorBody{Error: "streaming not available"})
	}
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	channelID := strings.TrimSpace(c.Param("ch"))
	recordingID := strings.TrimSpace(c.Param("recording"))
	if channelID == "" || recordingID == "" {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "channel and recording are required"})
	}
	if !s.nodeExists(c.Request().Context(), id) {
		return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
	}

	// The library is the source of truth for the recording's window: the
	// id alone does not say which span to play.
	recs, err := s.channels.Recordings(c.Request().Context(), id, channelID, time.Time{}, time.Time{})
	if err != nil {
		return recordingError(c, err)
	}
	var window port.Recording
	for _, rec := range recs {
		if rec.ID == recordingID {
			window = rec
			break
		}
	}
	if window.ID == "" {
		return c.JSON(http.StatusNotFound, errorBody{Error: "unknown recording " + recordingID})
	}

	ctx := c.Request().Context()
	ch, err := s.streaming.streaming.SubscribeRecording(ctx, id, channelID, window.Start, window.End)
	if err != nil {
		return recordingError(c, err)
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

// parseOptionalTime accepts an empty value as "unbounded".
func parseOptionalTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, raw)
}

func recordingError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, port.ErrRecordingUnsupported):
		return c.JSON(http.StatusNotImplemented, errorBody{Error: err.Error()})
	case errors.Is(err, port.ErrRecordingNotFound), errors.Is(err, model.ErrUnknownNode):
		return c.JSON(http.StatusNotFound, errorBody{Error: err.Error()})
	default:
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
}
