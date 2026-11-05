package app

import (
	"context"
	"strings"
	"testing"

	"github.com/noroadzh/gb28181-simulator/internal/domain/model"
	"github.com/noroadzh/gb28181-simulator/internal/domain/port"
)

type recordingPlaybackPort struct {
	plays   int
	pauses  int
	stops   int
	scales  []float64
	failAll bool
}

func (r *recordingPlaybackPort) Play(_ context.Context, _, _, _, _ string, scale float64) (string, error) {
	if r.failAll {
		return "", port.ErrPlaybackUnsupported
	}
	r.plays++
	r.scales = append(r.scales, scale)
	return "sess", nil
}
func (r *recordingPlaybackPort) Stop(_ context.Context, _ string) error {
	if r.failAll {
		return port.ErrPlaybackUnsupported
	}
	r.stops++
	return nil
}
func (r *recordingPlaybackPort) Query(_ context.Context, sessionID string) (port.PlaybackState, error) {
	return port.PlaybackState{SessionID: sessionID, Status: "playing", Scale: 1}, nil
}
func (r *recordingPlaybackPort) SetScale(_ context.Context, _ string, scale float64) error {
	if r.failAll {
		return port.ErrPlaybackUnsupported
	}
	r.pauses++
	r.scales = append(r.scales, scale)
	return nil
}

func servingPlatform(t *testing.T, h *messageHarness) *platform {
	t.Helper()
	h.acceptor.mu.Lock()
	defer h.acceptor.mu.Unlock()
	p := h.acceptor.serving[h.nodeID.String()]
	if p == nil {
		t.Fatal("platform not serving")
	}
	return p
}

func controlRequest(t *testing.T, body string) model.Message {
	t.Helper()
	msg, err := model.NewRequest("MESSAGE", "sip:34020000011310000001@3402000000", []model.Header{
		model.NewHeader("From", "<sip:34020000002000000001@3402000000>;tag=a"),
		model.NewHeader("To", "<sip:34020000011310000001@3402000000>"),
		model.NewHeader("Call-ID", "ctrl-call"),
		model.NewHeader("CSeq", "1 MESSAGE"),
		model.NewHeader("Content-Type", "Application/MANSCDP+XML"),
	}, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return msg
}

func TestHandlePlaybackControl_DrivesPlaybackPort(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	rec := &recordingPlaybackPort{}
	h.acceptor.WithPlayback(rec)
	p := servingPlatform(t, h)

	body := `<?xml version="1.0"?>
<Control>
  <CmdType>PlaybackControl</CmdType>
  <SN>3</SN>
  <DeviceID>34020000011310000001</DeviceID>
  <ChannelID>ch1</ChannelID>
  <MediaID>sess-1</MediaID>
  <Cmd>Pause</Cmd>
  <Scale>0</Scale>
</Control>`
	req := controlRequest(t, body)
	notify := mustNotify(t, model.CmdTypePlaybackControl, "34020000011310000001", 3)

	resp, ok := h.acceptor.handlePlaybackControl(context.Background(), p, req, notify)
	if !ok || resp.StatusCode() != 200 {
		t.Fatalf("resp ok=%v status=%d", ok, resp.StatusCode())
	}
	if rec.pauses != 1 || len(rec.scales) != 1 || rec.scales[0] != 0 {
		t.Fatalf("playback port calls: pauses=%d scales=%v", rec.pauses, rec.scales)
	}
}

func TestHandlePlaybackControl_NoSessionPauseFails(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	rec := &recordingPlaybackPort{failAll: true}
	h.acceptor.WithPlayback(rec)
	p := servingPlatform(t, h)

	body := `<Control><CmdType>PlaybackControl</CmdType><SN>1</SN><DeviceID>34020000011310000001</DeviceID><Cmd>Pause</Cmd></Control>`
	req := controlRequest(t, body)
	notify := mustNotify(t, model.CmdTypePlaybackControl, "34020000011310000001", 1)

	resp, ok := h.acceptor.handlePlaybackControl(context.Background(), p, req, notify)
	if !ok || resp.StatusCode() != 400 {
		t.Fatalf("want 400, got ok=%v status=%d", ok, resp.StatusCode())
	}
}

func TestHandleDeviceControl_UpdatesPTZState(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	p := servingPlatform(t, h)
	cmd, err := model.EncodePTZCmd("up", 128)
	if err != nil {
		t.Fatal(err)
	}
	body := `<?xml version="1.0"?><Control><CmdType>DeviceControl</CmdType><SN>1</SN><DeviceID>34020000011310000001</DeviceID><Cmd>` + cmd + `</Cmd></Control>`
	req := controlRequest(t, body)
	resp, ok := h.acceptor.handleDeviceControl(context.Background(), p, req, model.Notify{})
	if !ok || resp.StatusCode() != 200 {
		t.Fatalf("ok=%v status=%d", ok, resp.StatusCode())
	}
	st, found := h.acceptor.ControlState(h.nodeID)
	if !found || st.Tilt != 1 || st.Moving != "up" {
		t.Fatalf("state=%+v found=%v", st, found)
	}
}

func TestHandleDeviceControl_PresetAndRecordAndIFrame(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	p := servingPlatform(t, h)

	presetBody := `<Control><CmdType>DeviceControl</CmdType><SN>2</SN><DeviceID>34020000011310000001</DeviceID><PresetID>5</PresetID></Control>`
	req := controlRequest(t, presetBody)
	if _, ok := h.acceptor.handleDeviceControl(context.Background(), p, req, model.Notify{}); !ok {
		t.Fatal("preset")
	}
	st, _ := h.acceptor.ControlState(h.nodeID)
	if st.PresetID != 5 {
		t.Fatalf("preset=%d", st.PresetID)
	}

	recBody := `<Control><CmdType>DeviceControl</CmdType><SN>3</SN><DeviceID>34020000011310000001</DeviceID><RecordCmd>Record</RecordCmd></Control>`
	req = controlRequest(t, recBody)
	h.acceptor.handleDeviceControl(context.Background(), p, req, model.Notify{})
	st, _ = h.acceptor.ControlState(h.nodeID)
	if !st.Recording {
		t.Fatal("expected recording")
	}

	iframeBody := `<Control><CmdType>DeviceControl</CmdType><SN>4</SN><DeviceID>34020000011310000001</DeviceID><IFrameCmd>Send</IFrameCmd></Control>`
	req = controlRequest(t, iframeBody)
	h.acceptor.handleDeviceControl(context.Background(), p, req, model.Notify{})
	st, _ = h.acceptor.ControlState(h.nodeID)
	if st.IFrameCount != 1 {
		t.Fatalf("iframe=%d", st.IFrameCount)
	}
}

func TestHandleDeviceControl_DragZoomErrors(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	p := servingPlatform(t, h)
	body := `<Control><CmdType>DeviceControl</CmdType><SN>9</SN><DeviceID>34020000011310000001</DeviceID><DragZoomIn><Length>1</Length></DragZoomIn></Control>`
	req := controlRequest(t, body)
	resp, ok := h.acceptor.handleDeviceControl(context.Background(), p, req, model.Notify{})
	if !ok {
		t.Fatal("expected answer")
	}
	if !strings.Contains(resp.Body(), "ERROR") && resp.StatusCode() != 501 {
		t.Fatalf("want ERROR body or 501, got status=%d body=%q", resp.StatusCode(), resp.Body())
	}
}
