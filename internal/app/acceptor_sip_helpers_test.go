package app

import (
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// A relayed request carries one Via per hop, and RFC 3261 §8.2.6 requires the
// answer to copy all of them in order. Keeping only the first left the last
// relay with no Via to strip, so its forwarded answer could not be sent at all.
func TestBuildResponse_CopiesEveryVia(t *testing.T) {
	req, err := model.NewRequest("MESSAGE", "sip:34020000001310000001@3402000000", []model.Header{
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15062;branch=z9hG4bK-small"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15061;branch=z9hG4bK-large"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-probe"),
		model.NewHeader("From", "<sip:34020000002000000009@127.0.0.1>;tag=probe"),
		model.NewHeader("To", "<sip:34020000001310000001@3402000000>"),
		model.NewHeader("Call-ID", "relay-via"),
		model.NewHeader("CSeq", "1 MESSAGE"),
	}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := buildResponse(200, "OK", req, nil, "")
	if err != nil {
		t.Fatalf("buildResponse: %v", err)
	}

	var vias []string
	for _, h := range resp.Headers() {
		if h.Name() == "Via" {
			vias = append(vias, h.Value())
		}
	}
	want := []string{
		"SIP/2.0/UDP 127.0.0.1:15062;branch=z9hG4bK-small",
		"SIP/2.0/UDP 127.0.0.1:15061;branch=z9hG4bK-large",
		"SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-probe",
	}
	if len(vias) != len(want) {
		t.Fatalf("response carries %d Via headers, want %d: %v", len(vias), len(want), vias)
	}
	for i := range want {
		if vias[i] != want[i] {
			t.Errorf("Via[%d] = %q, want %q", i, vias[i], want[i])
		}
	}
}
