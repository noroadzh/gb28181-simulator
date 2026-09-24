package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// The online device table is the platform's answer to "who is actually
// talking to me", so it has to be readable over HTTP.
func TestNodeDevices_ListsDevices(t *testing.T) {
	view := newFakeNodeView()
	node := view.seed(t, "34020000002000000001", "127.0.0.1:15061", model.StatusOnline)
	view.seedDevice(t, node.ID().String(), "34020000011310000002", "127.0.0.1:15070")
	view.seedDevice(t, node.ID().String(), "34020000011310000001", "127.0.0.1:15060")
	srv := newNodesServer(t, view)
	defer srv.Close()

	status, body := getJSON(t, srv.URL+"/v1/nodes/"+node.ID().String()+"/devices")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", status, body)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		t.Fatalf("decode body %q: %v", body, err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (body %s)", len(rows), body)
	}
	// Ordered by device id, so a caller can diff two responses.
	if rows[0]["device_id"] != "34020000011310000001" || rows[1]["device_id"] != "34020000011310000002" {
		t.Errorf("rows out of order: %v then %v", rows[0]["device_id"], rows[1]["device_id"])
	}
	for _, key := range []string{"device_id", "addr", "expires", "registered_at", "expires_at", "last_seen_at"} {
		if _, ok := rows[0][key]; !ok {
			t.Errorf("row is missing %q: %v", key, rows[0])
		}
	}
	if rows[0]["expires"] != float64(3600) {
		t.Errorf("expires = %v, want 3600", rows[0]["expires"])
	}
}

// A platform with nobody registered is an empty array, never null: callers
// bind straight to it.
func TestNodeDevices_EmptyTable(t *testing.T) {
	view := newFakeNodeView()
	node := view.seed(t, "34020000002000000001", "127.0.0.1:15061", model.StatusOnline)
	srv := newNodesServer(t, view)
	defer srv.Close()

	status, body := getJSON(t, srv.URL+"/v1/nodes/"+node.ID().String()+"/devices")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if strings.TrimSpace(body) != "[]" {
		t.Errorf("body = %q, want an empty array", body)
	}
}

func TestNodeDevice_Detail(t *testing.T) {
	view := newFakeNodeView()
	node := view.seed(t, "34020000002000000001", "127.0.0.1:15061", model.StatusOnline)
	view.seedDevice(t, node.ID().String(), "34020000011310000001", "127.0.0.1:15060")
	srv := newNodesServer(t, view)
	defer srv.Close()

	status, body := getJSON(t, srv.URL+"/v1/nodes/"+node.ID().String()+"/devices/34020000011310000001")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", status, body)
	}
	if !strings.Contains(body, "34020000011310000001") || !strings.Contains(body, "127.0.0.1:15060") {
		t.Errorf("body = %s, want the device id and its address", body)
	}
	// The table is how an operator sees the platform; it must never carry
	// how the device proved itself.
	for _, secret := range []string{"password", "Authorization", "response"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(secret)) {
			t.Errorf("body leaks %q: %s", secret, body)
		}
	}
}

// An unknown node and an unknown device are both 404 — the request named
// something that is not there, which is not a server fault.
func TestNodeDevices_NotFound(t *testing.T) {
	view := newFakeNodeView()
	node := view.seed(t, "34020000002000000001", "127.0.0.1:15061", model.StatusOnline)
	view.seedDevice(t, node.ID().String(), "34020000011310000001", "127.0.0.1:15060")
	srv := newNodesServer(t, view)
	defer srv.Close()

	cases := []struct {
		name string
		path string
	}{
		{"unknown node list", "/v1/nodes/34020000002000000099/devices"},
		{"unknown device", "/v1/nodes/" + node.ID().String() + "/devices/34020000041310000099"},
		{"unknown node detail", "/v1/nodes/34020000002000000099/devices/34020000011310000001"},
		{"malformed node id", "/v1/nodes/not-a-node/devices"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			status, body := getJSON(t, srv.URL+tc.path)
			if status != http.StatusNotFound && status != http.StatusBadRequest {
				t.Errorf("status = %d, want 404 (or 400 for a malformed id), body %s", status, body)
			}
			if !strings.Contains(body, "error") {
				t.Errorf("body = %s, want a JSON error body", body)
			}
		})
	}
}
