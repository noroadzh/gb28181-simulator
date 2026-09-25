// Package app — platform-large registration acceptance.
package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// ErrNoDownstreamAccount is the reason a REGISTER is refused when the
// platform has no account for the device it came from. It is logged, never
// sent on the wire: the peer learns only that it was refused (403).
var ErrNoDownstreamAccount = fmt.Errorf("app: no account for the downstream")

// Sweeping bounds how stale the online table may get. A device is thrown
// out soon after the lifetime it was granted lapses — half that lifetime
// at the latest — but a platform with a long window must not spend its
// time re-reading a large table, so the beat is kept inside these bounds.
const (
	sweepMinInterval = time.Second
	sweepMaxInterval = 30 * time.Second
)

// contentTypeMANSCDP is what a MANSCDP body announces itself as, in the
// spelling GB/T 28181 uses.
const contentTypeMANSCDP = "Application/MANSCDP+XML"

// inviteTimers maps Call-ID → *time.Timer for pending INVITE dialogs waiting
// for ACK. The splitTransport dispatch checks this map to route ACK requests
// to the serving half so the expiry timer can be cancelled.
var inviteTimers sync.Map

// Acceptor is the UAS half of the simulator: for every platform-large node
// it runs one goroutine that receives REGISTERs on that node's transport,
// challenges them the way GB/T 28181 §L.2 prescribes, and records the
// devices that make it through.
//
// It depends only on domain ports. The transport is handed in per node
// because every node owns its own listener, and the root context is the
// process's rather than a request's: serving must outlive the call that
// started the node. Close ends everything.
//
// Besides registrations it answers the MANSCDP messages a registered
// downstream sends — keepalives refresh the row, catalog queries get the
// online table — and sweeps out rows whose granted lifetime lapsed.
//
// Optional dependencies (set through WithX methods) enable the supplementary
// platform-small capabilities: dialog tracking, playback, subscription and
// media-status handling.
type Acceptor struct {
	ctx           context.Context
	clock         port.Clock
	challenger    port.Challenger
	authenticator port.Authenticator
	creds         port.CredentialStore
	devices       port.DownstreamRegistry
	manscdp       port.MANSCDPCodec
	newTicker     port.TickerFactory
	log           *slog.Logger

	dialogs      *DialogManager
	playback     port.PlaybackPort
	subscribe    port.SubscribePort
	mediaStatus  port.MediaStatusPort
	mediaService *MediaService
	pipelines    map[string]*InboundPipeline

	mu      sync.Mutex
	serving map[string]*platform
}

// platform is one node's serving goroutine and the settings it serves with.
type platform struct {
	id         model.NodeID
	tr         port.SIPTransport
	realm      string
	policy     model.ExpiresPolicy
	sweepEvery time.Duration
	cancel     context.CancelFunc
	done       chan struct{}

	// subscribers tracks upstream catalog subscriptions. The map key is the
	// Call-ID of the SUBSCRIBE; the value is the subscriber's peer and expiry.
	subscribers map[string]*catalogSub
}

// catalogSub is one active upstream catalog subscription.
type catalogSub struct {
	peer      string
	fromValue string
	expiresAt time.Time
	timer     *time.Timer
}

// NewAcceptor builds an Acceptor whose serving is bounded by ctx.
// challenger, authenticator, creds, devices and manscdp are required;
// clock, newTicker and log fall back to the real clock, a real ticker and
// the default slog logger.
func NewAcceptor(
	ctx context.Context,
	clock port.Clock,
	challenger port.Challenger,
	authenticator port.Authenticator,
	creds port.CredentialStore,
	devices port.DownstreamRegistry,
	manscdp port.MANSCDPCodec,
	newTicker port.TickerFactory,
	log *slog.Logger,
) (*Acceptor, error) {
	if ctx == nil {
		return nil, fmt.Errorf("app: acceptor requires a root context")
	}
	if challenger == nil {
		return nil, fmt.Errorf("app: acceptor requires a Challenger")
	}
	if authenticator == nil {
		return nil, fmt.Errorf("app: acceptor requires an Authenticator")
	}
	if creds == nil {
		return nil, fmt.Errorf("app: acceptor requires a CredentialStore")
	}
	if devices == nil {
		return nil, fmt.Errorf("app: acceptor requires a DownstreamRegistry")
	}
	if manscdp == nil {
		return nil, fmt.Errorf("app: acceptor requires a MANSCDPCodec")
	}
	if clock == nil {
		clock = realClock{}
	}
	if newTicker == nil {
		newTicker = realTickerFactory()
	}
	if log == nil {
		log = slog.Default()
	}
	return &Acceptor{
		ctx:           ctx,
		clock:         clock,
		challenger:    challenger,
		authenticator: authenticator,
		creds:         creds,
		devices:       devices,
		manscdp:       manscdp,
		newTicker:     newTicker,
		log:           log,
		serving:       make(map[string]*platform),
		pipelines:     make(map[string]*InboundPipeline),
	}, nil
}

// WithDialogs attaches the dialog manager used to track INVITE dialogs. It
// is optional: an acceptor without one still serves REGISTERs and MESSAGEs,
// but cannot track INVITEs.
func (a *Acceptor) WithDialogs(d *DialogManager) *Acceptor {
	a.dialogs = d
	return a
}

