package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

func playbackAnswer(t *testing.T, invite model.Message, status int) model.Message {
	t.Helper()
	body := ""
	if status < 300 {
		start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)
		var err error
		body, err = model.PlaybackSDPBytes(model.PlaybackSDP{
			DeviceID: "ch-1", SN: "777", ConnIP: "127.0.0.1", RTPPort: 41000,
			Start: start, End: start.Add(5 * time.Minute), Mode: "sendonly",
		})
		if err != nil {
			t.Fatalf("PlaybackSDPBytes: %v", err)
		}
	}
	resp, err := model.NewResponse(status, "Status", []model.Header{
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-play"),
		model.NewHeader("Call-ID", headerValue(invite, "Call-ID")),
		model.NewHeader("CSeq", headerValue(invite, "CSeq")),
	}, body)
	if err != nil {
		t.Fatalf("NewResponse: %v", err)
	}
	return resp
}

// Task 1.4: the play request becomes a real playback INVITE carrying the
// window, and the session is recorded for the FLV endpoint.
func TestPlaybackStream_SendsInviteWithWindow(t *testing.T) {
	h := newTalkHarness(t)
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)
	end := start.Add(5 * time.Minute)

	done := make(chan error, 1)
	go func() {
		done <- h.svc.StartPlaybackStream(context.Background(), h.device, "ch-1", "777", start, end)
	}()

	invite := waitForSend(t, h.tr)
	if invite.Method() != "INVITE" {
		t.Fatalf("sent %q, want INVITE", invite.Method())
	}
	for _, want := range []string{
		"s=Playback",
		"u=ch-1:777",
		"t=2026-10-01T00:00:00 2026-10-01T00:05:00",
		"m=video ",
		"a=recvonly",
	} {
		if !strings.Contains(invite.Body(), want) {
			t.Errorf("offer is missing %q:\n%s", want, invite.Body())
		}
	}

	h.tr.inbound <- playbackAnswer(t, invite, 200)
	if err := <-done; err != nil {
		t.Fatalf("StartPlaybackStream: %v", err)
	}

	s, e, ok := h.svc.PlaybackWindow("777")
	if !ok {
		t.Fatal("the playback session was not recorded")
	}
	if !s.Equal(start) || !e.Equal(end) {
		t.Errorf("window = %s..%s, want %s..%s", s, e, start, end)
	}
}

// A device that refuses the dialog reports the refusal, and no session is
// recorded.
func TestPlaybackStream_RefusalIsReported(t *testing.T) {
	h := newTalkHarness(t)
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)

	done := make(chan error, 1)
	go func() {
		done <- h.svc.StartPlaybackStream(context.Background(), h.device, "ch-1", "778", start, start.Add(time.Minute))
	}()
	invite := waitForSend(t, h.tr)
	h.tr.inbound <- playbackAnswer(t, invite, 404)

	err := <-done
	if !errors.Is(err, port.ErrPlaybackUnsupported) {
		t.Fatalf("err = %v, want ErrPlaybackUnsupported", err)
	}
	if _, _, ok := h.svc.PlaybackWindow("778"); ok {
		t.Error("a refused playback was still recorded as a session")
	}
}
