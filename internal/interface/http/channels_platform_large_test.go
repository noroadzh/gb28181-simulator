package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/app"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
	httpapi "github.com/your-org/gb28181-simulator/internal/interface/http"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
)

// platformLargeNodeID is the dedicated node id used by all
// platform-large HTTP transport tests in this file.  It's a 20-digit GB/T
// 20-byte id with the platform-large prefix.
const platformLargeNodeID = "34020000002000000001"

// channelsForLarge builds a server that carries a platform-large node
// ready to receive dynamic-channel HTTP requests.
//
// We use two separate fakes: the standard fakeNodeView (which already
// implements every NodeView method, including AddChannel / RemoveChannel)
// and a small channelTable helper to seed the platform-large node before
// exercising the HTTP API.
func channelsForLarge(t *testing.T) (*httptest.Server, httpapi.ChannelView) {
	t.Helper()
	view := newFakeNodeView()
	view.seed(t, platformLargeNodeID, "127.0.0.1:15060", model.StatusOnline)
	store := newDynamicChannelStore()
	nodeView := autoIDNodeView{view: view, store: store}
	channelsView := autoIDChannelView{view: view, store: store}
	srv := httpapi.NewServer(
		platformconfig.Config{}, logging.NewHub(4), httpapi.Version{Version: "x"},
		nodeView, nil, channelsView, nil, nil,
	)
	ts := httptest.NewServer(srv.Echo())
	t.Cleanup(ts.Close)
	return ts, channelsView
}

// dynamicChannelStore is a side-table used to verify the HTTP layer's
// dynamic-channel CRUD end-to-end.  The platform-large node, unlike a
// device, has no pre-declared channel; we need a place for the HTTP
// handler to keep channels it receives from the API.
type dynamicChannelStore struct {
	mu       sync.Mutex
	channels map[string]map[string]model.Channel // nodeID → channelID → Channel
	seq      map[string]int                      // nodeID → next sequence
}

func newDynamicChannelStore() *dynamicChannelStore {
	return &dynamicChannelStore{
		channels: map[string]map[string]model.Channel{},
		seq:      map[string]int{},
	}
}

func (s *dynamicChannelStore) add(nodeID, channelID, name, parentID string, status model.ChannelStatus) (model.Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.channels[nodeID]; !ok {
		s.channels[nodeID] = map[string]model.Channel{}
	}
	if _, exists := s.channels[nodeID][channelID]; exists {
		return model.Channel{}, errChannelExists
	}
	ch, err := model.NewChannel(channelID, name, parentID, status)
	if err != nil {
		return model.Channel{}, err
	}
	s.channels[nodeID][channelID] = ch
	return ch, nil
}

func (s *dynamicChannelStore) addAutoID(nodeID, name, parentID string, status model.ChannelStatus) (model.Channel, error) {
	s.mu.Lock()
	s.seq[nodeID]++
	seq := s.seq[nodeID]
	s.mu.Unlock()
	id := nodeID + "_" + itoa(seq)
	return s.add(nodeID, id, name, parentID, status)
}

func (s *dynamicChannelStore) remove(nodeID, channelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.channels[nodeID][channelID]; !ok {
		return errChannelNotFound
	}
	delete(s.channels[nodeID], channelID)
	return nil
}

func (s *dynamicChannelStore) list(nodeID string) []model.Channel {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.Channel, 0, len(s.channels[nodeID]))
	for _, ch := range s.channels[nodeID] {
		out = append(out, ch)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}

var (
	errChannelExists   = errString("channel already exists")
	errChannelNotFound = errString("channel not found")
)

type errString string

func (e errString) Error() string { return string(e) }

