// Package app — Talk (voice intercom) acceptor.
//
// The TalkAcceptor is the platform-side endpoint for GB28181 audio intercom
// dialogs. It listens on an independent UDP port (the "talk port" the Web UI
// shows the operator), accepts the SIP INVITE a downstream camera sends when
// the operator presses the talk button, allocates a local RTP receiver port,
// and answers with 200 OK. The Web UI then opens a WebSocket, captures audio
// from getUserMedia(), encodes it as PCMU, and pushes it via the talk session.
//
// One Acceptor instance is bound to one Node (the platform-large simulated
// camera). The MediaService that owns the PS pipeline is the one it talks to
// — by construction the talk port and the media port are siblings on the
// same node.
//
// This file is the app-layer glue: per-node session bookkeeping, the talk
// session lifecycle, and the codec routing. The SIP machinery lives in
// internal/adapter/sip (TalkHandler); the PCMU RTP packer/unpacker lives in
// internal/adapter/media (PCMUTranscoder).
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// errTalkSessionNotFound is returned by SendAudio when the session has been
// removed already (or was never started).
var errTalkSessionNotFound = errors.New("app: talk session not found")

// talkSessionKey is the composite identifier for an intercom session: the
// owning node plus the device channel it binds to. The SIP Call-ID plus the
// From/To tags carry the dialog identity on the wire; this key is what the
// Web UI uses to correlate.
type talkSessionKey struct {
	NodeID    string
	ChannelID string
}

// talkSession is the app-layer view of an active intercom dialog.
type talkSession struct {
	id         string
	key        talkSessionKey
	codec      string // "PCMU" / "PCMA"
	rtpPort    int
	deviceAddr string
	startedAt  time.Time
	// audioIn carries PCMU frames from the device to the Web UI; audioOut
	// is the reverse path. Either side may be closed when the session is
	// half-duplex (e.g. device-only).
	audioIn  chan []byte
	audioOut chan []byte
}

// TalkAcceptor is a per-node voice-intercom listener. It owns the talk
// sessions for one node and exposes the methods the Web UI calls.
type TalkAcceptor struct {
	log       *slog.Logger
	media     *MediaService
	transport port.SIPTransport // optional: when set, used to send BYE on stop

	mu       sync.Mutex
	sessions map[string]*talkSession
	stopCh   chan struct{}
}

// NewTalkAcceptor creates an empty acceptor. The MediaService is mandatory
// because the talk RTP socket binds on the same host; transport is optional
// and may be nil while the SIP INVITE handler is still being built.
func NewTalkAcceptor(log *slog.Logger, media *MediaService, tr port.SIPTransport) *TalkAcceptor {
	if log == nil {
		log = slog.Default()
	}
	return &TalkAcceptor{
		log:       log.With(slog.String("comp", "talk-acceptor")),
		media:     media,
		transport: tr,
		sessions:  make(map[string]*talkSession),
		stopCh:    make(chan struct{}),
	}
}

// StartInvite implements the platform-issued side of the dialog. The Web UI
// calls it; the SIP layer then takes over to deliver the INVITE to the
// device.
//
// The returned rtpPort is what the device should send audio to. The Web UI
// will then read from the same port through the streaming server (a separate
// WebSocket route lives outside this file).
func (a *TalkAcceptor) StartInvite(ctx context.Context, deviceID, channelID, codec string) (string, int, error) {
	if a == nil {
		return "", 0, fmt.Errorf("app: talk acceptor is nil")
	}
	if deviceID == "" || channelID == "" {
		return "", 0, fmt.Errorf("app: talk invite requires deviceID and channelID")
	}
	switch codec {
	case "PCMU", "PCMA":
		// accepted
	default:
		return "", 0, fmt.Errorf("%w: codec %q", port.ErrTalkUnsupported, codec)
	}

	// Pick a free UDP port for the device's RTP stream. Port 0 lets the
	// kernel choose; we then read the chosen port back via LocalAddr.
	rtpAddr, err := net.ResolveUDPAddr("udp", "0.0.0.0:0")
	if err != nil {
		return "", 0, fmt.Errorf("app: talk resolve udp: %w", err)
	}
	rtpConn, err := net.ListenUDP("udp", rtpAddr)
	if err != nil {
		return "", 0, fmt.Errorf("app: talk listen udp: %w", err)
	}

	sid := fmt.Sprintf("talk-%s-%s-%d", deviceID, channelID, time.Now().UnixNano())
	sess := &talkSession{
		id:        sid,
		key:       talkSessionKey{NodeID: deviceID, ChannelID: channelID},
		codec:     codec,
		rtpPort:   rtpConn.LocalAddr().(*net.UDPAddr).Port,
		startedAt: time.Now(),
		audioIn:   make(chan []byte, 32),
		audioOut:  make(chan []byte, 32),
	}

	a.mu.Lock()
	a.sessions[sid] = sess
	a.mu.Unlock()

	// Pump the RTP socket into the audio channel until the session ends.
	// The goroutine exits when the session is removed (its audio channel
	// is closed by Stop) or the socket is closed.
	go a.receiveRTP(ctx, rtpConn, sess)

	a.log.Info("talk session started",
		slog.String("session", sid),
		slog.String("device", deviceID),
		slog.String("channel", channelID),
		slog.String("codec", codec),
		slog.Int("rtp_port", sess.rtpPort),
	)
	return sid, sess.rtpPort, nil
}

