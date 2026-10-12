// Package port — catalog media-source propagation value.
package port

import "github.com/your-org/gb28181-simulator/internal/domain/model"

// ChannelMedia is one channel's media-source configuration as it arrived on
// the wire inside a catalog Item's Extension/MediaSource block.
//
// It exists at the port boundary because the app layer may only import
// domain/port and domain/model — never internal/adapter/manscdp. The codec
// adapter produces these, the app layer consumes them.
//
// DeviceID is the channel id the configuration belongs to (it is the Item's
// DeviceID on the wire, not necessarily a node id). Config is the
// deserialized media source; callers SHOULD Validate it and skip the entry
// when it is incomplete rather than trusting a peer-supplied block.
type ChannelMedia struct {
	// DeviceID is the channel the configuration belongs to.
	DeviceID string
	// Config is the media source the peer advertised for that channel.
	Config model.MediaConfig
}
