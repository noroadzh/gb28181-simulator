package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/adapter/manscdp"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// ptzHarness is a started node whose listener captures what it sends, so the
// test can read the MESSAGE the way the device would.
func ptzHarness(t *testing.T, id, addr string) (*NodeService, *scriptedTransport, model.NodeID) {
	t.Helper()
	svc, _, lc, _ := newTestService(t)
	tr := &scriptedTransport{}
	lc.transport = tr
	if _, err := svc.WithControlCodec(manscdp.NewMANSCDPCodec()); err != nil {
		t.Fatalf("WithControlCodec: %v", err)
	}
	profile := testProfile(t, id, addr)
	if _, err := svc.Create(context.Background(), profile); err != nil {
		t.Fatalf("Create: %v", err)
	}
	nodeID := mustParse(t, id)
	if err := svc.Start(context.Background(), nodeID); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return svc, tr, nodeID
}

// A device node is the simulated camera: the command loops back to its own
// listener, where the acceptor decodes it exactly as it would a real
// platform's DeviceControl.
func TestPTZ_DeviceLoopsBackToItsListener(t *testing.T) {
	svc, tr, id := ptzHarness(t, "34020000001310000001", "127.0.0.1:15061")

	if err := svc.PTZ(context.Background(), id, "34020000001310000001001", "up", 128, 0); err != nil {
		t.Fatalf("PTZ: %v", err)
	}

	sent := tr.messages()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(sent))
	}
	if sent[0].dst != "127.0.0.1:15061" {
		t.Errorf("dst = %q, want the node's own listener", sent[0].dst)
	}
	body := sent[0].msg.Body()
	if !strings.Contains(body, "<CmdType>DeviceControl</CmdType>") {
		t.Errorf("body is not a DeviceControl:\n%s", body)
	}
	wantCmd, err := model.EncodePTZCmd("up", 128)
	if err != nil {
		t.Fatalf("EncodePTZCmd: %v", err)
	}
	if !strings.Contains(body, "<Cmd>"+wantCmd+"</Cmd>") {
		t.Errorf("body does not carry the encoded PTZCmd %q:\n%s", wantCmd, body)
	}
	if !strings.Contains(body, "<DeviceID>"+id.String()+"</DeviceID>") {
		t.Errorf("body targets the wrong device:\n%s", body)
	}
}

// A platform node owns no media: with no online downstream device there is
// nobody to send the command to, and that must be a distinguishable failure.
func TestPTZ_PlatformWithoutDownstreamIsUnreachable(t *testing.T) {
	svc, _, id := ptzHarness(t, "34020000002000000001", "127.0.0.1:15062")

	err := svc.PTZ(context.Background(), id, "ch-1", "up", 64, 0)
	if !errors.Is(err, port.ErrControlTargetUnreachable) {
		t.Fatalf("err = %v, want ErrControlTargetUnreachable", err)
	}
}
