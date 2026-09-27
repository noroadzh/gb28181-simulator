package manscdp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// TestGB2022DecodeHomePositionQuery_Golden verifies the HomePosition query parser.
func TestGB2022DecodeHomePositionQuery_Golden(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "home-position-query.xml"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	got, err := NewMANSCDPCodec().DecodeHomePositionQuery(string(want))
	if err != nil {
		t.Fatalf("DecodeHomePositionQuery: %v", err)
	}
	if got.SN != 86 {
		t.Errorf("sn = %d, want 86", got.SN)
	}
	if got.DeviceID != "34020000001320000001" {
		t.Errorf("device id = %q, want device id", got.DeviceID)
	}
}

// TestGB2022MarshalHomePositionQuery_Render renders a HomePosition query and verifies fields.
func TestGB2022MarshalHomePositionQuery_Render(t *testing.T) {
	query := model.HomePositionQuery{DeviceID: "34020000001320000001"}
	body, err := NewMANSCDPCodec().MarshalHomePositionQuery(query, 86)
	if err != nil {
		t.Fatalf("MarshalHomePositionQuery: %v", err)
	}
	if !strings.Contains(body, "<CmdType>HomePosition</CmdType>") {
		t.Errorf("missing CmdType in body:\n%s", body)
	}
	if !strings.Contains(body, "<SN>86</SN>") {
		t.Errorf("missing SN in body:\n%s", body)
	}
	if !strings.Contains(body, "<DeviceID>34020000001320000001</DeviceID>") {
		t.Errorf("missing DeviceID in body:\n%s", body)
	}
}

// TestGB2022DecodeHomePositionSet_Golden verifies the HomePosition set command parser.
func TestGB2022DecodeHomePositionSet_Golden(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "home-position-set.xml"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	got, err := NewMANSCDPCodec().DecodeHomePositionSet(string(want))
	if err != nil {
		t.Fatalf("DecodeHomePositionSet: %v", err)
	}
	if got.SN != 87 {
		t.Errorf("sn = %d, want 87", got.SN)
	}
	if got.DeviceID != "34020000001320000001" {
		t.Errorf("device id = %q, want device id", got.DeviceID)
	}
	if got.Longitude != "121.473701" {
		t.Errorf("longitude = %q, want 121.473701", got.Longitude)
	}
	if got.Latitude != "31.230370" {
		t.Errorf("latitude = %q, want 31.230370", got.Latitude)
	}
	if got.Altitude != "12.0" {
		t.Errorf("altitude = %q, want 12.0", got.Altitude)
	}
	if got.Azimuth != "45.5" {
		t.Errorf("azimuth = %q, want 45.5", got.Azimuth)
	}
}

// TestGB2022MarshalHomePositionSet_Render renders a HomePosition set command and re-parses it.
func TestGB2022MarshalHomePositionSet_Render(t *testing.T) {
	pos, _ := model.NewHomePosition(model.HomePositionParams{
		DeviceID:  "34020000001320000001",
		Longitude: "121.473701",
		Latitude:  "31.230370",
		Altitude:  "12.0",
		Azimuth:   "45.5",
	})
	body, err := NewMANSCDPCodec().MarshalHomePositionSet(pos, 87)
	if err != nil {
		t.Fatalf("MarshalHomePositionSet: %v", err)
	}
	if !strings.Contains(body, "<Longitude>121.473701</Longitude>") {
		t.Errorf("missing Longitude in body:\n%s", body)
	}
	if !strings.Contains(body, "<Azimuth>45.5</Azimuth>") {
		t.Errorf("missing Azimuth in body:\n%s", body)
	}
	// Round-trip: parse back and verify fields.
	got, err := NewMANSCDPCodec().DecodeHomePositionSet(body)
	if err != nil {
		t.Fatalf("DecodeHomePositionSet round trip: %v", err)
	}
	if got.Longitude != pos.Longitude() || got.Latitude != pos.Latitude() {
		t.Errorf("round-trip position = %s/%s, want %s/%s", got.Longitude, got.Latitude, pos.Longitude(), pos.Latitude())
	}
}

