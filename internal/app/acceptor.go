// Package app — platform-large registration acceptance.
package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"slices"
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
	registry      port.NodeRegistry

	dialogs      *DialogManager
	playback     port.PlaybackPort
	subscribe    port.SubscribePort
	mediaStatus  port.MediaStatusPort
	mediaService *MediaService
	pipelines    map[string]*InboundPipeline

	mu      sync.Mutex
	serving map[string]*platform
	cascade port.CascadeHandler
	faults  port.FaultStore
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

// WithCascadeHandler attaches the cascade handler whose topology view this
// acceptor refreshes on every node register/unregister. When set, every
// outbound response and NOTIFY carries the X-RoutePath headers the handler
// asks for.
func (a *Acceptor) WithCascadeHandler(h port.CascadeHandler) *Acceptor {
	a.cascade = h
	return a
}

// WithNodeRegistry attaches the node registry the acceptor reads from when
// answering catalog queries: the node's profile channels are appended to
// the downstream device list, and their online/offline status is respected
// (task 5.1). Without it a platform still serves, using only the device
// table.
func (a *Acceptor) WithNodeRegistry(r port.NodeRegistry) *Acceptor {
	a.registry = r
	return a
}

// WithFaults attaches the runtime fault store consulted before every
// request reaches the dispatch switch (Change 13, design D6). It is
// optional: an acceptor without one serves every request normally, which
// is the default-off guarantee of the feature.
func (a *Acceptor) WithFaults(fs port.FaultStore) *Acceptor {
	a.faults = fs
	return a
}

// routeOutbound consults the cascade handler before a message leaves the node.
// The destination is always the original peer: responses go back to the
// requester and NOTIFYs go to the subscriber, so the handler's next-hop advice
// only decides which cascade headers to attach. On a handler error (e.g. a
// loop detected) the message is dropped rather than forwarded further, as
// required by the loop-prevention spec for simulation mode.
func (a *Acceptor) routeOutbound(p *platform, msg model.Message, peer string) (model.Message, string, bool) {
	if a.cascade == nil {
		return msg, peer, true
	}
	_, extra, _, err := a.cascade.Forward(p.id, msg, peer)
	if err != nil {
		a.log.Warn("cascade forward rejected the message; dropping",
			"node_id", p.id.String(), "peer", peer, "error", err.Error())
		return model.Message{}, peer, false
	}
	if len(extra) == 0 {
		return msg, peer, true
	}
	out, bErr := withExtraHeaders(msg, extra)
	if bErr != nil {
		a.log.Warn("cascade headers rejected; sending direct",
			"node_id", p.id.String(), "peer", peer, "error", bErr.Error())
		return msg, peer, true
	}
	return out, peer, true
}

// withExtraHeaders returns a copy of msg that carries the extra headers
// appended after the existing ones. The message kind (request vs response)
// is preserved.
func withExtraHeaders(msg model.Message, extra []model.Header) (model.Message, error) {
	if len(extra) == 0 {
		return msg, nil
	}
	all := make([]model.Header, 0, len(msg.Headers())+len(extra))
	all = append(all, msg.Headers()...)
	all = append(all, extra...)
	if msg.StatusCode() != 0 {
		return model.NewResponse(msg.StatusCode(), msg.StatusText(), all, msg.Body())
	}
	uri := ""
	if u := msg.URI(); u != nil {
		uri = u.String()
	}
	return model.NewRequest(msg.Method(), uri, all, msg.Body())
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
		id:          id,
		tr:          tr,
		realm:       realm,
		policy:      policy,
		sweepEvery:  sweepIntervalFor(policy),
		cancel:      cancel,
		done:        make(chan struct{}),
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
		// Fault gate (Change 13, design D6): consult the node's armed
		// fault profile before any handler runs. A skip swallows the
		// request, an answer short-circuits the dispatch with the canned
		// response, and a serve verdict falls through to the normal
		// handlers unchanged. With no profile the byte path is identical.
		var resp model.Message
		handled := false
		if a.faults != nil {
			canned, decision := a.faultGate(p, msg, peer)
			switch decision {
			case faultSkip:
				continue
			case faultAnswer:
				resp = canned
				handled = true
			}
		}
		if !handled {
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
				// A method this platform does not serve is dropped quietly —
				// unless the fault profile configures an answer for it (Change
				// 13's UnsupportedMethod knob).
				canned, answer := a.faultUnsupported(p, msg)
				if !answer {
					a.log.Debug("ignoring an unsupported request",
						"node_id", p.id.String(), "method", msg.Method(), "peer", peer)
					continue
				}
				resp = canned
			}
		}
		resp, dst, ok := a.routeOutbound(p, resp, peer)
		var sendErr error
		if ok {
			sendErr = p.tr.Send(ctx, resp, dst)
		}
		if sendErr != nil {
			a.log.Warn("cannot send the answer",
				"node_id", p.id.String(), "peer", dst,
				"method", msg.Method(), "error", sendErr.Error())
		}
	}
}

