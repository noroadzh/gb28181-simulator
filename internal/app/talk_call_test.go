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

// talkHarness serves a device node and gives a NodeService the acceptor it
// talks through. The service is the one under test; the acceptor is the
// production implementation with a scripted transport.
type talkHarness struct {
	svc      *NodeService
	acceptor *Acceptor
	tr       *acceptorTransport
	device   model.NodeID
}

func newTalkHarness(t *testing.T) *talkHarness {
	t.Helper()
	deviceID := mustParse(t, "34020000001310000001")
	d := newDeviceHarness(t, deviceID, nil, nil)

	svc, err := NewNodeService(d.reg, noopLifecycle{}, d.reg,
		func(string, model.NodeID) (port.SIPTransport, error) {
			return nil, errors.New("no transport in this test")
		},
		&fakeClock{now: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("NewNodeService: %v", err)
	}
	if _, err := svc.WithAcceptor(d.acceptor); err != nil {
		t.Fatalf("WithAcceptor: %v", err)
	}
	d.acceptor.controlWait = 200 * time.Millisecond
	return &talkHarness{svc: svc, acceptor: d.acceptor, tr: d.tr, device: deviceID}
}

// noopLifecycle satisfies the constructor without binding anything: talk does
// not start or stop nodes.
type noopLifecycle struct{}

func (noopLifecycle) Start(context.Context, model.NodeID) error { return nil }
func (noopLifecycle) Stop(context.Context, model.NodeID) error  { return nil }
func (noopLifecycle) Status(context.Context, model.NodeID) (model.Status, error) {
	return model.StatusOnline, nil
}
func (noopLifecycle) Transport(model.NodeID) port.SIPTransport        { return nil }
func (noopLifecycle) Fail(context.Context, model.NodeID, error) error { return nil }

// answer builds the 200 OK for the INVITE the service sent, echoing its
// transaction identity.
func (h *talkHarness) answer(t *testing.T, invite model.Message) {
	t.Helper()
	resp, err := model.NewResponse(200, "OK", []model.Header{
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-talk"),
		model.NewHeader("Call-ID", headerValue(invite, "Call-ID")),
		model.NewHeader("CSeq", headerValue(invite, "CSeq")),
	}, "")
	if err != nil {
		t.Fatalf("NewResponse: %v", err)
	}
	h.tr.inbound <- resp
}

// Task 1.1: the offer carries a PCMU audio stream, and a 200 OK yields a
// session id plus the WebSocket URL the browser will use.
func TestTalkStart_InvitesWithPCMUAndReturnsSession(t *testing.T) {
	h := newTalkHarness(t)
	ctx := context.Background()

	type result struct {
		session, wsURL string
		err            error
	}
	done := make(chan result, 1)
	go func() {
		session, wsURL, err := h.svc.TalkStart(ctx, h.device, "ch-1")
		done <- result{session, wsURL, err}
	}()

	invite := waitForSend(t, h.tr)
	if invite.Method() != "INVITE" {
		t.Fatalf("sent %q, want INVITE", invite.Method())
	}
	body := invite.Body()
	for _, want := range []string{"m=audio ", "RTP/AVP 0", "s=Talk"} {
		if !strings.Contains(body, want) {
			t.Errorf("SDP offer is missing %q:\n%s", want, body)
		}
	}
	if got := headerValue(invite, "Content-Type"); got != "application/sdp" {
		t.Errorf("Content-Type = %q, want application/sdp", got)
	}

	h.answer(t, invite)
	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("TalkStart: %v", res.err)
		}
		if res.session == "" {
			t.Error("TalkStart returned an empty session id")
		}
		if !strings.Contains(res.wsURL, res.session) {
			t.Errorf("wsURL = %q, want it to carry the session id", res.wsURL)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("TalkStart never returned")
	}
}

