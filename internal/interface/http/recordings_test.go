package httpapi_test

// Tasks 2.1 / 2.2: the platform recording library endpoints.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
	httpapi "github.com/your-org/gb28181-simulator/internal/interface/http"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
)

type recordingView struct {
	*fakeChannelView
	recs []port.Recording
	err  error
	seen struct {
		start, end time.Time
		called     bool
	}
}

func (v *recordingView) Recordings(_ context.Context, _ model.NodeID, _ string, start, end time.Time) ([]port.Recording, error) {
	v.seen.start, v.seen.end, v.seen.called = start, end, true
	return v.recs, v.err
}

type recordingStreaming struct {
	*capturingStreaming
	chunks   [][]byte
	window   struct{ start, end time.Time }
	recorded string
}

// capturingStreaming satisfies the rest of StreamingService; the recording
// endpoints only use SubscribeRecording.
type capturingStreaming struct{}

func (c *capturingStreaming) Subscribe(context.Context, model.NodeID, string) (<-chan []byte, error) {
	return nil, fmt.Errorf("not used")
}

func (c *capturingStreaming) SubscribeSource(context.Context, model.NodeID, string, string) (<-chan []byte, error) {
	return nil, fmt.Errorf("not used")
}

func (c *capturingStreaming) SubscribeRecording(_ context.Context, _ model.NodeID, _ string, start, end time.Time) (<-chan []byte, error) {
	return nil, fmt.Errorf("not used")
}

func (c *capturingStreaming) Subscribers(model.NodeID, string) int { return 0 }
func (c *capturingStreaming) Close() error                         { return nil }

func (s *recordingStreaming) SubscribeRecording(_ context.Context, _ model.NodeID, channelID string, start, end time.Time) (<-chan []byte, error) {
	s.recorded = channelID
	s.window.start, s.window.end = start, end
	ch := make(chan []byte, len(s.chunks))
	for _, c := range s.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func newRecordingServer(t *testing.T, view httpapi.ChannelView, fs httpapi.StreamingService) (*httptest.Server, model.NodeID) {
	t.Helper()
	nodes := newFakeNodeView()
	node := nodes.seed(t, "34020000002000000001", "127.0.0.1:15061", model.StatusOnline)
	s := httpapi.NewServer(platformconfig.Config{}, logging.NewHub(4), httpapi.Version{Version: "x"},
		nodes, nil, view, nil, httpapi.NewStreamingServer(fs, nil))
	ts := httptest.NewServer(s.Echo())
	t.Cleanup(ts.Close)
	return ts, node.ID()
}

func testRecording(t *testing.T, nodeID model.NodeID) port.Recording {
	t.Helper()
	start := time.Date(2026, 10, 27, 10, 0, 0, 0, time.Local)
	return port.Recording{
		ID: "rec-1", NodeID: nodeID, ChannelID: "ch-1",
		Start: start, End: start.Add(time.Minute), Size: 2048,
		Path: "/tmp/rec-1.ps",
	}
}

func TestRecordingList_ReturnsWindow(t *testing.T) {
	view := &recordingView{fakeChannelView: newFakeChannelView()}
	ts, nodeID := newRecordingServer(t, view, &capturingStreaming{})
	view.recs = []port.Recording{testRecording(t, nodeID)}

	resp, err := http.Get(ts.URL + "/v1/nodes/" + nodeID.String() + "/channels/ch-1/recordings")
	if err != nil {
		t.Fatalf("GET recordings: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got struct {
		Recordings []struct {
			ID       string `json:"id"`
			Start    string `json:"start_time"`
			End      string `json:"end_time"`
			Size     int64  `json:"size"`
			FilePath string `json:"file_path"`
		} `json:"recordings"`
	}
	raw, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode: %v (body %s)", err, raw)
	}
	if len(got.Recordings) != 1 || got.Recordings[0].ID != "rec-1" || got.Recordings[0].Size != 2048 {
		t.Fatalf("recordings = %+v", got.Recordings)
	}
	if got.Recordings[0].Start != "2026-10-27T10:00:00+08:00" {
		t.Errorf("start_time = %q", got.Recordings[0].Start)
	}
	if !view.seen.called || !view.seen.start.IsZero() || !view.seen.end.IsZero() {
		t.Errorf("an unbounded query passed a window: %+v", view.seen)
	}
}

func TestRecordingList_ParsesWindow(t *testing.T) {
	view := &recordingView{fakeChannelView: newFakeChannelView()}
	ts, nodeID := newRecordingServer(t, view, &capturingStreaming{})

	start := "2026-10-27T10:00:00Z"
	end := "2026-10-27T11:00:00Z"
	resp, err := http.Get(ts.URL + "/v1/nodes/" + nodeID.String() + "/channels/ch-1/recordings?start=" + start + "&end=" + end)
	if err != nil {
		t.Fatalf("GET recordings: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if view.seen.start.Format(time.RFC3339) != start || view.seen.end.Format(time.RFC3339) != end {
		t.Errorf("window = %v..%v", view.seen.start, view.seen.end)
	}
}

func TestRecordingList_BadTimeIs400(t *testing.T) {
	view := &recordingView{fakeChannelView: newFakeChannelView()}
	ts, nodeID := newRecordingServer(t, view, &capturingStreaming{})

	resp, err := http.Get(ts.URL + "/v1/nodes/" + nodeID.String() + "/channels/ch-1/recordings?start=yesterday")
	if err != nil {
		t.Fatalf("GET recordings: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestRecordingList_UnsupportedIs501(t *testing.T) {
	view := &recordingView{fakeChannelView: newFakeChannelView(), err: fmt.Errorf("%w: off", port.ErrRecordingUnsupported)}
	ts, nodeID := newRecordingServer(t, view, &capturingStreaming{})

	resp, err := http.Get(ts.URL + "/v1/nodes/" + nodeID.String() + "/channels/ch-1/recordings")
	if err != nil {
		t.Fatalf("GET recordings: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", resp.StatusCode)
	}
}

func TestRecordingFLV_StreamsTheWindow(t *testing.T) {
	view := &recordingView{fakeChannelView: newFakeChannelView()}
	fs := &recordingStreaming{
		capturingStreaming: &capturingStreaming{},
		chunks:             [][]byte{[]byte("FLV\x01\x05\x00\x00\x00\x09")},
	}
	ts, nodeID := newRecordingServer(t, view, fs)
	view.recs = []port.Recording{testRecording(t, nodeID)}

	resp, err := http.Get(ts.URL + "/v1/nodes/" + nodeID.String() + "/channels/ch-1/recordings/rec-1/flv")
	if err != nil {
		t.Fatalf("GET recording flv: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "video/x-flv" {
		t.Errorf("content type = %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.HasPrefix(string(body), "FLV") {
		t.Errorf("body does not start with the FLV magic: %q", body)
	}
	if fs.recorded != "ch-1" || fs.window.start.IsZero() {
		t.Errorf("the gateway got channel %q window %+v", fs.recorded, fs.window)
	}
}

func TestRecordingFLV_UnknownRecordingIs404(t *testing.T) {
	view := &recordingView{fakeChannelView: newFakeChannelView()}
	ts, nodeID := newRecordingServer(t, view, &recordingStreaming{capturingStreaming: &capturingStreaming{}})

	resp, err := http.Get(ts.URL + "/v1/nodes/" + nodeID.String() + "/channels/ch-1/recordings/nope/flv")
	if err != nil {
		t.Fatalf("GET recording flv: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
