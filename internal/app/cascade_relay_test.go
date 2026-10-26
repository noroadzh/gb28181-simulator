package app

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// relayTestNode is the node whose hand-offs the table tests exercise.
func relayTestNode(t *testing.T) model.NodeID {
	t.Helper()
	id, err := model.ParseNodeID("34020000002000000001")
	if err != nil {
		t.Fatalf("ParseNodeID: %v", err)
	}
	return id
}

func relayRequest(t *testing.T, callID, cseq string) model.Message {
	t.Helper()
	msg, err := model.NewRequest("MESSAGE", "sip:34020000001310000001@127.0.0.1:5060",
		[]model.Header{
			model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:5060;branch=z9hG4bK-probe"),
			model.NewHeader("From", "<sip:34020000002000000001@127.0.0.1>;tag=probe"),
			model.NewHeader("To", "<sip:34020000001310000001@127.0.0.1>;tag=probe-to"),
			model.NewHeader("Call-ID", callID),
			model.NewHeader("CSeq", cseq),
		}, "<Query/>")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return msg
}

func relayResponse(t *testing.T, callID, cseq string) model.Message {
	t.Helper()
	msg, err := model.NewResponse(200, "OK",
		[]model.Header{
			model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:5061;branch=z9hG4bK-relay"),
			model.NewHeader("Call-ID", callID),
			model.NewHeader("CSeq", cseq),
		}, "<Response/>")
	if err != nil {
		t.Fatalf("NewResponse: %v", err)
	}
	return msg
}

func TestRelayTable_PutThenTake(t *testing.T) {
	table := newRelayTable(nil)
	if _, ok := table.put(relayTestNode(t), relayRequest(t, "call-1", "7 MESSAGE"), "127.0.0.1:15060", time.Second); !ok {
		t.Fatal("put refused a fully identified request")
	}
	if got := table.size(); got != 1 {
		t.Fatalf("size = %d, want 1", got)
	}

	entry, ok := table.take(relayTestNode(t), relayResponse(t, "call-1", "7 MESSAGE"))
	if !ok {
		t.Fatal("take did not match the response to its request")
	}
	if entry.peer != "127.0.0.1:15060" {
		t.Errorf("peer = %q, want the original requester", entry.peer)
	}
	if table.size() != 0 {
		t.Errorf("size = %d after take, want 0", table.size())
	}

	// A retransmitted answer finds nothing to release.
	if _, ok := table.take(relayTestNode(t), relayResponse(t, "call-1", "7 MESSAGE")); ok {
		t.Error("a second response for the same transaction was accepted")
	}
}

