// Package port — SDPCodec.
package port

import "github.com/your-org/gb28181-simulator/internal/domain/model"

// SDPCodec parses RFC 4566 SDP text (with GB/T 28181 §K.2 y=/f= extensions)
// into an immutable model.Session, and serialises a model.Session back to
// the wire format.
type SDPCodec interface {
	// Parse turns a raw SDP body into a Session. Empty input is an error
	// (RFC 4566 mandates at least v=/o=/s=/t=). The returned Session is
	// fully constructed; the adapter MUST NOT hold any reference to the
	// input string after Parse returns.
	Parse(text string) (model.Session, error)

	// Marshal turns a Session back into an SDP body suitable for the SIP
	// message body. The output preserves GB/T 28181 §K.2 ordering.
	Marshal(s model.Session) (string, error)
}
