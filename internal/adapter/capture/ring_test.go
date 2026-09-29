package capture

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/gopacket/pcapgo"

	"github.com/your-org/gb28181-simulator/internal/adapter/audit"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// fixedClock returns the same instant for every Now call.
type fixedClock struct{ t time.Time }

func (f fixedClock) Now() time.Time { return f.t }

// newStore is a shorthand used throughout these tests.
func newStore(t *testing.T, cap int) port.CaptureStore {
	t.Helper()
	if cap <= 0 {
		cap = 8
	}
	return NewWithCapacity(cap, nil)
}

// evt is a test helper that builds a port.CaptureEvent with the given
// direction string and payload bytes, stamping each event at a distinct
// second so that ordering is unambiguous.
func evt(nodeID, direction string, payload []byte, seq int) port.CaptureEvent {
	return port.CaptureEvent{
		NodeID:    nodeID,
		Direction: model.Direction(direction),
		Local:     "127.0.0.1:5060",
		Remote:    "127.0.0.1:6060",
		Transport: "udp",
		Bytes:     append([]byte(nil), payload...),
		At:        time.Unix(int64(seq), 0),
	}
}

// --- Task 4.1: Append / Query / Capacity / Eviction / Per-node isolation ---

func TestRing_AppendQueryOldestFirst(t *testing.T) {
	s := newStore(t, 4)
	store := s.(*store)

	for i := 0; i < 4; i++ {
		store.Append("n1", evt("n1", "t", []byte{byte(i)}, i))
	}

	got := s.Query("n1", 10)
	if len(got) != 4 {
		t.Fatalf("Query len = %d, want 4", len(got))
	}
	for i, e := range got {
		if e.Bytes[0] != byte(i) {
			t.Errorf("Query[%d] = %d, want %d", i, e.Bytes[0], i)
		}
	}
}

func TestRing_QueryLimit(t *testing.T) {
	s := newStore(t, 8)
	store := s.(*store)

	for i := 0; i < 8; i++ {
		store.Append("n1", evt("n1", "t", []byte{byte(i)}, i))
	}

	got := s.Query("n1", 3)
	if len(got) != 3 {
		t.Fatalf("Query(limit=3) len = %d, want 3", len(got))
	}
	if !bytes.Equal(got[0].Bytes, []byte{0}) {
		t.Errorf("first = %v, want [0]", got[0].Bytes)
	}
	if !bytes.Equal(got[2].Bytes, []byte{2}) {
		t.Errorf("third = %v, want [2]", got[2].Bytes)
	}
}

func TestRing_EvictionOldestFirst(t *testing.T) {
	s := newStore(t, 4)
	store := s.(*store)

	for i := 0; i < 8; i++ {
		store.Append("n1", evt("n1", "t", []byte{byte(i)}, i))
	}

	// cap=4, 8 appends: first 4 fill ring, set full=true; remaining 4 are
	// silently dropped. Only [0,1,2,3] remain.
	got := s.Query("n1", 10)
	if len(got) != 4 {
		t.Fatalf("after overflow Query len = %d, want 4", len(got))
	}
	want := [][]byte{{0}, {1}, {2}, {3}}
	for i, g := range got {
		if !bytes.Equal(g.Bytes, want[i]) {
			t.Errorf("[%d] = %v, want %v", i, g.Bytes, want[i])
		}
	}
}

func TestRing_PerNodeIsolation(t *testing.T) {
	s := newStore(t, 4)

	s.Append("a", evt("a", "t", []byte("from-a"), 0))
	s.Append("b", evt("b", "r", []byte("from-b"), 0))

	ga := s.Query("a", 10)
	if len(ga) != 1 || !bytes.Equal(ga[0].Bytes, []byte("from-a")) {
		t.Errorf("node a leaked b's event or missing own: %v", ga)
	}
	gb := s.Query("b", 10)
	if len(gb) != 1 || !bytes.Equal(gb[0].Bytes, []byte("from-b")) {
		t.Errorf("node b leaked a's event or missing own: %v", gb)
	}
}

func TestRing_ConcurrentAppendNonBlocking(t *testing.T) {
	s := newStore(t, 4)
	store := s.(*store)
	n := 256
	done := make(chan struct{})

	go func() {
		defer close(done)
		for i := 0; i < n; i++ {
			store.Append("n1", evt("n1", "t", []byte{byte(i)}, i))
		}
	}()

	<-done
	// The ring should not deadlock; just verify it's readable.
	got := s.Query("n1", 10)
	if len(got) == 0 {
		t.Error("concurrent append left ring empty — possible drop of all events")
	}
}

