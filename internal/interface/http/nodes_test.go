package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// fakeNodeView is a minimal NodeView that enforces the real transition
// table, so the HTTP layer's status-code mapping is exercised against the
// same rules the domain uses.
type fakeNodeView struct {
	mu      sync.Mutex
	nodes   map[string]model.Node
	devices map[string]map[string]model.DownstreamDevice
	faults  map[string]*fakeFaultEntry
	events  map[string][]port.CaptureEvent
	pcaps   map[string][]byte
}

// fakeFaultEntry mirrors what the real FaultStore keeps per node.
type fakeFaultEntry struct {
	profile  model.FaultProfile
	counters map[model.FaultAction]uint64
}

func newFakeNodeView() *fakeNodeView {
	return &fakeNodeView{
		nodes:   map[string]model.Node{},
		devices: map[string]map[string]model.DownstreamDevice{},
		faults:  map[string]*fakeFaultEntry{},
		events:  map[string][]port.CaptureEvent{},
		pcaps:   map[string][]byte{},
	}
}

// seed registers a node and drives it to status through legal transitions.
func (f *fakeNodeView) seed(t *testing.T, id, addr string, status model.Status) model.Node {
	t.Helper()
	profile, err := model.NewNodeProfile(id, addr, "3402000000", "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	node := model.NewNode(profile)
	// Walk the happy path plus a final stop so Offline is reachable too.
	for _, step := range []model.Status{
		model.StatusRegistering, model.StatusRegistered,
		model.StatusOnline, model.StatusOffline,
	} {
		if node.Status() == status {
			break
		}
		next, err := node.WithStatus(step)
		if err != nil {
			t.Fatalf("seed %s to %s: %v", id, status, err)
		}
		node = next
		if node.Status() == status {
			break
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nodes[id] = node
	return node
}

func (f *fakeNodeView) put(n model.Node) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nodes[n.ID().String()] = n
}

// InstallFault arms the fake node with the supplied profile. Unknown nodes
// are rejected with model.ErrUnknownNode to match the real service.
func (f *fakeNodeView) InstallFault(_ context.Context, id model.NodeID, p model.FaultProfile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.nodes[id.String()]; !ok {
		return model.ErrUnknownNode
	}
	entry := &fakeFaultEntry{
		profile:  p,
		counters: map[model.FaultAction]uint64{},
	}
	f.faults[id.String()] = entry
	return nil
}

// GetFault returns the installed profile, its counters, and whether one
// exists for the node.
func (f *fakeNodeView) GetFault(_ context.Context, id model.NodeID) (model.FaultProfile, map[model.FaultAction]uint64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.faults[id.String()]
	if !ok || e == nil {
		return model.FaultProfile{}, nil, false
	}
	counters := make(map[model.FaultAction]uint64, len(e.counters))
	for k, v := range e.counters {
		counters[k] = v
	}
	return e.profile, counters, true
}

// ClearFault removes the profile and resets counters. Clearing a node
// without a profile is a no-op.
func (f *fakeNodeView) ClearFault(_ context.Context, id model.NodeID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.faults, id.String())
	return nil
}

// QueryCapture returns buffered events for the fake node. limit caps the
// number of returned events; 0 or negative returns the whole buffer.
func (f *fakeNodeView) QueryCapture(_ context.Context, id model.NodeID, limit int) ([]port.CaptureEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	all := f.events[id.String()]
	if limit <= 0 || limit > len(all) {
		return append([]port.CaptureEvent(nil), all...), nil
	}
	return append([]port.CaptureEvent(nil), all[:limit]...), nil
}

// CapturePCAP returns a fixed binary blob for the fake node.
func (f *fakeNodeView) CapturePCAP(_ context.Context, id model.NodeID) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.pcaps[id.String()]
	if !ok {
		return []byte("pcap:" + id.String()), nil
	}
	return append([]byte(nil), b...), nil
}

// bumpCounter is a test helper that simulates an action being recorded
// against the fault store for node id.
func (f *fakeNodeView) bumpCounter(t *testing.T, nodeID string, action model.FaultAction, delta uint64) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.faults[nodeID]
	if e == nil {
		e = &fakeFaultEntry{}
		f.faults[nodeID] = e
	}
	e.counters[action] += delta
}

