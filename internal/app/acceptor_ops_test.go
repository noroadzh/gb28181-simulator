package app

// device-control-coverage: the six operations commands at the acceptor.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/adapter/manscdp"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// opsHarness serves a device node with a real codec, so each command is
// decoded and answered exactly as it would be on the wire.
type opsHarness struct {
	*deviceHarness
}

func newOpsHarness(t *testing.T) *opsHarness {
	t.Helper()
	deviceID := mustDeviceNodeID(t, "34020000001310000051")
	h := newDeviceHarness(t, deviceID, nil, nil)
	return &opsHarness{deviceHarness: h}
}

func (h *opsHarness) deliver(t *testing.T, body string) model.Message {
	t.Helper()
	return h.tr.deliver(t, deviceMessageRequest(t, h.deviceID.String(), body))
}

func TestOps_DeviceStatusQuery(t *testing.T) {
	h := newOpsHarness(t)
	body := `<Query><CmdType>DeviceStatus</CmdType><SN>1</SN><DeviceID>` + h.deviceID.String() + `</DeviceID></Query>`
	resp := h.deliver(t, body)
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d", resp.StatusCode())
	}
	for _, want := range []string{"<CmdType>DeviceStatus</CmdType>", "<Result>OK</Result>", "<Online>ONLINE</Online>"} {
		if !strings.Contains(resp.Body(), want) {
			t.Errorf("answer missing %q:\n%s", want, resp.Body())
		}
	}
}

func TestOps_AlarmQueryReturnsRecordedAlarms(t *testing.T) {
	h := newOpsHarness(t)
	snap, err := model.NewAlarmSnapshot("a1", h.deviceID.String(), "ch-1", 1, 5, "motion",
		time.Now().Format("2006-01-02T15:04:05"))
	if err != nil {
		t.Fatalf("NewAlarmSnapshot: %v", err)
	}
	if _, err := h.reg.MutateProfile(context.Background(), h.deviceID, func(p model.NodeProfile) (model.NodeProfile, error) {
		return p.AppendAlarm(snap)
	}); err != nil {
		t.Fatalf("AppendAlarm: %v", err)
	}
	body := `<Query><CmdType>Alarm</CmdType><SN>2</SN><DeviceID>` + h.deviceID.String() + `</DeviceID></Query>`
	resp := h.deliver(t, body)
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d", resp.StatusCode())
	}
	if !strings.Contains(resp.Body(), "<SumNum>1</SumNum>") {
		t.Errorf("alarm query did not return the recorded alarm:\n%s", resp.Body())
	}
}

func TestOps_ConfigQueryAndWrite(t *testing.T) {
	h := newOpsHarness(t)
	q := `<Query><CmdType>ConfigDownload</CmdType><SN>3</SN><DeviceID>` + h.deviceID.String() +
		`</DeviceID><ConfigType>BasicParam</ConfigType></Query>`
	resp := h.deliver(t, q)
	if resp.StatusCode() != 200 || !strings.Contains(resp.Body(), "<Result>OK</Result>") {
		t.Fatalf("config query: status=%d body=%s", resp.StatusCode(), resp.Body())
	}

	w := `<Control><CmdType>ConfigDownload</CmdType><SN>4</SN><DeviceID>` + h.deviceID.String() +
		`</DeviceID><ConfigType>BasicParam</ConfigType><BasicParam><HeartBeatInterval>45</HeartBeatInterval></BasicParam></Control>`
	resp = h.deliver(t, w)
	if resp.StatusCode() != 200 || !strings.Contains(resp.Body(), "<Result>OK</Result>") {
		t.Fatalf("config write: status=%d body=%s", resp.StatusCode(), resp.Body())
	}
	node, _ := h.reg.Get(context.Background(), h.deviceID)
	if hb, ok := node.Profile().HeartbeatOverride(); !ok || hb != 45*time.Second {
		t.Errorf("heartbeat override = %v, %v; want 45s", hb, ok)
	}

	bad := `<Control><CmdType>ConfigDownload</CmdType><SN>5</SN><DeviceID>` + h.deviceID.String() +
		`</DeviceID><ConfigType>VideoParam</ConfigType><BasicParam><HeartBeatInterval>30</HeartBeatInterval></BasicParam></Control>`
	resp = h.deliver(t, bad)
	if !strings.Contains(resp.Body(), "<Result>ERROR</Result>") {
		t.Errorf("an unsupported config type was accepted:\n%s", resp.Body())
	}
}

