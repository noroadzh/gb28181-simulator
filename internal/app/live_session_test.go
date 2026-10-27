package app

// Tasks 1.3 / 2.x: the platform's live pull against a scripted device.

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/adapter/media"
	"github.com/your-org/gb28181-simulator/internal/adapter/nodereg"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

const liveTestDevice = "34020000001310000001"

type liveHarness struct {
	svc *NodeService
	h   *messageHarness
	reg *nodereg.Registry
}

func newLiveHarness(t *testing.T) *liveHarness {
	t.Helper()
	h, reg := newMessageHarnessWithRegistryAndMedia(t, nil)
	svc, err := NewNodeService(reg, noopLifecycle{}, reg,
		func(string, model.NodeID) (port.SIPTransport, error) {
			return nil, errors.New("no transport in this test")
		},
		&fakeClock{now: time.Now()})
	if err != nil {
		t.Fatalf("NewNodeService: %v", err)
	}
	if _, err := svc.WithLogger(discardLogger()); err != nil {
		t.Fatalf("WithLogger: %v", err)
	}
	if _, err := svc.WithAcceptor(h.acceptor); err != nil {
		t.Fatalf("WithAcceptor: %v", err)
	}
	if _, err := svc.WithLiveDeizer(func(ssrc uint32) port.RTPDeizer { return media.NewRTPDeizer(ssrc) }); err != nil {
		t.Fatalf("WithLiveDeizer: %v", err)
	}
	h.acceptor.controlWait = 200 * time.Millisecond
	return &liveHarness{svc: svc, h: h, reg: reg}
}

