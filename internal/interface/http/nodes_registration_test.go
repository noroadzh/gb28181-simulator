package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// stagedError stands in for the app layer's registration failure. It
// satisfies the domain's StagedFailure interface, which is how the HTTP
// layer learns the stage without importing the app package.
type stagedError struct {
	stage string
}

func (e stagedError) Error() string {
	return "app: registration failed at " + e.stage + ": timed out waiting for a registration response"
}

func (e stagedError) FailureStage() string { return e.stage }

// failingStartView starts normally and then reports a registration failure,
// leaving the node faulted exactly as the app layer does.
type failingStartView struct {
	*fakeNodeView
	err error
}

func (v *failingStartView) Start(ctx context.Context, id model.NodeID) error {
	if err := v.fakeNodeView.Start(ctx, id); err != nil {
		return err
	}
	n, ok := v.Get(ctx, id)
	if !ok {
		return v.err
	}
	if next, err := n.WithStatus(model.StatusFault); err == nil {
		v.put(next)
	}
	return v.err
}

// A registration failure is the platform's answer, not a server bug: the
// endpoint must say so, name the stage, and never echo credentials
// (task 9.1).
func TestNodeStart_RegistrationFailureReportsStage(t *testing.T) {
	view := &failingStartView{fakeNodeView: newFakeNodeView(), err: stagedError{stage: "timeout"}}
	node := view.seed(t, "34020000011310000001", "127.0.0.1:5060", model.StatusIdle)
	srv := newNodesServer(t, view)
	defer srv.Close()

	status, body := postJSON(t, srv.URL+"/v1/nodes/"+node.ID().String()+"/start")
	if status != http.StatusBadGateway {
		t.Errorf("status = %d, want %d (bad gateway)", status, http.StatusBadGateway)
	}
	if !strings.Contains(body, `"stage":"timeout"`) {
		t.Errorf("body does not name the failing stage: %s", body)
	}
	if !strings.Contains(body, `"action":"start"`) {
		t.Errorf("body does not name the action: %s", body)
	}
	for _, secret := range []string{"secret", "password", "response="} {
		if strings.Contains(body, secret) {
			t.Errorf("body leaks %q: %s", secret, body)
		}
	}
	// The node really was left faulted, which the response echoes.
	if !strings.Contains(body, `"status":"fault"`) {
		t.Errorf("body does not report the node's status: %s", body)
	}
}

// The inventory and the detail view must be able to show the statuses a
// device node reaches now: online after a successful registration, fault
// after a failed one (task 9.2).
func TestNodeViews_ShowOnlineAndFault(t *testing.T) {
	view := newFakeNodeView()
	online := view.seed(t, "34020000011310000001", "127.0.0.1:5060", model.StatusOnline)

	// Fault needs a hand-built node: seed only walks the happy path.
	profile, err := model.NewNodeProfile("34020000011310000002", "127.0.0.1:5061", "3402000000", "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	faulted, err := model.NewNode(profile).WithStatus(model.StatusRegistering)
	if err != nil {
		t.Fatalf("WithStatus(registering): %v", err)
	}
	faulted, err = faulted.WithStatus(model.StatusFault)
	if err != nil {
		t.Fatalf("WithStatus(fault): %v", err)
	}
	view.put(faulted)

	srv := newNodesServer(t, view)
	defer srv.Close()

	status, body := getJSON(t, srv.URL+"/v1/nodes")
	if status != http.StatusOK {
		t.Fatalf("GET /v1/nodes = %d", status)
	}
	if !strings.Contains(body, `"status":"online"`) {
		t.Errorf("list does not show online: %s", body)
	}
	if !strings.Contains(body, `"status":"fault"`) {
		t.Errorf("list does not show fault: %s", body)
	}

	status, body = getJSON(t, srv.URL+"/v1/nodes/"+online.ID().String())
	if status != http.StatusOK {
		t.Fatalf("GET /v1/nodes/{id} = %d", status)
	}
	if !strings.Contains(body, `"status":"online"`) {
		t.Errorf("detail does not show online: %s", body)
	}
}

// A registration success leaves the node online, and the endpoint keeps
// returning 200 with the new status (task 9.1, happy path).
func TestNodeStart_SuccessLeavesOnline(t *testing.T) {
	view := newFakeNodeView()
	node := view.seed(t, "34020000011310000001", "127.0.0.1:5060", model.StatusIdle)
	srv := newNodesServer(t, view)
	defer srv.Close()

	status, body := postJSON(t, srv.URL+"/v1/nodes/"+node.ID().String()+"/start")
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200: %s", status, body)
	}
	if !strings.Contains(body, `"status":"registering"`) {
		t.Errorf("body = %s, want the registering status this view produces", body)
	}
}