// TestGB2022DecodeHomePositionResponse_Golden verifies the HomePosition response parser.
func TestGB2022DecodeHomePositionResponse_Golden(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "home-position-response.xml"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	got, err := NewMANSCDPCodec().DecodeHomePositionResponse(string(want))
	if err != nil {
		t.Fatalf("DecodeHomePositionResponse: %v", err)
	}
	if got.CmdType != model.CmdTypeHomePosition {
		t.Errorf("cmd type = %q, want HomePosition", got.CmdType)
	}
	if got.SN != 88 {
		t.Errorf("sn = %d, want 88", got.SN)
	}
	if got.DeviceID != "34020000001320000001" {
		t.Errorf("device id = %q, want device id", got.DeviceID)
	}
	if got.Result != "OK" {
		t.Errorf("result = %q, want OK", got.Result)
	}
}

// TestGB2022MarshalHomePositionResponse_Render renders a HomePosition response.
func TestGB2022MarshalHomePositionResponse_Render(t *testing.T) {
	resp := model.HomePositionResponse{
		CmdType:  model.CmdTypeHomePosition,
		DeviceID: "34020000001320000001",
		SN:       88,
		Result:   "OK",
	}
	body, err := NewMANSCDPCodec().MarshalHomePositionResponse(resp, 88)
	if err != nil {
		t.Fatalf("MarshalHomePositionResponse: %v", err)
	}
	if !strings.Contains(body, "<Result>OK</Result>") {
		t.Errorf("missing Result OK in body:\n%s", body)
	}
}