// --- Task 4.2: Subscribe fan-out / slow-drop / cancel ---

func TestRing_SubscribeFanOut(t *testing.T) {
	s := newStore(t, 8)

	// Subscribe BEFORE appending so live fan-out delivers both events.
	ch1, cancel1 := s.Subscribe("n1")
	ch2, cancel2 := s.Subscribe("n1")

	s.Append("n1", evt("n1", "t", []byte("e1"), 1))
	s.Append("n1", evt("n1", "r", []byte("e2"), 2))

	// Close channels to unblock consumers.
	cancel1()
	cancel2()

	var got1, got2 []port.CaptureEvent
	for e := range ch1 {
		got1 = append(got1, e)
	}
	for e := range ch2 {
		got2 = append(got2, e)
	}

	if len(got1) != 2 {
		t.Errorf("sub1 received %d events, want 2", len(got1))
	}
	if len(got2) != 2 {
		t.Errorf("sub2 received %d events, want 2", len(got2))
	}
}

func TestRing_SubscribeSlowDropped(t *testing.T) {
	// capacity=2, subscriber channel buffer=2 (subBufs entry), third
	// event should be dropped for the slow subscriber.
	s := newStore(t, 2)
	store := s.(*store)
	_ = store

	for i := 0; i < 3; i++ {
		s.Append("n1", evt("n1", "t", []byte{byte(i)}, i))
	}

	ch, cancel := s.Subscribe("n1")
	defer cancel()

	// Drain whatever arrived.
	var got []port.CaptureEvent
	timeout := time.After(200 * time.Millisecond)
collect:
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				break collect
			}
			got = append(got, e)
		case <-timeout:
			break collect
		}
	}

	// The ring buffer itself holds at most cap=2 entries. The subscriber
	// may receive up to 2 of them; the third event was dropped on write.
	if len(got) > 2 {
		t.Errorf("slow subscriber received %d events (> cap=2)", len(got))
	}
}

func TestRing_SubscribeCancelClosesChannel(t *testing.T) {
	s := newStore(t, 4)

	ch, cancel := s.Subscribe("n1")

	// Cancel before any append; the channel must be closed.
	cancel()

	_, ok := <-ch
	if ok {
		t.Error("channel was not closed after cancel")
	}
}

// --- Task 4.3: PCAP round-trip ---

func TestRing_PCAPRoundTrip(t *testing.T) {
	s := newStore(t, 8)
	_ = s

	payload1 := []byte("INVITE sip:bob@127.0.0.1 SIP/2.0\r\n")
	payload2 := []byte("SIP/2.0 200 OK\r\n")

	s.Append("n1", evt("n1", "t", payload1, 100))
	s.Append("n1", evt("n1", "r", payload2, 101))

	raw, err := s.PCAP("n1")
	if err != nil {
		t.Fatalf("PCAP error: %v", err)
	}

	r := bytes.NewReader(raw)
	pr, err := pcapgo.NewReader(r)
	if err != nil {
		t.Fatalf("pcapgo.NewReader: %v", err)
	}

	var recovered [][]byte
	for {
		data, _, err := pr.ReadPacketData()
		if err != nil {
			break
		}
		recovered = append(recovered, data)
	}

	if len(recovered) != 2 {
		t.Fatalf("packets in pcap = %d, want 2", len(recovered))
	}
	if !bytes.Contains(recovered[0], payload1) {
		t.Error("first packet payload does not contain INVITE body")
	}
	if !bytes.Contains(recovered[1], payload2) {
		t.Error("second packet payload does not contain 200 OK body")
	}
}

func TestRing_PCAPEmptyNode(t *testing.T) {
	s := newStore(t, 8)

	raw, err := s.PCAP("nonexistent")
	if err != nil {
		t.Fatalf("PCAP on empty node errored: %v", err)
	}
	if raw == nil {
		// nil is acceptable (no header written); anything non-nil must parse.
		return
	}

	r := bytes.NewReader(raw)
	_, err = pcapgo.NewReader(r)
	if err != nil {
		t.Fatalf("empty-node pcap not parseable: %v", err)
	}
}

