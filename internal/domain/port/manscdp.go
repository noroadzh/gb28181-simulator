// Package port — MANSCDP codec.
package port

import "github.com/your-org/gb28181-simulator/internal/domain/model"

// MANSCDPCodec reads and writes the MANSCDP bodies a platform exchanges:
// the notify a downstream sends and the catalog answer the platform gives
// back.
//
// It is the platform's half of the protocol and deliberately separate from
// KeepaliveCodec, which is the device's half (rendering only). Merging them
// would force every device to carry a parser it never calls.
//
// Implementations MUST NOT panic on malformed input: a body another
// vendor's device produced is data, and a platform that crashes on data is
// a platform that can be switched off remotely. They return an error
// instead, and the caller decides to ignore it.
type MANSCDPCodec interface {
	// DecodeNotify parses a MANSCDP notify body into its command. It
	// returns an error when the body is not XML, is not a notify, or
	// carries no command type or device id.
	DecodeNotify(body string) (model.Notify, error)

	// MarshalCatalog renders a catalog answer, declaration included and
	// terminated by a newline. The result is placed verbatim as the
	// message body.
	MarshalCatalog(catalog model.Catalog) (string, error)
}
