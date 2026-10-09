// Package model — Node.
package model

import (
	"fmt"
	"strings"
)

// NodeIDLen is the fixed length of a GB/T 28181 identifier: 8-digit centre
// code + 2-digit industry code + 3-digit type code + 1-digit network id +
// 6-digit serial.
const NodeIDLen = 20

// NodeKind enumerates the three identities the simulator can emulate. The
// zero value is Unknown and is never returned by a successful
// ParseNodeID: an unrecognised type-code segment is an error, not a default
// (design D1).
type NodeKind int

const (
	// NodeKindUnknown is the zero value; never produced by ParseNodeID.
	NodeKindUnknown NodeKind = iota
	// NodeKindDevice is a front-end device (camera, DVR, NVR, encoder).
	NodeKindDevice
	// NodeKindPlatformLarge is an upper-level / central platform.
	NodeKindPlatformLarge
	// NodeKindPlatformSmall is a lower-level / access platform.
	NodeKindPlatformSmall
)

// nodeKindByTypeCode maps the 3-digit type-code segment (bytes 11-13 of the
// 20-digit id, 1-based) to a NodeKind. Values follow GB/T 28181-2016 Annex
// B. Segments outside this set are rejected rather than guessed, so Change
// 5/6/7 can add identity-specific rules without silently widening what
// counts as a valid id.
var nodeKindByTypeCode = map[string]NodeKind{
	"111": NodeKindDevice,        // digital video recorder
	"112": NodeKindDevice,        // video server
	"113": NodeKindDevice,        // encoder
	"118": NodeKindDevice,        // network video recorder
	"131": NodeKindDevice,        // IP camera
	"132": NodeKindDevice,        // IP camera
	"200": NodeKindPlatformLarge, // central / upper-level platform
	"216": NodeKindPlatformSmall, // access / lower-level platform
}

// nodeKindNames is the inverse spelling used by configuration and JSON.
var nodeKindNames = map[NodeKind]string{
	NodeKindDevice:        "device",
	NodeKindPlatformLarge: "platform-large",
	NodeKindPlatformSmall: "platform-small",
}

// String returns the configuration/JSON spelling of the kind, or "" for
// NodeKindUnknown.
func (k NodeKind) String() string {
	return nodeKindNames[k]
}

// ParseNodeKind maps the configuration spelling to a NodeKind. The empty
// string is rejected so an omitted `kind:` field cannot pass silently.
func ParseNodeKind(s string) (NodeKind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "device":
		return NodeKindDevice, nil
	case "platform-large":
		return NodeKindPlatformLarge, nil
	case "platform-small":
		return NodeKindPlatformSmall, nil
	case "":
		return NodeKindUnknown, fmt.Errorf("model: empty node kind")
	default:
		return NodeKindUnknown, fmt.Errorf("model: unknown node kind %q", s)
	}
}

// NodeID is an immutable validated 20-digit GB/T 28181 identifier. The raw
// encoding is kept verbatim so String() round-trips exactly what the caller
// supplied.
type NodeID struct {
	raw  string
	kind NodeKind
}

// ParseNodeID validates raw and builds a NodeID. It rejects anything that is
// not exactly 20 ASCII digits, and any type-code segment (bytes 11-13) that
// does not map to a known NodeKind. Every error names the reason and the
// observed length so configuration loaders can report the offending entry.
func ParseNodeID(raw string) (NodeID, error) {
	if len(raw) != NodeIDLen {
		return NodeID{}, fmt.Errorf("model: illegal node id %q: length %d, want %d",
			raw, len(raw), NodeIDLen)
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return NodeID{}, fmt.Errorf(
				"model: illegal node id %q: length %d, byte %d is %q, want digit",
				raw, len(raw), i+1, raw[i:i+1])
		}
	}
	code := raw[10:13]
	kind, ok := nodeKindByTypeCode[code]
	if !ok {
		return NodeID{}, fmt.Errorf(
			"model: illegal node id %q: length %d, type code %q (bytes 11-13) maps to no known kind",
			raw, len(raw), code)
	}
	return NodeID{raw: raw, kind: kind}, nil
}

// String returns the verbatim 20-digit encoding.
func (id NodeID) String() string { return id.raw }

// Kind returns the identity implied by the type-code segment.
func (id NodeID) Kind() NodeKind { return id.kind }

