package app

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/adapter/media"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
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

// Task 1.3: the device sends the recording to the endpoint the offer named,
// as PS over RTP, for the length of the requested window.
func TestDevice_PlaybackStreamsRTP(t *testing.T) {
	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()
	rtpPort := listener.LocalAddr().(*net.UDPAddr).Port

	deviceID := mustDeviceNodeID(t, "34020000001310000008")
	mediaCfg := model.MediaConfig{Kind: model.SourceKindSynthetic, FPS: 25}
	h := newDeviceHarness(t, deviceID, nil, &mediaCfg)
	ms := NewMediaService(
		func(cfg model.MediaConfig) port.MediaSource {
			if cfg.Kind == model.SourceKindSynthetic {
				return media.NewSyntheticSource(cfg)
			}
			return nil
		},
		func() port.PSPacketizer { return media.NewPSPacketizer(nil) },
		func(mtu int) port.RTPizer { return media.NewRTPizer(0, mtu, nil) },
		nil,
	)
	h.acceptor.WithMediaService(ms)
	t.Cleanup(func() { _ = ms.Close() })

	start := time.Now()
	body, err := model.PlaybackSDPBytes(model.PlaybackSDP{
		DeviceID: "34020000001310000001001",
		SN:       "55",
		ConnIP:   "127.0.0.1",
		RTPPort:  rtpPort,
		Start:    start,
		End:      start.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("PlaybackSDPBytes: %v", err)
	}
	invite, err := model.NewRequest("INVITE", "sip:"+deviceID.String()+"@3402000000", []model.Header{
		model.NewHeader("From", "<sip:platform@3402000000>;tag=p-play"),
		model.NewHeader("To", "<sip:"+deviceID.String()+"@3402000000>"),
		model.NewHeader("Call-ID", "play-stream-1"),
		model.NewHeader("CSeq", "1 INVITE"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-play"),
		model.NewHeader("Content-Type", "application/sdp"),
	}, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if resp := h.tr.deliver(t, invite); resp.StatusCode() != 200 {
		t.Fatalf("INVITE status = %d, want 200", resp.StatusCode())
	}

	buf := make([]byte, 2048)
	_ = listener.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, _, err := listener.ReadFrom(buf)
	if err != nil {
		t.Fatalf("no RTP reached the platform endpoint: %v", err)
	}
	if n < 12 {
		t.Fatalf("RTP packet is %d bytes, want a 12-byte header plus payload", n)
	}
	if buf[0] != 0x80 {
		t.Errorf("first byte = 0x%02X, want 0x80 (RTP v2)", buf[0])
	}
	if !bytes.HasPrefix(buf[12:n], []byte{0x00, 0x00, 0x01, 0xBA}) {
		t.Errorf("payload does not start with a PS pack header: % X", buf[12:min(20, n)])
	}
}

// Task 3.2: a playback offer over TCP is served over TCP — the answer
// mirrors the transport and the device connects to the listening platform.
func TestDevice_AnswersTCPPlaybackInvite(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	port := ln.Addr().(*net.TCPAddr).Port

	// A live window: the stream stops when the offer's end passes.
	start := time.Now()
	body, err := model.PlaybackSDPBytes(model.PlaybackSDP{
		DeviceID: "34020000001310000001001", SN: "40001", ConnIP: "127.0.0.1",
		RTPPort: port, Start: start, End: start.Add(time.Minute),
		Proto: "TCP/RTP/AVP", Setup: "passive",
	})
	if err != nil {
		t.Fatalf("PlaybackSDPBytes: %v", err)
	}
	deviceID := mustDeviceNodeID(t, "34020000001310000012")
	mediaCfg := model.MediaConfig{Kind: model.SourceKindSynthetic, FPS: 25}
	h := newDeviceHarness(t, deviceID, nil, &mediaCfg)
	h.acceptor.WithMediaService(syntheticMediaService(t))

	req, err := model.NewRequest("INVITE", "sip:"+deviceID.String()+"@3402000000", []model.Header{
		model.NewHeader("From", "<sip:platform@3402000000>;tag=p-play-tcp"),
		model.NewHeader("To", "<sip:"+deviceID.String()+"@3402000000>"),
		model.NewHeader("Call-ID", "play-tcp-1"),
		model.NewHeader("CSeq", "1 INVITE"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-play-tcp"),
		model.NewHeader("Contact", "<sip:platform@127.0.0.1:15060>"),
		model.NewHeader("Content-Type", "application/sdp"),
	}, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp := h.tr.deliver(t, req)
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200 (body %q)", resp.StatusCode(), resp.Body())
	}
	for _, want := range []string{"s=Playback", "TCP/RTP/AVP", "a=setup:active", "a=sendonly"} {
		if !strings.Contains(resp.Body(), want) {
			t.Errorf("answer is missing %q:\n%s", want, resp.Body())
		}
	}

	_ = ln.(*net.TCPListener).SetDeadline(time.Now().Add(3 * time.Second))
	conn, err := ln.Accept()
	if err != nil {
		t.Fatalf("the device never connected: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var prefix [2]byte
	if _, err := io.ReadFull(conn, prefix[:]); err != nil {
		t.Fatalf("read length prefix: %v", err)
	}
	if n := binary.BigEndian.Uint16(prefix[:]); n < 12 {
		t.Fatalf("length prefix = %d, not a plausible RTP packet", n)
	}
}