func (f *fakeNodeView) List(_ context.Context) []model.Node {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.Node, 0, len(f.nodes))
	for _, n := range f.nodes {
		out = append(out, n)
	}
	return out
}

func (f *fakeNodeView) Get(_ context.Context, id model.NodeID) (model.Node, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.nodes[id.String()]
	return n, ok
}

func (f *fakeNodeView) Start(_ context.Context, id model.NodeID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.nodes[id.String()]
	if !ok {
		return fmt.Errorf("unknown node %s", id)
	}
	next, err := n.WithStatus(model.StatusRegistering)
	if err != nil {
		return err
	}
	f.nodes[id.String()] = next
	return nil
}

// Unregister mirrors the real service: only a node that is online (or at
// least registered) may leave, anything else is an illegal transition.
// seedDevice puts one device in a node's online table, which is what the
// device endpoints read.
func (f *fakeNodeView) seedDevice(t *testing.T, nodeID, deviceID, addr string) model.DownstreamDevice {
	t.Helper()
	dev, err := model.NewDownstreamDevice(model.DownstreamDeviceParams{
		DeviceID: deviceID,
		Addr:     addr,
		Contact:  "<sip:" + deviceID + "@" + addr + ">",
		Now:      time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewDownstreamDevice: %v", err)
	}
	dev = dev.WithGranted(3600, dev.RegisteredAt())
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.devices[nodeID] == nil {
		f.devices[nodeID] = map[string]model.DownstreamDevice{}
	}
	f.devices[nodeID][deviceID] = dev
	return dev
}

func (f *fakeNodeView) Devices(_ context.Context, id model.NodeID) ([]model.DownstreamDevice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.nodes[id.String()]; !ok {
		return nil, fmt.Errorf("%w: %s", model.ErrUnknownNode, id)
	}
	rows := f.devices[id.String()]
	out := make([]model.DownstreamDevice, 0, len(rows))
	for _, dev := range rows {
		out = append(out, dev)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeviceID() < out[j].DeviceID() })
	return out, nil
}

func (f *fakeNodeView) Device(_ context.Context, id model.NodeID, deviceID string) (model.DownstreamDevice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.nodes[id.String()]; !ok {
		return model.DownstreamDevice{}, fmt.Errorf("%w: %s", model.ErrUnknownNode, id)
	}
	dev, ok := f.devices[id.String()][deviceID]
	if !ok {
		return model.DownstreamDevice{}, fmt.Errorf("%w: %s", model.ErrUnknownDevice, deviceID)
	}
	return dev, nil
}

func (f *fakeNodeView) Unregister(_ context.Context, id model.NodeID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.nodes[id.String()]
	if !ok {
		return fmt.Errorf("unknown node %s", id)
	}
	if n.Status() != model.StatusOnline && n.Status() != model.StatusRegistered {
		return fmt.Errorf("%w: %s -> %s", model.ErrIllegalTransition, n.Status(), model.StatusOffline)
	}
	next, err := n.WithStatus(model.StatusOffline)
	if err != nil {
		return err
	}
	f.nodes[id.String()] = next
	return nil
}

func (f *fakeNodeView) Stop(_ context.Context, id model.NodeID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.nodes[id.String()]
	if !ok {
		return fmt.Errorf("unknown node %s", id)
	}
	next, err := n.WithStatus(model.StatusOffline)
	if err != nil {
		return err
	}
	f.nodes[id.String()] = next
	return nil
}

func (f *fakeNodeView) TriggerAlarm(_ context.Context, id model.NodeID, in app.AlarmInput) (model.AlarmSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.nodes[id.String()]
	if !ok {
		return model.AlarmSnapshot{}, fmt.Errorf("unknown node %s", id)
	}
	snap, err := model.NewAlarmSnapshot(
		fmt.Sprintf("%s-%d", id.String(), time.Now().UnixNano()),
		id.String(), in.ChannelID, in.Priority, in.Method, in.Description, time.Now().Format("2006-01-02T15:04:05"),
	)
	if err != nil {
		return model.AlarmSnapshot{}, err
	}
	next, err := n.Profile().AppendAlarm(snap)
	if err != nil {
		return model.AlarmSnapshot{}, err
	}
	f.nodes[id.String()] = n.WithProfile(next)
	return snap, nil
}