// itoa avoids pulling strconv into a test helper.  Sequence numbers are
// small enough for the simple loop.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// TestHTTPChannelAdd_PlatformLarge_AutoID verifies spec "通道 ID 生成格式必须
// 符合 20 位纯数字规范" — id 留空 → 自动生成 20 位 GB/T 28181 编码。
//
// Implementation note: the real NodeService.AddChannel requires a non-empty
// id; auto-id generation lives in the HTTP handler (handleChannelAdd). To
// exercise the handler we use a tiny custom NodeView/ChannelView pair that
// passes the auto-generated id straight through.  This is enough to cover
// the HTTP contract.
func TestHTTPChannelAdd_PlatformLarge_AutoID(t *testing.T) {
	view := newFakeNodeView()
	view.seed(t, platformLargeNodeID, "127.0.0.1:15060", model.StatusOnline)
	store := newDynamicChannelStore()

	srv := httpapi.NewServer(platformconfig.Config{}, logging.NewHub(4), httpapi.Version{Version: "x"},
		autoIDNodeView{view: view, store: store},
		nil,
		autoIDChannelView{view: view, store: store},
		nil, nil)
	ts := httptest.NewServer(srv.Echo())
	defer ts.Close()

	url := ts.URL + "/v1/nodes/" + platformLargeNodeID + "/channels"
	_, body := postJSONWithBody(t, url, []byte(`{"name":"lobby-cam","status":"ON"}`))
	var r1 struct {
		ID            string `json:"id"`
		AutoGenerated bool   `json:"auto_generated"`
	}
	if err := json.Unmarshal([]byte(body), &r1); err != nil {
		t.Fatalf("decode first body: %v", err)
	}
	if r1.ID == "" {
		t.Fatalf("first auto-id is empty (body: %s)", body)
	}
	// 必须为 20 位纯数字 GB/T 28181 编码：通过 isValidChannelID 形式校验。
	if !isValidGB28181ID(r1.ID) {
		t.Fatalf("auto-id = %q, want 20-digit numeric", r1.ID)
	}
	// 必须以节点 ID 前 6 位（行政区域码）开头。
	if !hasPrefix(r1.ID, platformLargeNodeID[:6]) {
		t.Fatalf("auto-id = %q, want prefix %q", r1.ID, platformLargeNodeID[:6])
	}
	// 第 7-8 位必须是行业码 00，第 9-10 位必须是类型码 13。
	if r1.ID[6:8] != "00" {
		t.Fatalf("auto-id = %q, want industry code \"00\" at [6:8]", r1.ID)
	}
	if r1.ID[8:10] != "13" {
		t.Fatalf("auto-id = %q, want type code \"13\" at [8:10]", r1.ID)
	}
	// 留空时 auto_generated MUST 为 true。
	if !r1.AutoGenerated {
		t.Fatalf("auto_generated = false, want true for empty id (body: %s)", body)
	}

	_, body2 := postJSONWithBody(t, url, []byte(`{"name":"entrance-cam","status":"ON"}`))
	var r2 struct {
		ID            string `json:"id"`
		AutoGenerated bool   `json:"auto_generated"`
	}
	if err := json.Unmarshal([]byte(body2), &r2); err != nil {
		t.Fatalf("decode second body: %v", err)
	}
	if r2.ID == r1.ID {
		t.Fatalf("second auto-id = %q, must differ from first", r2.ID)
	}
	if !isValidGB28181ID(r2.ID) {
		t.Fatalf("auto-id = %q, want 20-digit numeric", r2.ID)
	}
	if !hasPrefix(r2.ID, platformLargeNodeID[:6]) {
		t.Fatalf("auto-id = %q, want prefix %q", r2.ID, platformLargeNodeID[:6])
	}
	if !r2.AutoGenerated {
		t.Fatalf("auto_generated = false, want true for empty id (body: %s)", body2)
	}
}

