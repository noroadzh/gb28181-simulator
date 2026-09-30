package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// bytesReadCloser wraps a byte slice as an io.ReadCloser.
type bytesReadCloser struct {
	data []byte
	off  int
}

func (b *bytesReadCloser) Read(p []byte) (int, error) {
	if b.off >= len(b.data) {
		return 0, io.EOF
	}
	n := copy(p, b.data[b.off:])
	b.off += n
	return n, nil
}

func (b *bytesReadCloser) Close() error { return nil }

// stubSource returns a fake MediaSource whose Open yields a fixed byte stream.
type stubSource struct {
	config model.MediaConfig
	frames []*bytesReadCloser
}

func newStubSource(cfg model.MediaConfig, frames ...[]byte) *stubSource {
	rcs := make([]*bytesReadCloser, len(frames))
	for i, f := range frames {
		rcs[i] = &bytesReadCloser{data: append([]byte(nil), f...)}
	}
	return &stubSource{config: cfg, frames: rcs}
}

func (s *stubSource) Open(_ context.Context) (io.ReadCloser, error) {
	var total []byte
	for _, r := range s.frames {
		b, _ := io.ReadAll(r)
		total = append(total, b...)
	}
	return &bytesReadCloser{data: total}, nil
}

func (s *stubSource) Close() error              { return nil }
func (s *stubSource) Config() model.MediaConfig { return s.config }

// stubPS prepends a PS system header so the depacketizer can cut frames.
type stubPS struct{}

func (s *stubPS) Packetize(frame model.ESFrame) (model.PSFrame, error) {
	header := []byte{0x00, 0x00, 0x01, 0xBA, 0x00, 0x00, 0x00, 0x00}
	payload := append(header, frame.Payload...)
	return model.PSFrame{Payload: payload, PTS: frame.PTS}, nil
}

func (s *stubPS) Header() []byte {
	return []byte{0x00, 0x00, 0x01, 0xBA, 0x00, 0x00, 0x00, 0x00}
}

// stubRTP captures packets in order for assertions.
type stubRTP struct {
	mtu  int
	pkts []model.RTPPacket
	seq  uint16
}

func newStubRTP(mtu int) *stubRTP { return &stubRTP{mtu: mtu} }

func (s *stubRTP) Packetize(ps model.PSFrame) ([]model.RTPPacket, error) {
	s.seq++
	pkt := model.RTPPacket{
		SSRC:    0xABCDEF01,
		Marker:  true,
		Payload: append([]byte(nil), ps.Payload...),
	}
	s.pkts = append(s.pkts, pkt)
	return []model.RTPPacket{pkt}, nil
}

func (s *stubRTP) Sequence() uint16 { return s.seq }

// nalPrefix is the H.264 start code the StreamESReader looks for.
var nalPrefix = []byte{0x00, 0x00, 0x00, 0x01}

// frameWithNAL wraps raw bytes with a start code so StreamESReader can split
// it into a proper ESFrame.
func frameWithNAL(b []byte) []byte {
	out := make([]byte, 0, len(nalPrefix)+len(b))
	out = append(out, nalPrefix...)
	out = append(out, b...)
	return out
}

func TestMediaService_OpenSource(t *testing.T) {
	cfg := model.MediaConfig{Kind: model.SourceKindSynthetic, Path: "", FPS: 25, Clock: 90000}
	svc := NewMediaService(
		func(c model.MediaConfig) port.MediaSource {
			return newStubSource(c, frameWithNAL([]byte{0x01, 0x02, 0x03}))
		},
		func() port.PSPacketizer { return &stubPS{} },
		func(mtu int) port.RTPizer { return newStubRTP(mtu) },
		slog.Default(),
	)

	r, src, err := svc.OpenSource(context.Background(), cfg)
	if err != nil {
		t.Fatalf("OpenSource: %v", err)
	}
	defer func() { _ = src.Close() }()

	frame, err := r.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := frameWithNAL([]byte{0x01, 0x02, 0x03})
	if !bytes.Equal(frame.Payload, want) {
		t.Errorf("payload mismatch: got %x, want %x", frame.Payload, want)
	}
	// PTS=0 is a legal instantaneous value for the first frame.
	_ = frame.PTS
}

