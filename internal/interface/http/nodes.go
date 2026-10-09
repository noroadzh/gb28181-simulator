package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	app "github.com/your-org/gb28181-simulator/internal/app"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// nodeExists is a nil-safe helper that returns false when s.nodes is nil,
// preventing nil pointer panics in handlers that are registered unconditionally
// but may be exercised before the application wires up the node registry.
func (s *Server) nodeExists(ctx context.Context, id model.NodeID) bool {
	if s.nodes == nil {
		return false
	}
	_, ok := s.nodes.Get(ctx, id)
	return ok
}

// PositionInput is the payload for PUT /nodes/{id}/actions/position.
// Longitude/latitude are WGS-84 decimal degrees, speed is m/s.
type PositionInput struct {
	Longitude float64 `json:"longitude"`
	Latitude  float64 `json:"latitude"`
	Speed     float64 `json:"speed"`
}

// ChannelStatusInput is the payload for POST /nodes/{id}/channels/:ch/status.
type ChannelStatusInput struct {
	Status string `json:"status"`
}

// NodeView is the slice of node operations the HTTP layer needs. Depending
// on a narrow interface (rather than on *app.NodeService) keeps this package
// testable and keeps the dependency pointed at the domain.
type NodeView interface {
	List(ctx context.Context) []model.Node
	Get(ctx context.Context, id model.NodeID) (model.Node, bool)
	Start(ctx context.Context, id model.NodeID) error
	Stop(ctx context.Context, id model.NodeID) error
	Unregister(ctx context.Context, id model.NodeID) error
	Devices(ctx context.Context, id model.NodeID) ([]model.DownstreamDevice, error)
	Device(ctx context.Context, id model.NodeID, deviceID string) (model.DownstreamDevice, error)
	// TriggerAlarm makes the node emit an Alarm NOTIFY upstream and records
	// the snapshot in its profile. It returns the stored snapshot.
	TriggerAlarm(ctx context.Context, id model.NodeID, in app.AlarmInput) (model.AlarmSnapshot, error)
	// SetPosition overwrites the node's last-known geographic position.
	SetPosition(ctx context.Context, id model.NodeID, pos model.Position) error
	// SetChannelStatus updates the online/offline status of one dynamic channel.
	SetChannelStatus(ctx context.Context, id model.NodeID, channelID string, status model.ChannelStatus) error

	// AddChannel adds a new dynamic channel to a device node. Returns the
	// created channel.
	AddChannel(ctx context.Context, id model.NodeID, channelID, name, parentID string, status model.ChannelStatus) (model.Channel, error)
	// RemoveChannel removes a dynamic channel from a device node.
	RemoveChannel(ctx context.Context, id model.NodeID, channelID string) error

	// InstallFault arms the node with the supplied fault profile. A zero
	// profile is accepted but callers prefer ClearFault when clearing.
	InstallFault(ctx context.Context, id model.NodeID, p model.FaultProfile) error
	// GetFault returns the installed profile, its action counters, and
	// whether one exists. A node with no profile yields (zero, empty,
	// false).
	GetFault(ctx context.Context, id model.NodeID) (model.FaultProfile, map[model.FaultAction]uint64, bool)
	// ClearFault removes the profile and resets counters.
	ClearFault(ctx context.Context, id model.NodeID) error

	// QueryCapture returns up to limit capture events for the node, oldest
	// first among the returned subset. A node with no buffer yields nil.
	QueryCapture(ctx context.Context, nodeID model.NodeID, limit int) ([]port.CaptureEvent, error)
	// CapturePCAP renders the node's buffered capture as a pcap document.
	CapturePCAP(ctx context.Context, nodeID model.NodeID) ([]byte, error)

	// GetMedia returns the node's media config, or (zero, false) when none is set.
	GetMedia(ctx context.Context, id model.NodeID) (model.MediaConfig, bool, error)
	// SetMedia validates and stores the supplied config on the node's profile.
	SetMedia(ctx context.Context, id model.NodeID, cfg model.MediaConfig) error
	// ClearMedia removes any configured media from the node's profile.
	ClearMedia(ctx context.Context, id model.NodeID) error
}

// nodeResponse is the JSON shape of a node. Deliberately flat and
// lower-case: it is the contract the future Web UI (Change 14) will bind to.
// FaultCounters is non-empty only while a fault profile is armed; it resets
// to empty when the profile is cleared.
type nodeResponse struct {
	ID            string                       `json:"id"`
	Kind          string                       `json:"kind"`
	Status        string                       `json:"status"`
	Addr          string                       `json:"addr"`
	FaultCounters map[model.FaultAction]uint64 `json:"fault_counters,omitempty"`
}