// isValidGB28181ID 校验字符串是否恰好 20 个 ASCII 数字字符（用于测试断言）。
func isValidGB28181ID(s string) bool {
	if len(s) != 20 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// --- auto-id views ----------------------------------------------------------
//
// These two adapters wrap fakeNodeView and a dynamicChannelStore to mimic
// what the production code does in handleChannelAdd (auto-id) and
// handleChannelDelete.  This keeps the test self-contained without wiring
// up the real *app.NodeService.

type autoIDNodeView struct {
	view  *fakeNodeView
	store *dynamicChannelStore
}

func (a autoIDNodeView) List(ctx context.Context) []model.Node { return a.view.List(ctx) }
func (a autoIDNodeView) Get(ctx context.Context, id model.NodeID) (model.Node, bool) {
	return a.view.Get(ctx, id)
}
func (a autoIDNodeView) Start(ctx context.Context, id model.NodeID) error {
	return a.view.Start(ctx, id)
}
func (a autoIDNodeView) Stop(ctx context.Context, id model.NodeID) error { return a.view.Stop(ctx, id) }
func (a autoIDNodeView) Unregister(ctx context.Context, id model.NodeID) error {
	return a.view.Unregister(ctx, id)
}
func (a autoIDNodeView) Devices(ctx context.Context, id model.NodeID) ([]model.DownstreamDevice, error) {
	return a.view.Devices(ctx, id)
}
func (a autoIDNodeView) Device(ctx context.Context, id model.NodeID, deviceID string) (model.DownstreamDevice, error) {
	return a.view.Device(ctx, id, deviceID)
}
func (a autoIDNodeView) TriggerAlarm(ctx context.Context, id model.NodeID, in app.AlarmInput) (model.AlarmSnapshot, error) {
	return a.view.TriggerAlarm(ctx, id, in)
}
func (a autoIDNodeView) SetPosition(ctx context.Context, id model.NodeID, pos model.Position) error {
	return a.view.SetPosition(ctx, id, pos)
}
func (a autoIDNodeView) SetChannelStatus(ctx context.Context, id model.NodeID, channelID string, status model.ChannelStatus) error {
	return a.view.SetChannelStatus(ctx, id, channelID, status)
}
func (a autoIDNodeView) AddChannel(ctx context.Context, id model.NodeID, channelID, name, parentID string, status model.ChannelStatus) (model.Channel, error) {
	return a.store.add(id.String(), channelID, name, parentID, status)
}
func (a autoIDNodeView) RemoveChannel(ctx context.Context, id model.NodeID, channelID string) error {
	return a.store.remove(id.String(), channelID)
}
func (a autoIDNodeView) CreateNode(ctx context.Context, sn model.StoredNode) (model.Node, error) {
	return a.view.CreateNode(ctx, sn)
}
func (a autoIDNodeView) UpdateNode(ctx context.Context, sn model.StoredNode) error {
	return a.view.UpdateNode(ctx, sn)
}
func (a autoIDNodeView) DeleteNode(ctx context.Context, id model.NodeID) error {
	return a.view.DeleteNode(ctx, id)
}
func (a autoIDNodeView) UpdateCascade(ctx context.Context, id model.NodeID, parent string, children []string) error {
	return a.view.UpdateCascade(ctx, id, parent, children)
}
func (a autoIDNodeView) GetStoredNode(ctx context.Context, id model.NodeID) (model.StoredNode, bool) {
	return a.view.GetStoredNode(ctx, id)
}
func (a autoIDNodeView) InstallFault(ctx context.Context, id model.NodeID, p model.FaultProfile) error {
	return a.view.InstallFault(ctx, id, p)
}
func (a autoIDNodeView) GetFault(ctx context.Context, id model.NodeID) (model.FaultProfile, map[model.FaultAction]uint64, bool) {
	return a.view.GetFault(ctx, id)
}
func (a autoIDNodeView) ClearFault(ctx context.Context, id model.NodeID) error {
	return a.view.ClearFault(ctx, id)
}
func (a autoIDNodeView) QueryCapture(ctx context.Context, nodeID model.NodeID, limit int) ([]port.CaptureEvent, error) {
	return a.view.QueryCapture(ctx, nodeID, limit)
}
func (a autoIDNodeView) CapturePCAP(ctx context.Context, nodeID model.NodeID) ([]byte, error) {
	return a.view.CapturePCAP(ctx, nodeID)
}
func (a autoIDNodeView) GetMedia(ctx context.Context, id model.NodeID) (model.MediaConfig, bool, error) {
	return a.view.GetMedia(ctx, id)
}
func (a autoIDNodeView) SetMedia(ctx context.Context, id model.NodeID, cfg model.MediaConfig) error {
	return a.view.SetMedia(ctx, id, cfg)
}
func (a autoIDNodeView) ClearMedia(ctx context.Context, id model.NodeID) error {
	return a.view.ClearMedia(ctx, id)
}

// autoIDChannelView delegates to the same store and the existing fake.
type autoIDChannelView struct {
	view  *fakeNodeView
	store *dynamicChannelStore
}

func (a autoIDChannelView) ListChannels(ctx context.Context, id model.NodeID) []model.Channel {
	return a.store.list(id.String())
}
func (a autoIDChannelView) Channel(_ context.Context, id model.NodeID, channelID string) (model.Channel, bool) {
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	if m, ok := a.store.channels[id.String()]; ok {
		ch, ok := m[channelID]
		return ch, ok
	}
	return model.Channel{}, false
}
func (a autoIDChannelView) GetChannelMedia(_ context.Context, id model.NodeID, channelID string) (model.MediaConfig, bool, error) {
	n, ok := a.view.Get(context.Background(), id)
	if !ok {
		return model.MediaConfig{}, false, nil
	}
	cfg, ok := n.Profile().ChannelMediaConfig(channelID)
	return cfg, ok, nil
}
func (a autoIDChannelView) SetChannelMedia(_ context.Context, _ model.NodeID, _ string, _ model.MediaConfig) error {
	return nil
}
func (a autoIDChannelView) ClearChannelMedia(_ context.Context, _ model.NodeID, _ string) error {
	return nil
}
func (a autoIDChannelView) PTZ(_ context.Context, _ model.NodeID, _ string, _ string, _, _ int) error {
	return nil
}

func (a autoIDChannelView) Snapshot(_ context.Context, _ model.NodeID, _ string) ([]byte, string, error) {
	return nil, "", nil
}

func (a autoIDChannelView) Records(_ context.Context, _ model.NodeID, _ string, _, _ time.Time) ([]model.RecordInfoItem, error) {
	return nil, nil
}

func (a autoIDChannelView) Playback(_ context.Context, _ model.NodeID, _, _ string, _ int) error {
	return nil
}

func (a autoIDChannelView) TalkStart(_ context.Context, _ model.NodeID, _ string) (string, string, error) {
	return "", "", fmt.Errorf("unsupported")
}

func (a autoIDChannelView) TalkStop(_ context.Context, _ model.NodeID, _, _ string) error {
	return nil
}

// TestHTTPChannelAdd_PlatformLarge_ExplicitID covers 4.1 scenario "id 显式
// 给出 → 201".  Uses the app-layer test for behavior; here we only assert
// the HTTP path returns 201.
func TestHTTPChannelAdd_PlatformLarge_ExplicitID(t *testing.T) {
	ts, _ := channelsForLarge(t)
	url := ts.URL + "/v1/nodes/" + platformLargeNodeID + "/channels"
	status, body := postJSONWithBody(t, url, []byte(`{"id":"34020000002000000001","name":"entrance-main","status":"ON"}`))
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", status, body)
	}
}

