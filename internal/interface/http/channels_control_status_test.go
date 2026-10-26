package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// unsupportedControlView reports the same typed errors NodeService does while
// the signalling is unimplemented.
type unsupportedControlView struct{ *fakeChannelView }

func (v unsupportedControlView) PTZ(_ context.Context, id model.NodeID, ch, _ string, _, _ int) error {
	return port.PTZUnsupported(id, ch, "outbound device control is not implemented")
}

func (v unsupportedControlView) Records(_ context.Context, id model.NodeID, ch string, _, _ time.Time) ([]model.RecordInfoItem, error) {
	return nil, port.RecordUnsupported(id, ch, "record query is not implemented")
}

// A missing capability must surface as 501, never as a 200 the caller would
// read as success.
func TestChannelControl_UnsupportedIs501(t *testing.T) {
	nodes := newFakeNodeView()
	node := nodes.seed(t, "34020000001310000001", "127.0.0.1:15061", model.StatusOnline)
	ts := newMediaFileServer(t, nodes, unsupportedControlView{newFakeChannelView()})
	defer ts.Close()

	resp, err := http.Post(
		ts.URL+"/v1/nodes/"+node.ID().String()+"/channels/34020000001310000001001/ptz",
		"application/json", strings.NewReader(`{"direction":"up","speed":128}`))
	if err != nil {
		t.Fatalf("POST ptz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("PTZ status = %d, want 501", resp.StatusCode)
	}

	recordsURL := ts.URL + "/v1/nodes/" + node.ID().String() +
		"/channels/34020000001310000001001/records?start=2026-10-01T00:00:00Z&end=2026-10-08T00:00:00Z"
	resp2, err := http.Get(recordsURL)
	if err != nil {
		t.Fatalf("GET records: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusNotImplemented {
		t.Errorf("records status = %d, want 501", resp2.StatusCode)
	}
}
