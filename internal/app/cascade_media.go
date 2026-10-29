package app

// Cascade media relay: the platform as a B2BUA for live video. An upstream
// INVITE for a channel that belongs to a downstream device is answered by the
// platform itself, while the platform pulls that channel from the device and
// forwards the PS frames as RTP to the upstream.
//
// Each upstream gets its own downstream session, so two peers pulling the
// same channel do not share a dialog: one peer's BYE must not stop the
// other's picture.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// mediaRelayService is the slice of NodeService a relay needs. Keeping it
// small documents what the relay is allowed to touch.
type mediaRelayService interface {
	StartLiveRelay(ctx context.Context, id model.NodeID, channelID, relayID, transport string) (port.LiveSessionInfo, error)
	RelayPS(ctx context.Context, id model.NodeID, channelID, relayID string) (<-chan model.PSFrame, func(), bool)
	StopLiveRelay(ctx context.Context, id model.NodeID, channelID, relayID string) error
}

// relayCall is one upstream↔downstream pair.
type relayCall struct {
	upstreamCallID   string
	downstreamCallID string
	upstreamPeer     string
	channelID        string
	nodeID           model.NodeID
	sink             mediaSink
	stopCh           chan struct{}
	closeOnce        sync.Once
}

// mediaRelayTable indexes relay calls by upstream Call-ID, which is the only
// identity a BYE from the upstream carries.
type mediaRelayTable struct {
	mu    sync.Mutex
	calls map[string]*relayCall
}

func newMediaRelayTable() *mediaRelayTable {
	return &mediaRelayTable{calls: map[string]*relayCall{}}
}

func (t *mediaRelayTable) put(call *relayCall) {
	t.mu.Lock()
	t.calls[call.upstreamCallID] = call
	t.mu.Unlock()
}

func (t *mediaRelayTable) take(callID string) *relayCall {
	t.mu.Lock()
	defer t.mu.Unlock()
	call := t.calls[callID]
	delete(t.calls, callID)
	return call
}

func (t *mediaRelayTable) size() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.calls)
}

// WithMediaRelay attaches the service that can pull a channel on the
// platform's behalf. Without it the platform cannot relay media and keeps the
// generic INVITE answer.
func (a *Acceptor) WithMediaRelay(r mediaRelayService) *Acceptor {
	a.relayMedia = r
	if a.mediaRelays == nil {
		a.mediaRelays = newMediaRelayTable()
	}
	return a
}

// answerRelayedVideoInvite handles an upstream live-view INVITE by relaying.
// It reports whether the request was a relay (and therefore answered here).
// A channel this platform cannot route keeps the generic answer, unchanged.
// errResponseDeferred tells the read loop that a handler answered (or will
// answer) outside the loop, so it must not send a response itself.
var errResponseDeferred = errors.New("app: response handled outside the read loop")

// answerRelayedVideoInvite starts a relay for an upstream live-view INVITE.
// It reports whether the request is a relay; when it is, the answer is sent by
// the relay goroutine.
//
// The pull the relay performs is itself a SIP transaction on the same node
// socket, and the read loop is what delivers its answer. Waiting for it here
// would deadlock: the loop cannot read the device's response while it waits
// for this handler. So the relay runs in the background and answers the peer
// when it knows the outcome.
func (a *Acceptor) answerRelayedVideoInvite(p *platform, req model.Message, peer string) bool {
	if a.relayMedia == nil || a.registry == nil {
		return false
	}
	channelID := toUser(req)
	callID := headerValue(req, "Call-ID")
	if channelID == "" || callID == "" {
		return false
	}
	node, ok := a.registry.Get(context.Background(), p.id)
	if !ok {
		return false
	}
	_, _, terr := relayTarget(node, channelID, a.Devices(context.Background(), p.id))
	if errors.Is(terr, port.ErrLiveNoTarget) {
		return false
	}
	go a.runRelay(p, req, peer, channelID, callID, terr)
	return true
}