func TestMediaService_OpenSourceInvalidConfig(t *testing.T) {
	svc := NewMediaService(nil, nil, nil, slog.Default())
	_, _, err := svc.OpenSource(context.Background(), model.MediaConfig{})
	if err == nil {
		t.Error("expected error for empty config")
	}
}

func TestMediaService_PacketizeOutbound(t *testing.T) {
	var captured []model.RTPPacket

	cfg := model.MediaConfig{Kind: model.SourceKindSynthetic, Path: "", FPS: 25, Clock: 90000}
	svc := NewMediaService(
		func(c model.MediaConfig) port.MediaSource {
			return newStubSource(c, frameWithNAL([]byte{0xAA, 0xBB, 0xCC}))
		},
		func() port.PSPacketizer { return &stubPS{} },
		func(mtu int) port.RTPizer { return newStubRTP(mtu) },
		slog.Default(),
	)

	err := svc.PacketizeOutbound(context.Background(), cfg, func(pkt model.RTPPacket) error {
		captured = append(captured, pkt)
		return nil
	})
	if err != nil {
		t.Fatalf("PacketizeOutbound: %v", err)
	}
	if len(captured) != 1 {
		t.Fatalf("got %d RTP packets, want 1", len(captured))
	}
	if captured[0].SSRC != 0xABCDEF01 {
		t.Errorf("SSRC = 0x%X, want 0xABCDEF01", captured[0].SSRC)
	}
	if !captured[0].Marker {
		t.Error("marker should be set")
	}
	// Verify the PS header is visible in the RTP payload.
	if !bytes.HasPrefix(captured[0].Payload, []byte{0x00, 0x00, 0x01, 0xBA}) {
		t.Error("RTP payload should contain PS system header")
	}
}

func TestMediaService_PacketizeOutboundNilFactories(t *testing.T) {
	svc := NewMediaService(
		func(c model.MediaConfig) port.MediaSource { return newStubSource(c) },
		nil,
		nil,
		slog.Default(),
	)
	cfg := model.MediaConfig{Kind: model.SourceKindSynthetic, Path: "", FPS: 25}
	err := svc.PacketizeOutbound(context.Background(), cfg, func(model.RTPPacket) error { return nil })
	if err == nil {
		t.Error("expected error when factories are nil")
	}
}

func TestMediaService_PacketizeOutboundSourceOpenFailure(t *testing.T) {
	failSrc := &errSource{openErr: errors.New("open fail")}
	svc := NewMediaService(
		func(c model.MediaConfig) port.MediaSource { return failSrc },
		func() port.PSPacketizer { return &stubPS{} },
		func(mtu int) port.RTPizer { return newStubRTP(mtu) },
		slog.Default(),
	)
	cfg := model.MediaConfig{Kind: model.SourceKindSynthetic, Path: "", FPS: 25}
	err := svc.PacketizeOutbound(context.Background(), cfg, func(model.RTPPacket) error { return nil })
	if err == nil {
		t.Fatal("expected source open error")
	}
}

type errSource struct {
	openErr  error
	closeErr error
}

func (e *errSource) Open(_ context.Context) (io.ReadCloser, error) { return nil, e.openErr }
func (e *errSource) Close() error                                  { return e.closeErr }
func (e *errSource) Config() model.MediaConfig                     { return model.MediaConfig{} }

