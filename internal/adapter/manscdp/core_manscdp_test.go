package manscdp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// TestDecodeDeviceInfoQuery_Golden verifies the DeviceInfo query parser.
func TestDecodeDeviceInfoQuery_Golden(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "deviceinfo-query.xml"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	got, err := NewMANSCDPCodec().DecodeDeviceInfoQuery(string(want))
	if err != nil {
		t.Fatalf("DecodeDeviceInfoQuery: %v", err)
	}
	if got.CmdType != model.CmdTypeDeviceInfo {
		t.Errorf("cmd type = %q, want DeviceInfo", got.CmdType)
	}
	if got.SN != 1001 {
		t.Errorf("sn = %d, want 1001", got.SN)
	}
	if got.DeviceID != "34020000002160000001" {
		t.Errorf("device id = %q, want platform-small id", got.DeviceID)
	}
}

// TestMarshalDeviceInfoResponse verifies the DeviceInfo response marshal produces correct XML.
func TestMarshalDeviceInfoResponse(t *testing.T) {
	item, _ := model.NewDeviceInfoItem(model.DeviceInfoItemParams{
		DeviceID:     "34020000002160000001",
		Name:         "Camera 01",
		Manufacturer: "Hikvision",
		Model:        "DS-2DE4",
		Firmware:     "V5.5.0",
		RunMode:      1,
		CivilCode:    "34020000",
		Address:      "Building 1",
		Parental:     0,
		SafetyWay:    0,
		RegisterWay:  1,
		Secrecy:      0,
		Status:       "ON",
	})
	resp, _ := model.NewDeviceInfoResponse("34020000002160000001", 1001, []model.DeviceInfoItem{item})
	body, err := NewMANSCDPCodec().MarshalDeviceInfoResponse(resp)
	if err != nil {
		t.Fatalf("MarshalDeviceInfoResponse: %v", err)
	}
	if !strings.Contains(body, "<DeviceID>34020000002160000001</DeviceID>") {
		t.Errorf("missing DeviceID in body:\n%s", body)
	}
	if !strings.Contains(body, "<Name>Camera 01</Name>") {
		t.Errorf("missing Name in body:\n%s", body)
	}
	if !strings.Contains(body, "<Manufacturer>Hikvision</Manufacturer>") {
		t.Errorf("missing Manufacturer in body:\n%s", body)
	}
	// Decode it back as a response
	resp2, err := NewMANSCDPCodec().DecodeDeviceInfoResponse(body)
	if err != nil {
		t.Fatalf("DecodeDeviceInfoResponse: %v", err)
	}
	if resp2.DeviceID != resp.DeviceID || resp2.SN != resp.SN {
		t.Errorf("round-trip ids = %s/%d, want %s/%d", resp2.DeviceID, resp2.SN, resp.DeviceID, resp.SN)
	}
}

// TestDecodeRecordInfoResponse_Golden verifies the RecordInfo response parser.
func TestDecodeRecordInfoResponse_Golden(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "recordinfo-response.xml"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	got, err := NewMANSCDPCodec().DecodeRecordInfoResponse(string(want))
	if err != nil {
		t.Fatalf("DecodeRecordInfoResponse: %v", err)
	}
	if got.SN != 2002 {
		t.Errorf("sn = %d, want 2002", got.SN)
	}
	if got.DeviceID != "34020000002160000001" {
		t.Errorf("device id = %q, want platform-small id", got.DeviceID)
	}
	if got.SumNum != 2 {
		t.Errorf("sum num = %d, want 2", got.SumNum)
	}
	if len(got.Items) != 2 {
		t.Errorf("items count = %d, want 2", len(got.Items))
	}
}

// TestMarshalRecordInfoResponse_RoundTrip renders a response and verifies fields.
func TestMarshalRecordInfoResponse_RoundTrip(t *testing.T) {
	item := model.NewRecordInfoItem(model.RecordInfoItemParams{
		Name:         "recording_001",
		DeviceID:     "34020000002160000001",
		ChannelID:    "34020000002160000001",
		StartTime:    "2025-01-01T00:00:00Z",
		EndTime:      "2025-01-01T01:00:00Z",
		FilePath:     "/record/001.mp4",
		VideoCodec:   "H.264",
		AudioCodec:   "AAC",
		VideoBitrate: 2048,
		AudioBitrate: 128,
	})
	resp, _ := model.NewRecordInfoResponse("34020000002160000001", 2002, []model.RecordInfoItem{item})
	body, err := NewMANSCDPCodec().MarshalRecordInfoResponse(resp)
	if err != nil {
		t.Fatalf("MarshalRecordInfoResponse: %v", err)
	}
	if !strings.Contains(body, "<DeviceID>34020000002160000001</DeviceID>") {
		t.Errorf("missing DeviceID in body:\n%s", body)
	}
	if !strings.Contains(body, "<StartTime>2025-01-01T00:00:00Z</StartTime>") {
		t.Errorf("missing StartTime in body:\n%s", body)
	}
}

