// End-to-end coverage for cascade media relay.
//
// Change: cascade-media-relay. The chain: an upstream peer INVITEs the
// platform for a channel that belongs to a downstream device; the platform
// pulls that channel itself and forwards the PS/RTP upstream. A 200 alone
// would not prove the bridge, so the assertion is RTP arriving at the peer.

package e2e

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// liveInviteRequest is one upstream live-view INVITE in wire form.
func liveInviteRequest(channelID, callID, localAddr string, rtpPort int) string {
	body := fmt.Sprintf("v=0\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\n"+
		"m=video %d RTP/AVP 96\r\na=recvonly\r\na=rtpmap:96 PS/90000\r\n", rtpPort)
	lines := []string{
		"INVITE sip:" + channelID + "@3402000000 SIP/2.0",
		"Via: SIP/2.0/UDP " + localAddr + ";branch=z9hG4bK-e2e-media",
		"From: <sip:34020000002000000009@127.0.0.1>;tag=e2e-media",
		"To: <sip:" + channelID + "@3402000000>",
		"Call-ID: " + callID,
		"CSeq: 1 INVITE",
		"Max-Forwards: 70",
		"Contact: <sip:34020000002000000009@" + localAddr + ">",
		"Content-Type: application/sdp",
		fmt.Sprintf("Content-Length: %d", len(body)),
		"",
		body,
	}
	return strings.Join(lines, "\r\n")
}

// The platform already has one registered device (startLivePlatform), so the
// channel resolves to it and the INVITE is relayed rather than answered
// generically.
func TestCascadeMediaRelay_UpstreamPullsDeviceStream(t *testing.T) {
	_, channelID, platformAddr := startLivePlatform(t)

	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()
	local := listener.LocalAddr().String()
	rtpPort := listener.LocalAddr().(*net.UDPAddr).Port

	dst, err := net.ResolveUDPAddr("udp", platformAddr)
	if err != nil {
		t.Fatalf("resolve platform: %v", err)
	}
	if _, err := listener.WriteTo([]byte(liveInviteRequest(channelID, "e2e-media-relay-1", local, rtpPort)), dst); err != nil {
		t.Fatalf("send INVITE: %v", err)
	}

	buf := make([]byte, 64*1024)
	var answer string
	deadline := time.Now().Add(20 * time.Second)
	for answer == "" {
		_ = listener.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _, rerr := listener.ReadFrom(buf)
		if rerr != nil {
			if time.Now().After(deadline) {
				t.Fatalf("the relay never answered: %v", rerr)
			}
			continue
		}
		if n >= 12 && buf[0] == 0x80 {
			// Media before the answer: the bridge is already working.
			return
		}
		answer = string(buf[:n])
	}
	if !strings.HasPrefix(answer, "SIP/2.0 200") {
		t.Fatalf("relay answer was not 200:\n%s", answer)
	}
	if !strings.Contains(answer, "a=sendonly") || !strings.Contains(answer, "m=video ") {
		t.Fatalf("relay answer carries no sending media section:\n%s", answer)
	}

	// Media must follow the answer.
	_ = listener.SetReadDeadline(time.Now().Add(20 * time.Second))
	for {
		n, _, rerr := listener.ReadFrom(buf)
		if rerr != nil {
			t.Fatalf("no RTP arrived upstream: %v", rerr)
		}
		if n >= 13 && buf[0] == 0x80 {
			return
		}
	}
}
