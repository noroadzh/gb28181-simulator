// End-to-end coverage for the channel control surface.
//
// Change: channel-ptz-and-playback, task 2.3.
//
// A device node is the simulated camera, so a PTZ command from the API is
// expected to reach that camera over SIP. The node's own capture ring is the
// evidence: it records what arrived on the wire, not what the sender believed
// it sent.
package e2e

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

func TestPTZ_ReachesTheDeviceAsDeviceControl(t *testing.T) {
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
		"/v1/nodes/"+e2eDeviceID+"/channels/"+channelID+"/ptz",
		map[string]any{"direction": "up", "speed": 128})
	if status != http.StatusOK {
		t.Fatalf("POST ptz = %d, want 200 (body %v)", status, body)
	}

	// The device must have received the command as a DeviceControl whose Cmd
	// is the encoded PTZCmd — not just any MESSAGE.
	wantCmd, err := model.EncodePTZCmd("up", 128)
	if err != nil {
		t.Fatalf("EncodePTZCmd: %v", err)
	}
	waitCaptureContains(t, proc, e2eDeviceID, "<CmdType>DeviceControl</CmdType>")
	waitCaptureContains(t, proc, e2eDeviceID, "<Cmd>"+wantCmd+"</Cmd>")
}
