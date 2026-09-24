// Package port — node.
package port

import (
	"context"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// NodeRegistry is the process-wide catalogue of nodes. Implementations MUST
// be safe for concurrent use: several goroutines may register, unregister
// and look up nodes at the same time. A node identity is unique —
// registering an id that already exists is an error and MUST NOT overwrite
// the incumbent node.
type NodeRegistry interface {
	// Register adds a node built from profile and returns it in StatusIdle.
	// It returns an error if the id is already registered, or if the
	// signalling address is already claimed by another node (design D3:
	// never fall back to a random port silently).
	Register(ctx context.Context, profile model.NodeProfile) (model.Node, error)

	// Unregister removes the node and releases its signalling address. It
	// returns an error if the id is unknown; it MUST NOT affect any other
	// node.
	Unregister(ctx context.Context, id model.NodeID) error

	// Get looks a node up by id. The second result is false when the id is
	// unknown; callers use it to distinguish "absent" from an error.
	Get(ctx context.Context, id model.NodeID) (model.Node, bool)

	// List returns every registered node. The order is unspecified, so
	// tests MUST sort before comparing.
	List(ctx context.Context) []model.Node

	// RecordRegistration stores what a completed registration produced
	// (the platform, the lifetime it granted) and returns the updated
	// node. Recording is data, not a lifecycle step: it never changes the
	// node's status.
	RecordRegistration(ctx context.Context, id model.NodeID, result model.RegistrationResult) (model.Node, error)
}

// StagedFailure is an error that knows which stage of an operation it came
// from. It lets upper layers (HTTP, CLI) report "the registration timed
// out" instead of a bare failure, without depending on the package that
// produced the error.
type StagedFailure interface {
	error
	// FailureStage returns a short, machine-readable stage name, e.g.
	// "send", "challenge", "response", "timeout".
	FailureStage() string
}

// NodeAdvancer is the write path the app layer uses to move a node to a
// specific status. It is separate from NodeLifecycle because Start/Stop are
// intent-revealing operations, while this one exists so the identity
// implementations in Change 5/6/7 can advance a node to Registered or Online
// once the protocol exchange succeeds (design D9).
//
// The transition table lives in the model, so an illegal jump is refused
// with model.ErrIllegalTransition and the node is left unchanged.
type NodeAdvancer interface {
	// Advance moves the node to to and returns the updated node. On an
	// illegal transition it returns the unchanged node and
	// model.ErrIllegalTransition.
	Advance(ctx context.Context, id model.NodeID, to model.Status) (model.Node, error)
}

// NodeLifecycle drives a single node through the status machine. Starting or
// stopping one node MUST NOT affect any other node: each owns an independent
// listener and an independent lifecycle (design D3).
type NodeLifecycle interface {
	// Start binds the node's signalling listener and advances it to
	// StatusRegistering. It does NOT advance further — registration is
	// driven by the identity implementations in Change 5/6/7 (design D9).
	// A failure returns an error and leaves the node in StatusIdle or
	// StatusFault; a half-started node is never left behind.
	Start(ctx context.Context, id model.NodeID) error

	// Stop releases the node's listener and advances it to StatusOffline.
	Stop(ctx context.Context, id model.NodeID) error

	// Status returns the node's current status, or an error if the id is
	// unknown.
	Status(ctx context.Context, id model.NodeID) (model.Status, error)

	// Transport returns the listener bound to id, or nil when the node is
	// not running. An identity implementation uses it to talk to the peer
	// without ever depending on a concrete transport.
	Transport(id model.NodeID) SIPTransport

	// Fail ends a start that cannot complete (a registration that was
	// refused or timed out): the node moves to StatusFault and its
	// listener is released, so the port can be rebound immediately.
	// reason is logged, never returned.
	Fail(ctx context.Context, id model.NodeID, reason error) error
}
