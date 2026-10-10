package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// failingUnregisterView makes leaving fail with a staged error, the way a
// platform that never answers does.
type failingUnregisterView struct {
	*fakeNodeView
	err error
}

func (v *failingUnregisterView) Unregister(_ context.Context, _ model.NodeID) error {
	return v.err
}

// Leaving is a first-class action: it reports the node's new status.
func TestNodeUnregister_Succeeds(t *testing.T) {
	view := newFakeNodeView()
	node := view.seed(t, "34020000011310000001", "127.0.0.1:5060", model.StatusOnline)
	ts := newNodesServer(t, view)
	defer ts.Close()

	code, body := postJSON(t, ts.URL+"/v1/nodes/"+node.ID().String()+"/unregister")
	if code != http.StatusOK {
		t.Fatalf("POST /unregister = %d, want 200: %s", code, body)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	if s, ok := got["status"].(string); !ok || s != "offline" {
		t.Errorf("status = %q, want offline", got["status"])
	}
}

// Leaving is only legal for a node that is online: everything else is a
// conflict naming the status the node is actually in.
func TestNodeUnregister_RejectsOfflineNode(t *testing.T) {
	view := newFakeNodeView()
	node := view.seed(t, "34020000011310000001", "127.0.0.1:5060", model.StatusOffline)
	ts := newNodesServer(t, view)
	defer ts.Close()

	code, body := postJSON(t, ts.URL+"/v1/nodes/"+node.ID().String()+"/unregister")
	if code != http.StatusConflict {
		t.Fatalf("POST /unregister = %d, want 409: %s", code, body)
	}
	var got struct {
		Error  string `json:"error"`
		Status string `json:"status"`
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	if got.Status != "offline" || got.Action != "unregister" {
		t.Errorf("body = %+v, want the current status and the action", got)
	}
}

// A platform that refuses the leave is the platform's answer: the endpoint
// says so and names the stage, without echoing credentials.
func TestNodeUnregister_FailureReportsStage(t *testing.T) {
	view := &failingUnregisterView{fakeNodeView: newFakeNodeView(), err: stagedError{stage: "timeout"}}
	node := view.seed(t, "34020000011310000001", "127.0.0.1:5060", model.StatusOnline)
	ts := newNodesServer(t, view)
	defer ts.Close()

	code, body := postJSON(t, ts.URL+"/v1/nodes/"+node.ID().String()+"/unregister")
	if code != http.StatusBadGateway {
		t.Fatalf("POST /unregister = %d, want 502: %s", code, body)
	}
	var got struct {
		Error  string `json:"error"`
		Status string `json:"status"`
		Stage  string `json:"stage"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	if got.Stage != "timeout" {
		t.Errorf("stage = %q, want timeout", got.Stage)
	}
	// The node is still online: the failure changed nothing.
	if got.Status != "online" {
		t.Errorf("status = %q, want online — the registration still stands", got.Status)
	}
}
