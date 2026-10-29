package app

// Live pull: a platform node opens a SIP dialog with a downstream device,
// receives its RTP, and publishes the reassembled PS frames on a bus keyed by
// (node, channel). The HTTP-FLV gateway reads that bus; nothing here knows
// about FLV.

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

const (
	// liveNoMediaTimeout is the default for how long a session may go
	// without media before it is ended. It bounds both "the device
	// accepted but sends nothing" and "the stream died without a BYE".
	liveNoMediaTimeout = 30 * time.Second
	// liveWatchInterval is the default watchdog period.
	liveWatchInterval = 5 * time.Second
	// liveSubBuffer is how far a slow FLV subscriber may fall behind
	// before frames are dropped for it. Dropping beats blocking the
	// receiver: one stalled browser must not stall the device's stream.
	liveSubBuffer = 32
	// liveReadBuffer is one RTP datagram plus slack.
	liveReadBuffer = 65536
)

type liveKey struct {
	node    string
	channel string
}

// liveSession is one active pull.
type liveSession struct {
	key    liveKey
	info   port.LiveSessionInfo
	node   model.NodeID
	callID string
	target string
	addr   string

	deizer port.RTPDeizer
	udp    net.PacketConn
	tcp    net.Listener
	conn   net.Conn

	mu      sync.Mutex
	subs    map[int]chan model.PSFrame
	nextSub int
	lastAt  time.Time

	// relayQuiet suppresses the "tell the upstream" step when the upstream
	// itself is what ended this session.
	relayQuiet bool

	done       chan struct{}
	cancel     context.CancelFunc
	closeOnce  sync.Once
	recordDone chan struct{} // nil when recording is off
}

func newLiveSession(key liveKey, info port.LiveSessionInfo, node model.NodeID, callID, target, addr string, deizer port.RTPDeizer) *liveSession {
	_, cancel := context.WithCancel(context.Background())
	return &liveSession{
		key: key, info: info, node: node, callID: callID, target: target, addr: addr,
		deizer: deizer, subs: make(map[int]chan model.PSFrame),
		lastAt: time.Now(), done: make(chan struct{}), cancel: cancel,
	}
}

// frame publishes one PS frame. A subscriber whose buffer is full loses the
// frame rather than stalling the receiver.
func (l *liveSession) frame(ps model.PSFrame) {
	l.mu.Lock()
	l.lastAt = time.Now()
	for _, ch := range l.subs {
		select {
		case ch <- ps:
		default:
		}
	}
	l.mu.Unlock()
}

func (l *liveSession) subscribe() (<-chan model.PSFrame, func()) {
	ch := make(chan model.PSFrame, liveSubBuffer)
	l.mu.Lock()
	id := l.nextSub
	l.nextSub++
	l.subs[id] = ch
	l.mu.Unlock()
	cleanup := func() {
		l.mu.Lock()
		if _, ok := l.subs[id]; ok {
			delete(l.subs, id)
			close(ch)
		}
		l.mu.Unlock()
	}
	return ch, cleanup
}

func (l *liveSession) lastActivity() time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastAt
}

// close releases the ports and ends every subscriber's stream. It is
// idempotent: stop, BYE, watchdog and read-loop failure all land here.
func (l *liveSession) close() {
	l.closeOnce.Do(func() {
		l.cancel()
		if l.udp != nil {
			_ = l.udp.Close()
		}
		if l.tcp != nil {
			_ = l.tcp.Close()
		}
		if l.conn != nil {
			_ = l.conn.Close()
		}
		l.mu.Lock()
		for id, ch := range l.subs {
			delete(l.subs, id)
			close(ch)
		}
		l.mu.Unlock()
		close(l.done)
	})
}

// WithLiveDeizer wires the RTP reassembler a live pull uses to turn datagrams
// back into PS frames. Without it a platform cannot receive media, so
// StartLive reports the capability as unsupported.
func (s *NodeService) WithLiveDeizer(f RTPDeizerFactory) (*NodeService, error) {
	if f == nil {
		return nil, fmt.Errorf("app: NodeService requires a non-nil deizer factory")
	}
	s.liveDeizer = f
	return s, nil
}