func TestRing_PCAPPreservesTimestamp(t *testing.T) {
	s := newStore(t, 4)

	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	s.Append("n1", port.CaptureEvent{
		NodeID:    "n1",
		Direction: model.DirTransmit,
		Bytes:     []byte("data"),
		At:        ts,
		Local:     "127.0.0.1:5060",
		Remote:    "127.0.0.1:6060",
		Transport: "udp",
	})

	raw, err := s.PCAP("n1")
	if err != nil {
		t.Fatalf("PCAP: %v", err)
	}

	pr, err := pcapgo.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("pcapgo.NewReader: %v", err)
	}

	_, ci, err := pr.ReadPacketData()
	if err != nil {
		t.Fatalf("ReadPacketData: %v", err)
	}
	if !ci.Timestamp.Equal(ts) {
		t.Errorf("pcap timestamp = %v, want %v", ci.Timestamp, ts)
	}
}

// --- NodeID population (backwards-compat) ---

func TestRing_NodeIDBackfill(t *testing.T) {
	s := newStore(t, 4)
	store := s.(*store)
	_ = store

	// Event NodeID left empty — the ring must backfill from the key.
	s.Append("n42", port.CaptureEvent{
		Direction: model.DirTransmit,
		Bytes:     []byte("hi"),
		Local:     "127.0.0.1:5060",
		Remote:    "127.0.0.1:6060",
		Transport: "udp",
		At:        time.Now(),
	})

	got := s.Query("n42", 1)
	if len(got) != 1 {
		t.Fatal("no event after backfill Append")
	}
	if got[0].NodeID != "n42" {
		t.Errorf("event NodeID = %q, want %q", got[0].NodeID, "n42")
	}
}

// --- Task 4.4: audit bridge wiring (unit-level e2e) ---

func TestAuditBridge_ConsumesWireEventsIntoStore(t *testing.T) {
	s := newStore(t, 8)
	emitter := AuditBridge(s)

	before := len(s.Query("n9", 10))
	if before != 0 {
		t.Fatalf("store not empty before emit: %d events", before)
	}

	ts := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	emitter.Emit(audit.WireEvent{
		Direction: audit.DirTransmit,
		Local:     "127.0.0.1:5060",
		Remote:    "192.168.1.100:5060",
		Bytes:     []byte("REGISTER sip:34020000002000000001@3402000000 SIP/2.0\r\n"),
		Timestamp: ts,
		NodeID:    "n9",
	})

	got := s.Query("n9", 10)
	if len(got) != 1 {
		t.Fatalf("Query after emit = %d events, want 1", len(got))
	}
	e := got[0]
	if e.NodeID != "n9" {
		t.Errorf("NodeID = %q, want n9", e.NodeID)
	}
	if e.Direction != model.DirTransmit {
		t.Errorf("Direction = %q, want %q", e.Direction, model.DirTransmit)
	}
	if !bytes.Contains(e.Bytes, []byte("REGISTER")) {
		t.Error("Bytes lost the SIP payload")
	}
	if !e.At.Equal(ts) {
		t.Errorf("At = %v, want %v", e.At, ts)
	}
	if e.Local != "127.0.0.1:5060" || e.Remote != "192.168.1.100:5060" {
		t.Errorf("endpoints = %q -> %q, want preserved", e.Local, e.Remote)
	}
}

func TestAuditBridge_GlobalEmitterRoutesToStore(t *testing.T) {
	s := newStore(t, 4)
	prev := audit.Global()
	t.Cleanup(func() { audit.SetEmitter(prev) })

	audit.SetEmitter(AuditBridge(s))
	audit.Global().Emit(audit.WireEvent{
		Direction: audit.DirReceive,
		Local:     "127.0.0.1:5060",
		Bytes:     []byte("SIP/2.0 200 OK\r\n"),
		Timestamp: time.Now(),
		NodeID:    "n7",
	})

	got := s.Query("n7", 10)
	if len(got) != 1 {
		t.Fatalf("global emitter did not route into store: %d events", len(got))
	}
	if got[0].Direction != model.DirReceive {
		t.Errorf("Direction = %q, want %q", got[0].Direction, model.DirReceive)
	}
}

func TestAuditBridge_DistinctStoresIndependent(t *testing.T) {
	a := newStore(t, 4)
	b := newStore(t, 4)

	eb := AuditBridge(b)
	eb.Emit(audit.WireEvent{NodeID: "x", Direction: audit.DirTransmit, Bytes: []byte("only-b"), Timestamp: time.Now()})

	if got := a.Query("x", 10); len(got) != 0 {
		t.Errorf("bridge(b) leaked into a: %d events", len(got))
	}
	if got := b.Query("x", 10); len(got) != 1 {
		t.Errorf("bridge(b) lost its own event: %d events", len(got))
	}
}
