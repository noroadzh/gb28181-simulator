package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/noroadzh/gb28181-simulator/internal/domain/model"
	"github.com/noroadzh/gb28181-simulator/internal/domain/port"
	httpapi "github.com/noroadzh/gb28181-simulator/internal/interface/http"
	platformconfig "github.com/noroadzh/gb28181-simulator/internal/platform/config"
	"github.com/noroadzh/gb28181-simulator/internal/platform/observability/logging"
)

type cascadeView struct {
	*fakeChannelView
	sessions []port.CascadeSession
}

func (v *cascadeView) ListCascadeSessions(_ context.Context, _ model.NodeID) []port.CascadeSession {
	return v.sessions
}

func TestCascadeSessions_ListsActiveRelays(t *testing.T) {
	nodes := newFakeNodeView()
	node := nodes.seed(t, "34020000002000000001", "127.0.0.1:15061", model.StatusOnline)
	view := &cascadeView{
		fakeChannelView: newFakeChannelView(),
		sessions: []port.CascadeSession{{
			UpstreamCallID: "call-1",
			ChannelID:      "ch-1",
			Peer:           "127.0.0.1:15070",
			Transport:      "udp",
			LocalPort:      40000,
			StartedAt:      time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC),
		}},
	}
	s := httpapi.NewServer(platformconfig.Config{}, logging.NewHub(4), httpapi.Version{Version: "x"},
		nodes, nil, view, nil, nil)
	ts := httptest.NewServer(s.Echo())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/v1/nodes/" + node.ID().String() + "/cascade/sessions")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got struct {
		Sessions []struct {
			UpstreamCallID string `json:"upstream_call_id"`
			ChannelID      string `json:"channel_id"`
			LocalPort      int    `json:"local_port"`
		} `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Sessions) != 1 || got.Sessions[0].UpstreamCallID != "call-1" || got.Sessions[0].LocalPort != 40000 {
		t.Fatalf("got %+v", got)
	}
}

func TestCascadeSessions_EmptyWhenNone(t *testing.T) {
	nodes := newFakeNodeView()
	node := nodes.seed(t, "34020000002000000001", "127.0.0.1:15061", model.StatusOnline)
	view := &cascadeView{fakeChannelView: newFakeChannelView()}
	s := httpapi.NewServer(platformconfig.Config{}, logging.NewHub(4), httpapi.Version{Version: "x"},
		nodes, nil, view, nil, nil)
	ts := httptest.NewServer(s.Echo())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/v1/nodes/" + node.ID().String() + "/cascade/sessions")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got struct {
		Sessions []any `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Sessions == nil || len(got.Sessions) != 0 {
		t.Fatalf("want empty slice, got %#v", got.Sessions)
	}
}