// TypeCode returns the 3-digit type-code segment (bytes 11-13).
func (id NodeID) TypeCode() string {
	if len(id.raw) != NodeIDLen {
		return ""
	}
	return id.raw[10:13]
}

// NodeProfile is an immutable node identity together with everything needed
// to bind its signalling listener: the signalling address, the home domain
// and the vendor string. The kind is derived from the id so it can never
// disagree with the encoding.
type NodeProfile struct {
	id     NodeID
	addr   string
	domain string
	vendor string

	// registration and result are optional: nil means "this node does not
	// register" / "it has not registered yet". They are pointers so the
	// zero NodeProfile keeps its original meaning.
	registration *Registration
	result       *RegistrationResult

	// serving is how a platform node accepts its own downstreams; nil
	// means "not configured", which a platform-large node fills with
	// defaults at start-up.
	serving *PlatformServing

	// cascadeParent is the deviceID of the upstream node this node
	// forwards traffic to when it is not the final destination. Empty
	// means "no upstream" (the top of a topology).
	cascadeParent string
	// cascadeChildren is the ordered list of deviceIDs this node routes
	// traffic to when it receives a message whose destination is not
	// itself. Empty means "direct only" — no forwarding is configured.
	cascadeChildren []string

	// channels is the device's mutable channel list. Empty for non-device
	// nodes or when the device exposes only a single logical channel.
	channels []Channel
	// alarms is an append-only log of alarm snapshots emitted by this node.
	alarms []AlarmSnapshot
	// position is the last-known geographic position, if any.
	position *Position
	// presets is the device's preset position list. Empty when the device
	// does not expose presets or when none have been configured.
	presets []PresetItem
	// homePosition is the GB/T 28181-2022 guard position stored when a
	// downstream sets it; empty when never configured.
	homePosition *HomePosition
	// cruiseTracks is the list of cruise tracks exposed to the downstream
	// when it queries CruiseTrackList; empty when never configured.
	cruiseTracks []CruiseTrack
	// snapshots is an append-only log of SnapShot records emitted by this
	// node.
	snapshots []SnapShotRecord

	// mediaConfig is the optional per-node media source. Nil means "no
	// source configured" — the device responds to INVITE with no video.
	// Mutable through SetMediaConfig so the HTTP API can flip it at
	// runtime without touching the immutable rest of the profile.
	mediaConfig *MediaConfig
	// mediaConfigByChannel is the per-channel media source map. When
	// non-empty it takes precedence over mediaConfig for the channel
	// it covers. An entry with zero Kind means "this channel has no
	// source". This field is the multi-channel extension (Change 14).
	mediaConfigByChannel map[string]*MediaConfig
}

// NewNodeProfile validates id with ParseNodeID and requires a non-empty
// signalling address and home domain. Vendor is optional (empty allowed).
func NewNodeProfile(id, addr, domain, vendor string) (NodeProfile, error) {
	nodeID, err := ParseNodeID(id)
	if err != nil {
		return NodeProfile{}, err
	}
	if strings.TrimSpace(addr) == "" {
		return NodeProfile{}, fmt.Errorf("model: empty signalling address for node %s", id)
	}
	if strings.TrimSpace(domain) == "" {
		return NodeProfile{}, fmt.Errorf("model: empty home domain for node %s", id)
	}
	return NodeProfile{id: nodeID, addr: addr, domain: domain, vendor: vendor}, nil
}

// NewNodeProfileWithMedia validates id with ParseNodeID and requires a non-empty
// signalling address and home domain. Vendor is optional (empty allowed).
// It seeds the media source from mediaCfg (nil means no source configured).
func NewNodeProfileWithMedia(id, addr, domain, vendor string, mediaCfg *MediaConfig) (NodeProfile, error) {
	p, err := NewNodeProfile(id, addr, domain, vendor)
	if err != nil {
		return p, err
	}
	if mediaCfg != nil {
		p.SetMediaConfig(*mediaCfg)
	}
	return p, nil
}

