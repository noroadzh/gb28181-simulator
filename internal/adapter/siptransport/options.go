package siptransport

import "github.com/ghettovoice/gosip/sip"

// Option configures a Transport at construction time.
type Option func(*transportConfig)

type transportConfig struct {
	receiveBuffer int
	msgMapper     sip.MessageMapper // optional: transform inbound messages
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