func TestOps_UpgradeAndRebootRecordsState(t *testing.T) {
	h := newOpsHarness(t)
	up := `<Control><CmdType>DeviceUpgrade</CmdType><SN>6</SN><DeviceID>` + h.deviceID.String() +
		`</DeviceID><Firmware>2.0.0</Firmware></Control>`
	resp := h.deliver(t, up)
	if resp.StatusCode() != 200 || !strings.Contains(resp.Body(), "<Result>OK</Result>") {
		t.Fatalf("upgrade: status=%d body=%s", resp.StatusCode(), resp.Body())
	}
	node, _ := h.reg.Get(context.Background(), h.deviceID)
	if node.Profile().UpgradeTarget() != "2.0.0" {
		t.Errorf("upgrade target = %q", node.Profile().UpgradeTarget())
	}
	if node.Status() == model.StatusFault || node.Status() == model.StatusOffline {
		t.Errorf("upgrade disturbed the node: %s", node.Status())
	}

	reboot := `<Control><CmdType>DeviceControl</CmdType><SN>7</SN><DeviceID>` + h.deviceID.String() +
		`</DeviceID><TeleBoot>Boot</TeleBoot></Control>`
	if resp := h.deliver(t, reboot); resp.StatusCode() != 200 {
		t.Fatalf("reboot status = %d", resp.StatusCode())
	}
	node, _ = h.reg.Get(context.Background(), h.deviceID)
	if _, ok := node.Profile().LastReboot(); !ok {
		t.Error("reboot was not recorded")
	}
}

func TestOps_GuardCmdSetsArmingState(t *testing.T) {
	h := newOpsHarness(t)
	arm := `<Control><CmdType>DeviceControl</CmdType><SN>8</SN><DeviceID>` + h.deviceID.String() +
		`</DeviceID><GuardCmd>SetGuard</GuardCmd></Control>`
	if resp := h.deliver(t, arm); resp.StatusCode() != 200 {
		t.Fatalf("arm status = %d", resp.StatusCode())
	}
	node, _ := h.reg.Get(context.Background(), h.deviceID)
	if armed, set := node.Profile().AlarmArmed(); !set || !armed {
		t.Errorf("armed = %v set = %v, want armed", armed, set)
	}

	disarm := strings.Replace(arm, "SetGuard", "ResetGuard", 1)
	if resp := h.deliver(t, disarm); resp.StatusCode() != 200 {
		t.Fatalf("disarm status = %d", resp.StatusCode())
	}
	node, _ = h.reg.Get(context.Background(), h.deviceID)
	if armed, _ := node.Profile().AlarmArmed(); armed {
		t.Error("ResetGuard left the node armed")
	}
}

func TestOps_BroadcastAck(t *testing.T) {
	h := newOpsHarness(t)
	body := `<Notify><CmdType>Broadcast</CmdType><SN>9</SN><SourceID>34020000002000000001</SourceID><TargetID>` +
		h.deviceID.String() + `</TargetID></Notify>`
	resp := h.deliver(t, body)
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d", resp.StatusCode())
	}
	if !strings.Contains(resp.Body(), "<CmdType>Broadcast</CmdType>") ||
		!strings.Contains(resp.Body(), "<Result>OK</Result>") {
		t.Errorf("broadcast ack body:\n%s", resp.Body())
	}
}

