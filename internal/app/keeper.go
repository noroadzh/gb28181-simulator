// Package app — device keepalive and registration renewal.
package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// Renewal timing. A registration is renewed at half its granted lifetime,
// or renewLead before it lapses when that is earlier — so a platform that
// grants a very long lifetime still gets a renewal with time to spare, and
// a failure still has the whole second half to be retried in.
const renewLead = 60 * time.Second

// Renewal backoff: a refused or unreachable platform is retried with a
// doubling delay, capped so a long outage cannot turn into a tight loop.
const (
	renewBackoffStart = 5 * time.Second
	renewBackoffMax   = 60 * time.Second
)

// ErrKeepaliveLost reports that a node stopped answering its own keepalive
// schedule: several consecutive heartbeats went unanswered.
var ErrKeepaliveLost = fmt.Errorf("app: keepalive unanswered")

// ErrNotRegistered is returned when a node is asked to leave although it
// holds no registration: there is nothing to send `Expires: 0` for.
var ErrNotRegistered = fmt.Errorf("app: node holds no registration")

// Keeper holds registrations open: for every online device node it runs one
// goroutine that sends MESSAGE keepalives and renews the registration before
// it expires.
//
// It depends only on domain ports — the transport is handed in per node
// because every node owns its own listener — and it never imports an
// adapter: the notify body comes from port.KeepaliveCodec and the renewal
// reuses Registrar.
//
// The root context is the process's, not a request's: background work must
// outlive the call that started the node. Close ends everything.
type Keeper struct {
	ctx       context.Context
	registry  port.NodeRegistry
	lifecycle port.NodeLifecycle
	registrar *Registrar
	codec     port.KeepaliveCodec
	clock     port.Clock
	newTicker port.TickerFactory
	log       *slog.Logger

	mu      sync.Mutex
	running map[string]*session
}

// session is one node's background work.
type session struct {
	id     model.NodeID
	tr     port.SIPTransport
	reg    model.Registration
	cancel context.CancelFunc
	done   chan struct{}

	// Owned by the goroutine running the session, hence unlocked: only
	// that goroutine reads or writes them.
	sn        uint32
	failures  uint32
	nextRenew time.Time
	backoff   time.Duration
	result    model.RegistrationResult
}

// NewKeeper builds a Keeper whose background work is bounded by ctx.
// registrar and codec are required; clock, newTicker and log fall back to
// the real clock, a real ticker and the default slog logger.
func NewKeeper(
	ctx context.Context,
	registry port.NodeRegistry,
	lifecycle port.NodeLifecycle,
	registrar *Registrar,
	codec port.KeepaliveCodec,
	clock port.Clock,
	newTicker port.TickerFactory,
	log *slog.Logger,
) (*Keeper, error) {
	if ctx == nil {
		return nil, fmt.Errorf("app: keeper requires a root context")
	}
	if registry == nil {
		return nil, fmt.Errorf("app: keeper requires a NodeRegistry")
	}
	if lifecycle == nil {
		return nil, fmt.Errorf("app: keeper requires a NodeLifecycle")
	}
	if registrar == nil {
		return nil, fmt.Errorf("app: keeper requires a Registrar")
	}
	if codec == nil {
		return nil, fmt.Errorf("app: keeper requires a KeepaliveCodec")
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
	return &Keeper{
		ctx:       ctx,
		registry:  registry,
		lifecycle: lifecycle,
		registrar: registrar,
		codec:     codec,
		clock:     clock,
		newTicker: newTicker,
		log:       log,
		running:   make(map[string]*session),
	}, nil
}

// Start launches the keepalive and renewal loop for id. result is what the
// registration that brought the node online produced; it seeds the renewal
// schedule. Starting a node that is already kept is a no-op, so a repeated
// start cannot double the traffic.
func (k *Keeper) Start(
	id model.NodeID,
	tr port.SIPTransport,
	reg model.Registration,
	result model.RegistrationResult,
) error {
	if tr == nil {
		return fmt.Errorf("app: keeper for node %s requires a transport", id)
	}
	key := id.String()

	k.mu.Lock()
	defer k.mu.Unlock()
	if _, ok := k.running[key]; ok {
		return nil
	}
	runCtx, cancel := context.WithCancel(k.ctx)
	s := &session{
		id:        id,
		tr:        tr,
		reg:       reg,
		cancel:    cancel,
		done:      make(chan struct{}),
		backoff:   renewBackoffStart,
		result:    result,
		nextRenew: k.renewAt(k.clock.Now(), result, reg),
	}
	k.running[key] = s
	go k.run(runCtx, s)
	k.log.Debug("keepalive started", "node_id", key,
		"interval", reg.HeartbeatInterval(), "renew_at", s.nextRenew)
	return nil
}

// Stop ends one node's background work and waits for its goroutine to
// finish, so nothing can send through a transport that is about to be
// released. It is idempotent, and safe to call for a node that is not
// running — including from inside that node's own goroutine, which removes
// itself before faulting.
func (k *Keeper) Stop(id model.NodeID) {
	k.mu.Lock()
	s := k.running[id.String()]
	delete(k.running, id.String())
	k.mu.Unlock()
	if s == nil {
		return
	}
	s.cancel()
	<-s.done
}

// Running reports how many nodes are currently kept. Tests use it to assert
// that background work really stopped.
func (k *Keeper) Running() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.running)
}