// runRelay establishes both legs and answers the peer.
func (a *Acceptor) runRelay(p *platform, req model.Message, peer, channelID, callID string, targetErr error) {
	answer := func(resp model.Message) {
		if resp.Method() != "" {
			return
		}
		if out, dst, ok := a.routeOutbound(p, resp, peer); ok {
			if err := p.tr.Send(context.Background(), out, dst); err != nil {
				a.log.Warn("relay cannot answer the peer",
					"node_id", p.id.String(), "call_id", callID, "error", err.Error())
			}
		}
	}

	offer, perr := parseVideoOffer(req.Body())
	if perr != nil {
		answer(mustResponse(400, "Bad Request", req, nil, ""))
		return
	}
	upstreamSink, serr := openMediaSink(offer.offerTransport, offer.Host, offer.Port)
	if serr != nil {
		a.log.Warn("relay cannot open the upstream transport",
			"node_id", p.id.String(), "error", serr.Error())
		answer(mustResponse(488, "Not Acceptable Here", req, nil, ""))
		return
	}
	if targetErr != nil {
		// An offline owner is still a relay attempt: the peer asked for a
		// stream this platform is supposed to serve.
		_ = upstreamSink.Close()
		answer(a.relayFailure(req, targetErr))
		return
	}

	info, lerr := a.relayMedia.StartLiveRelay(context.Background(), p.id, channelID, callID, "udp")
	if lerr != nil {
		_ = upstreamSink.Close()
		a.log.Warn("relay could not pull the downstream stream",
			"node_id", p.id.String(), "channel", channelID, "error", lerr.Error())
		answer(a.relayFailure(req, lerr))
		return
	}

	frames, cleanup, ok := a.relayMedia.RelayPS(context.Background(), p.id, channelID, callID)
	if !ok {
		_ = upstreamSink.Close()
		_ = a.relayMedia.StopLiveRelay(context.Background(), p.id, channelID, callID)
		answer(mustResponse(500, "Server Internal Error", req, nil, ""))
		return
	}

	call := &relayCall{
		upstreamCallID: callID, upstreamPeer: peer, channelID: channelID, nodeID: p.id,
		sink: upstreamSink, stopCh: make(chan struct{}),
	}
	a.mediaRelays.put(call)
	go a.pumpRelay(call, frames, cleanup)

	localPort, proto, setup := upstreamSink.Answer()
	host := hostOf(p.tr.LocalAddr())
	body := fmt.Sprintf("v=0\r\no=%s 0 0 IN IP4 %s\r\ns=Play\r\nc=IN IP4 %s\r\nt=0 0\r\n"+
		"m=video %d %s 96\r\na=sendonly\r\n", p.id.String(), host, host, localPort, proto)
	if setup != "" {
		body += "a=setup:" + setup + "\r\n"
	}
	body += "a=rtpmap:96 PS/90000\r\n"

	hdrs := []model.Header{
		model.NewHeader("Contact", "<sip:"+p.id.String()+"@"+p.realm+">"),
		model.NewHeader("Content-Type", "application/sdp"),
		allowHeader(),
	}
	answer(mustResponse(200, "OK", req, hdrs, body))
	a.log.Info("cascade media relay started",
		"node_id", p.id.String(), "channel", channelID,
		"upstream_call_id", callID, "downstream_port", info.LocalPort)
}

// pumpRelay forwards downstream PS frames upstream until either side ends.
func (a *Acceptor) pumpRelay(call *relayCall, frames <-chan model.PSFrame, cleanup func()) {
	defer cleanup()
	rtpizer, err := a.relayRTPizer()
	if err != nil {
		a.log.Warn("relay has no RTP packetizer", "error", err.Error())
		a.endRelay(call, true)
		return
	}
	for {
		select {
		case <-call.stopCh:
			return
		case ps, ok := <-frames:
			if !ok {
				a.endRelay(call, true)
				return
			}
			pkts, perr := rtpizer.Packetize(ps)
			if perr != nil {
				continue
			}
			for _, pkt := range pkts {
				if werr := call.sink.Write(pkt.Bytes()); werr != nil {
					a.log.Debug("relay write failed; ending the call", "error", werr.Error())
					a.endRelay(call, true)
					return
				}
			}
		}
	}
}

// relayRTPizer builds the packetizer used to forward frames upstream.
func (a *Acceptor) relayRTPizer() (port.RTPizer, error) {
	if a.mediaService == nil {
		return nil, fmt.Errorf("app: no media service wired")
	}
	return a.mediaService.NewRTPizer(0)
}