// TestHTTPChannelAdd_PlatformLarge_DuplicateID covers 4.1 scenario "重复
// id → 4xx".  The app layer returns an error which the HTTP handler maps
// to 400 (per existing device behavior).
func TestHTTPChannelAdd_PlatformLarge_DuplicateID(t *testing.T) {
	ts, _ := channelsForLarge(t)
	url := ts.URL + "/v1/nodes/" + platformLargeNodeID + "/channels"
	body := []byte(`{"id":"34020000002000000002","name":"entrance-dup","status":"ON"}`)
	status, _ := postJSONWithBody(t, url, body)
	if status != http.StatusCreated {
		t.Fatalf("first POST status = %d, want 201", status)
	}
	status2, body2 := postJSONWithBody(t, url, body)
	if status2 < 400 {
		t.Fatalf("duplicate POST status = %d, want 4xx (body: %s)", status2, body2)
	}
}

// TestHTTPChannelAdd_20DigitID_OK covers spec task 4.1 scenario
// "合法 20 位数字 ID → 201".
func TestHTTPChannelAdd_20DigitID_OK(t *testing.T) {
	ts, _ := channelsForLarge(t)
	url := ts.URL + "/v1/nodes/" + platformLargeNodeID + "/channels"
	status, body := postJSONWithBody(t, url, []byte(`{"id":"34020000002000000003","name":"gate-cam","status":"ON"}`))
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", status, body)
	}
}

// TestHTTPChannelAdd_NonNumericID_BadRequest covers spec task 4.1 scenario
// "非数字 ID → 400".
func TestHTTPChannelAdd_NonNumericID_BadRequest(t *testing.T) {
	ts, _ := channelsForLarge(t)
	url := ts.URL + "/v1/nodes/" + platformLargeNodeID + "/channels"
	status, body := postJSONWithBody(t, url, []byte(`{"id":"entrance-cam","name":"gate-cam","status":"ON"}`))
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", status, body)
	}
}