// WithRecordings attaches the platform's recording library. A start that
// happens after this call records from the first frame; a store attached
// later still applies to pulls started afterwards.
func (s *NodeService) WithRecordings(store port.RecordingStore) (*NodeService, error) {
	if store == nil {
		return nil, fmt.Errorf("app: NodeService requires a non-nil recording store")
	}
	s.recordings = store
	if s.acceptor != nil {
		s.acceptor.WithRecordings(store)
	}
	return s, nil
}

// StartLive pulls the stream of channelID from the downstream device that
// owns it. It returns once the device has answered (or refused): the caller
// learns the port and transport it can watch on.
func (s *NodeService) StartLive(ctx context.Context, id model.NodeID, channelID, transport string) (port.LiveSessionInfo, error) {
	return s.startLiveWithKey(ctx, liveKey{id.String(), channelID}, id, channelID, transport)
}

// relayChannelKey is the session key a relay uses: the channel plus the
// peer's Call-ID, so two upstreams pulling the same channel do not share (and
// therefore cannot kill) each other's session.
const relayChannelSep = "|"

func relayChannelKey(channelID, relayID string) string { return channelID + relayChannelSep + relayID }

// StartLiveRelay pulls one channel on behalf of a cascade peer. Each peer gets
// its own session: a second relay must not tear down the first peer's stream,
// so the key carries the peer's Call-ID instead of the channel alone.
func (s *NodeService) StartLiveRelay(ctx context.Context, id model.NodeID, channelID, relayID, transport string) (port.LiveSessionInfo, error) {
	return s.startLiveWithKey(ctx, liveKey{id.String(), relayChannelKey(channelID, relayID)}, id, channelID, transport)
}

// RelayPS hands a relay the frames of its own downstream session.
func (s *NodeService) RelayPS(_ context.Context, id model.NodeID, channelID, relayID string) (<-chan model.PSFrame, func(), bool) {
	sess := s.liveSession(liveKey{id.String(), relayChannelKey(channelID, relayID)})
	if sess == nil {
		return nil, nil, false
	}
	ch, cleanup := sess.subscribe()
	return ch, cleanup, true
}

// StopLiveRelay ends one relay's downstream session and says goodbye to the
// device. It leaves other peers of the same channel alone.
func (s *NodeService) StopLiveRelay(_ context.Context, id model.NodeID, channelID, relayID string) error {
	sess := s.takeLive(liveKey{id.String(), relayChannelKey(channelID, relayID)})
	if sess == nil {
		return nil
	}
	sess.setQuiet()
	s.endLive(sess, true)
	return nil
}