func (f *fakeNodeView) SetPosition(_ context.Context, id model.NodeID, pos model.Position) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.nodes[id.String()]
	if !ok {
		return fmt.Errorf("unknown node %s", id)
	}
	f.nodes[id.String()] = n.WithProfile(n.Profile().WithPosition(pos))
	return nil
}

func (f *fakeNodeView) SetChannelStatus(_ context.Context, id model.NodeID, channelID string, status model.ChannelStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.nodes[id.String()]
	if !ok {
		return fmt.Errorf("unknown node %s", id)
	}
	next, err := n.Profile().WithChannelStatus(channelID, status)
	if err != nil {
		return err
	}
	f.nodes[id.String()] = n.WithProfile(next)
	return nil
}

func newNodesServer(t *testing.T, view httpapi.NodeView) *httptest.Server {
	t.Helper()
	s := httpapi.NewServer(platformconfig.Config{}, logging.NewHub(4), httpapi.Version{Version: "x"}, view, nil)
	return httptest.NewServer(s.Echo())
}

func getJSON(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(body)
}

func postJSON(t *testing.T, url string) (int, string) {
	return postJSONWithBody(t, url, nil)
}

func postJSONWithBody(t *testing.T, url string, body []byte) (int, string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytesReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(b)
}

func putJSON(t *testing.T, url string, body []byte) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, url, bytesReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(b)
}

func deleteJSON(t *testing.T, url string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodDelete, url, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(b)
}

func bytesReader(b []byte) io.Reader {
	if b == nil {
		return nil
	}
	return bytes.NewReader(b)
}

// TestNodes_ListEmpty asserts an empty inventory renders as a JSON array,
// not null (task 8.1).
func TestNodes_ListEmpty(t *testing.T) {
	ts := newNodesServer(t, newFakeNodeView())
	defer ts.Close()

	code, body := getJSON(t, ts.URL+"/v1/nodes")
	if code != http.StatusOK {
		t.Fatalf("GET /v1/nodes = %d, want 200", code)
	}
	var got []map[string]string
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("body %q is not a JSON array: %v", body, err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

// TestNodes_ListFields asserts each entry carries id/kind/status/addr
// (task 8.1).
func TestNodes_ListFields(t *testing.T) {
	view := newFakeNodeView()
	view.seed(t, "34020000011310000001", "127.0.0.1:5060", model.StatusIdle)
	view.seed(t, "34020000012000000001", "127.0.0.1:5061", model.StatusIdle)
	ts := newNodesServer(t, view)
	defer ts.Close()

	code, body := getJSON(t, ts.URL+"/v1/nodes")
	if code != http.StatusOK {
		t.Fatalf("GET /v1/nodes = %d, want 200", code)
	}
	var got []map[string]string
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, body)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (%s)", len(got), body)
	}
	for _, entry := range got {
		for _, field := range []string{"id", "kind", "status", "addr"} {
			if _, ok := entry[field]; !ok {
				t.Errorf("entry %v missing field %q", entry, field)
			}
		}
	}
}

// TestNodes_DetailFoundAndUnknown asserts 200 for a known id and 404 with a
// JSON error body for an unknown one (task 8.2).
func TestNodes_DetailFoundAndUnknown(t *testing.T) {
	view := newFakeNodeView()
	view.seed(t, "34020000011310000001", "127.0.0.1:5060", model.StatusIdle)
	ts := newNodesServer(t, view)
	defer ts.Close()

	code, body := getJSON(t, ts.URL+"/v1/nodes/34020000011310000001")
	if code != http.StatusOK {
		t.Fatalf("GET known node = %d, want 200 (%s)", code, body)
	}
	if !json.Valid([]byte(body)) {
		t.Errorf("body %q is not valid JSON", body)
	}

	code, body = getJSON(t, ts.URL+"/v1/nodes/34020000011310000009")
	if code != http.StatusNotFound {
		t.Fatalf("GET unknown node = %d, want 404 (%s)", code, body)
	}
	var errBody map[string]string
	if err := json.Unmarshal([]byte(body), &errBody); err != nil {
		t.Fatalf("404 body %q is not a JSON object: %v", body, err)
	}
	if errBody["error"] == "" {
		t.Errorf("404 body %q has no error field", body)
	}
}

