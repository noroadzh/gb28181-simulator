// Package app implements the SIP acceptor handling device registration, message
// routing, and media pipeline management for GB/T 28181 simulation.
//
// This file contains subscription / NOTIFY / catalog render (Cluster 5).
// Extracted verbatim from acceptor.go (was lines 1148-1531, 384 lines).
package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// The SIP Event values a served platform accepts SUBSCRIBEs for. They are
// also the Event header echoed back on every NOTIFY.
const (
	subEventCatalog        = "catalog"
	subEventAlarm          = "alarm"
	subEventMobilePosition = "mobileposition"
)

// supportedSubscribeEvent normalizes the Event header of a SUBSCRIBE and
// reports whether the platform serves it. An absent Event defaults to
// catalog: GB/T 28181 devices in the wild omit it.
func supportedSubscribeEvent(raw string) (string, bool) {
	e := strings.ToLower(strings.TrimSpace(raw))
	switch e {
	case "":
		return subEventCatalog, true
	case subEventCatalog, subEventAlarm, subEventMobilePosition:
		return e, true
	}
	return "", false
}

// handleSubscribe accepts a subscription request from a downstream or an
// upstream platform. The subscribed event must be one the platform serves —
// catalog, alarm or mobileposition, case-insensitively — or the request is
// answered 489 Bad Event. The subscriber is recorded with its Expires
// value, an initial NOTIFY is sent for its event, and a 200 OK is
// answered. A SUBSCRIBE with Expires 0 closes the matching subscription.
func (a *Acceptor) handleSubscribe(
	ctx context.Context,
	p *platform,
	req model.Message,
	peer string,
) (model.Message, error) {
	event, ok := supportedSubscribeEvent(headerValue(req, "Event"))
	if !ok {
		a.log.Debug("SUBSCRIBE rejected: unsupported Event",
			"node_id", p.id.String(), "event", headerValue(req, "Event"))
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
	a.recordSubscriber(p, callID, peer, fromValue, event, expires)
	// Queue the initial NOTIFY for delivery after the 200 OK so the client
	// receives the response first (RFC 6665 §5.2).  sipx will deliver it
	// only when the goroutine calls flushPendingNotify.
	p.pendingNotify = &pendingEventNotify{
		callID:    callID,
		peer:      peer,
		fromValue: fromValue,
		event:     event,
		body:      "", // render lazily in flushPendingNotify
	}
	a.log.Debug("SUBSCRIBE recorded",
		"node_id", p.id.String(), "call_id", callID, "event", event, "expires", expires)
	return buildResponse(200, "OK", req, []model.Header{
		model.NewHeader("Expires", strconv.FormatUint(uint64(expires), 10)),
	}, "")
}

// recordSubscriber registers an upstream subscriber with the duration its
// SUBSCRIBE asked for. When the timer fires the subscriber is dropped.
func (a *Acceptor) recordSubscriber(
	p *platform,
	callID, peer, fromValue, event string,
	expires uint32,
) {
	a.removeSubscriber(p, callID)
	sub := &catalogSub{
		event:     event,
		peer:      peer,
		fromValue: fromValue,
		expiresAt: a.clock.Now().Add(time.Duration(expires) * time.Second),
	}
	sub.timer = time.AfterFunc(time.Duration(expires)*time.Second, func() {
		a.removeSubscriber(p, callID)
		a.log.Debug("SUBSCRIBE expired",
			"node_id", p.id.String(), "call_id", callID)
	})
	p.addSubscriber(callID, sub)
}

// removeSubscriber drops a subscriber by Call-ID and stops its expiry timer.
// Safe to call for an unknown Call-ID.
func (a *Acceptor) removeSubscriber(p *platform, callID string) {
	p.deleteSubscriber(callID)
}

// subscribersFor returns a snapshot of the subscribers of one event, safe
// to iterate while the live table changes underneath.
func (a *Acceptor) subscribersFor(p *platform, event string) map[string]*catalogSub {
	all := p.snapshotSubscribers()
	out := make(map[string]*catalogSub, len(all))
	for callID, sub := range all {
		if sub.event == event {
			out[callID] = sub
		}
	}
	return out
}

// notifyCatalogChange sends a fresh catalog NOTIFY to every active catalog
// subscriber. It is called from the device-register and device-sweep paths;
// alarm and mobileposition subscribers are filtered out so the two event
// streams never cross.
func (a *Acceptor) notifyCatalogChange(ctx context.Context, p *platform) {
	for callID, sub := range a.subscribersFor(p, subEventCatalog) {
		a.sendEventNotify(ctx, p, callID, sub.peer, sub.fromValue, sub.event, "")
	}
}

// flushPendingNotify sends the initial NOTIFY that was queued by
// handleSubscribe so the client receives it after the 200 OK. The body is
// rendered at flush time because the served roster can change between
// SUBSCRIBE and 200 dispatch. A fresh context is used so that a cancelled
// request context cannot silently drop the message.
func (a *Acceptor) flushPendingNotify(p *platform) {
	if p.pendingNotify == nil {
		return
	}
	pn := p.pendingNotify
	p.pendingNotify = nil
	sendCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.sendEventNotify(sendCtx, p, pn.callID, pn.peer, pn.fromValue, pn.event, pn.body)
}

// notifyAlarmSubscribers forwards an alarm that just arrived to every
// active alarm subscriber. With no subscriber it stays quiet: nobody asked
// for alarm events, so nobody is waiting for this one.
func (a *Acceptor) notifyAlarmSubscribers(ctx context.Context, p *platform, body string) {
	subs := a.subscribersFor(p, subEventAlarm)
	if len(subs) == 0 {
		a.log.Debug("alarm with no subscribers", "node_id", p.id.String())
		return
	}
	for callID, sub := range subs {
		a.sendEventNotify(ctx, p, callID, sub.peer, sub.fromValue, subEventAlarm, body)
	}
}

// NotifyAlarm pushes an alarm that just happened to every alarm subscriber
// of the served node id. It is best-effort and silent for a node that does
// not serve or has no subscribers: the runtime API caller drives the
// simulation, not the subscriber count.
func (a *Acceptor) NotifyAlarm(ctx context.Context, id model.NodeID, snap model.AlarmSnapshot) {
	a.mu.Lock()
	serving := make([]*platform, 0, len(a.serving))
	for _, p := range a.serving {
		serving = append(serving, p)
	}
	a.mu.Unlock()
	if len(serving) == 0 {
		return
	}
	notify, err := model.NewAlarmNotify(model.AlarmNotifyParams{
		SN:            1,
		DeviceID:      snap.DeviceID(),
		ChannelID:     snap.ChannelID(),
		AlarmPriority: snap.Priority(),
		AlarmMethod:   snap.Method(),
		EventTime:     snap.EventTime(),
		Description:   snap.Description(),
	})
	if err != nil {
		a.log.Warn("cannot build alarm NOTIFY",
			"node_id", id.String(), "error", err.Error())
		return
	}
	body, err := a.manscdp.MarshalAlarmNotify(notify)
	if err != nil {
		a.log.Warn("cannot marshal alarm NOTIFY",
			"node_id", id.String(), "error", err.Error())
		return
	}
	// The alarm belongs to every served platform that currently lists the
	// source device as an online downstream; a platform that never
	// registered the device has no subscribers to care about it.
	for _, p := range serving {
		if _, ok := a.devices.Lookup(ctx, p.id, snap.DeviceID()); !ok {
			continue
		}
		a.notifyAlarmSubscribers(ctx, p, body)
	}
}

// NotifyPositionChanged pushes a MobilePosition NOTIFY carrying pos to
// every mobileposition subscriber of the served node id. It is best-effort
// and silent for a node that does not serve or has no subscribers: the
// runtime API caller drives the simulation, not the subscriber count.
func (a *Acceptor) NotifyPositionChanged(ctx context.Context, id model.NodeID, pos model.Position) {
	a.mu.Lock()
	p, ok := a.serving[id.String()]
	a.mu.Unlock()
	if !ok {
		return
	}
	// Store the latest position on the platform so that a later
	// mobileposition SUBSCRIBE can render it without needing a registry.
	p.position = pos
	p.hasFix = true
	mp, err := model.NewMobilePositionNotify(model.MobilePositionNotifyParams{
		SN:        1,
		DeviceID:  id.String(),
		Longitude: pos.Longitude(),
		Latitude:  pos.Latitude(),
		Speed:     pos.Speed(),
		Time:      a.clock.Now().Format("2006-01-02T15:04:05"),
	})
	if err != nil {
		a.log.Warn("cannot build mobile position NOTIFY",
			"node_id", id.String(), "error", err.Error())
		return
	}
	body, err := a.manscdp.MarshalMobilePositionNotify(mp)
	if err != nil {
		a.log.Warn("cannot marshal mobile position NOTIFY",
			"node_id", id.String(), "error", err.Error())
		return
	}
	for callID, sub := range a.subscribersFor(p, subEventMobilePosition) {
		a.sendEventNotify(ctx, p, callID, sub.peer, sub.fromValue, subEventMobilePosition, body)
	}
}

// buildEventNotifyMessage constructs and (optionally) sends a NOTIFY for one
// subscriber. It renders the body, builds the SIP request, routes it, and
// either sends it synchronously (if send=true) or returns the routed message
// for the caller to send later (if send=false). This two-step approach lets
// the serve loop flush the 200 OK before the initial NOTIFY so the client
// receives responses in the order the RFC requires.
func (a *Acceptor) buildEventNotifyMessage(
	ctx context.Context,
	p *platform,
	callID, peer, fromValue, event, body string,
	send bool,
) (*model.Message, string, bool) {
	payload, err := a.renderEventBody(ctx, p, event, body)
	if err != nil {
		a.log.Warn("cannot render NOTIFY body",
			"node_id", p.id.String(), "event", event, "error", err.Error())
		return nil, "", false
	}
	hdrs := []model.Header{
		model.NewHeader("From", "<sip:"+p.id.String()+"@"+p.realm+">;tag="+randomTag()),
		model.NewHeader("To", fromValue+";tag="+randomTag()),
		model.NewHeader("Via", "SIP/2.0/UDP "+p.tr.LocalAddr()+";branch=z9hG4bK-"+randomTag()),
		model.NewHeader("Call-ID", callID),
		model.NewHeader("CSeq", "1 NOTIFY"),
		model.NewHeader("Event", event),
		model.NewHeader("Max-Forwards", "70"),
	}
	if payload != "" {
		hdrs = append(hdrs, model.NewHeader("Content-Type", contentTypeMANSCDP))
	}
	msg, err := model.NewRequest("NOTIFY", "sip:"+peer, hdrs, payload)
	if err != nil {
		a.log.Warn("cannot build NOTIFY",
			"node_id", p.id.String(), "error", err.Error())
		return nil, "", false
	}
	msg, dst, ok := a.routeOutbound(p, msg, peer)
	if !ok {
		return nil, "", false
	}
	if send {
		if err := p.tr.Send(ctx, msg, dst); err != nil {
			a.log.Warn("cannot send NOTIFY",
				"node_id", p.id.String(), "peer", dst, "error", err.Error())
			return nil, "", false
		}
		a.log.Debug("NOTIFY sent",
			"node_id", p.id.String(), "peer", dst, "call_id", callID,
			"event", event, "size", len(payload))
	}
	return &msg, dst, true
}

// sendEventNotify renders the event body for one subscriber and sends it as
// a NOTIFY. An explicitly given body wins over the rendered one — that is
// how alarm and position pushes carry the exact event that happened. An
// empty body is still sent: it is the initial alarm notification's shape.
// It acquires a fresh context so it can be called from any execution path.
func (a *Acceptor) sendEventNotify(
	ctx context.Context,
	p *platform,
	callID, peer, fromValue, event, body string,
) {
	// Use a fresh background context so that a late-request ctx does not
	// cause Send to abort before the datagram is handed to the kernel.
	sendCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, ok := a.buildEventNotifyMessage(sendCtx, p, callID, peer, fromValue, event, body, true)
	if !ok {
		return
	}
	_ = ctx // sendCtx is used intentionally
}

// renderEventBody decides what a NOTIFY for one event carries. A body given
// by the caller always wins; otherwise the event renders its own current
// state.
func (a *Acceptor) renderEventBody(
	ctx context.Context,
	p *platform,
	event, body string,
) (string, error) {
	if body != "" {
		return body, nil
	}
	switch event {
	case subEventCatalog:
		return a.renderCatalogBody(ctx, p)
	case subEventMobilePosition:
		return a.renderMobilePositionBody(ctx, p)
	default:
		// alarm: the initial NOTIFY after SUBSCRIBE is an empty body;
		// pushes supply the alarm that just happened explicitly.
		return "", nil
	}
}

// renderCatalogBody renders the current device list as a catalog NOTIFY
// body.
func (a *Acceptor) renderCatalogBody(ctx context.Context, p *platform) (string, error) {
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
		return "", fmt.Errorf("app: build catalog: %w", err)
	}
	payload, err := a.manscdp.MarshalCatalogNotify(catalog)
	if err != nil {
		return "", fmt.Errorf("app: marshal catalog notify: %w", err)
	}
	return payload, nil
}

