package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

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
	pipelines    map[string]pipelineEntry

	mu      sync.Mutex
	serving map[string]*platform
	cascade port.CascadeHandler
	faults  port.FaultStore

	// talkHangup is told about every BYE the node receives, so a talk
	// dialog the device ends is reclaimed without waiting for the browser.
	talkHangup func(model.NodeID, string)

	// deviceTalks holds the audio port each talk dialog opened on this node,
	// keyed by Call-ID. A device answers a talk INVITE with its own RTP port;
	// the port lives until the dialog ends.
	deviceTalksMu   sync.Mutex
	deviceTalks     map[string]net.PacketConn
	devicePlaybacks map[string]net.PacketConn

	// relay carries the cascade hand-offs this node is waiting on: which
	// requester is waiting for which downstream answer. relayWait bounds the
	// wait before the node answers the requester itself.
	relay     *relayTable
	relayWait time.Duration

	// responses carries the control requests waiting for an answer (record
	// queries). controlWait bounds that wait.
	responses   *responseTable
	controlWait time.Duration

	// catalogPollEvery configures the catalog pull interval for each serving
	// node. Zero means the poller is disabled (the default). Set through
	// WithCatalogPollInterval before the first call to Serve.
	catalogPollEvery time.Duration

	// inviteMu guards both invite maps. Watchers live for the life of one
	// pending INVITE and are removed either by the timer firing or by an
	// explicit cancel (ACK arrived, or node is stopping). Keeping them on the
	// acceptor instead of in a package-level sync.Map means two Acceptor
	// instances (e.g. one per node in a multi-tenant process, or two test
	// fixtures running side by side) never see each other's timers.
	inviteMu       sync.Mutex
	inviteTimers   map[string]*time.Timer
	inviteWatchers map[string]chan struct{}
}

// platform is one node's serving goroutine and the settings it serves with.
type platform struct {
	id               model.NodeID
	tr               port.SIPTransport
	realm            string
	policy           model.ExpiresPolicy
	sweepEvery       time.Duration
	cancel           context.CancelFunc
	done             chan struct{}
	catalogPoller    *catalogPoller
	catalogPollEvery time.Duration

	// isDevice marks this platform as a device node. A device answers only
	// MESSAGE (catalog query) and returns 405 Method Not Allowed for others.
	isDevice bool

	// position stores the last position configured via SetPosition.
	// hasFix reports whether a position was ever configured; the initial
	// mobileposition NOTIFY renders as empty until it is.
	position model.Position
	hasFix   bool

	// subscribers tracks upstream subscriptions. The map key is the
	// Call-ID of the SUBSCRIBE; the value carries the subscriber's peer,
	// event and expiry. subMu guards the map because a timer goroutine
	// may delete an expired entry while a request handler is reading it.
	subMu       sync.Mutex
	subscribers map[string]*catalogSub

	// pendingNotify carries the parameters for the initial NOTIFY that must
	// be flushed after the 200 OK response so the client sees it last.
	pendingNotify *pendingEventNotify
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
		ctx:             ctx,
		clock:           clock,
		challenger:      challenger,
		authenticator:   authenticator,
		creds:           creds,
		devices:         devices,
		manscdp:         manscdp,
		newTicker:       newTicker,
		log:             log.With("component", "internal/app", "subsystem", "sip_acceptor"),
		serving:         make(map[string]*platform),
		pipelines:       make(map[string]pipelineEntry),
		inviteTimers:    make(map[string]*time.Timer),
		inviteWatchers:  make(map[string]chan struct{}),
		relay:           newRelayTable(nil),
		relayWait:       relayTimeout,
		responses:       newResponseTable(),
		deviceTalks:     make(map[string]net.PacketConn),
		devicePlaybacks: make(map[string]net.PacketConn),
		controlWait:     5 * time.Second,
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

// WithTalkHangup registers the callback told about every received BYE. The
// callback decides whether the Call-ID belongs to one of its sessions.
func (a *Acceptor) WithTalkHangup(fn func(model.NodeID, string)) *Acceptor {
	a.talkHangup = fn
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