// TestMarshalRecordInfoResponse_EmptyList verifies an empty record list renders
// a valid answer with SumNum=0 (task 2.2).
func TestMarshalRecordInfoResponse_EmptyList(t *testing.T) {
	resp, _ := model.NewRecordInfoResponse("34020000002160000001", 2002, nil)
	body, err := NewMANSCDPCodec().MarshalRecordInfoResponse(resp)
	if err != nil {
		t.Fatalf("MarshalRecordInfoResponse: %v", err)
	}
	if !strings.Contains(body, "<SumNum>0</SumNum>") {
		t.Errorf("empty record list must have SumNum=0:\n%s", body)
	}
	// Round-trip: empty items decode back to an empty slice.
	got, err := NewMANSCDPCodec().DecodeRecordInfoResponse(body)
	if err != nil {
		t.Fatalf("DecodeRecordInfoResponse round trip: %v", err)
	}
	if len(got.Items) != 0 {
		t.Errorf("decoded items = %d, want 0", len(got.Items))
	}
}

// TestMarshalRecordInfoResponse_PopulatedList verifies multiple items are
// rendered with SumNum equal to the length (task 2.2).
func TestMarshalRecordInfoResponse_PopulatedList(t *testing.T) {
	items := []model.RecordInfoItem{
		model.NewRecordInfoItem(model.RecordInfoItemParams{
			Name: "rec_A", DeviceID: "34020000002160000001", ChannelID: "1",
			StartTime: "2025-01-01T00:00:00Z", EndTime: "2025-01-01T00:30:00Z",
			FilePath: "/records/a.mp4",
		}),
		model.NewRecordInfoItem(model.RecordInfoItemParams{
			Name: "rec_B", DeviceID: "34020000002160000001", ChannelID: "2",
			StartTime: "2025-01-01T00:30:00Z", EndTime: "2025-01-01T01:00:00Z",
			FilePath: "/records/b.mp4",
		}),
	}
	resp, _ := model.NewRecordInfoResponse("34020000002160000001", 2003, items)
	body, err := NewMANSCDPCodec().MarshalRecordInfoResponse(resp)
	if err != nil {
		t.Fatalf("MarshalRecordInfoResponse: %v", err)
	}
	if !strings.Contains(body, "<SumNum>2</SumNum>") {
		t.Errorf("populated record list must have SumNum=2:\n%s", body)
	}
	if !strings.Contains(body, "<FilePath>/records/a.mp4</FilePath>") {
		t.Errorf("missing first item FilePath:\n%s", body)
	}
	// Round-trip preserves both items.
	got, err := NewMANSCDPCodec().DecodeRecordInfoResponse(body)
	if err != nil {
		t.Fatalf("DecodeRecordInfoResponse round trip: %v", err)
	}
	if len(got.Items) != 2 {
		t.Errorf("decoded items = %d, want 2", len(got.Items))
	}
}

// TestDecodeAlarmNotify_Golden verifies the Alarm notify parser.
func TestDecodeAlarmNotify_Golden(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "alarm-notify.xml"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	got, err := NewMANSCDPCodec().DecodeAlarmNotify(string(want))
	if err != nil {
		t.Fatalf("DecodeAlarmNotify: %v", err)
	}
	if got.CmdType() != model.CmdTypeAlarm {
		t.Errorf("cmd type = %q, want Alarm", got.CmdType())
	}
	if got.SN() != 3001 {
		t.Errorf("sn = %d, want 3001", got.SN())
	}
	if got.EventType() != "Alarm" {
		t.Errorf("event type = %q, want Alarm", got.EventType())
	}
	if got.AlarmPriority() != 2 {
		t.Errorf("alarm priority = %d, want 2", got.AlarmPriority())
	}
}

// TestMarshalAlarmAck_Render renders an Alarm ack and verifies fields.
func TestMarshalAlarmAck_Render(t *testing.T) {
	ack := model.NewAlarmAck("34020000002160000001", 3001, "OK")
	body, err := NewMANSCDPCodec().MarshalAlarmAck(ack)
	if err != nil {
		t.Fatalf("MarshalAlarmAck: %v", err)
	}
	if !strings.Contains(body, "<Result>OK</Result>") {
		t.Errorf("missing Result OK in body:\n%s", body)
	}
	if !strings.Contains(body, "<SN>3001</SN>") {
		t.Errorf("missing SN in body:\n%s", body)
	}
}

// TestDecodePTZControl_Golden verifies the PTZ control parser.
func TestDecodePTZControl_Golden(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "ptz-control.xml"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	got, err := NewMANSCDPCodec().DecodePTZControl(string(want))
	if err != nil {
		t.Fatalf("DecodePTZControl: %v", err)
	}
	if got.CmdType != model.CmdTypeDeviceControl {
		t.Errorf("cmd type = %q, want DeviceControl", got.CmdType)
	}
	if got.SN != 4001 {
		t.Errorf("sn = %d, want 4001", got.SN)
	}
	if got.Speed != 128 {
		t.Errorf("speed = %d, want 128", got.Speed)
	}
	if got.Command != "Left" {
		t.Errorf("command = %q, want Left", got.Command)
	}
}

