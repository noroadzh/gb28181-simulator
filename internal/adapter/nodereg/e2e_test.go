package nodereg_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/adapter/nodereg"
	"github.com/your-org/gb28181-simulator/internal/adapter/siptransport"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// realFactory binds a genuine UDP listener through the siptransport
// adapter, so this test exercises the real socket path rather than a stub.
func realFactory(addr string) (port.SIPTransport, error) {
	tr, err := siptransport.New("udp://" + addr)
	if err != nil {
		return nil, err
	}
	return siptransport.NewPortAdapter(tr), nil
}

// freeUDPPort reserves a loopback port and releases it so a node can bind
// it; Transport.LocalAddr() echoes the bind string, so ":0" is unusable.
func freeUDPPort(t *testing.T) string {
	t.Helper()
	l, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe free port: %v", err)
	}
	addr := l.LocalAddr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close probe socket: %v", err)
	}
	return addr
}

// TestRegistry_TwoNodesRealTransports is the end-to-end check for task 11.2:
// two nodes registered in one process, each starting its own real listener
// without disturbing the other.
func TestRegistry_TwoNodesRealTransports(t *testing.T) {
	ctx := context.Background()
	reg := nodereg.New()
	lc := nodereg.NewLifecycle(reg, realFactory)

	a, err := reg.Register(ctx, mustProfile(t, idA, freeUDPPort(t)))
	if err != nil {
		t.Fatalf("Register A: %v", err)
	}
	b, err := reg.Register(ctx, mustProfile(t, idB, freeUDPPort(t)))
	if err != nil {
		t.Fatalf("Register B: %v", err)
	}

	if err := lc.Start(ctx, a.ID()); err != nil {
		t.Fatalf("Start A: %v", err)
	}
	if err := lc.Start(ctx, b.ID()); err != nil {
		t.Fatalf("Start B: %v", err)
	}

	for _, id := range []model.NodeID{a.ID(), b.ID()} {
		st, err := lc.Status(ctx, id)
		if err != nil {
			t.Fatalf("Status %s: %v", id, err)
		}
		if st != model.StatusRegistering {
			t.Errorf("node %s status = %v, want registering", id, st)
		}
		if lc.Transport(id) == nil {
			t.Errorf("node %s has no listener", id)
		}
	}

	// Each node owns a distinct listener: stopping one leaves the other up.
	addrA := a.Profile().Addr()
	addrB := b.Profile().Addr()
	if addrA == addrB {
		t.Fatalf("both nodes bound %s; addresses must differ", addrA)
	}
	if err := lc.Stop(ctx, a.ID()); err != nil {
		t.Fatalf("Stop A: %v", err)
	}
	if st, _ := lc.Status(ctx, b.ID()); st != model.StatusRegistering {
		t.Errorf("B status after stopping A = %v, want registering", st)
	}
	if lc.Transport(b.ID()) == nil {
		t.Error("B lost its listener when A stopped")
	}

	// Stopping releases the socket but keeps the address claim: the node is
	// still registered and may be restarted, so nobody else may steal its
	// port in the meantime (design D3).
	if _, err := reg.Register(ctx, mustProfile(t, idReuse, addrA)); err == nil {
		t.Fatalf("registering %s while node %s is merely stopped succeeded, want error", addrA, a.ID())
	}

	// Restarting the same node on the same address is the common path
	// (Offline -> Registering) and must work.
	if err := lc.Start(ctx, a.ID()); err != nil {
		t.Fatalf("restart A on %s: %v", addrA, err)
	}
	if st, _ := lc.Status(ctx, a.ID()); st != model.StatusRegistering {
		t.Errorf("A status after restart = %v, want registering", st)
	}
}

// TestRegistry_TwoNodesExchangeWithoutCrosstalk asserts two running nodes
// can each send and receive on their own listener without the messages
// landing on the wrong node (task 11.2).
func TestRegistry_TwoNodesExchangeWithoutCrosstalk(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	reg := nodereg.New()
	lc := nodereg.NewLifecycle(reg, realFactory)

	a, err := reg.Register(ctx, mustProfile(t, idA, freeUDPPort(t)))
	if err != nil {
		t.Fatalf("Register A: %v", err)
	}
	b, err := reg.Register(ctx, mustProfile(t, idB, freeUDPPort(t)))
	if err != nil {
		t.Fatalf("Register B: %v", err)
	}
	if err := lc.Start(ctx, a.ID()); err != nil {
		t.Fatalf("Start A: %v", err)
	}
	if err := lc.Start(ctx, b.ID()); err != nil {
		t.Fatalf("Start B: %v", err)
	}

	trA := lc.Transport(a.ID())
	trB := lc.Transport(b.ID())
	if trA == nil || trB == nil {
		t.Fatal("a running node has no transport")
	}

	// A sends an INVITE to B's address.
	invite, err := model.NewRequest("INVITE",
		"sip:34020000001320000001@"+b.Profile().Addr(),
		[]model.Header{model.NewHeader("CSeq", "1 INVITE")}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := trA.Send(ctx, invite, b.Profile().Addr()); err != nil {
		t.Fatalf("A send: %v", err)
	}

	got, peer, err := trB.Receive(ctx)
	if err != nil {
		t.Fatalf("B receive: %v", err)
	}
	if got.Method() != "INVITE" {
		t.Errorf("B got method %q, want INVITE", got.Method())
	}
	if peer != a.Profile().Addr() {
		t.Errorf("B reported peer %q, want A's address %q", peer, a.Profile().Addr())
	}

	// The return direction proves neither node swallows the other's
	// traffic. A response is used on purpose: it carries caller-supplied
	// headers (CSeq / Call-ID / From / To) that must survive the trip
	// through the transport's message conversion. It is the regression
	// guard for Change 5's header-preservation fix — the conversion used
	// to drop every header the caller had set.
	reply, err := model.NewResponse(200, "OK", []model.Header{
		model.NewHeader("Via", "SIP/2.0/UDP "+b.Profile().Addr()+";branch=z9hG4bK-e2e-reply"),
		model.NewHeader("CSeq", "1 INVITE"),
		model.NewHeader("Call-ID", "e2e-crosstalk-callid"),
		model.NewHeader("From", "<sip:"+idB+"@"+b.Profile().Addr()+">;tag=e2e-b"),
		model.NewHeader("To", "<sip:"+idA+"@"+a.Profile().Addr()+">;tag=e2e-a"),
	}, "")
	if err != nil {
		t.Fatalf("NewResponse reply: %v", err)
	}
	if err := trB.Send(ctx, reply, peer); err != nil {
		t.Fatalf("B send: %v", err)
	}
	answer, answerPeer, err := trA.Receive(ctx)
	if err != nil {
		t.Fatalf("A receive: %v", err)
	}
	if answer.StatusCode() != 200 {
		t.Errorf("A got status %d, want 200", answer.StatusCode())
	}
	if h, ok := answer.Header("Call-ID"); !ok || h.Value() != "e2e-crosstalk-callid" {
		t.Errorf("A got Call-ID %q (present=%v), want e2e-crosstalk-callid", h.Value(), ok)
	}
	if h, ok := answer.Header("CSeq"); !ok || h.Value() != "1 INVITE" {
		t.Errorf("A got CSeq %q (present=%v), want \"1 INVITE\"", h.Value(), ok)
	}
	if answerPeer != b.Profile().Addr() {
		t.Errorf("A reported peer %q, want B's address %q", answerPeer, b.Profile().Addr())
	}
}

// idReuse is a third legal device id used to prove an address can be
// reclaimed after its previous owner stopped.
const idReuse = "34020000011310000003"
