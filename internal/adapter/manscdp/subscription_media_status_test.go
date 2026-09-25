package manscdp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// TestDecodeSubscribe_Golden verifies the SUBSCRIBE parser against a real
// GB/T 28181 wire sample. The golden file is the contract with upstreams.
func TestDecodeSubscribe_Golden(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "subscribe.xml"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	got, err := NewMANSCDPCodec().DecodeSubscribe(want)
	if err != nil {
		t.Fatalf("DecodeSubscribe: %v", err)
	}
	if got.CmdType != model.CmdTypeSubscribe {
		t.Errorf("cmd type = %q, want Subscribe", got.CmdType)
	}
	if got.SN != 101 {
		t.Errorf("sn = %d, want 101", got.SN)
	}
	if got.DeviceID != "34020000002160000001" {
		t.Errorf("device id = %q, want platform-small id", got.DeviceID)
	}
	if got.Expires != 3600 {
		t.Errorf("expires = %d, want 3600", got.Expires)
	}
	if got.EventType != "catalog" {
		t.Errorf("event type = %q, want catalog", got.EventType)
	}
}

// TestDecodeSubscribe_RoundTrip parses our own marshalled output.
func TestDecodeSubscribe_RoundTrip(t *testing.T) {
	raw := `<?xml version="1.0" encoding="UTF-8"?>
<Subscribe>
  <CmdType>Subscribe</CmdType>
  <SN>55</SN>
  <DeviceID>34020000002160000001</DeviceID>
  <Expires>7200</Expires>
  <EventID>1</EventID>
  <EventType>Catalog</EventType>
</Subscribe>`
	got, err := NewMANSCDPCodec().DecodeSubscribe([]byte(raw))
	if err != nil {
		t.Fatalf("DecodeSubscribe: %v", err)
	}
	if got.SN != 55 || got.DeviceID != "34020000002160000001" || got.EventType != "Catalog" {
		t.Fatalf("decoded = %+v, want SN=55/device=platform-small/type=Catalog", got)
	}
}

// TestDecodeSubscribe_Defaults fills in sensible defaults when optional
// fields are missing.
func TestDecodeSubscribe_Defaults(t *testing.T) {
	raw := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<Subscribe>
  <CmdType>Subscribe</CmdType>
  <SN>1</SN>
  <DeviceID>34020000002160000001</DeviceID>
</Subscribe>`)
	got, err := NewMANSCDPCodec().DecodeSubscribe(raw)
	if err != nil {
		t.Fatalf("DecodeSubscribe: %v", err)
	}
	if got.Expires != 3600 {
		t.Errorf("default expires = %d, want 3600", got.Expires)
	}
	if got.EventType != "catalog" {
		t.Errorf("default event type = %q, want catalog", got.EventType)
	}
}

// TestDecodeSubscribe_RejectsMalformed returns an error for non-XML input.
func TestDecodeSubscribe_RejectsMalformed(t *testing.T) {
	if _, err := NewMANSCDPCodec().DecodeSubscribe([]byte("not xml")); err == nil {
		t.Fatal("expected an error for malformed XML")
	}
}

// TestDecodeMediaStatus_Golden verifies the MediaStatus parser against a real
// GB/T 28181 wire sample.
func TestDecodeMediaStatus_Golden(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "media_status.xml"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	got, err := NewMANSCDPCodec().DecodeMediaStatus(string(want))
	if err != nil {
		t.Fatalf("DecodeMediaStatus: %v", err)
	}
	if got.DeviceID != "34020000001320000001" {
		t.Errorf("device id = %q, want device", got.DeviceID)
	}
	if got.ChannelID != "34020000001320000001" {
		t.Errorf("channel id = %q, want channel", got.ChannelID)
	}
	if got.RecordStatus != model.RecordStatusRecording {
		t.Errorf("record status = %v, want recording", got.RecordStatus)
	}
	if got.Video == nil || got.Video.Width != 1920 || got.Video.Height != 1080 {
		t.Errorf("video param = %+v, want 1920x1080", got.Video)
	}
}

// TestDecodeMediaStatus_RoundTrip verifies that MarshalMediaStatus output is
// parseable by DecodeMediaStatus.
func TestDecodeMediaStatus_RoundTrip(t *testing.T) {
	ms := model.MediaStatus{
		DeviceID:  "34020000001320000001",
		ChannelID: "34020000001320000001",
		RecordStatus: model.RecordStatusRecording,
		Video: &model.VideoParam{
			Width:     1920,
			Height:    1080,
			Bitrate:   4096,
			FrameRate: 25,
			Codec:     "H.264",
		},
	}
	body, err := NewMANSCDPCodec().MarshalMediaStatus(ms)
	if err != nil {
		t.Fatalf("MarshalMediaStatus: %v", err)
	}
	got, err := NewMANSCDPCodec().DecodeMediaStatus(body)
	if err != nil {
		t.Fatalf("DecodeMediaStatus round trip: %v", err)
	}
	if got.DeviceID != ms.DeviceID || got.ChannelID != ms.ChannelID {
		t.Errorf("ids = %s/%s, want %s/%s", got.DeviceID, got.ChannelID, ms.DeviceID, ms.ChannelID)
	}
	if got.RecordStatus != ms.RecordStatus {
		t.Errorf("record status = %v, want %v", got.RecordStatus, ms.RecordStatus)
	}
	if got.Video == nil || got.Video.Width != 1920 {
		t.Errorf("video = %+v, want width=1920", got.Video)
	}
}

// TestDecodeMediaStatus_RejectsWrongCmdType returns an error when the body is
// a Notify but not a MediaStatus.
func TestDecodeMediaStatus_RejectsWrongCmdType(t *testing.T) {
	raw := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<Notify>
  <CmdType>Catalog</CmdType>
  <SN>1</SN>
  <DeviceID>34020000001320000001</DeviceID>
</Notify>`)
	if _, err := NewMANSCDPCodec().DecodeMediaStatus(string(raw)); err == nil {
		t.Fatal("expected an error for a non-MediaStatus Notify")
	}
}
