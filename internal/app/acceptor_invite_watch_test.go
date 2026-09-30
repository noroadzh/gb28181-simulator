package app

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// A cancelled INVITE watchdog must not survive the cancel. The old
// implementation stopped the timer but left its goroutine parked on
// timer.C forever, so every confirmed dialog cost one goroutine for the
// life of the process.
func TestAcceptor_INVITEExpiry_NoLeak(t *testing.T) {
	acceptor, _, _, _, nodeID := acceptorFixture(t, &fakeAuthenticator{})
	p := acceptor.platformForTest(t, nodeID)

	const dialogs = 200
	for i := 0; i < dialogs; i++ {
		acceptor.watchInviteExpiry(p, fmt.Sprintf("leak-call-%03d", i))
	}

	// Every watcher is registered before any is cancelled, so the "leaked"
	// state is a real one rather than a race between two loops.
	deadline := time.Now().Add(2 * time.Second)
	for {
		acceptor.inviteMu.Lock()
		pending := len(acceptor.inviteWatchers)
		acceptor.inviteMu.Unlock()
		if pending == dialogs {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d watchers registered", pending, dialogs)
		}
		time.Sleep(5 * time.Millisecond)
	}

	gBefore := runtime.NumGoroutine()
	for i := 0; i < dialogs; i++ {
		acceptor.cancelInviteExpiry(fmt.Sprintf("leak-call-%03d", i))
	}

	// The registry is emptied synchronously by cancel, but the goroutines
	// still have to wake and unwind; wait for that instead of assuming it.
	deadline = time.Now().Add(2 * time.Second)
	for {
		acceptor.inviteMu.Lock()
		left := len(acceptor.inviteWatchers)
		timers := len(acceptor.inviteTimers)
		acceptor.inviteMu.Unlock()
		if left == 0 && timers == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after cancelling all %d dialogs: %d watchers and %d timers remain",
				dialogs, left, timers)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// A bound on goroutine growth rather than an exact delta: the Go test
	// harness, the runtime's GC background workers and the time-sleeping
	// goroutines above all move the baseline around. 200 watchers is well
	// above any reasonable noise.
	gAfter := runtime.NumGoroutine()
	if delta := gAfter - gBefore; delta > 50 {
		t.Errorf("goroutine count grew by %d after cancelling 200 watchers; expected near zero",
			delta)
	}
}

// The watch list is an Acceptor field, not a package global: cancelling
// on one node must not affect a sibling in the same process.
func TestAcceptor_INVITEExpiry_RegistryIsPerAcceptor(t *testing.T) {
	first, _, _, _, firstID := acceptorFixture(t, &fakeAuthenticator{})
	second, _, _, _, secondID := acceptorFixture(t, &fakeAuthenticator{})

	first.watchInviteExpiry(first.platformForTest(t, firstID), "shared-call-id")
	second.watchInviteExpiry(second.platformForTest(t, secondID), "shared-call-id")

	first.cancelInviteExpiry("shared-call-id")

	if left := watcherCount(first); left != 0 {
		t.Errorf("the cancelled node still has %d watchers", left)
	}
	if pending := watcherCount(second); pending != 1 {
		t.Errorf("the other node has %d watchers, want 1: registries are not isolated",
			pending)
	}

	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// Stopping the node must release the watchdogs belonging to it. This is the
// scenario "node shutdown cancels all pending watchdogs" from the spec.
func TestAcceptor_INVITEExpiry_NodeStopReleasesWatchers(t *testing.T) {
	acceptor, _, _, _, nodeID := acceptorFixture(t, &fakeAuthenticator{})
	p := acceptor.platformForTest(t, nodeID)

	for i := 0; i < 25; i++ {
		acceptor.watchInviteExpiry(p, fmt.Sprintf("node-stop-call-%02d", i))
	}

	if pending := watcherCount(acceptor); pending != 25 {
		t.Fatalf("registered %d watchers, got %d", 25, pending)
	}

	acceptor.Stop(nodeID)

	deadline := time.Now().Add(2 * time.Second)
	for {
		if watcherCount(acceptor) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("node shutdown left %d watchers behind", watcherCount(acceptor))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Cancelling twice, or cancelling a Call-ID that was never registered, must
// not panic: SIP dialogs can be confirmed, then BYE'd, then another BYE
// arrives out of order — and the watchdog code must stay quiet.
func TestAcceptor_INVITEExpiry_RepeatedCancelIsSafe(t *testing.T) {
	acceptor, _, _, _, nodeID := acceptorFixture(t, &fakeAuthenticator{})
	p := acceptor.platformForTest(t, nodeID)

	acceptor.watchInviteExpiry(p, "double-cancel")
	acceptor.cancelInviteExpiry("double-cancel")
	acceptor.cancelInviteExpiry("double-cancel")
	acceptor.cancelInviteExpiry("never-registered")
}

// helper: peek at the watch map without racing the cleanup goroutine.
func watcherCount(a *Acceptor) int {
	a.inviteMu.Lock()
	defer a.inviteMu.Unlock()
	return len(a.inviteWatchers)
}

// helper: hand a test the platform value for a node that the fixture served.
// Tests reach into a.serving directly because there is no public lookup.
func (a *Acceptor) platformForTest(t *testing.T, id model.NodeID) *platform {
	t.Helper()
	a.mu.Lock()
	p, ok := a.serving[id.String()]
	a.mu.Unlock()
	if !ok {
		t.Fatalf("no platform for node %s", id.String())
	}
	return p
}
