package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// faultGateFixture builds an Acceptor wired to a FaultStore and starts it.
// The profile is installed before Serve returns so the gate sees it on the
// first request.
func faultGateFixture(
	t *testing.T,
	auth port.Authenticator,
	profile model.FaultProfile,
) (*FaultStoreAdapter, *acceptorTransport, model.NodeID) {
	t.Helper()
	platformID := mustPlatformNode(t)
	tr := newAcceptorTransport()
	creds := newFakeCredentials()
	devices := newFakeDevices()
	clock := newSyncClock(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC))
	acceptor, err := NewAcceptor(context.Background(), clock, &fakeChallenger{}, auth, creds,
		devices, newFakeMANSCDP(), nil, discardLogger())
	if err != nil {
		t.Fatalf("NewAcceptor: %v", err)
	}

	reg := &fakeNodeRegistry{nodes: map[model.NodeID]bool{platformID: true}}
	fs := NewFaultStore(reg, nil)
	if !profile.IsZero() {
		if err := fs.Install(context.Background(), platformID, profile); err != nil {
			t.Fatalf("Install: %v", err)
		}
	}
	acceptor.WithFaults(fs)

	policy := mustPolicy(t, 60, 3600, 7200)
	if err := acceptor.Serve(platformID, tr, "3402000000", policy); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	t.Cleanup(func() { acceptor.Close() })
	return fs, tr, platformID
}

// send injects a request and returns when the acceptor answers.
func send(tb testing.TB, tr *acceptorTransport, req model.Message) model.Message {
	tb.Helper()
	return tr.deliver(tb, req)
}

// sendNoAnswer fires a request and asserts no answer is produced within
// the supplied timeout.
func sendNoAnswer(tb testing.TB, tr *acceptorTransport, req model.Message, timeout time.Duration) {
	tb.Helper()
	before := tr.answers()
	select {
	case tr.inbound <- req:
	default:
		tb.Fatal("inbound buffer full")
	}
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			goto done
		default:
		}
		if tr.answers() > before {
			tb.Fatal("expected silent drop, got an answer")
		}
		time.Sleep(time.Millisecond)
	}
done:
	if tr.answers() != before {
		tb.Fatalf("answers went from %d to %d, want silent drop", before, tr.answers())
	}
}

// gateResponseHeader extracts a header value from a response (test-local
// name to avoid clashing with the one in acceptor_test.go — same package).
func gateResponseHeader(tb testing.TB, msg model.Message, name string) string {
	tb.Helper()
	h, ok := msg.Header(name)
	if !ok {
		tb.Fatalf("response has no %s header", name)
	}
	return h.Value()
}

// TestFaultGate_Blackhole asserts that a request whose method is listed in
// the profile's Blackhole slice is silently ignored and increments the
// blackhole counter.
func TestFaultGate_Blackhole(t *testing.T) {
	auth := &fakeAuthenticator{}
	fs, tr, nodeID := faultGateFixture(t, auth, model.FaultProfile{
		Blackhole: []string{"REGISTER"},
	})

	req := registerRequest(t, "34020000011320000001")
	sendNoAnswer(t, tr, req, 200*time.Millisecond)

	counters := fs.FaultCounters(nodeID)
	if counters[model.FaultBlackhole] != 1 {
		t.Fatalf("blackhole counter = %v, want 1", counters)
	}
}

// TestFaultGate_Drop asserts that a request is silently discarded when the
// drop probability forces it (here p=1.0 for determinism).
func TestFaultGate_Drop(t *testing.T) {
	auth := &fakeAuthenticator{}
	fs, tr, nodeID := faultGateFixture(t, auth, model.FaultProfile{
		Drop: 1.0,
	})

	sendNoAnswer(t, tr, registerRequest(t, "34020000011320000002"), 200*time.Millisecond)

	counters := fs.FaultCounters(nodeID)
	if counters[model.FaultDrop] != 1 {
		t.Fatalf("drop counter = %v, want 1", counters)
	}
}

// TestFaultGate_Delay asserts that the response is still sent after the
// configured delay and the delay counter is incremented.
func TestFaultGate_Delay(t *testing.T) {
	auth := &fakeAuthenticator{}
	fs, tr, nodeID := faultGateFixture(t, auth, model.FaultProfile{
		Delay: model.FaultDelayConfig{Base: 30 * time.Millisecond},
	})

	start := time.Now()
	resp := send(t, tr, registerRequest(t, "34020000011320000003"))
	elapsed := time.Since(start)

	// The gate only delays; the normal pipeline still answers (here with the
	// usual 401 challenge, since the request carries no credentials).
	if resp.StatusCode() == 0 {
		t.Fatal("no response status")
	}
	if elapsed < 20*time.Millisecond {
		t.Fatalf("response arrived in %v, want >= 20ms", elapsed)
	}

	counters := fs.FaultCounters(nodeID)
	if counters[model.FaultDelay] != 1 {
		t.Fatalf("delay counter = %v, want 1", counters)
	}
}

