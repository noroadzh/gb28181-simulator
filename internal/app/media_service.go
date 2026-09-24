// Package app — MediaService.
package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// MediaSourceFactory builds a MediaSource for a given configuration.
// It lives in the app composition root so the app can dispatch sources
// without importing internal/adapter.
type MediaSourceFactory func(cfg model.MediaConfig) port.MediaSource

// PSPacketizerFactory builds a PS packetizer for a session. The MTU/SSRC
// defaults are sourced from cfg so callers do not have to thread them
// themselves.
type PSPacketizerFactory func() port.PSPacketizer

// RTPizerFactory builds an RTP packetizer for a session. The MTU is
// passed in so the packetizer can size its payloads per-source.
type RTPizerFactory func(mtu int) port.RTPizer

// MediaService orchestrates the PS/RTP media pipeline for a node. It depends
// only on domain ports and the injected factories (sources, PS packetizers,
// RTP packetizers), so the app layer never has to import internal/adapter.
type MediaService struct {
	factory   MediaSourceFactory
	psFactory PSPacketizerFactory
	rtpFty    RTPizerFactory
	logger    *slog.Logger
}

// NewMediaService builds a MediaService. The logger is optional and falls
// back to slog.Default. The PS/RTP factories are required.
func NewMediaService(factory MediaSourceFactory, psFactory PSPacketizerFactory, rtpFactory RTPizerFactory, logger *slog.Logger) *MediaService {
	if logger == nil {
		logger = slog.Default()
	}
	return &MediaService{
		factory:   factory,
		psFactory: psFactory,
		rtpFty:    rtpFactory,
		logger:    logger,
	}
}

// OpenSource opens the media source described by cfg and returns an
// ESReader over it. The caller is responsible for closing both the
// returned reader (which closes the underlying stream) and the source
// itself when the session ends.
func (m *MediaService) OpenSource(ctx context.Context, cfg model.MediaConfig) (port.ESReader, port.MediaSource, error) {
	cfg = cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return nil, nil, fmt.Errorf("app: invalid media config: %w", err)
	}

	src := m.factory(cfg)
	if src == nil {
		return nil, nil, fmt.Errorf("app: no media source factory for kind %q", cfg.Kind)
	}

	rc, err := src.Open(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("app: open source: %w", err)
	}

	return port.NewStreamESReader(rc, cfg), src, nil
}

// outboundPipeline holds the assembled packetizers for one session.
type outboundPipeline struct {
	reader port.ESReader
	ps     port.PSPacketizer
	rtp    port.RTPizer
	src    port.MediaSource
}

// PacketizeOutbound opens the source for cfg, reads ES frames, and feeds them
// through the PS and RTP packetizers, invoking onRTP for every RTP datagram
// produced. It returns when the source ends, the context is done, or onRTP
// returns an error. The source and any packetizers are closed on the way out.
func (m *MediaService) PacketizeOutbound(ctx context.Context, cfg model.MediaConfig, onRTP func(model.RTPPacket) error) error {
	cfg = cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("app: invalid media config: %w", err)
	}
	if m.factory == nil {
		return fmt.Errorf("app: no media source factory wired")
	}
	if m.psFactory == nil || m.rtpFty == nil {
		return fmt.Errorf("app: no PS/RTP factories wired")
	}

	src := m.factory(cfg)
	if src == nil {
		return fmt.Errorf("app: no media source factory for kind %q", cfg.Kind)
	}

	rc, err := src.Open(ctx)
	if err != nil {
		return fmt.Errorf("app: open source: %w", err)
	}

	psPktz := m.psFactory()
	rtpPktz := m.rtpFty(cfg.MTU)
	if psPktz == nil || rtpPktz == nil {
		_ = src.Close()
		return fmt.Errorf("app: media service has no PS/RTP factories wired")
	}

	p := &outboundPipeline{
		reader: port.NewStreamESReader(rc, cfg),
		ps:     psPktz,
		rtp:    rtpPktz,
		src:    src,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- p.run(ctx, onRTP)
	}()

	select {
	case <-ctx.Done():
		_ = p.close()
		// Wait for the run goroutine to finish draining so callers
		// can safely read any state it was mutating (e.g. a shared
		// capture slice) after we return.
		<-errCh
		return ctx.Err()
	case err := <-errCh:
		_ = p.close()
		return err
	}
}

func (p *outboundPipeline) close() error {
	return p.src.Close()
}

func (p *outboundPipeline) run(ctx context.Context, onRTP func(model.RTPPacket) error) error {
	for {
		frame, err := p.reader.Read(ctx)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("app: ES read: %w", err)
		}

		ps, err := p.ps.Packetize(frame)
		if err != nil {
			continue
		}

		pkts, err := p.rtp.Packetize(ps)
		if err != nil {
			continue
		}
		for _, pkt := range pkts {
			if err := onRTP(pkt); err != nil {
				return err
			}
		}
	}
}

// Close tears down the media service. It returns the first non-nil error
// encountered while closing sources.
func (m *MediaService) Close() error {
	return nil
}