// WithPlayback attaches the playback port. When set, downstream PLAY/
// playback MESSAGEs are routed here.
func (a *Acceptor) WithPlayback(p port.PlaybackPort) *Acceptor {
	a.playback = p
	return a
}

// WithSubscribe attaches the subscription port for catalog-change notifies.
func (a *Acceptor) WithSubscribe(s port.SubscribePort) *Acceptor {
	a.subscribe = s
	return a
}

// WithMediaStatus attaches the media-status notify handler.
func (a *Acceptor) WithMediaStatus(m port.MediaStatusPort) *Acceptor {
	a.mediaStatus = m
	return a
}

// WithMediaService attaches the media service used to create inbound
// media pipelines for INVITE dialogs. When set, handleInvite will parse
// the SDP, create an InboundPipeline, and wire it to the Dialog lifecycle.
func (a *Acceptor) WithMediaService(svc *MediaService) *Acceptor {
	a.mediaService = svc
	return a
}

// Serve starts accepting registrations for id over tr. realm is what the
// node challenges in and policy bounds the lifetime it grants; an
// unconstructed policy is refused so a mis-wired node cannot grant zero.
//
// Serving a node that already serves replaces the old goroutine, so a
// restarted node never ends up with two of them.
func (a *Acceptor) Serve(id model.NodeID, tr port.SIPTransport, realm string, policy model.ExpiresPolicy) error {
	if tr == nil {
		return fmt.Errorf("app: acceptor for node %s requires a transport", id)
	}
	if strings.TrimSpace(realm) == "" {
		return fmt.Errorf("app: acceptor for node %s requires a realm", id)
	}
	if !policy.HasPolicy() {
		return fmt.Errorf("app: acceptor for node %s requires an expires policy", id)
	}
	key := id.String()

	a.Stop(id)

	a.mu.Lock()
	defer a.mu.Unlock()
	runCtx, cancel := context.WithCancel(a.ctx)
	p := &platform{
		id:         id,
		tr:         tr,
		realm:      realm,
		policy:     policy,
		sweepEvery: sweepIntervalFor(policy),
		cancel:     cancel,
		done:       make(chan struct{}),
		subscribers: make(map[string]*catalogSub),
	}
	a.serving[key] = p
	go a.run(runCtx, p)
	return nil
}

// Stop ends the serving goroutine of id and waits for it to leave, so a
// caller that goes on to release the listener cannot race a response being
// written to it. Stopping a node that does not serve is a no-op.
func (a *Acceptor) Stop(id model.NodeID) {
	key := id.String()
	a.mu.Lock()
	p := a.serving[key]
	delete(a.serving, key)
	a.mu.Unlock()
	if p == nil {
		return
	}
	p.cancel()
	<-p.done
	// A platform that stopped serving can no longer reach the devices it
	// accepted, so it must not go on reporting them as online.
	if err := a.devices.Clear(a.ctx, id); err != nil {
		a.log.Warn("cannot clear the online device table",
			"node_id", id.String(), "error", err.Error())
	}
}

// Devices returns the online device table of id, ordered by device id. An
// unknown or stopped node has none.
func (a *Acceptor) Devices(ctx context.Context, id model.NodeID) []model.DownstreamDevice {
	return a.devices.List(ctx, id)
}

// Device returns one row of the online device table of id.
func (a *Acceptor) Device(ctx context.Context, id model.NodeID, deviceID string) (model.DownstreamDevice, bool) {
	return a.devices.Lookup(ctx, id, deviceID)
}

// Serving reports how many nodes are currently served. Tests use it to
// assert that serving really stopped.
func (a *Acceptor) Serving() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.serving)
}

// Close implements io.Closer: it ends every node's serving goroutine, so
// the process can shut down without leaving one behind.
func (a *Acceptor) Close() error {
	a.mu.Lock()
	open := make([]*platform, 0, len(a.serving))
	for key, p := range a.serving {
		open = append(open, p)
		delete(a.serving, key)
	}
	a.mu.Unlock()
	for _, p := range open {
		p.cancel()
		<-p.done
	}
	return nil
}

// Compile-time check: the composition root closes the Acceptor on shutdown.
var _ io.Closer = (*Acceptor)(nil)