// registerDevice seeds the platform's online device table.
func (l *liveHarness) registerDevice(t *testing.T, deviceID, addr string) {
	t.Helper()
	dev, err := model.NewDownstreamDevice(model.DownstreamDeviceParams{
		DeviceID: deviceID, Addr: addr, Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("NewDownstreamDevice: %v", err)
	}
	if err := l.h.devices.Upsert(context.Background(), l.h.nodeID, dev); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
}

type liveStartResult struct {
	info port.LiveSessionInfo
	err  error
}

// startLive runs StartLive while answering the INVITE it sends. The answer
// body is built by the test so each case can shape the device's response.
func (l *liveHarness) startLive(t *testing.T, channelID, transport string, answer func(invite model.Message) model.Message) (port.LiveSessionInfo, model.Message, error) {
	t.Helper()
	done := make(chan liveStartResult, 1)
	go func() {
		info, err := l.svc.StartLive(context.Background(), l.h.nodeID, channelID, transport)
		done <- liveStartResult{info, err}
	}()
	var invite model.Message
	if answer != nil {
		invite = waitForSend(t, l.h.tr)
		if invite.Method() != "INVITE" {
			t.Fatalf("sent %q, want INVITE", invite.Method())
		}
		l.h.tr.inbound <- answer(invite)
	}
	select {
	case res := <-done:
		return res.info, invite, res.err
	case <-time.After(3 * time.Second):
		t.Fatal("StartLive never returned")
		return port.LiveSessionInfo{}, model.Message{}, nil
	}
}

func udpAnswer(ssrc uint32) func(model.Message) model.Message {
	return func(invite model.Message) model.Message {
		body := fmt.Sprintf("v=0\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\n"+
			"m=video 40002 RTP/AVP 96\r\na=sendonly\r\ny=%010d\r\n", ssrc)
		resp, err := model.NewResponse(200, "OK", []model.Header{
			model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-live-udp"),
			model.NewHeader("Call-ID", headerValue(invite, "Call-ID")),
			model.NewHeader("CSeq", headerValue(invite, "CSeq")),
			model.NewHeader("Content-Type", "application/sdp"),
		}, body)
		if err != nil {
			panic(err)
		}
		return resp
	}
}

func rtpDatagram(ssrc uint32, payload []byte) []byte {
	return model.RTPPacket{
		Sequence: 1, SSRC: ssrc, PayloadType: 96, Marker: true,
		Timestamp: 90000, Payload: payload,
	}.Bytes()
}

func waitForFrame(t *testing.T, ch <-chan model.PSFrame) model.PSFrame {
	t.Helper()
	select {
	case ps, ok := <-ch:
		if !ok {
			t.Fatal("the PS stream closed before a frame arrived")
		}
		return ps
	case <-time.After(2 * time.Second):
		t.Fatal("no PS frame reached the subscriber")
		return model.PSFrame{}
	}
}

func TestStartLive_UDPReceivesPSFrames(t *testing.T) {
	l := newLiveHarness(t)
	l.registerDevice(t, liveTestDevice, "127.0.0.1:15060")

	info, _, err := l.startLive(t, "ch-1", "udp", udpAnswer(12345))
	if err != nil {
		t.Fatalf("StartLive: %v", err)
	}
	if info.Transport != "udp" || info.LocalPort == 0 || info.SSRC != 12345 {
		t.Fatalf("info = %+v", info)
	}

	frames, cleanup, ok := l.svc.LivePS(context.Background(), l.h.nodeID, "ch-1")
	if !ok {
		t.Fatal("no live session for the channel")
	}
	defer cleanup()

	conn, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", info.LocalPort))
	if err != nil {
		t.Fatalf("dial live port: %v", err)
	}
	defer func() { _ = conn.Close() }()
	payload := []byte{0x00, 0x00, 0x01, 0xBA, 0x21, 0x22}
	if _, err := conn.Write(rtpDatagram(12345, payload)); err != nil {
		t.Fatalf("write RTP: %v", err)
	}
	ps := waitForFrame(t, frames)
	if !bytes.Equal(ps.Payload, payload) {
		t.Errorf("PS payload = % X, want % X", ps.Payload, payload)
	}
}

func TestStartLive_FiltersForeignSSRC(t *testing.T) {
	l := newLiveHarness(t)
	l.registerDevice(t, liveTestDevice, "127.0.0.1:15060")

	info, _, err := l.startLive(t, "ch-1", "udp", udpAnswer(12345))
	if err != nil {
		t.Fatalf("StartLive: %v", err)
	}
	frames, cleanup, ok := l.svc.LivePS(context.Background(), l.h.nodeID, "ch-1")
	if !ok {
		t.Fatal("no live session")
	}
	defer cleanup()

	conn, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", info.LocalPort))
	if err != nil {
		t.Fatalf("dial live port: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write(rtpDatagram(999, []byte{0xDE, 0xAD})); err != nil {
		t.Fatalf("write RTP: %v", err)
	}

	select {
	case ps := <-frames:
		t.Fatalf("a foreign-SSRC packet produced a frame: % X", ps.Payload)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestStartLive_TCPReceivesLengthPrefixedPS(t *testing.T) {
	l := newLiveHarness(t)
	l.registerDevice(t, liveTestDevice, "127.0.0.1:15060")

	answer := func(invite model.Message) model.Message {
		body := "v=0\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\n" +
			"m=video 0 TCP/RTP/AVP 96\r\na=sendonly\r\na=setup:active\r\ny=0000000000\r\n"
		resp, err := model.NewResponse(200, "OK", []model.Header{
			model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-live-tcp"),
			model.NewHeader("Call-ID", headerValue(invite, "Call-ID")),
			model.NewHeader("CSeq", headerValue(invite, "CSeq")),
			model.NewHeader("Content-Type", "application/sdp"),
		}, body)
		if err != nil {
			panic(err)
		}
		return resp
	}

	info, invite, err := l.startLive(t, "ch-1", "tcp", answer)
	if err != nil {
		t.Fatalf("StartLive: %v", err)
	}
	if info.Transport != "tcp" || info.LocalPort == 0 {
		t.Fatalf("info = %+v", info)
	}
	if body := invite.Body(); !strings.Contains(body, "TCP/RTP/AVP") || !strings.Contains(body, "a=setup:passive") {
		t.Errorf("TCP offer does not advertise passive listening:\n%s", body)
	}

	frames, cleanup, ok := l.svc.LivePS(context.Background(), l.h.nodeID, "ch-1")
	if !ok {
		t.Fatal("no live session")
	}
	defer cleanup()

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", info.LocalPort))
	if err != nil {
		t.Fatalf("dial live tcp port: %v", err)
	}
	defer func() { _ = conn.Close() }()
	payload := []byte{0x00, 0x00, 0x01, 0xBA, 0x33}
	pkt := rtpDatagram(0, payload)
	var prefix [2]byte
	binary.BigEndian.PutUint16(prefix[:], uint16(len(pkt)))
	if _, err := conn.Write(append(prefix[:], pkt...)); err != nil {
		t.Fatalf("write framed RTP: %v", err)
	}
	if ps := waitForFrame(t, frames); !bytes.Equal(ps.Payload, payload) {
		t.Errorf("PS payload = % X, want % X", ps.Payload, payload)
	}
}

func TestStartLive_RefusalIsReported(t *testing.T) {
	l := newLiveHarness(t)
	l.registerDevice(t, liveTestDevice, "127.0.0.1:15060")

	_, _, err := l.startLive(t, "ch-1", "udp", func(invite model.Message) model.Message {
		resp, _ := model.NewResponse(404, "Not Found", []model.Header{
			model.NewHeader("Call-ID", headerValue(invite, "Call-ID")),
			model.NewHeader("CSeq", headerValue(invite, "CSeq")),
		}, "")
		return resp
	})
	if !errors.Is(err, port.ErrLiveRefused) {
		t.Fatalf("err = %v, want ErrLiveRefused", err)
	}
	if _, _, ok := l.svc.LivePS(context.Background(), l.h.nodeID, "ch-1"); ok {
		t.Error("a refused pull left a session behind")
	}
}

func TestStartLive_NoTargetIsReported(t *testing.T) {
	l := newLiveHarness(t)
	_, _, err := l.startLive(t, "ch-1", "udp", nil)
	if !errors.Is(err, port.ErrLiveNoTarget) {
		t.Fatalf("err = %v, want ErrLiveNoTarget", err)
	}
}

func TestStartLive_OfflineParentIsUnreachable(t *testing.T) {
	l := newLiveHarness(t)
	ch, err := model.NewChannel("34020000001310000001001", "lobby", "34020000001310000077", model.ChannelStatusOnline)
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}
	if _, err := l.reg.MutateProfile(context.Background(), l.h.nodeID, func(p model.NodeProfile) (model.NodeProfile, error) {
		return p.WithChannels([]model.Channel{ch})
	}); err != nil {
		t.Fatalf("MutateProfile: %v", err)
	}

	_, _, err = l.startLive(t, ch.ID(), "udp", nil)
	if !errors.Is(err, port.ErrControlTargetUnreachable) {
		t.Fatalf("err = %v, want ErrControlTargetUnreachable", err)
	}
}

func TestStartLive_DeviceNodeIsUnsupported(t *testing.T) {
	l := newLiveHarness(t)
	deviceID := mustDeviceNodeID(t, "34020000001310000031")
	profile, err := model.NewNodeProfile(deviceID.String(), "127.0.0.1:15061", "3402000000", "sim")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	if _, err := l.reg.Register(context.Background(), profile); err != nil {
		t.Fatalf("Register: %v", err)
	}
	_, err = l.svc.StartLive(context.Background(), deviceID, "ch-1", "udp")
	if !errors.Is(err, port.ErrLiveUnsupported) {
		t.Fatalf("err = %v, want ErrLiveUnsupported", err)
	}
}

func TestStopLive_SendsByeAndEndsStream(t *testing.T) {
	l := newLiveHarness(t)
	l.registerDevice(t, liveTestDevice, "127.0.0.1:15060")
	info, invite, err := l.startLive(t, "ch-1", "udp", udpAnswer(12345))
	if err != nil {
		t.Fatalf("StartLive: %v", err)
	}
	frames, cleanup, ok := l.svc.LivePS(context.Background(), l.h.nodeID, "ch-1")
	if !ok {
		t.Fatal("no live session")
	}
	defer cleanup()

	if err := l.svc.StopLive(context.Background(), l.h.nodeID, "ch-1"); err != nil {
		t.Fatalf("StopLive: %v", err)
	}
	if _, _, ok := l.svc.LivePS(context.Background(), l.h.nodeID, "ch-1"); ok {
		t.Error("the session survived StopLive")
	}
	select {
	case _, ok := <-frames:
		if ok {
			// Drain any frame that was already queued, then require the
			// closed channel.
			for range frames {
			}
		}
	case <-time.After(time.Second):
		t.Error("the subscriber's stream was not closed")
	}

	var bye model.Message
	for _, sent := range l.h.tr.delivered() {
		if sent.Method() == "BYE" && headerValue(sent, "Call-ID") == headerValue(invite, "Call-ID") {
			bye = sent
		}
	}
	if bye.Method() != "BYE" {
		t.Fatalf("no BYE for %s was sent", headerValue(invite, "Call-ID"))
	}
	_ = info
}

func TestStopLive_IsIdempotent(t *testing.T) {
	l := newLiveHarness(t)
	if err := l.svc.StopLive(context.Background(), l.h.nodeID, "nothing"); err != nil {
		t.Fatalf("StopLive on an idle channel: %v", err)
	}
}

func TestDeviceByeEndsLiveSession(t *testing.T) {
	l := newLiveHarness(t)
	l.registerDevice(t, liveTestDevice, "127.0.0.1:15060")
	_, invite, err := l.startLive(t, "ch-1", "udp", udpAnswer(12345))
	if err != nil {
		t.Fatalf("StartLive: %v", err)
	}

	bye, err := model.NewRequest("BYE", "sip:"+l.h.nodeID.String()+"@3402000000", []model.Header{
		model.NewHeader("From", "<sip:"+liveTestDevice+"@3402000000>;tag=dev"),
		model.NewHeader("To", "<sip:"+l.h.nodeID.String()+"@3402000000>"),
		model.NewHeader("Call-ID", headerValue(invite, "Call-ID")),
		model.NewHeader("CSeq", "2 BYE"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-live-bye"),
	}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if resp := l.h.tr.deliver(t, bye); resp.StatusCode() != 200 {
		t.Fatalf("BYE status = %d, want 200", resp.StatusCode())
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, _, ok := l.svc.LivePS(context.Background(), l.h.nodeID, "ch-1"); !ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the device's BYE left the live session running")
}

func TestNodeStopEndsLiveSessions(t *testing.T) {
	l := newLiveHarness(t)
	l.registerDevice(t, liveTestDevice, "127.0.0.1:15060")
	if _, _, err := l.startLive(t, "ch-1", "udp", udpAnswer(12345)); err != nil {
		t.Fatalf("StartLive: %v", err)
	}
	if err := l.svc.Stop(context.Background(), l.h.nodeID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, _, ok := l.svc.LivePS(context.Background(), l.h.nodeID, "ch-1"); ok {
		t.Fatal("stopping the node left a live session behind")
	}
}

// Task 2.4: a session that never produces media ends on its own, so a
// silent device cannot pin a port forever.
func TestLiveSession_EndsWithoutMedia(t *testing.T) {
	l := newLiveHarness(t)
	l.registerDevice(t, liveTestDevice, "127.0.0.1:15060")
	l.svc.liveIdle = 100 * time.Millisecond
	l.svc.liveWatch = 10 * time.Millisecond

	if _, _, err := l.startLive(t, "ch-1", "udp", udpAnswer(12345)); err != nil {
		t.Fatalf("StartLive: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, ok := l.svc.LivePS(context.Background(), l.h.nodeID, "ch-1"); !ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("a silent session never ended")
}

// Media flowing keeps the watchdog quiet.
func TestLiveSession_MediaKeepsSessionAlive(t *testing.T) {
	l := newLiveHarness(t)
	l.registerDevice(t, liveTestDevice, "127.0.0.1:15060")
	l.svc.liveIdle = 150 * time.Millisecond
	l.svc.liveWatch = 10 * time.Millisecond

	info, _, err := l.startLive(t, "ch-1", "udp", udpAnswer(12345))
	if err != nil {
		t.Fatalf("StartLive: %v", err)
	}
	frames, cleanup, ok := l.svc.LivePS(context.Background(), l.h.nodeID, "ch-1")
	if !ok {
		t.Fatal("no live session")
	}
	defer cleanup()

	conn, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", info.LocalPort))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	stop := time.Now().Add(600 * time.Millisecond)
	for time.Now().Before(stop) {
		if _, err := conn.Write(rtpDatagram(12345, []byte{0x00, 0x00, 0x01, 0xBA})); err != nil {
			t.Fatalf("write: %v", err)
		}
		select {
		case <-frames:
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, _, ok := l.svc.LivePS(context.Background(), l.h.nodeID, "ch-1"); !ok {
		t.Fatal("a session with media was reaped")
	}
}