// TestMarshalPTZControl_RoundTrip renders a PTZ command and re-parses it.
func TestMarshalPTZControl_RoundTrip(t *testing.T) {
	ctrl := model.NewPTZControl("34020000002160000001", "34020000002160000001", "Left", 4001, 128, 5)
	body, err := NewMANSCDPCodec().MarshalPTZControl(ctrl)
	if err != nil {
		t.Fatalf("MarshalPTZControl: %v", err)
	}
	got, err := NewMANSCDPCodec().DecodePTZControl(body)
	if err != nil {
		t.Fatalf("round-trip DecodePTZControl: %v", err)
	}
	if got.DeviceID != ctrl.DeviceID || got.SN != ctrl.SN || got.Speed != ctrl.Speed {
		t.Errorf("round-trip = %+v, want %+v", got, ctrl)
	}
}

// TestDecodePresetQuery_Golden verifies the Preset query parser.
func TestDecodePresetQuery_Golden(t *testing.T) {
	raw := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<Query>
  <CmdType>PresetQuery</CmdType>
  <SN>5001</SN>
  <DeviceID>34020000002160000001</DeviceID>
  <ChannelID>34020000002160000001</ChannelID>
</Query>`)
	got, err := NewMANSCDPCodec().DecodePresetQuery(string(raw))
	if err != nil {
		t.Fatalf("DecodePresetQuery: %v", err)
	}
	if got.CmdType != model.CmdTypePresetQuery {
		t.Errorf("cmd type = %q, want PresetQuery", got.CmdType)
	}
	if got.SN != 5001 {
		t.Errorf("sn = %d, want 5001", got.SN)
	}
}

// TestDecodePresetSet_RoundTrip parses a Preset set command and verifies fields.
func TestDecodePresetSet_RoundTrip(t *testing.T) {
	raw := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<Control>
  <CmdType>Preset</CmdType>
  <SN>5002</SN>
  <DeviceID>34020000002160000001</DeviceID>
  <ChannelID>34020000002160000001</ChannelID>
  <PresetIndex>10</PresetIndex>
  <Name>Entrance</Name>
</Control>`)
	got, err := NewMANSCDPCodec().DecodePresetSet(string(raw))
	if err != nil {
		t.Fatalf("DecodePresetSet: %v", err)
	}
	if got.CmdType != model.CmdTypePreset {
		t.Errorf("cmd type = %q, want Preset", got.CmdType)
	}
	if got.PresetIndex != 10 {
		t.Errorf("preset index = %d, want 10", got.PresetIndex)
	}
	if got.Name != "Entrance" {
		t.Errorf("name = %q, want Entrance", got.Name)
	}
}

// TestMarshalPresetAck_Render renders a Preset ack and verifies fields.
func TestMarshalPresetAck_Render(t *testing.T) {
	ack := model.NewPresetAck("34020000002160000001", 5002, "OK")
	body, err := NewMANSCDPCodec().MarshalPresetAck(ack)
	if err != nil {
		t.Fatalf("MarshalPresetAck: %v", err)
	}
	if !strings.Contains(body, "<Result>OK</Result>") {
		t.Errorf("missing Result OK in body:\n%s", body)
	}
	if !strings.Contains(body, "<SN>5002</SN>") {
		t.Errorf("missing SN in body:\n%s", body)
	}
}

// TestDecodeConfigCommand_Golden verifies the ConfigDownload command parser.
func TestDecodeConfigCommand_Golden(t *testing.T) {
	raw := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<Query>
  <CmdType>ConfigDownload</CmdType>
  <SN>6001</SN>
  <DeviceID>34020000002160000001</DeviceID>
  <ConfigType>BasicParam</ConfigType>
</Query>`)
	got, err := NewMANSCDPCodec().DecodeConfigCommand(string(raw))
	if err != nil {
		t.Fatalf("DecodeConfigCommand: %v", err)
	}
	if got.CmdType != model.CmdTypeConfigDownload {
		t.Errorf("cmd type = %q, want ConfigDownload", got.CmdType)
	}
	if got.ConfigType != "BasicParam" {
		t.Errorf("config type = %q, want BasicParam", got.ConfigType)
	}
}

// TestMarshalConfigAck_Render renders a ConfigDownload ack and verifies fields.
func TestMarshalConfigAck_Render(t *testing.T) {
	ack := model.NewConfigAck("34020000002160000001", 6001, "OK")
	body, err := NewMANSCDPCodec().MarshalConfigAck(ack)
	if err != nil {
		t.Fatalf("MarshalConfigAck: %v", err)
	}
	if !strings.Contains(body, "<Result>OK</Result>") {
		t.Errorf("missing Result OK in body:\n%s", body)
	}
	if !strings.Contains(body, "<SN>6001</SN>") {
		t.Errorf("missing SN in body:\n%s", body)
	}
}