// ParseNodeMediaConfig converts raw YAML/JSON fields into a validated
// MediaConfig. Empty kind is treated as "not configured" and returns the
// zero value with no error. For file/rtsp/hls, path must be non-empty.
// The loop flag is preserved on the returned MediaConfig.Path behaviour by
// callers that need it (e.g. FileSource), so it is returned via *bool.
func ParseNodeMediaConfig(kind, path string, loop bool, mtu int, ssrc uint32, clock uint64, fps int) (MediaConfig, error) {
	if kind == "" {
		return MediaConfig{}, nil
	}
	switch MediaSourceKind(kind) {
	case SourceKindFile, SourceKindRTSP, SourceKindHLS, SourceKindSynthetic:
	default:
		return MediaConfig{}, fmt.Errorf("model: unknown media kind %q", kind)
	}
	if MediaSourceKind(kind) != SourceKindSynthetic && strings.TrimSpace(path) == "" {
		return MediaConfig{}, fmt.Errorf("model: media.path is empty for kind %q", kind)
	}
	cfg := MediaConfig{
		Kind:  MediaSourceKind(kind),
		Path:  path,
		SSRC:  ssrc,
		MTU:   mtu,
		FPS:   fps,
		Clock: clock,
	}
	cfg = cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return MediaConfig{}, err
	}
	return cfg, nil
}

// ID returns the node identity.
func (p NodeProfile) ID() NodeID { return p.id }

// Kind returns the identity kind (delegates to the id).
func (p NodeProfile) Kind() NodeKind { return p.id.Kind() }

// Addr returns the signalling address the node listens on.
func (p NodeProfile) Addr() string { return p.addr }

// Domain returns the home domain the node registers against.
func (p NodeProfile) Domain() string { return p.domain }

// Vendor returns the vendor string; may be empty.
func (p NodeProfile) Vendor() string { return p.vendor }

// WithAddr returns a copy with a new signalling address.
func (p NodeProfile) WithAddr(addr string) (NodeProfile, error) {
	if strings.TrimSpace(addr) == "" {
		return p, fmt.Errorf("model: empty signalling address for node %s", p.id)
	}
	cp := p
	cp.addr = addr
	return cp, nil
}

// WithDomain returns a copy with a new home domain.
func (p NodeProfile) WithDomain(domain string) (NodeProfile, error) {
	if strings.TrimSpace(domain) == "" {
		return p, fmt.Errorf("model: empty home domain for node %s", p.id)
	}
	cp := p
	cp.domain = domain
	return cp, nil
}

// WithVendor returns a copy with a new vendor string.
func (p NodeProfile) WithVendor(vendor string) NodeProfile {
	cp := p
	cp.vendor = vendor
	return cp
}

// WithRegistration returns a copy that registers with the platform
// described by reg. An unconstructed (zero) Registration is refused, so a
// mis-built configuration surfaces here instead of silently disabling
// registration.
func (p NodeProfile) WithRegistration(reg Registration) (NodeProfile, error) {
	if !reg.HasRegistration() {
		return p, fmt.Errorf("model: empty registration for node %s", p.id)
	}
	cp := p
	cp.registration = &reg
	return cp, nil
}

// Registration returns how the node registers, and whether it registers at
// all. The second result is false for a node never configured to.
func (p NodeProfile) Registration() (Registration, bool) {
	if p.registration == nil {
		return Registration{}, false
	}
	return *p.registration, true
}

// WithRegistrationResult returns a copy carrying the outcome of a
// registration. An unconstructed result is refused.
func (p NodeProfile) WithRegistrationResult(res RegistrationResult) (NodeProfile, error) {
	if !res.HasResult() {
		return p, fmt.Errorf("model: empty registration result for node %s", p.id)
	}
	cp := p
	cp.result = &res
	return cp, nil
}

// RegistrationResult returns the last registration outcome, and whether
// there is one.
func (p NodeProfile) RegistrationResult() (RegistrationResult, bool) {
	if p.result == nil {
		return RegistrationResult{}, false
	}
	return *p.result, true
}

// WithPlatformServing returns a copy that serves downstreams as described.
// An unconstructed serving description is refused.
func (p NodeProfile) WithPlatformServing(serving PlatformServing) (NodeProfile, error) {
	if !serving.HasServing() {
		return p, fmt.Errorf("model: empty platform serving for node %s", p.id)
	}
	cp := p
	cp.serving = &serving
	return cp, nil
}

// PlatformServing returns how the node serves its downstreams, and whether
// it was configured to. The second result is false for a node that never
// declared a `platform:` section; a platform-large node then falls back to
// DefaultPlatformServing(domain).
func (p NodeProfile) PlatformServing() (PlatformServing, bool) {
	if p.serving == nil {
		return PlatformServing{}, false
	}
	return *p.serving, true
}

