package app

// GB/T 28181 附录 D: RTP over TCP, both directions of the connection.

import (
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

func tcpRTPOffer(setup string, port int) offerTransport {
	return offerTransport{Proto: "TCP/RTP/AVP", Setup: setup}
}

// The platform listens (a=setup:passive in its offer); the device connects
// and frames every packet with a two-byte length prefix.
func TestMediaSink_TCPActiveConnectsAndFrames(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	port := ln.Addr().(*net.TCPAddr).Port

	sink, err := openMediaSink(tcpRTPOffer("passive", port), "127.0.0.1", port)
	if err != nil {
		t.Fatalf("openMediaSink: %v", err)
	}
	defer func() { _ = sink.Close() }()

	gotPort, proto, setup := sink.Answer()
	if gotPort != port || proto != "TCP/RTP/AVP" || setup != "active" {
		t.Fatalf("answer = (%d,%q,%q), want (%d,TCP/RTP/AVP,active)", gotPort, proto, setup, port)
	}

	conn, err := ln.Accept()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	defer func() { _ = conn.Close() }()

	packet := []byte{0x80, 0x60, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00}
	if err := sink.Write(packet); err != nil {
		t.Fatalf("Write: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var prefix [2]byte
	if _, err := io.ReadFull(conn, prefix[:]); err != nil {
		t.Fatalf("read prefix: %v", err)
	}
	if n := binary.BigEndian.Uint16(prefix[:]); int(n) != len(packet) {
		t.Fatalf("length prefix = %d, want %d", n, len(packet))
	}
	body := make([]byte, len(packet))
	if _, err := io.ReadFull(conn, body); err != nil {
		t.Fatalf("read packet: %v", err)
	}
	if body[0] != 0x80 {
		t.Errorf("payload starts with %#x, want an RTP v2 header", body[0])
	}
}

// The platform connects to us (a=setup:active in its offer); the answer
// advertises the port we listen on and a=setup:passive.
func TestMediaSink_TCPPassiveAcceptsAndFrames(t *testing.T) {
	sink, err := openMediaSink(tcpRTPOffer("active", 0), "127.0.0.1", 0)
	if err != nil {
		t.Fatalf("openMediaSink: %v", err)
	}
	defer func() { _ = sink.Close() }()

	port, proto, setup := sink.Answer()
	if port <= 0 || proto != "TCP/RTP/AVP" || setup != "passive" {
		t.Fatalf("answer = (%d,%q,%q), want (a port,TCP/RTP/AVP,passive)", port, proto, setup)
	}

	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", itoa(port)), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	packet := []byte{0x80, 0x60, 0x00, 0x02}
	done := make(chan error, 1)
	go func() { done <- sink.Write(packet) }()

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var prefix [2]byte
	if _, err := io.ReadFull(conn, prefix[:]); err != nil {
		t.Fatalf("read prefix: %v", err)
	}
	if n := binary.BigEndian.Uint16(prefix[:]); int(n) != len(packet) {
		t.Fatalf("length prefix = %d, want %d", n, len(packet))
	}
	body := make([]byte, len(packet))
	if _, err := io.ReadFull(conn, body); err != nil {
		t.Fatalf("read packet: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Write: %v", err)
	}
}

// An offer that names TCP but leaves us nothing to connect to cannot be
// served: the caller answers 488 instead of falling back to UDP.
func TestMediaSink_TCPWithoutEndpointFails(t *testing.T) {
	if _, err := openMediaSink(tcpRTPOffer("passive", 0), "127.0.0.1", 0); err == nil {
		t.Fatal("openMediaSink accepted a TCP offer without a port")
	}
}

// The pre-附录 D shape stays on UDP.
func TestMediaSink_DefaultIsUDP(t *testing.T) {
	sink, err := openMediaSink(offerTransport{Proto: "RTP/AVP"}, "127.0.0.1", 40000)
	if err != nil {
		t.Fatalf("openMediaSink: %v", err)
	}
	defer func() { _ = sink.Close() }()
	if _, proto, setup := sink.Answer(); proto != "RTP/AVP" || setup != "" {
		t.Errorf("answer proto=%q setup=%q, want RTP/AVP with no a=setup", proto, setup)
	}
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