// errorBody is the uniform JSON error envelope for the node endpoints.
type errorBody struct {
	Error string `json:"error"`
}

// faultState is the JSON body the fault endpoints share: the installed
// profile plus the action counters observed since it was armed.
type faultState struct {
	Status   string                       `json:"status"`
	Profile  model.FaultProfile           `json:"profile"`
	Counters map[model.FaultAction]uint64 `json:"counters"`
}

// nodeError adds the node's current status to a conflict response so a
// caller can see what the request collided with. Stage is set when the
// failure happened inside a multi-step operation (e.g. a registration
// transaction) and names the step, so the response says more than "it
// failed".
type nodeError struct {
	Error  string `json:"error"`
	Status string `json:"status"`
	Action string `json:"action"`
	Stage  string `json:"stage,omitempty"`
}

func newNodeResponse(n model.Node, counters map[model.FaultAction]uint64) nodeResponse {
	return nodeResponse{
		ID:            n.ID().String(),
		Kind:          n.Profile().Kind().String(),
		Status:        n.Status().String(),
		Addr:          n.Profile().Addr(),
		FaultCounters: counters,
	}
}

// handleNodeList returns every registered node; with zero nodes it returns
// an empty JSON array, never null.
func (s *Server) handleNodeList(c echo.Context) error {
	if s.nodes == nil {
		return c.JSON(http.StatusOK, []nodeResponse{})
	}
	nodes := s.nodes.List(c.Request().Context())
	out := make([]nodeResponse, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, newNodeResponse(n, nil))
	}
	return c.JSON(http.StatusOK, out)
}

// handleNodeDetail returns one node, or 404 with a JSON error body.
func (s *Server) handleNodeDetail(c echo.Context) error {
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	ctx := c.Request().Context()
	n, ok := s.nodes.Get(ctx, id)
	if !ok {
		return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
	}
	counters := map[model.FaultAction]uint64{}
	if _, c, ok := s.nodes.GetFault(ctx, id); ok {
		counters = c
	}
	return c.JSON(http.StatusOK, newNodeResponse(n, counters))
}

// handleNodeStart starts one node. An unknown id is 404; an illegal status
// transition is 409 and reports the status the node is actually in.
func (s *Server) handleNodeStart(c echo.Context) error {
	return s.controlNode(c, "start", NodeView.Start)
}

// handleNodeStop stops one node, with the same status mapping as start.
func (s *Server) handleNodeStop(c echo.Context) error {
	return s.controlNode(c, "stop", NodeView.Stop)
}

// handleNodeUnregister asks the platform to forget one node. It shares the
// start/stop mapping, so a refused unregistration is reported as a bad
// gateway naming the stage, and unregistering a node that is not online is a
// conflict.
func (s *Server) handleNodeUnregister(c echo.Context) error {
	return s.controlNode(c, "unregister", NodeView.Unregister)
}

// deviceResponse is the JSON shape of one row of a platform's online
// device table. It carries what an operator needs to see — who is
// connected, from where, until when — and deliberately nothing about how
// that device proved itself.
type deviceResponse struct {
	DeviceID     string `json:"device_id"`
	Addr         string `json:"addr"`
	Contact      string `json:"contact,omitempty"`
	Transport    string `json:"transport,omitempty"`
	GBVersion    string `json:"gb_version,omitempty"`
	Expires      uint32 `json:"expires"`
	RegisteredAt string `json:"registered_at"`
	ExpiresAt    string `json:"expires_at"`
	LastSeenAt   string `json:"last_seen_at"`
}

func newDeviceResponse(d model.DownstreamDevice) deviceResponse {
	return deviceResponse{
		DeviceID:     d.DeviceID(),
		Addr:         d.Addr(),
		Contact:      d.Contact(),
		Transport:    d.Transport(),
		GBVersion:    d.GBVersion(),
		Expires:      d.GrantedExpiry(),
		RegisteredAt: d.RegisteredAt().UTC().Format(time.RFC3339),
		ExpiresAt:    d.ExpiresAt().UTC().Format(time.RFC3339),
		LastSeenAt:   d.LastSeenAt().UTC().Format(time.RFC3339),
	}
}

