// Package app — platform-large registration acceptance.
package app

import (
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

// Acceptor is the UAS half of the simulator: for every platform-large node
// it runs one goroutine that receives REGISTERs on that node's transport,
// challenges them the way GB/T 28181 §L.2 prescribes, and records the
// devices that make it through.
//
// It depends only on domain ports. The transport is handed in per node
// because every node owns its own listener, and the root context is the
// process's rather than a request's: serving must outlive the call that
// started the node. Close ends everything.
type Acceptor struct {
	ctx           context.Context
	clock         port.Clock
	challenger    port.Challenger
	authenticator port.Authenticator
	creds         port.CredentialStore
	devices       port.DownstreamRegistry
	log           *slog.Logger

	mu      sync.Mutex
	serving map[string]*platform
}

// platform is one node's serving goroutine and the settings it serves with.
type platform struct {
	id     model.NodeID
	tr     port.SIPTransport
	realm  string
	policy model.ExpiresPolicy
	cancel context.CancelFunc
	done   chan struct{}
}

// NewAcceptor builds an Acceptor whose serving is bounded by ctx.
// challenger, authenticator, creds and devices are required; clock and log
// fall back to the real clock and the default slog logger.
func NewAcceptor(
	ctx context.Context,
	clock port.Clock,
	challenger port.Challenger,
	authenticator port.Authenticator,
	creds port.CredentialStore,
	devices port.DownstreamRegistry,
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
	if clock == nil {
		clock = realClock{}
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
		log:           log,
		serving:       make(map[string]*platform),
	}, nil
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
		id:     id,
		tr:     tr,
		realm:  realm,
		policy: policy,
		cancel: cancel,
		done:   make(chan struct{}),
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
// context ends; every REGISTER gets exactly one answer.
func (a *Acceptor) run(ctx context.Context, p *platform) {
	defer close(p.done)

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
		if msg.Method() != "REGISTER" {
			// Heartbeats and the rest belong to later changes; a
			// platform that does not understand a message must not
			// answer it with an error, so it is dropped quietly.
			a.log.Debug("ignoring a non-REGISTER request",
				"node_id", p.id.String(), "method", msg.Method(), "peer", peer)
			continue
		}
		resp, err := a.handle(ctx, p, msg, peer)
		if err != nil {
			a.log.Error("cannot answer a REGISTER",
				"node_id", p.id.String(), "peer", peer, "error", err.Error())
			continue
		}
		if err := p.tr.Send(ctx, resp, peer); err != nil {
			a.log.Warn("cannot send the answer to a REGISTER",
				"node_id", p.id.String(), "peer", peer, "error", err.Error())
		}
	}
}

// handle decides one REGISTER: challenge it, refuse it, or grant it and
// record the device.
func (a *Acceptor) handle(ctx context.Context, p *platform, req model.Message, peer string) (model.Message, error) {
	username := requestUser(req)
	if username == "" {
		// Without a caller there is nobody to look up and nothing worth
		// challenging: refuse rather than hand out a nonce to a stranger.
		a.log.Warn("refusing a REGISTER without a caller", "node_id", p.id.String())
		return buildResponse(403, "Forbidden", req, nil)
	}
	if _, ok := req.Header("Authorization"); !ok {
		a.log.Debug("challenging a REGISTER", "node_id", p.id.String(), "username", username)
		return a.challenge(req, p)
	}
	cred, ok := a.creds.Lookup(p.id, username)
	if !ok {
		a.log.Warn("refusing a REGISTER for an unknown account",
			"node_id", p.id.String(), "username", username)
		return buildResponse(403, "Forbidden", req, nil)
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
		return buildResponse(403, "Forbidden", req, nil)
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
	})
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
		return buildResponse(200, "OK", req, []model.Header{
			model.NewHeader("Expires", "0"),
			model.NewHeader("Date", httpDate(now)),
		})
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

	hdrs := []model.Header{
		model.NewHeader("Expires", strconv.FormatUint(uint64(granted), 10)),
		model.NewHeader("Date", httpDate(now)),
	}
	if contact != "" {
		hdrs = append(hdrs, model.NewHeader("Contact", contact))
	}
	return buildResponse(200, "OK", req, hdrs)
}

// buildResponse echoes the transaction headers a response must carry and
// adds the ones the answer itself needs. Via, From, To (tagged), Call-ID
// and CSeq are what let the peer match the answer to its transaction.
func buildResponse(status int, reason string, req model.Message, extra []model.Header) (model.Message, error) {
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
	return model.NewResponse(status, reason, hdrs, "")
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