// WithCascadeParent returns a copy that forwards upstream traffic to
// parentID. The value is validated with ParseNodeID so a typo surfaces at
// configuration time, not on the wire.
func (p NodeProfile) WithCascadeParent(parentID string) (NodeProfile, error) {
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return p, fmt.Errorf("model: empty cascade parent for node %s", p.id)
	}
	if _, err := ParseNodeID(parentID); err != nil {
		return p, err
	}
	cp := p
	cp.cascadeParent = parentID
	return cp, nil
}

// CascadeParent returns the upstream deviceID, or "" when this node has no
// upstream configured.
func (p NodeProfile) CascadeParent() string { return p.cascadeParent }

// WithCascadeChildren returns a copy whose downstream routing order is
// children. Duplicates are rejected at configuration time: a duplicated
// child would make PreferredPath resolution ambiguous.
func (p NodeProfile) WithCascadeChildren(children []string) (NodeProfile, error) {
	seen := make(map[string]struct{}, len(children))
	cleaned := make([]string, 0, len(children))
	for _, c := range children {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, err := ParseNodeID(c); err != nil {
			return p, err
		}
		if _, dup := seen[c]; dup {
			return p, fmt.Errorf("model: duplicate cascade child %s for node %s", c, p.id)
		}
		seen[c] = struct{}{}
		cleaned = append(cleaned, c)
	}
	if len(cleaned) == 0 {
		return p, nil // clearing the list is a valid no-op
	}
	cp := p
	cp.cascadeChildren = cleaned
	return cp, nil
}

// CascadeChildren returns the downstream routing order; nil when none.
func (p NodeProfile) CascadeChildren() []string {
	if len(p.cascadeChildren) == 0 {
		return nil
	}
	return append([]string(nil), p.cascadeChildren...)
}

// HasCascadeParent reports whether the node forwards traffic upstream.
func (p NodeProfile) HasCascadeParent() bool { return p.cascadeParent != "" }

// HasCascadeChildren reports whether the node routes traffic downstream.
func (p NodeProfile) HasCascadeChildren() bool { return len(p.cascadeChildren) > 0 }

// HasCascadeRouting reports whether the node participates in any cascade
// topology at all.
func (p NodeProfile) HasCascadeRouting() bool {
	return p.HasCascadeParent() || p.HasCascadeChildren()
}

// WithChannels returns a copy whose dynamic channel list is channels. All
// entries must be valid and unique by id; an empty list clears it.
func (p NodeProfile) WithChannels(channels []Channel) (NodeProfile, error) {
	seen := make(map[string]struct{}, len(channels))
	for _, ch := range channels {
		if ch.ID() == "" {
			return p, fmt.Errorf("model: channel without id for node %s", p.id)
		}
		if _, dup := seen[ch.ID()]; dup {
			return p, fmt.Errorf("model: duplicate channel %s for node %s", ch.ID(), p.id)
		}
		seen[ch.ID()] = struct{}{}
	}
	cp := p
	if len(channels) == 0 {
		cp.channels = nil
	} else {
		cp.channels = append([]Channel(nil), channels...)
	}
	return cp, nil
}

// Channels returns a copy of the node's channel list; nil when none.
func (p NodeProfile) Channels() []Channel {
	if len(p.channels) == 0 {
		return nil
	}
	return append([]Channel(nil), p.channels...)
}

// ChannelByID returns the channel with the given id and whether it exists.
func (p NodeProfile) ChannelByID(id string) (Channel, bool) {
	for _, ch := range p.channels {
		if ch.ID() == id {
			return ch, true
		}
	}
	return Channel{}, false
}

// WithChannelStatus returns a copy whose channel id has status. An unknown
// channel id is an error, not a silent no-op.
func (p NodeProfile) WithChannelStatus(id string, status ChannelStatus) (NodeProfile, error) {
	idx := -1
	for i, ch := range p.channels {
		if ch.ID() == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return p, fmt.Errorf("model: unknown channel %s for node %s", id, p.id)
	}
	cp := p
	cp.channels = append([]Channel(nil), p.channels...)
	cp.channels[idx] = cp.channels[idx].WithStatus(status)
	return cp, nil
}

