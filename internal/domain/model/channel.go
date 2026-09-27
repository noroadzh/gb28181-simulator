// Package model — Channel and dynamic device channel list.
package model

import (
	"fmt"
	"strings"
)

// ChannelStatus enumerates the online/offline status of a device channel
// reported through Catalog and MediaStatus.
type ChannelStatus int

const (
	// ChannelStatusUnknown is the zero value.
	ChannelStatusUnknown ChannelStatus = iota
	// ChannelStatusOnline means the channel is reachable.
	ChannelStatusOnline
	// ChannelStatusOffline means the channel is registered but unreachable.
	ChannelStatusOffline
)

// channelStatusNames is the configuration/JSON spelling used by Status().
var channelStatusNames = map[ChannelStatus]string{
	ChannelStatusOnline:  "ON",
	ChannelStatusOffline: "OFF",
}

// channelStatusParse maps the configuration spelling to a ChannelStatus.
// Empty input is rejected so callers cannot pass status silently.
func channelStatusParse(s string) (ChannelStatus, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "ON", "ONLINE":
		return ChannelStatusOnline, nil
	case "OFF", "OFFLINE":
		return ChannelStatusOffline, nil
	case "":
		return ChannelStatusUnknown, fmt.Errorf("model: empty channel status")
	default:
		return ChannelStatusUnknown, fmt.Errorf("model: unknown channel status %q", s)
	}
}

// ParseChannelStatus is the exported form of channelStatusParse: it maps the
// wire/config spelling ("ON"/"OFF"/"online"/"offline") to a ChannelStatus.
func ParseChannelStatus(s string) (ChannelStatus, error) {
	return channelStatusParse(s)
}

// String returns the canonical status spelling ("ON" / "OFF") or "" for
// ChannelStatusUnknown.
func (s ChannelStatus) String() string { return channelStatusNames[s] }

// Channel describes one logical channel of a device (camera, alarm input).
// It is independent of the lower-level SIP Channel/VideoParam pair: a Channel
// is what Catalog and MediaStatus surface to upstream platforms.
type Channel struct {
	id       string
	name     string
	status   ChannelStatus
	parentID string
}

// NewChannel builds a Channel. id and name are required; parentID may be
// empty (channel is part of the device itself).
func NewChannel(id, name, parentID string, status ChannelStatus) (Channel, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Channel{}, fmt.Errorf("model: channel without id")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Channel{}, fmt.Errorf("model: channel %s without name", id)
	}
	if status == ChannelStatusUnknown {
		status = ChannelStatusOnline
	}
	return Channel{
		id:       id,
		name:     name,
		status:   status,
		parentID: strings.TrimSpace(parentID),
	}, nil
}

// ID returns the channel identifier (typically DeviceID:ChannelID).
func (c Channel) ID() string { return c.id }

// Name returns the human-readable channel name.
func (c Channel) Name() string { return c.name }

// Status returns the current online/offline status.
func (c Channel) Status() ChannelStatus { return c.status }

// ParentID returns the parent device id, or "" for a top-level channel.
func (c Channel) ParentID() string { return c.parentID }

// WithStatus returns a copy with a new status.
func (c Channel) WithStatus(status ChannelStatus) Channel {
	c.status = status
	return c
}

// String renders a log-safe one-line summary.
func (c Channel) String() string {
	return fmt.Sprintf("Channel<id=%s name=%s status=%s>", c.id, c.name, c.status)
}
