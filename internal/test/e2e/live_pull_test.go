// End-to-end coverage for the platform's live pull.
//
// Change: platform-live-pull. The chain under test crosses a process
// boundary in both directions: the simulated platform opens a SIP dialog with
// a simulated device, receives its RTP, and serves the resulting PS stream
// through /v1/flv. A 200 from the FLV endpoint alone would not prove a
// picture exists, so the assertion is the FLV magic.

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestLivePull_PlatformPullsDeviceStream(t *testing.T) {
	bin := buildSimulator(t)
	dir := t.TempDir()
	httpPort := freeTCPPort(t)
	cfgPath := writeSimulatorConfig(t, dir, httpPort)
	proc := startSimulator(t, bin, cfgPath, dir, httpPort)

	platformAddr := fmt.Sprintf("127.0.0.1:%d", freeUDPPort(t))
	deviceAddr := fmt.Sprintf("127.0.0.1:%d", freeUDPPort(t))
	const platformID = "34020000002000000001"
	const username = e2eDeviceID

	// The platform serves downstreams; one account is enough for the device.
	status, body := proc.call(t, http.MethodPost, "/v1/nodes", map[string]any{
		"id": platformID, "kind": "platform-large", "domain": "3402000000",
		"addr": platformAddr, "vendor": "e2e",
		"platform": map[string]any{"realm": "3402000000"},
	})
	if status != http.StatusCreated {
		t.Fatalf("create platform = %d, want 201 (body %v)", status, body)
	}
	status, body = proc.call(t, http.MethodPost, "/v1/platforms/"+platformID+"/accounts",
		map[string]any{"username": username, "password": "secret"})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("add account = %d (body %v)", status, body)
	}

	// The device registers to the platform and serves one synthetic channel.
	status, body = proc.call(t, http.MethodPost, "/v1/nodes", map[string]any{
		"id": e2eDeviceID, "kind": "device", "domain": "3402000000",
		"addr": deviceAddr, "vendor": "e2e",
		"registration": map[string]any{
			"server": platformAddr, "server_id": platformID,
			"username": username, "password": "secret", "expires": 3600,
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("create device = %d, want 201 (body %v)", status, body)
	}
	status, body = proc.call(t, http.MethodPost, "/v1/nodes/"+e2eDeviceID+"/channels",
		map[string]any{"name": "cam-1"})
	if status != http.StatusCreated {
		t.Fatalf("create channel = %d, want 201 (body %v)", status, body)
	}
	channelID, _ := body["id"].(string)
	if channelID == "" {
		t.Fatalf("channel id missing: %v", body)
	}
	if status, body = proc.call(t, http.MethodPut,
		"/v1/nodes/"+e2eDeviceID+"/channels/"+channelID+"/media",
		map[string]any{"kind": "synthetic"}); status != http.StatusOK {
		t.Fatalf("set media = %d, want 200 (body %v)", status, body)
	}

	if status, body = proc.call(t, http.MethodPost, "/v1/nodes/"+platformID+"/start", nil); status != http.StatusOK {
		t.Fatalf("start platform = %d (body %v)", status, body)
	}
	proc.waitNodeStatus(t, platformID, "online")
	if status, body = proc.call(t, http.MethodPost, "/v1/nodes/"+e2eDeviceID+"/start", nil); status != http.StatusOK {
		t.Fatalf("start device = %d (body %v)", status, body)
	}
	proc.waitNodeStatus(t, e2eDeviceID, "online")

	// The platform must see the device before it can pull from it. The
	// devices endpoint answers a bare array, not an object.
	deadline := time.Now().Add(15 * time.Second)
	for {
		resp, err := http.Get(proc.baseURL + "/v1/nodes/" + platformID + "/devices")
		if err == nil {
			var devices []map[string]any
			derr := json.NewDecoder(resp.Body).Decode(&devices)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK && derr == nil && len(devices) > 0 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the platform never saw the device (last err %v)", err)
		}
		time.Sleep(200 * time.Millisecond)
	}

	status, body = proc.call(t, http.MethodPost,
		"/v1/nodes/"+platformID+"/channels/"+channelID+"/live", nil)
	if status != http.StatusOK {
		t.Fatalf("live start = %d, want 200 (body %v)", status, body)
	}
	if port, _ := body["local_port"].(float64); port <= 0 {
		t.Errorf("live start reported no local port: %v", body)
	}
	if transport, _ := body["transport"].(string); transport != "udp" {
		t.Errorf("transport = %q, want udp by default", transport)
	}

	// The FLV endpoint must serve the pulled stream, starting with the magic.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	url := proc.baseURL + "/v1/flv/" + platformID + "/" + channelID + "?source=live"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET flv: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("flv = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "video/x-flv" {
		t.Errorf("content type = %q, want video/x-flv", ct)
	}
	head := make([]byte, 3)
	if _, err := io.ReadFull(resp.Body, head); err != nil {
		t.Fatalf("read FLV header: %v", err)
	}
	if string(head) != "FLV" {
		t.Errorf("body starts with %q, want FLV", head)
	}

	// Stopping is idempotent and ends the pull.
	if status, body = proc.call(t, http.MethodPost,
		"/v1/nodes/"+platformID+"/channels/"+channelID+"/live/stop", nil); status != http.StatusNoContent {
		t.Fatalf("live stop = %d, want 204 (body %v)", status, body)
	}
	if status, _ = proc.call(t, http.MethodPost,
		"/v1/nodes/"+platformID+"/channels/"+channelID+"/live/stop", nil); status != http.StatusNoContent {
		t.Errorf("second live stop = %d, want 204", status)
	}
}
