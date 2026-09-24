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
)
