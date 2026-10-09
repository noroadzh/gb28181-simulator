// Package channelsql — the SQLite-backed channel store. It persists dynamic
// channels and their per-channel media configuration for device nodes so a
// process restart restores the in-memory profile without re-reading the YAML.
//
// The schema is:
//   - channels      (node_id, channel_id) → name, status, parent_id
//   - channel_media (node_id, channel_id) → kind, path, loop, mtu, fps, clock
//   - node_media    (node_id)             → kind, path, loop, mtu, fps, clock
package channelsql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// Store is the durable channel store. The zero value is NOT usable;
// construct one with New over an initialised *sql.DB.
type Store struct {
	db *sql.DB
}

// New returns a store over db. The database must already carry the channels,
// channel_media and node_media tables (storage.Bootstrap runs the schema).
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// Compile-time interface check.
var _ port.ChannelStore = (*Store)(nil)

// SaveChannel upserts one dynamic channel row for the node.
func (s *Store) SaveChannel(ctx context.Context, nodeID model.NodeID, ch model.Channel) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO channels (node_id, channel_id, name, status, parent_id)
		 VALUES (?, ?, ?, ?, ?)`,
		nodeID.String(), ch.ID(), ch.Name(), ch.Status().String(), ch.ParentID(),
	)
	if err != nil {
		return fmt.Errorf("channelsql: save channel %s on node %s: %w", ch.ID(), nodeID, err)
	}
	return nil
}

// RemoveChannel deletes the dynamic channel row and its per-channel media
// configuration. An absent row is silently ignored.
func (s *Store) RemoveChannel(ctx context.Context, nodeID model.NodeID, channelID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("channelsql: remove channel %s on node %s: %w", channelID, nodeID, err)
	}
	defer tx.Rollback() // no-op when already committed

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM channel_media WHERE node_id=? AND channel_id=?`,
		nodeID.String(), channelID,
	); err != nil {
		return fmt.Errorf("channelsql: remove channel media %s on node %s: %w", channelID, nodeID, err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM channels WHERE node_id=? AND channel_id=?`,
		nodeID.String(), channelID,
	); err != nil {
		return fmt.Errorf("channelsql: remove channel %s on node %s: %w", channelID, nodeID, err)
	}
	return tx.Commit()
}

// ListChannels returns every persisted channel row for the node, ordered by
// channel id. An empty (nil) slice means none are persisted.
func (s *Store) ListChannels(ctx context.Context, nodeID model.NodeID) ([]model.Channel, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT channel_id, name, status, parent_id FROM channels
		 WHERE node_id=? ORDER BY channel_id`,
		nodeID.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("channelsql: list channels for node %s: %w", nodeID, err)
	}
	defer rows.Close()

	var out []model.Channel
	for rows.Next() {
		var id, name, statusStr, parentID string
		if err := rows.Scan(&id, &name, &statusStr, &parentID); err != nil {
			return nil, fmt.Errorf("channelsql: scan channel for node %s: %w", nodeID, err)
		}
		status, err := channelStatusParse(statusStr)
		if err != nil {
			// Treat an unrecognised status as online rather than failing
			// the whole list: the operator can fix the row later.
			status = model.ChannelStatusOnline
		}
		ch, err := model.NewChannel(id, name, parentID, status)
		if err != nil {
			// Skip rows the model refuses rather than failing the whole
			// list: the operator can fix the row later.
			continue
		}
		out = append(out, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("channelsql: list channels for node %s: %w", nodeID, err)
	}
	return out, nil
}

// SaveChannelMedia upserts the per-channel media configuration.
func (s *Store) SaveChannelMedia(ctx context.Context, nodeID model.NodeID, channelID string, cfg model.MediaConfig) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO channel_media
		 (node_id, channel_id, kind, path, loop, mtu, fps, clock)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		nodeID.String(), channelID, string(cfg.Kind), cfg.Path, boolToInt(cfg.Loop),
		cfg.MTU, cfg.FPS, cfg.Clock,
	)
	if err != nil {
		return fmt.Errorf("channelsql: save channel media for %s on node %s: %w", channelID, nodeID, err)
	}
	return nil
}