// TestHTTPChannelAdd_19DigitID_BadRequest covers spec task 4.1 scenario
// "19 位 ID → 400".
func TestHTTPChannelAdd_19DigitID_BadRequest(t *testing.T) {
	ts, _ := channelsForLarge(t)
	url := ts.URL + "/v1/nodes/" + platformLargeNodeID + "/channels"
	status, body := postJSONWithBody(t, url, []byte(`{"id":"3402000000200000001","name":"short-cam","status":"ON"}`))
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", status, body)
	}
}

// TestHTTPChannelRemove_PlatformLarge covers 4.1 scenario "DELETE 成功 → 204".
func TestHTTPChannelRemove_PlatformLarge(t *testing.T) {
	view := newFakeNodeView()
	view.seed(t, platformLargeNodeID, "127.0.0.1:15060", model.StatusOnline)
	store := newDynamicChannelStore()
	// seed channel via the store directly
	if _, err := store.add(platformLargeNodeID, "to-remove", "to-remove", "", model.ChannelStatusOnline); err != nil {
		t.Fatalf("seed: %v", err)
	}
	nodeView := autoIDNodeView{view: view, store: store}
	channelsView := autoIDChannelView{view: view, store: store}
	srv := httpapi.NewServer(platformconfig.Config{}, logging.NewHub(4), httpapi.Version{Version: "x"},
		nodeView, nil, channelsView, nil, nil)
	ts := httptest.NewServer(srv.Echo())
	defer ts.Close()

	req := httptest.NewRequest(http.MethodDelete, ts.URL+"/v1/nodes/"+platformLargeNodeID+"/channels/to-remove", nil)
	// httptest.NewRequest populates RequestURI; net/http forbids setting it on
	// client requests, so clear it before handing the request to Client.Do.
	req.RequestURI = ""
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", resp.StatusCode)
	}
}

// TestHTTPChannelDetail_MediaFallback_NodeLevelOnly covers 2.1 scenario
// "通道级无媒体源 → 读取节点级 → has_media=true, media_kind 与节点级一致".
//
// The autoIDChannelView.GetChannelMedia reads from the node profile's
// ChannelMediaConfig(channelID); returning (zero, false) when the channel has
// no per-channel entry.  The handler must fall back to s.nodes.GetMedia and surface
// the node-level config.
func TestHTTPChannelDetail_MediaFallback_NodeLevelOnly(t *testing.T) {
	view := newFakeNodeView()
	node := view.seed(t, platformLargeNodeID, "127.0.0.1:15060", model.StatusOnline)
	// Set node-level media only; channel-level is deliberately left empty.
	profile := node.Profile()
	profile.SetMediaConfig(model.MediaConfig{Kind: model.SourceKindSynthetic, FPS: 25})
	view.put(node.WithProfile(profile))
	store := newDynamicChannelStore()
	store.add(platformLargeNodeID, "34020000130000000001", "prod-cam", "", model.ChannelStatusOnline)
	nodeView := autoIDNodeView{view: view, store: store}
	channelsView := autoIDChannelView{view: view, store: store}
	srv := httpapi.NewServer(platformconfig.Config{}, logging.NewHub(4), httpapi.Version{Version: "x"},
		nodeView, nil, channelsView, nil, nil)
	ts := httptest.NewServer(srv.Echo())
	defer ts.Close()

	url := ts.URL + "/v1/nodes/" + platformLargeNodeID + "/channels/34020000130000000001"
	status, body := getJSON(t, url)
	if status != http.StatusOK {
		t.Fatalf("GET status = %d, want 200 (body: %s)", status, body)
	}
	var ch struct {
		HasMedia  bool   `json:"has_media"`
		MediaKind string `json:"media_kind"`
	}
	if err := json.Unmarshal([]byte(body), &ch); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !ch.HasMedia {
		t.Fatalf("has_media = false, want true (body: %s)", body)
	}
	if ch.MediaKind != string(model.SourceKindSynthetic) {
		t.Fatalf("media_kind = %q, want %q (body: %s)", ch.MediaKind, model.SourceKindSynthetic, body)
	}
}

