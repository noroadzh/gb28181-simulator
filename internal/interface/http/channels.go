// Package httpapi — 通道级 HTTP API（多通道前端可视化）。
//
// 本文件为本期（T4）新增，提供如下端点：
//
//	GET    /v1/nodes/:id/channels                       通道列表
//	GET    /v1/nodes/:id/channels/:ch                   通道详情
//	GET    /v1/nodes/:id/channels/:ch/media             通道级媒体源
//	PUT    /v1/nodes/:id/channels/:ch/media             设置/替换通道级媒体源
//	DELETE /v1/nodes/:id/channels/:ch/media             清除通道级媒体源
//	POST   /v1/nodes/:id/channels/:ch/ptz               PTZ 云台控制
//	GET    /v1/nodes/:id/channels/:ch/records           录像查询
//	POST   /v1/nodes/:id/channels/:ch/playback         录像回放控制
//	POST   /v1/nodes/:id/channels/:ch/talk/start        开始对讲
//	POST   /v1/nodes/:id/channels/:ch/talk/stop         停止对讲
//	GET    /v1/nodes/:id/channels/:ch/snapshot          抓取快照
//
// 所有错误统一用 errorBody 包装；HTTP 状态码遵循既有约定（200/204/400/404/409/501）。
package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// ChannelView is the slice of channel operations the HTTP layer needs.
//
// 与 NodeView 一样，使用窄接口而非 *app.NodeService，以保持可测试性、避免
// 拉入 app 层类型。当为 nil 时所有 channel 端点统一返回 501。
type ChannelView interface {
	// ListChannels returns the channels declared in the node's profile.
	// 一个空切片表示“没有任何 channel”。
	ListChannels(ctx context.Context, id model.NodeID) []model.Channel

	// Channel returns one channel by id, or (zero, false) when not found.
	Channel(ctx context.Context, id model.NodeID, channelID string) (model.Channel, bool)

	// GetChannelMedia returns the per-channel MediaConfig and whether one is
	// set. (zero, false) 表示“该 channel 没有显式配置”，HTTP 层应回退到
	// 节点级 MediaConfig。
	GetChannelMedia(ctx context.Context, id model.NodeID, channelID string) (model.MediaConfig, bool, error)
	// SetChannelMedia validates and stores the per-channel MediaConfig.
	SetChannelMedia(ctx context.Context, id model.NodeID, channelID string, cfg model.MediaConfig) error
	// ClearChannelMedia removes the per-channel MediaConfig.
	ClearChannelMedia(ctx context.Context, id model.NodeID, channelID string) error

	// PTZ sends a PTZ command for the channel. direction ∈
	// {"up","down","left","right","upleft","upright","downleft","downright",
	// "in","out"}；speed ∈ [0,255]；durationMs ≥ 0。
	PTZ(ctx context.Context, id model.NodeID, channelID, direction string, speed, durationMs int) error

	// Records returns the recording list for the channel in [start, end].
	// start/end 为零值时使用节点/设备级默认值。
	Records(ctx context.Context, id model.NodeID, channelID string, start, end time.Time) ([]model.RecordInfoItem, error)

	// Playback issues a PLAY/PAUSE/TEARDOWN command for the channel.
	// action ∈ {"play","pause","teardown"}；scale ∈ {-4,-2,1,2,4}。
	Playback(ctx context.Context, id model.NodeID, channelID, action string, scale int) error

	// TalkStart opens a voice-talk session for the channel and returns the
	// session id + WSS URL the browser should connect to.
	TalkStart(ctx context.Context, id model.NodeID, channelID string) (string, string, error)
	// TalkStop closes the talk session.
	TalkStop(ctx context.Context, id model.NodeID, channelID, sessionID string) error

	// Snapshot triggers a SnapShot NOTIFY; returns the snapshot bytes
	// (PNG/JPEG), the media type, and an error. The first return is empty
	// when the platform does not have a snapshot subsystem and the request
	// should be queued (HTTP 202).
	Snapshot(ctx context.Context, id model.NodeID, channelID string) ([]byte, string, error)
}

// channelResponse is the JSON shape for GET /channels/:ch.
type channelResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Online    bool   `json:"online"`
	Status    string `json:"status"`
	ParentID  string `json:"parent_id,omitempty"`
	HasMedia  bool   `json:"has_media"`
	MediaKind string `json:"media_kind,omitempty"`
}

func newChannelResponse(ch model.Channel) channelResponse {
	r := channelResponse{
		ID:       ch.ID(),
		Name:     ch.Name(),
		Online:   ch.Status() == model.ChannelStatusOnline,
		Status:   ch.Status().String(),
		ParentID: ch.ParentID(),
	}
	return r
}

func newChannelResponseWithMedia(ch model.Channel, cfg model.MediaConfig, has bool) channelResponse {
	r := newChannelResponse(ch)
	r.HasMedia = has
	if has {
		r.MediaKind = string(cfg.Kind)
	}
	return r
}