// WithChannelAdded returns a copy with ch appended to the channel list.
// A duplicate id or empty id is an error; the original profile is unchanged.
func (p NodeProfile) WithChannelAdded(ch Channel) (NodeProfile, error) {
	if ch.ID() == "" {
		return p, fmt.Errorf("model: channel without id for node %s", p.id)
	}
	for _, existing := range p.channels {
		if existing.ID() == ch.ID() {
			return p, fmt.Errorf("model: duplicate channel %s for node %s", ch.ID(), p.id)
		}
	}
	cp := p
	cp.channels = append(append([]Channel(nil), p.channels...), ch)
	return cp, nil
}

// WithChannelRemoved returns a copy with the channel id removed. An unknown
// id is an error, not a silent no-op.
func (p NodeProfile) WithChannelRemoved(id string) (NodeProfile, error) {
	idx := -1
	for i, ch := range p.channels {
		if ch.ID() == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return p, fmt.Errorf("model: unknown channel %s for node %s", id, p.id)
	}
	cp := p
	cp.channels = append([]Channel(nil), p.channels...)
	cp.channels = append(cp.channels[:idx], cp.channels[idx+1:]...)
	return cp, nil
}

// AppendAlarm returns a copy with snap added to the alarm log. Snap must be
// constructed via NewAlarmSnapshot; duplicates by id are refused.
func (p NodeProfile) AppendAlarm(snap AlarmSnapshot) (NodeProfile, error) {
	if snap.ID() == "" {
		return p, fmt.Errorf("model: alarm snapshot without id for node %s", p.id)
	}
	for _, a := range p.alarms {
		if a.ID() == snap.ID() {
			return p, fmt.Errorf("model: duplicate alarm %s for node %s", snap.ID(), p.id)
		}
	}
	cp := p
	cp.alarms = append(cp.alarms, snap)
	return cp, nil
}

// Alarms returns a copy of the node's alarm log; nil when empty.
func (p NodeProfile) Alarms() []AlarmSnapshot {
	if len(p.alarms) == 0 {
		return nil
	}
	return append([]AlarmSnapshot(nil), p.alarms...)
}

// WithPosition returns a copy carrying pos as the last-known position.
func (p NodeProfile) WithPosition(pos Position) NodeProfile {
	cp := p
	pp := pos
	cp.position = &pp
	return cp
}

// Position returns the last-known position and whether one was set.
func (p NodeProfile) Position() (Position, bool) {
	if p.position == nil {
		return Position{}, false
	}
	return *p.position, true
}

// WithPresets returns a copy whose preset list is items. Empty or nil clears
// it; duplicates by PresetIndex are refused.
func (p NodeProfile) WithPresets(items []PresetItem) (NodeProfile, error) {
	seen := make(map[int]struct{}, len(items))
	cp := p
	cp.presets = nil
	for _, it := range items {
		if _, dup := seen[it.PresetIndex]; dup {
			return p, fmt.Errorf("model: duplicate preset index %d for node %s", it.PresetIndex, p.id)
		}
		seen[it.PresetIndex] = struct{}{}
		cp.presets = append(cp.presets, it)
	}
	return cp, nil
}

// Presets returns a copy of the node's preset list; nil when none.
func (p NodeProfile) Presets() []PresetItem {
	if len(p.presets) == 0 {
		return nil
	}
	return append([]PresetItem(nil), p.presets...)
}

// WithHomePosition returns a copy carrying pos as the GB/T 28181-2022 guard
// position. An unconstructed HomePosition is refused so a malformed set
// command cannot clear a configured position silently.
func (p NodeProfile) WithHomePosition(pos HomePosition) (NodeProfile, error) {
	if pos.DeviceID() == "" {
		return p, fmt.Errorf("model: empty home position for node %s", p.id)
	}
	cp := p
	cp.homePosition = &pos
	return cp, nil
}

// HomePosition returns the stored guard position and whether one exists.
func (p NodeProfile) HomePosition() (HomePosition, bool) {
	if p.homePosition == nil {
		return HomePosition{}, false
	}
	return *p.homePosition, true
}

// WithCruiseTracks returns a copy whose cruise track list is tracks. All
// entries must be valid and unique by id; an empty list clears it.
func (p NodeProfile) WithCruiseTracks(tracks []CruiseTrack) (NodeProfile, error) {
	seen := make(map[string]struct{}, len(tracks))
	for _, t := range tracks {
		if t.ID() == "" {
			return p, fmt.Errorf("model: cruise track without id for node %s", p.id)
		}
		if _, dup := seen[t.ID()]; dup {
			return p, fmt.Errorf("model: duplicate cruise track %s for node %s", t.ID(), p.id)
		}
		seen[t.ID()] = struct{}{}
	}
	cp := p
	if len(tracks) == 0 {
		cp.cruiseTracks = nil
	} else {
		cp.cruiseTracks = append([]CruiseTrack(nil), tracks...)
	}
	return cp, nil
}

