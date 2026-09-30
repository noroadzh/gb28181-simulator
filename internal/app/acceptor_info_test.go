package app

import (
	"context"
	"sync"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// infoRequest builds an INFO request with the given Content-Type and body.
func infoRequest(t *testing.T, deviceID, callID, contentType, body string) model.Message {
	t.Helper()
	hdrs := []model.Header{
		model.NewHeader("From", "<sip:"+deviceID+"@3402000000>;tag=info1"),
		model.NewHeader("To", "<sip:34020000002000000001@3402000000>"),
		model.NewHeader("Call-ID", callID),
		model.NewHeader("CSeq", "1 INFO"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-info"),
	}
	if contentType != "" {
		hdrs = append(hdrs, model.NewHeader("Content-Type", contentType))
	}
	msg, err := model.NewRequest("INFO", "sip:34020000002000000001@3402000000", hdrs, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return msg
}

// recordingPlayback captures every Play call so tests can assert on it.
// Guarded by mu so it is safe under -race (acceptor writes, test reads).
type recordingPlayback struct {
	mu    sync.Mutex
	plays []playbackCall
}

type playbackCall struct {
	deviceID  string
	channelID string
	start     string
	end       string
	scale     float64
}

func (r *recordingPlayback) Play(_ context.Context, deviceID, channelID, start, end string, scale float64) (string, error) {
	r.mu.Lock()
	r.plays = append(r.plays, playbackCall{deviceID, channelID, start, end, scale})
	r.mu.Unlock()
	return "sess-1", nil
}
func (r *recordingPlayback) Stop(_ context.Context, _ string) error { return nil }
func (r *recordingPlayback) Query(_ context.Context, _ string) (port.PlaybackState, error) {
	return port.PlaybackState{}, nil
}
func (r *recordingPlayback) SetScale(_ context.Context, _ string, scale float64) error {
	r.mu.Lock()
	r.plays = append(r.plays, playbackCall{scale: scale})
	r.mu.Unlock()
	return nil
}

// snapshot returns a thread-safe copy of the recorded calls.
func (r *recordingPlayback) snapshot() []playbackCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]playbackCall, len(r.plays))
	copy(cp, r.plays)
	return cp
}

// TestAcceptor_InfoMANSRTSPPlayAnswers200 verifies that a MANSRTSP PLAY body
// gets routed to the PlaybackPort and answered 200 OK (task 7.4).
func TestAcceptor_InfoMANSRTSPPlayAnswers200(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	rec := &recordingPlayback{}
	h.acceptor.WithPlayback(rec)

	body := "PLAY RTSP/1.0\r\nCSeq: 1\r\nScale: 2.0\r\nRange: npt=3600-\r\n\r\n"
	resp := h.tr.deliver(t, infoRequest(t, "34020000011310000003", "info-play", "Application/MANSRTSP", body))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	if len(rec.snapshot()) != 1 {
		t.Fatalf("PlaybackPort.Play called %d times, want 1", len(rec.snapshot()))
	}
	if rec.snapshot()[0].scale != 2.0 {
		t.Errorf("scale = %v, want 2.0", rec.snapshot()[0].scale)
	}
}

// TestAcceptor_InfoMANSRTSPPauseAnswers200 verifies that PAUSE answers 200
// without crashing (task 7.4).
func TestAcceptor_InfoMANSRTSPPauseAnswers200(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	h.acceptor.WithPlayback(&recordingPlayback{})

	body := "PAUSE RTSP/1.0\r\nCSeq: 2\r\n\r\n"
	resp := h.tr.deliver(t, infoRequest(t, "34020000011310000003", "info-pause", "Application/MANSRTSP", body))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
}

// TestAcceptor_InfoUnknownBodyAnswers200 verifies the default INFO path
// returns 200 OK even when no MANSRTSP / MANSCDP is detected (task 7.3).
func TestAcceptor_InfoUnknownBodyAnswers200(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))

	resp := h.tr.deliver(t, infoRequest(t, "34020000011310000003", "info-unknown", "text/plain", "hello"))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
}

// TestAcceptor_InfoMANSCDPMediaStatusAnswers200 verifies that a
// MANSCDP-typed INFO carrying a MediaStatus notify routes to the
// MediaStatusPort and answers 200 OK (task 7.3).
func TestAcceptor_InfoMANSCDPMediaStatusAnswers200(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	called := false
	h.acceptor.WithMediaStatus(&fakeMediaStatusPort{
		handle: func(ctx context.Context, r model.MediaStatusReport) error {
			called = true
			return nil
		},
	})

	body := `<Notify>
  <CmdType>MediaStatus</CmdType>
  <SN>1</SN>
  <DeviceID>34020000011310000003</DeviceID>
  <Status>OK</Status>
</Notify>`
	resp := h.tr.deliver(t, infoRequest(t, "34020000011310000003", "info-ms", "Application/MANSCDP+XML", body))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	if !called {
		t.Fatal("MediaStatusPort was not called")
	}
}

// TestAcceptor_InfoPlaybackUnknownWithoutPortAnswers200 verifies that an
// INFO with a MANSRTSP body still answers 200 when no playback port is
// attached — i.e. the default branch handles it gracefully (task 7.3).
func TestAcceptor_InfoPlaybackUnknownWithoutPortAnswers200(t *testing.T) {
	h := newMessageHarness(t, defaultHarnessPolicy(t))
	// Note: no WithPlayback call.

	body := "PLAY RTSP/1.0\r\nCSeq: 1\r\n\r\n"
	resp := h.tr.deliver(t, infoRequest(t, "34020000011310000003", "info-no-port", "Application/MANSRTSP", body))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
}
