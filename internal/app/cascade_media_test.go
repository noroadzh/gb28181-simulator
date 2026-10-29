package app

// cascade-media-relay: the platform bridges an upstream live INVITE to the
// downstream device that owns the channel.

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

const (
	mediaRelayChannel    = "34020000001310000001001"
	mediaRelayDeviceID   = "34020000001310000002"
	mediaRelayDeviceAddr = "127.0.0.1:15060"
)

// relayHarness is a platform that can relay: a device in its online table, one
// channel owned by it, and media wiring.
type mediaRelayHarness struct {
	*liveHarness
	channelID string
}

func newMediaRelayHarness(t *testing.T) *mediaRelayHarness {
	t.Helper()
	l := newLiveHarness(t)
	l.registerDevice(t, mediaRelayDeviceID, mediaRelayDeviceAddr)

	ch, err := model.NewChannel(mediaRelayChannel, "lobby", mediaRelayDeviceID, model.ChannelStatusOnline)
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}
	if _, err := l.reg.MutateProfile(context.Background(), l.h.nodeID, func(p model.NodeProfile) (model.NodeProfile, error) {
		return p.WithChannels([]model.Channel{ch})
	}); err != nil {
		t.Fatalf("MutateProfile: %v", err)
	}
	return &mediaRelayHarness{liveHarness: l, channelID: ch.ID()}
}

