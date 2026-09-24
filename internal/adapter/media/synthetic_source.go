package media

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"sync"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// SyntheticSource produces a looping series of frames rendered from a
// procedural function, suitable for testing and demos without an external
// media source.
type SyntheticSource struct {
	config  model.MediaConfig
	pipe    *io.PipeWriter
	pipeRdr *io.PipeReader
	stop    chan struct{}
	stopped sync.Once
	mu      sync.Mutex
	closed  bool
}

// NewSyntheticSource builds a synthetic frame generator.
func NewSyntheticSource(config model.MediaConfig) *SyntheticSource {
	return &SyntheticSource{
		config: config,
		stop:   make(chan struct{}),
	}
}

// Open starts the render loop in a background goroutine and returns a
// pipe reader that yields PNG-encoded image frames. The goroutine is
// cancelled when the reader is closed or the context is done.
func (s *SyntheticSource) Open(ctx context.Context) (io.ReadCloser, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, context.Canceled
	}
	pipeRdr, pipeWtr := io.Pipe()
	s.pipe = pipeWtr
	s.pipeRdr = pipeRdr
	s.mu.Unlock()

	go s.renderLoop(pipeWtr, s.config, ctx)
	return pipeRdr, nil
}

func (s *SyntheticSource) renderLoop(w *io.PipeWriter, cfg model.MediaConfig, ctx context.Context) {
	defer w.Close()

	width := 640
	height := 480
	fps := cfg.FPS
	if fps <= 0 {
		fps = 25
	}

	frameDuration := time.Second / time.Duration(fps)
	ticker := time.NewTicker(frameDuration)
	defer ticker.Stop()

	tick := int64(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-ticker.C:
		}

		img := s.renderFrame(width, height, tick)
		tick++

		enc := png.Encoder{CompressionLevel: png.NoCompression}
		if err := enc.Encode(w, img); err != nil {
			return
		}
	}
}

// renderFrame draws a procedural test pattern (a shifting colour gradient
// with a circle) and returns it as an image.RGBA.
func (s *SyntheticSource) renderFrame(width, height int, t int64) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	f := float64(t) * 0.04
	cx, cy := float64(width)/2, float64(height)/2
	radius := float64(height) * 0.35
	for y := 0; y < height; y++ {
		py := float64(y) - cy
		for x := 0; x < width; x++ {
			px := float64(x) - cx
			dist := math.Hypot(px, py)
			inside := dist < radius

			var r, g, b uint8
			if inside {
				// Colour fills the circle with a wave pattern.
				wave := 0.5 + 0.5*math.Cos(f*3+dist/radius*math.Pi*4)
				phase := 0.5 + 0.5*math.Sin(f*2)
				r = uint8(wave * 200 * (0.5 + 0.5*math.Cos(phase*math.Pi*2)))
				g = uint8(wave * 200 * (0.5 + 0.5*math.Sin(phase*math.Pi*2)))
				b = uint8(wave * 200 * (0.5 + 0.5*math.Cos(f*5)))
			} else {
				// Background is a dark gradient.
				r = uint8(float64(x) / float64(width) * 20)
				g = uint8(float64(y) / float64(height) * 20)
				b = 10
			}
			img.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return img
}

// Close cancels the render loop. It is safe to call multiple times.
func (s *SyntheticSource) Close() error {
	var err error
	s.stopped.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		close(s.stop)
		if s.pipe != nil {
			s.pipe.Close()
		}
	})
	return err
}

// Config returns the source configuration.
func (s *SyntheticSource) Config() model.MediaConfig {
	return s.config
}
