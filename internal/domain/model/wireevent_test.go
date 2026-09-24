package model

import (
	"net"
	"strings"
	"testing"
	"time"
)

// TestWireEvent_BuildAndRead asserts basic round-trip.
func TestWireEvent_BuildAndRead(t *testing.T) {
	at := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	peer := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 5060}
	we, err := NewWireEvent(at, DirTransmit, "udp", nil, peer, 512, "REGISTER sip:x SIP/2.0\r\n")
	if err != nil {
		t.Fatalf("NewWireEvent: %v", err)
	}
	if !we.At().Equal(at) {
		t.Errorf("At() = %v, want %v", we.At(), at)
	}
	if we.Direction() != DirTransmit {
		t.Errorf("Direction() = %q, want %q", we.Direction(), DirTransmit)
	}
	if we.Transport() != "udp" {
		t.Errorf("Transport() = %q, want udp", we.Transport())
	}
	if we.Size() != 512 {
		t.Errorf("Size() = %d, want 512", we.Size())
	}
	if !strings.Contains(we.Preview(), "REGISTER") {
		t.Errorf("Preview() missing REGISTER: %q", we.Preview())
	}
}

// TestWireEvent_EmptyTransportRejects asserts transport is mandatory.
func TestWireEvent_EmptyTransportRejects(t *testing.T) {
	if _, err := NewWireEvent(time.Now(), DirTransmit, "", nil, nil, 0, ""); err == nil {
		t.Fatal("empty transport accepted")
	}
}

// TestWireEvent_NegativeSizeRejects asserts constructor rejects negative size.
func TestWireEvent_NegativeSizeRejects(t *testing.T) {
	if _, err := NewWireEvent(time.Now(), DirTransmit, "udp", nil, nil, -1, ""); err == nil {
		t.Fatal("negative size accepted")
	}
}

// TestWireEvent_PreviewTruncatedAt256 asserts long previews are clipped.
func TestWireEvent_PreviewTruncatedAt256(t *testing.T) {
	long := strings.Repeat("A", 1000)
	we, err := NewWireEvent(time.Now(), DirReceive, "tcp", nil, nil, 1000, long)
	if err != nil {
		t.Fatalf("NewWireEvent: %v", err)
	}
	if len(we.Preview()) != 256 {
		t.Errorf("Preview length = %d, want 256", len(we.Preview()))
	}
}

// TestWireEvent_StringIncludesPeer asserts the debug summary contains the
// peer address.
func TestWireEvent_StringIncludesPeer(t *testing.T) {
	peer := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 5060}
	we, _ := NewWireEvent(time.Now(), DirReceive, "udp", nil, peer, 512, "x")
	if !strings.Contains(we.String(), "10.0.0.1:5060") {
		t.Errorf("String() missing peer: %q", we.String())
	}
}