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
	segments, err := h.fetchSegments(ctx)
	if err != nil {
		return nil, err
	}
	if len(segments) == 0 {
		return nil, fmt.Errorf("hls: no segments in playlist")
	}

	h.mu.Lock()
	h.closed = false
	h.mu.Unlock()

	pr, pw := io.Pipe()
	go h.stream(ctx, segments, pw)
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

// Close stops the segment downloader.
func (h *HLSSource) Close() error {
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
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
