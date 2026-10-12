// Package app — platform-large registration acceptance.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// catalogPoller is the platform side of the catalog pull cycle. It wakes on
// a fixed interval, asks each registered downstream device for its catalog,
// and writes the per-channel MediaSource extensions from the response into
// the receiving node's profile via MutateProfile.
//
// One poller exists per serving node. It is started and stopped alongside
// the node goroutine and never outlives it.
//
// Transport semantics: a SIP transport is a single-consumer queue, so this
// poller only Sends queries; it never Reads from the transport. Responses
// travel back through the acceptor's main read loop, which detects the
// incoming MESSAGE as a Response and dispatches to handleMessage, which in
// turn calls applyMediaSourcesFromCatalog.
type catalogPoller struct {
	id       model.NodeID
	tr       port.SIPTransport
	registry port.DownstreamRegistry
	nodeReg  port.NodeRegistry
	codec    port.MANSCDPCodec
	clock    port.TickerFactory
	log      *slog.Logger
	interval time.Duration
	mu       sync.Mutex
	running  bool
	done     chan struct{}
}

// newCatalogPoller builds a poller for a single serving node. interval is the
// time between two successive poll rounds; zero or negative disables polling
// and start() becomes a no-op. clock is the TickerFactory so tests can inject
// a sync clock.
func newCatalogPoller(
	id model.NodeID,
	tr port.SIPTransport,
	registry port.DownstreamRegistry,
	nodeReg port.NodeRegistry,
	codec port.MANSCDPCodec,
	clock port.TickerFactory,
	log *slog.Logger,
	interval time.Duration,
) *catalogPoller {
	return &catalogPoller{
		id:       id,
		tr:       tr,
		registry: registry,
		nodeReg:  nodeReg,
		codec:    codec,
		clock:    clock,
		log:      log,
		interval: interval,
		done:     make(chan struct{}),
	}
}

// start arms the poller. It is safe to call multiple times.
func (p *catalogPoller) start(ctx context.Context) {
	p.mu.Lock()
	if p.interval <= 0 || p.running {
		p.mu.Unlock()
		return
	}
	p.running = true
	p.mu.Unlock()
	go p.run(ctx)
}

// stop gracefully shuts the poller down and returns when the current round
// has finished its send phase. Safe to call multiple times.
func (p *catalogPoller) stop() {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return
	}
	p.running = false
	close(p.done)
	p.mu.Unlock()
}

// run is the poll loop. It fires immediately on entry (eager first poll so
// the platform discovers the channel media promptly after startup) then every
// interval thereafter. A context-cancel stops the loop.
func (p *catalogPoller) run(ctx context.Context) {
	defer func() { close(p.done) }()

	// Eager first poll.
	p.pollRound(ctx)

	ticker := p.clock(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C():
			p.pollRound(ctx)
		case <-ctx.Done():
			return
		case <-p.done:
			return
		}
	}
}

// pollRound lists the live downstream devices for this node and sends each
// a catalog Query. Responses are processed asynchronously by the acceptor's
// main read loop → handleMessage → applyMediaSourcesFromCatalog.
func (p *catalogPoller) pollRound(ctx context.Context) {
	devs := p.registry.List(ctx, p.id)
	if len(devs) == 0 {
		return
	}

	sn := uint32(time.Now().UnixNano())
	var sent, failed int
	for _, dev := range devs {
		if err := p.sendQuery(ctx, dev, sn); err != nil {
			failed++
			p.log.Warn("catalog query send failed",
				"node_id", p.id.String(), "device_id", dev.DeviceID(), "error", err.Error())
		} else {
			sent++
		}
	}
	if sent > 0 {
		p.log.Debug("catalog poll round complete",
			"node_id", p.id.String(), "sent", sent, "failed", failed)
	}
}

// sendQuery renders and sends one catalog Query to a downstream device.
// It is synchronous — returns when the transport has accepted the message.
// The SIP response comes back through the acceptor's read loop.
func (p *catalogPoller) sendQuery(ctx context.Context, dev model.DownstreamDevice, sn uint32) error {
	body, err := p.codec.MarshalCatalogQuery(dev.DeviceID(), sn)
	if err != nil {
		return fmt.Errorf("marshal catalog query: %w", err)
	}
	uri := fmt.Sprintf("sip:%s@%s", dev.DeviceID(), dev.Addr())
	callID := randomCallID()
	msg, err := model.NewRequest("MESSAGE", uri, []model.Header{
		model.NewHeader("From", fmt.Sprintf("<sip:%s@%s>;tag=%s", p.id.String(), dev.Addr(), randomTag())),
		model.NewHeader("To", fmt.Sprintf("<sip:%s@%s>", dev.DeviceID(), dev.Addr())),
		model.NewHeader("Call-ID", callID),
		model.NewHeader("CSeq", strconv.FormatUint(uint64(sn), 10)+" MESSAGE"),
		model.NewHeader("Content-Type", "Application/MANSCDP+XML"),
		model.NewHeader("Max-Forwards", "70"),
	}, body)
	if err != nil {
		return fmt.Errorf("build MESSAGE: %w", err)
	}
	// dst is the device's contact address; fall back to Addr when the
	// device never advertised one.
	dst := dev.Contact()
	if dst == "" {
		dst = dev.Addr()
	}
	if err := p.tr.Send(ctx, msg, dst); err != nil {
		return fmt.Errorf("send to %s: %w", dst, err)
	}
	p.log.Debug("catalog query sent",
		"node_id", p.id.String(), "device_id", dev.DeviceID(),
		"sn", sn, "dst", dst)
	return nil
}
