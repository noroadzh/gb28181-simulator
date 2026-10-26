// End-to-end coverage for the playback FLV endpoint.
//
// Change: playback-flv-stream, task 2.2.
//
// The chain under test: a device node with a synthetic media source answers a
// playback INVITE carrying a time window, and the FLV endpoint serves that
// session. The assertion is the FLV magic in the body — a 200 alone would not
// prove a picture exists.
package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestPlaybackFLV_ServesTheSessionWindow(t *testing.T) {
	bin := buildSimulator(t)
	dir := t.TempDir()
	httpPort := freeTCPPort(t)
	cfgPath := writeSimulatorConfig(t, dir, httpPort)
	proc := startSimulator(t, bin, cfgPath, dir, httpPort)

	sipAddr := fmt.Sprintf("127.0.0.1:%d", freeUDPPort(t))
	status, body := proc.call(t, http.MethodPost, "/v1/nodes", map[string]any{
		"id": e2eDeviceID, "kind": "device", "domain": "3402000000",
		"addr": sipAddr, "vendor": "e2e",
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
	status, body = proc.call(t, http.MethodPut,
		"/v1/nodes/"+e2eDeviceID+"/channels/"+channelID+"/media",
		map[string]any{"kind": "synthetic"})
	if status != http.StatusOK {
		t.Fatalf("set media = %d, want 200 (body %v)", status, body)
	}
	status, body = proc.call(t, http.MethodPost, "/v1/nodes/"+e2eDeviceID+"/start", nil)
	if status != http.StatusOK {
		t.Fatalf("start device = %d, want 200 (body %v)", status, body)
	}
	proc.waitNodeStatus(t, e2eDeviceID, "online")

	start := time.Now().Add(-time.Minute)
	end := time.Now().Add(30 * time.Second)
	const sn = "e2e-pb-1"
	status, body = proc.call(t, http.MethodPost,
		"/v1/nodes/"+e2eDeviceID+"/channels/"+channelID+"/playback",
		map[string]any{
			"action": "play", "sn": sn,
			"start": start.Format(time.RFC3339), "end": end.Format(time.RFC3339),
		})
	if status != http.StatusOK {
		t.Fatalf("play with window = %d, want 200 (body %v)", status, body)
	}

	// The FLV endpoint must start the body with the FLV magic.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		proc.baseURL+"/v1/nodes/"+e2eDeviceID+"/channels/"+channelID+"/playback/flv?sn="+sn, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET playback/flv: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("playback/flv = %d, want 200", resp.StatusCode)
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

	// An unknown session is 404, not an empty 200.
	status, _ = proc.call(t, http.MethodGet,
		"/v1/nodes/"+e2eDeviceID+"/channels/"+channelID+"/playback/flv?sn=nope", nil)
	if status != http.StatusNotFound {
		t.Errorf("unknown sn = %d, want 404", status)
	}
}
