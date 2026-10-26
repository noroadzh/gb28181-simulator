package app

import (
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

func playbackInvite(t *testing.T, deviceID model.NodeID, callID, sn string) model.Message {
	t.Helper()
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)
	body, err := model.PlaybackSDPBytes(model.PlaybackSDP{
		DeviceID: "34020000001310000001001",
		SN:       sn,
		ConnIP:   "127.0.0.1",
		RTPPort:  40000,
		Start:    start,
		End:      start.Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("PlaybackSDPBytes: %v", err)
	}
	req, err := model.NewRequest("INVITE", "sip:"+deviceID.String()+"@3402000000", []model.Header{
		model.NewHeader("From", "<sip:platform@3402000000>;tag=p-play"),
		model.NewHeader("To", "<sip:"+deviceID.String()+"@3402000000>"),
		model.NewHeader("Call-ID", callID),
		model.NewHeader("CSeq", "1 INVITE"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-play"),
		model.NewHeader("Contact", "<sip:platform@127.0.0.1:15060>"),
		model.NewHeader("Content-Type", "application/sdp"),
	}, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return req
}

// Task 1.2: a device with a media source answers a playback INVITE with its
// own sending port and the same time window.
func TestDevice_AnswersPlaybackInvite(t *testing.T) {
	deviceID := mustDeviceNodeID(t, "34020000001310000006")
	mediaCfg := model.MediaConfig{Kind: model.SourceKindFile, Path: "clip.mp4"}
	h := newDeviceHarness(t, deviceID, nil, &mediaCfg)

	resp := h.tr.deliver(t, playbackInvite(t, deviceID, "play-call-1", "12345"))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200 (body %q)", resp.StatusCode(), resp.Body())
	}
	body := resp.Body()
	for _, want := range []string{"s=Playback", "u=34020000001310000001001:12345", "a=sendonly"} {
		if !strings.Contains(body, want) {
			t.Errorf("answer is missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "m=video 40000 ") {
		t.Errorf("answer echoed the offer's port instead of choosing one:\n%s", body)
	}
	if !strings.Contains(body, "t=2026-10-01T00:00:00 2026-10-01T00:05:00") {
		t.Errorf("answer changed the requested window:\n%s", body)
	}

	h.acceptor.deviceTalksMu.Lock()
	_, registered := h.acceptor.devicePlaybacks["play-call-1"]
	h.acceptor.deviceTalksMu.Unlock()
	if !registered {
		t.Error("the device did not register a playback port for the dialog")
	}
}

// Task 1.2: no media source means 404 — the device cannot produce that
// recording.
func TestDevice_PlaybackWithoutSourceIs404(t *testing.T) {
	deviceID := mustDeviceNodeID(t, "34020000001310000007")
	h := newDeviceHarness(t, deviceID, nil, nil)

	resp := h.tr.deliver(t, playbackInvite(t, deviceID, "play-call-2", "777"))
	if resp.StatusCode() != 404 {
		t.Fatalf("status = %d, want 404", resp.StatusCode())
	}
}