// upstreamInvite builds the peer's live-view INVITE for the relay channel.
func upstreamInvite(t *testing.T, callID string, rtpPort int) model.Message {
	t.Helper()
	body := fmt.Sprintf("v=0\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\n"+
		"m=video %d RTP/AVP 96\r\na=recvonly\r\na=rtpmap:96 PS/90000\r\n", rtpPort)
	req, err := model.NewRequest("INVITE", "sip:"+mediaRelayChannel+"@3402000000", []model.Header{
		model.NewHeader("From", "<sip:34020000002000000009@3402000000>;tag=up"),
		model.NewHeader("To", "<sip:"+mediaRelayChannel+"@3402000000>"),
		model.NewHeader("Call-ID", callID),
		model.NewHeader("CSeq", "1 INVITE"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15070;branch=z9hG4bK-relay"),
		model.NewHeader("Contact", "<sip:34020000002000000009@127.0.0.1:15070>"),
		model.NewHeader("Content-Type", "application/sdp"),
	}, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return req
}

// answerDownstream answers the platform's own INVITE to the device with the
// device's RTP endpoint and SSRC.
func (h *mediaRelayHarness) answerDownstream(t *testing.T, invite model.Message, ssrc uint32) {
	t.Helper()
	body := fmt.Sprintf("v=0\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\n"+
		"m=video 40002 RTP/AVP 96\r\na=sendonly\r\ny=%010d\r\n", ssrc)
	resp, err := model.NewResponse(200, "OK", []model.Header{
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-live"),
		model.NewHeader("Call-ID", headerValue(invite, "Call-ID")),
		model.NewHeader("CSeq", headerValue(invite, "CSeq")),
		model.NewHeader("Content-Type", "application/sdp"),
	}, body)
	if err != nil {
		t.Fatalf("NewResponse: %v", err)
	}
	h.h.tr.inbound <- resp
}

func upstreamListener(t *testing.T) (net.PacketConn, int) {
	t.Helper()
	ln, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln, ln.LocalAddr().(*net.UDPAddr).Port
}

// relayedInvite drives one relay: the peer INVITEs, the platform INVITEs the
// device, the device answers, the peer gets its answer.
func (h *mediaRelayHarness) relayedInvite(t *testing.T, callID string, upstreamPort int, ssrc uint32) model.Message {
	t.Helper()
	done := make(chan model.Message, 1)
	go func() {
		done <- h.h.tr.deliver(t, upstreamInvite(t, callID, upstreamPort))
	}()
	downstream := waitForSend(t, h.h.tr)
	if downstream.Method() != "INVITE" {
		t.Fatalf("the platform sent %q, want its own INVITE to the device", downstream.Method())
	}
	if got := headerValue(downstream, "To"); !strings.Contains(got, mediaRelayChannel) {
		t.Errorf("downstream To = %q, want it addressed to the channel", got)
	}
	h.answerDownstream(t, downstream, ssrc)
	select {
	case resp := <-done:
		return resp
	case <-time.After(3 * time.Second):
		t.Fatal("the upstream INVITE never got an answer")
		return model.Message{}
	}
}

// downstreamPort reports the local port of the relay's own pull, which is
// where the device is expected to send RTP.
func (h *mediaRelayHarness) downstreamPort(t *testing.T, relayID string) int {
	t.Helper()
	sess := h.svc.liveSession(liveKey{h.h.nodeID.String(), relayChannelKey(mediaRelayChannel, relayID)})
	if sess == nil {
		t.Fatal("no downstream session for the relay")
	}
	return sess.info.LocalPort
}

func TestRelay_ForwardsDownstreamStreamUpstream(t *testing.T) {
	h := newMediaRelayHarness(t)
	listener, upstreamPort := upstreamListener(t)

	resp := h.relayedInvite(t, "relay-call-1", upstreamPort, 1234)
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200 (body %q)", resp.StatusCode(), resp.Body())
	}
	for _, want := range []string{"m=video ", "a=sendonly", "PS/90000"} {
		if !strings.Contains(resp.Body(), want) {
			t.Errorf("answer is missing %q:\n%s", want, resp.Body())
		}
	}

	// The device sends PS over RTP to the port the platform named in its
	// own offer; the peer must then receive it.
	deviceAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", h.downstreamPort(t, "relay-call-1")))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	dev, err := net.DialUDP("udp", nil, deviceAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = dev.Close() }()

	frame := []byte{0x00, 0x00, 0x01, 0xBA, 0x44, 0x55}
	pkt := model.RTPPacket{Sequence: 1, SSRC: 1234, PayloadType: 96, Marker: true, Timestamp: 90000, Payload: frame}
	if _, err := dev.Write(pkt.Bytes()); err != nil {
		t.Fatalf("device write: %v", err)
	}

	buf := make([]byte, 2048)
	_ = listener.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, _, err := listener.ReadFrom(buf)
	if err != nil {
		t.Fatalf("nothing reached the peer: %v", err)
	}
	if n < 12+len(frame) || buf[0] != 0x80 {
		t.Fatalf("not an RTP v2 packet: % X", buf[:n])
	}
	if !bytes.HasPrefix(buf[12:n], frame) {
		t.Errorf("payload = % X, want the device's PS frame", buf[12:n])
	}
}

func TestRelay_UpstreamByeEndsBothLegs(t *testing.T) {
	h := newMediaRelayHarness(t)
	_, upstreamPort := upstreamListener(t)

	if resp := h.relayedInvite(t, "relay-call-2", upstreamPort, 1234); resp.StatusCode() != 200 {
		t.Fatalf("status = %d", resp.StatusCode())
	}
	before := len(h.h.tr.delivered())

	bye, err := model.NewRequest("BYE", "sip:"+h.h.nodeID.String()+"@3402000000", []model.Header{
		model.NewHeader("From", "<sip:34020000002000000009@3402000000>;tag=up"),
		model.NewHeader("To", "<sip:"+h.h.nodeID.String()+"@3402000000>"),
		model.NewHeader("Call-ID", "relay-call-2"),
		model.NewHeader("CSeq", "2 BYE"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15070;branch=z9hG4bK-bye"),
	}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if resp := h.h.tr.deliver(t, bye); resp.StatusCode() != 200 {
		t.Fatalf("BYE status = %d, want 200", resp.StatusCode())
	}

	sent := h.h.tr.delivered()[before:]
	var downstreamBye bool
	for _, msg := range sent {
		if msg.Method() == "BYE" {
			downstreamBye = true
		}
	}
	if !downstreamBye {
		t.Errorf("no BYE was sent to the device; sent %d messages", len(sent))
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.svc.liveSession(liveKey{h.h.nodeID.String(), relayChannelKey(mediaRelayChannel, "relay-call-2")}) == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the relay's downstream session survived the upstream BYE")
}

func TestRelay_DownstreamByeEndsUpstream(t *testing.T) {
	h := newMediaRelayHarness(t)
	_, upstreamPort := upstreamListener(t)

	if resp := h.relayedInvite(t, "relay-call-3", upstreamPort, 1234); resp.StatusCode() != 200 {
		t.Fatalf("status = %d", resp.StatusCode())
	}
	// The device hangs up its own dialog with the platform.
	var downstreamCallID string
	for _, msg := range h.h.tr.delivered() {
		if msg.Method() == "INVITE" {
			downstreamCallID = headerValue(msg, "Call-ID")
		}
	}
	if downstreamCallID == "" {
		t.Fatal("no downstream INVITE was sent")
	}
	h.svc.onPeerLiveBye(h.h.nodeID, downstreamCallID)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, msg := range h.h.tr.delivered() {
			if msg.Method() == "BYE" && headerValue(msg, "Call-ID") == "relay-call-3" {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the device's BYE did not end the upstream dialog")
}

// A channel with no resolvable downstream keeps the generic answer: relay
// must not change what unrelated INVITEs do.
func TestRelay_UnroutableChannelKeepsGenericAnswer(t *testing.T) {
	h := newLiveHarness(t) // no devices, no channels
	listener, port := upstreamListener(t)
	_ = listener

	resp := h.h.tr.deliver(t, upstreamInvite(t, "relay-call-4", port))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want the generic 200", resp.StatusCode())
	}
	h.h.tr.mu.Lock()
	sent := append([]model.Message(nil), h.h.tr.sent...)
	h.h.tr.mu.Unlock()
	for _, msg := range sent {
		if msg.Method() == "INVITE" {
			t.Errorf("an unroutable channel still triggered a downstream INVITE:\n%s", msg)
		}
	}
}

// Task 3.3: two peers pulling the same channel get independent relays; one
// hanging up leaves the other's stream running.
func TestRelay_TwoPeersAreIndependent(t *testing.T) {
	h := newMediaRelayHarness(t)
	first, firstPort := upstreamListener(t)
	second, secondPort := upstreamListener(t)

	if resp := h.relayedInvite(t, "relay-a", firstPort, 1234); resp.StatusCode() != 200 {
		t.Fatalf("first relay = %d", resp.StatusCode())
	}

	// The second peer's INVITE travels on the same socket; answer the new
	// downstream INVITE it produces.
	h.h.tr.mu.Lock()
	before := len(h.h.tr.sent)
	h.h.tr.mu.Unlock()
	done := make(chan model.Message, 1)
	go func() {
		done <- h.h.tr.deliver(t, upstreamInvite(t, "relay-b", secondPort))
	}()
	var downstream model.Message
	deadline := time.Now().Add(2 * time.Second)
	for downstream.Method() == "" && time.Now().Before(deadline) {
		h.h.tr.mu.Lock()
		if len(h.h.tr.sent) > before {
			for _, msg := range h.h.tr.sent[before:] {
				if msg.Method() == "INVITE" {
					downstream = msg
				}
			}
		}
		h.h.tr.mu.Unlock()
		if downstream.Method() == "" {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if downstream.Method() == "" {
		t.Fatal("the second peer produced no downstream INVITE")
	}
	h.answerDownstream(t, downstream, 1234)
	select {
	case resp := <-done:
		if resp.StatusCode() != 200 {
			t.Fatalf("second relay = %d", resp.StatusCode())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the second relay never answered")
	}

	// One RTP packet per peer reaches its own listener.
	for i, dst := range []struct {
		ln   net.PacketConn
		port int
	}{{first, firstPort}, {second, secondPort}} {
		_ = dst
		_ = i
	}
	for _, peer := range []struct {
		name string
		ln   net.PacketConn
	}{{"a", first}, {"b", second}} {
		id := "relay-" + peer.name
		addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", h.downstreamPort(t, id)))
		if err != nil {
			t.Fatalf("resolve %s: %v", id, err)
		}
		conn, err := net.DialUDP("udp", nil, addr)
		if err != nil {
			t.Fatalf("dial %s: %v", id, err)
		}
		pkt := model.RTPPacket{Sequence: 1, SSRC: 1234, PayloadType: 96, Marker: true,
			Timestamp: 90000, Payload: []byte{0x00, 0x00, 0x01, 0xBA, 0x01}}
		if _, err := conn.Write(pkt.Bytes()); err != nil {
			t.Fatalf("device write %s: %v", id, err)
		}
		_ = conn.Close()
		buf := make([]byte, 2048)
		_ = peer.ln.SetReadDeadline(time.Now().Add(3 * time.Second))
		if _, _, err := peer.ln.ReadFrom(buf); err != nil {
			t.Fatalf("peer %s received nothing: %v", peer.name, err)
		}
	}

	// Stopping the first peer leaves the second alone.
	if err := h.svc.StopLiveRelay(context.Background(), h.h.nodeID, mediaRelayChannel, "relay-a"); err != nil {
		t.Fatalf("StopLiveRelay: %v", err)
	}
	if h.svc.liveSession(liveKey{h.h.nodeID.String(), relayChannelKey(mediaRelayChannel, "relay-b")}) == nil {
		t.Error("stopping one peer also ended the other")
	}
}

// Task 3.1: a downstream refusal maps to 480 and leaves no session behind.
func TestRelay_RefusalMapsTo480(t *testing.T) {
	h := newMediaRelayHarness(t)
	_, port := upstreamListener(t)

	done := make(chan model.Message, 1)
	go func() {
		done <- h.h.tr.deliver(t, upstreamInvite(t, "relay-refused", port))
	}()
	downstream := waitForSend(t, h.h.tr)
	refusal, err := model.NewResponse(404, "Not Found", []model.Header{
		model.NewHeader("Call-ID", headerValue(downstream, "Call-ID")),
		model.NewHeader("CSeq", headerValue(downstream, "CSeq")),
	}, "")
	if err != nil {
		t.Fatalf("NewResponse: %v", err)
	}
	h.h.tr.inbound <- refusal

	select {
	case resp := <-done:
		if resp.StatusCode() != 480 {
			t.Fatalf("status = %d, want 480", resp.StatusCode())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no answer to the upstream INVITE")
	}
	if h.svc.liveSession(liveKey{h.h.nodeID.String(), relayChannelKey(mediaRelayChannel, "relay-refused")}) != nil {
		t.Error("a refused relay left a downstream session")
	}
}

// Task 4.1: the relay's stream is the same bus the FLV gateway and the
// recorder read, so it is previewable and recordable without a second pull.
func TestRelay_StreamFeedsBus(t *testing.T) {
	h := newMediaRelayHarness(t)
	_, port := upstreamListener(t)
	if resp := h.relayedInvite(t, "relay-bus", port, 1234); resp.StatusCode() != 200 {
		t.Fatalf("relay = %d", resp.StatusCode())
	}

	frames, cleanup, ok := h.svc.RelayPS(context.Background(), h.h.nodeID, mediaRelayChannel, "relay-bus")
	if !ok {
		t.Fatal("the relay exposed no PS stream")
	}
	defer cleanup()

	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", h.downstreamPort(t, "relay-bus")))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	payload := []byte{0x00, 0x00, 0x01, 0xBA, 0x99, 0xAA}
	pkt := model.RTPPacket{Sequence: 1, SSRC: 1234, PayloadType: 96, Marker: true, Timestamp: 90000, Payload: payload}
	if _, err := conn.Write(pkt.Bytes()); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case ps := <-frames:
		if !bytes.Equal(ps.Payload, payload) {
			t.Errorf("PS payload = % X, want % X", ps.Payload, payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no PS frame reached the relay's consumer")
	}
}
