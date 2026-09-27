package model

import (
	"encoding/json"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestPosition_JSON_RoundTrip(t *testing.T) {
	in := Position{longitude: 116.39, latitude: 39.9, speed: 12.5}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Position
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Longitude() != in.Longitude() || got.Latitude() != in.Latitude() || got.Speed() != in.Speed() {
		t.Fatalf("mismatch: got %+v want %+v", got, in)
	}
	if string(b) != `{"longitude":116.39,"latitude":39.9,"speed":12.5}` {
		t.Fatalf("unexpected JSON: %s", b)
	}
}

func TestPosition_JSON_RejectsInvalid(t *testing.T) {
	var p Position
	cases := []string{
		`{"longitude":200,"latitude":0,"speed":0}`,
		`{"longitude":0,"latitude":100,"speed":0}`,
		`{"longitude":0,"latitude":0,"speed":-1}`,
		`not-json`,
	}
	for _, c := range cases {
		if err := json.Unmarshal([]byte(c), &p); err == nil {
			t.Fatalf("expected error for %q", c)
		}
	}
}

func TestPosition_YAML_RoundTrip(t *testing.T) {
	in := Position{longitude: 116.39, latitude: 39.9, speed: 12.5}
	b, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Position
	if err := yaml.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Longitude() != in.Longitude() || got.Latitude() != in.Latitude() || got.Speed() != in.Speed() {
		t.Fatalf("mismatch: got %+v want %+v", got, in)
	}
}

func TestRecordItem_JSON_RoundTrip(t *testing.T) {
	start := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	end := time.Date(2024, 1, 2, 4, 0, 0, 0, time.UTC)
	in := RecordItem{
		DeviceID:  "34020000001310000001",
		ChannelID: "34020000001310000001",
		Start:     start,
		End:       end,
		Format:    "MP4",
		FilePath:  "/data/rec/01.mp4",
		FileSize:  1024,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got RecordItem
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != in {
		t.Fatalf("mismatch: got %+v want %+v", got, in)
	}
}

func TestRecordItem_YMAL_RoundTrip(t *testing.T) {
	start := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	end := time.Date(2024, 1, 2, 4, 0, 0, 0, time.UTC)
	in := RecordItem{
		DeviceID:  "34020000001310000001",
		ChannelID: "34020000001310000001",
		Start:     start,
		End:       end,
		Format:    "MP4",
		FilePath:  "/data/rec/01.mp4",
		FileSize:  1024,
	}
	b, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got RecordItem
	if err := yaml.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != in {
		t.Fatalf("mismatch: got %+v want %+v", got, in)
	}
}

func TestPosition_ZeroValueRoundTrip(t *testing.T) {
	p := Position{}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Position
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Longitude() != 0 || got.Latitude() != 0 || got.Speed() != 0 {
		t.Fatalf("expected zero values, got %+v", got)
	}
}
