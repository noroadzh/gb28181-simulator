package app

import (
	"bytes"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/adapter/media"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

const videoChannelID = "34020000001310000001001"

func videoOffer(t *testing.T, rtpPort int) string {
	t.Helper()
	return fmt.Sprintf("v=0\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=video %d RTP/AVP 96\r\na=recvonly\r\na=rtpmap:96 PS/90000\r\n", rtpPort)
}

func videoInvite(t *testing.T, deviceID model.NodeID, callID, body string) model.Message {
	t.Helper()
	req, err := model.NewRequest("INVITE", "sip:"+videoChannelID+"@3402000000", []model.Header{
		model.NewHeader("From", "<sip:34020000002000000001@3402000000>;tag=platform"),
		model.NewHeader("To", "<sip:"+videoChannelID+"@3402000000>"),
		model.NewHeader("Call-ID", callID),
		model.NewHeader("CSeq", "1 INVITE"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-video"),
		model.NewHeader("Contact", "<sip:34020000002000000001@127.0.0.1:15060>"),
		model.NewHeader("Content-Type", "application/sdp"),
	}, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return req
}

func syntheticMediaService(t *testing.T) *MediaService {
	t.Helper()
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
	t.Cleanup(func() { _ = ms.Close() })
	return ms
}

// Task 1.1: the offer parser reads the platform's endpoint and refuses
// anything that does not name one.
func TestParseVideoOffer(t *testing.T) {
	host, port, err := parseVideoOffer(videoOffer(t, 40002))
	if err != nil || host != "127.0.0.1" || port != 40002 {
		t.Fatalf("got (%q,%d,%v), want (127.0.0.1,40002,nil)", host, port, err)
	}
	for name, body := range map[string]string{
		"no c=":      "v=0\r\nm=video 40002 RTP/AVP 96\r\n",
		"no m=video": "v=0\r\nc=IN IP4 127.0.0.1\r\nm=audio 40002 RTP/AVP 0\r\n",
		"port zero":  "v=0\r\nc=IN IP4 127.0.0.1\r\nm=video 0 RTP/AVP 96\r\n",
	} {
		if _, _, err := parseVideoOffer(body); err == nil {
			t.Errorf("%s: accepted without an endpoint", name)
		}
	}
}

// Task 1.2 + 1.3: a live video INVITE is answered (not 488), and the device
// streams PS/RTP to the endpoint the offer named.
func TestDevice_AnswersVideoInviteAndStreams(t *testing.T) {
	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()
	rtpPort := listener.LocalAddr().(*net.UDPAddr).Port

	deviceID := mustDeviceNodeID(t, "34020000001310000009")
	mediaCfg := model.MediaConfig{Kind: model.SourceKindSynthetic, FPS: 25}
	h := newDeviceHarness(t, deviceID, nil, &mediaCfg)
	h.acceptor.WithMediaService(syntheticMediaService(t))

	resp := h.tr.deliver(t, videoInvite(t, deviceID, "video-call-1", videoOffer(t, rtpPort)))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200 (body %q)", resp.StatusCode(), resp.Body())
	}
	for _, want := range []string{"m=video ", "a=sendonly", "PS/90000"} {
		if !strings.Contains(resp.Body(), want) {
			t.Errorf("answer is missing %q:\n%s", want, resp.Body())
		}
	}

	buf := make([]byte, 2048)
	_ = listener.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, _, err := listener.ReadFrom(buf)
	if err != nil {
		t.Fatalf("no RTP reached the platform: %v", err)
	}
	if n < 12 || buf[0] != 0x80 {
		t.Fatalf("not an RTP v2 packet (n=%d): % X", n, buf[:min(12, n)])
	}
	if !bytes.HasPrefix(buf[12:n], []byte{0x00, 0x00, 0x01, 0xBA}) {
		t.Errorf("payload is not PS: % X", buf[12:min(20, n)])
	}

	// Task 1.3: BYE releases the port.
	bye, err := model.NewRequest("BYE", "sip:"+videoChannelID+"@3402000000", []model.Header{
		model.NewHeader("From", "<sip:34020000002000000001@3402000000>;tag=platform"),
		model.NewHeader("To", "<sip:"+videoChannelID+"@3402000000>"),
		model.NewHeader("Call-ID", "video-call-1"),
		model.NewHeader("CSeq", "2 BYE"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-bye"),
	}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	h.tr.inbound <- bye
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.acceptor.deviceTalksMu.Lock()
		_, still := h.acceptor.deviceVideos["video-call-1"]
		h.acceptor.deviceTalksMu.Unlock()
		if !still {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the BYE left the video port open")
}

// Task 1.2: a channel without a source is 404, not a silent 200.
func TestDevice_VideoInviteWithoutSourceIs404(t *testing.T) {
	deviceID := mustDeviceNodeID(t, "34020000001310000010")
	h := newDeviceHarness(t, deviceID, nil, nil)

	resp := h.tr.deliver(t, videoInvite(t, deviceID, "video-call-2", videoOffer(t, 40010)))
	if resp.StatusCode() != 404 {
		t.Fatalf("status = %d, want 404", resp.StatusCode())
	}
}
