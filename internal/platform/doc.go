// Package platform hosts cross-cutting infrastructure that every other layer
// can depend on: structured logging, OpenTelemetry tracing, audit emission,
// typed configuration, an injectable Clock, and a hand-written DI container
// (ServiceContext).
//
// Layering rule: platform may be imported by domain / adapter / interface /
// app, but it must never import any of those packages. Platform is the
// outermost layer in the hexagonal architecture and has zero awareness of SIP,
// SDP or storage.
package platform