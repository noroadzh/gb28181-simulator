package streaming

import (
	"context"
	"fmt"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// MediaServiceBridge adapts *app.MediaService + NodeRegistry to the
// MediaSourceSupplier interface required by Gateway.
//
// It is the composition root for the streaming pipeline: MediaService knows
// how to open a source and read ES frames; NodeRegistry provides the
// per-channel MediaConfig; this bridge puts them together.
type MediaServiceBridge struct {
	media    MediaServiceForStreaming
	registry NodeRegistry
}

// MediaServiceForStreaming is the subset of *app.MediaService needed by the
// streaming gateway. Defined here to avoid import cycles.
type MediaServiceForStreaming interface {
	SubscribePS(ctx context.Context, cfg model.MediaConfig) (<-chan model.PSFrame, func(), error)
}

// NewMediaServiceBridge builds a bridge between MediaService and NodeRegistry.
func NewMediaServiceBridge(media MediaServiceForStreaming, registry NodeRegistry) *MediaServiceBridge {
	return &MediaServiceBridge{media: media, registry: registry}
}

// SubscribePS implements MediaSourceSupplier: looks up the per-channel
// MediaConfig from the registry and opens the source.
func (b *MediaServiceBridge) SubscribePS(ctx context.Context, nodeID model.NodeID, channelID string) (<-chan model.PSFrame, error) {
	node, ok := b.registry.Get(ctx, nodeID)
	if !ok {
		return nil, fmt.Errorf("streaming: node %s not found", nodeID)
	}
	cfg, hasCfg := node.Profile().MediaConfigForChannel(channelID)
	if !hasCfg {
		return nil, fmt.Errorf("streaming: no media source for node %s channel %s", nodeID, channelID)
	}
	psCh, cleanup, err := b.media.SubscribePS(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// Wrap so cleanup is called when the channel is closed.
	ch := make(chan model.PSFrame)
	go func() {
		defer cleanup()
		defer close(ch)
		for frame := range psCh {
			select {
			case ch <- frame:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}