// Close implements io.Closer: it stops every node's background work, so the
// process can shut down without leaving goroutines behind.
func (k *Keeper) Close() error {
	k.mu.Lock()
	sessions := make([]*session, 0, len(k.running))
	for key, s := range k.running {
		sessions = append(sessions, s)
		delete(k.running, key)
	}
	k.mu.Unlock()
	for _, s := range sessions {
		s.cancel()
		<-s.done
	}
	return nil
}

// run is one node's loop. A single ticker drives both duties, so a node
// costs one goroutine and one timer however much it has to do.
func (k *Keeper) run(ctx context.Context, s *session) {
	defer close(s.done)

	ticker := k.newTicker(s.reg.HeartbeatInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			k.forget(s)
			return
		case <-ticker.C():
			if err := k.heartbeat(ctx, s); err != nil {
				s.failures++
				k.log.Warn("keepalive failed", "node_id", s.id.String(),
					"consecutive", s.failures, "max", s.reg.MaxHeartbeatFailures(),
					"error", err.Error())
				if s.failures >= s.reg.MaxHeartbeatFailures() {
					k.fail(ctx, s, err)
					return
				}
			} else {
				s.failures = 0
			}
			k.renewIfDue(ctx, s)
		}
	}
}

// heartbeat sends one MESSAGE keepalive and waits for the platform's
// answer. Only a 2xx from the platform we registered with, carrying the
// Call-ID we sent, counts.
func (k *Keeper) heartbeat(ctx context.Context, s *session) error {
	s.sn++
	notify, err := model.NewKeepalive(s.id.String(), s.sn)
	if err != nil {
		return err
	}
	body, err := k.codec.MarshalKeepalive(notify)
	if err != nil {
		return err
	}
	node, ok := k.registry.Get(ctx, s.id)
	if !ok {
		return fmt.Errorf("app: node %s is gone", s.id)
	}
	domain := node.Profile().Domain()
	uri := registrationRequestURI(domain, s.reg)
	callID := randomCallID()
	msg, err := model.NewRequest("MESSAGE", uri, []model.Header{
		model.NewHeader("From", "<sip:"+s.id.String()+"@"+domain+">;tag="+randomTag()),
		model.NewHeader("To", "<sip:"+s.id.String()+"@"+domain+">"),
		model.NewHeader("Call-ID", callID),
		model.NewHeader("CSeq", strconv.FormatUint(uint64(s.sn), 10)+" MESSAGE"),
		model.NewHeader("Content-Type", "Application/MANSCDP+XML"),
		model.NewHeader("Max-Forwards", "70"),
	}, body)
	if err != nil {
		return err
	}

	waitCtx, cancel := context.WithTimeout(ctx, s.reg.HeartbeatTimeout())
	defer cancel()

	if err := s.tr.Send(waitCtx, msg, s.reg.Server()); err != nil {
		return &RegistrationError{Stage: StageSend, Err: err}
	}
	k.log.Debug("keepalive sent", "node_id", s.id.String(), "sn", s.sn, "call_id", callID)

	for {
		got, peer, err := s.tr.Receive(waitCtx)
		if err != nil {
			return &RegistrationError{
				Stage: StageTimeout,
				Err:   fmt.Errorf("%w: %v", ErrRegisterTimeout, err),
			}
		}
		if h, ok := got.Header("Call-ID"); !ok || h.Value() != callID {
			continue
		}
		if peer != s.reg.Server() {
			continue
		}
		status := got.StatusCode()
		switch {
		case status >= 100 && status < 200:
			continue
		case status >= 200 && status < 300:
			k.log.Debug("keepalive answered", "node_id", s.id.String(), "sn", s.sn)
			return nil
		default:
			return &RegistrationError{
				Stage:      StageResponse,
				StatusCode: status,
				Err:        fmt.Errorf("platform refused the keepalive"),
			}
		}
	}
}

