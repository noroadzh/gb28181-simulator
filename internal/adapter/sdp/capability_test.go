package sdp

import (
	"testing"

	pionsdp "github.com/pion/sdp"
)

// --- 常量校验 (任务 §2.1) --------------------------------------------------

func TestCapabilityModule_PayloadTypes(t *testing.T) {
	if ModuleH265.PayloadType() != 100 {
		t.Errorf("ModuleH265 payload = %d, want 100", ModuleH265.PayloadType())
	}
	if ModuleAAC.PayloadType() != 97 {
		t.Errorf("ModuleAAC payload = %d, want 97", ModuleAAC.PayloadType())
	}
	if ModuleG7221.PayloadType() != 99 {
		t.Errorf("ModuleG7221 payload = %d, want 99", ModuleG7221.PayloadType())
	}
}

func TestCapabilityModule_RTPMapValues(t *testing.T) {
	if ModuleH265.RTPMapValue() != "H265/90000" {
		t.Errorf("ModuleH265 rtpmap = %q, want %q", ModuleH265.RTPMapValue(), "H265/90000")
	}
	if ModuleAAC.RTPMapValue() != "MPEG4-GENERIC/16000/2" {
		t.Errorf("ModuleAAC rtpmap = %q, want %q", ModuleAAC.RTPMapValue(), "MPEG4-GENERIC/16000/2")
	}
	if ModuleG7221.RTPMapValue() != "G7221/16000" {
		t.Errorf("ModuleG7221 rtpmap = %q, want %q", ModuleG7221.RTPMapValue(), "G7221/16000")
	}
}

func TestCapabilityModule_Unknown(t *testing.T) {
	if MediaCapabilityModule("unknown").PayloadType() != 0 {
		t.Errorf("unknown module payload = %d, want 0", MediaCapabilityModule("unknown").PayloadType())
	}
	if MediaCapabilityModule("unknown").RTPMapValue() != "" {
		t.Errorf("unknown module rtpmap = %q, want empty", MediaCapabilityModule("unknown").RTPMapValue())
	}
}

// --- DeclareCapability 注入 (任务 §2.1) -------------------------------------

func newVideoMediaBlock() *MediaBlock {
	return &MediaBlock{
		MediaDescription: &pionsdp.MediaDescription{
			MediaName: pionsdp.MediaName{
				Media:   "video",
				Port:    pionsdp.RangedPort{Value: 1234},
				Protos:  []string{"RTP/AVP"},
				Formats: []string{"96"},
			},
			Attributes: make([]pionsdp.Attribute, 0),
		},
	}
}

func TestDeclareCapability_InjectsRtpmapAndFormat(t *testing.T) {
	mb := newVideoMediaBlock()
	mb.DeclareCapability(ModuleH265, ModuleAAC, ModuleG7221)

	// All three payload types must appear in the m= line format list.
	wantFormats := []string{"96", "97", "99", "100"}
	if len(mb.MediaName.Formats) != len(wantFormats) {
		t.Fatalf("Formats len = %d, want %d (got %v)", len(mb.MediaName.Formats), len(wantFormats), mb.MediaName.Formats)
	}
	for i, want := range wantFormats {
		if mb.MediaName.Formats[i] != want {
			t.Errorf("Formats[%d] = %q, want %q", i, mb.MediaName.Formats[i], want)
		}
	}

	// Each module must produce an a=rtpmap: line.
	rtpmapLines := collectRtpmapLines(mb)
	if len(rtpmapLines) != 3 {
		t.Fatalf("rtpmap lines = %d, want 3", len(rtpmapLines))
	}
	want := map[string]bool{
		"100 H265/90000":           false,
		"97 MPEG4-GENERIC/16000/2": false,
		"99 G7221/16000":           false,
	}
	for _, line := range rtpmapLines {
		if _, ok := want[line]; ok {
			want[line] = true
		}
	}
	for line, found := range want {
		if !found {
			t.Errorf("missing rtpmap line %q", line)
		}
	}
}

func TestDeclareCapability_Idempotent(t *testing.T) {
	mb := newVideoMediaBlock()
	mb.DeclareCapability(ModuleH265)
	mb.DeclareCapability(ModuleH265) // second call must be a no-op

	if len(mb.MediaName.Formats) != 2 {
		t.Errorf("Formats len = %d, want 2 (idempotent)", len(mb.MediaName.Formats))
	}
	rtpmapLines := collectRtpmapLines(mb)
	if len(rtpmapLines) != 1 {
		t.Errorf("rtpmap lines = %d, want 1 (idempotent)", len(rtpmapLines))
	}
}

func TestDeclareCapability_NilBlock(t *testing.T) {
	var mb *MediaBlock
	mb.DeclareCapability(ModuleH265) // must not panic
}

func TestDeclareCapability_UnknownModuleSkipped(t *testing.T) {
	mb := newVideoMediaBlock()
	mb.DeclareCapability("unknown", ModuleAAC)

	wantFormats := []string{"96", "97"}
	if len(mb.MediaName.Formats) != len(wantFormats) {
		t.Fatalf("Formats len = %d, want %d (unknown skipped)", len(mb.MediaName.Formats), len(wantFormats))
	}
}

func TestDeclareCapability_PayloadTypeOrdering(t *testing.T) {
	mb := newVideoMediaBlock()
	// Pass in reverse order to force sorting.
	mb.DeclareCapability(ModuleG7221, ModuleAAC, ModuleH265)

	wantFormats := []string{"96", "97", "99", "100"}
	for i, want := range wantFormats {
		if mb.MediaName.Formats[i] != want {
			t.Errorf("Formats[%d] = %q, want %q (ascending)", i, mb.MediaName.Formats[i], want)
		}
	}
}

func TestDeclareCapability_IgnoredFor2016(t *testing.T) {
	// Reuse an existing media block with only PS payload (96) and assert
	// that calling DeclareCapability with no modules leaves it untouched.
	mb := newVideoMediaBlock()
	mb.DeclareCapability()

	if len(mb.MediaName.Formats) != 1 || mb.MediaName.Formats[0] != "96" {
		t.Errorf("no-module call mutated Formats = %v", mb.MediaName.Formats)
	}
	if len(mb.Attributes) != 0 {
		t.Errorf("no-module call mutated Attributes = %v", mb.Attributes)
	}
}

// --- Marshal 集成 (任务 §2.1) -------------------------------------------------

// --- 辅助 ------------------------------------------------------------------

func collectRtpmapLines(mb *MediaBlock) []string {
	var lines []string
	for _, a := range mb.Attributes {
		if a.Key == "rtpmap" {
			lines = append(lines, a.Value)
		}
	}
	return lines
}
