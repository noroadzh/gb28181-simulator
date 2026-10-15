// Package app — platform-large registration acceptance.
package app

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

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
		id:               id,
		tr:               tr,
		realm:            realm,
		policy:           policy,
		sweepEvery:       sweepIntervalFor(policy),
		cancel:           cancel,
		done:             make(chan struct{}),
		subscribers:      make(map[string]*catalogSub),
		catalogPollEvery: a.catalogPollEvery,
	}
	if p.catalogPollEvery > 0 && a.registry != nil {
		p.catalogPoller = newCatalogPoller(id, tr, a.devices, a.registry, a.manscdp, a.newTicker, a.log, p.catalogPollEvery)
	}
	a.serving[key] = p
	go a.run(runCtx, p)
	return nil
}

// ServeDevice starts a SIP read loop for a device node. Unlike Serve, the
// device is a UAS: it answers MESSAGE requests (notably the catalog query
// that lets an upstream learn its MediaSource extension) and returns 405
// Method Not Allowed for every other method, so a probing peer learns the
// node is alive without being able to drive it into unwanted behaviour.
//
// catalogPoller is intentionally not started: a device has no downstream
// devices to poll. The devices.Clear() call inside Stop is a no-op for a
// device that never registered any downstream, so the existing Stop path
// works without change. Sweep too is harmless: the device's own device
// table is always empty.
func (a *Acceptor) ServeDevice(id model.NodeID, tr port.SIPTransport, realm string) error {
	if tr == nil {
		return fmt.Errorf("app: acceptor for device node %s requires a transport", id)
	}
	if strings.TrimSpace(realm) == "" {
		return fmt.Errorf("app: acceptor for device node %s requires a realm", id)
	}
	// The policy is a placeholder so the sweep beat can be derived; the
	// device never serves REGISTERs, so the granted lifetime is unused.
	policy, err := model.NewExpiresPolicy(3600, 3600, 3600)
	if err != nil {
		return fmt.Errorf("app: build device policy for node %s: %w", id, err)
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
		isDevice:    true,
		// catalogPollEvery left at its zero value: ServeDevice has no
		// poller, and the "poller active" guard in run stays false.
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
