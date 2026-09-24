// Package sip is the GB/T 28181-2016 §L-aware wrapper around
// github.com/ghettovoice/gosip. It exposes the public types and helpers
// consumed by Change 4-15 and the sipprobe CLI.
//
// Design:
//
//   - Request and Response are interfaces (re-exported from gosip) so
//     callers can mock them in tests.
//   - BuildRequest / BuildResponse construct RFC 3261 messages with the
//     §L.1 mandatory headers auto-filled (Via branch, Max-Forwards,
//     User-Agent, Content-Length).
//   - ParseMessage is a thin convenience over gosip's parser.
//   - NewBranchGenerator produces collision-resistant Via branches per
//     RFC 3261 §8.1.1.7 (z9hG4bK magic cookie + 12-byte random suffix).
//
// The package does NOT depend on internal/sdp or internal/auth; only the
// auth/auth.go Fields type is referenced from ParseAuthorization (which
// lives in this package and exposes the auth.Fields result).
package sip

import (
	"github.com/ghettovoice/gosip/sip"
)

// Request re-exports the gosip Request interface so callers don't have
// to import the upstream package.
type Request = sip.Request

// Response re-exports the gosip Response interface.
type Response = sip.Response

// Message re-exports the gosip Message interface.
type Message = sip.Message

// Header re-exports the gosip Header interface.
type Header = sip.Header

// RequestMethod is the SIP method type.
type RequestMethod = sip.RequestMethod

// StatusCode is the SIP status code type.
type StatusCode = sip.StatusCode

// Common request methods, exposed as constants for convenience.
const (
	MethodRegister sip.RequestMethod = sip.REGISTER
	MethodInvite   sip.RequestMethod = sip.INVITE
	MethodAck      sip.RequestMethod = sip.ACK
	MethodBye      sip.RequestMethod = sip.BYE
	MethodCancel   sip.RequestMethod = sip.CANCEL
	MethodOptions  sip.RequestMethod = sip.OPTIONS
)

// --- version --------------------------------------------------------------

// Version is the User-Agent suffix used when BuildRequest auto-fills the
// User-Agent header. Overridable from main package via SetVersion.
var Version = "0.0.0-dev"

// SetVersion updates the auto-filled User-Agent suffix. Intended for
// tests that want deterministic bytes.
func SetVersion(v string) {
	if v != "" {
		Version = v
	}
}
