package httpapi

// Operations endpoints added by device-control-coverage. They are served only
// when the node view also implements opsView, so test doubles and partial
// wirings keep compiling without carrying the whole surface.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	app "github.com/noroadzh/gb28181-simulator/internal/app"
	"github.com/noroadzh/gb28181-simulator/internal/domain/model"
	"github.com/noroadzh/gb28181-simulator/internal/domain/port"
)

// opsView is the operations slice of the node service.
type opsView interface {
	DeviceStatus(ctx context.Context, id model.NodeID) (bool, error)
	Alarms(ctx context.Context, id model.NodeID, start, end time.Time) ([]model.AlarmSnapshot, error)
	SetAlarmMode(ctx context.Context, id model.NodeID, armed bool) error
	AlarmMode(ctx context.Context, id model.NodeID) (armed, set bool, err error)
	Config(ctx context.Context, id model.NodeID) (model.BasicConfig, error)
	SetConfig(ctx context.Context, id model.NodeID, cfg model.BasicConfig) error
	Upgrade(ctx context.Context, id model.NodeID, version string) error
	Reboot(ctx context.Context, id model.NodeID) error
	StartDownload(ctx context.Context, id model.NodeID, channelID string, start, end time.Time, transport string) (string, error)
}

func (s *Server) ops(c echo.Context) (opsView, bool) {
	v, ok := s.nodes.(opsView)
	if !ok {
		_ = c.JSON(http.StatusNotImplemented, errorBody{Error: "operations not implemented"})
	}
	return v, ok
}

type statusResponse struct {
	NodeID string `json:"node_id"`
	Online bool   `json:"online"`
}

// handleNodeStatus answers GET /v1/nodes/:id/status.
func (s *Server) handleNodeStatus(c echo.Context) error {
	ops, ok := s.ops(c)
	if !ok {
		return nil
	}
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	online, err := ops.DeviceStatus(c.Request().Context(), id)
	if err != nil {
		return opsError(c, err)
	}
	return c.JSON(http.StatusOK, statusResponse{NodeID: id.String(), Online: online})
}

type alarmItem struct {
	DeviceID    string `json:"device_id"`
	ChannelID   string `json:"channel_id,omitempty"`
	Priority    int    `json:"priority"`
	Method      int    `json:"method"`
	Description string `json:"description,omitempty"`
	EventTime   string `json:"event_time"`
}

// handleChannelAlarms answers GET /v1/nodes/:id/channels/:ch/alarms.
func (s *Server) handleChannelAlarms(c echo.Context) error {
	ops, ok := s.ops(c)
	if !ok {
		return nil
	}
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	start, err := parseOptionalTime(c.QueryParam("start"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid start: " + err.Error()})
	}
	end, err := parseOptionalTime(c.QueryParam("end"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid end: " + err.Error()})
	}
	alarms, err := ops.Alarms(c.Request().Context(), id, start, end)
	if err != nil {
		return opsError(c, err)
	}
	items := make([]alarmItem, 0, len(alarms))
	for _, a := range alarms {
		items = append(items, alarmItem{
			DeviceID: a.DeviceID(), ChannelID: a.ChannelID(), Priority: a.Priority(),
			Method: a.Method(), Description: a.Description(), EventTime: a.EventTime(),
		})
	}
	return c.JSON(http.StatusOK, map[string]any{"alarms": items})
}

type alarmModeRequest struct {
	Mode string `json:"mode"`
}

// handleAlarmMode answers POST /v1/nodes/:id/channels/:ch/alarm-mode.
func (s *Server) handleAlarmMode(c echo.Context) error {
	ops, ok := s.ops(c)
	if !ok {
		return nil
	}
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	var in alarmModeRequest
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid JSON: " + err.Error()})
	}
	var armed bool
	switch strings.ToLower(strings.TrimSpace(in.Mode)) {
	case "armed":
		armed = true
	case "disarmed":
		armed = false
	default:
		return c.JSON(http.StatusBadRequest, errorBody{Error: `mode must be "armed" or "disarmed"`})
	}
	if err := ops.SetAlarmMode(c.Request().Context(), id, armed); err != nil {
		return opsError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"mode": strings.ToLower(in.Mode)})
}

func (s *Server) handleNodeConfigGet(c echo.Context) error {
	ops, ok := s.ops(c)
	if !ok {
		return nil
	}
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	cfg, err := ops.Config(c.Request().Context(), id)
	if err != nil {
		return opsError(c, err)
	}
	return c.JSON(http.StatusOK, cfg)
}

func (s *Server) handleNodeConfigPut(c echo.Context) error {
	ops, ok := s.ops(c)
	if !ok {
		return nil
	}
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	var cfg model.BasicConfig
	if err := c.Bind(&cfg); err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid JSON: " + err.Error()})
	}
	if err := ops.SetConfig(c.Request().Context(), id, cfg); err != nil {
		return opsError(c, err)
	}
	return c.JSON(http.StatusOK, cfg)
}

type upgradeRequest struct {
	Version string `json:"version"`
}

func (s *Server) handleNodeUpgrade(c echo.Context) error {
	ops, ok := s.ops(c)
	if !ok {
		return nil
	}
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	var in upgradeRequest
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid JSON: " + err.Error()})
	}
	if err := ops.Upgrade(c.Request().Context(), id, in.Version); err != nil {
		return opsError(c, err)
	}
	return c.JSON(http.StatusOK, in)
}

func (s *Server) handleNodeReboot(c echo.Context) error {
	ops, ok := s.ops(c)
	if !ok {
		return nil
	}
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	if err := ops.Reboot(c.Request().Context(), id); err != nil {
		return opsError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

type downloadRequest struct {
	Start     string `json:"start"`
	End       string `json:"end"`
	Transport string `json:"transport"`
}

func (s *Server) handleChannelDownload(c echo.Context) error {
	ops, ok := s.ops(c)
	if !ok {
		return nil
	}
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	channelID := strings.TrimSpace(c.Param("ch"))
	if channelID == "" {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "channel id is required"})
	}
	var in downloadRequest
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid JSON: " + err.Error()})
	}
	start, err := time.Parse(time.RFC3339, in.Start)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid start: " + err.Error()})
	}
	end, err := time.Parse(time.RFC3339, in.End)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid end: " + err.Error()})
	}
	transport := strings.ToLower(strings.TrimSpace(in.Transport))
	switch transport {
	case "", "udp":
		transport = "udp"
	case "tcp":
	default:
		return c.JSON(http.StatusBadRequest, errorBody{Error: `transport must be "udp" or "tcp"`})
	}
	callID, err := ops.StartDownload(c.Request().Context(), id, channelID, start, end, transport)
	if err != nil {
		return opsError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"call_id": callID, "transport": transport})
}

// opsError maps the operations vocabulary onto HTTP.
func opsError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, model.ErrUnknownNode):
		return c.JSON(http.StatusNotFound, errorBody{Error: err.Error()})
	case errors.Is(err, app.ErrOpsUnsupported), errors.Is(err, port.ErrLiveUnsupported),
		errors.Is(err, port.ErrRecordingUnsupported), errors.Is(err, port.ErrPlaybackUnsupported):
		return c.JSON(http.StatusNotImplemented, errorBody{Error: err.Error()})
	case errors.Is(err, port.ErrControlTargetUnreachable), errors.Is(err, port.ErrLiveRefused):
		return c.JSON(http.StatusBadGateway, errorBody{Error: err.Error()})
	case errors.Is(err, port.ErrControlTimeout):
		return c.JSON(http.StatusGatewayTimeout, errorBody{Error: err.Error()})
	default:
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
}
