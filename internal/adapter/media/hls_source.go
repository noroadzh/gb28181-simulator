package media

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// HLSSource reads a byte stream from an HLS playlist.
type HLSSource struct {
	config model.MediaConfig
	client *http.Client
	mu     sync.Mutex
	closed bool

	// cancel interrupts the in-flight HTTP request that stream is blocked
	// on. pw is the writer half of the pipe that the reader is waiting on;
	// closing it with an error wakes the reader with a non-EOF result so
	// that "we stopped this" is distinguishable from "the source ran out".
	cancel context.CancelFunc
	pw     *io.PipeWriter
}

// NewHLSSource builds an HLS-backed MediaSource.
func NewHLSSource(config model.MediaConfig) *HLSSource {
	return &HLSSource{
		config: config,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Open fetches the playlist and starts streaming segments.
func (h *HLSSource) Open(ctx context.Context) (io.ReadCloser, error) {
	// Use a child of the caller's context: the stream goroutine is not
	// allowed to outlive either the caller cancelling or the source being
	// closed, and a cancellable child makes both paths expressible without
	// a custom signal channel.
	runCtx, cancel := context.WithCancel(ctx)
	segments, err := h.fetchSegments(runCtx)
	if err != nil {
		cancel()
		return nil, err
	}
	if len(segments) == 0 {
		cancel()
		return nil, fmt.Errorf("hls: no segments in playlist")
	}

	pr, pw := io.Pipe()

	h.mu.Lock()
	h.closed = false
	h.cancel = cancel
	h.pw = pw
	h.mu.Unlock()

	go h.stream(runCtx, segments, pw)
	return pr, nil
}

func (h *HLSSource) fetchSegments(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", h.config.Path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return parseM3U8(string(body), h.config.Path), nil
}

func (h *HLSSource) stream(ctx context.Context, segments []string, w *io.PipeWriter) {
	defer w.Close()

	for _, seg := range segments {
		select {
		case <-ctx.Done():
			return
		default:
		}

		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return
		}
		h.mu.Unlock()

		req, err := http.NewRequestWithContext(ctx, "GET", seg, nil)
		if err != nil {
			continue
		}
		resp, err := h.client.Do(req)
		if err != nil {
			continue
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}

		if _, err := w.Write(data); err != nil {
			return
		}
	}
}

// Close stops the segment downloader. It is idempotent: a second call sees
// the closed flag and returns nil without touching anything.
//
// Three things have to happen, in any order, for a slow segment fetch to
// stop promptly: cancel() releases the HTTP client blocked on client.Do
// and io.ReadAll, pw.CloseWithError wakes the reader with a non-EOF error
// (so the call site can tell "we closed it" from "the source ended"), and
// the closed flag stops a future Open from racing the in-flight stream.
// We pull cancel and pw out under the lock and use them outside it so a
// concurrent Open cannot observe them half-set.
func (h *HLSSource) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	cancel := h.cancel
	pw := h.pw
	h.cancel = nil
	h.pw = nil
	h.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if pw != nil {
		pw.CloseWithError(ErrSourceClosed)
	}
	return nil
}

// Config returns the source configuration.
func (h *HLSSource) Config() model.MediaConfig {
	return h.config
}

func parseM3U8(body, base string) []string {
	baseURL, err := url.Parse(base)
	if err != nil {
		return nil
	}
	var segs []string
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		u, err := url.Parse(line)
		if err != nil {
			continue
		}
		if u.IsAbs() {
			segs = append(segs, u.String())
		} else {
			segs = append(segs, baseURL.ResolveReference(u).String())
		}
	}
	return segs
}