func (s *NodeService) startLiveWithKey(ctx context.Context, key liveKey, id model.NodeID, channelID, transport string) (port.LiveSessionInfo, error) {
	if transport == "" {
		transport = "udp"
	}
	transport = strings.ToLower(strings.TrimSpace(transport))
	if transport != "udp" && transport != "tcp" {
		return port.LiveSessionInfo{}, fmt.Errorf("app: live transport %q is not udp or tcp", transport)
	}
	if s.acceptor == nil || s.liveDeizer == nil {
		return port.LiveSessionInfo{}, fmt.Errorf("%w: this process has no media wiring", port.ErrLiveUnsupported)
	}
	node, ok := s.registry.Get(ctx, id)
	if !ok {
		return port.LiveSessionInfo{}, model.ErrUnknownNode
	}
	if node.ID().Kind() == model.NodeKindDevice {
		return port.LiveSessionInfo{}, fmt.Errorf("%w: node %s is a device, which is the source side", port.ErrLiveUnsupported, id)
	}
	target, addr, err := s.liveTarget(ctx, node, channelID)
	if err != nil {
		return port.LiveSessionInfo{}, err
	}

	// A second start on the same key replaces the first: the old dialog goes
	// away before the new port is advertised, so the device is never told two
	// destinations. A relay's key includes the peer, so peers coexist.
	if old := s.takeLive(key); old != nil {
		s.endLive(old, true)
	}

	sess := newLiveSession(key, port.LiveSessionInfo{Transport: transport},
		id, "", target, addr, s.liveDeizer(0))
	fail := func(err error) (port.LiveSessionInfo, error) {
		sess.close()
		return port.LiveSessionInfo{}, err
	}

	localIP := hostOf(node.Profile().Addr())
	var offer model.LiveOffer
	if transport == "tcp" {
		ln, lerr := net.Listen("tcp", "0.0.0.0:0")
		if lerr != nil {
			return fail(fmt.Errorf("app: listen for tcp live media: %w", lerr))
		}
		sess.tcp = ln
		sess.info.LocalPort = ln.Addr().(*net.TCPAddr).Port
		offer = model.LiveOffer{ChannelID: channelID, ConnIP: localIP, RTPPort: sess.info.LocalPort,
			Proto: "TCP/RTP/AVP", Setup: "passive"}
	} else {
		conn, lerr := net.ListenPacket("udp4", "0.0.0.0:0")
		if lerr != nil {
			return fail(fmt.Errorf("app: open live media port: %w", lerr))
		}
		sess.udp = conn
		sess.info.LocalPort = conn.LocalAddr().(*net.UDPAddr).Port
		offer = model.LiveOffer{ChannelID: channelID, ConnIP: localIP, RTPPort: sess.info.LocalPort}
	}
	body, berr := model.LiveOfferBytes(offer)
	if berr != nil {
		return fail(fmt.Errorf("app: build live offer: %w", berr))
	}

	callID, answer, ierr := s.acceptor.InviteVideo(ctx, id, target, addr, channelID, body)
	if ierr != nil {
		return fail(ierr)
	}
	parsed, perr := model.ParseLiveAnswer(answer.Body())
	if perr != nil {
		return fail(fmt.Errorf("%w: unusable answer from %s: %v", port.ErrLiveRefused, target, perr))
	}
	if !sameTransport(parsed.Proto, transport) {
		return fail(fmt.Errorf("%w: device answered %q to a %s offer", port.ErrLiveRefused, parsed.Proto, transport))
	}
	// A UDP device must name the port it sends to; a TCP device connects
	// instead, so it may answer with 0.
	if transport == "udp" && parsed.Port <= 0 {
		return fail(fmt.Errorf("%w: device answered no media port", port.ErrLiveRefused))
	}

	sess.callID = callID
	sess.info.SSRC = parsed.SSRC
	sess.deizer = s.liveDeizer(parsed.SSRC)

	s.liveMu.Lock()
	s.live[key] = sess
	s.liveMu.Unlock()

	if transport == "tcp" {
		go s.receiveLiveTCP(sess)
	} else {
		go s.receiveLiveUDP(sess)
	}
	go s.watchLive(sess)
	if s.recordings != nil {
		sess.recordDone = make(chan struct{})
		go func() {
			defer close(sess.recordDone)
			s.recordLive(sess)
		}()
	}

	s.log.Info("live pull started",
		"node_id", id.String(), "channel", channelID, "target", target,
		"transport", transport, "port", sess.info.LocalPort, "ssrc", sess.info.SSRC)
	return sess.info, nil
}

// StopLive ends the pull and says goodbye to the device. Stopping a channel
// that has no session is a no-op: callers retry safely.
func (s *NodeService) StopLive(ctx context.Context, id model.NodeID, channelID string) error {
	sess := s.takeLive(liveKey{id.String(), channelID})
	if sess == nil {
		return nil
	}
	s.endLive(sess, true)
	return nil
}

// LivePS hands a consumer the PS frames of an active pull. The third return
// is false when no session exists for the channel, which is how the FLV
// gateway decides to fall back to the local media source.
func (s *NodeService) LivePS(_ context.Context, id model.NodeID, channelID string) (<-chan model.PSFrame, func(), bool) {
	sess := s.liveSession(liveKey{id.String(), channelID})
	if sess == nil {
		return nil, nil, false
	}
	ch, cleanup := sess.subscribe()
	return ch, cleanup, true
}

// liveTarget resolves which downstream device owns the channel. A channel
// entry's parent wins; with no parent a single online device is the only
// unambiguous answer. Everything else is "no target" (404) or "that device
// is offline" (502).
func (s *NodeService) liveTarget(ctx context.Context, node model.Node, channelID string) (string, string, error) {
	owner := ""
	if ch, ok := node.Profile().ChannelByID(channelID); ok {
		owner = ch.ParentID()
	}
	devices, err := s.Devices(ctx, node.ID())
	if err != nil {
		return "", "", err
	}
	if owner != "" {
		for _, d := range devices {
			if d.DeviceID() == owner {
				return owner, d.Addr(), nil
			}
		}
		return "", "", fmt.Errorf("%w: device %s is not online", port.ErrControlTargetUnreachable, owner)
	}
	if len(devices) == 1 {
		return devices[0].DeviceID(), devices[0].Addr(), nil
	}
	return "", "", fmt.Errorf("%w: channel %s names no owner and node %s has %d online devices",
		port.ErrLiveNoTarget, channelID, node.ID(), len(devices))
}

