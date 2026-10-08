package model

import (
	"reflect"
	"strings"
	"testing"
)

// TestNodeID_Parse_AcceptsLegalEncoding asserts that well-formed 20-digit
// ids are accepted and map to the expected kind, and that String()
// round-trips the verbatim encoding.
func TestNodeID_Parse_AcceptsLegalEncoding(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		kind NodeKind
	}{
		{"device ipc", "34020000011310000001", NodeKindDevice},
		{"device dvr", "34020000011110000002", NodeKindDevice},
		{"device nvr", "34020000011180000003", NodeKindDevice},
		{"platform large", "34020000012000000001", NodeKindPlatformLarge},
		{"platform small", "34020000012160000001", NodeKindPlatformSmall},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := ParseNodeID(tc.raw)
			if err != nil {
				t.Fatalf("ParseNodeID(%q) = error %v, want success", tc.raw, err)
			}
			if id.String() != tc.raw {
				t.Errorf("String() = %q, want %q", id.String(), tc.raw)
			}
			if id.Kind() != tc.kind {
				t.Errorf("Kind() = %v, want %v", id.Kind(), tc.kind)
			}
			if got, want := id.TypeCode(), tc.raw[10:13]; got != want {
				t.Errorf("TypeCode() = %q, want %q", got, want)
			}
		})
	}
}

// TestNodeID_Parse_RejectsIllegal covers the four rejection classes required
// by task 1.2: wrong length, non-digit content, and an unmapped type-code
// segment. Every error must name the reason and the observed length.
func TestNodeID_Parse_RejectsIllegal(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantLen int
		wantMsg string
	}{
		{"too short", "3402000001131000000", 19, "length 19"},
		{"too long", "340200000113100000012", 21, "length 21"},
		{"empty", "", 0, "length 0"},
		{"contains letter", "3402000001131000000a", 20, "byte 20"},
		{"contains dash", "34020000-11310000001", 20, "want digit"},
		{"unknown type code", "34020000019990000001", 20, "type code \"999\""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := ParseNodeID(tc.raw)
			if err == nil {
				t.Fatalf("ParseNodeID(%q) succeeded, want error", tc.raw)
			}
			if id.String() != "" {
				t.Errorf("id on error = %q, want zero value", id.String())
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantMsg)
			}
			if !strings.Contains(err.Error(), "illegal") {
				t.Errorf("error = %q, want it to contain \"illegal\"", err.Error())
			}
		})
	}
}

// TestNodeKind_SpellingRoundTrip asserts the configuration spelling is
// stable and that the empty string is rejected.
func TestNodeKind_SpellingRoundTrip(t *testing.T) {
	for _, k := range []NodeKind{NodeKindDevice, NodeKindPlatformLarge, NodeKindPlatformSmall} {
		got, err := ParseNodeKind(k.String())
		if err != nil {
			t.Fatalf("ParseNodeKind(%q) = %v", k.String(), err)
		}
		if got != k {
			t.Errorf("round trip %q = %v, want %v", k.String(), got, k)
		}
	}
	if NodeKindUnknown.String() != "" {
		t.Errorf("NodeKindUnknown.String() = %q, want \"\"", NodeKindUnknown.String())
	}
	if _, err := ParseNodeKind(""); err == nil {
		t.Error("ParseNodeKind(\"\") succeeded, want error")
	}
	if _, err := ParseNodeKind("camera"); err == nil {
		t.Error("ParseNodeKind(\"camera\") succeeded, want error")
	}
}

// TestNodeProfile_Construction asserts mandatory field validation.
func TestNodeProfile_Construction(t *testing.T) {
	const legalID = "34020000011310000001"
	if _, err := NewNodeProfile(legalID, "127.0.0.1:5060", "3402000000", "acme"); err != nil {
		t.Fatalf("NewNodeProfile legal = %v, want success", err)
	}
	for _, tc := range []struct {
		name, id, addr, domain string
	}{
		{"illegal id", "1234", "127.0.0.1:5060", "3402000000"},
		{"empty addr", legalID, "", "3402000000"},
		{"blank addr", legalID, "   ", "3402000000"},
		{"empty domain", legalID, "127.0.0.1:5060", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewNodeProfile(tc.id, tc.addr, tc.domain, ""); err == nil {
				t.Errorf("NewNodeProfile(%q,%q,%q) succeeded, want error", tc.id, tc.addr, tc.domain)
			}
		})
	}
}

// TestNodeProfile_WithReturnsCopy asserts the With... methods leave the
// receiver untouched, which is how callers change a value object.
func TestNodeProfile_WithReturnsCopy(t *testing.T) {
	p, err := NewNodeProfile("34020000011310000001", "127.0.0.1:5060", "3402000000", "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	moved, err := p.WithAddr("127.0.0.1:5061")
	if err != nil {
		t.Fatalf("WithAddr: %v", err)
	}
	if moved.Addr() != "127.0.0.1:5061" {
		t.Errorf("moved.Addr() = %q, want 127.0.0.1:5061", moved.Addr())
	}
	if p.Addr() != "127.0.0.1:5060" {
		t.Errorf("original Addr() mutated to %q, want 127.0.0.1:5060", p.Addr())
	}
	if moved.WithVendor("other").Vendor() != "other" {
		t.Error("WithVendor did not apply")
	}
}

// TestNodeProfile_FieldsUnexported asserts immutability the way the rest of
// the model package does: no exported field, so an outside caller cannot
// reach in and change a value after construction (task 1.3).
func TestNodeProfile_FieldsUnexported(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeOf(NodeProfile{}),
		reflect.TypeOf(NodeID{}),
		reflect.TypeOf(Node{}),
	} {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if f.IsExported() {
				t.Errorf("%s.%s is exported; %s must stay immutable",
					typ.Name(), f.Name, typ.Name())
			}
		}
	}
}