// TestNodes_StartStop asserts the control endpoints return 200 and move the
// node through registering then offline (task 8.3).
func TestNodes_StartStop(t *testing.T) {
	view := newFakeNodeView()
	view.seed(t, "34020000011310000001", "127.0.0.1:5060", model.StatusOffline)
	ts := newNodesServer(t, view)
	defer ts.Close()

	code, body := postJSON(t, ts.URL+"/v1/nodes/34020000011310000001/start")
	if code != http.StatusOK {
		t.Fatalf("POST start = %d, want 200 (%s)", code, body)
	}
	var started map[string]string
	if err := json.Unmarshal([]byte(body), &started); err != nil {
		t.Fatalf("start body: %v", err)
	}
	if started["status"] != "registering" {
		t.Errorf("status after start = %q, want registering", started["status"])
	}

	code, body = postJSON(t, ts.URL+"/v1/nodes/34020000011310000001/stop")
	if code != http.StatusOK {
		t.Fatalf("POST stop = %d, want 200 (%s)", code, body)
	}
	var stopped map[string]string
	if err := json.Unmarshal([]byte(body), &stopped); err != nil {
		t.Fatalf("stop body: %v", err)
	}
	if stopped["status"] != "offline" {
		t.Errorf("status after stop = %q, want offline", stopped["status"])
	}
}

// TestNodes_ConflictOnIllegalTransition asserts an illegal transition is 409
// and the body reports the status the node is actually in (design D8).
func TestNodes_ConflictOnIllegalTransition(t *testing.T) {
	view := newFakeNodeView()
	view.seed(t, "34020000011310000001", "127.0.0.1:5060", model.StatusOnline)
	ts := newNodesServer(t, view)
	defer ts.Close()

	code, body := postJSON(t, ts.URL+"/v1/nodes/34020000011310000001/start")
	if code != http.StatusConflict {
		t.Fatalf("POST start on an online node = %d, want 409 (%s)", code, body)
	}
	var conflict struct {
		Error  string `json:"error"`
		Status string `json:"status"`
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(body), &conflict); err != nil {
		t.Fatalf("409 body: %v", err)
	}
	if conflict.Status != "online" {
		t.Errorf("conflict status = %q, want online", conflict.Status)
	}
	if conflict.Action != "start" {
		t.Errorf("conflict action = %q, want start", conflict.Action)
	}
	if conflict.Error == "" {
		t.Error("conflict body has no error field")
	}

	// The node must be untouched by the refused request.
	if n, _ := view.Get(context.Background(), mustNodeID(t, "34020000011310000001")); n.Status() != model.StatusOnline {
		t.Errorf("node status = %v, want online (unchanged)", n.Status())
	}
}

// TestNodes_MalformedID asserts a syntactically invalid id is rejected
// before any lookup.
func TestNodes_MalformedID(t *testing.T) {
	ts := newNodesServer(t, newFakeNodeView())
	defer ts.Close()
	if code, _ := getJSON(t, ts.URL+"/v1/nodes/not-an-id"); code != http.StatusBadRequest {
		t.Errorf("GET malformed id = %d, want 400", code)
	}
}

// TestNodes_HealthEndpointsUnaffected asserts the pre-existing endpoints
// still work after the node routes were added (task 8.4).
func TestNodes_HealthEndpointsUnaffected(t *testing.T) {
	ts := newNodesServer(t, newFakeNodeView())
	defer ts.Close()
	for _, path := range []string{"/v1/health", "/healthz"} {
		code, body := getJSON(t, ts.URL+path)
		if code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200 (%s)", path, code, body)
		}
	}
}