// run is one node's serving loop. It reads until the transport or the
// context ends; every REGISTER and every understood MESSAGE gets exactly
// one answer.
//
// Besides serving it runs the sweeper that throws out devices whose granted
// lifetime lapsed. The sweeper is a goroutine of its own because reading
// from the transport blocks, and it is joined before run returns so a
// stopped node owns no goroutine and touches no table.
func (a *Acceptor) run(ctx context.Context, p *platform) {
	defer close(p.done)
	// Leaving the loop for any reason — context cancelled, transport
	// closed, read failed — must end the sweeper with it.
	defer p.cancel()
	swept := make(chan struct{})
	go func() {
		defer close(swept)
		a.sweepLoop(ctx, p)
	}()
	defer func() { <-swept }()

	for {
		msg, peer, err := p.tr.Receive(ctx)
		if err != nil {
			if ctx.Err() == nil {
				a.log.Debug("platform stopped receiving",
					"node_id", p.id.String(), "error", err.Error())
			}
			return
		}
		if !msg.IsRequest() {
			continue
		}
		var resp model.Message
		switch msg.Method() {
		case "REGISTER":
			resp, err = a.handle(ctx, p, msg, peer)
			if err != nil {
				a.log.Error("cannot answer a REGISTER",
					"node_id", p.id.String(), "peer", peer, "error", err.Error())
				continue
			}
		case "MESSAGE":
			var answered bool
			resp, answered = a.handleMessage(ctx, p, msg, peer)
			if !answered {
				continue
			}
		case "INVITE":
			resp, err = a.handleInvite(ctx, p, msg, peer)
			if err != nil {
				a.log.Warn("cannot answer an INVITE",
					"node_id", p.id.String(), "peer", peer, "error", err.Error())
				continue
			}
		case "ACK":
			a.handleAck(ctx, p, msg, peer)
			continue
		case "BYE":
			resp, err = a.handleBye(ctx, p, msg, peer)
			if err != nil {
				a.log.Warn("cannot answer a BYE",
					"node_id", p.id.String(), "peer", peer, "error", err.Error())
				continue
			}
		case "OPTIONS":
			resp, err = a.handleOptions(ctx, p, msg, peer)
			if err != nil {
				a.log.Warn("cannot answer OPTIONS",
					"node_id", p.id.String(), "peer", peer, "error", err.Error())
				continue
			}
		case "SUBSCRIBE":
			resp, err = a.handleSubscribe(ctx, p, msg, peer)
			if err != nil {
				a.log.Warn("cannot answer a SUBSCRIBE",
					"node_id", p.id.String(), "peer", peer, "error", err.Error())
				continue
			}
		default:
			// A method this platform does not serve must not be
			// answered with an error, so it is dropped quietly.
			a.log.Debug("ignoring an unsupported request",
				"node_id", p.id.String(), "method", msg.Method(), "peer", peer)
			continue
		}
		if err := p.tr.Send(ctx, resp, peer); err != nil {
			a.log.Warn("cannot send the answer",
				"node_id", p.id.String(), "peer", peer,
				"method", msg.Method(), "error", err.Error())
		}
	}
}

// sweepLoop wakes the sweeper on the node's beat until the context ends.
func (a *Acceptor) sweepLoop(ctx context.Context, p *platform) {
	ticker := a.newTicker(p.sweepEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			a.sweep(ctx, p)
		}
	}
}

// sweep throws out every device of p whose granted lifetime has lapsed. A
// device that keeps registering is never touched; one that went silent
// disappears, so the catalog stops reporting it as online.
func (a *Acceptor) sweep(ctx context.Context, p *platform) {
	now := a.clock.Now()
	changed := false
	for _, d := range a.devices.List(ctx, p.id) {
		expiresAt := d.ExpiresAt()
		if expiresAt.IsZero() || now.Before(expiresAt) {
			continue
		}
		if err := a.devices.Remove(ctx, p.id, d.DeviceID()); err != nil {
			a.log.Warn("cannot sweep an expired device",
				"node_id", p.id.String(), "device_id", d.DeviceID(), "error", err.Error())
			continue
		}
		a.log.Info("downstream timed out", "node_id", p.id.String(),
			"device_id", d.DeviceID(), "overdue", now.Sub(expiresAt).Round(time.Second).String())
		changed = true
	}
	if changed {
		a.notifyCatalogChange(ctx, p)
	}
}

// sweepIntervalFor turns a node's expires window into the beat its sweeper
// runs on: half the shortest lifetime it grants, so a device is thrown out
// within half a lifetime of going silent, inside the bounds above.
func sweepIntervalFor(policy model.ExpiresPolicy) time.Duration {
	d := time.Duration(policy.Min()) * time.Second / 2
	if d < sweepMinInterval {
		return sweepMinInterval
	}
	if d > sweepMaxInterval {
		return sweepMaxInterval
	}
	return d
}

// handleMessage answers the MANSCDP messages a downstream sends. The second
// result is false when there is nothing to answer: an unreadable body, or a
// command this platform does not serve. A platform that cannot read a
// message stays silent rather than answering an error the peer will not
// understand.
func (a *Acceptor) handleMessage(
	ctx context.Context,
	p *platform,
	req model.Message,
	peer string,
) (model.Message, bool) {
	notify, err := a.manscdp.DecodeNotify(req.Body())
	if err != nil {
		a.log.Debug("ignoring an unreadable MESSAGE",
			"node_id", p.id.String(), "peer", peer, "error", err.Error())
		return model.Message{}, false
	}
	switch {
	case notify.IsKeepalive():
		return a.refresh(ctx, p, req, notify)
	case notify.IsCatalogQuery():
		return a.answerCatalog(ctx, p, req, notify)
	case notify.IsMediaStatus():
		return a.handleMediaStatus(ctx, p, req, notify)
	case notify.IsPlaybackControl():
		return a.handlePlaybackControl(ctx, p, req, notify)
	default:
		a.log.Debug("ignoring an unsupported command",
			"node_id", p.id.String(), "cmd", notify.CmdType(), "device_id", notify.DeviceID())
		return model.Message{}, false
	}
}

