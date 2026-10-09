// Package streaming — gateway orchestrates PS-to-FLV translation for live streams.
package streaming

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// ErrStreamingUnsupported is returned when the streaming service cannot
// handle the requested node+channel (e.g. node is not a device, or the
// channel has no media source configured).
type ErrStreamingUnsupported struct {
	NodeID    model.NodeID
	ChannelID string
	Reason    string
}

func (e *ErrStreamingUnsupported) Error() string {
	return "streaming: node " + e.NodeID.String() + " channel " + e.ChannelID + ": " + e.Reason
}

// MediaSourceSupplier is the gateway's view of MediaService: it returns a
// channel of PS frames for the requested node+channel. The gateway consumes
// PS frames, parses them, and emits FLV tags.
//
// A nil MediaSourceSupplier causes Subscribe to return ErrStreamingUnsupported.
type MediaSourceSupplier interface {
	// SubscribePS starts an outbound PS pipeline for the given node+channel
	// and returns a channel of model.PSFrame. The caller (gateway) is
	// responsible for draining the channel and not blocking the source.
	SubscribePS(ctx context.Context, nodeID model.NodeID, channelID string) (<-chan model.PSFrame, error)
}

// NodeSourceFunc is the canonical implementation: it takes a node+channel
// and returns the appropriate MediaConfig plus a "no source" flag.
type NodeSourceFunc func(ctx context.Context, nodeID model.NodeID, channelID string) (model.MediaConfig, bool)

// Gateway is the HTTP-FLV streaming gateway. It brokers per-(node, channel)
// live streams: opens the media source, consumes PS frames, parses them
// into NALUs, and emits FLV tags to a channel of subscribers.
//
// Each Subscribe call currently gets its own pipeline (one source open per
// HTTP request). For multi-subscriber scenarios we can later add a shared
// pipeline + fan-out layer.
type Gateway struct {
	mu       sync.Mutex
	media    MediaSourceSupplier
	registry NodeRegistry
	sessions *activeSessions
	log      *slog.Logger
}

// NodeRegistry is the gateway's read-only view of the node catalogue.
// It is satisfied by *app.NodeService (which exposes Get / Profile).
type NodeRegistry interface {
	// Get returns the live node by ID. The bool is false if the node
	// is not registered (e.g. removed via scenario delete-node).
	Get(ctx context.Context, id model.NodeID) (model.Node, bool)
}

// NewGateway builds a Gateway. All fields are required except log (defaults
// to slog.Default()).
func NewGateway(media MediaSourceSupplier, registry NodeRegistry, log *slog.Logger) *Gateway {
	if log == nil {
		log = slog.Default()
	}
	return &Gateway{
		media:    media,
		registry: registry,
		sessions: newActiveSessions(),
		log:      log,
	}
}

// Subscribe starts an HTTP-FLV live stream for the given node+channel and
// returns a channel of FLV chunks (already prefixed with the FLV file
// header). The caller MUST drain the channel; cancelling ctx stops the
// underlying pipeline.
//
// The first chunk contains the FLV header; the next chunk is the AVC
// sequence header (SPS/PPS); subsequent chunks are FLV video tags.
//
// Errors:
//   - ErrStreamingUnsupported when the node is not a device, has no
//     media source for the channel, or the channelID is not in the device's
//     channel list.
//   - other errors when the underlying source cannot be opened.
func (g *Gateway) Subscribe(ctx context.Context, nodeID model.NodeID, channelID string) (<-chan []byte, error) {
	if g.media == nil {
		return nil, &ErrStreamingUnsupported{NodeID: nodeID, ChannelID: channelID, Reason: "no media service"}
	}
	if g.registry == nil {
		return nil, &ErrStreamingUnsupported{NodeID: nodeID, ChannelID: channelID, Reason: "no node registry"}
	}
	node, ok := g.registry.Get(ctx, nodeID)
	if !ok {
		return nil, &ErrStreamingUnsupported{NodeID: nodeID, ChannelID: channelID, Reason: "node not found"}
	}
	if node.Profile().ID().Kind() != model.NodeKindDevice {
		return nil, &ErrStreamingUnsupported{NodeID: nodeID, ChannelID: channelID, Reason: "node is not a device"}
	}
	if _, ok := node.Profile().MediaConfigForChannel(channelID); !ok {
		return nil, &ErrStreamingUnsupported{NodeID: nodeID, ChannelID: channelID, Reason: "no media source configured"}
	}

	out := make(chan []byte)

	// Open the PS pipeline.
	psCh, err := g.media.SubscribePS(ctx, nodeID, channelID)
	if err != nil {
		return nil, fmt.Errorf("streaming: open source: %w", err)
	}

	key := sessionKey{nodeID: nodeID, channelID: channelID}
	g.mu.Lock()
	g.sessions.Add(key, out, nil, func() {})
	g.mu.Unlock()

	// Start the pipeline.
	go g.runPipeline(ctx, key, psCh, out)

	g.log.Info("flv: new stream session started",
		"node_id", nodeID.String(), "channel_id", channelID)
	return out, nil
}

// runPipeline consumes PS frames, parses them into NALUs, and emits FLV
// tags to the subscriber channel. The pipeline is shared across multiple
// HTTP response channels: each subscribes by calling Subscribe.
func (g *Gateway) runPipeline(
	ctx context.Context,
	key sessionKey,
	psCh <-chan model.PSFrame,
	out chan<- []byte,
) {
	defer close(out)
	g.mu.Lock()
	g.sessions.Remove(key)
	g.mu.Unlock()

	// First chunk: FLV header.
	select {
	case out <- FLVHeader:
	case <-ctx.Done():
		return
	}

	var sentSeqHeader bool

	for {
		select {
		case <-ctx.Done():
			g.log.Debug("flv: pipeline cancelled",
				"node_id", key.nodeID.String(), "channel_id", key.channelID)
			return

		case frame, ok := <-psCh:
			if !ok {
				g.log.Debug("flv: source closed",
					"node_id", key.nodeID.String(), "channel_id", key.channelID)
				return
			}
			nalus, tsMS, parsed := ParsePS(frame.Payload)
			if !parsed {
				continue
			}
			// First video frame: extract SPS/PPS and emit sequence header.
			if !sentSeqHeader {
				sps, pps := ExtractSPSPPS(nalus)
				if sps != nil && pps != nil {
					seqHeader := BuildAVCSequenceHeader(sps, pps)
					select {
					case out <- seqHeader:
						sentSeqHeader = true
					case <-ctx.Done():
						return
					}
				} else {
					// No SPS/PPS yet; keep skipping frames until we see them.
					continue
				}
			}

			// Emit one video tag per NALU.
			for _, nalu := range nalus {
				if IsSPSPPSN(nalu) {
					continue // already sent in sequence header
				}
				isKey := IsKeyframeN(nalu)
				tag := BuildVideoTag(nalu, isKey, tsMS)
				select {
				case out <- tag:
				case <-ctx.Done():
					return
				}
			}
		}
	}
}

// Subscribers returns the active subscriber count for a node+channel.
func (g *Gateway) Subscribers(nodeID model.NodeID, channelID string) int {
	return g.sessions.ChannelCount(nodeID, channelID)
}

// TotalSubscribers returns the total number of active subscribers across
// all sessions.
func (g *Gateway) TotalSubscribers() int {
	return g.sessions.CountAll()
}

// Close releases all active sessions. Currently a no-op (sessions tear down
// when their request contexts are cancelled). Kept for symmetry with the
// port interface.
func (g *Gateway) Close() error {
	for _, key := range g.sessions.AllKeys() {
		g.sessions.Remove(key)
	}
	return nil
}