// handleChannelList returns the channel list for a node.
func (s *Server) handleChannelList(c echo.Context) error {
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
	chs := s.channels.ListChannels(c.Request().Context(), id)
	out := make([]channelResponse, 0, len(chs))
	for _, ch := range chs {
		cfg, has, _ := s.channels.GetChannelMedia(c.Request().Context(), id, ch.ID())
		out = append(out, newChannelResponseWithMedia(ch, cfg, has))
	}
	return c.JSON(http.StatusOK, out)
}

// channelAddRequest is the JSON shape for POST /v1/nodes/:id/channels.
type channelAddRequest struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parent_id,omitempty"`
	Status   string `json:"status,omitempty"` // ON / OFF，缺省 ON
}

// handleChannelAdd creates a new dynamic channel on a device node.
// Path: POST /v1/nodes/:id/channels
func (s *Server) handleChannelAdd(c echo.Context) error {
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
	var req channelAddRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid request body"})
	}
	req.ID = strings.TrimSpace(req.ID)
	req.Name = strings.TrimSpace(req.Name)
	req.ParentID = strings.TrimSpace(req.ParentID)
	if req.ID == "" || req.Name == "" {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "id and name are required"})
	}
	status := model.ChannelStatusOnline
	if req.Status != "" && !strings.EqualFold(req.Status, "ON") {
		status = model.ChannelStatusOffline
	}
	ch, err := s.nodes.AddChannel(c.Request().Context(), id, req.ID, req.Name, req.ParentID, status)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	return c.JSON(http.StatusCreated, newChannelResponse(ch))
}

// handleChannelRemove deletes a dynamic channel from a device node.
// Path: DELETE /v1/nodes/:id/channels/:ch
func (s *Server) handleChannelRemove(c echo.Context) error {
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
	channelID := strings.TrimSpace(c.Param("ch"))
	if channelID == "" {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "channel id is required"})
	}
	if err := s.nodes.RemoveChannel(c.Request().Context(), id, channelID); err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

// handleChannelDetail returns one channel.
func (s *Server) handleChannelDetail(c echo.Context) error {
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
	ch, ok := s.channels.Channel(c.Request().Context(), id, channelID)
	if !ok {
		return c.JSON(http.StatusNotFound, errorBody{Error: "channel " + channelID + " not found"})
	}
	cfg, has, _ := s.channels.GetChannelMedia(c.Request().Context(), id, channelID)
	return c.JSON(http.StatusOK, newChannelResponseWithMedia(ch, cfg, has))
}

// mediaResponse is the JSON shape for GET /channels/:ch/media.
type mediaResponse struct {
	ChannelID string            `json:"channel_id"`
	Has       bool              `json:"has"`
	Cfg       model.MediaConfig `json:"config,omitempty"`
}

func (s *Server) handleGetChannelMedia(c echo.Context) error {
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
	cfg, has, err := s.channels.GetChannelMedia(c.Request().Context(), id, channelID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
	if has {
		cfg = cfg.Normalize()
	}
	return c.JSON(http.StatusOK, mediaResponse{ChannelID: channelID, Has: has, Cfg: cfg})
}

// handlePutChannelMedia sets the per-channel MediaConfig.
func (s *Server) handlePutChannelMedia(c echo.Context) error {
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
	var in model.MediaConfig
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid JSON: " + err.Error()})
	}
	if err := s.channels.SetChannelMedia(c.Request().Context(), id, channelID, in); err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	cfg, has, _ := s.channels.GetChannelMedia(c.Request().Context(), id, channelID)
	if has {
		cfg = cfg.Normalize()
	}
	return c.JSON(http.StatusOK, mediaResponse{ChannelID: channelID, Has: has, Cfg: cfg})
}

// handleDeleteChannelMedia removes the per-channel MediaConfig.
func (s *Server) handleDeleteChannelMedia(c echo.Context) error {
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
	if err := s.channels.ClearChannelMedia(c.Request().Context(), id, channelID); err != nil {
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

// ptzInput is the body for POST /channels/:ch/ptz.
type ptzInput struct {
	Direction  string `json:"direction"`
	Speed      int    `json:"speed"`
	DurationMs int    `json:"duration_ms"`
}

func (s *Server) handleChannelPTZ(c echo.Context) error {
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
	var in ptzInput
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid JSON: " + err.Error()})
	}
	if !isValidPTZDirection(in.Direction) {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid direction: " + in.Direction})
	}
	if in.Speed < 0 || in.Speed > 255 {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "speed must be in [0,255]"})
	}
	if in.DurationMs < 0 {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "duration_ms must be >= 0"})
	}
	if err := s.channels.PTZ(c.Request().Context(), id, channelID, in.Direction, in.Speed, in.DurationMs); err != nil {
		if errors.Is(err, port.ErrTalkUnsupported) || errors.Is(err, port.ErrPlaybackUnsupported) {
			return c.JSON(http.StatusNotImplemented, errorBody{Error: err.Error()})
		}
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{
		"channel_id": channelID,
		"direction":  in.Direction,
		"speed":      in.Speed,
		"duration":   in.DurationMs,
	})
}