// CruiseTracks returns a copy of the node's cruise track list; nil when none.
func (p NodeProfile) CruiseTracks() []CruiseTrack {
	if len(p.cruiseTracks) == 0 {
		return nil
	}
	return append([]CruiseTrack(nil), p.cruiseTracks...)
}

// AppendSnapShot returns a copy with rec added to the snapshot log. Snap must
// be constructed via NewSnapShotRecord; duplicates by (device, channel, time)
// are tolerated — each capture is a distinct event.
func (p NodeProfile) AppendSnapShot(rec SnapShotRecord) (NodeProfile, error) {
	if rec.DeviceID() == "" {
		return p, fmt.Errorf("model: snapshot record without device id for node %s", p.id)
	}
	cp := p
	cp.snapshots = append(cp.snapshots, rec)
	return cp, nil
}

// SnapShots returns a copy of the node's snapshot log; nil when empty.
func (p NodeProfile) SnapShots() []SnapShotRecord {
	if len(p.snapshots) == 0 {
		return nil
	}
	return append([]SnapShotRecord(nil), p.snapshots...)
}

// String renders a log-safe one-line summary; never contains secrets.
func (p NodeProfile) String() string {
	return fmt.Sprintf("NodeProfile<id=%s kind=%s addr=%s domain=%s vendor=%q>",
		p.id.String(), p.id.Kind(), p.addr, p.domain, p.vendor)
}

// SetMediaConfig stores cfg on the profile. Pass the zero MediaConfig (Kind
// empty) to clear any previously set source — the device then answers INVITE
// with no media. Other profile fields are untouched; the call is idempotent.
func (p *NodeProfile) SetMediaConfig(cfg MediaConfig) {
	cp := cfg
	if cp.Kind == "" {
		p.mediaConfig = nil
		return
	}
	p.mediaConfig = &cp
}

// MediaConfig returns the configured source, or (zero, false) when none is
// set. The returned value is a copy: mutating it does not affect the
// profile's state.
func (p NodeProfile) MediaConfig() (MediaConfig, bool) {
	if p.mediaConfig == nil {
		return MediaConfig{}, false
	}
	return *p.mediaConfig, true
}

// SetChannelMediaConfig stores cfg on the profile for the given channelID.
// Pass the zero MediaConfig (Kind empty) to clear that channel's source —
// the channel then answers INVITE with no media. A previously empty
// mediaConfigByChannel is created lazily; the call is idempotent.
func (p *NodeProfile) SetChannelMediaConfig(channelID string, cfg MediaConfig) {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return
	}
	if p.mediaConfigByChannel == nil {
		p.mediaConfigByChannel = make(map[string]*MediaConfig)
	}
	if cfg.Kind == "" {
		delete(p.mediaConfigByChannel, channelID)
		if len(p.mediaConfigByChannel) == 0 {
			p.mediaConfigByChannel = nil
		}
		return
	}
	cp := cfg
	p.mediaConfigByChannel[channelID] = &cp
}

// ClearChannelMediaConfig removes the media source for the given channelID.
// Removing a non-existent channel is a no-op.
func (p *NodeProfile) ClearChannelMediaConfig(channelID string) {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return
	}
	delete(p.mediaConfigByChannel, channelID)
	if len(p.mediaConfigByChannel) == 0 {
		p.mediaConfigByChannel = nil
	}
}

// ChannelMediaConfig returns the configured source for the given channelID
// and whether one was set. The second result is false when no per-channel
// source was configured for that channel — callers MUST fall back to
// MediaConfig() in that case.
func (p NodeProfile) ChannelMediaConfig(channelID string) (MediaConfig, bool) {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" || len(p.mediaConfigByChannel) == 0 {
		return MediaConfig{}, false
	}
	cfg, ok := p.mediaConfigByChannel[channelID]
	if !ok || cfg == nil {
		return MediaConfig{}, false
	}
	return *cfg, true
}

