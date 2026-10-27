package app

// Task 1.2: the platform-side video INVITE transaction. It must hand back the
// device's answer (the caller reads c=/m=/y= out of it), report refusals as
// refusals, and time out like the other control transactions.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

func liveAnswer(t *testing.T, invite model.Message, status int, body string) model.Message {
	t.Helper()
	hdrs := []model.Header{
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-live"),
		model.NewHeader("Call-ID", headerValue(invite, "Call-ID")),
		model.NewHeader("CSeq", headerValue(invite, "CSeq")),
	}
	if body != "" {
		hdrs = append(hdrs, model.NewHeader("Content-Type", "application/sdp"))
	}
	resp, err := model.NewResponse(status, "OK", hdrs, body)
	if err != nil {
		t.Fatalf("NewResponse: %v", err)
	}
	return resp
}

func TestInviteVideo_ReturnsDeviceAnswer(t *testing.T) {
	h := newTalkHarness(t)
	offer, err := model.LiveOfferBytes(model.LiveOffer{
		ChannelID: "ch-1", ConnIP: "127.0.0.1", RTPPort: 40000,
	})
	if err != nil {
		t.Fatalf("LiveOfferBytes: %v", err)
	}

	type result struct {
		callID string
		answer model.Message
		err    error
	}
	done := make(chan result, 1)
	go func() {
		callID, answer, err := h.acceptor.InviteVideo(context.Background(), h.device,
			"34020000001310000001", "127.0.0.1:15060", "ch-1", offer)
		done <- result{callID, answer, err}
	}()

	invite := waitForSend(t, h.tr)
	if invite.Method() != "INVITE" {
		t.Fatalf("sent %q, want INVITE", invite.Method())
	}
	if !strings.Contains(invite.Body(), "m=video 40000 RTP/AVP 96") {
		t.Errorf("offer is missing the video line:\n%s", invite.Body())
	}
	if !strings.Contains(invite.Body(), "a=recvonly") {
		t.Errorf("offer is missing a=recvonly:\n%s", invite.Body())
	}

	h.tr.inbound <- liveAnswer(t, invite, 200,
		"v=0\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=video 40002 RTP/AVP 96\r\na=sendonly\r\ny=0100001234\r\n")

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("InviteVideo: %v", res.err)
		}
		if res.callID == "" {
			t.Error("InviteVideo returned an empty Call-ID")
		}
		answer, err := model.ParseLiveAnswer(res.answer.Body())
		if err != nil {
			t.Fatalf("ParseLiveAnswer: %v", err)
		}
		if answer.Port != 40002 || answer.SSRC != 100001234 {
			t.Errorf("answer = %+v, want port 40002 and SSRC 100001234", answer)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("InviteVideo never returned")
	}
}

func TestInviteVideo_RefusalIsReported(t *testing.T) {
	h := newTalkHarness(t)
	done := make(chan error, 1)
	go func() {
		_, _, err := h.acceptor.InviteVideo(context.Background(), h.device,
			"34020000001310000001", "127.0.0.1:15060", "ch-1", "v=0\r\n")
		done <- err
	}()

	invite := waitForSend(t, h.tr)
	h.tr.inbound <- liveAnswer(t, invite, 404, "")

	select {
	case err := <-done:
		if !errors.Is(err, port.ErrLiveRefused) {
			t.Fatalf("err = %v, want ErrLiveRefused", err)
		}
		if errors.Is(err, port.ErrLiveUnsupported) {
			t.Error("a refusal was reported as unsupported")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("InviteVideo never returned")
	}
}

func TestInviteVideo_Timeout(t *testing.T) {
	h := newTalkHarness(t)
	done := make(chan error, 1)
	go func() {
		_, _, err := h.acceptor.InviteVideo(context.Background(), h.device,
			"34020000001310000001", "127.0.0.1:15060", "ch-1", "v=0\r\n")
		done <- err
	}()
	waitForSend(t, h.tr)

	select {
	case err := <-done:
		if !errors.Is(err, port.ErrControlTimeout) {
			t.Fatalf("err = %v, want ErrControlTimeout", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("InviteVideo never timed out")
	}
}