func mustNodeID(t *testing.T, raw string) model.NodeID {
	t.Helper()
	id, err := model.ParseNodeID(raw)
	if err != nil {
		t.Fatalf("ParseNodeID(%q): %v", raw, err)
	}
	return id
}

// TestNodes_TriggerAlarm asserts the runtime API end-to-end: POST triggers an
// alarm that the NodeView stores in the node's profile (task 6.4).
func TestNodes_TriggerAlarmStoresSnapshot(t *testing.T) {
	view := newFakeNodeView()
	nodeID := "34020000011310000001"
	view.seed(t, nodeID, "127.0.0.1:5060", model.StatusOnline)
	ts := newNodesServer(t, view)
	defer ts.Close()

	payload := `{"channelID":"34020000011320000001","priority":2,"method":1,"description":"smoke on floor 3"}`
	code, body := postJSONWithBody(t, ts.URL+"/v1/nodes/"+nodeID+"/actions/trigger-alarm", []byte(payload))
	if code != http.StatusAccepted {
		t.Fatalf("trigger-alarm = %d, want 202 (%s)", code, body)
	}
	var out struct {
		ID          string `json:"id"`
		DeviceID    string `json:"deviceID"`
		EventTime   string `json:"eventTime"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("trigger-alarm body: %v", err)
	}
	if out.ID == "" || out.DeviceID != nodeID || out.Description != "smoke on floor 3" {
		t.Fatalf("unexpected snapshot in body: %+v", out)
	}

	n, _ := view.Get(context.Background(), mustNodeID(t, nodeID))
	alarms := n.Profile().Alarms()
	if len(alarms) != 1 {
		t.Fatalf("stored %d alarms, want 1", len(alarms))
	}
	got := alarms[0]
	if got.ID() != out.ID || got.ChannelID() != "34020000011320000001" || got.Description() != "smoke on floor 3" {
		t.Fatalf("stored snapshot mismatch: %+v", got)
	}
}

// TestNodes_SetPositionStoresPosition asserts the position endpoint round-trips
// a position into the node's profile (task 6.4).
func TestNodes_SetPositionStoresPosition(t *testing.T) {
	view := newFakeNodeView()
	nodeID := "34020000011310000001"
	view.seed(t, nodeID, "127.0.0.1:5060", model.StatusOnline)
	ts := newNodesServer(t, view)
	defer ts.Close()

	payload := `{"longitude":116.39,"latitude":39.9,"speed":12.5}`
	code, body := putJSON(t, ts.URL+"/v1/nodes/"+nodeID+"/actions/position", []byte(payload))
	if code != http.StatusNoContent {
		t.Fatalf("set position = %d, want 204 (%s)", code, body)
	}
	n, _ := view.Get(context.Background(), mustNodeID(t, nodeID))
	pos, ok := n.Profile().Position()
	if !ok {
		t.Fatal("node has no stored position")
	}
	if pos.Longitude() != 116.39 || pos.Latitude() != 39.9 || pos.Speed() != 12.5 {
		t.Fatalf("stored position mismatch: %+v", pos)
	}
}

// TestNodes_TriggerAlarmUnknownNode asserts an unknown node id is 404.
func TestNodes_TriggerAlarmUnknownNode(t *testing.T) {
	ts := newNodesServer(t, newFakeNodeView())
	defer ts.Close()
	code, body := postJSONWithBody(t, ts.URL+"/v1/nodes/34020000011310000001/actions/trigger-alarm", []byte(`{"priority":1}`))
	if code != http.StatusNotFound {
		t.Fatalf("trigger-alarm on unknown node = %d, want 404 (%s)", code, body)
	}
}

// captureEventResponse mirrors the server's capture response in the test
// package because the real type is unexported in the server package.
type captureEventResponse struct {
	Direction string    `json:"direction"`
	Local     string    `json:"local"`
	Remote    string    `json:"remote"`
	Transport string    `json:"transport"`
	Bytes     string    `json:"bytes"`
	At        time.Time `json:"at"`
}

// nodeResponse mirrors the server's node detail response in the test package.
type nodeResponse struct {
	ID            string                       `json:"id"`
	Kind          string                       `json:"kind"`
	Status        string                       `json:"status"`
	Addr          string                       `json:"addr"`
	FaultCounters map[model.FaultAction]uint64 `json:"fault_counters,omitempty"`
}

// TestNodes_CaptureEndpoints asserts the capture query and pcap endpoints
// honour the node existence, limit, and content-type contracts (task 6.3).
func TestNodes_CaptureEndpoints(t *testing.T) {
	view := newFakeNodeView()
	nodeID := "34020000011310000001"
	view.seed(t, nodeID, "127.0.0.1:5060", model.StatusOnline)
	view.mu.Lock()
	view.events[nodeID] = append(view.events[nodeID],
		port.CaptureEvent{Direction: model.DirTransmit, Local: "127.0.0.1:5060", Remote: "1.2.3.4:5060", Bytes: []byte("a"), At: time.Now()},
		port.CaptureEvent{Direction: model.DirReceive, Local: "127.0.0.1:5060", Remote: "1.2.3.4:5060", Bytes: []byte("b"), At: time.Now()},
	)
	view.pcaps[nodeID] = []byte("fake-pcap")
	view.mu.Unlock()

	ts := newNodesServer(t, view)
	defer ts.Close()

	// Unknown node -> 404. Use a syntactically valid node id (type code
	// 131 = device) that is not present in the fake registry so the
	// 20-character validation passes and existence is what fails.
	code, body := getJSON(t, ts.URL+"/v1/nodes/34020000131180000001/capture")
	if code != http.StatusNotFound {
		t.Fatalf("unknown capture query = %d, want 404 (%s)", code, body)
	}

	// Limit=1 returns the oldest event only.
	code, body = getJSON(t, ts.URL+"/v1/nodes/"+nodeID+"/capture?limit=1")
	if code != http.StatusOK {
		t.Fatalf("capture query = %d, want 200 (%s)", code, body)
	}
	var out []captureEventResponse
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode capture json: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("capture len = %d, want 1", len(out))
	}
	if string(out[0].Bytes) != "a" {
		t.Fatalf("capture oldest = %q, want \"a\"", string(out[0].Bytes))
	}

	// PCAP endpoint returns binary with the right content type.
	resp, err := http.Get(ts.URL + "/v1/nodes/" + nodeID + "/capture.pcap")
	if err != nil {
		t.Fatalf("GET pcap: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.Header.Get("Content-Type") != "application/vnd.tcpdump.pcap" {
		t.Fatalf("pcap content type = %s", resp.Header.Get("Content-Type"))
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pcap status = %d, want 200", resp.StatusCode)
	}
	data, _ := io.ReadAll(resp.Body)
	if string(data) != "fake-pcap" {
		t.Fatalf("pcap body = %q, want fake-pcap", string(data))
	}
}

// TestNodes_DetailFaultCounters asserts the node detail endpoint exposes
// fault_counters when a profile is armed, and omits the field after the
// profile is cleared (task 6.4).
func TestNodes_DetailFaultCounters(t *testing.T) {
	view := newFakeNodeView()
	nodeID := "34020000011310000001"
	view.seed(t, nodeID, "127.0.0.1:5060", model.StatusOnline)
	ts := newNodesServer(t, view)
	defer ts.Close()

	// Arm a fault profile and simulate one recorded action so the detail
	// endpoint has a counter to report.
	_, _ = postJSONWithBody(t, ts.URL+"/v1/nodes/"+nodeID+"/fault", []byte(`{"status_code":403,"methods":["INVITE"]}`))
	view.bumpCounter(t, nodeID, model.FaultCannedResponse, 1)

	code, body := getJSON(t, ts.URL+"/v1/nodes/"+nodeID)
	if code != http.StatusOK {
		t.Fatalf("node detail = %d, want 200 (%s)", code, body)
	}
	var detail nodeResponse
	if err := json.Unmarshal([]byte(body), &detail); err != nil {
		t.Fatalf("decode node detail: %v", err)
	}
	if detail.FaultCounters[model.FaultCannedResponse] != 1 {
		t.Fatalf("fault_counters = %v, want {canned_response:1}", detail.FaultCounters)
	}

	// Clear the profile.
	cc, _ := deleteJSON(t, ts.URL+"/v1/nodes/"+nodeID+"/fault")
	if cc != http.StatusNoContent {
		t.Fatalf("clear-fault = %d, want 204", cc)
	}

	code, body = getJSON(t, ts.URL+"/v1/nodes/"+nodeID)
	if code != http.StatusOK {
		t.Fatalf("node detail after clear = %d, want 200 (%s)", code, body)
	}
	var after nodeResponse
	if err := json.Unmarshal([]byte(body), &after); err != nil {
		t.Fatalf("decode node detail after clear: %v", err)
	}
	if len(after.FaultCounters) != 0 {
		t.Fatalf("fault_counters = %v, want empty after clear", after.FaultCounters)
	}
}

// TestNodes_FaultsPluralRoute asserts the spec-mandated /faults (plural)
// path exposes the same install/inspect/clear contract as /fault, including
// the 404 for an unknown node.
func TestNodes_FaultsPluralRoute(t *testing.T) {
	view := newFakeNodeView()
	nodeID := "34020000011310000001"
	view.seed(t, nodeID, "127.0.0.1:5060", model.StatusOnline)
	ts := newNodesServer(t, view)
	defer ts.Close()

	// Unknown node via the plural path is 404.
	code, body := postJSONWithBody(t, ts.URL+"/v1/nodes/34020000131180000001/faults", []byte(`{}`))
	if code != http.StatusNotFound {
		t.Fatalf("install fault on unknown node via /faults = %d, want 404 (%s)", code, body)
	}

	// Install via the plural path.
	code, body = postJSONWithBody(t, ts.URL+"/v1/nodes/"+nodeID+"/faults", []byte(`{"canned":{"REGISTER":403}}`))
	if code != http.StatusOK {
		t.Fatalf("install fault via /faults = %d, want 200 (%s)", code, body)
	}

	// Inspect via the plural path.
	code, body = getJSON(t, ts.URL+"/v1/nodes/"+nodeID+"/faults")
	if code != http.StatusOK {
		t.Fatalf("get fault via /faults = %d, want 200 (%s)", code, body)
	}
	var state struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(body), &state); err != nil {
		t.Fatalf("decode fault state: %v", err)
	}
	if state.Status != "ok" {
		t.Fatalf("fault status = %q, want ok", state.Status)
	}

	// Clear via the plural path.
	cc, _ := deleteJSON(t, ts.URL+"/v1/nodes/"+nodeID+"/faults")
	if cc != http.StatusNoContent {
		t.Fatalf("clear fault via /faults = %d, want 204", cc)
	}
	// After clearing, inspect via the plural path is 404 again.
	code, _ = getJSON(t, ts.URL+"/v1/nodes/"+nodeID+"/faults")
	if code != http.StatusNotFound {
		t.Fatalf("get fault after clear via /faults = %d, want 404", code)
	}
}

// TestScenarios_RunPlaceholder asserts the Change 14 placeholder endpoint
// answers 501 with a body that names the follow-up change, so the web UI
// can surface the limitation instead of failing silently (task 3.1).
func TestScenarios_RunPlaceholder(t *testing.T) {
	ts := newNodesServer(t, newFakeNodeView())
	defer ts.Close()

	code, body := postJSONWithBody(t, ts.URL+"/v1/scenarios/run", []byte(`{"id":"s1"}`))
	if code != http.StatusNotImplemented {
		t.Fatalf("POST /v1/scenarios/run = %d, want 501 (%s)", code, body)
	}
	var errBody map[string]string
	if err := json.Unmarshal([]byte(body), &errBody); err != nil {
		t.Fatalf("501 body %q is not a JSON object: %v", body, err)
	}
	if errBody["error"] == "" {
		t.Fatal("501 body has no error field")
	}
}