func (s *NodeService) receiveLiveUDP(sess *liveSession) {
	buf := make([]byte, liveReadBuffer)
	for {
		n, _, err := sess.udp.ReadFrom(buf)
		if err != nil {
			s.endLive(sess, false)
			return
		}
		s.ingest(sess, buf[:n])
	}
}

func (s *NodeService) receiveLiveTCP(sess *liveSession) {
	conn, err := sess.tcp.Accept()
	if err != nil {
		s.endLive(sess, false)
		return
	}
	sess.mu.Lock()
	sess.conn = conn
	sess.mu.Unlock()

	var prefix [2]byte
	for {
		if _, err := io.ReadFull(conn, prefix[:]); err != nil {
			s.endLive(sess, false)
			return
		}
		size := int(binary.BigEndian.Uint16(prefix[:]))
		if size == 0 {
			continue
		}
		payload := make([]byte, size)
		if _, err := io.ReadFull(conn, payload); err != nil {
			s.endLive(sess, false)
			return
		}
		s.ingest(sess, payload)
	}
}

// ingest turns one RTP datagram into PS frames on the bus. Malformed packets
// are counted as noise, not as a reason to end the dialog: UDP reordering and
// loss are normal on a media path.
func (s *NodeService) ingest(sess *liveSession, datagram []byte) {
	pkt, err := model.ParseRTPPacket(datagram)
	if err != nil {
		return
	}
	// The device names its SSRC in the answer; anything else on this port
	// belongs to another stream and must not be spliced into this one.
	if sess.info.SSRC != 0 && pkt.SSRC != sess.info.SSRC {
		return
	}
	ps, err := sess.deizer.Write(pkt)
	if err != nil || ps.Payload == nil {
		return
	}
	sess.frame(ps)
}

func (s *NodeService) watchLive(sess *liveSession) {
	ticker := time.NewTicker(s.liveWatchPeriod())
	defer ticker.Stop()
	for {
		select {
		case <-sess.done:
			return
		case <-ticker.C:
			if time.Since(sess.lastActivity()) > s.liveIdleTimeout() {
				s.log.Warn("live pull ended: no media",
					"node_id", sess.node.String(), "channel", sess.key.channel,
					"call_id", sess.callID)
				s.endLive(sess, true)
				return
			}
		}
	}
}

// endLive removes the session, optionally says goodbye, and releases
// everything. It is safe to call from several paths at once.
func (s *NodeService) endLive(sess *liveSession, sendBye bool) {
	s.liveMu.Lock()
	if cur, ok := s.live[sess.key]; ok && cur == sess {
		delete(s.live, sess.key)
	}
	s.liveMu.Unlock()
	sess.close()
	// A relay session ending on its own (device BYE, media timeout) has to
	// tell the upstream too; a stop we initiated already did.
	if relayID, ok := relayIDOf(sess.key); ok && s.acceptor != nil && !sess.quiet() {
		s.acceptor.relayEndedByDownstream(sess.node, relayID)
	}
	// Wait for the recorder to close its segment, so a stop returns with
	// the recording complete rather than racing the last frame.
	if sess.recordDone != nil {
		select {
		case <-sess.recordDone:
		case <-time.After(2 * time.Second):
			s.log.Warn("recorder did not stop in time", "channel", sess.key.channel)
		}
	}
	if sendBye && s.acceptor != nil && sess.callID != "" {
		byeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.acceptor.Bye(byeCtx, sess.node, sess.target, sess.addr, sess.callID)
	}
}

// onPeerLiveBye ends the pull a device hung up. The BYE carries only the
// Call-ID, so the lookup walks the (small) session table.
func (s *NodeService) onPeerLiveBye(nodeID model.NodeID, callID string) {
	s.liveMu.Lock()
	var found *liveSession
	for _, sess := range s.live {
		if sess.node == nodeID && sess.callID == callID {
			found = sess
			break
		}
	}
	s.liveMu.Unlock()
	if found != nil {
		s.endLive(found, false)
	}
}