// receiveRTP reads PCMU/PCMA frames from the device's RTP stream until the
// socket is closed. The frames are forwarded to the Web UI via the audioIn
// channel; the implementation in this file stops the loop when the session
// is removed from the acceptor.
func (a *TalkAcceptor) receiveRTP(ctx context.Context, conn *net.UDPConn, sess *talkSession) {
	defer func() {
		_ = conn.Close()
	}()
	buf := make([]byte, 160) // 20 ms at 8 kHz, fits PCMU RTP payload
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				// idle: check whether the session still exists; if not, exit
				if !a.hasSession(sess.id) {
					return
				}
				continue
			}
			return
		}
		if n == 0 {
			continue
		}
		frame := make([]byte, n)
		copy(frame, buf[:n])
		select {
		case sess.audioIn <- frame:
		default:
			// Web UI is slow; drop the oldest queued frame to make room
			select {
			case <-sess.audioIn:
			default:
			}
			select {
			case sess.audioIn <- frame:
			default:
			}
		}
	}
}

func (a *TalkAcceptor) hasSession(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.sessions[id]
	return ok
}

// Stop ends the talk session and tears down its RTP socket. The audioIn
// channel is closed so the Web UI stream reader exits cleanly.
func (a *TalkAcceptor) Stop(ctx context.Context, sessionID string) error {
	a.mu.Lock()
	sess, ok := a.sessions[sessionID]
	if !ok {
		a.mu.Unlock()
		return nil
	}
	delete(a.sessions, sessionID)
	a.mu.Unlock()

	close(sess.audioIn)
	a.log.Info("talk session stopped",
		slog.String("session", sessionID),
		slog.String("device", sess.key.NodeID),
		slog.String("channel", sess.key.ChannelID),
		slog.Duration("duration", time.Since(sess.startedAt)),
	)
	return nil
}

// SendAudio pushes one PCMU/PCMA frame to the device. Currently this is a
// stub that buffers; the SIP/RTP outbound path will be wired when the
// TalkHandler adapter is built in a follow-up change.
func (a *TalkAcceptor) SendAudio(ctx context.Context, sessionID string, pcm []byte) error {
	a.mu.Lock()
	sess, ok := a.sessions[sessionID]
	a.mu.Unlock()
	if !ok {
		return errTalkSessionNotFound
	}
	if sess.codec != "PCMU" && sess.codec != "PCMA" {
		return fmt.Errorf("%w: codec %s", port.ErrTalkUnsupported, sess.codec)
	}
	select {
	case sess.audioOut <- pcm:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// List returns the IDs of every active session. Used by the Web UI to render
// the "active talks" indicator.
func (a *TalkAcceptor) List(ctx context.Context) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	ids := make([]string, 0, len(a.sessions))
	for id := range a.sessions {
		ids = append(ids, id)
	}
	return ids
}

// Close stops all active sessions and prevents new ones. The WebSocket
// readers exit as their audioIn channels close.
func (a *TalkAcceptor) Close() error {
	select {
	case <-a.stopCh:
		return nil
	default:
		close(a.stopCh)
	}
	a.mu.Lock()
	sessions := make([]*talkSession, 0, len(a.sessions))
	for _, s := range a.sessions {
		sessions = append(sessions, s)
	}
	a.mu.Unlock()
	for _, s := range sessions {
		_ = a.Stop(context.Background(), s.id)
	}
	return nil
}
