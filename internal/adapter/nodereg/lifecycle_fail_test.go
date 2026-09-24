package nodereg_test

import (
	"context"
	"errors"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/adapter/nodereg"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// TestLifecycle_FailFaultsAndReleasesListener is task 7.1: a start that
// cannot complete must leave a faulted node whose port is free again.
func TestLifecycle_FailFaultsAndReleasesListener(t *testing.T) {
	ctx := context.Background()
	reg := nodereg.New()
	factory := newFakeFactory()
	lc := nodereg.NewLifecycle(reg, factory.bind)

	node, err := reg.Register(ctx, mustProfile(t, idA, "127.0.0.1:5060"))
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := lc.Start(ctx, node.ID()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	cause := errors.New("registration timed out")
	if err := lc.Fail(ctx, node.ID(), cause); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if st, _ := lc.Status(ctx, node.ID()); st != model.StatusFault {
		t.Errorf("status after Fail = %v, want fault", st)
	}
	if tr := factory.last(); tr == nil || !tr.closed {
		t.Error("the listener was not closed, so the port is still held")
	}
	if lc.Transport(node.ID()) != nil {
		t.Error("Transport() still set after Fail")
	}
	// The port is really free: the node can bind again straight away,
	// which is what makes "fix the config and retry" possible.
	if err := lc.Start(ctx, node.ID()); err != nil {
		t.Errorf("restart after a fault: %v", err)
	}
	if lc.Transport(node.ID()) == nil {
		t.Error("no listener bound after restarting a faulted node")
	}
	if factory.count() != 2 {
		t.Errorf("factory bound %d listeners, want 2 (initial + restart)", factory.count())
	}
}

// A node that never started has no legal path to fault; Fail must say so
// instead of panicking (task 7.2).
func TestLifecycle_FailWithoutStart(t *testing.T) {
	ctx := context.Background()
	reg := nodereg.New()
	lc := nodereg.NewLifecycle(reg, newFakeFactory().bind)

	node, err := reg.Register(ctx, mustProfile(t, idA, "127.0.0.1:5060"))
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := lc.Fail(ctx, node.ID(), errors.New("boom")); err == nil {
		t.Fatal("Fail on an idle node succeeded, want an illegal-transition error")
	}
	if st, _ := lc.Status(ctx, node.ID()); st != model.StatusIdle {
		t.Errorf("status = %v, want idle (unchanged)", st)
	}
}

// Calling Fail twice must not corrupt the node: the second attempt is
// simply refused.
func TestLifecycle_FailTwice(t *testing.T) {
	ctx := context.Background()
	reg := nodereg.New()
	lc := nodereg.NewLifecycle(reg, newFakeFactory().bind)

	node, err := reg.Register(ctx, mustProfile(t, idA, "127.0.0.1:5060"))
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := lc.Start(ctx, node.ID()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := lc.Fail(ctx, node.ID(), errors.New("first")); err != nil {
		t.Fatalf("first Fail: %v", err)
	}
	if err := lc.Fail(ctx, node.ID(), errors.New("second")); err == nil {
		t.Fatal("second Fail succeeded, want it refused")
	}
	if st, _ := lc.Status(ctx, node.ID()); st != model.StatusFault {
		t.Errorf("status = %v, want fault", st)
	}
}

// Fail on an unknown id names the node rather than silently succeeding.
func TestLifecycle_FailUnknownNode(t *testing.T) {
	ctx := context.Background()
	reg := nodereg.New()
	lc := nodereg.NewLifecycle(reg, newFakeFactory().bind)

	unknown := mustParseID(t, idA)
	if err := lc.Fail(ctx, unknown, errors.New("boom")); err == nil {
		t.Fatal("Fail on an unknown node succeeded, want an error")
	}
}

// A nil cause is tolerated: the log line simply omits it.
func TestLifecycle_FailWithNilReason(t *testing.T) {
	ctx := context.Background()
	reg := nodereg.New()
	lc := nodereg.NewLifecycle(reg, newFakeFactory().bind)

	node, err := reg.Register(ctx, mustProfile(t, idA, "127.0.0.1:5060"))
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := lc.Start(ctx, node.ID()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := lc.Fail(ctx, node.ID(), nil); err != nil {
		t.Fatalf("Fail(nil): %v", err)
	}
	if st, _ := lc.Status(ctx, node.ID()); st != model.StatusFault {
		t.Errorf("status = %v, want fault", st)
	}
}
