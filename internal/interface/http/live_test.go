package httpapi_test

// Task 3.1: the live-pull control endpoints and their failure vocabulary.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
	httpapi "github.com/your-org/gb28181-simulator/internal/interface/http"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
)

type liveView struct {
	*fakeChannelView
	startErr         error
	stopErr          error
	startedTransport string
	stopped          bool
	startCalls       int
}

func (v *liveView) StartLive(_ context.Context, _ model.NodeID, channelID, transport string) (port.LiveSessionInfo, error) {
	v.startCalls++
	v.startedTransport = transport
	if v.startErr != nil {
		return port.LiveSessionInfo{}, v.startErr
	}
	return port.LiveSessionInfo{SessionID: "live-" + channelID, Transport: transport, LocalPort: 41000, SSRC: 12345}, nil
}

func (v *liveView) StopLive(_ context.Context, _ model.NodeID, _ string) error {
	v.stopped = true
	return v.stopErr
}

func newLiveServer(t *testing.T, view httpapi.ChannelView) (*httptest.Server, model.NodeID) {
	t.Helper()
	nodes := newFakeNodeView()
	node := nodes.seed(t, "34020000002000000001", "127.0.0.1:15061", model.StatusOnline)
	s := httpapi.NewServer(platformconfig.Config{}, logging.NewHub(4), httpapi.Version{Version: "x"},
		nodes, nil, view, nil, nil)
	ts := httptest.NewServer(s.Echo())
	t.Cleanup(ts.Close)
	return ts, node.ID()
}