func TestRelayTable_RejectsUnidentifiedMessages(t *testing.T) {
	table := newRelayTable(nil)
	noCallID, err := model.NewRequest("MESSAGE", "sip:x@127.0.0.1",
		[]model.Header{model.NewHeader("CSeq", "1 MESSAGE")}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if _, ok := table.put(relayTestNode(t), noCallID, "127.0.0.1:15060", time.Second); ok {
		t.Error("put accepted a request without Call-ID")
	}

	if _, ok := table.put(relayTestNode(t), relayRequest(t, "call-2", "1 MESSAGE"), "127.0.0.1:15060", time.Second); !ok {
		t.Fatal("put refused a valid request")
	}
	noCSeq, err := model.NewResponse(200, "OK",
		[]model.Header{model.NewHeader("Call-ID", "call-2")}, "")
	if err != nil {
		t.Fatalf("NewResponse: %v", err)
	}
	if _, ok := table.take(relayTestNode(t), noCSeq); ok {
		t.Error("take matched a response without CSeq")
	}
	if table.size() != 1 {
		t.Errorf("size = %d, want the unmatched entry to stay", table.size())
	}
}

func TestRelayTable_TakeKeyRemovesOnce(t *testing.T) {
	table := newRelayTable(nil)
	key, ok := table.put(relayTestNode(t), relayRequest(t, "call-3", "2 MESSAGE"), "127.0.0.1:15060", time.Second)
	if !ok {
		t.Fatal("put refused a valid request")
	}

	entry, ok := table.takeKey(key)
	if !ok {
		t.Fatal("takeKey did not find the entry")
	}
	if !strings.Contains(entry.request.Body(), "Query") {
		t.Errorf("stored request body = %q, want the original body", entry.request.Body())
	}
	// The timeout goroutine racing a late answer: whoever comes second loses.
	if _, ok := table.takeKey(key); ok {
		t.Error("takeKey removed the same entry twice")
	}
	if table.size() != 0 {
		t.Errorf("size = %d, want 0", table.size())
	}
}

func TestRelayTable_SecondPutReplacesWaiter(t *testing.T) {
	table := newRelayTable(nil)
	req := relayRequest(t, "call-4", "3 MESSAGE")
	if _, ok := table.put(relayTestNode(t), req, "127.0.0.1:15060", time.Second); !ok {
		t.Fatal("first put failed")
	}
	if _, ok := table.put(relayTestNode(t), req, "127.0.0.1:15061", time.Second); !ok {
		t.Fatal("second put failed")
	}
	if got := table.size(); got != 1 {
		t.Fatalf("size = %d, want 1 (same transaction)", got)
	}
	entry, ok := table.take(relayTestNode(t), relayResponse(t, "call-4", "3 MESSAGE"))
	if !ok {
		t.Fatal("take failed")
	}
	if entry.peer != "127.0.0.1:15061" {
		t.Errorf("peer = %q, want the newest waiter", entry.peer)
	}
}

// renderWire renders a message the way the SIP adapter will put it on the
// wire. The header block is what this package contributes, so a golden string
// over it pins the bytes that matter for relaying.
func renderWire(msg model.Message) string {
	var b strings.Builder
	if msg.IsRequest() {
		b.WriteString(msg.Method() + " " + msg.URI().String() + " SIP/2.0\r\n")
	} else {
		b.WriteString("SIP/2.0 " + strconv.Itoa(msg.StatusCode()) + " " + msg.StatusText() + "\r\n")
	}
	for _, h := range msg.Headers() {
		b.WriteString(h.String() + "\r\n")
	}
	b.WriteString("\r\n")
	b.WriteString(msg.Body())
	return b.String()
}

// Task 1.2: byte-level golden for the forwarded request.
func TestForwardRequest_GoldenWire(t *testing.T) {
	req := relayRequest(t, "call-9", "4 MESSAGE")

	out, err := forwardRequest(req,
		[]model.Header{model.NewHeader("X-RoutePath", "34020000002000000001")})
	if err != nil {
		t.Fatalf("forwardRequest: %v", err)
	}

	want := "MESSAGE sip:34020000001310000001@127.0.0.1:5060 SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 127.0.0.1:5060;branch=z9hG4bK-probe\r\n" +
		"From: <sip:34020000002000000001@127.0.0.1>;tag=probe\r\n" +
		"To: <sip:34020000001310000001@127.0.0.1>;tag=probe-to\r\n" +
		"Call-ID: call-9\r\n" +
		"CSeq: 4 MESSAGE\r\n" +
		"X-RoutePath: 34020000002000000001\r\n" +
		"\r\n" +
		"<Query/>"
	if got := renderWire(out); got != want {
		t.Fatalf("forwarded request wire mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestForwardRequest_RefusesResponse(t *testing.T) {
	if _, err := forwardRequest(relayResponse(t, "call-9", "4 MESSAGE"), nil); err == nil {
		t.Fatal("forwardRequest accepted a response")
	}
}

// Task 1.3: byte-level golden for the answer handed back to the requester.
// It is rebuilt from the stored request, so it carries the requester's own
// Via rather than whatever the downstream node sent back.
func TestRebuildRelayResponse_GoldenWire(t *testing.T) {
	entry := pendingRelay{peer: "127.0.0.1:15060", request: relayRequest(t, "call-9", "4 MESSAGE")}
	downstream, err := model.NewResponse(200, "OK", []model.Header{
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15062;branch=z9hG4bK-small"),
		model.NewHeader("From", "<sip:34020000002000000001@127.0.0.1>;tag=probe"),
		model.NewHeader("To", "<sip:34020000001310000001@127.0.0.1>;tag=device"),
		model.NewHeader("Call-ID", "call-9"),
		model.NewHeader("CSeq", "4 MESSAGE"),
		model.NewHeader("Content-Type", "Application/MANSCDP+XML"),
	}, "<Response/>")
	if err != nil {
		t.Fatalf("NewResponse: %v", err)
	}

	out, err := rebuildRelayResponse(entry, downstream)
	if err != nil {
		t.Fatalf("rebuildRelayResponse: %v", err)
	}

	want := "SIP/2.0 200 OK\r\n" +
		"Via: SIP/2.0/UDP 127.0.0.1:5060;branch=z9hG4bK-probe\r\n" +
		"From: <sip:34020000002000000001@127.0.0.1>;tag=probe\r\n" +
		"To: <sip:34020000001310000001@127.0.0.1>;tag=probe-to\r\n" +
		"Call-ID: call-9\r\n" +
		"CSeq: 4 MESSAGE\r\n" +
		"Content-Type: Application/MANSCDP+XML\r\n" +
		"\r\n" +
		"<Response/>"
	if got := renderWire(out); got != want {
		t.Fatalf("rebuilt response wire mismatch:\n got: %q\nwant: %q", got, want)
	}
}

// One Acceptor serves every node in the process, and a relayed request keeps
// its Call-ID/CSeq across hops. Two nodes handing off in the same chain must
// therefore hold independent entries — otherwise the second hop overwrites the
// first and the first hop can never match its answer.
func TestRelayTable_SameTransactionOnTwoNodesIsIndependent(t *testing.T) {
	table := newRelayTable(nil)
	first := relayTestNode(t)
	second := mustParse(t, "34020000002160000001")
	req := relayRequest(t, "shared-call", "1 MESSAGE")

	if _, ok := table.put(first, req, "127.0.0.1:15060", time.Second); !ok {
		t.Fatal("first put failed")
	}
	if _, ok := table.put(second, req, "127.0.0.1:15061", time.Second); !ok {
		t.Fatal("second put failed")
	}
	if got := table.size(); got != 2 {
		t.Fatalf("size = %d, want 2 (one per node)", got)
	}

	answer := relayResponse(t, "shared-call", "1 MESSAGE")
	entry, ok := table.take(first, answer)
	if !ok || entry.peer != "127.0.0.1:15060" {
		t.Fatalf("first node took %+v, ok=%v; want its own hand-off", entry, ok)
	}
	entry, ok = table.take(second, answer)
	if !ok || entry.peer != "127.0.0.1:15061" {
		t.Fatalf("second node took %+v, ok=%v; want its own hand-off", entry, ok)
	}
	if table.size() != 0 {
		t.Errorf("size = %d after both answers, want 0", table.size())
	}
}
