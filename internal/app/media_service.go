// Package app — MediaService.
package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

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

// RTPDeizerFactory builds an RTP depacketizer for an inbound session. The
// SSRC seeds the reassembler so it can filter stray packets.
type RTPDeizerFactory func(ssrc uint32) port.RTPDeizer

// PSDepacketizerFactory builds a PS depacketizer for an inbound session.
type PSDepacketizerFactory func() port.PSDepacketizer

// MediaService orchestrates the PS/RTP media pipeline for a node. It depends
// only on domain ports and the injected factories (sources, PS packetizers,
// RTP packetizers), so the app layer never has to import internal/adapter.
type MediaService struct {
	factory   MediaSourceFactory
	psFactory PSPacketizerFactory
	rtpFty    RTPizerFactory
	rtpDeFty  RTPDeizerFactory
	psDeFty   PSDepacketizerFactory
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

// SetInboundFactories wires the depacketizer factories for inbound pipelines.
// Call this after NewMediaService if the node needs to accept INVITE-based
// media (platform-small). If not called, NewInboundPipeline returns an error.
func (m *MediaService) SetInboundFactories(rtpDeFty RTPDeizerFactory, psDeFty PSDepacketizerFactory) {
	m.rtpDeFty = rtpDeFty
	m.psDeFty = psDeFty
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

	return readerFor(rc, cfg), src, nil
}

// readerFor prefers a reader the source itself produced with container PTS
// (an mp4 demuxer) over wrapping the raw byte stream in a StreamESReader,
// which would have to synthesise PTS from FPS. The demuxer exposes
// ReadFrame (to coexist with io.Reader on the same type); this bridges it
// to the ESReader facade the pipeline consumes.
func readerFor(rc io.ReadCloser, cfg model.MediaConfig) port.ESReader {
	if fr, ok := rc.(port.ESFrameReader); ok {
		return frameReaderES{fr: fr}
	}
	return port.NewStreamESReader(rc, cfg)
}

// frameReaderES adapts port.ESFrameReader to port.ESReader.
type frameReaderES struct {
	fr port.ESFrameReader
}

func (a frameReaderES) Read(ctx context.Context) (model.ESFrame, error) {
	return a.fr.ReadFrame(ctx)
}

// InboundPipeline binds an RTPDeizer + PSDepacketizer + ESWriteCloser
// into a single write path that feeds RTP packets inbound. It is intended
// to be run in its own goroutine; the caller should close the writer when
// the session ends or the context is done.
type InboundPipeline struct {
	w      port.ESWriteCloser
	deizer port.RTPDeizer
	depkt  port.PSDepacketizer
	logger *slog.Logger
}

// NewInboundPipeline builds an InboundPipeline for the given session.
// The SSRC is passed to the RTPDeizer so it can filter stray packets.
func (m *MediaService) NewInboundPipeline(ssrc uint32, w port.ESWriteCloser) (*InboundPipeline, error) {
	if m.rtpDeFty == nil || m.psDeFty == nil {
		return nil, fmt.Errorf("app: inbound factories not wired; call SetInboundFactories first")
	}
	if w == nil {
		return nil, fmt.Errorf("app: nil ESWriteCloser")
	}
	return &InboundPipeline{
		w:      w,
		deizer: m.rtpDeFty(ssrc),
		depkt:  m.psDeFty(),
		logger: m.logger,
	}, nil
}

// WriteRTP ingests one inbound RTP datagram, reassembles PS frames via the
// deizer, depacketizes them into ES frames, and writes them to the bound
// writer. It returns when the writer errors or the context is done.
func (p *InboundPipeline) WriteRTP(ctx context.Context, pkt model.RTPPacket) error {
	ps, err := p.deizer.Write(pkt)
	if err != nil {
		return fmt.Errorf("app: deizer: %w", err)
	}
	if ps.Payload == nil {
		return nil
	}
	frames, err := p.depkt.Write(ps.Payload)
	if err != nil {
		return fmt.Errorf("app: depacketizer: %w", err)
	}
	for _, f := range frames {
		if err := p.w.Write(ctx, f); err != nil {
			return fmt.Errorf("app: write ES: %w", err)
		}
	}
	return nil
}

// Close tears down the inbound pipeline. It closes the depacketizer and the
// underlying writer; the deizer is closed last.
func (p *InboundPipeline) Close() error {
	var first error
	for _, c := range []func() error{p.depkt.Close, p.w.Close, p.deizer.Close} {
		if c == nil {
			continue
		}
		if err := c(); err != nil && first == nil {
			first = err
		}
	}
	return first
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
		reader: readerFor(rc, cfg),
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

// SubscribePS opens the media source for cfg, reads ES frames, and feeds them
// through the PS packetizer, emitting PS frames onto the returned channel.
// The channel is closed when the source ends, ctx is cancelled, or onPS returns
// an error. The source is closed when the channel is drained or cancelled.
//
// This method is the PS-only variant of PacketizeOutbound, intended for the
// HTTP-FLV streaming gateway which needs raw PS frames (not RTP datagrams).
// The caller is responsible for closing the returned cleanup function.
func (m *MediaService) SubscribePS(ctx context.Context, cfg model.MediaConfig) (psCh <-chan model.PSFrame, cleanup func(), err error) {
	cfg = cfg.Normalize()
	m.logger.Info("media: SubscribePS called", "cfg_kind", cfg.Kind, "cfg_path", cfg.Path)
	if err := cfg.Validate(); err != nil {
		return nil, nil, fmt.Errorf("app: invalid media config: %w", err)
	}
	if m.factory == nil {
		return nil, nil, fmt.Errorf("app: no media source factory wired")
	}
	if m.psFactory == nil {
		return nil, nil, fmt.Errorf("app: no PS factory wired")
	}

	src := m.factory(cfg)
	if src == nil {
		return nil, nil, fmt.Errorf("app: no media source factory for kind %q", cfg.Kind)
	}

	rc, err := src.Open(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("app: open source: %w", err)
	}

	psPktz := m.psFactory()
	if psPktz == nil {
		_ = src.Close()
		return nil, nil, fmt.Errorf("app: no PS packetizer factory wired")
	}

	psChRaw := make(chan model.PSFrame, 256) // generous buffer: lets reader run ahead without blocking on runPipeline
	reader := readerFor(rc, cfg)

	// Ownership model: only the reader goroutine closes psChRaw, so no other
	// closer can race with its send on that channel. cleanup closes the source
	// and signals done, then synchronously waits for the reader to exit (it
	// closes psChRaw in its defer before signaling readerFinished).
	//
	// Termination paths:
	//  - EOF / read error: reader exits on its own, closes channel.
	//  - ctx cancel: reader's select sees ctx.Done and exits.
	//  - cleanup: closes src (unblocking Read), closes done; reader exits via
	//    the select guard or the send-select done branch.
	done := make(chan struct{})
	readerFinished := make(chan struct{})
	var (
		closeCh  sync.Once // channel closure, reader-owned
		closeSrc sync.Once // source closure, cleanup-owned
	)
	cleanup = func() {
		closeSrc.Do(func() {
			_ = src.Close()
			close(done)
		})
		<-readerFinished
	}

	go func() {
		// LIFO defers: close channel first, then signal completion.
		defer close(readerFinished)
		defer closeCh.Do(func() { close(psChRaw) })
		m.logger.Info("media: SubscribePS reader goroutine started",
			"cfg_kind", cfg.Kind, "cfg_path", cfg.Path)
		frameCount := 0
		defer func() {
			m.logger.Info("media: SubscribePS reader goroutine exiting",
				"cfg_kind", cfg.Kind, "cfg_path", cfg.Path, "frames_sent", frameCount)
		}()
		for {
			// Guard: exit immediately if either signal is set, before
			// attempting a potentially blocking Read. This prevents goroutine
			// leaks when the subscriber (HandleFLV) disappears without draining
			// psChRaw, leaving the next Read to block forever.
			select {
			case <-ctx.Done():
				m.logger.Info("media: SubscribePS reader exiting (ctx done)",
					"cfg_kind", cfg.Kind, "cfg_path", cfg.Path)
				return
			case <-done:
				m.logger.Info("media: SubscribePS reader exiting (done signal)",
					"cfg_kind", cfg.Kind)
				return
			default:
			}
			frame, err := reader.Read(ctx)
			if err != nil {
				if err == io.EOF {
					m.logger.Info("media: SubscribePS reader EOF", "cfg_kind", cfg.Kind)
					return
				}
				m.logger.Debug("media: SubscribePS reader error",
					"cfg_kind", cfg.Kind, "error", err.Error())
				return
			}
			ps, err := psPktz.Packetize(frame)
			if err != nil {
				m.logger.Debug("media: PS packetize error, skipping frame", "error", err.Error())
				continue
			}
			select {
			case <-ctx.Done():
				m.logger.Debug("media: SubscribePS send ctx cancelled", "cfg_kind", cfg.Kind)
				return
			case <-done:
				m.logger.Debug("media: SubscribePS send done signal", "cfg_kind", cfg.Kind)
				return
			case psChRaw <- ps:
				frameCount++
				// Sent successfully. Loop back to read the next frame.
			}
		}
	}()

	return psChRaw, cleanup, nil
}
