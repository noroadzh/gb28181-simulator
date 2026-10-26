package app

import (
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// deviceInvite builds an INVITE carrying the given SDP body.
func deviceInvite(t *testing.T, deviceID model.NodeID, callID, sdp string) model.Message {
	t.Helper()
	req, err := model.NewRequest("INVITE", "sip:"+deviceID.String()+"@3402000000", []model.Header{
		model.NewHeader("From", "<sip:platform@3402000000>;tag=p-talk"),
		model.NewHeader("To", "<sip:"+deviceID.String()+"@3402000000>"),
		model.NewHeader("Call-ID", callID),
		model.NewHeader("CSeq", "1 INVITE"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-talk"),
		model.NewHeader("Contact", "<sip:platform@127.0.0.1:15060>"),
		model.NewHeader("Content-Type", "application/sdp"),
	}, sdp)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return req
}

const audioOfferSDP = "v=0\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio 4000 RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\n"
const videoOfferSDP = "v=0\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=video 4002 RTP/AVP 96\r\n"

// Task 1.1: the device answers a talk INVITE with its own RTP port in the
// answer — echoing the offer's port would point both sides at one socket.
func TestDevice_AnswersTalkInviteWithItsOwnPort(t *testing.T) {
	deviceID := mustDeviceNodeID(t, "34020000001310000003")
	h := newDeviceHarness(t, deviceID, nil, nil)

	resp := h.tr.deliver(t, deviceInvite(t, deviceID, "talk-call-1", audioOfferSDP))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200 (body %q)", resp.StatusCode(), resp.Body())
	}
	body := resp.Body()
	if !strings.Contains(body, "m=audio ") || !strings.Contains(body, "RTP/AVP 0") {
		t.Errorf("answer carries no PCMU audio line:\n%s", body)
	}
	if !strings.Contains(body, "c=IN IP4 127.0.0.1") {
		t.Errorf("answer carries no connection line:\n%s", body)
	}
	if strings.Contains(body, "m=audio 4000 ") {
		t.Errorf("answer echoed the offer's port instead of choosing one:\n%s", body)
	}

	h.acceptor.deviceTalksMu.Lock()
	_, registered := h.acceptor.deviceTalks["talk-call-1"]
	h.acceptor.deviceTalksMu.Unlock()
	if !registered {
		t.Error("the device did not register an audio port for the dialog")
	}
}

// Media the device cannot serve stays 488. Video used to be in this set; the
// interop-gaps change made the device answer live video INVITEs, so the
// refusal is checked with a media type it still does not serve.
func TestDevice_RefusesUnsupportedMediaInvite(t *testing.T) {
	deviceID := mustDeviceNodeID(t, "34020000001310000004")
	h := newDeviceHarness(t, deviceID, nil, nil)

	const applicationSDP = "v=0\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=application 4000 RTP/AVP 98\r\n"
	resp := h.tr.deliver(t, deviceInvite(t, deviceID, "app-call-1", applicationSDP))
	if resp.StatusCode() != 488 {
		t.Fatalf("status = %d, want 488", resp.StatusCode())
	}
}

// Task 1.1: ending the dialog releases the audio port.
func TestDevice_ByeClosesTalkAudioPort(t *testing.T) {
	deviceID := mustDeviceNodeID(t, "34020000001310000005")
	h := newDeviceHarness(t, deviceID, nil, nil)

	if resp := h.tr.deliver(t, deviceInvite(t, deviceID, "talk-call-2", audioOfferSDP)); resp.StatusCode() != 200 {
		t.Fatalf("INVITE status = %d, want 200", resp.StatusCode())
	}

	bye, err := model.NewRequest("BYE", "sip:"+deviceID.String()+"@3402000000", []model.Header{
		model.NewHeader("From", "<sip:platform@3402000000>;tag=p-talk"),
		model.NewHeader("To", "<sip:"+deviceID.String()+"@3402000000>"),
		model.NewHeader("Call-ID", "talk-call-2"),
		model.NewHeader("CSeq", "2 BYE"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-bye"),
	}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	h.tr.inbound <- bye

	deadline := time.Now().Add(2 * time.Second)
	for {
		h.acceptor.deviceTalksMu.Lock()
		_, still := h.acceptor.deviceTalks["talk-call-2"]
		h.acceptor.deviceTalksMu.Unlock()
		if !still {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the BYE left the talk audio port open")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