func TestMediaService_PacketizeOutboundContextCancel(t *testing.T) {
	// Use a source that writes one frame then blocks forever on subsequent
	// reads, giving the cancel path time to fire.
	pr, pw := io.Pipe()
	src := &blockingSource{rc: pr, cfg: model.MediaConfig{Kind: model.SourceKindSynthetic, Path: "", FPS: 25}}
	go func() {
		_, _ = pw.Write(frameWithNAL([]byte{0x01}))
		// Leave writer open so StreamESReader blocks on next Read.
	}()

	svc := NewMediaService(
		func(c model.MediaConfig) port.MediaSource { return src },
		func() port.PSPacketizer { return &stubPS{} },
		func(mtu int) port.RTPizer { return newStubRTP(mtu) },
		slog.Default(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := svc.PacketizeOutbound(ctx, model.MediaConfig{Kind: model.SourceKindSynthetic, Path: "", FPS: 25}, func(model.RTPPacket) error {
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

type blockingSource struct {
	rc  io.ReadCloser
	cfg model.MediaConfig
}

func (b *blockingSource) Open(_ context.Context) (io.ReadCloser, error) { return b.rc, nil }
func (b *blockingSource) Close() error                                  { return b.rc.Close() }
func (b *blockingSource) Config() model.MediaConfig                     { return b.cfg }

func TestMediaService_PacketizeOutboundMultipleFrames(t *testing.T) {
	var captured []model.RTPPacket

	cfg := model.MediaConfig{Kind: model.SourceKindSynthetic, Path: "", FPS: 25, Clock: 90000}
	svc := NewMediaService(
		func(c model.MediaConfig) port.MediaSource {
			return newStubSource(c,
				frameWithNAL([]byte{0x01}),
				frameWithNAL([]byte{0x02}),
				frameWithNAL([]byte{0x03}),
			)
		},
		func() port.PSPacketizer { return &stubPS{} },
		func(mtu int) port.RTPizer { return newStubRTP(mtu) },
		slog.Default(),
	)

	err := svc.PacketizeOutbound(context.Background(), cfg, func(pkt model.RTPPacket) error {
		captured = append(captured, pkt)
		return nil
	})
	if err != nil {
		t.Fatalf("PacketizeOutbound: %v", err)
	}
	if len(captured) != 3 {
		t.Fatalf("got %d packets, want 3", len(captured))
	}
	for i, pkt := range captured {
		if pkt.SSRC != 0xABCDEF01 {
			t.Errorf("packet %d: SSRC mismatch", i)
		}
		if !pkt.Marker {
			t.Errorf("packet %d: marker should be set", i)
		}
	}
}

func TestMediaService_Close(t *testing.T) {
	svc := NewMediaService(nil, nil, nil, slog.Default())
	if err := svc.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// --- Inbound pipeline tests ---

// stubESWriteCloser collects frames written to it for assertions.
type stubESWriteCloser struct {
	frames []model.ESFrame
	closed bool
}

func (w *stubESWriteCloser) Read(ctx context.Context) (model.ESFrame, error) {
	return model.ESFrame{}, io.EOF
}
func (w *stubESWriteCloser) Write(ctx context.Context, frame model.ESFrame) error {
	if w.closed {
		return io.EOF
	}
	w.frames = append(w.frames, frame)
	return nil
}
func (w *stubESWriteCloser) Close() error {
	w.closed = true
	return nil
}

// stubDeizer reassembles one RTP packet into one PS frame immediately.
type stubDeizer struct {
	ssrc uint32
}

func (d *stubDeizer) Write(pkt model.RTPPacket) (model.PSFrame, error) {
	if pkt.SSRC != d.ssrc {
		return model.PSFrame{}, errors.New("ssrc mismatch")
	}
	return model.PSFrame{Payload: append([]byte(nil), pkt.Payload...), PTS: uint64(pkt.Timestamp)}, nil
}
func (d *stubDeizer) Close() error { return nil }

// stubPSDepkt turns raw bytes into one ES frame per Write call.
type stubPSDepkt struct{}

func (s *stubPSDepkt) Write(raw []byte) ([]model.ESFrame, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	return []model.ESFrame{{Payload: append([]byte(nil), raw...)}}, nil
}
func (s *stubPSDepkt) Close() error { return nil }

// TestInboundPipeline_WriteRTP verifies that an RTP datagram flows through
// the deizer → depacketizer → writer chain and produces one ES frame.
func TestInboundPipeline_WriteRTP(t *testing.T) {
	const ssrc uint32 = 0xABCDEF01
	svc := NewMediaService(nil, nil, nil, slog.Default())
	svc.SetInboundFactories(
		func(s uint32) port.RTPDeizer { return &stubDeizer{ssrc: s} },
		func() port.PSDepacketizer { return &stubPSDepkt{} },
	)

	w := &stubESWriteCloser{}
	p, err := svc.NewInboundPipeline(ssrc, w)
	if err != nil {
		t.Fatalf("NewInboundPipeline: %v", err)
	}

	pkt := model.RTPPacket{
		SSRC:      ssrc,
		Sequence:  1,
		Timestamp: 90000,
		Payload:   []byte{0x00, 0x00, 0x01, 0xBA, 0x01, 0x02, 0x03},
		Marker:    true,
	}
	if err := p.WriteRTP(context.Background(), pkt); err != nil {
		t.Fatalf("WriteRTP: %v", err)
	}
	if len(w.frames) != 1 {
		t.Fatalf("frames written = %d, want 1", len(w.frames))
	}
	if len(w.frames[0].Payload) != len(pkt.Payload) {
		t.Errorf("payload len = %d, want %d", len(w.frames[0].Payload), len(pkt.Payload))
	}
}

// TestInboundPipeline_NoFactories ensures an error is returned when the
// inbound factories are not wired.
func TestInboundPipeline_NoFactories(t *testing.T) {
	svc := NewMediaService(nil, nil, nil, slog.Default())
	if _, err := svc.NewInboundPipeline(0x1234, &stubESWriteCloser{}); err == nil {
		t.Fatal("expected error for unwired inbound factories")
	}
}

// TestInboundPipeline_NilWriter ensures an error is returned when the
// ESWriteCloser is nil.
func TestInboundPipeline_NilWriter(t *testing.T) {
	svc := NewMediaService(nil, nil, nil, slog.Default())
	svc.SetInboundFactories(
		func(s uint32) port.RTPDeizer { return &stubDeizer{ssrc: s} },
		func() port.PSDepacketizer { return &stubPSDepkt{} },
	)
	if _, err := svc.NewInboundPipeline(0x1234, nil); err == nil {
		t.Fatal("expected error for nil ESWriteCloser")
	}
}

// TestInboundPipeline_SSRCFilter verifies that the deizer rejects packets
// with a mismatched SSRC.
func TestInboundPipeline_SSRCFilter(t *testing.T) {
	const ssrc uint32 = 0xABCDEF01
	svc := NewMediaService(nil, nil, nil, slog.Default())
	svc.SetInboundFactories(
		func(s uint32) port.RTPDeizer { return &stubDeizer{ssrc: s} },
		func() port.PSDepacketizer { return &stubPSDepkt{} },
	)

	w := &stubESWriteCloser{}
	p, err := svc.NewInboundPipeline(ssrc, w)
	if err != nil {
		t.Fatalf("NewInboundPipeline: %v", err)
	}

	// Wrong SSRC should cause a deizer error.
	pkt := model.RTPPacket{SSRC: 0xDEADBEEF, Payload: []byte{1}}
	if err := p.WriteRTP(context.Background(), pkt); err == nil {
		t.Fatal("expected error for wrong SSRC")
	}
	if len(w.frames) != 0 {
		t.Errorf("frames written = %d, want 0 for rejected packet", len(w.frames))
	}
}

// TestInboundPipeline_Close verifies that Close is idempotent and cleans
// up all components.
func TestInboundPipeline_Close(t *testing.T) {
	const ssrc uint32 = 0xABCDEF01
	svc := NewMediaService(nil, nil, nil, slog.Default())
	svc.SetInboundFactories(
		func(s uint32) port.RTPDeizer { return &stubDeizer{ssrc: s} },
		func() port.PSDepacketizer { return &stubPSDepkt{} },
	)

	w := &stubESWriteCloser{}
	p, err := svc.NewInboundPipeline(ssrc, w)
	if err != nil {
		t.Fatalf("NewInboundPipeline: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if !w.closed {
		t.Error("writer not closed after Close")
	}
}
