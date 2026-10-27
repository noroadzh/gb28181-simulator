package app

// Media pipeline handling for the SIP Acceptor.
//
// An INVITE that carries a usable SDP needs a media destination. Two paths
// exist and both land in the same Acceptor.pipelines map:
//
//   - device node with a MediaConfig: open a UDP socket to the address the
//     peer's SDP names and pump the configured source into it as RTP
//     (outboundPipelineEntry).
//   - platform (or a device without a media source): keep the historical
//     InboundPipeline with a nullESWriter sink so the dialog lifecycle
//     stays consistent (inboundOnlyPipeline).
//
// pipelineEntry unifies teardown, so closePipeline does not need to know
// which path a given call took.

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// nullESWriter is a port.ESWriteCloser that discards everything; the media
// pipeline passes it as the ES-sink when no real consumer is configured yet.
type nullESWriter struct{}

func (nullESWriter) Read(ctx context.Context) (model.ESFrame, error) {
	return model.ESFrame{}, io.EOF
}
func (nullESWriter) Write(ctx context.Context, frame model.ESFrame) error { return nil }
func (nullESWriter) Close() error                                         { return nil }

// parseSDPMetadata scans the INVITE body for the first audio/video m= line
// and returns its media type, port, and protocol. An empty mediaType signals
// no media description was found.
func parseSDPMetadata(body string) (mediaType, portStr, protocol string, _ error) {
	if body == "" {
		return "", "", "", errors.New("empty body")
	}
	if !strings.Contains(body, "v=0") {
		return "", "", "", errors.New("SDP missing v=0")
	}
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "m=") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		m, p, rest := fields[0][2:], fields[1], fields[2:]
		switch m {
		case "audio", "video":
			if _, err := strconv.Atoi(p); err != nil || p == "0" {
				return "", "", "", errors.New("invalid media port: " + p)
			}
			proto := ""
			if len(rest) > 0 {
				proto = rest[0]
			}
			return m, p, proto, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", "", err
	}
	return "", "", "", nil
}

// extractMediaAddress reads the SDP for the first c=IN IP4/IP6 address.
// It returns the first non-empty address it finds or an empty string when
// the body carries no connection data.
func extractMediaAddress(body string) string {
	if body == "" {
		return ""
	}
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "c=") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			return strings.TrimSpace(fields[2])
		}
	}
	return ""
}

