// Package app implements the SIP acceptor handling device registration, message
// routing, and media pipeline management for GB/T 28181 simulation.
//
// This file contains expired entry cleanup logic (Cluster 3): sweepLoop, sweep,
// sweepIntervalFor.
package app

import (
	"context"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// Sweeping bounds how stale the online table may get. A device is thrown
// out soon after the lifetime it was granted lapses — half that lifetime
// at the latest — but a platform with a long window must not spend its
// time re-reading a large table, so the beat is kept inside these bounds.
const (
	sweepMinInterval = time.Second
	sweepMaxInterval = 30 * time.Second
)

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
