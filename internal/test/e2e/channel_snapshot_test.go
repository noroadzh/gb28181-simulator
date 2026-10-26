// End-to-end coverage for the snapshot endpoint.
//
// Change: channel-talk-and-snapshot, task 2.4.
//
// The snapshot is the one channel operation whose result is a picture, so the
// test decodes it: a 200 with a few bytes would satisfy a status-only check
// while telling the operator nothing.
package e2e

import (
	"bytes"
	"fmt"
	"image/jpeg"
	"io"
	"net/http"
	"testing"
)

// getRaw performs a GET and returns status, content type and the raw body.
func getRaw(t *testing.T, url string) (int, string, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), body
}

func TestSnapshot_SyntheticSourceReturnsJPEG(t *testing.T) {
	bin := buildSimulator(t)
	dir := t.TempDir()
	httpPort := freeTCPPort(t)
	cfgPath := writeSimulatorConfig(t, dir, httpPort)
	proc := startSimulator(t, bin, cfgPath, dir, httpPort)

	// A device node owning one channel is enough: the snapshot renders from
	// the channel's media configuration and never touches SIP.
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
		t.Fatalf("channel id missing from the create response: %v", body)
	}

	status, body = proc.call(t, http.MethodPut,
		"/v1/nodes/"+e2eDeviceID+"/channels/"+channelID+"/media",
		map[string]any{"kind": "synthetic"})
	if status != http.StatusOK {
		t.Fatalf("set channel media = %d, want 200 (body %v)", status, body)
	}

	code, contentType, img := getRaw(t,
		proc.baseURL+"/v1/nodes/"+e2eDeviceID+"/channels/"+channelID+"/snapshot")
	if code != http.StatusOK {
		t.Fatalf("snapshot = %d, want 200", code)
	}
	if contentType != "image/jpeg" {
		t.Errorf("content type = %q, want image/jpeg", contentType)
	}
	if len(img) < 2 || img[0] != 0xFF || img[1] != 0xD8 {
		t.Fatalf("body is not a JPEG (starts % X)", img[:min(4, len(img))])
	}
	decoded, err := jpeg.Decode(bytes.NewReader(img))
	if err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if b := decoded.Bounds(); b.Dx() != 640 || b.Dy() != 480 {
		t.Errorf("snapshot is %dx%d, want 640x480", b.Dx(), b.Dy())
	}
}