// stopLiveOfNode ends every pull of a node that is going away. Nothing is
// sent to the devices: the node's signalling is stopping too, and a failed
// goodbye must not hold up the stop.
func (s *NodeService) stopLiveOfNode(id model.NodeID) {
	s.liveMu.Lock()
	sessions := make([]*liveSession, 0, len(s.live))
	for _, sess := range s.live {
		if sess.node == id {
			sessions = append(sessions, sess)
		}
	}
	s.liveMu.Unlock()
	for _, sess := range sessions {
		s.endLive(sess, false)
	}
}

func (s *NodeService) takeLive(key liveKey) *liveSession {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	sess := s.live[key]
	if sess != nil {
		delete(s.live, key)
	}
	return sess
}

func (s *NodeService) liveSession(key liveKey) *liveSession {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	return s.live[key]
}

// sameTransport reports whether the device's answer matches the offer. An
// answer that names no proto at all is read as UDP, the default.
func sameTransport(answerProto, offered string) bool {
	proto := strings.ToUpper(strings.TrimSpace(answerProto))
	if proto == "" {
		proto = "RTP/AVP"
	}
	want := "RTP/AVP"
	if offered == "tcp" {
		want = "TCP/RTP/AVP"
	}
	return proto == want
}

func hostOf(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

func (s *NodeService) liveIdleTimeout() time.Duration {
	if s.liveIdle > 0 {
		return s.liveIdle
	}
	return liveNoMediaTimeout
}

func (s *NodeService) liveWatchPeriod() time.Duration {
	if s.liveWatch > 0 {
		return s.liveWatch
	}
	return liveWatchInterval
}

// recordLive writes the session's PS frames to the recording library. A
// write failure is logged once and does not stop the pull: losing the
// recording is bad, losing the live picture because of it is worse.
func (s *NodeService) recordLive(sess *liveSession) {
	frames, cleanup := sess.subscribe()
	defer cleanup()
	defer func() {
		if err := s.recordings.Finish(context.Background(), sess.node, sess.key.channel); err != nil {
			s.log.Warn("cannot close the recording segment", "error", err.Error())
		}
	}()
	warned := false
	for {
		select {
		case <-sess.done:
			return
		case ps, ok := <-frames:
			if !ok {
				return
			}
			if err := s.recordings.Append(context.Background(), sess.node, sess.key.channel, ps); err != nil {
				if !warned {
					s.log.Warn("recording failed; the live stream continues",
						"node_id", sess.node.String(), "channel", sess.key.channel,
						"error", err.Error())
					warned = true
				} else {
					s.log.Debug("recording still failing", "error", err.Error())
				}
			}
		}
	}
}

// Recordings lists the platform's recordings for one channel. A process
// without a recording library reports the capability as unsupported rather
// than as an empty library.
func (s *NodeService) Recordings(ctx context.Context, id model.NodeID, channelID string, start, end time.Time) ([]port.Recording, error) {
	if s.recordings == nil {
		return nil, fmt.Errorf("%w: recording library is not wired", port.ErrRecordingUnsupported)
	}
	if _, ok := s.registry.Get(ctx, id); !ok {
		return nil, model.ErrUnknownNode
	}
	return s.recordings.List(ctx, id, channelID, start, end), nil
}

// OpenRecording hands a consumer the PS frames of a window of recordings.
// It is the streaming gateway's source for recording playback.
func (s *NodeService) OpenRecording(ctx context.Context, id model.NodeID, channelID string, start, end time.Time) (<-chan model.PSFrame, func(), error) {
	if s.recordings == nil {
		return nil, nil, fmt.Errorf("%w: recording library is not wired", port.ErrRecordingUnsupported)
	}
	return s.recordings.Stream(ctx, id, channelID, start, end)
}

// relayIDOf reports the peer Call-ID of a relay session key.
func relayIDOf(key liveKey) (string, bool) {
	i := strings.LastIndex(key.channel, relayChannelSep)
	if i <= 0 || i == len(key.channel)-1 {
		return "", false
	}
	return key.channel[i+1:], true
}

// quiet reports whether this session was ended by the upstream, in which case
// the upstream needs no further news.
func (l *liveSession) quiet() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.relayQuiet
}

func (l *liveSession) setQuiet() {
	l.mu.Lock()
	l.relayQuiet = true
	l.mu.Unlock()
}
