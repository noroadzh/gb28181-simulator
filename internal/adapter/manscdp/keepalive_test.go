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
