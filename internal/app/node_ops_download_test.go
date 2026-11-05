package app

// Platform StartDownload over UDP and 附录 D TCP.

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/noroadzh/gb28181-simulator/internal/adapter/recording"
	"github.com/noroadzh/gb28181-simulator/internal/domain/model"
)

func TestStartDownload_TCPReceivesLengthPrefixedPS(t *testing.T) {
	l := newLiveHarness(t)
	store, err := recording.New(t.TempDir(), time.Minute)
	if err != nil {
		t.Fatalf("recording.New: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := l.svc.WithRecordings(store); err != nil {
		t.Fatalf("WithRecordings: %v", err)
	}
	l.registerDevice(t, liveTestDevice, "127.0.0.1:15060")

	start := time.Now().Add(-time.Minute)
	end := time.Now().Add(30 * time.Second)

	type result struct {
		callID string
		err    error
	}
	done := make(chan result, 1)
	go func() {
		callID, err := l.svc.StartDownload(context.Background(), l.h.nodeID, "ch-1", start, end, "tcp")
		done <- result{callID, err}
	}()

	invite := waitForSend(t, l.h.tr)
	if invite.Method() != "INVITE" {
		t.Fatalf("sent %q, want INVITE", invite.Method())
	}
	body := invite.Body()
	if !strings.Contains(body, "s=Download") {
		t.Errorf("offer missing s=Download:\n%s", body)
	}
	if !strings.Contains(body, "TCP/RTP/AVP") || !strings.Contains(body, "a=setup:passive") {
		t.Errorf("TCP download offer does not advertise passive listening:\n%s", body)
	}
	offer, err := model.ParsePlaybackSDP(body)
	if err != nil {
		t.Fatalf("ParsePlaybackSDP: %v", err)
	}
	resp, err := model.NewResponse(200, "OK", []model.Header{
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-dl-tcp"),
		model.NewHeader("Call-ID", headerValue(invite, "Call-ID")),
		model.NewHeader("CSeq", headerValue(invite, "CSeq")),
		model.NewHeader("Content-Type", "application/sdp"),
	}, "v=0\r\n")
	if err != nil {
		t.Fatalf("NewResponse: %v", err)
	}
	l.h.tr.inbound <- resp

	var callID string
	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("StartDownload: %v", res.err)
		}
		callID = res.callID
	case <-time.After(3 * time.Second):
		t.Fatal("StartDownload never returned")
	}
	if callID == "" {
		t.Fatal("empty call_id")
	}

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", offer.RTPPort))
	if err != nil {
		t.Fatalf("dial download tcp port: %v", err)
	}
	defer func() { _ = conn.Close() }()
	payload := []byte{0x00, 0x00, 0x01, 0xBA, 0x44}
	pkt := rtpDatagram(0, payload)
	var prefix [2]byte
	binary.BigEndian.PutUint16(prefix[:], uint16(len(pkt)))
	if _, err := conn.Write(append(prefix[:], pkt...)); err != nil {
		t.Fatalf("write framed RTP: %v", err)
	}

	key := "download:ch-1"
	deadline := time.Now().Add(2 * time.Second)
	for {
		recs := store.List(context.Background(), l.h.nodeID, key, time.Time{}, time.Time{})
		if len(recs) == 1 && recs[0].Size > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("tcp download produced %d recordings", len(recs))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStartDownload_RejectsUnknownTransport(t *testing.T) {
	l := newLiveHarness(t)
	store, err := recording.New(t.TempDir(), time.Minute)
	if err != nil {
		t.Fatalf("recording.New: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := l.svc.WithRecordings(store); err != nil {
		t.Fatalf("WithRecordings: %v", err)
	}
	l.registerDevice(t, liveTestDevice, "127.0.0.1:15060")

	_, err = l.svc.StartDownload(context.Background(), l.h.nodeID, "ch-1",
		time.Now().Add(-time.Minute), time.Now().Add(time.Minute), "sctp")
	if err == nil {
		t.Fatal("expected unknown transport error")
	}
}