// handleInvite answers an incoming INVITE request. For now it creates or
// reuses a Dialog for the Call-ID, echoes the received body as the answer,
// and returns 200 OK. A real media pipeline will be wired in Change #8.
func (a *Acceptor) handleInvite(
	ctx context.Context,
	p *platform,
	req model.Message,
	peer string,
) (model.Message, error) {
	callID := headerValue(req, "Call-ID")
	if callID == "" {
		return buildResponse(400, "Bad Request", req, nil, "")
	}
	if a.dialogs != nil {
		if _, err := a.dialogs.Get(callID); !err {
			a.dialogs.Create(callID)
			a.log.Debug("INVITE dialog created", "node_id", p.id.String(), "call_id", callID)
		}
	}
	mediaType, _, _, sdpErr := parseSDPMetadata(req.Body())
	switch {
	case sdpErr != nil:
		a.log.Warn("INVITE SDP unreadable", "node_id", p.id.String(),
			"call_id", callID, "error", sdpErr.Error())
		if a.dialogs != nil {
			a.dialogs.Terminate(callID)
		}
		return buildResponse(400, "Bad Request", req, nil, "")
	case mediaType == "":
		a.log.Debug("INVITE accepted without media section",
			"node_id", p.id.String(), "call_id", callID)
	default:
		a.createInboundPipeline(ctx, p, callID, mediaType, req.Body())
		a.watchInviteExpiry(p, callID)
	}
	hdrs := []model.Header{
		model.NewHeader("Contact", "<sip:"+p.id.String()+"@"+p.realm+">"),
		model.NewHeader("Allow", "REGISTER, MESSAGE, INVITE, ACK, BYE, OPTIONS, SUBSCRIBE"),
	}
	return buildResponse(200, "OK", req, hdrs, req.Body())
}

// watchInviteExpiry schedules a 30 s timer for the INVITE dialog. If the ACK
// is not received in time the dialog is terminated, which tears down the
// inbound media pipeline through the manager's onDelete callback.
func (a *Acceptor) watchInviteExpiry(p *platform, callID string) {
	timer := time.NewTimer(30 * time.Second)
	inviteTimers.Store(callID, timer)
	go func() {
		<-timer.C
		a.log.Debug("INVITE expired without ACK, terminating dialog",
			"node_id", p.id.String(), "call_id", callID)
		if a.dialogs != nil {
			a.dialogs.Terminate(callID)
		}
		inviteTimers.Delete(callID)
	}()
}

// cancelInviteExpiry stops a pending INVITE expiry timer and removes it.
func (a *Acceptor) cancelInviteExpiry(callID string) {
	if v, ok := inviteTimers.LoadAndDelete(callID); ok {
		if t, ok := v.(*time.Timer); ok {
			t.Stop()
		}
	}
}

// handleAck advances the dialog for an INVITE transaction but does not answer:
// ACK carries no response in the SIP request/response model.
func (a *Acceptor) handleAck(
	ctx context.Context,
	p *platform,
	req model.Message,
	peer string,
) {
	callID := headerValue(req, "Call-ID")
	if callID == "" || a.dialogs == nil {
		return
	}
	if d, err := a.dialogs.Get(callID); err && !d.IsTerminated() {
		if _, err := a.dialogs.Confirm(callID, nil); err != nil {
			a.log.Warn("ACK could not confirm dialog",
				"node_id", p.id.String(), "call_id", callID, "error", err.Error())
		}
	}
}

// handleBye terminates the dialog identified by the Call-ID and answers with
// 200 OK.
func (a *Acceptor) handleBye(
	ctx context.Context,
	p *platform,
	req model.Message,
	peer string,
) (model.Message, error) {
	callID := headerValue(req, "Call-ID")
	if callID != "" && a.dialogs != nil {
		a.dialogs.Terminate(callID)
		a.closePipeline(callID)
		a.log.Debug("BYE terminated dialog", "node_id", p.id.String(), "call_id", callID)
	}
	return buildResponse(200, "OK", req, nil, "")
}

// handleOptions answers a keepalive probe. The Allow header advertises what
// the platform serves; a minimal 200 OK is enough for the peer to mark the
// link healthy.
func (a *Acceptor) handleOptions(
	ctx context.Context,
	p *platform,
	req model.Message,
	peer string,
) (model.Message, error) {
	hdrs := []model.Header{
		model.NewHeader("Allow", "REGISTER, MESSAGE, INVITE, ACK, BYE, OPTIONS, SUBSCRIBE"),
		model.NewHeader("Accept", "Application/MANSCDP+XML"),
	}
	return buildResponse(200, "OK", req, hdrs, "")
}