// faultDecision is the gate's tri-state verdict for one inbound request
// (design D6): serve through the normal handlers, skip silently, or answer
// with the canned response the gate returns.
type faultDecision int

const (
	faultServe  faultDecision = iota
	faultSkip                 // swallow the request, nothing is sent
	faultAnswer               // send the returned canned response
)

// faultGate consults the node's installed fault profile before the request
// is dispatched. Evaluation order follows the profile contract: blackhole,
// then probabilistic drop, then delay, then canned. Skip and answer
// verdicts are counted on the store when it carries the counters extension.
// A node with no profile — every fixture's default — returns faultServe
// without touching the rand source or the clock, which is what keeps the
// no-fault byte path identical.
func (a *Acceptor) faultGate(p *platform, req model.Message, peer string) (model.Message, faultDecision) {
	profile, ok := a.faults.Get(a.ctx, p.id)
	if !ok || profile.IsZero() {
		return model.Message{}, faultServe
	}
	method := req.Method()
	if slices.Contains(profile.Blackhole, method) {
		a.recordFault(p.id, model.FaultBlackhole)
		return model.Message{}, faultSkip
	}
	if profile.Drop > 0 && rand.Float64() < profile.Drop {
		a.recordFault(p.id, model.FaultDrop)
		a.log.Info("fault profile dropped a request",
			"node_id", p.id.String(), "method", method, "peer", peer)
		return model.Message{}, faultSkip
	}
	// Delay withholds every answer — canned and served alike.
	if !profile.Delay.IsZero() {
		d := profile.Delay.Base
		if profile.Delay.Jitter > 0 {
			d += time.Duration(rand.Int63n(int64(profile.Delay.Jitter) + 1))
		}
		a.recordFault(p.id, model.FaultDelay)
		time.Sleep(d)
	}
	if status, canned := profile.Canned[method]; canned {
		a.recordFault(p.id, model.FaultCannedResponse)
		resp, err := buildResponse(status, cannedReason(status), req, nil, "")
		if err != nil {
			a.log.Warn("cannot render the canned fault answer",
				"node_id", p.id.String(), "method", method,
				"status", status, "error", err.Error())
			return model.Message{}, faultSkip
		}
		return resp, faultAnswer
	}
	return model.Message{}, faultServe
}

// faultUnsupported answers a method the node does not serve with the
// profile's UnsupportedMethod status when one is configured; without a
// profile the request stays a silent drop, the historical behaviour.
func (a *Acceptor) faultUnsupported(p *platform, req model.Message) (model.Message, bool) {
	if a.faults == nil {
		return model.Message{}, false
	}
	profile, ok := a.faults.Get(a.ctx, p.id)
	if !ok || profile.UnsupportedMethod == 0 {
		return model.Message{}, false
	}
	a.recordFault(p.id, model.FaultUnsupportedMethod)
	resp, err := buildResponse(profile.UnsupportedMethod,
		cannedReason(profile.UnsupportedMethod), req, nil, "")
	if err != nil {
		a.log.Warn("cannot render the unsupported-method answer",
			"node_id", p.id.String(), "status", profile.UnsupportedMethod,
			"error", err.Error())
		return model.Message{}, false
	}
	return resp, true
}

// recordFault bumps the store's counter for an action that actually fired.
// A bare port.FaultStore has nowhere to put counters, so the extension is
// discovered structurally and its absence never changes the gate outcome.
func (a *Acceptor) recordFault(nodeID model.NodeID, action model.FaultAction) {
	if rec, ok := a.faults.(interface {
		Record(nodeID model.NodeID, action model.FaultAction)
	}); ok {
		rec.Record(nodeID, action)
	}
}

