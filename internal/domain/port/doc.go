// Package port declares the interfaces (the "ports" of the hexagonal
// architecture) that every adapter must satisfy. The signatures take and
// return only domain/model value objects so that adapters remain decoupled
// from concrete types in adapter/.
//
// Adding a port MUST come with at least one adapter that implements it and a
// compile-time assertion in that adapter of the form
// `var _ port.SIPTransport = (*Transport)(nil)`.
package port