// handleNodeDevices lists the devices registered with a platform-large
// node. An unknown node is 404; a node with no devices is an empty array,
// never null.
func (s *Server) handleNodeDevices(c echo.Context) error {
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	devices, err := s.nodes.Devices(c.Request().Context(), id)
	if err != nil {
		if errors.Is(err, model.ErrUnknownNode) {
			return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
		}
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
	out := make([]deviceResponse, 0, len(devices))
	for _, d := range devices {
		out = append(out, newDeviceResponse(d))
	}
	return c.JSON(http.StatusOK, out)
}

// handleNodeDevice returns one device of a platform-large node, or 404 when
// the node or the device is unknown.
func (s *Server) handleNodeDevice(c echo.Context) error {
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	deviceID := c.Param("deviceID")
	if deviceID == "" {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "empty device id"})
	}
	dev, err := s.nodes.Device(c.Request().Context(), id, deviceID)
	if err != nil {
		if errors.Is(err, model.ErrUnknownNode) {
			return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
		}
		if errors.Is(err, model.ErrUnknownDevice) {
			return c.JSON(http.StatusNotFound, errorBody{Error: "unknown device " + deviceID})
		}
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
	return c.JSON(http.StatusOK, newDeviceResponse(dev))
}

// controlNode is the shared body of the start/stop endpoints.
func (s *Server) controlNode(
	c echo.Context,
	action string,
	op func(NodeView, context.Context, model.NodeID) error,
) error {
	id, err := s.nodeIDParam(c)
	if err != nil {
		s.log.Warn("node action rejected: bad id", "action", action, "error", err.Error())
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	ctx := c.Request().Context()
	if _, ok := s.nodes.Get(ctx, id); !ok {
		s.log.Warn("node action on unknown node", "action", action, "node_id", id.String())
		return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
	}
	if err := op(s.nodes, ctx, id); err != nil {
		// A failure inside a multi-step operation (a registration that was
		// refused or timed out) is the peer's answer, not a bug in the
		// simulator: report it as bad gateway and name the stage.
		var staged port.StagedFailure
		if errors.As(err, &staged) {
			current := "unknown"
			if n, ok := s.nodes.Get(ctx, id); ok {
				current = n.Status().String()
			}
			s.log.Warn("node action failed at a staged step",
				"action", action, "node_id", id.String(),
				"stage", staged.FailureStage(), "error", err.Error())
			return c.JSON(http.StatusBadGateway, nodeError{
				Error:  err.Error(),
				Status: current,
				Action: action,
				Stage:  staged.FailureStage(),
			})
		}
		// A node that exists but refuses the transition is a conflict, not a
		// server error (design D8).
		if errors.Is(err, model.ErrIllegalTransition) {
			current := "unknown"
			if n, ok := s.nodes.Get(ctx, id); ok {
				current = n.Status().String()
			}
			s.log.Warn("node action refused: illegal transition",
				"action", action, "node_id", id.String(),
				"current_status", current, "error", err.Error())
			return c.JSON(http.StatusConflict, nodeError{
				Error:  err.Error(),
				Status: current,
				Action: action,
			})
		}
		s.log.Error("node action failed unexpectedly",
			"action", action, "node_id", id.String(), "error", err.Error())
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
	after, _ := s.nodes.Get(ctx, id)
	s.log.Info("node action done",
		"action", action, "node_id", id.String(), "status", after.Status().String())
	return c.JSON(http.StatusOK, newNodeResponse(after, nil))
}

// handleTriggerAlarm makes the node emit a manual Alarm NOTIFY upstream and
// records the snapshot in its profile. This is the Change 10 dynamic
// simulation endpoint: callers drive the alarm lifecycle without waiting
// for a real camera event.
func (s *Server) handleTriggerAlarm(c echo.Context) error {
	id, err := s.nodeIDParam(c)
	if err != nil {
		s.log.Warn("trigger-alarm rejected: bad node id", "error", err.Error())
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	ctx := c.Request().Context()
	if _, ok := s.nodes.Get(ctx, id); !ok {
		s.log.Warn("trigger-alarm on unknown node", "node_id", id.String())
		return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
	}
	var in app.AlarmInput
	if err := c.Bind(&in); err != nil {
		s.log.Warn("trigger-alarm rejected: bad body", "node_id", id.String(), "error", err.Error())
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid request body"})
	}
	if in.Priority < 1 || in.Priority > 4 {
		s.log.Warn("trigger-alarm rejected: invalid priority",
			"node_id", id.String(), "priority", in.Priority)
		return c.JSON(http.StatusBadRequest, errorBody{Error: "priority must be 1..4"})
	}
	snap, err := s.nodes.TriggerAlarm(ctx, id, in)
	if err != nil {
		s.log.Error("trigger-alarm failed", "node_id", id.String(), "error", err.Error())
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
	s.log.Info("trigger-alarm emitted",
		"node_id", id.String(),
		"alarm_id", snap.ID(),
		"priority", snap.Priority(),
		"description", snap.Description())
	return c.JSON(http.StatusAccepted, map[string]string{
		"id":          snap.ID(),
		"deviceID":    snap.DeviceID(),
		"eventTime":   snap.EventTime(),
		"description": snap.Description(),
	})
}

// handleSetPosition overwrites the node's geographic position. Used by the
// Web UI (Change 14) to simulate a vehicle moving or a device being
// relocated.
func (s *Server) handleSetPosition(c echo.Context) error {
	id, err := s.nodeIDParam(c)
	if err != nil {
		s.log.Warn("set-position rejected: bad node id", "error", err.Error())
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	ctx := c.Request().Context()
	if _, ok := s.nodes.Get(ctx, id); !ok {
		s.log.Warn("set-position on unknown node", "node_id", id.String())
		return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
	}
	var in PositionInput
	if err := c.Bind(&in); err != nil {
		s.log.Warn("set-position rejected: bad body", "node_id", id.String(), "error", err.Error())
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid request body"})
	}
	pos, err := model.NewPosition(in.Longitude, in.Latitude, in.Speed)
	if err != nil {
		s.log.Warn("set-position rejected: invalid coordinates",
			"node_id", id.String(),
			"longitude", in.Longitude, "latitude", in.Latitude, "speed", in.Speed,
			"error", err.Error())
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	if err := s.nodes.SetPosition(ctx, id, pos); err != nil {
		s.log.Error("set-position failed", "node_id", id.String(), "error", err.Error())
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
	s.log.Info("position updated",
		"node_id", id.String(),
		"longitude", pos.Longitude(), "latitude", pos.Latitude(), "speed", pos.Speed())
	return c.JSON(http.StatusNoContent, nil)
}

// handleSetChannelStatus updates the online/offline status of a node's
// dynamic channel. Used by the Web UI to simulate cameras going dark.
func (s *Server) handleSetChannelStatus(c echo.Context) error {
	id, err := s.nodeIDParam(c)
	if err != nil {
		s.log.Warn("set-channel-status rejected: bad node id", "error", err.Error())
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	channelID := strings.TrimSpace(c.Param("ch"))
	if channelID == "" {
		s.log.Warn("set-channel-status rejected: missing channel id", "node_id", id.String())
		return c.JSON(http.StatusBadRequest, errorBody{Error: "missing channel id"})
	}
	ctx := c.Request().Context()
	if _, ok := s.nodes.Get(ctx, id); !ok {
		s.log.Warn("set-channel-status on unknown node",
			"node_id", id.String(), "channel_id", channelID)
		return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
	}
	var in ChannelStatusInput
	if err := c.Bind(&in); err != nil {
		s.log.Warn("set-channel-status rejected: bad body",
			"node_id", id.String(), "channel_id", channelID, "error", err.Error())
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid request body"})
	}
	status, err := model.ParseChannelStatus(in.Status)
	if err != nil {
		s.log.Warn("set-channel-status rejected: invalid status",
			"node_id", id.String(), "channel_id", channelID, "status", in.Status)
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	if err := s.nodes.SetChannelStatus(ctx, id, channelID, status); err != nil {
		s.log.Error("set-channel-status failed",
			"node_id", id.String(), "channel_id", channelID, "error", err.Error())
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
	s.log.Info("channel status updated",
		"node_id", id.String(), "channel_id", channelID, "status", status.String())
	return c.JSON(http.StatusNoContent, nil)
}

// nodeIDParam parses the {id} path parameter into a validated NodeID. A
// malformed id is rejected before any lookup so the error can name the
// syntax problem.
func (s *Server) nodeIDParam(c echo.Context) (model.NodeID, error) {
	return model.ParseNodeID(c.Param("id"))
}

// handleInstallFault arms a node with the supplied FaultProfile. A zero
// profile is accepted (it clears active faults); the fault API also exposes
// an explicit DELETE so callers can distinguish "clear" from "set empty".
func (s *Server) handleInstallFault(c echo.Context) error {
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	ctx := c.Request().Context()
	if !s.nodeExists(ctx, id) {
		return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
	}
	var p model.FaultProfile
	if err := c.Bind(&p); err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid request body"})
	}
	if err := s.nodes.InstallFault(ctx, id, p); err != nil {
		s.log.Error("install-fault failed", "node_id", id.String(), "error", err.Error())
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
	state, counters, _ := s.nodes.GetFault(ctx, id)
	return c.JSON(http.StatusOK, faultState{Status: "ok", Profile: state, Counters: counters})
}

// handleGetFault returns the installed profile and action counters for a
// node. A node with no profile yields HTTP 404 so the caller can
// distinguish "not configured" from an empty profile.
func (s *Server) handleGetFault(c echo.Context) error {
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	ctx := c.Request().Context()
	if !s.nodeExists(ctx, id) {
		return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
	}
	profile, counters, ok := s.nodes.GetFault(ctx, id)
	if !ok {
		return c.JSON(http.StatusNotFound, errorBody{Error: "no fault profile installed"})
	}
	return c.JSON(http.StatusOK, faultState{
		Status:   "ok",
		Profile:  profile,
		Counters: counters,
	})
}

// handleClearFault removes the fault profile and resets counters for a node.
// The 204 response is intentionally empty to keep polling UIs cheap.
func (s *Server) handleClearFault(c echo.Context) error {
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	ctx := c.Request().Context()
	if !s.nodeExists(ctx, id) {
		return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
	}
	if err := s.nodes.ClearFault(ctx, id); err != nil {
		s.log.Error("clear-fault failed", "node_id", id.String(), "error", err.Error())
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

// captureEventResponse is the JSON shape of one buffered capture event.
type captureEventResponse struct {
	Direction string    `json:"direction"`
	Local     string    `json:"local"`
	Remote    string    `json:"remote"`
	Transport string    `json:"transport"`
	Bytes     string    `json:"bytes"`
	At        time.Time `json:"at"`
}

// handleQueryCapture returns the node's buffered capture as JSON. The
// optional `limit` query parameter caps the number of returned events
// (oldest first among the returned subset); querying never evicts.
func (s *Server) handleQueryCapture(c echo.Context) error {
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	ctx := c.Request().Context()
	if !s.nodeExists(ctx, id) {
		return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
	}
	limit := 0
	if raw := c.QueryParam("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return c.JSON(http.StatusBadRequest, errorBody{Error: "limit must be a positive integer"})
		}
		limit = parsed
	}
	events, err := s.nodes.QueryCapture(ctx, id, limit)
	if err != nil {
		if errors.Is(err, model.ErrUnknownNode) {
			return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
		}
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
	out := make([]captureEventResponse, 0, len(events))
	for _, e := range events {
		out = append(out, captureEventResponse{
			Direction: string(e.Direction),
			Local:     e.Local,
			Remote:    e.Remote,
			Transport: e.Transport,
			Bytes:     string(e.Bytes),
			At:        e.At,
		})
	}
	return c.JSON(http.StatusOK, out)
}

// handleCapturePCAP streams the node's buffered capture as a pcap document.
// The body is binary with the tcpdump content type; an empty buffer yields a
// valid empty pcap.
func (s *Server) handleCapturePCAP(c echo.Context) error {
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	ctx := c.Request().Context()
	if !s.nodeExists(ctx, id) {
		return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
	}
	data, err := s.nodes.CapturePCAP(ctx, id)
	if err != nil {
		if errors.Is(err, model.ErrUnknownNode) {
			return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
		}
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
	return c.Blob(http.StatusOK, "application/vnd.tcpdump.pcap", data)
}

// handleGetMedia returns the node's media config or 204 if none is set.
func (s *Server) handleGetMedia(c echo.Context) error {
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	cfg, ok, err := s.nodes.GetMedia(c.Request().Context(), id)
	if err != nil {
		if errors.Is(err, model.ErrUnknownNode) {
			return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
		}
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
	if !ok {
		return c.NoContent(http.StatusNoContent)
	}
	return c.JSON(http.StatusOK, cfg)
}

// handlePutMedia validates and stores the supplied config on the node.
func (s *Server) handlePutMedia(c echo.Context) error {
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	var cfg model.MediaConfig
	if err := c.Bind(&cfg); err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid json: " + err.Error()})
	}
	if err := s.nodes.SetMedia(c.Request().Context(), id, cfg); err != nil {
		if errors.Is(err, model.ErrUnknownNode) {
			return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
		}
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	return c.JSON(http.StatusOK, cfg)
}

// handleDeleteMedia clears the node's media config.
func (s *Server) handleDeleteMedia(c echo.Context) error {
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	if err := s.nodes.ClearMedia(c.Request().Context(), id); err != nil {
		if errors.Is(err, model.ErrUnknownNode) {
			return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
		}
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}
