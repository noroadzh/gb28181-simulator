package model

import "testing"

// validStreams is the convenience that two well-formed MediaStream values.
func validStreams() []MediaStream {
	return []MediaStream{
		{MediaType: "video", Port: 9000, Protocol: "RTP/AVP", Formats: []string{"96"}},
		{MediaType: "audio", Port: 9002, Protocol: "RTP/AVP", Formats: []string{"8"}},
	}
}

// TestSession_BuildAndRead asserts a Session can be built and read.
func TestSession_BuildAndRead(t *testing.T) {
	streams := validStreams()
	s := NewSession(
		"o=- 1234 0 IN IP4 127.0.0.1",
		"PS Media",
		"c=IN IP4 0.0.0.0",
		streams,
		[]string{"y=1234567890abcdef", "f=v/////a///a///8//"},
	)
	if s.Origin() == "" || s.SessionName() != "PS Media" {
		t.Errorf("Origin/SessionName mismatch: %q %q", s.Origin(), s.SessionName())
	}
	if got := s.Streams(); len(got) != 2 || got[0].MediaType != "video" {
		t.Errorf("Streams() = %v, want 2 items with video first", got)
	}
	if got := s.Extensions(); len(got) != 2 || got[0] != "y=1234567890abcdef" {
		t.Errorf("Extensions() = %v", got)
	}
}

// TestSession_StreamsDefensiveCopy asserts Streams() returns a copy.
func TestSession_StreamsDefensiveCopy(t *testing.T) {
	s := NewSession("o=- 0 0 IN IP4 0", "S", "c=IN IP4 0", validStreams(), nil)
	cp := s.Streams()
	cp[0].MediaType = "tampered"
	if s.Streams()[0].MediaType != "video" {
		t.Errorf("Streams() leaked live slice; got %q", s.Streams()[0].MediaType)
	}
}

// TestSession_EmptyOriginPanics asserts constructor panics.
func TestSession_EmptyOriginPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewSession with empty origin accepted; want panic")
		}
	}()
	_ = NewSession("", "S", "c=IN IP4 0", validStreams(), nil)
}

// TestSession_NoStreamsPanics asserts constructor panics.
func TestSession_NoStreamsPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewSession with no streams accepted; want panic")
		}
	}()
	_ = NewSession("o=- 0 0 IN IP4 0", "S", "c=IN IP4 0", nil, nil)
}

// TestSession_BadStreamPanics asserts constructor panics on malformed
// MediaStream.
func TestSession_BadStreamPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewSession with empty MediaType accepted; want panic")
		}
	}()
	_ = NewSession("o=- 0 0 IN IP4 0", "S", "c=IN IP4 0",
		[]MediaStream{{MediaType: "", Port: 9000, Protocol: "RTP/AVP", Formats: []string{"96"}}}, nil)
}

// TestSession_ExtensionCRLFPanics asserts constructor panics on CRLF in
// extension line.
func TestSession_ExtensionCRLFPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewSession with CRLF extension accepted; want panic")
		}
	}()
	_ = NewSession("o=- 0 0 IN IP4 0", "S", "c=IN IP4 0",
		validStreams(), []string{"y=abc\r\nf=v"})
}

// TestMediaStream_Validate exercises the four required-field checks.
func TestMediaStream_Validate(t *testing.T) {
	good := MediaStream{MediaType: "video", Port: 9000, Protocol: "RTP/AVP", Formats: []string{"96"}}
	if err := good.Validate(); err != nil {
		t.Errorf("good Validate: %v", err)
	}
	cases := []MediaStream{
		{MediaType: "", Port: 9000, Protocol: "RTP/AVP", Formats: []string{"96"}},
		{MediaType: "video", Port: -1, Protocol: "RTP/AVP", Formats: []string{"96"}},
		{MediaType: "video", Port: 9000, Protocol: "", Formats: []string{"96"}},
		{MediaType: "video", Port: 9000, Protocol: "RTP/AVP", Formats: nil},
	}
	for i, bad := range cases {
		if err := bad.Validate(); err == nil {
			t.Errorf("case[%d]: Validate accepted bad stream", i)
		}
	}
}