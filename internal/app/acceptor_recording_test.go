package app

// Tasks 3.1 / 3.2: the platform answers RecordInfo and playback INVITEs from
// its recording library; the device keeps its fixed catalogue.

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/adapter/manscdp"
	"github.com/your-org/gb28181-simulator/internal/adapter/recording"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

func psPayload(marker byte) model.PSFrame {
	return model.PSFrame{Payload: []byte{0x00, 0x00, 0x01, 0xBA, marker, marker}}
}

func seededStore(t *testing.T, node model.NodeID, channelID string, frames ...model.PSFrame) *recording.Store {
	t.Helper()
	store, err := recording.New(t.TempDir(), time.Minute)
	if err != nil {
		t.Fatalf("recording.New: %v", err)
	}
	for _, fr := range frames {
		if err := store.Append(context.Background(), node, channelID, fr); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestPlatformRecordInfoAnswersFromLibrary(t *testing.T) {
	h, _ := newMessageHarnessWithRegistryAndMedia(t, nil)
	channel := "34020000001310000001001"
	store := seededStore(t, h.nodeID, channel, psPayload(0x11), psPayload(0x22))
	h.acceptor.WithRecordings(store)

	h.codec.setNotify(mustNotify(t, model.CmdTypeRecordInfo, channel, 21))
	h.codec.setRecordQuery(model.RecordInfoQuery{DeviceID: channel,
		StartTime: time.Now().Add(-time.Hour).Format("2006-01-02T15:04:05"),
		EndTime:   time.Now().Add(time.Hour).Format("2006-01-02T15:04:05"),
	})
	resp := h.tr.deliver(t, messageRequest(t, channel, "<Query/>"))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	if !strings.Contains(resp.Body(), "SumNum=1") {
		t.Errorf("the library has one recording (one append run), answer was:\n%s", resp.Body())
	}
}

func TestPlatformRecordInfoEmptyLibraryAnswersZero(t *testing.T) {
	h, _ := newMessageHarnessWithRegistryAndMedia(t, nil)
	store := seededStore(t, h.nodeID, "other-channel", psPayload(0x11))
	h.acceptor.WithRecordings(store)

	channel := "34020000001310000001001"
	h.codec.setNotify(mustNotify(t, model.CmdTypeRecordInfo, channel, 22))
	h.codec.setRecordQuery(model.RecordInfoQuery{DeviceID: channel})
	resp := h.tr.deliver(t, messageRequest(t, channel, "<Query/>"))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	if !strings.Contains(resp.Body(), "SumNum=0") {
		t.Errorf("an empty library must answer zero items, got:\n%s", resp.Body())
	}
}

// A device ignores the library: its catalogue is fixed and unrelated to what
// the platform pulled.
func TestDeviceRecordInfoIgnoresLibrary(t *testing.T) {
	deviceID := mustDeviceNodeID(t, "34020000001310000041")
	store := seededStore(t, deviceID, deviceID.String(), psPayload(0x11), psPayload(0x22))
	h := newDeviceHarness(t, deviceID, nil, nil)
	h.acceptor.WithRecordings(store)

	body, err := manscdp.NewMANSCDPCodec().MarshalRecordInfoQuery(
		model.NewRecordInfoQuery(deviceID.String(), 5, "20260101T000000", "20261231T000000"), 5)
	if err != nil {
		t.Fatalf("MarshalRecordInfoQuery: %v", err)
	}
	resp := h.tr.deliver(t, deviceMessageRequest(t, deviceID.String(), body))
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	if !strings.Contains(resp.Body(), "<SumNum>1</SumNum>") {
		t.Errorf("the device answered from the library instead of its catalogue:\n%s", resp.Body())
	}
}

func TestPlatformPlaybackInviteStreamsRecording(t *testing.T) {
	svc := syntheticMediaService(t)
	h, _ := newMessageHarnessWithRegistryAndMedia(t, svc)
	channel := "34020000001310000001001"
	payload := []byte{0x00, 0x00, 0x01, 0xBA, 0x77, 0x88}
	store := seededStore(t, h.nodeID, channel, model.PSFrame{Payload: payload})
	h.acceptor.WithRecordings(store)

	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()
	port := listener.LocalAddr().(*net.UDPAddr).Port

	start := time.Now().Add(-time.Minute)
	end := time.Now().Add(time.Minute)
	body, err := model.PlaybackSDPBytes(model.PlaybackSDP{
		DeviceID: channel, SN: "77", ConnIP: "127.0.0.1", RTPPort: port,
		Start: start, End: end,
	})
	if err != nil {
		t.Fatalf("PlaybackSDPBytes: %v", err)
	}
	req, err := model.NewRequest("INVITE", "sip:"+channel+"@3402000000", []model.Header{
		model.NewHeader("From", "<sip:34020000002000000001@3402000000>;tag=up"),
		model.NewHeader("To", "<sip:"+channel+"@3402000000>"),
		model.NewHeader("Call-ID", "playback-rec-1"),
		model.NewHeader("CSeq", "1 INVITE"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-pb-rec"),
		model.NewHeader("Contact", "<sip:34020000002000000001@127.0.0.1:15060>"),
		model.NewHeader("Content-Type", "application/sdp"),
	}, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp := h.tr.deliver(t, req)
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200 (body %q)", resp.StatusCode(), resp.Body())
	}
	for _, want := range []string{"s=Playback", "a=sendonly", "m=video "} {
		if !strings.Contains(resp.Body(), want) {
			t.Errorf("answer is missing %q:\n%s", want, resp.Body())
		}
	}

	buf := make([]byte, 2048)
	_ = listener.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, _, err := listener.ReadFrom(buf)
	if err != nil {
		t.Fatalf("no RTP reached the requester: %v", err)
	}
	if n < 16 || buf[0] != 0x80 {
		t.Fatalf("not an RTP v2 packet: % X", buf[:min(16, n)])
	}
	if !strings.HasPrefix(string(buf[12:n]), string(payload)) {
		t.Errorf("playback payload = % X, want the recorded PS frame", buf[12:n])
	}
}

func TestPlatformPlaybackInviteWithoutRecordingIs404(t *testing.T) {
	svc := syntheticMediaService(t)
	h, _ := newMessageHarnessWithRegistryAndMedia(t, svc)
	store := seededStore(t, h.nodeID, "other-channel", psPayload(0x11))
	h.acceptor.WithRecordings(store)

	channel := "34020000001310000001001"
	start := time.Now().Add(-time.Minute)
	body, err := model.PlaybackSDPBytes(model.PlaybackSDP{
		DeviceID: channel, SN: "78", ConnIP: "127.0.0.1", RTPPort: 40000,
		Start: start, End: start.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("PlaybackSDPBytes: %v", err)
	}
	req, err := model.NewRequest("INVITE", "sip:"+channel+"@3402000000", []model.Header{
		model.NewHeader("From", "<sip:34020000002000000001@3402000000>;tag=up"),
		model.NewHeader("To", "<sip:"+channel+"@3402000000>"),
		model.NewHeader("Call-ID", "playback-rec-2"),
		model.NewHeader("CSeq", "1 INVITE"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-pb-rec2"),
		model.NewHeader("Content-Type", "application/sdp"),
	}, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if resp := h.tr.deliver(t, req); resp.StatusCode() != 404 {
		t.Fatalf("status = %d, want 404", resp.StatusCode())
	}
}
