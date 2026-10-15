// Package app implements the SIP acceptor handling device registration, message
// routing, and media pipeline management for GB/T 28181 simulation.
//
// This file contains catalog query/response handling (Cluster 5).
// Extracted verbatim from acceptor.go (was lines 2112-2356, 245 lines).
package app

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// ErrNoDownstreamAccount is the reason a REGISTER is refused when the
// platform has no account for the device it came from. It is logged, never
// sent on the wire: the peer learns only that it was refused (403).
var ErrNoDownstreamAccount = fmt.Errorf("app: no account for the downstream")

// WithCatalogPollInterval sets the interval at which a platform-large node
// polls each downstream device's catalog. The poller refreshes the device's
// own profile with the device's catalog and imports the per-channel MediaSource
// Extension into the node's profile. Zero disables the poller. The setter is
// idempotent; it is safe to call before or between Serve calls.
func (a *Acceptor) WithCatalogPollInterval(d time.Duration) *Acceptor {
	a.catalogPollEvery = d
	return a
}

// answerCatalog answers a catalog query with the online device table of the
// node it arrived at — never with another node's, so two platforms sharing
// a process cannot see each other's downstreams.
//
// Each downstream device is listed as itself, and when a node registry is
// attached the device's own profile channels are also expanded under it so
// an upstream sees the full sub-tree behind a device node. The platform's
// own profile channels are appended last. ChannelStatus is mapped to the GB/T
// 28181 CatalogStatus so online/offline enforcement is visible upstream.
func (a *Acceptor) answerCatalog(
	ctx context.Context,
	p *platform,
	req model.Message,
	notify model.Notify,
) (model.Message, bool) {
	if !notify.HasSN() {
		a.log.Warn("ignoring a catalog query without a sequence number",
			"node_id", p.id.String(), "device_id", notify.DeviceID())
		return model.Message{}, false
	}
	items := make([]model.CatalogItem, 0)
	for _, d := range a.devices.List(ctx, p.id) {
		item, err := model.NewCatalogItemFromDevice(d)
		if err != nil {
			continue
		}
		items = append(items, item)
		// A downstream device that is itself a node exposes the channels
		// its profile carries, so an upstream sees the whole subtree
		// behind it and not just the device that registered.
		if a.registry != nil {
			items = append(items, a.downstreamChannels(ctx, p.id, d)...)
		}
	}
	if a.registry != nil {
		if node, ok := a.registry.Get(ctx, p.id); ok {
			items = append(items, channelCatalogItems(node.Profile().Channels(), "")...)
		}
	}
	// spec platform-large-node-catalog-aggregate: DeviceList 先按 ParentID 升序、再按 DeviceID 升序。
	slices.SortFunc(items, func(a, b model.CatalogItem) int {
		ap, bp := a.ParentID(), b.ParentID()
		if ap != bp {
			if ap < bp {
				return -1
			}
			return 1
		}
		ad, bd := a.DeviceID(), b.DeviceID()
		if ad < bd {
			return -1
		}
		if ad > bd {
			return 1
		}
		return 0
	})
	catalog, err := model.NewCatalog(p.id.String(), notify.SN(), items)
	if err != nil {
		a.log.Warn("cannot build a catalog answer",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	// task 3: embed per-channel media-source Extension in the catalog.
	// mediaByDeviceID is nil when there is no registry, keeping the legacy
	// bytes identical (no Extension element in any Item).
	mediaByDeviceID := buildCatalogMediaByDeviceID(ctx, a, p.id)
	body, err := a.manscdp.MarshalCatalogWithMedia(catalog, mediaByDeviceID)
	if err != nil {
		a.log.Warn("cannot render a catalog answer",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	a.log.Debug("answering a catalog query", "node_id", p.id.String(),
		"sn", notify.SN(), "sum", catalog.SumNum())
	resp, err := buildResponse(200, "OK", req, []model.Header{
		model.NewHeader("Content-Type", contentTypeMANSCDP),
	}, body)
	if err != nil {
		a.log.Warn("cannot answer a catalog query",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	return resp, true
}

// channelCatalogItems renders profile channels as catalog entries. The
// channel status is mapped to the GB/T 28181 CatalogStatus so online/offline
// enforcement is visible upstream; a channel without a parent falls back to
// fallbackParent so its lineage stays visible. Unrenderable channels are
// skipped rather than failing the whole answer.
func channelCatalogItems(channels []model.Channel, fallbackParent string) []model.CatalogItem {
	if len(channels) == 0 {
		return nil
	}
	items := make([]model.CatalogItem, 0, len(channels))
	for _, ch := range channels {
		parentID := ch.ParentID()
		if parentID == "" {
			parentID = fallbackParent
		}
		status := model.CatalogStatusON
		if ch.Status() == model.ChannelStatusOffline {
			status = model.CatalogStatusOFF
		}
		catalogItem, err := model.NewCatalogItem(model.CatalogItemParams{
			DeviceID: ch.ID(),
			Name:     ch.Name(),
			Status:   status,
			ParentID: parentID,
		})
		if err != nil {
			continue
		}
		items = append(items, catalogItem)
	}
	return items
}

// buildCatalogMediaByDeviceID returns the per-channel MediaConfig map the
// device embeds in its catalog answer. The map is keyed by channel id, so
// downstream devices and own channels are merged into one. A nil map means
// "no media source configured anywhere"; the codec then omits Extension
// blocks from every Item, which is the legacy shape.
//
// Per-channel configuration wins over the node-level one (see
// model.MediaConfigForChannel). Zero-valued configurations are skipped: an
// empty Kind cannot open a source, and the codec treats a zero MediaConfig
// as "no media" anyway. With no registry the map is nil, preserving the
// golden bytes of the older device-only fixtures.
func buildCatalogMediaByDeviceID(ctx context.Context, a *Acceptor, id model.NodeID) map[string]model.MediaConfig {
	if a.registry == nil {
		return nil
	}
	node, ok := a.registry.Get(ctx, id)
	if !ok {
		return nil
	}
	profile := node.Profile()
	var out map[string]model.MediaConfig
	for _, ch := range profile.Channels() {
		cfg, has := profile.MediaConfigForChannel(ch.ID())
		if !has || cfg.Kind == "" {
			continue
		}
		if out == nil {
			out = make(map[string]model.MediaConfig, 4)
		}
		out[ch.ID()] = cfg
	}
	return out
}

// handleCatalogResponse parses a 2xx Response body as a Catalog and imports
// the per-channel MediaSource Extension the device advertised, so a platform
// learns a cascade child's real media source without a separate query.
// The error is non-nil when the body is not a catalog Response at all —
// this is the normal case for every other SIP Response the transport delivers.
func (a *Acceptor) handleCatalogResponse(ctx context.Context, p *platform, msg model.Message) {
	decoded, media, err := a.manscdp.DecodeCatalogResponse(msg.Body())
	if err != nil {
		to, _ := msg.Header("To")
		a.log.Debug("ignoring a response body that is not a catalog",
			"node_id", p.id.String(), "peer", to.Value())
		return
	}
	if len(media) == 0 {
		return
	}
	a.log.Debug("catalog response received; importing media sources",
		"node_id", p.id.String(), "device_id", decoded.DeviceID(), "channels", len(media))
	// The device's id is the node id we polled. Every channel in the response
	// belongs to it; keying the write by the device id from the body lets a
	// multi-device aggregation in the same response land on the right profile.
	a.applyMediaSourcesFromCatalog(ctx, p.id, media)
}

// applyMediaSourcesFromCatalog writes the per-channel media configuration
// extracted from a decoded catalog response into the receiving node's
// profile. It is the platform side of the device→platform propagation. An
// entry whose DeviceID is empty, fails ParseNodeID, or carries an invalid
// MediaConfig is logged and skipped — a single bad row must not corrupt
// the profile or take the rest of the response down.
//
// The function is a no-op when media is empty or when a NodeRegistry is not
// attached; both are legitimate production states (real devices do not
// advertise media sources, and a test harness that omits the registry
// should still pass).
func (a *Acceptor) applyMediaSourcesFromCatalog(ctx context.Context, nodeID model.NodeID, media []port.ChannelMedia) {
	if a.registry == nil || len(media) == 0 {
		return
	}
	for _, m := range media {
		deviceID := strings.TrimSpace(m.DeviceID)
		if deviceID == "" {
			a.log.Warn("catalog media entry with empty device id; skipping",
				"node_id", nodeID.String())
			continue
		}
		if _, err := model.ParseNodeID(deviceID); err != nil {
			a.log.Warn("catalog media entry with invalid device id; skipping",
				"node_id", nodeID.String(), "device_id", deviceID, "error", err.Error())
			continue
		}
		if err := m.Config.Validate(); err != nil {
			a.log.Warn("catalog media entry invalid; skipping",
				"node_id", nodeID.String(), "device_id", deviceID, "error", err.Error())
			continue
		}
		if _, err := a.registry.MutateProfile(ctx, nodeID, func(np model.NodeProfile) (model.NodeProfile, error) {
			np.SetChannelMediaConfig(deviceID, m.Config.Normalize())
			return np, nil
		}); err != nil {
			a.log.Warn("cannot store catalog media on profile",
				"node_id", nodeID.String(), "device_id", deviceID, "error", err.Error())
		}
	}
}

// downstreamChannels expands one downstream device into the channels its node
// profile carries. A device node shares its GB id with its registry entry,
// so the lookup uses ParseNodeID of the device id; a device the registry
// does not know, or one whose id is not a node id at all, is answered as
// itself alone.
func (a *Acceptor) downstreamChannels(
	ctx context.Context,
	node model.NodeID,
	d model.DownstreamDevice,
) []model.CatalogItem {
	if a.registry == nil {
		return nil
	}
	id, err := model.ParseNodeID(d.DeviceID())
	if err != nil {
		a.log.Debug("answering a device as itself: its id is not a node id",
			"node_id", node.String(), "device_id", d.DeviceID())
		return nil
	}
	n, ok := a.registry.Get(ctx, id)
	if !ok {
		return nil
	}
	return channelCatalogItems(n.Profile().Channels(), d.DeviceID())
}

// Devices returns the online device table of id, ordered by device id. An
// unknown or stopped node has none.
func (a *Acceptor) Devices(ctx context.Context, id model.NodeID) []model.DownstreamDevice {
	return a.devices.List(ctx, id)
}

// Device returns one row of the online device table of id.
func (a *Acceptor) Device(ctx context.Context, id model.NodeID, deviceID string) (model.DownstreamDevice, bool) {
	return a.devices.Lookup(ctx, id, deviceID)
}
