package port

import (
	"testing"
)

// TestParse_PlayGolden asserts the byte-level shape of a fully populated
// PLAY body (tidy up 7.1 golden test).
func TestParse_PlayGolden(t *testing.T) {
	body := "PLAY RTSP/1.0\r\nCSeq: 1\r\nScale: 2.0\r\nRange: npt=3600-\r\n\r\n"
	got, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Command != CommandPlay {
		t.Errorf("Command = %q, want PLAY", got.Command)
	}
	if got.CSeq != 1 {
		t.Errorf("CSeq = %d, want 1", got.CSeq)
	}
	if got.Scale != 2.0 {
		t.Errorf("Scale = %v, want 2.0", got.Scale)
	}
	if !got.HasSeek || got.SeekFrom != 3600 {
		t.Errorf("SeekFrom = %v (HasSeek=%v), want 3600", got.SeekFrom, got.HasSeek)
	}
	if got.SeekTo >= 0 {
		t.Errorf("SeekTo = %v, want <0 (no end)", got.SeekTo)
	}
}

// TestParse_PauseDefaults asserts that PAUSE without extra headers
// collapses to Scale=1.0 and no seek (tidy up 7.1).
func TestParse_PauseDefaults(t *testing.T) {
	got, err := Parse("PAUSE RTSP/1.0\r\nCSeq: 8\r\n\r\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Command != CommandPause {
		t.Errorf("Command = %q, want PAUSE", got.Command)
	}
	if got.Scale != 1.0 {
		t.Errorf("Scale = %v, want 1.0 (default)", got.Scale)
	}
	if got.HasSeek {
		t.Errorf("HasSeek = true, want false")
	}
	if got.CSeq != 8 {
		t.Errorf("CSeq = %d, want 8", got.CSeq)
	}
}

// TestParse_LineEndings asserts that both CRLF and LF terminators are
// accepted (the RFC allows either).
func TestParse_LineEndings(t *testing.T) {
	cases := []string{
		"PLAY RTSP/1.0\nCSeq: 1\n\n",
		"PLAY RTSP/1.0\r\nCSeq: 1\r\n\r\n",
		"PLAY RTSP/1.0\nCSeq: 1", // no terminating blank line
	}
	for i, c := range cases {
		got, err := Parse(c)
		if err != nil {
			t.Fatalf("[%d] Parse(%q): %v", i, c, err)
		}
		if got.Command != CommandPlay {
			t.Errorf("[%d] Command = %q", i, got.Command)
		}
		if got.CSeq != 1 {
			t.Errorf("[%d] CSeq = %d", i, got.CSeq)
		}
	}
}

// TestParse_NegativeScale asserts that negative scales (reverse playback)
// parse without error.
func TestParse_NegativeScale(t *testing.T) {
	got, err := Parse("PLAY RTSP/1.0\r\nCSeq: 1\r\nScale: -1.0\r\n\r\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Scale != -1.0 {
		t.Errorf("Scale = %v, want -1.0", got.Scale)
	}
}

// TestParse_BadScale asserts that NaN and Inf are refused so the port's
// own validator is never asked to handle an unparseable rate.
func TestParse_BadScale(t *testing.T) {
	cases := []string{"NaN", "Inf", "-Inf", "two"}
	for _, c := range cases {
		if _, err := Parse("PLAY RTSP/1.0\r\nScale: " + c + "\r\n\r\n"); err == nil {
			t.Errorf("Scale %q should have failed to parse", c)
		}
	}
}

// TestParse_UnknownCommand asserts that verbs outside PLAY/PAUSE are
// rejected — the acceptor must not silently turn a TEARDOWN into a
// playback command.
func TestParse_UnknownCommand(t *testing.T) {
	if _, err := Parse("TEARDOWN RTSP/1.0\r\n\r\n"); err == nil {
		t.Error("TEARDOWN parsed successfully, want error")
	}
	if _, err := Parse("\r\n"); err == nil {
		t.Error("empty body parsed successfully, want error")
	}
}

// TestParse_NPTNowStart asserts "now" as the start is accepted and
// resolves to 0; "now" as the end is rejected.
func TestParse_NPTNowStart(t *testing.T) {
	got, err := Parse("PLAY RTSP/1.0\r\nRange: npt=now-\r\n\r\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !got.HasSeek || got.SeekFrom != 0 {
		t.Errorf("SeekFrom = %v (HasSeek=%v), want 0/true", got.SeekFrom, got.HasSeek)
	}
	if _, err := Parse("PLAY RTSP/1.0\r\nRange: npt=0-now\r\n\r\n"); err == nil {
		t.Error("now as end parsed successfully, want error")
	}
}