// ChannelMediaConfigs returns a copy of the per-channel media source map
// (channel id → MediaConfig). Nil when no per-channel source was configured.
func (p NodeProfile) ChannelMediaConfigs() map[string]MediaConfig {
	if len(p.mediaConfigByChannel) == 0 {
		return nil
	}
	out := make(map[string]MediaConfig, len(p.mediaConfigByChannel))
	for k, v := range p.mediaConfigByChannel {
		if v == nil {
			continue
		}
		out[k] = *v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// MediaConfigForChannel returns the effective media source for the given
// channelID: a per-channel entry takes precedence over the node-level
// config. The bool is false only when neither is set.
func (p NodeProfile) MediaConfigForChannel(channelID string) (MediaConfig, bool) {
	if cfg, ok := p.ChannelMediaConfig(channelID); ok {
		return cfg, true
	}
	return p.MediaConfig()
}

// Node is an immutable node: an identity plus a lifecycle status. Status
// changes go through WithStatus, which enforces the transition table.
type Node struct {
	profile NodeProfile
	status  Status
}

// NewNode returns a node in StatusIdle.
func NewNode(profile NodeProfile) Node {
	return Node{profile: profile, status: StatusIdle}
}

// Profile returns the node identity and signalling details.
func (n Node) Profile() NodeProfile { return n.profile }

// Status returns the current lifecycle status.
func (n Node) Status() Status { return n.status }

// ID is a convenience accessor for the node id.
func (n Node) ID() NodeID { return n.profile.ID() }

// WithStatus returns a copy advanced to to. An illegal transition returns
// ErrIllegalTransition (judgeable with errors.Is) and leaves the receiver
// untouched; it never panics.
func (n Node) WithStatus(to Status) (Node, error) {
	if !n.status.CanTransition(to) {
		return n, fmt.Errorf("%w: %s -> %s", ErrIllegalTransition, n.status, to)
	}
	cp := n
	cp.status = to
	return cp, nil
}

// Registration is a convenience accessor for the profile's registration
// settings.
func (n Node) Registration() (Registration, bool) { return n.profile.Registration() }

// RegistrationResult is a convenience accessor for the profile's last
// registration outcome.
func (n Node) RegistrationResult() (RegistrationResult, bool) {
	return n.profile.RegistrationResult()
}

// PlatformServing is a convenience accessor for the profile's serving
// configuration.
func (n Node) PlatformServing() (PlatformServing, bool) { return n.profile.PlatformServing() }

// WithRegistrationResult returns a copy of the node whose profile records
// res. It is separate from WithStatus because a registration outcome is
// data, not a lifecycle step: recording one never moves the status.
func (n Node) WithRegistrationResult(res RegistrationResult) (Node, error) {
	profile, err := n.profile.WithRegistrationResult(res)
	if err != nil {
		return n, err
	}
	cp := n
	cp.profile = profile
	return cp, nil
}

// WithProfile returns a copy of the node whose profile is profile. The
// status and identity are untouched: swapping dynamic simulation state is
// data, not a lifecycle step, and a node cannot change its own id — the
// id from profile is replaced by the receiver's.
func (n Node) WithProfile(profile NodeProfile) Node {
	profile.id = n.profile.id
	cp := n
	cp.profile = profile
	return cp
}

// WithChannels returns a copy of the node whose profile carries the
// given channels. Used by handlers that want to mutate the device's
// channel list (e.g. when an upper-layer requests a fresh catalog) and
// need the change to take effect immediately. Other dynamic fields
// (alarms, position) are preserved.
func (n Node) WithChannels(channels []Channel) Node {
	cp := n
	cp.profile.channels = append([]Channel(nil), channels...)
	return cp
}

// WithPosition returns a copy of the node whose profile carries the
// given position. Pass nil to clear it.
func (n Node) WithPosition(pos *Position) Node {
	cp := n
	if pos == nil {
		cp.profile.position = nil
	} else {
		copy := *pos
		cp.profile.position = &copy
	}
	return cp
}

// AppendAlarm returns a copy of the node whose profile records an
// additional alarm snapshot. Order is preserved (oldest first).
func (n Node) AppendAlarm(a AlarmSnapshot) Node {
	cp := n
	cp.profile.alarms = append(append([]AlarmSnapshot(nil), n.profile.alarms...), a)
	return cp
}

// String renders a log-safe one-line summary.
func (n Node) String() string {
	return fmt.Sprintf("Node<id=%s status=%s>", n.ID().String(), n.status)
}
