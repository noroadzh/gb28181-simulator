package manscdp

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

func testKeepalive(t *testing.T, sn uint32) model.Keepalive {
	t.Helper()
	k, err := model.NewKeepalive("34020000001320000001", sn)
	if err != nil {
		t.Fatalf("NewKeepalive: %v", err)
	}
	return k
}

// TestMarshalKeepaliveGolden pins the wire bytes: a platform parses these
// with a real XML parser, so the layout is the contract.
func TestMarshalKeepaliveGolden(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "keepalive.xml"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	got, err := NewKeepaliveCodec().MarshalKeepalive(testKeepalive(t, 1))
	if err != nil {
		t.Fatalf("MarshalKeepalive: %v", err)
	}
	if got != string(want) {
		t.Fatalf("keepalive body mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

// TestMarshalKeepaliveRoundTrip parses our own output with encoding/xml, the
// way a platform would, and checks every field survived.
func TestMarshalKeepaliveRoundTrip(t *testing.T) {
	body, err := NewKeepaliveCodec().MarshalKeepalive(testKeepalive(t, 42))
	if err != nil {
		t.Fatalf("MarshalKeepalive: %v", err)
	}
	var n keepaliveNotify
	if err := xml.Unmarshal([]byte(body), &n); err != nil {
		t.Fatalf("reparse: %v\nbody:\n%s", err, body)
	}
	if n.CmdType != CmdTypeKeepalive || n.SN != 42 ||
		n.DeviceID != "34020000001320000001" || n.Status != model.KeepaliveStatusOK {
		t.Fatalf("parsed notify = %+v, want Keepalive/42/34020000001320000001/OK", n)
	}
}

// TestMarshalKeepaliveSNIncrements is the property the session relies on: a
// platform can tell retransmissions apart by SN.
func TestMarshalKeepaliveSNIncrements(t *testing.T) {
	c := NewKeepaliveCodec()
	var last uint32
	for _, sn := range []uint32{1, 2, 3} {
		body, err := c.MarshalKeepalive(testKeepalive(t, sn))
		if err != nil {
			t.Fatalf("MarshalKeepalive(%d): %v", sn, err)
		}
		if !strings.Contains(body, "<SN>"+itoa(sn)+"</SN>") {
			t.Fatalf("body for sn %d lacks the sequence number:\n%s", sn, body)
		}
		if sn <= last {
			t.Fatalf("sn did not increase: %d after %d", sn, last)
		}
		last = sn
	}
}

func TestMarshalKeepaliveRejectsEmpty(t *testing.T) {
	if _, err := NewKeepaliveCodec().MarshalKeepalive(model.Keepalive{}); err == nil {
		t.Fatal("expected an error for a zero keepalive")
	}
}

// TestMarshalAlarmNotifyGolden pins the wire shape of an Alarm notify that
// carries the optional priority/method/description fields (task 2.1). A
// platform must be able to parse every field it emitted.
func TestMarshalAlarmNotifyGolden(t *testing.T) {
	n, err := model.NewAlarmNotify(model.AlarmNotifyParams{
		SN:            7,
		DeviceID:      "34020000001320000001",
		ChannelID:     "34020000001320000001",
		AlarmPriority: 1,
		AlarmMethod:   5,
		EventType:     "alarm",
		EventTime:     "2026-09-25T12:00:00",
		Description:   "motion detected",
	})
	if err != nil {
		t.Fatalf("NewAlarmNotify: %v", err)
	}
	body, err := NewKeepaliveCodec().MarshalAlarmNotify(n)
	if err != nil {
		t.Fatalf("MarshalAlarmNotify: %v", err)
	}
	for _, want := range []string{
		"<CmdType>Alarm</CmdType>",
		"<SN>7</SN>",
		"<DeviceID>34020000001320000001</DeviceID>",
		"<AlarmPriority>1</AlarmPriority>",
		"<AlarmMethod>5</AlarmMethod>",
		"<Description>motion detected</Description>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("alarm body missing %q:\n%s", want, body)
		}
	}
	// Round-trip: a platform parses what a device emitted.
	got, err := NewMANSCDPCodec().DecodeAlarmNotify(body)
	if err != nil {
		t.Fatalf("DecodeAlarmNotify round trip: %v", err)
	}
	if got.AlarmPriority() != 1 || got.AlarmMethod() != 5 ||
		got.Description() != "motion detected" || got.EventType() != "alarm" {
		t.Errorf("decoded alarm = %+v, want priority=1/method=5/desc=motion detected", got)
	}
}

// TestMarshalAlarmNotifyMinimal verifies that an alarm without the optional
// fields stays minimal on the wire (omitempty, task 2.1).
func TestMarshalAlarmNotifyMinimal(t *testing.T) {
	n, err := model.NewAlarmNotify(model.AlarmNotifyParams{
		SN:       1,
		DeviceID: "34020000001320000001",
	})
	if err != nil {
		t.Fatalf("NewAlarmNotify: %v", err)
	}
	body, err := NewKeepaliveCodec().MarshalAlarmNotify(n)
	if err != nil {
		t.Fatalf("MarshalAlarmNotify: %v", err)
	}
	if strings.Contains(body, "<AlarmPriority>") {
		t.Errorf("minimal alarm must omit AlarmPriority:\n%s", body)
	}
	if strings.Contains(body, "<Description>") {
		t.Errorf("minimal alarm must omit Description:\n%s", body)
	}
}

func TestMarshalAlarmNotifyRejectsEmptyDeviceID(t *testing.T) {
	// Validation happens at NewAlarmNotify, not at Marshal.
	if _, err := model.NewAlarmNotify(model.AlarmNotifyParams{DeviceID: ""}); err == nil {
		t.Fatal("expected an error for an alarm without a device id")
	}
}

func itoa(n uint32) string {
	if n == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