// renderMobilePositionBody renders the node's configured position as a
// MobilePosition NOTIFY body. An unconfigured position renders as empty:
// the subscriber learns "no fix yet" rather than fabricated coordinates.
// The latest SetPosition wins, but if it was never called, the configured
// profile position is honoured so the initial NOTIFY still carries a fix.
func (a *Acceptor) renderMobilePositionBody(ctx context.Context, p *platform) (string, error) {
	pos := p.position
	if !p.hasFix {
		if a.registry == nil {
			return "", nil
		}
		node, ok := a.registry.Get(ctx, p.id)
		if !ok {
			return "", nil
		}
		profilePos, ok := node.Profile().Position()
		if !ok {
			return "", nil
		}
		pos = profilePos
	}
	mp, err := model.NewMobilePositionNotify(model.MobilePositionNotifyParams{
		SN:        1,
		DeviceID:  p.id.String(),
		Longitude: pos.Longitude(),
		Latitude:  pos.Latitude(),
		Speed:     pos.Speed(),
		Time:      a.clock.Now().Format("2006-01-02T15:04:05"),
	})
	if err != nil {
		return "", fmt.Errorf("app: build mobile position: %w", err)
	}
	payload, err := a.manscdp.MarshalMobilePositionNotify(mp)
	if err != nil {
		return "", fmt.Errorf("app: marshal mobile position: %w", err)
	}
	return payload, nil
}