// handleSubscribe accepts a catalog-subscription request from a downstream or
// an upstream platform. The subscriber is recorded with its Expires value, a
// NOTIFY is sent with the current device list, and a 200 OK is answered. A
// SUBSCRIBE for an event other than `catalog` is rejected with 489 Bad Event;
// a SUBSCRIBE with Expires 0 closes the matching subscription.
func (a *Acceptor) handleSubscribe(
	ctx context.Context,
	p *platform,
	req model.Message,
	peer string,
) (model.Message, error) {
	event := headerValue(req, "Event")
	// Only "catalog" subscription is supported by this node.
	if event != "" && !strings.EqualFold(event, "catalog") {
		a.log.Debug("SUBSCRIBE rejected: unsupported Event",
			"node_id", p.id.String(), "event", event)
		return buildResponse(489, "Bad Event", req, nil, "")
	}

	callID := headerValue(req, "Call-ID")
	expires, hasExpires := requestedExpires(req)
	fromValue := headerValue(req, "From")

	// Expires=0 without a known call-ID is just a misformed goodbye — answer
	// 200 OK and move on; there is nothing to remove.
	if hasExpires && expires == 0 {
		if callID != "" {
			a.removeSubscriber(p, callID)
		}
		return buildResponse(200, "OK", req, []model.Header{
			model.NewHeader("Expires", "0"),
		}, "")
	}

	if callID == "" {
		return buildResponse(400, "Bad Request", req, nil, "")
	}

	if expires == 0 {
		expires = 3600
	}
	a.recordSubscriber(p, callID, peer, fromValue, expires)
	// Send an initial NOTIFY so the subscriber does not have to wait for the
	// next event to learn the current roster.
	a.sendCatalogNotify(ctx, p, callID, peer, fromValue, "")
	a.log.Debug("SUBSCRIBE recorded",
		"node_id", p.id.String(), "call_id", callID, "expires", expires)
	return buildResponse(200, "OK", req, []model.Header{
		model.NewHeader("Expires", strconv.FormatUint(uint64(expires), 10)),
	}, "")
}

// recordSubscriber registers an upstream subscriber with the duration its
// SUBSCRIBE asked for. When the timer fires the subscriber is dropped.
func (a *Acceptor) recordSubscriber(
	p *platform,
	callID, peer, fromValue string,
	expires uint32,
) {
	a.removeSubscriber(p, callID)
	sub := &catalogSub{
		peer:      peer,
		fromValue: fromValue,
		expiresAt: a.clock.Now().Add(time.Duration(expires) * time.Second),
	}
	sub.timer = time.AfterFunc(time.Duration(expires)*time.Second, func() {
		a.removeSubscriber(p, callID)
		a.log.Debug("SUBSCRIBE expired",
			"node_id", p.id.String(), "call_id", callID)
	})
	p.subscribers[callID] = sub
}

// removeSubscriber drops a subscriber by Call-ID and stops its expiry timer.
// Safe to call for an unknown Call-ID.
func (a *Acceptor) removeSubscriber(p *platform, callID string) {
	if sub, ok := p.subscribers[callID]; ok {
		if sub.timer != nil {
			sub.timer.Stop()
		}
		delete(p.subscribers, callID)
	}
}

// notifyCatalogChange sends a fresh catalog NOTIFY to every active
// subscriber. It is called from the device-register and device-sweep paths.
func (a *Acceptor) notifyCatalogChange(ctx context.Context, p *platform) {
	for callID, sub := range p.subscribers {
		a.sendCatalogNotify(ctx, p, callID, sub.peer, sub.fromValue, "")
	}
}

// sendCatalogNotify renders the current device list and sends it as a NOTIFY
// to one subscriber. An empty body signals an end-of-stream notification so
// subscribers can confirm graceful close.
func (a *Acceptor) sendCatalogNotify(
	ctx context.Context,
	p *platform,
	callID, peer, fromValue, body string,
) {
	devs := a.devices.List(ctx, p.id)
	items := make([]model.CatalogItem, 0, len(devs))
	for _, d := range devs {
		item, err := model.NewCatalogItemFromDevice(d)
		if err != nil {
			continue
		}
		items = append(items, item)
	}
	sn := uint32(1)
	catalog, err := model.NewCatalog(p.id.String(), sn, items)
	if err != nil {
		a.log.Warn("cannot build catalog for NOTIFY",
			"node_id", p.id.String(), "error", err.Error())
		return
	}
	payload, err := a.manscdp.MarshalCatalog(catalog)
	if err != nil {
		a.log.Warn("cannot marshal catalog for NOTIFY",
			"node_id", p.id.String(), "error", err.Error())
		return
	}
	if body != "" {
		payload = body
	}
	domain := p.realm
	fromTag := randomTag()
	toTag := randomTag()
	msg, err := model.NewRequest("NOTIFY", "sip:"+peer, []model.Header{
		model.NewHeader("From", "<sip:"+p.id.String()+"@"+domain+">;tag="+fromTag),
		model.NewHeader("To", fromValue+";tag="+toTag),
		model.NewHeader("Call-ID", callID),
		model.NewHeader("CSeq", "1 NOTIFY"),
		model.NewHeader("Event", "catalog"),
		model.NewHeader("Content-Type", "Application/MANSCDP+XML"),
		model.NewHeader("Max-Forwards", "70"),
	}, payload)
	if err != nil {
		a.log.Warn("cannot build NOTIFY",
			"node_id", p.id.String(), "error", err.Error())
		return
	}
	if err := p.tr.Send(ctx, msg, peer); err != nil {
		a.log.Warn("cannot send NOTIFY",
			"node_id", p.id.String(), "peer", peer, "error", err.Error())
		return
	}
	a.log.Debug("NOTIFY sent",
		"node_id", p.id.String(), "peer", peer, "call_id", callID, "items", len(items))
}

