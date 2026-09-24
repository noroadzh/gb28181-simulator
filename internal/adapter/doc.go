// Package adapter contains concrete implementations of the domain ports:
// SIP transport (UDP/TCP/TLS), SDP codec, digest auth, in-memory / on-disk
// storage, and audit emitter. Every exported type that fulfils a port must
// end the file with a compile-time interface assertion of the form
// `var _ port.SIPTransport = (*Transport)(nil)` so a future signature drift
// is caught at build time.
//
// Layering rule: adapter imports domain (and platform). It MUST NOT import
// interface or app. Adapters are swappable without touching the use cases.
package adapter