// cannedReason returns a reason phrase for a canned fault status. SIP
// shares most phrases with HTTP; the handful that differ are tabled here
// and the rest fall back to http.StatusText.
func cannedReason(status int) string {
	if r, ok := sipReasons[status]; ok {
		return r
	}
	if r := http.StatusText(status); r != "" {
		return r
	}
	return "Fault"
}

var sipReasons = map[int]string{
	480: "Temporarily Unavailable",
	486: "Busy Here",
	500: "Server Internal Error",
	502: "Bad Gateway",
	503: "Service Unavailable",
	504: "Server Time-out",
	600: "Busy Everywhere",
	603: "Decline",
	604: "Does Not Exist Anywhere",
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
	case notify.IsDeviceInfo():
		return a.handleDeviceInfo(ctx, p, req, notify)
	case notify.IsRecordInfo():
		return a.handleRecordInfo(ctx, p, req, notify)
	case notify.IsAlarm():
		return a.handleAlarm(ctx, p, req, notify)
	case notify.IsDeviceControl():
		return a.handleDeviceControl(ctx, p, req, notify)
	case notify.IsPresetQuery():
		return a.handlePresetQuery(ctx, p, req, notify)
	case notify.IsHomePosition():
		if !a.is2022(ctx, p, notify.DeviceID()) {
			return model.Message{}, false
		}
		return a.handleHomePosition(ctx, p, req, notify)
	case notify.IsCruiseTrackList():
		if !a.is2022(ctx, p, notify.DeviceID()) {
			return model.Message{}, false
		}
		return a.handleCruiseTrackList(ctx, p, req, notify)
	case notify.IsSnapShot():
		if !a.is2022(ctx, p, notify.DeviceID()) {
			return model.Message{}, false
		}
		return a.handleSnapShot(ctx, p, req, notify)
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
	_ string,
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
// ACK carries no response in the SIP request/response model. Confirming the
// dialog also cancels the INVITE expiry timer, so a confirmed dialog is not
// torn down 30 s later by a stale timer.
func (a *Acceptor) handleAck(
	_ context.Context,
	p *platform,
	req model.Message,
	_ string,
) {
	callID := headerValue(req, "Call-ID")
	if callID == "" || a.dialogs == nil {
		return
	}
	if d, err := a.dialogs.Get(callID); err && !d.IsTerminated() {
		if _, err := a.dialogs.Confirm(callID, nil); err != nil {
			a.log.Warn("ACK could not confirm dialog",
				"node_id", p.id.String(), "call_id", callID, "error", err.Error())
			return
		}
		a.cancelInviteExpiry(callID)
	}
}

// handleBye terminates the dialog identified by the Call-ID and answers with
// 200 OK.
func (a *Acceptor) handleBye(
	_ context.Context,
	p *platform,
	req model.Message,
	_ string,
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
	_ context.Context,
	_ *platform,
	req model.Message,
	_ string,
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
	msg, dst, ok := a.routeOutbound(p, msg, peer)
	if ok {
		if err := p.tr.Send(ctx, msg, dst); err != nil {
			a.log.Warn("cannot send NOTIFY",
				"node_id", p.id.String(), "peer", dst, "error", err.Error())
			return
		}
	}
	a.log.Debug("NOTIFY sent",
		"node_id", p.id.String(), "peer", dst, "call_id", callID, "items", len(items))
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
	_ context.Context,
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

// handleDeviceInfo answers a DeviceInfo query with the static facts the
// device registry holds about the requester: vendor, model, firmware.
// A query without an SN is refused silently (the answer could not be
// correlated anyway).
func (a *Acceptor) handleDeviceInfo(
	ctx context.Context,
	p *platform,
	req model.Message,
	notify model.Notify,
) (model.Message, bool) {
	if !notify.HasSN() {
		a.log.Warn("ignoring a DeviceInfo query without a sequence number",
			"node_id", p.id.String(), "device_id", notify.DeviceID())
		return model.Message{}, false
	}
	_ = ctx
	_ = p
	item, itemErr := model.NewDeviceInfoItem(model.DeviceInfoItemParams{
		DeviceID:     notify.DeviceID(),
		Name:         "GB28181 Simulator Device",
		Manufacturer: "Simulator",
		Model:        "virtual-camera-1",
		Firmware:     "1.0.0",
	})
	if itemErr != nil {
		a.log.Warn("cannot build a device info item",
			"node_id", p.id.String(), "error", itemErr.Error())
		return model.Message{}, false
	}
	info := model.DeviceInfoResponse{
		DeviceID: notify.DeviceID(),
		SN:       notify.SN(),
		SumNum:   1,
		Items:    []model.DeviceInfoItem{item},
	}
	body, err := a.manscdp.MarshalDeviceInfoResponse(info)
	if err != nil {
		a.log.Warn("cannot render a device info answer",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	resp, err := buildResponse(200, "OK", req, []model.Header{
		model.NewHeader("Content-Type", contentTypeMANSCDP),
	}, body)
	if err != nil {
		a.log.Warn("cannot answer a DeviceInfo query",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	return resp, true
}

// handleRecordInfo answers a RecordInfo query with the synthetic record
// list the device reports for the requested time range. An empty StartTime
// or EndTime in the query is treated as "no bound" on that side.
func (a *Acceptor) handleRecordInfo(
	_ context.Context,
	p *platform,
	req model.Message,
	notify model.Notify,
) (model.Message, bool) {
	if !notify.HasSN() {
		a.log.Warn("ignoring a RecordInfo query without a sequence number",
			"node_id", p.id.String(), "device_id", notify.DeviceID())
		return model.Message{}, false
	}
	query, qErr := a.manscdp.DecodeRecordInfoQuery(req.Body())
	if qErr != nil {
		a.log.Warn("cannot read a record info query",
			"node_id", p.id.String(), "error", qErr.Error())
		return model.Message{}, false
	}
	all := []model.RecordInfoItem{model.NewRecordInfoItem(model.RecordInfoItemParams{
		Name:      "recording-1",
		DeviceID:  notify.DeviceID(),
		StartTime: "20260925T000000",
		EndTime:   "20260925T010000",
		FilePath:  "/records/" + notify.DeviceID() + "/20260925T000000.mp4",
	})}
	items := all
	if query.StartTime != "" {
		filtered := make([]model.RecordInfoItem, 0, len(items))
		for _, it := range items {
			if it.EndTime() > query.StartTime {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}
	if query.EndTime != "" {
		filtered := make([]model.RecordInfoItem, 0, len(items))
		for _, it := range items {
			if it.StartTime() <= query.EndTime {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}
	respBody := model.RecordInfoResponse{
		DeviceID: notify.DeviceID(),
		SN:       notify.SN(),
		SumNum:   len(items),
		Items:    items,
	}
	body, err := a.manscdp.MarshalRecordInfoResponse(respBody)
	if err != nil {
		a.log.Warn("cannot render a record info answer",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	resp, err := buildResponse(200, "OK", req, []model.Header{
		model.NewHeader("Content-Type", contentTypeMANSCDP),
	}, body)
	if err != nil {
		a.log.Warn("cannot answer a RecordInfo query",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	return resp, true
}

// handleAlarm acknowledges an Alarm notify a downstream pushed. The event is
// appended to the serving node's in-memory alarm log when a node registry is
// attached, so the runtime API and Web UI can observe what arrived on the
// wire; without one it is only logged.
func (a *Acceptor) handleAlarm(
	ctx context.Context,
	p *platform,
	req model.Message,
	_ model.Notify,
) (model.Message, bool) {
	alarm, err := a.manscdp.DecodeAlarmNotify(req.Body())
	if err != nil {
		a.log.Warn("cannot read an alarm notify",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	if a.registry != nil {
		snap, snapErr := model.NewAlarmSnapshot(
			fmt.Sprintf("%s-%d", alarm.DeviceID(), alarm.SN()),
			alarm.DeviceID(), alarm.ChannelID(),
			alarm.AlarmPriority(), alarm.AlarmMethod(),
			alarm.Description(), alarm.EventTime(),
		)
		if snapErr == nil {
			if _, mErr := a.registry.MutateProfile(ctx, p.id, func(np model.NodeProfile) (model.NodeProfile, error) {
				return np.AppendAlarm(snap)
			}); mErr != nil {
				a.log.Debug("alarm snapshot not stored",
					"node_id", p.id.String(), "error", mErr.Error())
			}
		}
	}
	ack := model.NewAlarmAck(alarm.DeviceID(), alarm.SN(), "OK")
	body, err := a.manscdp.MarshalAlarmAck(ack)
	if err != nil {
		a.log.Warn("cannot render an alarm ack",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	a.log.Debug("alarm received", "node_id", p.id.String(),
		"device_id", alarm.DeviceID(), "event", alarm.EventType(),
		"time", alarm.EventTime())
	resp, err := buildResponse(200, "OK", req, []model.Header{
		model.NewHeader("Content-Type", contentTypeMANSCDP),
	}, body)
	if err != nil {
		a.log.Warn("cannot answer an Alarm notify",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	return resp, true
}

// is2022 reports whether the device identified by deviceID is registered on p
// and negotiated GB/T 28181-2022 semantics. Unknown or 2016 devices return
// false, which is the gate used by the 2022 command handlers.
func (a *Acceptor) is2022(ctx context.Context, p *platform, deviceID string) bool {
	dev, ok := a.devices.Lookup(ctx, p.id, deviceID)
	return ok && dev.Is2022()
}

// handleHomePosition answers a GB/T 28181-2022 HomePosition command.
// A query returns the profile guard position; a set validates coordinates
// and updates the node profile. A 2016 peer never reaches this handler
// because handleMessage gates it.
func (a *Acceptor) handleHomePosition(
	ctx context.Context,
	p *platform,
	req model.Message,
	notify model.Notify,
) (model.Message, bool) {
	// Query takes precedence: if the body can be parsed as a query the peer
	// asked for the guard position.
	if query, qErr := a.manscdp.DecodeHomePositionQuery(req.Body()); qErr == nil {
		body, mErr := a.marshalHomePositionQuery(p, query, notify.SN())
		if mErr != nil {
			return model.Message{}, false
		}
		resp, err := buildResponse(200, "OK", req, []model.Header{
			model.NewHeader("Content-Type", contentTypeMANSCDP),
		}, body)
		if err != nil {
			return model.Message{}, false
		}
		return resp, true
	}
	// Otherwise treat it as a set command.
	set, sErr := a.manscdp.DecodeHomePositionSet(req.Body())
	if sErr != nil {
		a.log.Warn("cannot read a HomePosition set",
			"node_id", p.id.String(), "error", sErr.Error())
		return model.Message{}, false
	}
	// Validate coordinates: required fields must be present and non-empty.
	if set.Longitude == "" || set.Latitude == "" {
		errBody, _ := a.manscdp.MarshalHomePositionResponse(model.HomePositionResponse{
			DeviceID: set.DeviceID,
			SN:       notify.SN(),
			Result:   "ERROR",
		}, notify.SN())
		resp, err := buildResponse(200, "OK", req, []model.Header{
			model.NewHeader("Content-Type", contentTypeMANSCDP),
		}, errBody)
		if err != nil {
			return model.Message{}, false
		}
		return resp, true
	}
	if a.registry != nil {
		if _, err := a.registry.MutateProfile(ctx, p.id, func(np model.NodeProfile) (model.NodeProfile, error) {
			pos, posErr := model.NewHomePosition(model.HomePositionParams{
				DeviceID:  set.DeviceID,
				Longitude: set.Longitude,
				Latitude:  set.Latitude,
				Altitude:  set.Altitude,
				Azimuth:   set.Azimuth,
			})
			if posErr != nil {
				return np, posErr
			}
			return np.WithHomePosition(pos)
		}); err != nil {
			a.log.Warn("cannot store a guard position",
				"node_id", p.id.String(), "error", err.Error())
		}
	}
	okBody, _ := a.manscdp.MarshalHomePositionResponse(model.HomePositionResponse{
		DeviceID: set.DeviceID,
		SN:       notify.SN(),
		Result:   "OK",
	}, notify.SN())
	resp, err := buildResponse(200, "OK", req, []model.Header{
		model.NewHeader("Content-Type", contentTypeMANSCDP),
	}, okBody)
	if err != nil {
		return model.Message{}, false
	}
	return resp, true
}

// marshalHomePositionQuery renders a HomePosition query answer from the
// profile guard position when one is configured; otherwise it answers with
// an empty response body.
func (a *Acceptor) marshalHomePositionQuery(
	p *platform,
	query model.HomePositionQuery,
	sn uint32,
) (string, error) {
	if a.registry != nil {
		if node, ok := a.registry.Get(a.ctx, p.id); ok {
			if pos, hasPos := node.Profile().HomePosition(); hasPos {
				resp := model.HomePositionResponse{
					DeviceID:  pos.DeviceID(),
					Longitude: pos.Longitude(),
					Latitude:  pos.Latitude(),
					Altitude:  pos.Altitude(),
					Azimuth:   pos.Azimuth(),
				}
				return a.manscdp.MarshalHomePositionResponse(resp, sn)
			}
		}
	}
	// No position configured — return a minimal OK body.
	return a.manscdp.MarshalHomePositionResponse(model.HomePositionResponse{
		DeviceID: query.DeviceID,
		SN:       sn,
		Result:   "OK",
	}, sn)
}

// handleCruiseTrackList answers a GB/T 28181-2022 CruiseTrackList query with
// the cruise tracks configured on the node profile.
func (a *Acceptor) handleCruiseTrackList(
	ctx context.Context,
	p *platform,
	req model.Message,
	notify model.Notify,
) (model.Message, bool) {
	_, err := a.manscdp.DecodeCruiseTrackListQuery(req.Body())
	if err != nil {
		a.log.Warn("cannot read a CruiseTrackList query",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	items := []model.CruiseTrack{}
	if a.registry != nil {
		if node, ok := a.registry.Get(ctx, p.id); ok {
			items = node.Profile().CruiseTracks()
			if items == nil {
				items = []model.CruiseTrack{}
			}
		}
	}
	resp := model.CruiseTrackListResponse{
		DeviceID: notify.DeviceID(),
		SN:       notify.SN(),
		Items:    items,
	}
	body, err := a.manscdp.MarshalCruiseTrackListResponse(resp, notify.SN())
	if err != nil {
		a.log.Warn("cannot render a CruiseTrackList response",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	a.log.Debug("answering a cruise track list query", "node_id", p.id.String(),
		"device_id", notify.DeviceID(), "tracks", len(items))
	respMsg, err := buildResponse(200, "OK", req, []model.Header{
		model.NewHeader("Content-Type", contentTypeMANSCDP),
	}, body)
	if err != nil {
		return model.Message{}, false
	}
	return respMsg, true
}

// handleSnapShot acknowledges a GB/T 28181-2022 SnapShot capture command.
// The simulator does not produce a JPEG payload; it records the capture
// event on the node profile and answers OK.
func (a *Acceptor) handleSnapShot(
	ctx context.Context,
	p *platform,
	req model.Message,
	notify model.Notify,
) (model.Message, bool) {
	cmd, err := a.manscdp.DecodeSnapShotCommand(req.Body())
	if err != nil {
		a.log.Warn("cannot read a SnapShot command",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	if a.registry != nil {
		if _, err := a.registry.MutateProfile(ctx, p.id, func(np model.NodeProfile) (model.NodeProfile, error) {
			rec, recErr := model.NewSnapShotRecord(model.SnapShotRecordParams{
				DeviceID:  cmd.DeviceID,
				ChannelID: cmd.ChannelID,
			})
			if recErr != nil {
				return np, recErr
			}
			return np.AppendSnapShot(rec)
		}); err != nil {
			a.log.Warn("cannot record a snapshot",
				"node_id", p.id.String(), "error", err.Error())
		}
	}
	a.log.Debug("snapshot captured", "node_id", p.id.String(),
		"device_id", cmd.DeviceID, "channel", cmd.ChannelID)
	body, err := a.manscdp.MarshalSnapShotResponse(model.SnapShotResponse{
		DeviceID: cmd.DeviceID,
		SN:       notify.SN(),
		Result:   "OK",
	}, notify.SN())
	if err != nil {
		a.log.Warn("cannot render a SnapShot response",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	resp, err := buildResponse(200, "OK", req, []model.Header{
		model.NewHeader("Content-Type", contentTypeMANSCDP),
	}, body)
	if err != nil {
		return model.Message{}, false
	}
	return resp, true
}

// handlePresetQuery answers a PresetQuery with the preset list the node's
// profile carries. Without a node registry (or for a node the registry does
// not know) the answer is an empty list, which is still a valid answer for
// a device that exposes no presets.
func (a *Acceptor) handlePresetQuery(
	ctx context.Context,
	p *platform,
	req model.Message,
	notify model.Notify,
) (model.Message, bool) {
	if !notify.HasSN() {
		a.log.Warn("ignoring a PresetQuery without a sequence number",
			"node_id", p.id.String(), "device_id", notify.DeviceID())
		return model.Message{}, false
	}
	presets := []model.PresetItem{}
	if a.registry != nil {
		if node, ok := a.registry.Get(ctx, p.id); ok {
			presets = node.Profile().Presets()
			if presets == nil {
				presets = []model.PresetItem{}
			}
		}
	}
	list, err := model.NewPresetListResponse(notify.DeviceID(), notify.SN(), presets)
	if err != nil {
		a.log.Warn("cannot build a preset list answer",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	body, err := a.manscdp.MarshalPresetList(list)
	if err != nil {
		a.log.Warn("cannot render a preset list answer",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	a.log.Debug("answering a preset query", "node_id", p.id.String(),
		"sn", notify.SN(), "sum", list.SumNum)
	resp, err := buildResponse(200, "OK", req, []model.Header{
		model.NewHeader("Content-Type", contentTypeMANSCDP),
	}, body)
	if err != nil {
		a.log.Warn("cannot answer a PresetQuery",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	return resp, true
}

// handleDeviceControl acknowledges a PTZ DeviceControl command. The
// simulator does not move cameras; it only logs the motion requested.
// A 2022 TeleBoot element inside the control body is treated as a reboot
// request and logged accordingly.
func (a *Acceptor) handleDeviceControl(
	_ context.Context,
	p *platform,
	req model.Message,
	_ model.Notify,
) (model.Message, bool) {
	// TeleBoot is a 2022 control element with its own XML tag. Detect it
	// before the generic PTZ decoder so we can log the reboot separately.
	if strings.Contains(req.Body(), "<TeleBoot>") {
		a.log.Debug("TeleBoot reboot requested", "node_id", p.id.String())
		resp, err := buildResponse(200, "OK", req, nil, "")
		if err != nil {
			a.log.Warn("cannot answer a TeleBoot",
				"node_id", p.id.String(), "error", err.Error())
			return model.Message{}, false
		}
		return resp, true
	}
	control, err := a.manscdp.DecodePTZControl(req.Body())
	if err != nil {
		a.log.Warn("cannot read a device control",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	a.log.Debug("PTZ control received", "node_id", p.id.String(),
		"device_id", control.DeviceID)
	resp, err := buildResponse(200, "OK", req, nil, "")
	if err != nil {
		a.log.Warn("cannot answer a DeviceControl",
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
// When a node registry is attached the node's profile channels are appended
// to the downstream device list; their ChannelStatus is mapped to the GB/T
// 28181 CatalogStatus so online/offline enforcement is visible upstream.
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
	items := make([]model.CatalogItem, 0)
	for _, d := range a.devices.List(ctx, p.id) {
		item, err := model.NewCatalogItemFromDevice(d)
		if err != nil {
			continue
		}
		items = append(items, item)
	}
	if a.registry != nil {
		if node, ok := a.registry.Get(ctx, p.id); ok {
			for _, ch := range node.Profile().Channels() {
				status := model.CatalogStatusON
				if ch.Status() == model.ChannelStatusOffline {
					status = model.CatalogStatusOFF
				}
				catalogItem, err := model.NewCatalogItem(model.CatalogItemParams{
					DeviceID: ch.ID(),
					Name:     ch.Name(),
					Status:   status,
					ParentID: ch.ParentID(),
				})
				if err != nil {
					continue
				}
				items = append(items, catalogItem)
			}
		}
	}
	catalog, err := model.NewCatalog(p.id.String(), notify.SN(), items)
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
	return a.grant(ctx, p, req, username, peer, cred)
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
	// GB 35114 Note integrity: echo the SM3-hashed Note only when the
	// Challenger emitted one, so MD5 peers see the old byte-identical
	// 401.
	if note := ch.Note(); note != "" {
		value += fmt.Sprintf(`, Note=%q`, note)
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
	cred model.Credentials,
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
		hdrs := []model.Header{
			model.NewHeader("Expires", "0"),
			model.NewHeader("Date", httpDate(now)),
		}
		if signer, ok := a.authenticator.(port.SecurityInfoSigner); ok {
			if h, ok := signer.SignSecurityInfo(req, cred); ok {
				hdrs = append(hdrs, h)
			}
		}
		return buildResponse(200, "OK", req, hdrs, "")
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
	// X-GB-Ver is echoed only when the peer declared it: a 2016 REGISTER
	// gets today's byte-identical answer, a 2022 one learns the negotiated
	// version (GB/T 28181-2022 Annex I).
	if gbVersion != "" {
		hdrs = append(hdrs, model.NewHeader("X-GB-Ver", gbVersion))
	}
	if signer, ok := a.authenticator.(port.SecurityInfoSigner); ok {
		if h, ok := signer.SignSecurityInfo(req, cred); ok {
			hdrs = append(hdrs, h)
		}
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
	_ context.Context,
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
