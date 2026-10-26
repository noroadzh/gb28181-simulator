// End-to-end coverage for the talk audio path.
//
// Change: talk-audio-bridge, task 2.4.
//
// What can be observed from outside the process: the SIP dialog completes
// (the device answers its own talk INVITE), the WebSocket endpoint accepts
// that session and rejects unknown ones, and talk/stop closes it. Per-frame
// audio assertions live in the unit tests, because both RTP sockets are
// internal to the node.
package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestTalk_AudioSessionOverWebSocket(t *testing.T) {
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
	status, body = proc.call(t, http.MethodPost, "/v1/nodes/"+e2eDeviceID+"/start", nil)
	if status != http.StatusOK {
		t.Fatalf("start device = %d, want 200 (body %v)", status, body)
	}
	proc.waitNodeStatus(t, e2eDeviceID, "online")

	const channelID = "34020000001310000001001"
	status, body = proc.call(t, http.MethodPost,
		"/v1/nodes/"+e2eDeviceID+"/channels/"+channelID+"/talk/start", nil)
	if status != http.StatusOK {
		t.Fatalf("talk/start = %d, want 200 (body %v)", status, body)
	}
	session, _ := body["session_id"].(string)
	wsPath, _ := body["ws_url"].(string)
	if session == "" || wsPath == "" {
		t.Fatalf("talk/start returned session=%q ws_url=%q", session, wsPath)
	}
	if !strings.Contains(wsPath, session) {
		t.Errorf("ws_url %q does not carry the session id", wsPath)
	}

	wsURL := "ws" + strings.TrimPrefix(proc.baseURL, "http") + wsPath
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial %s: %v (status %v)", wsURL, err, statusOf(resp))
	}
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}

	// One up-link frame must not close the socket.
	if err := conn.WriteMessage(websocket.BinaryMessage, make([]byte, 160)); err != nil {
		t.Fatalf("write up-link frame: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := conn.WriteMessage(websocket.BinaryMessage, make([]byte, 160)); err != nil {
		t.Fatalf("socket closed after an up-link frame: %v", err)
	}
	_ = conn.Close()

	// An unknown session is refused before the upgrade.
	badURL := "ws" + strings.TrimPrefix(proc.baseURL, "http") +
		"/v1/nodes/" + e2eDeviceID + "/channels/" + channelID + "/talk/audio?session=nope"
	if _, badResp, err := websocket.DefaultDialer.Dial(badURL, nil); err == nil {
		t.Fatal("dialling an unknown talk session succeeded")
	} else if badResp == nil || badResp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown session status = %v, want 404", badResp)
	}

	status, body = proc.call(t, http.MethodPost,
		"/v1/nodes/"+e2eDeviceID+"/channels/"+channelID+"/talk/stop",
		map[string]any{"session_id": session})
	if status != http.StatusNoContent {
		t.Fatalf("talk/stop = %d, want 204 (body %v)", status, body)
	}
}
