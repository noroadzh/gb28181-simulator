package siptransport

import "github.com/ghettovoice/gosip/sip"

// Option configures a Transport at construction time.
type Option func(*transportConfig)

type transportConfig struct {
	receiveBuffer int
	msgMapper     sip.MessageMapper // optional: transform inbound messages
	nodeID        string            // optional: owning node identifier for audit events
}

func defaultConfig() transportConfig {
	return transportConfig{receiveBuffer: DefaultReceiveBuffer}
}

// WithReceiveBuffer overrides the receive-channel capacity. Values ≤ 0
// fall back to DefaultReceiveBuffer.
func WithReceiveBuffer(n int) Option {
	return func(c *transportConfig) {
		if n > 0 {
			c.receiveBuffer = n
		}
	}
}

// WithMessageMapper replaces gosip's default message mapper. Pass nil to
// use the no-op mapper (default). Change 13 (抓包) and Change 14 (Web)
// will pass a custom mapper here to inject tracing.
func WithMessageMapper(m sip.MessageMapper) Option {
	return func(c *transportConfig) {
		c.msgMapper = m
	}
}

// WithNodeID tags the transport with an owning node identifier. When set,
// every audit WireEvent this transport emits carries it, so the capture
// store (Change 13) can attribute events to the right node when multiple
// nodes coexist in one process. An empty string keeps the legacy behaviour
// (events are untagged).
func WithNodeID(id string) Option {
	return func(c *transportConfig) {
		c.nodeID = id
	}
}
