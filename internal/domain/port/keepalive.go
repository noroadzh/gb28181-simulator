// Package port — Keepalive codec.
package port

import "github.com/your-org/gb28181-simulator/internal/domain/model"

// KeepaliveCodec renders the body of a GB/T 28181 keepalive — a MANSCDP
// notify — from the domain value.
//
// The XML belongs to an adapter: the app layer composes the SIP message and
// only ever sees the rendered string, which keeps MANSCDP details out of the
// use cases and makes the bytes testable on their own.
type KeepaliveCodec interface {
	// MarshalKeepalive renders the notify body. The result is placed
	// verbatim as the message body.
	MarshalKeepalive(k model.Keepalive) (string, error)
}