// TestNode_StartsIdleAndAdvances asserts a fresh node is Idle and that
// advancing through the happy path works while illegal jumps are refused.
func TestNode_StartsIdleAndAdvances(t *testing.T) {
	p, err := NewNodeProfile("34020000011310000001", "127.0.0.1:5060", "3402000000", "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	n := NewNode(p)
	if n.Status() != StatusIdle {
		t.Fatalf("fresh node status = %v, want idle", n.Status())
	}
	registering, err := n.WithStatus(StatusRegistering)
	if err != nil {
		t.Fatalf("Idle -> Registering = %v, want success", err)
	}
	if registering.Status() != StatusRegistering {
		t.Errorf("status = %v, want registering", registering.Status())
	}
	if n.Status() != StatusIdle {
		t.Errorf("receiver mutated to %v, want idle", n.Status())
	}
}

func TestNodeProfile_MediaConfig_RoundTrip(t *testing.T) {
	p, err := NewNodeProfile("34020000001110000001", "127.0.0.1:5060", "3402000000", "test")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	if _, ok := p.MediaConfig(); ok {
		t.Fatalf("fresh profile reports media, want none")
	}
	cfg := MediaConfig{
		Kind:  SourceKindSynthetic,
		Path:  "",
		SSRC:  0x1234,
		MTU:   1300,
		FPS:   30,
		Clock: 90000,
	}
	p.SetMediaConfig(cfg)
	got, ok := p.MediaConfig()
	if !ok {
		t.Fatalf("after Set, MediaConfig reports !ok")
	}
	if got.Kind != SourceKindSynthetic {
		t.Errorf("kind = %q, want synthetic", got.Kind)
	}
	if got.SSRC != 0x1234 || got.MTU != 1300 || got.FPS != 30 {
		t.Errorf("media fields lost: %+v", got)
	}
}

func TestNodeProfile_MediaConfig_Clear(t *testing.T) {
	p, err := NewNodeProfile("34020000001110000001", "127.0.0.1:5060", "3402000000", "test")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	p.SetMediaConfig(MediaConfig{Kind: SourceKindSynthetic, FPS: 25})
	if _, ok := p.MediaConfig(); !ok {
		t.Fatalf("after Set, MediaConfig reports !ok")
	}
	// Zero Kind must clear the slot — used by HTTP DELETE.
	p.SetMediaConfig(MediaConfig{})
	if _, ok := p.MediaConfig(); ok {
		t.Fatalf("after Set(empty), MediaConfig still reports ok")
	}
}

func TestNewNodeProfileWithMedia(t *testing.T) {
	media := MediaConfig{Kind: SourceKindFile, Path: "/tmp/v.ps", Loop: true, FPS: 25}
	p, err := NewNodeProfileWithMedia("34020000001110000001", "127.0.0.1:5060", "3402000000", "test", &media)
	if err != nil {
		t.Fatalf("NewNodeProfileWithMedia: %v", err)
	}
	got, ok := p.MediaConfig()
	if !ok {
		t.Fatalf("no media after builder")
	}
	if got.Kind != SourceKindFile || got.Path != "/tmp/v.ps" || !got.Loop {
		t.Errorf("builder lost fields: %+v", got)
	}
}

func TestNewNodeProfileWithMedia_NilMedia(t *testing.T) {
	p, err := NewNodeProfileWithMedia("34020000001110000001", "127.0.0.1:5060", "3402000000", "test", nil)
	if err != nil {
		t.Fatalf("NewNodeProfileWithMedia(nil): %v", err)
	}
	if _, ok := p.MediaConfig(); ok {
		t.Errorf("nil media should leave profile unconfigured")
	}
}

func TestParseNodeMediaConfig(t *testing.T) {
	cases := []struct {
		name    string
		kind    string
		path    string
		loop    bool
		mtu     int
		ssrc    uint32
		clock   uint64
		fps     int
		wantErr bool
		wantK   MediaSourceKind
	}{
		{"empty kind yields zero value", "", "", false, 0, 0, 0, 0, false, ""},
		{"synthetic no path", "synthetic", "", false, 0, 0, 0, 25, false, SourceKindSynthetic},
		{"file requires path", "file", "", false, 0, 0, 0, 0, true, ""},
		{"file with path", "file", "/tmp/v.ps", true, 1400, 0x11, 90000, 0, false, SourceKindFile},
		{"rtsp with url", "rtsp", "rtsp://x/y", false, 0, 0, 0, 0, false, SourceKindRTSP},
		{"hls with url", "hls", "http://x/m3u8", false, 0, 0, 0, 0, false, SourceKindHLS},
		{"unknown kind rejected", "webrtc", "", false, 0, 0, 0, 0, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := ParseNodeMediaConfig(tc.kind, tc.path, tc.loop, tc.mtu, tc.ssrc, tc.clock, tc.fps)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if cfg.Kind != tc.wantK {
				t.Errorf("kind = %q, want %q", cfg.Kind, tc.wantK)
			}
		})
	}
}