// TestGB2022DecodeCruiseTrackListQuery_Golden verifies the CruiseTrackList query parser.
func TestGB2022DecodeCruiseTrackListQuery_Golden(t *testing.T) {
	raw := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<Query>
  <CmdType>CruiseTrackList</CmdType>
  <SN>90</SN>
  <DeviceID>34020000001320000001</DeviceID>
</Query>`)
	got, err := NewMANSCDPCodec().DecodeCruiseTrackListQuery(string(raw))
	if err != nil {
		t.Fatalf("DecodeCruiseTrackListQuery: %v", err)
	}
	if got.SN != 90 {
		t.Errorf("sn = %d, want 90", got.SN)
	}
	if got.DeviceID != "34020000001320000001" {
		t.Errorf("device id = %q, want device id", got.DeviceID)
	}
}

// TestGB2022MarshalCruiseTrackListQuery_Render renders a CruiseTrackList query and verifies fields.
func TestGB2022MarshalCruiseTrackListQuery_Render(t *testing.T) {
	query := model.CruiseTrackListQuery{DeviceID: "34020000001320000001"}
	body, err := NewMANSCDPCodec().MarshalCruiseTrackListQuery(query, 90)
	if err != nil {
		t.Fatalf("MarshalCruiseTrackListQuery: %v", err)
	}
	if !strings.Contains(body, "<CmdType>CruiseTrackList</CmdType>") {
		t.Errorf("missing CmdType in body:\n%s", body)
	}
	if !strings.Contains(body, "<SN>90</SN>") {
		t.Errorf("missing SN in body:\n%s", body)
	}
	if !strings.Contains(body, "<DeviceID>34020000001320000001</DeviceID>") {
		t.Errorf("missing DeviceID in body:\n%s", body)
	}
}

// TestGB2022DecodeCruiseTrackListResponse_Golden verifies the CruiseTrackList response parser.
func TestGB2022DecodeCruiseTrackListResponse_Golden(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "cruise-track-list.xml"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	got, err := NewMANSCDPCodec().DecodeCruiseTrackListResponse(string(want))
	if err != nil {
		t.Fatalf("DecodeCruiseTrackListResponse: %v", err)
	}
	if got.CmdType != model.CmdTypeCruiseTrackList {
		t.Errorf("cmd type = %q, want CruiseTrackList", got.CmdType)
	}
	if got.SN != 91 {
		t.Errorf("sn = %d, want 91", got.SN)
	}
	if got.DeviceID != "34020000001320000001" {
		t.Errorf("device id = %q, want device id", got.DeviceID)
	}
	if len(got.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(got.Items))
	}
	if got.Items[0].ID() != "track-1" {
		t.Errorf("first item id = %q, want track-1", got.Items[0].ID())
	}
	if got.Items[1].Name() != "Perimeter" {
		t.Errorf("second item name = %q, want Perimeter", got.Items[1].Name())
	}
	if got.Items[0].WaypointCount() != 5 {
		t.Errorf("first item waypoint count = %d, want 5", got.Items[0].WaypointCount())
	}
}

// TestGB2022MarshalCruiseTrackListResponse_RoundTrip renders a CruiseTrackList response and re-parses it.
func TestGB2022MarshalCruiseTrackListResponse_RoundTrip(t *testing.T) {
	item1, err := model.NewCruiseTrack(model.CruiseTrackParams{ID: "track-1", Name: "Horizon", WaypointCount: 5})
	if err != nil {
		t.Fatalf("NewCruiseTrack item1: %v", err)
	}
	item2, err := model.NewCruiseTrack(model.CruiseTrackParams{ID: "track-2", Name: "Perimeter", WaypointCount: 8})
	if err != nil {
		t.Fatalf("NewCruiseTrack item2: %v", err)
	}
	resp := model.CruiseTrackListResponse{
		DeviceID: "34020000001320000001",
		Items:    []model.CruiseTrack{item1, item2},
	}
	body, err := NewMANSCDPCodec().MarshalCruiseTrackListResponse(resp, 91)
	if err != nil {
		t.Fatalf("MarshalCruiseTrackListResponse: %v", err)
	}
	got, err := NewMANSCDPCodec().DecodeCruiseTrackListResponse(body)
	if err != nil {
		t.Fatalf("DecodeCruiseTrackListResponse round trip: %v", err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("round-trip items = %d, want 2", len(got.Items))
	}
	if got.Items[0].ID() != "track-1" {
		t.Errorf("round-trip first item id = %q, want track-1", got.Items[0].ID())
	}
	if got.Items[1].WaypointCount() != 8 {
		t.Errorf("round-trip second item waypoint count = %d, want 8", got.Items[1].WaypointCount())
	}
}

// TestGB2022DecodeSnapShotCommand_Golden verifies the SnapShot command parser.
func TestGB2022DecodeSnapShotCommand_Golden(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "snapshot-command.xml"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	got, err := NewMANSCDPCodec().DecodeSnapShotCommand(string(want))
	if err != nil {
		t.Fatalf("DecodeSnapShotCommand: %v", err)
	}
	if got.SN != 94 {
		t.Errorf("sn = %d, want 94", got.SN)
	}
	if got.DeviceID != "34020000001320000001" {
		t.Errorf("device id = %q, want device id", got.DeviceID)
	}
	if got.ChannelID != "1" {
		t.Errorf("channel id = %q, want 1", got.ChannelID)
	}
}

// TestGB2022MarshalSnapShotCommand_Render renders a SnapShot command and verifies fields.
func TestGB2022MarshalSnapShotCommand_Render(t *testing.T) {
	cmd := model.SnapShotCommand{DeviceID: "34020000001320000001", ChannelID: "1"}
	body, err := NewMANSCDPCodec().MarshalSnapShotCommand(cmd, 94)
	if err != nil {
		t.Fatalf("MarshalSnapShotCommand: %v", err)
	}
	if !strings.Contains(body, "<CmdType>SnapShot</CmdType>") {
		t.Errorf("missing CmdType in body:\n%s", body)
	}
	if !strings.Contains(body, "<ChannelID>1</ChannelID>") {
		t.Errorf("missing ChannelID in body:\n%s", body)
	}
}

// TestGB2022DecodeSnapShotResponse_Golden verifies the SnapShot response parser.
func TestGB2022DecodeSnapShotResponse_Golden(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "snapshot-response.xml"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	got, err := NewMANSCDPCodec().DecodeSnapShotResponse(string(want))
	if err != nil {
		t.Fatalf("DecodeSnapShotResponse: %v", err)
	}
	if got.CmdType != model.CmdTypeSnapShot {
		t.Errorf("cmd type = %q, want SnapShot", got.CmdType)
	}
	if got.SN != 95 {
		t.Errorf("sn = %d, want 95", got.SN)
	}
	if got.DeviceID != "34020000001320000001" {
		t.Errorf("device id = %q, want device id", got.DeviceID)
	}
	if got.Result != "OK" {
		t.Errorf("result = %q, want OK", got.Result)
	}
}

// TestGB2022MarshalSnapShotResponse_Render renders a SnapShot response.
func TestGB2022MarshalSnapShotResponse_Render(t *testing.T) {
	resp := model.SnapShotResponse{
		CmdType:  model.CmdTypeSnapShot,
		DeviceID: "34020000001320000001",
		SN:       95,
		Result:   "OK",
	}
	body, err := NewMANSCDPCodec().MarshalSnapShotResponse(resp, 95)
	if err != nil {
		t.Fatalf("MarshalSnapShotResponse: %v", err)
	}
	if !strings.Contains(body, "<Result>OK</Result>") {
		t.Errorf("missing Result OK in body:\n%s", body)
	}
}