// createInboundPipeline answers a SDP that names a media destination. The
// path depends on the node's role:
//   - device (with a configured MediaConfig): the SDP tells us where to
//     push the configured media to, so we open a UDP socket to the peer
//     and run PacketizeOutbound in a goroutine that emits RTP datagrams
//     from the configured source (file/rtsp/hls/synthetic).
//   - platform-large / platform-small (no media, or media-less device):
//     keep the historical behaviour: build an InboundPipeline with a
//     nullESWriter so the dialog lifecycle stays consistent. A future
//     "record the inbound stream" change can swap the writer.
//
// The function never returns an error: any failure to open a sink is
// logged and the call is treated as a no-op so a misconfigured SDP does
// not break the dialog's 200 OK.
func (a *Acceptor) createInboundPipeline(
	ctx context.Context,
	p *platform,
	callID, mediaType, portStr, sdpBody string,
) {
	media := a.mediaService
	if media == nil {
		return
	}
	// An in-dialog INVITE (re-INVITE) replaces the dialog's media: stop the
	// pipeline the first INVITE started before building the new one, or the
	// old goroutine keeps sending to the old endpoint forever.
	a.closePipeline(callID)
	address := extractMediaAddress(sdpBody)
	if address == "" || portStr == "" {
		a.log.Debug("INVITE without usable media section; skipping pipeline",
			"node_id", p.id.String(), "call_id", callID, "media", mediaType,
			"address", address, "port", portStr)
		return
	}

	// Look up the node's profile to decide whether to send or to receive.
	// The acceptor already has a NodeRegistry attached (WithNodeRegistry);
	// without one we fall back to the historical "receive and discard"
	// behaviour so a platform without a registry still serves.
	var (
		haveMedia bool
		mediaCfg  model.MediaConfig
	)
	if a.registry != nil {
		if node, ok := a.registry.Get(ctx, p.id); ok {
			if cfg, has := node.Profile().MediaConfig(); has {
				haveMedia = true
				mediaCfg = cfg
			}
		}
	}

	if !haveMedia {
		pl, plErr := media.NewInboundPipeline(uint32(0), nullESWriter{})
		if plErr != nil {
			a.log.Warn("cannot create inbound pipeline",
				"call_id", callID, "error", plErr.Error())
			return
		}
		a.mu.Lock()
		a.pipelines[callID] = inboundOnlyPipeline{pl: pl}
		a.mu.Unlock()
		a.log.Debug("inbound pipeline created (no media config)",
			"node_id", p.id.String(), "call_id", callID,
			"media", mediaType, "address", address)
		return
	}

	// Device path: send the configured source to the peer's SDP address.
	udpAddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(address, portStr))
	if err != nil {
		a.log.Warn("cannot resolve media peer",
			"node_id", p.id.String(), "call_id", callID,
			"address", address, "port", portStr, "error", err.Error())
		return
	}
	udpConn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		a.log.Warn("cannot dial media peer",
			"node_id", p.id.String(), "call_id", callID,
			"address", udpAddr.String(), "error", err.Error())
		return
	}
	a.log.Info("media pipeline started (device → peer)",
		"node_id", p.id.String(), "call_id", callID,
		"peer", udpAddr.String(), "kind", mediaCfg.Kind, "path", mediaCfg.Path)
	// PacketizeOutbound blocks until the source ends or the context
	// closes. We give it a fresh context (derived from the call's) so
	// closing the pipeline later can stop the goroutine without
	// cancelling the caller's whole request tree.
	pipelineCtx, cancel := context.WithCancel(ctx)
	entry := outboundPipelineEntry{conn: udpConn, cancel: cancel, done: make(chan struct{})}
	a.mu.Lock()
	a.pipelines[callID] = entry
	a.mu.Unlock()
	go func() {
		defer close(entry.done)
		defer func() { _ = udpConn.Close() }()
		_ = media.PacketizeOutbound(pipelineCtx, mediaCfg, func(pkt model.RTPPacket) error {
			_, werr := udpConn.Write(pkt.Bytes())
			return werr
		})
	}()
}

// pipelineEntry is the value the Acceptor's pipeline map stores. A device
// INVITE triggers an outbound RTP push (outboundPipelineEntry); a platform
// INVITE without a mediaConfig keeps the historical InboundPipeline with
// a null writer. The interface unifies teardown so closePipeline stays
// agnostic about which path was taken.
type pipelineEntry interface {
	Close() error
}

// inboundOnlyPipeline wraps a MediaService InboundPipeline with the
// historical nullESWriter so the platform-large path can still answer an
// INVITE without a configured media source.
type inboundOnlyPipeline struct {
	pl *InboundPipeline
}

func (e inboundOnlyPipeline) Close() error {
	if e.pl == nil {
		return nil
	}
	return e.pl.Close()
}

// outboundPipelineEntry holds the dial-up and the cancellation handle for
// the goroutine that pumps RTP to a peer's SDP-described address.
type outboundPipelineEntry struct {
	conn   *net.UDPConn
	cancel context.CancelFunc
	done   chan struct{}
}

func (e outboundPipelineEntry) Close() error {
	if e.cancel != nil {
		e.cancel()
	}
	if e.done != nil {
		<-e.done
	}
	return nil
}

// closePipeline removes the pipeline entry from the map and closes it.
// Safe to call multiple times for the same callID.
func (a *Acceptor) closePipeline(callID string) {
	a.mu.Lock()
	entry, ok := a.pipelines[callID]
	if ok {
		delete(a.pipelines, callID)
	}
	a.mu.Unlock()
	if ok && entry != nil {
		if err := entry.Close(); err != nil {
			slog.Warn("pipeline close", "call_id", callID, "error", err.Error())
		}
	}
}
