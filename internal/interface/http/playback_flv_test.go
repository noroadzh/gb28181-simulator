package httpapi_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	httpapi "github.com/your-org/gb28181-simulator/internal/interface/http"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
)

// fakeStreaming is the FLV source: it hands out one chunk and closes.
type fakeStreaming struct {
	ch    chan []byte
	calls int
}

func (f *fakeStreaming) Subscribe(_ context.Context, _ model.NodeID, _ string) (<-chan []byte, error) {
	f.calls++
	return f.ch, nil
}

func (f *fakeStreaming) SubscribeSource(_ context.Context, _ model.NodeID, _, source string) (<-chan []byte, error) {
	if source != "local" {
		return nil, fmt.Errorf("playback preview asked for %q", source)
	}
	f.calls++
	return f.ch, nil
}

func (f *fakeStreaming) Subscribers(model.NodeID, string) int { return 0 }
func (f *fakeStreaming) Close() error                         { return nil }

// playbackView reports one live playback session.
type playbackView struct {
	*fakeChannelView
	end                      time.Time
	startedSn                string
	startedStart, startedEnd time.Time
}

func (v *playbackView) PlaybackWindow(sn string) (time.Time, time.Time, bool) {
	if sn != "sn1" {
		return time.Time{}, time.Time{}, false
	}
	return time.Now().Add(-time.Minute), v.end, true
}

func (v *playbackView) StartPlaybackStream(_ context.Context, _ model.NodeID, _ string, sn string, start, end time.Time) error {
	if sn != "sn1" {
		return fmt.Errorf("unknown sn")
	}
	v.startedSn, v.startedStart, v.startedEnd = sn, start, end
	return nil
}

func newPlaybackServer(t *testing.T, view httpapi.ChannelView, fs httpapi.StreamingService) (*httptest.Server, model.NodeID) {
	t.Helper()
	nodes := newFakeNodeView()
	node := nodes.seed(t, "34020000001310000001", "127.0.0.1:15061", model.StatusOnline)
	s := httpapi.NewServer(platformconfig.Config{}, logging.NewHub(4), httpapi.Version{Version: "x"},
		nodes, nil, view, nil, httpapi.NewStreamingServer(fs, nil))
	ts := httptest.NewServer(s.Echo())
	t.Cleanup(ts.Close)
	return ts, node.ID()
}

// Task 2.1: an active session streams FLV; an unknown one is 404.
func TestPlaybackFLV_StreamsActiveSession(t *testing.T) {
	fs := &fakeStreaming{ch: make(chan []byte, 1)}
	view := &playbackView{fakeChannelView: newFakeChannelView(), end: time.Now().Add(time.Minute)}
	ts, nodeID := newPlaybackServer(t, view, fs)

	fs.ch <- []byte("FLV\x01\x05\x00\x00\x00\x09")
	close(fs.ch)

	resp, err := http.Get(ts.URL + "/v1/nodes/" + nodeID.String() + "/channels/ch-1/playback/flv?sn=sn1")
	if err != nil {
		t.Fatalf("GET playback/flv: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "video/x-flv" {
		t.Errorf("content type = %q, want video/x-flv", got)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 || string(body[:3]) != "FLV" {
		t.Errorf("body does not start with the FLV magic: %q", body)
	}

	resp2, err := http.Get(ts.URL + "/v1/nodes/" + nodeID.String() + "/channels/ch-1/playback/flv?sn=nope")
	if err != nil {
		t.Fatalf("GET unknown sn: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("unknown sn status = %d, want 404", resp2.StatusCode)
	}
}

// Task 2.1: a play that carries a window opens the stream and records it.
func TestPlaybackFLV_PlayWithWindowStartsStream(t *testing.T) {
	view := &playbackView{fakeChannelView: newFakeChannelView(), end: time.Now().Add(time.Minute)}
	ts, nodeID := newPlaybackServer(t, view, &fakeStreaming{ch: make(chan []byte)})

	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(5 * time.Minute)
	body := fmt.Sprintf(`{"action":"play","sn":"sn1","start":%q,"end":%q}`,
		start.Format(time.RFC3339), end.Format(time.RFC3339))
	req, _ := http.NewRequest(http.MethodPost,
		ts.URL+"/v1/nodes/"+nodeID.String()+"/channels/ch-1/playback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST playback: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if view.startedSn != "sn1" {
		t.Fatalf("StartPlaybackStream was not called (sn=%q)", view.startedSn)
	}
	if !view.startedStart.Equal(start) || !view.startedEnd.Equal(end) {
		t.Errorf("window = %s..%s, want %s..%s", view.startedStart, view.startedEnd, start, end)
	}
}

func (f *fakeStreaming) SubscribeRecording(_ context.Context, _ model.NodeID, _ string, _, _ time.Time) (<-chan []byte, error) {
	return nil, fmt.Errorf("recording playback unsupported")
}