// pendingEventNotify carries the parameters for the initial NOTIFY that must
// be flushed after the 200 OK response so the client sees it last.
type pendingEventNotify struct {
	callID    string
	peer      string
	fromValue string
	event     string
	body      string
}

// catalogSub is one active upstream subscription. Since the SUBSCRIBE event
// expansion it tracks catalog, alarm and mobileposition subscribers,
// distinguished by event.
type catalogSub struct {
	event     string
	peer      string
	fromValue string
	expiresAt time.Time
	timer     *time.Timer
}

// addSubscriber registers a new subscriber or replaces the existing entry
// with the same call-ID. The caller is responsible for stopping any prior
// timer.
func (p *platform) addSubscriber(callID string, sub *catalogSub) {
	p.subMu.Lock()
	defer p.subMu.Unlock()
	p.subscribers[callID] = sub
}

// deleteSubscriber removes a subscriber by Call-ID and stops its timer.
// Safe to call for an unknown Call-ID.
func (p *platform) deleteSubscriber(callID string) {
	p.subMu.Lock()
	defer p.subMu.Unlock()
	if sub, ok := p.subscribers[callID]; ok {
		if sub.timer != nil {
			sub.timer.Stop()
		}
		delete(p.subscribers, callID)
	}
}

// snapshotSubscribers copies the current subscriber table so callers can
// iterate while another goroutine (a timer, a request handler) mutates the
// live one.
func (p *platform) snapshotSubscribers() map[string]*catalogSub {
	p.subMu.Lock()
	defer p.subMu.Unlock()
	out := make(map[string]*catalogSub, len(p.subscribers))
	for k, v := range p.subscribers {
		cp := *v
		out[k] = &cp
	}
	return out
}
