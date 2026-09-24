package sdp

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readFixture loads a testdata/*.sdp file and returns its bytes.
func readFixture(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("testdata", name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read fixture %s: %v", p, err)
	}
	return string(b)
}

// TestParse_INVITE_PS_PreservesSSRC exercises Scenario 1 of task §2.2:
// parsing a GB28181 INVITE body and recovering the y=/f= extension lines.
func TestParse_INVITE_PS_PreservesSSRC(t *testing.T) {
	body := readFixture(t, "gb28181-invite-ps.sdp")
	s, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got, want := len(s.Media), 1; got != want {
		t.Fatalf("Media len = %d, want %d", got, want)
	}
	mb := s.Media[0]
	if mb.SSRC != "1234567890" {
		t.Errorf("SSRC = %q, want %q", mb.SSRC, "1234567890")
	}
	if mb.MediaOption != "v" {
		t.Errorf("MediaOption = %q, want %q", mb.MediaOption, "v")
	}
	// RFC 4566 fields remain accessible via the embedded MediaDescription.
	if _, ok := mb.Attribute("rtpmap"); !ok {
		t.Errorf("rtpmap attribute lost after round-trip")
	}
}

// TestParse_RFC4566_Only covers Scenario 2 of task §2.2: no §K lines.
func TestParse_RFC4566_Only(t *testing.T) {
	body := readFixture(t, "rfc4566-only.sdp")
	s, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := len(s.Media); got != 0 {
		t.Fatalf("Media len = %d, want 0 (no m= line)", got)
	}
}

// TestParse_AV_TwoBlocks covers dual-m= scenario (audio + video).
func TestParse_AV_TwoBlocks(t *testing.T) {
	body := readFixture(t, "gb28181-invite-av.sdp")
	s, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got, want := len(s.Media), 2; got != want {
		t.Fatalf("Media len = %d, want %d", got, want)
	}
	if s.Media[0].SSRC != "9876543210" || s.Media[0].MediaOption != "a" {
		t.Errorf("block0 = (SSRC=%q,MediaOption=%q), want (9876543210,a)",
			s.Media[0].SSRC, s.Media[0].MediaOption)
	}
	if s.Media[1].SSRC != "1234567890" || s.Media[1].MediaOption != "v" {
		t.Errorf("block1 = (SSRC=%q,MediaOption=%q), want (1234567890,v)",
			s.Media[1].SSRC, s.Media[1].MediaOption)
	}
}

// TestMarshal_PreservesKOrdering exercises Scenario 1 of task §2.3:
// marshal a session that already carries SSRC + MediaOption and assert
// that y= appears before f= inside each block.
func TestMarshal_PreservesKOrdering(t *testing.T) {
	body := readFixture(t, "gb28181-invite-ps.sdp")
	s, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// Find the order: y= must appear before f= inside the same block.
	yIdx := strings.Index(out, "y=1234567890")
	fIdx := strings.Index(out, "f=v")
	if yIdx < 0 || fIdx < 0 {
		t.Fatalf("missing y=/f= in marshalled output:\n%s", out)
	}
	if yIdx >= fIdx {
		t.Errorf("y= did not appear before f= (yIdx=%d, fIdx=%d)", yIdx, fIdx)
	}
}

// TestMarshal_EmptySSRC_OmitsLine verifies Scenario 2 of task §2.3.
func TestMarshal_EmptySSRC_OmitsLine(t *testing.T) {
	body := readFixture(t, "gb28181-invite-ps.sdp")
	s, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	s.Media[0].SSRC = "" // drop SSRC
	out, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(out, "y=") {
		t.Errorf("empty SSRC should suppress y= line, got:\n%s", out)
	}
	// f=v must still be there (independent of SSRC).
	if !strings.Contains(out, "f=v") {
		t.Errorf("MediaOption=f=v should remain, got:\n%s", out)
	}
}

// TestRoundTrip_AV verifies parse→marshal→parse is stable for the
// audio+video fixture. Round-trip is stable on parsed SSRC/MediaOption
// (which is the semantic contract §2.2 / §2.3 needs); the o= session-id
// field is pinned by pion's Marshal to 0 on empty SessionDescription,
// but that does not affect §K fields.
func TestRoundTrip_AV(t *testing.T) {
	body := readFixture(t, "gb28181-invite-av.sdp")
	s1, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse1: %v", err)
	}
	out, err := Marshal(s1)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if out == "" {
		t.Fatal("Marshal produced empty output")
	}
	s2, err := Parse(out)
	if err != nil {
		t.Fatalf("Parse2: %v — marshal output:\n%s", err, out)
	}
	if len(s2.Media) != len(s1.Media) {
		t.Fatalf("Media len drift: %d → %d", len(s1.Media), len(s2.Media))
	}
	for i := range s1.Media {
		if s1.Media[i].SSRC != s2.Media[i].SSRC {
			t.Errorf("block[%d] SSRC drift: %q → %q",
				i, s1.Media[i].SSRC, s2.Media[i].SSRC)
		}
		if s1.Media[i].MediaOption != s2.Media[i].MediaOption {
			t.Errorf("block[%d] MediaOption drift: %q → %q",
				i, s1.Media[i].MediaOption, s2.Media[i].MediaOption)
		}
	}
}

// --- 恶意 / 边界 SDP (任务 §2.4) ----------------------------------------------

func TestParse_MissingKLines(t *testing.T) {
	// No y=/f= at all → returns success with empty SSRC/MediaOption.
	body := "v=0\r\no=- 0 0 IN IP4 0.0.0.0\r\ns=-\r\nc=IN IP4 0.0.0.0\r\nt=0 0\r\nm=video 0 RTP/AVP 96\r\n"
	s, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(s.Media) != 1 {
		t.Fatalf("Media len = %d, want 1", len(s.Media))
	}
	if s.Media[0].SSRC != "" {
		t.Errorf("SSRC = %q, want empty", s.Media[0].SSRC)
	}
	if s.Media[0].MediaOption != "" {
		t.Errorf("MediaOption = %q, want empty", s.Media[0].MediaOption)
	}
}

func TestParse_EmptyF(t *testing.T) {
	// Empty `f=` is recorded as MediaOption=kEmpty and is then omitted on
	// marshal. Parse must not error.
	body := "v=0\r\no=- 0 0 IN IP4 0.0.0.0\r\ns=-\r\nc=IN IP4 0.0.0.0\r\nt=0 0\r\nm=video 0 RTP/AVP 96\r\ny=111\r\nf=\r\n"
	s, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if s.Media[0].SSRC != "111" {
		t.Errorf("SSRC = %q, want 111", s.Media[0].SSRC)
	}
	if s.Media[0].MediaOption != "" {
		t.Errorf("MediaOption = %q, want empty", s.Media[0].MediaOption)
	}
}

func TestParse_A_Y_Attribute(t *testing.T) {
	// `a=y:12345` is a (bogus) RFC 4566 attribute, NOT a §K y= line.
	body := "v=0\r\no=- 0 0 IN IP4 0.0.0.0\r\ns=-\r\nc=IN IP4 0.0.0.0\r\nt=0 0\r\nm=video 0 RTP/AVP 96\r\na=y:12345\r\n"
	s, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if s.Media[0].SSRC != "" {
		t.Errorf("SSRC = %q, want empty (a=y:12345 is an attribute)", s.Media[0].SSRC)
	}
	// pion parses `a=y:12345` into `Attribute{Key:"y", Value:"12345"}`.
	if v, ok := s.Media[0].Attribute("y"); !ok || v != "12345" {
		t.Errorf("a=y:12345 got value=%q ok=%v, want value=12345", v, ok)
	}
}

func TestParse_CRLF_LF_Mixed(t *testing.T) {
	// Mix CRLF and LF in the same body — pion must handle both.
	body := "v=0\r\no=- 0 0 IN IP4 0.0.0.0\ns=-\r\nc=IN IP4 0.0.0.0\nt=0 0\nm=video 0 RTP/AVP 96\ny=222\nf=v\n"
	s, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if s.Media[0].SSRC != "222" || s.Media[0].MediaOption != "v" {
		t.Errorf("got (SSRC=%q,MediaOption=%q)", s.Media[0].SSRC, s.Media[0].MediaOption)
	}
}

func TestParse_EmptyBody(t *testing.T) {
	// Empty body → ErrEmptyBody (per ErrEmptyBody sentinel contract).
	if _, err := Parse(""); err == nil {
		t.Errorf("Parse(\"\") should return error")
	} else if err != ErrEmptyBody {
		t.Errorf("Parse(\"\") err = %v, want ErrEmptyBody", err)
	}
	if _, err := Parse("   \n\n  "); err == nil {
		t.Errorf("Parse(whitespace) should return error")
	}
}

func TestParse_MultipleY(t *testing.T) {
	// GB/T 28181 does not allow more than one y= per block; but Parse
	// must not crash — last-write-wins is acceptable.
	body := "v=0\r\no=- 0 0 IN IP4 0.0.0.0\r\ns=-\r\nc=IN IP4 0.0.0.0\r\nt=0 0\r\nm=video 0 RTP/AVP 96\r\ny=111\r\ny=222\r\n"
	s, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Both klines survive; they stack into two separate SSRC fields is
	// not possible (string), so last-wins is fine.
	if s.Media[0].SSRC != "111" && s.Media[0].SSRC != "222" {
		t.Errorf("SSRC = %q, want 111 or 222 (last-wins)", s.Media[0].SSRC)
	}
}

// --- Fixture SHA256 校验 (任务 §8.1) -------------------------------------------

// TestFixtures_SHA256Stable ensures the on-disk fixtures have not drifted.
// Update the expected values in testdata/golden-sha256 only when the
// fixture is intentionally modified.
func TestFixtures_SHA256Stable(t *testing.T) {
	expected, err := os.ReadFile(filepath.Join("testdata", "golden-sha256"))
	if err != nil {
		t.Fatalf("read golden-sha256: %v", err)
	}
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sdp") {
			continue
		}
		b, err := os.ReadFile(filepath.Join("testdata", e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		sum := sha256.Sum256(b)
		got := hex.EncodeToString(sum[:])
		line := strings.TrimSpace(string(expected))
		want := ""
		for _, ln := range strings.Split(line, "\n") {
			if strings.HasSuffix(ln, "  "+e.Name()) {
				want = strings.Fields(ln)[0]
				break
			}
		}
		if want == "" {
			t.Errorf("no SHA256 recorded for %s; add to golden-sha256", e.Name())
			continue
		}
		if got != want {
			t.Errorf("SHA256 drift on %s: got=%s want=%s", e.Name(), got, want)
		}
	}
}

// --- 性能基准 (任务 §2.5) ----------------------------------------------------

// BenchmarkParse measures Parse() on a representative 1 KiB SDP body.
// Goal: < 50 µs per call. Use `go test -bench BenchmarkParse -benchmem`.
func BenchmarkParse(b *testing.B) {
	body := readFixture(&testing.T{}, "gb28181-invite-av.sdp")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Parse(body)
	}
}