// TestHTTPChannelList_MediaFallback_NodeLevelOnly covers 2.2 scenario
// "列表端点同等条件下每个通道项 has_media=true, media_kind 与节点级一致".
func TestHTTPChannelList_MediaFallback_NodeLevelOnly(t *testing.T) {
	view := newFakeNodeView()
	node := view.seed(t, platformLargeNodeID, "127.0.0.1:15060", model.StatusOnline)
	profile := node.Profile()
	profile.SetMediaConfig(model.MediaConfig{Kind: model.SourceKindSynthetic, FPS: 25})
	view.put(node.WithProfile(profile))
	store := newDynamicChannelStore()
	for _, id := range []string{"34020000130000000001", "34020000130000000002"} {
		store.add(platformLargeNodeID, id, id, "", model.ChannelStatusOnline)
	}
	nodeView := autoIDNodeView{view: view, store: store}
	channelsView := autoIDChannelView{view: view, store: store}
	srv := httpapi.NewServer(platformconfig.Config{}, logging.NewHub(4), httpapi.Version{Version: "x"},
		nodeView, nil, channelsView, nil, nil)
	ts := httptest.NewServer(srv.Echo())
	defer ts.Close()

	url := ts.URL + "/v1/nodes/" + platformLargeNodeID + "/channels"
	status, body := getJSON(t, url)
	if status != http.StatusOK {
		t.Fatalf("GET status = %d, want 200 (body: %s)", status, body)
	}
	var channels []struct {
		HasMedia  bool   `json:"has_media"`
		MediaKind string `json:"media_kind"`
	}
	if err := json.Unmarshal([]byte(body), &channels); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(channels) != 2 {
		t.Fatalf("channels = %d, want 2 (body: %s)", len(channels), body)
	}
	for i, ch := range channels {
		if !ch.HasMedia {
			t.Fatalf("channels[%d].has_media = false, want true (body: %s)", i, body)
		}
		if ch.MediaKind != string(model.SourceKindSynthetic) {
			t.Fatalf("channels[%d].media_kind = %q, want %q (body: %s)",
				i, ch.MediaKind, model.SourceKindSynthetic, body)
		}
	}
}

// TestHTTPChannelDetail_MediaPriority_ChannelLevelWins covers 2.3 scenario
// "通道级已配置 → 优先返回通道级, 节点级不覆盖".
func TestHTTPChannelDetail_MediaPriority_ChannelLevelWins(t *testing.T) {
	view := newFakeNodeView()
	node := view.seed(t, platformLargeNodeID, "127.0.0.1:15060", model.StatusOnline)
	// Node-level synthetic.
	profile := node.Profile()
	profile.SetMediaConfig(model.MediaConfig{Kind: model.SourceKindSynthetic, FPS: 25})
	// Channel-level file (takes priority).
	profile.SetChannelMediaConfig("34020000130000000001",
		model.MediaConfig{Kind: model.SourceKindFile, Path: "/data/v.mp4"})
	view.put(node.WithProfile(profile))
	store := newDynamicChannelStore()
	store.add(platformLargeNodeID, "34020000130000000001", "prod-cam", "", model.ChannelStatusOnline)
	nodeView := autoIDNodeView{view: view, store: store}
	channelsView := autoIDChannelView{view: view, store: store}
	srv := httpapi.NewServer(platformconfig.Config{}, logging.NewHub(4), httpapi.Version{Version: "x"},
		nodeView, nil, channelsView, nil, nil)
	ts := httptest.NewServer(srv.Echo())
	defer ts.Close()

	url := ts.URL + "/v1/nodes/" + platformLargeNodeID + "/channels/34020000130000000001"
	status, body := getJSON(t, url)
	if status != http.StatusOK {
		t.Fatalf("GET status = %d, want 200 (body: %s)", status, body)
	}
	var ch struct {
		HasMedia  bool   `json:"has_media"`
		MediaKind string `json:"media_kind"`
	}
	if err := json.Unmarshal([]byte(body), &ch); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !ch.HasMedia {
		t.Fatalf("has_media = false, want true (body: %s)", body)
	}
	if ch.MediaKind != string(model.SourceKindFile) {
		t.Fatalf("media_kind = %q, want %q (body: %s)",
			ch.MediaKind, model.SourceKindFile, body)
	}
}

