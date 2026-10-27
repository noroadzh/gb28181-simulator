package app

// RTP media transport for the device side of a dialog.
//
// GB/T 28181 附录 D allows the media to travel over TCP instead of UDP. The
// SDP m-line carries the transport ("RTP/AVP" or "TCP/RTP/AVP") and the
// a=setup attribute says which side opens the connection:
//
//	a=setup:passive (or absent) — the peer listens; we connect and send
//	a=setup:active              — the peer connects to the port we listen on
//
// Over TCP every RTP packet is length-prefixed with two big-endian bytes
// (RFC 4571) — there are no datagram boundaries to lean on.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// tcpConnectTimeout bounds how long a dialog waits for its TCP peer before
// the stream ends. A platform that accepted the INVITE is already listening,
// so this only fires on a genuinely broken path.
const tcpConnectTimeout = 15 * time.Second

// mediaSink delivers RTP packets to one dialog's endpoint. The answer SDP is
// built from it, so the sink owns the transport details.
type mediaSink interface {
	// Write sends one RTP packet, framing it for the transport.
	Write(pkt []byte) error
	// Answer describes the transport for the SDP answer: the m= port, the
	// m= proto, and the a=setup value ("" for UDP).
	Answer() (port int, proto, setup string)
	Close() error
}

// udpSink is the default: RTP datagrams to a resolved endpoint.
type udpSink struct {
	conn net.PacketConn
	dst  *net.UDPAddr
	port int
}

func openUDPSink(host string, port int) (mediaSink, error) {
	dst, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("rtp: resolve %s:%d: %w", host, port, err)
	}
	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		return nil, fmt.Errorf("rtp: open udp port: %w", err)
	}
	return &udpSink{conn: conn, dst: dst, port: conn.LocalAddr().(*net.UDPAddr).Port}, nil
}

func (s *udpSink) Write(pkt []byte) error {
	_, err := s.conn.WriteTo(pkt, s.dst)
	return err
}

func (s *udpSink) Answer() (int, string, string) { return s.port, "RTP/AVP", "" }

func (s *udpSink) Close() error { return s.conn.Close() }

// tcpSink carries RTP over one TCP connection, either opened by us (active)
// or accepted from the peer (passive).
type tcpSink struct {
	// port is what the answer advertises: our listening port when passive,
	// the peer's port when active.
	port  int
	setup string

	mu        sync.Mutex
	conn      net.Conn
	listener  net.Listener
	acceptErr error
	ready     chan struct{}
	closeOnce sync.Once
	closed    chan struct{}
}

func openTCPActiveSink(host string, port int) (mediaSink, error) {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), tcpConnectTimeout)
	if err != nil {
		return nil, fmt.Errorf("rtp: connect %s:%d: %w", host, port, err)
	}
	return &tcpSink{port: port, setup: "active", conn: conn, closed: make(chan struct{})}, nil
}

func openTCPPassiveSink() (mediaSink, error) {
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return nil, fmt.Errorf("rtp: listen for tcp media: %w", err)
	}
	s := &tcpSink{
		port:     ln.Addr().(*net.TCPAddr).Port,
		setup:    "passive",
		listener: ln,
		ready:    make(chan struct{}),
		closed:   make(chan struct{}),
	}
	go s.accept(ln)
	return s, nil
}

// accept takes the single connection this dialog will use. Writes wait on
// ready, so the stream starts the moment the peer connects.
func (s *tcpSink) accept(ln net.Listener) {
	defer close(s.ready)
	type result struct {
		conn net.Conn
		err  error
	}
	got := make(chan result, 1)
	go func() {
		conn, err := ln.Accept()
		got <- result{conn: conn, err: err}
	}()
	select {
	case r := <-got:
		_ = ln.Close()
		if r.err != nil {
			s.mu.Lock()
			s.acceptErr = r.err
			s.mu.Unlock()
			return
		}
		s.mu.Lock()
		s.conn = r.conn
		s.mu.Unlock()
	case <-s.closed:
		_ = ln.Close()
		s.mu.Lock()
		s.acceptErr = net.ErrClosed
		s.mu.Unlock()
	}
}

func (s *tcpSink) Write(pkt []byte) error {
	if s.ready != nil {
		// Only the passive sink waits. The fast path is taken on every
		// packet once the peer is connected, so it must not allocate.
		select {
		case <-s.ready:
		default:
			timer := time.NewTimer(tcpConnectTimeout)
			defer timer.Stop()
			select {
			case <-s.ready:
			case <-timer.C:
				return fmt.Errorf("rtp: no tcp peer connected within %s", tcpConnectTimeout)
			case <-s.closed:
				return net.ErrClosed
			}
		}
	}
	s.mu.Lock()
	conn, aerr := s.conn, s.acceptErr
	s.mu.Unlock()
	if aerr != nil {
		return fmt.Errorf("rtp: tcp accept: %w", aerr)
	}
	if conn == nil {
		return errors.New("rtp: tcp sink is not connected")
	}
	var prefix [2]byte
	binary.BigEndian.PutUint16(prefix[:], uint16(len(pkt)))
	if _, err := conn.Write(prefix[:]); err != nil {
		return err
	}
	_, err := conn.Write(pkt)
	return err
}

func (s *tcpSink) Answer() (int, string, string) { return s.port, "TCP/RTP/AVP", s.setup }

func (s *tcpSink) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.closed)
		s.mu.Lock()
		conn := s.conn
		s.mu.Unlock()
		if s.listener != nil {
			_ = s.listener.Close()
		}
		if conn != nil {
			err = conn.Close()
		}
	})
	return err
}

// offerTransport is what an offer says about the media transport.
type offerTransport struct {
	Proto string // "RTP/AVP" (default) or "TCP/RTP/AVP"
	Setup string // a=setup: active | passive | actpass | ""
}

// openMediaSink builds the sender for an offer. UDP is the default; TCP
// follows 附录 D, where the passive peer listens and the active peer sends.
// An offer that asks us to be passive but gives no usable port cannot be
// served.
func openMediaSink(offer offerTransport, host string, port int) (mediaSink, error) {
	if !strings.EqualFold(strings.TrimSpace(offer.Proto), "TCP/RTP/AVP") {
		return openUDPSink(host, port)
	}
	if strings.EqualFold(strings.TrimSpace(offer.Setup), "active") {
		return openTCPPassiveSink()
	}
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("rtp: tcp offer without a usable port")
	}
	return openTCPActiveSink(host, port)
}
