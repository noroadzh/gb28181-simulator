package sdp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// TestSDPCodecAdapter_ImplementsPort is a redundant runtime assertion that
// the adapter satisfies port.SDPCodec. The package already declares
// `var _ port.SDPCodec = (*SDPCodecAdapter)(nil)`, but this test catches
// any future drift between the file-level assertion and the actual type.
func TestSDPCodecAdapter_ImplementsPort(t *testing.T) {
	var _ port.SDPCodec = NewSDPCodecAdapter()
}

// loadFixture reads a testdata file relative to the adapter/sdp package.
// Centralising the read keeps port tests agnostic of file path layout.
func loadPortFixture(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("testdata", name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

// TestSDPCodecAdapter_ParseGB28181EmitsSession exercises the port.SDPCodec
// path against a real GB28181 INVITE body (audio + video PS streams) and
// asserts the adapter preserves both streams and high-level session
// metadata. This is the primary smoke test for §6.2.
func TestSDPCodecAdapter_ParseGB28181EmitsSession(t *testing.T) {
	body := loadPortFixture(t, "gb28181-invite-av.sdp")
	a := NewSDPCodecAdapter()
	got, err := a.Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Origin() == "" {
		t.Error("Origin must not be empty")
	}
	if got.SessionName() != "Play" {
		t.Errorf("SessionName = %q, want Play", got.SessionName())
	}
	if got.ConnectionAddress() != "239.1.1.1" {
		t.Errorf("ConnectionAddress = %q, want 239.1.1.1 (session-level c= line)",
			got.ConnectionAddress())
	}
	if len(got.Streams()) != 2 {
		t.Fatalf("Streams len = %d, want 2", len(got.Streams()))
	}
	wantTypes := []string{"audio", "video"}
	for i, want := range wantTypes {
		if got.Streams()[i].MediaType != want {
			t.Errorf("Streams[%d].MediaType = %q, want %q",
				i, got.Streams()[i].MediaType, want)
		}
	}
}

// TestSDPCodecAdapter_MarshalReplaysLastRaw covers the documented guarantee
// that Marshal is faithful to the last Parse input (no mutation, no
// round-trip through the legacy session). If a future change introduces
// a write-through path, this test will catch the resulting divergence.
func TestSDPCodecAdapter_MarshalReplaysLastRaw(t *testing.T) {
	body := loadPortFixture(t, "gb28181-invite-av.sdp")
	a := NewSDPCodecAdapter()
	if _, err := a.Parse(body); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Build a fresh, completely-different model.Session — Marshal must NOT
	// consult its fields and must return the stored raw text instead.
	streams := []model.MediaStream{{
		MediaType: "video", Port: 9999, Protocol: "RTP/AVP", Formats: []string{"0"},
	}}
	fresh := model.NewSession("o=zzz", "AAA", "127.0.0.1", streams, nil)
	out, err := a.Marshal(fresh)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if out != body {
		t.Errorf("Marshal diverged from stored raw text.\n--- got ---\n%s\n--- want ---\n%s", out, body)
	}
}

// TestSDPCodecAdapter_MarshalWithoutParseErrors verifies the documented
// error path: Marshal called before Parse must error rather than silently
// emit an empty string. This protects callers from a footgun where they
// build a model.Session externally and expect Marshal to round-trip.
func TestSDPCodecAdapter_MarshalWithoutParseErrors(t *testing.T) {
	a := NewSDPCodecAdapter()
	streams := []model.MediaStream{{
		MediaType: "audio", Port: 0, Protocol: "RTP/AVP", Formats: []string{"0"},
	}}
	s := model.NewSession("o=x", "-", "127.0.0.1", streams, nil)
	if _, err := a.Marshal(s); err == nil {
		t.Error("Marshal without Parse must error; got nil")
	}
}

// TestSDPCodecAdapter_ParseEmptyErrors ensures the adapter rejects empty
// bodies up-front instead of forwarding them to the legacy parser.
func TestSDPCodecAdapter_ParseEmptyErrors(t *testing.T) {
	a := NewSDPCodecAdapter()
	if _, err := a.Parse(""); err == nil {
		t.Error("Parse(\"\") must error; got nil")
	}
}

// TestSDPCodecAdapter_ParseMalformedErrors covers the propagation path:
// a syntactically broken body should return a non-nil error. We don't
// pin the exact message (legacy is free to evolve), only that the error
// is non-nil.
func TestSDPCodecAdapter_ParseMalformedErrors(t *testing.T) {
	a := NewSDPCodecAdapter()
	_, err := a.Parse("this is not SDP\r\n")
	if err == nil {
		t.Fatal("malformed SDP must error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "sdp") {
		t.Errorf("error must mention SDP; got %q", err)
	}
}

// TestSDPCodecAdapter_RFC4566OnlyAccepts documents that the adapter
// accepts a pure RFC 4566 body (no GB28181 y=/f= extensions) by exercising
// the same Parse path against the rfc4566-only.sdp fixture. This guards
// against future regressions where the adapter might add a hard
// requirement on GB28181-only fields.
func TestSDPCodecAdapter_RFC4566OnlyAccepts(t *testing.T) {
	body := loadPortFixture(t, "rfc4566-only.sdp")
	a := NewSDPCodecAdapter()
	got, err := a.Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got.Streams()) == 0 {
		t.Error("Streams must be non-empty for a body containing m= lines")
	}
}