func isValidPTZDirection(d string) bool {
	switch d {
	case "up", "down", "left", "right",
		"upleft", "upright", "downleft", "downright",
		"in", "out", "stop":
		return true
	}
	return false
}

// recordResponse is the JSON shape for one entry in GET /channels/:ch/records.
type recordResponse struct {
	DeviceID   string `json:"device_id"`
	ChannelID  string `json:"channel_id"`
	Name       string `json:"name"`
	StartTime  string `json:"start_time"`
	EndTime    string `json:"end_time"`
	FilePath   string `json:"file_path"`
	VideoCodec string `json:"video_codec,omitempty"`
	AudioCodec string `json:"audio_codec,omitempty"`
}

// handleChannelRecords returns the recording list for the channel.
func (s *Server) handleChannelRecords(c echo.Context) error {
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
	var start, end time.Time
	if v := c.QueryParam("start"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid start: " + err.Error()})
		}
		start = t
	}
	if v := c.QueryParam("end"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid end: " + err.Error()})
		}
		end = t
	}
	recs, err := s.channels.Records(c.Request().Context(), id, channelID, start, end)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
	out := make([]recordResponse, 0, len(recs))
	for _, r := range recs {
		out = append(out, recordResponse{
			DeviceID:   r.DeviceID(),
			ChannelID:  r.ChannelID(),
			Name:       r.Name(),
			StartTime:  r.StartTime(),
			EndTime:    r.EndTime(),
			FilePath:   r.FilePath(),
			VideoCodec: r.VideoCodec(),
			AudioCodec: r.AudioCodec(),
		})
	}
	return c.JSON(http.StatusOK, out)
}

// playbackInput is the body for POST /channels/:ch/playback.
type playbackInput struct {
	Action string `json:"action"`
	Scale  int    `json:"scale"`
}

func (s *Server) handleChannelPlayback(c echo.Context) error {
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
	var in playbackInput
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid JSON: " + err.Error()})
	}
	if !isValidPlaybackAction(in.Action) {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid action: " + in.Action})
	}
	if err := s.channels.Playback(c.Request().Context(), id, channelID, in.Action, in.Scale); err != nil {
		if errors.Is(err, port.ErrPlaybackUnsupported) {
			return c.JSON(http.StatusNotImplemented, errorBody{Error: err.Error()})
		}
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{
		"channel_id": channelID,
		"action":     in.Action,
		"scale":      in.Scale,
	})
}

func isValidPlaybackAction(a string) bool {
	switch a {
	case "play", "pause", "teardown":
		return true
	}
	return false
}

// talkStartResponse is the body for POST /channels/:ch/talk/start.
type talkStartResponse struct {
	ChannelID string `json:"channel_id"`
	SessionID string `json:"session_id"`
	WSURL     string `json:"ws_url"`
}

func (s *Server) handleTalkStart(c echo.Context) error {
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
	sessID, wsURL, err := s.channels.TalkStart(c.Request().Context(), id, channelID)
	if err != nil {
		if errors.Is(err, port.ErrTalkUnsupported) {
			return c.JSON(http.StatusNotImplemented, errorBody{Error: err.Error()})
		}
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
	return c.JSON(http.StatusOK, talkStartResponse{
		ChannelID: channelID,
		SessionID: sessID,
		WSURL:     wsURL,
	})
}

type talkStopInput struct {
	SessionID string `json:"session_id"`
}

func (s *Server) handleTalkStop(c echo.Context) error {
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
	var in talkStopInput
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid JSON: " + err.Error()})
	}
	if in.SessionID == "" {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "session_id is required"})
	}
	if err := s.channels.TalkStop(c.Request().Context(), id, channelID, in.SessionID); err != nil {
		if errors.Is(err, port.ErrTalkUnsupported) {
			return c.JSON(http.StatusNotImplemented, errorBody{Error: err.Error()})
		}
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

// handleChannelSnapshot triggers a snapshot. Returns the snapshot URL.
func (s *Server) handleChannelSnapshot(c echo.Context) error {
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
	img, mime, err := s.channels.Snapshot(c.Request().Context(), id, channelID)
	if err != nil {
		var unsupported port.ErrSnapshotUnsupported
		if errors.As(err, &unsupported) {
			return c.JSON(http.StatusNotImplemented, errorBody{Error: err.Error()})
		}
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
	if len(img) == 0 {
		return c.JSON(http.StatusAccepted, map[string]any{
			"channel_id": channelID,
			"status":     "queued",
		})
	}
	if mime == "" {
		mime = "image/jpeg"
	}
	return c.Blob(http.StatusOK, mime, img)
}