// ClearChannelMedia deletes the per-channel media configuration.
func (s *Store) ClearChannelMedia(ctx context.Context, nodeID model.NodeID, channelID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM channel_media WHERE node_id=? AND channel_id=?`,
		nodeID.String(), channelID,
	)
	if err != nil {
		return fmt.Errorf("channelsql: clear channel media for %s on node %s: %w", channelID, nodeID, err)
	}
	return nil
}

// ListChannelMedia returns every persisted per-channel media config for the
// node. An empty (nil) slice means none are persisted.
func (s *Store) ListChannelMedia(ctx context.Context, nodeID model.NodeID) ([]port.PersistedChannelMedia, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT channel_id, kind, path, loop, mtu, fps, clock
		 FROM channel_media WHERE node_id=? ORDER BY channel_id`,
		nodeID.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("channelsql: list channel media for node %s: %w", nodeID, err)
	}
	defer rows.Close()

	var out []port.PersistedChannelMedia
	for rows.Next() {
		var p port.PersistedChannelMedia
		var loopInt int
		if err := rows.Scan(&p.ChannelID, &p.Kind, &p.Path, &loopInt, &p.MTU, &p.FPS, &p.Clock); err != nil {
			return nil, fmt.Errorf("channelsql: scan channel media for node %s: %w", nodeID, err)
		}
		p.Loop = loopInt != 0
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("channelsql: list channel media for node %s: %w", nodeID, err)
	}
	return out, nil
}

// SaveNodeMedia upserts the node-level media configuration.
func (s *Store) SaveNodeMedia(ctx context.Context, nodeID model.NodeID, cfg model.MediaConfig) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO node_media
		 (node_id, kind, path, loop, mtu, fps, clock)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		nodeID.String(), string(cfg.Kind), cfg.Path, boolToInt(cfg.Loop),
		cfg.MTU, cfg.FPS, cfg.Clock,
	)
	if err != nil {
		return fmt.Errorf("channelsql: save node media for node %s: %w", nodeID, err)
	}
	return nil
}

// ClearNodeMedia deletes the node-level media configuration.
func (s *Store) ClearNodeMedia(ctx context.Context, nodeID model.NodeID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM node_media WHERE node_id=?`,
		nodeID.String(),
	)
	if err != nil {
		return fmt.Errorf("channelsql: clear node media for node %s: %w", nodeID, err)
	}
	return nil
}

// LoadNodeMedia returns the persisted node-level media config and whether
// one was stored. (zero, false) when none.
func (s *Store) LoadNodeMedia(ctx context.Context, nodeID model.NodeID) (model.MediaConfig, bool, error) {
	var kind, path string
	var loopInt, mtu, fps, clock int
	err := s.db.QueryRowContext(ctx,
		`SELECT kind, path, loop, mtu, fps, clock FROM node_media WHERE node_id=?`,
		nodeID.String(),
	).Scan(&kind, &path, &loopInt, &mtu, &fps, &clock)
	if err == sql.ErrNoRows {
		return model.MediaConfig{}, false, nil
	}
	if err != nil {
		return model.MediaConfig{}, false, fmt.Errorf("channelsql: load node media for node %s: %w", nodeID, err)
	}
	return model.MediaConfig{
		Kind:  model.MediaSourceKind(kind),
		Path:  path,
		Loop:  loopInt != 0,
		MTU:   mtu,
		FPS:   fps,
		Clock: uint64(clock),
	}, true, nil
}

// channelStatusParse mirrors the same-named function in model/channel.go so
// the adapter can restore ChannelStatus without depending on the model
// package's private map. An empty string is treated as ChannelStatusOnline.
func channelStatusParse(s string) (model.ChannelStatus, error) {
	switch s {
	case "ON", "ONLINE":
		return model.ChannelStatusOnline, nil
	case "OFF", "OFFLINE":
		return model.ChannelStatusOffline, nil
	case "":
		return model.ChannelStatusOnline, nil
	default:
		return model.ChannelStatusUnknown, fmt.Errorf("channelsql: unknown channel status %q", s)
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