// TestFaultGate_Canned asserts that a canned profile answers with the
// configured status, the transaction headers are echoed and the canned
// counter increments.
func TestFaultGate_Canned(t *testing.T) {
	auth := &fakeAuthenticator{}
	fs, tr, nodeID := faultGateFixture(t, auth, model.FaultProfile{
		Canned: map[string]int{"REGISTER": 403},
	})

	resp := send(t, tr, registerRequest(t, "34020000011320000004"))
	if resp.StatusCode() != 403 {
		t.Fatalf("status = %d, want 403", resp.StatusCode())
	}
	if r := gateResponseHeader(t, resp, "Call-ID"); r != "call-1" {
		t.Errorf("Call-ID = %q, want call-1", r)
	}
	if _, hasVia := resp.Header("Via"); !hasVia {
		t.Error("canned response has no Via")
	}
	if to := gateResponseHeader(t, resp, "To"); !strings.Contains(to, "tag=") {
		t.Errorf("To = %q, want a tag", to)
	}

	counters := fs.FaultCounters(nodeID)
	if counters[model.FaultCannedResponse] != 1 {
		t.Fatalf("canned_response counter = %v, want 1", counters)
	}
}

// TestFaultGate_UnsupportedMethod asserts that the UnsupportedMethod status
// is answered for a method the node does not serve.
func TestFaultGate_UnsupportedMethod(t *testing.T) {
	auth := &fakeAuthenticator{}
	fs, tr, nodeID := faultGateFixture(t, auth, model.FaultProfile{
		UnsupportedMethod: 501,
	})

	req, err := model.NewRequest(
		"FOOBAR",
		"sip:3402000000@3402000000",
		[]model.Header{
			model.NewHeader("From", "<sip:34020000011320000005@3402000000>;tag=x"),
			model.NewHeader("To", "<sip:34020000002000000001@3402000000>"),
			model.NewHeader("Call-ID", "call-foobar"),
			model.NewHeader("CSeq", "1 FOOBAR"),
			model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-x"),
		},
		"",
	)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp := send(t, tr, req)
	if resp.StatusCode() != 501 {
		t.Fatalf("status = %d, want 501", resp.StatusCode())
	}
	if r := gateResponseHeader(t, resp, "Call-ID"); r != "call-foobar" {
		t.Errorf("Call-ID = %q, want call-foobar", r)
	}

	counters := fs.FaultCounters(nodeID)
	if counters[model.FaultUnsupportedMethod] != 1 {
		t.Fatalf("unsupported_method counter = %v, want 1", counters)
	}
}

// TestFaultGate_Default405 asserts that a method with no fault profile gets a
// 405 Method Not Allowed with an Allow header (task 6.1).
func TestFaultGate_Default405(t *testing.T) {
	auth := &fakeAuthenticator{}
	fs, tr, _ := faultGateFixture(t, auth, model.FaultProfile{}) // no profile

	req, err := model.NewRequest(
		"FOO",
		"sip:3402000000@3402000000",
		[]model.Header{
			model.NewHeader("From", "<sip:34020000011320000006@3402000000>;tag=y"),
			model.NewHeader("To", "<sip:34020000002000000001@3402000000>"),
			model.NewHeader("Call-ID", "call-foo"),
			model.NewHeader("CSeq", "1 FOO"),
			model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-y"),
		},
		"",
	)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp := send(t, tr, req)
	if resp.StatusCode() != 405 {
		t.Fatalf("status = %d, want 405", resp.StatusCode())
	}
	allow := gateResponseHeader(t, resp, "Allow")
	for _, m := range []string{"REGISTER", "MESSAGE", "INVITE", "ACK", "BYE", "OPTIONS", "SUBSCRIBE", "INFO"} {
		if !strings.Contains(allow, m) {
			t.Errorf("Allow header %q is missing %q", allow, m)
		}
	}
	if !strings.Contains(allow, "INFO") {
		t.Error("Allow header is missing INFO")
	}
	// No fault counter since the default path does not count as a fault.
	if n := len(fs.FaultCounters(model.NodeID{})); n != 0 {
		t.Errorf("fault counters = %v, want empty for default 405", fs.FaultCounters(model.NodeID{}))
	}
}

// TestFaultGate_CannedOverridesUnsupportedMethod asserts that a canned response
// for a specific method wins over the UnsupportedMethod status (task 6.1).
func TestFaultGate_CannedOverridesUnsupportedMethod(t *testing.T) {
	auth := &fakeAuthenticator{}
	fs, tr, _ := faultGateFixture(t, auth, model.FaultProfile{
		UnsupportedMethod: 501,
		Canned:            map[string]int{"FOOBAR": 403},
	})

	req, err := model.NewRequest(
		"FOOBAR",
		"sip:3402000000@3402000000",
		[]model.Header{
			model.NewHeader("From", "<sip:34020000011320000007@3402000000>;tag=z"),
			model.NewHeader("To", "<sip:34020000002000000001@3402000000>"),
			model.NewHeader("Call-ID", "call-foobar-canned"),
			model.NewHeader("CSeq", "1 FOOBAR"),
			model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-z"),
		},
		"",
	)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp := send(t, tr, req)
	if resp.StatusCode() != 403 {
		t.Fatalf("status = %d, want 403 (canned overrides UnsupportedMethod)", resp.StatusCode())
	}
	counters := fs.FaultCounters(mustPlatformNode(t))
	if counters[model.FaultCannedResponse] != 1 {
		t.Fatalf("canned_response counter = %v, want 1", counters)
	}
}
