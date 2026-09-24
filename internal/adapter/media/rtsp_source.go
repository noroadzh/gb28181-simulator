package media

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strings"
	"sync"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// RTSPSource reads an RTP byte stream from an RTSP server.
type RTSPSource struct {
	config model.MediaConfig
	conn   net.Conn
	mu     sync.Mutex
	closed bool
}

// NewRTSPSource builds an RTSP-backed MediaSource.
func NewRTSPSource(config model.MediaConfig) *RTSPSource {
	return &RTSPSource{config: config}
}

// Open connects to the RTSP server and starts the PLAY session.
func (r *RTSPSource) Open(ctx context.Context) (io.ReadCloser, error) {
	addr := normalizeRTSPAddr(r.config.Path)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("rtsp dial: %w", err)
	}
	r.conn = conn

	reader := textproto.NewReader(bufio.NewReader(conn))
	cseq := 1

	if _, err := r.request(conn, reader, "OPTIONS", "*", cseq, nil); err != nil {
		conn.Close()
		return nil, fmt.Errorf("rtsp OPTIONS: %w", err)
	}
	cseq++

	sdp, err := r.request(conn, reader, "DESCRIBE", r.config.Path, cseq, map[string]string{
		"Accept": "application/sdp",
	})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("rtsp DESCRIBE: %w", err)
	}
	_ = sdp
	cseq++

	track := trackURL(r.config.Path)
	session, err := r.request(conn, reader, "SETUP", track, cseq, map[string]string{
		"Transport": "RTP/AVP/TCP;unicast;interleaved=0-1",
	})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("rtsp SETUP: %w", err)
	}
	cseq++

	sid := extractSession(session)
	if sid == "" {
		conn.Close()
		return nil, fmt.Errorf("rtsp: missing Session header in SETUP response")
	}

	if _, err := r.request(conn, reader, "PLAY", r.config.Path, cseq, map[string]string{
		"Session": sid,
		"Range":   "npt=0-",
	}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("rtsp PLAY: %w", err)
	}

	r.mu.Lock()
	r.closed = false
	r.mu.Unlock()

	pr, pw := io.Pipe()
	go r.readRTP(conn, pw, sid)
	return pr, nil
}

func (r *RTSPSource) readRTP(conn net.Conn, w *io.PipeWriter, sid string) {
	defer w.Close()
	defer conn.Close()

	reader := bufio.NewReader(conn)
	for {
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			return
		}
		r.mu.Unlock()

		b, err := reader.ReadByte()
		if err != nil {
			return
		}
		if b != '$' {
			// Not interleaved RTP; skip the line.
			reader.UnreadByte()
			line, _ := reader.ReadString('\n')
			_ = line
			continue
		}

		_, _ = reader.ReadByte()
		lenBuf := make([]byte, 2)
		if _, err := reader.Read(lenBuf); err != nil {
			return
		}
		length := int(lenBuf[0])<<8 | int(lenBuf[1])

		data := make([]byte, length)
		if _, err := reader.Read(data); err != nil {
			return
		}

		if _, err := w.Write(data); err != nil {
			return
		}
	}
}

func (r *RTSPSource) request(conn net.Conn, reader *textproto.Reader, method, url string, cseq int, headers map[string]string) (string, error) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s %s RTSP/1.0\r\nCSeq: %d\r\n", method, url, cseq))
	for k, v := range headers {
		sb.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	sb.WriteString("\r\n")

	if _, err := conn.Write([]byte(sb.String())); err != nil {
		return "", err
	}

	resp, err := reader.ReadLine()
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(resp, "RTSP/1.0 2") {
		return "", fmt.Errorf("rtsp: non-2xx: %s", resp)
	}

	var body strings.Builder
	for {
		line, err := reader.ReadLine()
		if err != nil {
			return "", err
		}
		if line == "" {
			break
		}
		body.WriteString(line)
		body.WriteString("\r\n")
	}
	return body.String(), nil
}

// Close tears down the RTSP session.
func (r *RTSPSource) Close() error {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	if r.conn != nil {
		return r.conn.Close()
	}
	return nil
}

// Config returns the source configuration.
func (r *RTSPSource) Config() model.MediaConfig {
	return r.config
}

func normalizeRTSPAddr(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "rtsp://" + raw
	}
	if idx := strings.Index(raw, "/"); idx >= 0 {
		raw = raw[:idx]
	}
	if !strings.Contains(raw, ":") {
		raw += ":554"
	}
	return raw
}

func trackURL(base string) string {
	base = strings.TrimRight(base, "/") + "/trackID=0"
	return base
}

func extractSession(headers string) string {
	for _, line := range strings.Split(headers, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "session:") {
			return strings.TrimSpace(line[len("session:"):])
		}
	}
	return ""
}
