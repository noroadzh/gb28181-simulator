package app

// RFC 3261 exception flows: CANCEL and requests on an established dialog.
//
// CANCEL: while the INVITE has no final response, answer the CANCEL 200 OK
// and end the INVITE with 487 Request Terminated; afterwards the transaction
// is gone and the CANCEL gets 481. This UAS answers synchronously, so 481 is
// the ordinary outcome and 487 needs the transaction to still be pending.
//
// BYE and re-INVITE arrive inside an established dialog and must be handled
// by the dialog owner, not refused as unsupported methods.

import (
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

func cancelRequest(t *testing.T, uri, callID, peer string) model.Message {
	t.Helper()
	msg, err := model.NewRequest("CANCEL", uri, []model.Header{
		model.NewHeader("From", "<sip:platform@3402000000>;tag=p-cancel"),
		model.NewHeader("To", "<"+uri+">"),
		model.NewHeader("Call-ID", callID),
		model.NewHeader("CSeq", "1 CANCEL"),
		model.NewHeader("Via", "SIP/2.0/UDP "+peer+";branch=z9hG4bK-cancel"),
	}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return msg
}

// A CANCEL that names no live transaction is answered 481, not 200: the
// platform must learn that its INVITE is already finished.
func TestAcceptor_CancelWithoutPendingInviteIs481(t *testing.T) {
	_, tr, _, _, _ := acceptorFixture(t, &fakeAuthenticator{})
	resp := tr.deliver(t, cancelRequest(t, "sip:34020000002000000001@3402000000", "cancel-none", "127.0.0.1:15060"))

	if resp.StatusCode() != 481 {
		t.Fatalf("status = %d, want 481", resp.StatusCode())
	}
}

// While the INVITE transaction is pending, the CANCEL is answered 200 and
// the INVITE ends with 487 Request Terminated carrying its own CSeq.
func TestAcceptor_CancelTerminatesPendingInvite(t *testing.T) {
	acceptor, tr, _, _, nodeID := acceptorFixture(t, &fakeAuthenticator{})
	invite := inviteRequest(t, "34020000011310000001", "cancel-call-1", validSDP)
	acceptor.markPendingInvite(nodeID, "cancel-call-1", invite, "127.0.0.1:15060")

	resp := tr.deliver(t, cancelRequest(t, "sip:34020000002000000001@3402000000", "cancel-call-1", "127.0.0.1:15060"))
	if resp.StatusCode() != 200 {
		t.Fatalf("CANCEL status = %d, want 200", resp.StatusCode())
	}

	var terminated model.Message
	found := false
	for _, sent := range tr.delivered() {
		if sent.StatusCode() == 487 && headerValue(sent, "Call-ID") == "cancel-call-1" {
			terminated, found = sent, true
		}
	}
	if !found {
		t.Fatalf("no 487 was sent for the cancelled INVITE; sent %v", tr.delivered())
	}
	if got := headerValue(terminated, "CSeq"); got != "1 INVITE" {
		t.Errorf("487 CSeq = %q, want the INVITE's \"1 INVITE\"", got)
	}
	if _, ok := acceptor.takePendingInvite(nodeID, "cancel-call-1"); ok {
		t.Error("the cancelled INVITE is still listed as pending")
	}
}

// A device must answer BYE so the platform can close the dialog.
func TestDevice_AnswersByeWith200(t *testing.T) {
	deviceID := mustDeviceNodeID(t, "34020000001310000021")
	h := newDeviceHarness(t, deviceID, nil, nil)

	bye, err := model.NewRequest("BYE", "sip:"+deviceID.String()+"@3402000000", []model.Header{
		model.NewHeader("From", "<sip:platform@3402000000>;tag=p-bye"),
		model.NewHeader("To", "<sip:"+deviceID.String()+"@3402000000>"),
		model.NewHeader("Call-ID", "bye-call-1"),
		model.NewHeader("CSeq", "2 BYE"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-bye"),
	}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if resp := h.tr.deliver(t, bye); resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
}

// A re-INVITE inside an established video dialog renegotiates media: the
// answer is fresh and the previous sending socket is closed.
func TestDevice_ReInviteReplacesVideoSink(t *testing.T) {
	deviceID := mustDeviceNodeID(t, "34020000001310000022")
	mediaCfg := model.MediaConfig{Kind: model.SourceKindSynthetic, FPS: 25}
	h := newDeviceHarness(t, deviceID, nil, &mediaCfg)
	h.acceptor.WithMediaService(syntheticMediaService(t))

	first := h.tr.deliver(t, videoInvite(t, deviceID, "reinvite-1", videoOffer(t, 40010)))
	if first.StatusCode() != 200 {
		t.Fatalf("first INVITE status = %d", first.StatusCode())
	}
	h.acceptor.deviceTalksMu.Lock()
	oldSink := h.acceptor.deviceVideos["reinvite-1"]
	h.acceptor.deviceTalksMu.Unlock()
	if oldSink == nil {
		t.Fatal("no sink recorded for the first INVITE")
	}

	reinvite, err := model.NewRequest("INVITE", "sip:"+videoChannelID+"@3402000000", []model.Header{
		model.NewHeader("From", "<sip:34020000002000000001@3402000000>;tag=platform"),
		model.NewHeader("To", "<sip:"+videoChannelID+"@3402000000>;tag=dev-1"),
		model.NewHeader("Call-ID", "reinvite-1"),
		model.NewHeader("CSeq", "2 INVITE"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-reinvite"),
		model.NewHeader("Content-Type", "application/sdp"),
	}, videoOffer(t, 40011))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if resp := h.tr.deliver(t, reinvite); resp.StatusCode() != 200 {
		t.Fatalf("re-INVITE status = %d, want 200", resp.StatusCode())
	}

	h.acceptor.deviceTalksMu.Lock()
	newSink := h.acceptor.deviceVideos["reinvite-1"]
	h.acceptor.deviceTalksMu.Unlock()
	if newSink == nil {
		t.Fatal("the re-INVITE left no sink in place")
	}
	if newSink == oldSink {
		t.Fatal("the re-INVITE kept the previous socket instead of renegotiating")
	}
	if err := oldSink.Write([]byte{0x80}); err == nil {
		t.Error("the previous sending socket is still open")
	}
}

// A re-INVITE on a platform node replaces the dialog's pipeline instead of
// leaving the old one streaming.
func TestAcceptor_ReInviteReplacesPipeline(t *testing.T) {
	svc := NewMediaService(
		func(model.MediaConfig) port.MediaSource { return nil },
		func() port.PSPacketizer { return nil },
		func(int) port.RTPizer { return nil },
		discardLogger(),
	)
	svc.SetInboundFactories(
		func(uint32) port.RTPDeizer { return &stubRTPDeizer{} },
		func() port.PSDepacketizer { return &stubPSDePacketizer{} },
	)
	h, _ := newMessageHarnessWithRegistryAndMedia(t, svc)

	if resp := h.tr.deliver(t, inviteRequest(t, "34020000011310000001", "reinvite-platform-1", validSDP)); resp.StatusCode() != 200 {
		t.Fatalf("first INVITE status = %d", resp.StatusCode())
	}
	h.acceptor.mu.Lock()
	firstEntry := h.acceptor.pipelines["reinvite-platform-1"]
	h.acceptor.mu.Unlock()
	if firstEntry == nil {
		t.Fatal("no pipeline after the first INVITE")
	}

	reinvite, err := model.NewRequest("INVITE", "sip:34020000011310000001@3402000000", []model.Header{
		model.NewHeader("From", "<sip:34020000011310000001@3402000000>;tag=inv1"),
		model.NewHeader("To", "<sip:34020000002000000001@3402000000>;tag=plat-1"),
		model.NewHeader("Call-ID", "reinvite-platform-1"),
		model.NewHeader("CSeq", "2 INVITE"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-reinvite-plat"),
		model.NewHeader("Contact", "<sip:34020000011310000001@127.0.0.1:15060>"),
		model.NewHeader("Content-Type", "application/sdp"),
	}, validSDP)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if resp := h.tr.deliver(t, reinvite); resp.StatusCode() != 200 {
		t.Fatalf("re-INVITE status = %d, want 200", resp.StatusCode())
	}

	closed := make(chan struct{})
	go func() {
		defer close(closed)
		_ = firstEntry.Close()
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("the first pipeline was not closed by the re-INVITE")
	}
	h.acceptor.mu.Lock()
	secondEntry := h.acceptor.pipelines["reinvite-platform-1"]
	h.acceptor.mu.Unlock()
	if secondEntry == nil || secondEntry == firstEntry {
		t.Error("the re-INVITE did not install a new pipeline")
	}
}
