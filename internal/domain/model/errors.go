// Package model — shared errors.
package model

import "errors"

// Lookups that find nothing. They are domain errors rather than per-layer
// ones because every layer has to agree on what they mean: an unknown node
// or device is a 404 in HTTP, not a server fault, and the same answer has
// to come out of a use case, a test double and a future CLI.
var (
	ErrUnknownNode   = errors.New("model: unknown node")
	ErrUnknownDevice = errors.New("model: unknown downstream device")

	// ErrSourceClosed is what a reader of an already-closed media source
	// gets back. It is a domain error because the distinction callers care
	// about is "we closed it" versus "it broke": shutting a session down is
	// not a media fault, and the node lifecycle must not be told it is.
	// Every layer agrees on that meaning, so the sentinel lives here as well
	// as next to the sources that raise it.
	ErrSourceClosed = errors.New("model: media source closed")
)