// The codec is real here, so a malformed body must not answer.
func TestOps_MalformedBodyIsIgnored(t *testing.T) {
	h := newOpsHarness(t)
	h.tr.mu.Lock()
	before := len(h.tr.sent)
	h.tr.mu.Unlock()
	h.tr.inbound <- deviceMessageRequest(t, h.deviceID.String(), "<Query><CmdType>DeviceStatus</CmdType>")
	time.Sleep(100 * time.Millisecond)
	h.tr.mu.Lock()
	after := len(h.tr.sent)
	h.tr.mu.Unlock()
	if after != before {
		t.Errorf("a malformed query produced %d messages", after-before)
	}
}

var _ = manscdp.NewMANSCDPCodec

// A platform forwards a status query to the downstream that owns the target
// and relays its answer verbatim.
func TestOps_PlatformForwardsStatusToDownstream(t *testing.T) {
	h, _ := newMessageHarnessWithRegistryAndMedia(t, nil)
	h.acceptor.controlWait = 200 * time.Millisecond
	dev, err := model.NewDownstreamDevice(model.DownstreamDeviceParams{
		DeviceID: "34020000001310000061", Addr: "127.0.0.1:15060", Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("NewDownstreamDevice: %v", err)
	}
	if err := h.devices.Upsert(context.Background(), h.nodeID, dev); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	h.codec.setNotify(mustNotify(t, model.CmdTypeDeviceStatus, dev.DeviceID(), 31))
	h.codec.setDeviceStatusQuery(model.DeviceStatusQuery{SN: 31, DeviceID: dev.DeviceID()})

	done := make(chan model.Message, 1)
	go func() {
		done <- h.tr.deliver(t, messageRequest(t, dev.DeviceID(), "<Query/>"))
	}()
	forwarded := waitForSend(t, h.tr)
	if headerValue(forwarded, "To") != "<sip:"+dev.DeviceID()+"@127.0.0.1:15060>" {
		t.Errorf("forwarded To = %q", headerValue(forwarded, "To"))
	}
	answer, err := model.NewResponse(200, "OK", []model.Header{
		model.NewHeader("Call-ID", headerValue(forwarded, "Call-ID")),
		model.NewHeader("CSeq", headerValue(forwarded, "CSeq")),
		model.NewHeader("Content-Type", "application/MANSCDP+XML"),
	}, "device-status-from-child")
	if err != nil {
		t.Fatalf("NewResponse: %v", err)
	}
	h.tr.inbound <- answer

	select {
	case resp := <-done:
		if resp.StatusCode() != 200 || !strings.Contains(resp.Body(), "from-child") {
			t.Fatalf("relayed answer = %d %q", resp.StatusCode(), resp.Body())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the platform never answered the upstream query")
	}
}

// A broadcast makes the device open the audio dialog the standard puts on its
// side of the command; the platform it is registered with is the target.
func TestOps_BroadcastStartsDeviceInvite(t *testing.T) {
	h := newOpsHarness(t)
	reg, err := model.NewRegistration(model.RegistrationParams{
		Server: "127.0.0.1:15070", ServerID: "34020000002000000001",
		Password: "secret", Expires: 3600, Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}
	if _, err := h.reg.MutateProfile(context.Background(), h.deviceID,
		func(p model.NodeProfile) (model.NodeProfile, error) { return p.WithRegistration(reg) }); err != nil {
		t.Fatalf("WithRegistration: %v", err)
	}
	h.acceptor.controlWait = 300 * time.Millisecond

	body := `<Notify><CmdType>Broadcast</CmdType><SN>11</SN><SourceID>34020000002000000001</SourceID><TargetID>` +
		h.deviceID.String() + `</TargetID></Notify>`
	if resp := h.deliver(t, body); resp.StatusCode() != 200 {
		t.Fatalf("broadcast ack status = %d", resp.StatusCode())
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.tr.mu.Lock()
		for _, msg := range h.tr.sent {
			if msg.Method() == "INVITE" && strings.Contains(msg.Body(), "m=audio") {
				h.tr.mu.Unlock()
				return
			}
		}
		h.tr.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the device never opened the broadcast audio dialog")
}