// TestHTTPChannelDetail_MediaNeitherLevel covers 2.4 scenario
// "两级均未配置 → has_media=false, media_kind 省略".
func TestHTTPChannelDetail_MediaNeitherLevel(t *testing.T) {
	view := newFakeNodeView()
	view.seed(t, platformLargeNodeID, "127.0.0.1:15060", model.StatusOnline)
	store := newDynamicChannelStore()
	store.add(platformLargeNodeID, "34020000130000000001", "prod-cam", "", model.ChannelStatusOnline)
	nodeView := autoIDNodeView{view: view, store: store}
	channelsView := autoIDChannelView{view: view, store: store}
	srv := httpapi.NewServer(platformconfig.Config{}, logging.NewHub(4), httpapi.Version{Version: "x"},
		nodeView, nil, channelsView, nil, nil)
	ts := httptest.NewServer(srv.Echo())
	defer ts.Close()

	url := ts.URL + "/v1/nodes/" + platformLargeNodeID + "/channels/34020000130000000001"
	status, body := getJSON(t, url)
	if status != http.StatusOK {
		t.Fatalf("GET status = %d, want 200 (body: %s)", status, body)
	}
	var ch struct {
		HasMedia  bool   `json:"has_media"`
		MediaKind string `json:"media_kind"`
	}
	if err := json.Unmarshal([]byte(body), &ch); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ch.HasMedia {
		t.Fatalf("has_media = true, want false (body: %s)", body)
	}
	if ch.MediaKind != "" {
		t.Fatalf("media_kind = %q, want empty (body: %s)", ch.MediaKind, body)
	}
}

// TestHTTPChannelList_PlatformLarge covers the list endpoint returning 200
func TestHTTPChannelList_PlatformLarge(t *testing.T) {
	view := newFakeNodeView()
	view.seed(t, platformLargeNodeID, "127.0.0.1:15060", model.StatusOnline)
	store := newDynamicChannelStore()
	// seed channels via the store directly (avoids needing AddChannel on fakeNodeView)
	for _, id := range []string{"ch-1", "ch-2"} {
		if _, err := store.add(platformLargeNodeID, id, id, "", model.ChannelStatusOnline); err != nil {
			t.Fatalf("seed channel %s: %v", id, err)
		}
	}
	nodeView := autoIDNodeView{view: view, store: store}
	channelsView := autoIDChannelView{view: view, store: store}
	srv := httpapi.NewServer(platformconfig.Config{}, logging.NewHub(4), httpapi.Version{Version: "x"},
		nodeView, nil, channelsView, nil, nil)
	ts := httptest.NewServer(srv.Echo())
	defer ts.Close()

	url := ts.URL + "/v1/nodes/" + platformLargeNodeID + "/channels"
	status, body := getJSON(t, url)
	if status != http.StatusOK {
		t.Fatalf("GET status = %d, want 200 (body: %s)", status, body)
	}
	var channels []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(body), &channels); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(channels) != 2 {
		t.Fatalf("channels = %d, want 2 (body: %s)", len(channels), body)
	}
}

// mustParseNodeID wraps model.ParseNodeID for test brevity.
func mustParseNodeID(t *testing.T, raw string) model.NodeID {
	t.Helper()
	id, err := model.ParseNodeID(raw)
	if err != nil {
		t.Fatalf("ParseNodeID(%q): %v", raw, err)
	}
	return id
}

func (a autoIDChannelView) SubscribeTalkAudio(_ context.Context, _ model.NodeID, _, _ string) (<-chan []byte, func(), error) {
	return nil, nil, fmt.Errorf("talk audio unsupported")
}

func (a autoIDChannelView) SendTalkAudio(_ context.Context, _ model.NodeID, _, _ string, _ []byte) error {
	return fmt.Errorf("talk audio unsupported")
}

func (a autoIDChannelView) StartPlaybackStream(_ context.Context, _ model.NodeID, _, _ string, _, _ time.Time) error {
	return fmt.Errorf("playback streaming unsupported")
}

func (a autoIDChannelView) PlaybackWindow(_ string) (time.Time, time.Time, bool) {
	return time.Time{}, time.Time{}, false
}

func (a autoIDChannelView) StartLive(_ context.Context, _ model.NodeID, _, _ string) (port.LiveSessionInfo, error) {
	return port.LiveSessionInfo{}, fmt.Errorf("live pull unsupported")
}

func (a autoIDChannelView) StopLive(_ context.Context, _ model.NodeID, _ string) error {
	return nil
}

func (a autoIDChannelView) Recordings(_ context.Context, _ model.NodeID, _ string, _, _ time.Time) ([]port.Recording, error) {
	return nil, fmt.Errorf("recording unsupported")
}