// endRelay tears one call down. sendBye reports whether the downstream should
// be told (the upstream already knows when it hung up).
func (a *Acceptor) endRelay(call *relayCall, sendBye bool) {
	call.closeOnce.Do(func() {
		close(call.stopCh)
		_ = call.sink.Close()
		if a.mediaRelays != nil {
			a.mediaRelays.take(call.upstreamCallID)
		}
		if sendBye && a.relayMedia != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = a.relayMedia.StopLiveRelay(ctx, call.nodeID, call.channelID, call.upstreamCallID)
		}
	})
}

// relayBye ends the relay an upstream hung up, if this Call-ID names one.
func (a *Acceptor) relayBye(callID string) bool {
	if a.mediaRelays == nil || callID == "" {
		return false
	}
	call := a.mediaRelays.take(callID)
	if call == nil {
		return false
	}
	a.endRelay(call, true)
	return true
}

// relayFailure maps a failed downstream attempt onto the status the upstream
// should see.
func (a *Acceptor) relayFailure(req model.Message, err error) model.Message {
	switch {
	case errors.Is(err, port.ErrLiveNoTarget):
		return mustResponse(404, "Not Found", req, []model.Header{allowHeader()}, "")
	case errors.Is(err, port.ErrLiveRefused):
		return mustResponse(480, "Temporarily Unavailable", req, nil, "")
	case errors.Is(err, port.ErrControlTimeout):
		return mustResponse(504, "Server Time-out", req, nil, "")
	default:
		return mustResponse(502, "Bad Gateway", req, nil, "")
	}
}

// relayTarget resolves which downstream owns the channel, mirroring the local
// pull's rules: a channel's parent wins, a lone online device is the fallback.
func relayTarget(node model.Node, channelID string, devices []model.DownstreamDevice) (string, string, error) {
	owner := ""
	if ch, ok := node.Profile().ChannelByID(channelID); ok {
		owner = ch.ParentID()
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
	return "", "", fmt.Errorf("%w: channel %s names no owner", port.ErrLiveNoTarget, channelID)
}

// mustResponse builds a response the caller already decided is correct. A
// construction failure can only come from a malformed request we are
// answering anyway, so falling back to a bare 500 keeps the read loop simple.
func mustResponse(status int, reason string, req model.Message, hdrs []model.Header, body string) model.Message {
	resp, err := buildResponse(status, reason, req, hdrs, body)
	if err != nil {
		return model.Message{}
	}
	return resp
}

// relayEndedByDownstream tells the upstream that the device hung up. The
// downstream session is already gone, so nothing is sent back down.
func (a *Acceptor) relayEndedByDownstream(nodeID model.NodeID, relayID string) {
	if a.mediaRelays == nil || relayID == "" {
		return
	}
	a.mediaRelays.mu.Lock()
	var found *relayCall
	for _, call := range a.mediaRelays.calls {
		if call.nodeID == nodeID && call.upstreamCallID == relayID {
			found = call
			break
		}
	}
	a.mediaRelays.mu.Unlock()
	if found == nil {
		return
	}
	found.closeOnce.Do(func() { close(found.stopCh) })
	_ = found.sink.Close()
	if a.mediaRelays.take(found.upstreamCallID) == nil {
		return
	}
	a.sayByeUpstream(found)
}

// sayByeUpstream sends a BYE to the peer that opened the dialog.
func (a *Acceptor) sayByeUpstream(call *relayCall) bool {
	p, ok := a.platformByID(call.nodeID)
	if !ok || p.tr == nil || call.upstreamPeer == "" {
		return false
	}
	req, err := model.NewRequest("BYE", "sip:"+p.id.String()+"@"+call.upstreamPeer, []model.Header{
		model.NewHeader("From", "<sip:"+p.id.String()+"@"+p.realm+">;tag="+randomTag()),
		model.NewHeader("To", "<sip:"+p.id.String()+"@"+p.realm+">"),
		model.NewHeader("Call-ID", call.upstreamCallID),
		model.NewHeader("CSeq", "2 BYE"),
		model.NewHeader("Max-Forwards", "70"),
	}, "")
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := p.tr.Send(ctx, req, call.upstreamPeer); err != nil {
		a.log.Warn("relay cannot send BYE upstream",
			"node_id", call.nodeID.String(), "call_id", call.upstreamCallID, "error", err.Error())
		return false
	}
	a.log.Debug("relay ended downstream; BYE sent upstream",
		"node_id", call.nodeID.String(), "call_id", call.upstreamCallID)
	return true
}
