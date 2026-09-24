// Package domain holds the business invariants of the gb28181-simulator:
// immutable value objects (Message, Session, Credentials, WireEvent) and
// the port interfaces that adapters must satisfy (SIPTransport, SDPCodec,
// Authenticator, Challenger, AuditSink, Clock, Storage).
//
// Layering rule: domain depends only on the Go standard library and on
// platform/clock when it needs time. It MUST NOT import any adapter,
// interface or app package. Domain is the innermost layer and never knows
// how a SIP packet is parsed or how a device is stored on disk.
package domain