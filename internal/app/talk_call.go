package app

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// talkTimeout bounds how long TalkStart waits for the device's answer.
const talkTimeout = 5 * time.Second

// talkCall is one active intercom dialog: the SIP transaction identity, where
// the device is, and the local RTP socket the device's audio arrives on.
type talkCall struct {
	id      string
	node    model.NodeID
	channel string
	target  string
	addr    string
	rtp     net.PacketConn
}

// talkSessionState is the per-service bookkeeping for talk. It is a separate
// lock from the node registry so a slow INVITE never blocks node operations.
type talkSessionState struct {
	mu    sync.Mutex
	calls map[string]*talkCall
}

// buildTalkOffer renders the SDP offer for a PCMU talk stream. The body is
// fixed (one audio m-line, payload type 0), so it is written here rather than
// round-tripped through the SDP codec — that adapter can only replay text it
// parsed, it cannot serialise a session built from scratch.
func buildTalkOffer(localIP string, rtpPort int) string {
	return fmt.Sprintf(
		"v=0\r\no=- %s 0 0 IN IP4 %s\r\ns=Talk\r\nc=IN IP4 %s\r\nt=0 0\r\n"+
			"m=audio %d RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\na=sendrecv\r\n",
		localIP, localIP, localIP, rtpPort)
}

// TalkStart opens a voice-intercom dialog with the device that owns the
// channel: an INVITE offering PCMU audio, answered with 2xx. The returned
// wsURL is where the browser streams microphone audio for this session.
//
// Only device nodes are talk targets; a platform node has no microphone to
// open.
func (s *NodeService) TalkStart(ctx context.Context, id model.NodeID, channelID string) (string, string, error) {
	node, ok := s.registry.Get(ctx, id)
	if !ok {
		return "", "", model.ErrUnknownNode
	}
	if node.ID().Kind() != model.NodeKindDevice {
		return "", "", fmt.Errorf("%w: node %s is not a device", port.ErrTalkUnsupported, id)
	}
	if s.acceptor == nil {
		return "", "", fmt.Errorf("%w: talk is not wired", port.ErrTalkUnsupported)
	}
	target, addr, err := s.controlTarget(ctx, node, channelID)
	if err != nil {
		return "", "", err
	}

	// The platform receives the device's audio on this socket; its port goes
	// into the SDP offer so the device knows where to send RTP.
	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		return "", "", fmt.Errorf("app: allocate talk RTP port: %w", err)
	}
	localPort := conn.LocalAddr().(*net.UDPAddr).Port

	localIP := node.Profile().Addr()
	if host, _, splitErr := net.SplitHostPort(localIP); splitErr == nil {
		localIP = host
	}
	body := buildTalkOffer(localIP, localPort)

	callID, err := s.acceptor.Invite(ctx, id, target, addr, body)
	if err != nil {
		_ = conn.Close()
		return "", "", err
	}

	call := &talkCall{id: callID, node: id, channel: channelID, target: target, addr: addr, rtp: conn}
	s.talk.mu.Lock()
	if s.talk.calls == nil {
		s.talk.calls = map[string]*talkCall{}
	}
	s.talk.calls[callID] = call
	s.talk.mu.Unlock()

	wsURL := fmt.Sprintf("/v1/nodes/%s/channels/%s/talk/audio?session=%s", id.String(), channelID, callID)
	s.log.Info("talk started", "node_id", id.String(), "channel", channelID, "session", callID)
	return callID, wsURL, nil
}

// TalkStop ends a talk session. Stopping an unknown session is a no-op, so a
// retried stop after a lost response is harmless.
func (s *NodeService) TalkStop(ctx context.Context, id model.NodeID, channelID, sessionID string) error {
	s.talk.mu.Lock()
	call := s.talk.calls[sessionID]
	delete(s.talk.calls, sessionID)
	s.talk.mu.Unlock()
	if call == nil {
		return nil
	}
	if call.rtp != nil {
		_ = call.rtp.Close()
	}
	if s.acceptor == nil {
		return nil
	}
	if err := s.acceptor.Bye(ctx, id, call.target, call.addr, call.id); err != nil {
		// The local session is already gone; report the wire failure so the
		// caller knows the device may still believe the dialog is alive.
		return err
	}
	s.log.Info("talk stopped", "node_id", id.String(), "channel", channelID, "session", sessionID)
	return nil
}
