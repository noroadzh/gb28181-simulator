package media

import "github.com/your-org/gb28181-simulator/internal/domain/model"

// ErrSourceClosed is the error a caller sees when a media source was closed
// underneath it. It is an alias rather than a second value on purpose: a
// caller that holds only an io.ReadCloser from Open still recognises the
// difference between "we stopped this" and "the source failed" with
// errors.Is, and an error that arrives wrapped from the packetizer keeps
// matching, because both names denote one identity.
var ErrSourceClosed = model.ErrSourceClosed
