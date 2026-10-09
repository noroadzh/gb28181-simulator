// Package port — ChannelStore.
package port

import (
	"context"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// PersistedChannelMedia is the durable projection of a per-channel media
// source. It carries the same fields the channels / channel_media tables
// store, so the app layer can restore runtime state after a restart without
// reaching into a concrete database.
type PersistedChannelMedia struct {
	ChannelID string
	Kind      string
	Path      string
	Loop      bool
	MTU       int
	FPS       int
	Clock     uint64
}

// ChannelStore persists the dynamic channels of a device node and their
// per-channel media configuration. The in-memory profile (NodeProfile via
// the registry) stays the live source of truth for signalling; this store is
// what makes a restart reproduce it.
//
// Implementations MUST tolerate a nil-ish node (no rows) and return an empty
// result, not an error, so a first run behaves like an empty one.
type ChannelStore interface {
	// SaveChannel upserts one dynamic channel row for the node.
	SaveChannel(ctx context.Context, nodeID model.NodeID, ch model.Channel) error
	// RemoveChannel deletes the dynamic channel row and its per-channel
	// media configuration. Removing an absent row is a no-op, not an
	// error: the profile in the registry is the authority on existence.
	RemoveChannel(ctx context.Context, nodeID model.NodeID, channelID string) error
	// ListChannels returns every persisted channel row for the node,
	// ordered by channel id. Empty (nil) when none are persisted.
	ListChannels(ctx context.Context, nodeID model.NodeID) ([]model.Channel, error)
	// SaveChannelMedia upserts the per-channel media configuration.
	SaveChannelMedia(ctx context.Context, nodeID model.NodeID, channelID string, cfg model.MediaConfig) error
	// ClearChannelMedia deletes the per-channel media configuration.
	ClearChannelMedia(ctx context.Context, nodeID model.NodeID, channelID string) error
	// ListChannelMedia returns every persisted per-channel media config
	// for the node. Empty (nil) when none are persisted.
	ListChannelMedia(ctx context.Context, nodeID model.NodeID) ([]PersistedChannelMedia, error)
	// SaveNodeMedia upserts the node-level media configuration.
	SaveNodeMedia(ctx context.Context, nodeID model.NodeID, cfg model.MediaConfig) error
	// ClearNodeMedia deletes the node-level media configuration.
	ClearNodeMedia(ctx context.Context, nodeID model.NodeID) error
	// LoadNodeMedia returns the persisted node-level media config and
	// whether one was stored. (zero, false) when none.
	LoadNodeMedia(ctx context.Context, nodeID model.NodeID) (model.MediaConfig, bool, error)
}