// handleMediaStatus processes a downstream MediaStatus notify. The port is
// optional: when absent the notify is acknowledged but otherwise ignored.
func (a *Acceptor) handleMediaStatus(
	ctx context.Context,
	p *platform,
	req model.Message,
	notify model.Notify,
) (model.Message, bool) {
	if a.mediaStatus != nil {
		report, err := model.NewMediaStatusReport(
			notify.DeviceID(),
			notify.Status(),
			notify.SN(),
		)
		if err != nil {
			a.log.Warn("media-status report malformed",
				"node_id", p.id.String(), "device_id", notify.DeviceID(), "error", err.Error())
		} else if err := a.mediaStatus.HandleMediaStatus(ctx, report); err != nil {
			a.log.Warn("media-status handler failed",
				"node_id", p.id.String(), "device_id", notify.DeviceID(), "error", err.Error())
		}
	}
	resp, err := buildResponse(200, "OK", req, nil, "")
	if err != nil {
		a.log.Warn("cannot answer a MediaStatus",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	return resp, true
}

// handlePlaybackControl forwards a downstream PlaybackControl notify to the
// playback port. When the port is absent the notify is merely acknowledged.
func (a *Acceptor) handlePlaybackControl(
	ctx context.Context,
	p *platform,
	req model.Message,
	notify model.Notify,
) (model.Message, bool) {
	if a.playback != nil {
		// PlaybackControl detail is carried in the MANSCDP body; the notify
		// gives us the device id, sn and a status hint. Real command fields
		// are extracted by the adapter when needed.
		a.log.Debug("playback control notify (port absent)",
			"node_id", p.id.String(), "device_id", notify.DeviceID())
	}
	resp, err := buildResponse(200, "OK", req, nil, "")
	if err != nil {
		a.log.Warn("cannot answer a PlaybackControl",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	return resp, true
}

// refresh answers a keepalive: the device's row is stamped as seen and the
// granted lifetime is left alone, because a heartbeat is not a
// re-registration and must not extend it.
//
// A keepalive from a device that is not in the table is ignored rather than
// answered: a platform that grants nothing cannot say "OK" to it.
func (a *Acceptor) refresh(
	ctx context.Context,
	p *platform,
	req model.Message,
	notify model.Notify,
) (model.Message, bool) {
	deviceID := notify.DeviceID()
	dev, ok := a.devices.Lookup(ctx, p.id, deviceID)
	if !ok {
		a.log.Warn("ignoring a keepalive from a device that is not registered",
			"node_id", p.id.String(), "device_id", deviceID)
		return model.Message{}, false
	}
	now := a.clock.Now()
	if err := a.devices.Upsert(ctx, p.id, dev.WithSeen(now)); err != nil {
		a.log.Warn("cannot refresh a downstream",
			"node_id", p.id.String(), "device_id", deviceID, "error", err.Error())
		return model.Message{}, false
	}
	a.log.Debug("downstream keepalive", "node_id", p.id.String(),
		"device_id", deviceID, "sn", notify.SN())
	resp, err := buildResponse(200, "OK", req, nil, "")
	if err != nil {
		a.log.Warn("cannot answer a keepalive",
			"node_id", p.id.String(), "device_id", deviceID, "error", err.Error())
		return model.Message{}, false
	}
	return resp, true
}

// answerCatalog answers a catalog query with the online device table of the
// node it arrived at — never with another node's, so two platforms sharing
// a process cannot see each other's downstreams.
//
// A query without an SN is ignored: the answer could not be matched to the
// question, and a platform guessing one would only confuse the peer.
func (a *Acceptor) answerCatalog(
	ctx context.Context,
	p *platform,
	req model.Message,
	notify model.Notify,
) (model.Message, bool) {
	if !notify.HasSN() {
		a.log.Warn("ignoring a catalog query without a sequence number",
			"node_id", p.id.String(), "device_id", notify.DeviceID())
		return model.Message{}, false
	}
	catalog, err := model.NewCatalogFromDevices(p.id.String(), notify.SN(), a.devices.List(ctx, p.id))
	if err != nil {
		a.log.Warn("cannot build a catalog answer",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	body, err := a.manscdp.MarshalCatalog(catalog)
	if err != nil {
		a.log.Warn("cannot render a catalog answer",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	a.log.Debug("answering a catalog query", "node_id", p.id.String(),
		"sn", notify.SN(), "sum", catalog.SumNum())
	resp, err := buildResponse(200, "OK", req, []model.Header{
		model.NewHeader("Content-Type", contentTypeMANSCDP),
	}, body)
	if err != nil {
		a.log.Warn("cannot answer a catalog query",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	return resp, true
}

// handle decides one REGISTER: challenge it, refuse it, or grant it and
// record the device.
func (a *Acceptor) handle(ctx context.Context, p *platform, req model.Message, peer string) (model.Message, error) {
	username := requestUser(req)
	if username == "" {
		// Without a caller there is nobody to look up and nothing worth
		// challenging: refuse rather than hand out a nonce to a stranger.
		a.log.Warn("refusing a REGISTER without a caller", "node_id", p.id.String())
		return buildResponse(403, "Forbidden", req, nil, "")
	}
	if _, ok := req.Header("Authorization"); !ok {
		a.log.Debug("challenging a REGISTER", "node_id", p.id.String(), "username", username)
		return a.challenge(req, p)
	}
	cred, ok := a.creds.Lookup(p.id, username)
	if !ok {
		a.log.Warn("refusing a REGISTER for an unknown account",
			"node_id", p.id.String(), "username", username)
		return buildResponse(403, "Forbidden", req, nil, "")
	}
	if err := a.authenticator.Verify(req, cred); err != nil {
		// A header we cannot read may be a transient botch, so it is
		// worth another challenge; one that reads and does not match
		// is not (GB/T 28181 §L.2 forbids re-challenging).
		if errors.Is(err, port.ErrMalformedCredentials) {
			a.log.Warn("re-challenging an unreadable Authorization",
				"node_id", p.id.String(), "username", username, "error", err.Error())
			return a.challenge(req, p)
		}
		a.log.Warn("refusing a REGISTER with a bad response",
			"node_id", p.id.String(), "username", username, "error", err.Error())
		return buildResponse(403, "Forbidden", req, nil, "")
	}
	return a.grant(ctx, p, req, username, peer)
}

// challenge answers 401 with a fresh WWW-Authenticate for the node's realm.
func (a *Acceptor) challenge(req model.Message, p *platform) (model.Message, error) {
	ch, err := a.challenger.Challenge(p.realm)
	if err != nil {
		return model.Message{}, fmt.Errorf("app: challenge for node %s: %w", p.id, err)
	}
	value := fmt.Sprintf(`Digest realm=%q, nonce=%q, qop="auth", algorithm=%s`,
		ch.Realm(), ch.Nonce(), ch.Algorithm())
	if ch.Opaque() != "" {
		value += fmt.Sprintf(`, opaque=%q`, ch.Opaque())
	}
	return buildResponse(401, "Unauthorized", req, []model.Header{
		model.NewHeader("WWW-Authenticate", value),
	}, "")
}

// grant answers 200 OK and records the device — or, when the request asks
// for no lifetime at all, forgets it instead.
func (a *Acceptor) grant(
	ctx context.Context,
	p *platform,
	req model.Message,
	username string,
	peer string,
) (model.Message, error) {
	now := a.clock.Now()
	requested, hasExpires := requestedExpires(req)
	if hasExpires && requested == 0 {
		if err := a.devices.Remove(ctx, p.id, username); err != nil {
			a.log.Warn("cannot forget a device that asked to leave",
				"node_id", p.id.String(), "device_id", username, "error", err.Error())
		} else {
			a.log.Info("downstream unregistered", "node_id", p.id.String(), "device_id", username)
		}
		a.notifyCatalogChange(ctx, p)
		return buildResponse(200, "OK", req, []model.Header{
			model.NewHeader("Expires", "0"),
			model.NewHeader("Date", httpDate(now)),
		}, "")
	}

	granted := p.policy.Negotiate(requested)
	contact := ""
	if h, ok := req.Header("Contact"); ok {
		contact = h.Value()
	}
	gbVersion := ""
	if h, ok := req.Header("X-GB-Ver"); ok {
		gbVersion = h.Value()
	}
	// The address the request came from is what the platform can answer;
	// the Contact is only what the device claims, so it is stored but not
	// trusted as the address.
	addr := peer
	if addr == "" {
		addr = hostOfContact(contact)
	}
	dev, err := model.NewDownstreamDevice(model.DownstreamDeviceParams{
		DeviceID:  username,
		Addr:      addr,
		Contact:   contact,
		Transport: transportOf(req),
		GBVersion: gbVersion,
		Now:       now,
	})
	if err != nil {
		return model.Message{}, fmt.Errorf("app: record device %s: %w", username, err)
	}
	if err := a.devices.Upsert(ctx, p.id, dev.WithGranted(granted, now)); err != nil {
		return model.Message{}, fmt.Errorf("app: record device %s: %w", username, err)
	}
	a.log.Info("downstream registered", "node_id", p.id.String(),
		"device_id", username, "expires", granted)
	a.notifyCatalogChange(ctx, p)

	hdrs := []model.Header{
		model.NewHeader("Expires", strconv.FormatUint(uint64(granted), 10)),
		model.NewHeader("Date", httpDate(now)),
	}
	if contact != "" {
		hdrs = append(hdrs, model.NewHeader("Contact", contact))
	}
	return buildResponse(200, "OK", req, hdrs, "")
}

// buildResponse echoes the transaction headers a response must carry and
// adds the ones the answer itself needs. Via, From, To (tagged), Call-ID
// and CSeq are what let the peer match the answer to its transaction. body
// is the message body, "" for answers that carry none.
func buildResponse(
	status int,
	reason string,
	req model.Message,
	extra []model.Header,
	body string,
) (model.Message, error) {
	hdrs := make([]model.Header, 0, 6+len(extra))
	for _, name := range []string{"Via", "From", "To", "Call-ID", "CSeq"} {
		h, ok := req.Header(name)
		if !ok {
			continue
		}
		if name == "To" && !strings.Contains(strings.ToLower(h.Value()), "tag=") {
			hdrs = append(hdrs, model.NewHeader("To", h.Value()+";tag="+randomTag()))
			continue
		}
		hdrs = append(hdrs, h)
	}
	hdrs = append(hdrs, extra...)
	return model.NewResponse(status, reason, hdrs, body)
}

// headerValue returns the value of a header by name, or "" when absent.
func headerValue(req model.Message, name string) string {
	h, ok := req.Header(name)
	if !ok {
		return ""
	}
	return h.Value()
}

// requestUser returns the user part of the request's From URI — the device
// id a GB/T 28181 device both calls itself and authenticates as. It falls
// back to To, and to "" when neither carries one.
func requestUser(req model.Message) string {
	for _, name := range []string{"From", "To"} {
		h, ok := req.Header(name)
		if !ok {
			continue
		}
		if user := sipUser(h.Value()); user != "" {
			return user
		}
	}
	return ""
}

// sipUser extracts the user part of a SIP URI inside a header value such as
// `<sip:34020000011310000001@3402000000>;tag=abc`.
func sipUser(value string) string {
	start := strings.Index(value, "<sip:")
	body := value
	if start >= 0 {
		body = value[start+len("<sip:"):]
	} else if i := strings.Index(value, "sip:"); i >= 0 {
		body = value[i+len("sip:"):]
	} else {
		return ""
	}
	if i := strings.IndexAny(body, "@>"); i > 0 {
		return body[:i]
	}
	return ""
}

// requestedExpires reads the lifetime the request asks for. The second
// result is false when the header is absent, which is not the same thing as
// asking for zero: zero is an explicit goodbye.
func requestedExpires(req model.Message) (uint32, bool) {
	h, ok := req.Header("Expires")
	if !ok {
		return 0, false
	}
	value := strings.TrimSpace(h.Value())
	if value == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(n), true
}

// transportOf reads the transport the request arrived over from its Via, so
// the online table can say how to reach the device.
func transportOf(req model.Message) string {
	h, ok := req.Header("Via")
	if !ok {
		return ""
	}
	// Via: SIP/2.0/UDP 127.0.0.1:15060;branch=...
	fields := strings.Fields(h.Value())
	for _, f := range fields {
		parts := strings.Split(f, "/")
		if len(parts) == 3 {
			return strings.ToLower(parts[2])
		}
	}
	return ""
}

// hostOfContact extracts "host:port" from a Contact such as
// `<sip:34020000011310000001@127.0.0.1:15060>`.
func hostOfContact(value string) string {
	i := strings.LastIndex(value, "@")
	if i < 0 {
		return ""
	}
	rest := value[i+1:]
	end := strings.IndexAny(rest, ">;")
	if end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest)
}

// httpDate renders a Date header the way SIP expects it: GMT, in the
// IMF-fixdate form RFC 3261 borrows from HTTP.
func httpDate(t time.Time) string {
	return t.UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")
}

// nullESWriter is a port.ESWriteCloser that discards everything; the media
// pipeline passes it as the ES-sink when no real consumer is configured yet.
type nullESWriter struct{}

func (nullESWriter) Read(ctx context.Context) (model.ESFrame, error) {
	return model.ESFrame{}, io.EOF
}
func (nullESWriter) Write(ctx context.Context, frame model.ESFrame) error { return nil }
func (nullESWriter) Close() error                                         { return nil }

// parseSDPMetadata scans the INVITE body for the first audio/video m= line
// and returns its media type, port, and protocol. An empty mediaType signals
// no media description was found.
func parseSDPMetadata(body string) (mediaType, portStr, protocol string, _ error) {
	if body == "" {
		return "", "", "", errors.New("empty body")
	}
	if !strings.Contains(body, "v=0") {
		return "", "", "", errors.New("SDP missing v=0")
	}
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "m=") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		m, p, rest := fields[0][2:], fields[1], fields[2:]
		switch m {
		case "audio", "video":
			if _, err := strconv.Atoi(p); err != nil || p == "0" {
				return "", "", "", errors.New("invalid media port: " + p)
			}
			proto := ""
			if len(rest) > 0 {
				proto = rest[0]
			}
			return m, p, proto, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", "", err
	}
	return "", "", "", nil
}

// extractMediaAddress reads the SDP for the first c=IN IP4/IP6 address.
// It returns the first non-empty address it finds or an empty string when
// the body carries no connection data.
func extractMediaAddress(body string) string {
	if body == "" {
		return ""
	}
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "c=") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			return strings.TrimSpace(fields[2])
		}
	}
	return ""
}

// createInboundPipeline asks the media service for an InboundPipeline for the
// INVITE session. The resulting pipeline is tracked in a.pipelines and will
// be closed when the dialog ends (via the DialogManager onDelete callback).
func (a *Acceptor) createInboundPipeline(
	ctx context.Context,
	_ *platform,
	callID, mediaType, sdpBody string,
) {
	media := a.mediaService
	if media == nil {
		return
	}
	address := extractMediaAddress(sdpBody)
	if address == "" {
		return
	}
	pl, plErr := media.NewInboundPipeline(uint32(0), nullESWriter{})
	if plErr != nil {
		a.log.Warn("cannot create inbound pipeline",
			"call_id", callID, "error", plErr.Error())
		return
	}
	a.mu.Lock()
	a.pipelines[callID] = pl
	a.mu.Unlock()
	a.log.Debug("inbound pipeline created",
		"call_id", callID, "media", mediaType, "address", address)
}

// closePipeline removes the pipeline entry from the map and closes it.
// Safe to call multiple times for the same callID.
func (a *Acceptor) closePipeline(callID string) {
	a.mu.Lock()
	pl, ok := a.pipelines[callID]
	if ok {
		delete(a.pipelines, callID)
	}
	a.mu.Unlock()
	if ok && pl != nil {
		pl.Close()
	}
}