func TestLiveStart_DefaultsToUDPAndReportsPort(t *testing.T) {
	view := &liveView{fakeChannelView: newFakeChannelView()}
	ts, nodeID := newLiveServer(t, view)

	resp, err := http.Post(ts.URL+"/v1/nodes/"+nodeID.String()+"/channels/ch-1/live", "application/json", nil)
	if err != nil {
		t.Fatalf("POST live: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got struct {
		SessionID string `json:"session_id"`
		Transport string `json:"transport"`
		LocalPort int    `json:"local_port"`
		SSRC      uint32 `json:"ssrc"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.SessionID != "live-ch-1" || got.Transport != "udp" || got.LocalPort != 41000 || got.SSRC != 12345 {
		t.Errorf("response = %+v", got)
	}
	if view.startedTransport != "udp" {
		t.Errorf("view got transport %q, want udp", view.startedTransport)
	}
}

func TestLiveStart_TCP(t *testing.T) {
	view := &liveView{fakeChannelView: newFakeChannelView()}
	ts, nodeID := newLiveServer(t, view)

	resp, err := http.Post(ts.URL+"/v1/nodes/"+nodeID.String()+"/channels/ch-1/live",
		"application/json", strings.NewReader(`{"transport":"tcp"}`))
	if err != nil {
		t.Fatalf("POST live: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if view.startedTransport != "tcp" {
		t.Errorf("view got transport %q, want tcp", view.startedTransport)
	}
}

func TestLiveStart_RejectsUnknownTransport(t *testing.T) {
	view := &liveView{fakeChannelView: newFakeChannelView()}
	ts, nodeID := newLiveServer(t, view)

	resp, err := http.Post(ts.URL+"/v1/nodes/"+nodeID.String()+"/channels/ch-1/live",
		"application/json", strings.NewReader(`{"transport":"sctp"}`))
	if err != nil {
		t.Fatalf("POST live: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if view.startCalls != 0 {
		t.Error("an unknown transport still reached the service")
	}
}

func TestLiveStart_ErrorMapping(t *testing.T) {
	cases := map[string]struct {
		err  error
		want int
	}{
		"unsupported": {fmt.Errorf("%w: device", port.ErrLiveUnsupported), http.StatusNotImplemented},
		"no target":   {fmt.Errorf("%w: none", port.ErrLiveNoTarget), http.StatusNotFound},
		"refused":     {fmt.Errorf("%w: 404", port.ErrLiveRefused), http.StatusBadGateway},
		"unreachable": {fmt.Errorf("%w: gone", port.ErrControlTargetUnreachable), http.StatusBadGateway},
		"timeout":     {fmt.Errorf("%w: 5s", port.ErrControlTimeout), http.StatusGatewayTimeout},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			view := &liveView{fakeChannelView: newFakeChannelView(), startErr: tc.err}
			ts, nodeID := newLiveServer(t, view)
			resp, err := http.Post(ts.URL+"/v1/nodes/"+nodeID.String()+"/channels/ch-1/live", "application/json", nil)
			if err != nil {
				t.Fatalf("POST live: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
}

func TestLiveStart_UnknownNodeIs404(t *testing.T) {
	view := &liveView{fakeChannelView: newFakeChannelView()}
	ts, _ := newLiveServer(t, view)

	resp, err := http.Post(ts.URL+"/v1/nodes/34020000002000000009/channels/ch-1/live", "application/json", nil)
	if err != nil {
		t.Fatalf("POST live: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestLiveStop_Is204(t *testing.T) {
	view := &liveView{fakeChannelView: newFakeChannelView()}
	ts, nodeID := newLiveServer(t, view)

	resp, err := http.Post(ts.URL+"/v1/nodes/"+nodeID.String()+"/channels/ch-1/live/stop", "application/json", nil)
	if err != nil {
		t.Fatalf("POST live stop: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if !view.stopped {
		t.Error("the service was not asked to stop")
	}
}

func TestLiveStop_ErrorMapping(t *testing.T) {
	view := &liveView{fakeChannelView: newFakeChannelView(),
		stopErr: errors.Join(port.ErrControlTargetUnreachable, errors.New("bye failed"))}
	ts, nodeID := newLiveServer(t, view)

	resp, err := http.Post(ts.URL+"/v1/nodes/"+nodeID.String()+"/channels/ch-1/live/stop", "application/json", nil)
	if err != nil {
		t.Fatalf("POST live stop: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
}

// sourceStreaming records which source the FLV handler asked for.
type sourceStreaming struct {
	seen []string
}

func (s *sourceStreaming) Subscribe(ctx context.Context, node model.NodeID, ch string) (<-chan []byte, error) {
	return s.SubscribeSource(ctx, node, ch, "")
}

func (s *sourceStreaming) SubscribeSource(_ context.Context, _ model.NodeID, _, source string) (<-chan []byte, error) {
	s.seen = append(s.seen, source)
	ch := make(chan []byte, 1)
	ch <- []byte("FLV\x01\x05\x00\x00\x00\x09")
	close(ch)
	return ch, nil
}

func (s *sourceStreaming) Subscribers(model.NodeID, string) int { return 0 }
func (s *sourceStreaming) Close() error                         { return nil }

func newSourceServer(t *testing.T) (*httptest.Server, model.NodeID, *sourceStreaming) {
	t.Helper()
	nodes := newFakeNodeView()
	node := nodes.seed(t, "34020000002000000001", "127.0.0.1:15061", model.StatusOnline)
	fs := &sourceStreaming{}
	s := httpapi.NewServer(platformconfig.Config{}, logging.NewHub(4), httpapi.Version{Version: "x"},
		nodes, nil, nil, nil, httpapi.NewStreamingServer(fs, nil))
	ts := httptest.NewServer(s.Echo())
	t.Cleanup(ts.Close)
	return ts, node.ID(), fs
}

// Task 3.2: ?source= reaches the gateway, and an unknown value is a 400
// rather than a silent fallback.
func TestFLVSourceParameter(t *testing.T) {
	ts, nodeID, fs := newSourceServer(t)
	base := ts.URL + "/v1/flv/" + nodeID.String() + "/ch-1"

	for _, tc := range []struct {
		query string
		want  string
	}{
		{"", ""},
		{"?source=live", "live"},
		{"?source=local", "local"},
		{"?source=AUTO", "auto"},
	} {
		resp, err := http.Get(base + tc.query)
		if err != nil {
			t.Fatalf("GET %s: %v", tc.query, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%q status = %d, want 200", tc.query, resp.StatusCode)
		}
		if got := fs.seen[len(fs.seen)-1]; got != tc.want {
			t.Errorf("%q forwarded source %q, want %q", tc.query, got, tc.want)
		}
	}

	before := len(fs.seen)
	resp, err := http.Get(base + "?source=satellite")
	if err != nil {
		t.Fatalf("GET bad source: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad source status = %d, want 400", resp.StatusCode)
	}
	if len(fs.seen) != before {
		t.Error("a bad source still reached the gateway")
	}
}