// Task 1.3: stopping sends BYE for the same dialog, and a repeated stop is a
// no-op rather than an error.
func TestTalkStop_SendsByeAndIsIdempotent(t *testing.T) {
	h := newTalkHarness(t)
	ctx := context.Background()

	type result struct {
		session string
		err     error
	}
	done := make(chan result, 1)
	go func() {
		session, _, err := h.svc.TalkStart(ctx, h.device, "ch-1")
		done <- result{session, err}
	}()
	invite := waitForSend(t, h.tr)
	h.answer(t, invite)
	res := <-done
	if res.err != nil {
		t.Fatalf("TalkStart: %v", res.err)
	}

	if err := h.svc.TalkStop(ctx, h.device, "ch-1", res.session); err != nil {
		t.Fatalf("TalkStop: %v", err)
	}
	bye := waitForMethod(t, h.tr, "BYE")
	if got := headerValue(bye, "Call-ID"); got != res.session {
		t.Errorf("BYE Call-ID = %q, want the talk session %q", got, res.session)
	}

	if err := h.svc.TalkStop(ctx, h.device, "ch-1", res.session); err != nil {
		t.Errorf("a repeated stop returned %v, want nil", err)
	}
}

// Task 1.4: a device that never answers is a timeout, not a hang.
func TestTalkStart_TimeoutIsDistinguishable(t *testing.T) {
	h := newTalkHarness(t)
	h.acceptor.controlWait = 30 * time.Millisecond

	_, _, err := h.svc.TalkStart(context.Background(), h.device, "ch-1")
	if !errors.Is(err, port.ErrControlTimeout) {
		t.Fatalf("err = %v, want ErrControlTimeout", err)
	}
}

// Task 1.4: a platform node has no microphone to open.
func TestTalkStart_NonDeviceIsUnsupported(t *testing.T) {
	h := newTalkHarness(t)
	platformID := mustParse(t, "34020000002000000001")
	if _, err := h.svc.Create(context.Background(),
		testProfile(t, platformID.String(), "127.0.0.1:15062")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, _, err := h.svc.TalkStart(context.Background(), platformID, "ch-1")
	if !errors.Is(err, port.ErrTalkUnsupported) {
		t.Fatalf("err = %v, want ErrTalkUnsupported", err)
	}
}

// waitForMethod returns the first message the transport sent with method.
func waitForMethod(t *testing.T, tr *acceptorTransport, method string) model.Message {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, msg := range tr.delivered() {
			if msg.Method() == method {
				return msg
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("no %s was sent", method)
	return model.Message{}
}

// Task 1.4: a transport failure is 502, not a timeout.
func TestTalkStart_SendFailureIsUnreachable(t *testing.T) {
	h := newTalkHarness(t)
	h.tr.failSend = errors.New("network down")

	_, _, err := h.svc.TalkStart(context.Background(), h.device, "ch-1")
	if !errors.Is(err, port.ErrControlTargetUnreachable) {
		t.Fatalf("err = %v, want ErrControlTargetUnreachable", err)
	}
}

func talkSessionCount(svc *NodeService) int {
	svc.talk.mu.Lock()
	defer svc.talk.mu.Unlock()
	return len(svc.talk.calls)
}

// startTalk begins a session and answers the INVITE, leaving one live call.
func startTalk(t *testing.T, h *talkHarness) string {
	t.Helper()
	type result struct {
		session string
		err     error
	}
	done := make(chan result, 1)
	go func() {
		session, _, err := h.svc.TalkStart(context.Background(), h.device, "ch-1")
		done <- result{session, err}
	}()
	invite := waitForSend(t, h.tr)
	h.answer(t, invite)
	res := <-done
	if res.err != nil {
		t.Fatalf("TalkStart: %v", res.err)
	}
	return res.session
}

// Task 1.3: a session nobody closes is reclaimed, and the device is told.
func TestTalk_IdleSessionReclaimsItself(t *testing.T) {
	h := newTalkHarness(t)
	h.svc.talkIdle = 20 * time.Millisecond
	startTalk(t, h)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if talkSessionCount(h.svc) == 0 {
			waitForMethod(t, h.tr, "BYE")
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the idle talk session was never reclaimed")
}

// Task 1.3: the device may hang up first; the local session must go away
// without waiting for the browser to stop.
func TestTalk_PeerByeReclaims(t *testing.T) {
	h := newTalkHarness(t)
	session := startTalk(t, h)

	bye, err := model.NewRequest("BYE", "sip:34020000002000000001@3402000000", []model.Header{
		model.NewHeader("From", "<sip:34020000001310000001@3402000000>;tag=device"),
		model.NewHeader("To", "<sip:34020000002000000001@3402000000>;tag=platform"),
		model.NewHeader("Call-ID", session),
		model.NewHeader("CSeq", "3 BYE"),
	}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	h.tr.inbound <- bye

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if talkSessionCount(h.svc) == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("a peer BYE left the talk session registered")
}
