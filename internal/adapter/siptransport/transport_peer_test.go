package siptransport

import (
	"context"
	"strings"
	"testing"

	"github.com/ghettovoice/gosip/sip"

	sipbuild "github.com/your-org/gb28181-simulator/internal/adapter/sip"
)

// TestTransport_Receive_PeerAddressUnavailableIsError asserts the spec
// requirement that a transport which cannot determine the peer address
// surfaces an explicit error instead of returning an empty address, which
// would let a caller silently reply to the wrong destination.
func TestTransport_Receive_PeerAddressUnavailableIsError(t *testing.T) {
	// Note: gosip ignores SetSource("") and keeps whatever value the
	// builder installed, so a truly empty source cannot be produced
	// through the public API. A blank string trims to empty inside
	// Receive and therefore exercises the same "no usable address" path.
	cases := []struct {
		name    string
		source  string
		wantMsg string
	}{
		{"blank source", "   ", "peer address unavailable"},
		{"source without port", "127.0.0.1", "lacks :port"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &Transport{
				out:  make(chan sip.Message, 1),
				stop: make(chan struct{}),
			}
			req, err := sipbuild.BuildRequest(
				sip.RequestMethod("INVITE"),
				"sip:34020000001320000001@127.0.0.1:5060",
			)
			if err != nil {
				t.Fatalf("BuildRequest: %v", err)
			}
			req.SetSource(tc.source)
			tr.out <- req

			msg, peer, err := tr.Receive(context.Background())
			if err == nil {
				t.Fatalf("Receive succeeded with source %q, want error", tc.source)
			}
			if msg != nil {
				t.Errorf("msg on error = %v, want nil", msg)
			}
			if peer != "" {
				t.Errorf("peer on error = %q, want empty", peer)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantMsg)
			}
		})
	}
}

// TestTransport_Receive_ReportsPeerAddress asserts the happy path: a message
// carrying a source is returned together with that address, so callers can
// reply without extra routing configuration.
func TestTransport_Receive_ReportsPeerAddress(t *testing.T) {
	tr := &Transport{
		out:  make(chan sip.Message, 1),
		stop: make(chan struct{}),
	}
	req, err := sipbuild.BuildRequest(
		sip.RequestMethod("INVITE"),
		"sip:34020000001320000001@127.0.0.1:5060",
	)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	const want = "127.0.0.1:5061"
	req.SetSource(want)
	tr.out <- req

	msg, peer, err := tr.Receive(context.Background())
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if msg == nil {
		t.Fatal("msg is nil")
	}
	if peer != want {
		t.Errorf("peer = %q, want %q", peer, want)
	}
}