// renewIfDue re-registers the node once the renewal point has passed.
func (k *Keeper) renewIfDue(ctx context.Context, s *session) {
	if s.nextRenew.IsZero() || k.clock.Now().Before(s.nextRenew) {
		return
	}
	node, ok := k.registry.Get(ctx, s.id)
	if !ok {
		k.log.Warn("renewal skipped: node is gone", "node_id", s.id.String())
		return
	}
	result, err := k.registrar.Register(ctx, s.tr, node, s.reg)
	if err != nil {
		// The registration is still valid — we are well inside the
		// lifetime we were granted — so the node stays online and we
		// simply try again, later each time.
		s.nextRenew = k.clock.Now().Add(s.backoff)
		if s.backoff < renewBackoffMax {
			s.backoff *= 2
			if s.backoff > renewBackoffMax {
				s.backoff = renewBackoffMax
			}
		}
		k.log.Warn("registration renewal failed", "node_id", s.id.String(),
			"retry_at", s.nextRenew, "error", err.Error())
		return
	}
	s.backoff = renewBackoffStart
	s.result = result
	if _, err := k.registry.RecordRegistration(ctx, s.id, result); err != nil {
		k.log.Warn("registration renewal could not be recorded",
			"node_id", s.id.String(), "error", err.Error())
	}
	s.nextRenew = k.renewAt(result.RegisteredAt(), result, s.reg)
	k.log.Info("registration renewed", "node_id", s.id.String(),
		"expires", result.GrantedExpiry(), "renew_at", s.nextRenew)
}

// renewAt computes the next renewal point: half the granted lifetime, or
// renewLead before it lapses, whichever is earlier. A platform that granted
// a lifetime too short to have a middle is renewed at once rather than
// never.
func (k *Keeper) renewAt(now time.Time, result model.RegistrationResult, reg model.Registration) time.Time {
	registeredAt := result.RegisteredAt()
	if registeredAt.IsZero() {
		return time.Time{}
	}
	expiry := result.ExpiresAt(reg.Expires())
	at := registeredAt.Add(expiry.Sub(registeredAt) / 2)
	if lead := expiry.Add(-renewLead); lead.Before(at) {
		at = lead
	}
	if at.Before(now) {
		return now
	}
	return at
}

// fail ends the session because the platform stopped answering: the node
// moves to fault and its listener is released.
func (k *Keeper) fail(ctx context.Context, s *session, cause error) {
	// The count and the threshold travel together: an operator reading the
	// fault needs to know both how many beats were missed and what the
	// limit was.
	err := fmt.Errorf("%w: %d/%d consecutive heartbeats unanswered: %v",
		ErrKeepaliveLost, s.failures, s.reg.MaxHeartbeatFailures(), cause)
	// Forget first: faulting the node reaches back into Stop, which must
	// not wait for the goroutine that is calling it.
	k.forget(s)
	if ferr := k.lifecycle.Fail(ctx, s.id, err); ferr != nil {
		k.log.Error("faulting an unreachable node failed", "node_id", s.id.String(),
			"error", ferr.Error())
	}
}

// forget removes s from the running set unless a newer session replaced it.
func (k *Keeper) forget(s *session) {
	key := s.id.String()
	k.mu.Lock()
	defer k.mu.Unlock()
	if cur, ok := k.running[key]; ok && cur == s {
		delete(k.running, key)
	}
}

// realTickerFactory is the production ticker source. It lives here so the
// app package does not import platform/clock just for a default.
func realTickerFactory() port.TickerFactory {
	return func(d time.Duration) port.Ticker {
		if d <= 0 {
			d = time.Second
		}
		return &stdTicker{t: time.NewTicker(d)}
	}
}

// stdTicker adapts *time.Ticker to port.Ticker.
type stdTicker struct {
	t    *time.Ticker
	once sync.Once
}

func (s *stdTicker) C() <-chan time.Time { return s.t.C }

func (s *stdTicker) Stop() { s.once.Do(s.t.Stop) }

// Compile-time check: the service container closes the Keeper on shutdown.
var _ io.Closer = (*Keeper)(nil)
