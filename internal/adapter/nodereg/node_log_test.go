package nodereg_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/adapter/nodereg"
	"github.com/your-org/gb28181-simulator/internal/adapter/siptransport"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// Two legal device ids differing only in their sequence number, used to prove
// that each node's log records carry — and only carry — its own id.
const (
	logNodeA = "34020000011310000001"
	logNodeB = "34020000011310000002"
)

// TestRegistry_LogFieldsAreKeyedByNodeID covers the spec clause that every
// node owns independent log fields keyed by its node id: registering,
// starting, stopping and unregistering one node must never emit a record that
// is missing its id, nor one that mentions another node's id.
func TestRegistry_LogFieldsAreKeyedByNodeID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var buf bytes.Buffer
	reg := nodereg.NewWithLogger(slog.New(slog.NewTextHandler(&buf, nil)))
	lc := nodereg.NewLifecycle(reg, logTestFactory)

	profileA, err := model.NewNodeProfile(logNodeA, freeLogAddr(t), "3402000000", "test-vendor")
	if err != nil {
		t.Fatalf("NewNodeProfile A: %v", err)
	}
	profileB, err := model.NewNodeProfile(logNodeB, freeLogAddr(t), "3402000000", "test-vendor")
	if err != nil {
		t.Fatalf("NewNodeProfile B: %v", err)
	}

	a, err := reg.Register(ctx, profileA)
	if err != nil {
		t.Fatalf("Register A: %v", err)
	}
	assertOnlyNodeID(t, buf.String(), logNodeA, logNodeB)
	buf.Reset()

	b, err := reg.Register(ctx, profileB)
	if err != nil {
		t.Fatalf("Register B: %v", err)
	}
	assertOnlyNodeID(t, buf.String(), logNodeB, logNodeA)
	buf.Reset()

	if err := lc.Start(ctx, a.ID()); err != nil {
		t.Fatalf("Start A: %v", err)
	}
	assertOnlyNodeID(t, buf.String(), logNodeA, logNodeB)
	if !strings.Contains(buf.String(), profileA.Addr()) {
		t.Errorf("start log for node %s omits its addr %s: %s", logNodeA, profileA.Addr(), buf.String())
	}
	buf.Reset()

	// A second Start is an illegal transition (registering -> registering);
	// the rejection must be recorded against the same node id.
	if err := lc.Start(ctx, a.ID()); !errors.Is(err, model.ErrIllegalTransition) {
		t.Fatalf("second Start A: error = %v, want ErrIllegalTransition", err)
	}
	if !strings.Contains(buf.String(), "transition rejected") {
		t.Errorf("illegal transition was not logged: %s", buf.String())
	}
	assertOnlyNodeID(t, buf.String(), logNodeA, logNodeB)
	buf.Reset()

	if err := lc.Stop(ctx, a.ID()); err != nil {
		t.Fatalf("Stop A: %v", err)
	}
	if !strings.Contains(buf.String(), "listener released") {
		t.Errorf("stop log for node %s is missing the release record: %s", logNodeA, buf.String())
	}
	assertOnlyNodeID(t, buf.String(), logNodeA, logNodeB)
	buf.Reset()

	if err := reg.Unregister(ctx, a.ID()); err != nil {
		t.Fatalf("Unregister A: %v", err)
	}
	assertOnlyNodeID(t, buf.String(), logNodeA, logNodeB)
	buf.Reset()

	// B was never touched by any of the above and stays usable.
	if err := lc.Start(ctx, b.ID()); err != nil {
		t.Fatalf("Start B: %v", err)
	}
	assertOnlyNodeID(t, buf.String(), logNodeB, logNodeA)
}

// assertOnlyNodeID fails unless out contains at least one record and every
// record in it is keyed by want, with other never appearing.
func assertOnlyNodeID(t *testing.T, out, want, other string) {
	t.Helper()
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		t.Fatalf("no log output, want at least one record for node %s", want)
	}
	for _, line := range strings.Split(trimmed, "\n") {
		if !strings.Contains(line, "node_id="+want) {
			t.Errorf("record for node %s is missing node_id: %s", want, line)
		}
		if strings.Contains(line, other) {
			t.Errorf("record for node %s mentions node %s: %s", want, other, line)
		}
	}
}

// logTestFactory binds a real UDP listener so Start exercises the same path
// production does.
func logTestFactory(addr string, _ model.NodeID) (port.SIPTransport, error) {
	tr, err := siptransport.New(addr)
	if err != nil {
		return nil, err
	}
	return siptransport.NewPortAdapter(tr), nil
}

// freeLogAddr returns a currently unused 127.0.0.1 UDP address.
func freeLogAddr(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("grab a free UDP port: %v", err)
	}
	defer pc.Close()
	return pc.LocalAddr().String()
}
