package model

import (
	"strings"
	"testing"
	"time"
)

func TestNewHomePosition_Validation(t *testing.T) {
	cases := []struct {
		name    string
		params  HomePositionParams
		wantErr bool
	}{
		{"complete", HomePositionParams{DeviceID: "34020000011310000001", Longitude: "121.47", Latitude: "31.23"}, false},
		{"missing device id", HomePositionParams{Longitude: "121.47", Latitude: "31.23"}, true},
		{"missing longitude", HomePositionParams{DeviceID: "34020000011310000001", Latitude: "31.23"}, true},
		{"missing latitude", HomePositionParams{DeviceID: "34020000011310000001", Longitude: "121.47"}, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			pos, err := NewHomePosition(tc.params)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NewHomePosition: got nil error, want one for %+v", tc.params)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewHomePosition: %v", err)
			}
			if pos.DeviceID() != tc.params.DeviceID {
				t.Errorf("DeviceID = %q, want %q", pos.DeviceID(), tc.params.DeviceID)
			}
			if pos.Longitude() != tc.params.Longitude {
				t.Errorf("Longitude = %q, want %q", pos.Longitude(), tc.params.Longitude)
			}
			if pos.Latitude() != tc.params.Latitude {
				t.Errorf("Latitude = %q, want %q", pos.Latitude(), tc.params.Latitude)
			}
			if pos.Altitude() != strings.TrimSpace(tc.params.Altitude) {
				t.Errorf("Altitude = %q, want %q", pos.Altitude(), strings.TrimSpace(tc.params.Altitude))
			}
			if pos.Azimuth() != strings.TrimSpace(tc.params.Azimuth) {
				t.Errorf("Azimuth = %q, want %q", pos.Azimuth(), strings.TrimSpace(tc.params.Azimuth))
			}
			if pos.HasAzimuth() != (strings.TrimSpace(tc.params.Azimuth) != "") {
				t.Errorf("HasAzimuth = %v, want %v", pos.HasAzimuth(), strings.TrimSpace(tc.params.Azimuth) != "")
			}
		})
	}
}

func TestHomePosition_WithAzimuth(t *testing.T) {
	pos, err := NewHomePosition(HomePositionParams{DeviceID: "34020000011310000001", Longitude: "121.47", Latitude: "31.23"})
	if err != nil {
		t.Fatalf("NewHomePosition: %v", err)
	}
	updated := pos.WithAzimuth("121.4731")
	if updated.Azimuth() != "121.4731" {
		t.Errorf("Azimuth = %q, want 121.4731", updated.Azimuth())
	}
	if !updated.HasAzimuth() {
		t.Error("HasAzimuth = false after WithAzimuth, want true")
	}
	cleared := updated.WithAzimuth("")
	if cleared.Azimuth() != "" {
		t.Errorf("Azimuth after clear = %q, want empty", cleared.Azimuth())
	}
	if cleared.HasAzimuth() {
		t.Error("HasAzimuth = true after clear, want false")
	}
}

func TestNewCruiseTrack_Validation(t *testing.T) {
	cases := []struct {
		name    string
		params  CruiseTrackParams
		wantErr bool
	}{
		{"complete", CruiseTrackParams{ID: "track-1", Name: "Patrol A"}, false},
		{"missing id", CruiseTrackParams{Name: "Patrol A"}, true},
		{"missing name", CruiseTrackParams{ID: "track-1"}, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			track, err := NewCruiseTrack(tc.params)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NewCruiseTrack: got nil error, want one for %+v", tc.params)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewCruiseTrack: %v", err)
			}
			if track.ID() != strings.TrimSpace(tc.params.ID) {
				t.Errorf("ID = %q, want %q", track.ID(), strings.TrimSpace(tc.params.ID))
			}
			if track.Name() != strings.TrimSpace(tc.params.Name) {
				t.Errorf("Name = %q, want %q", track.Name(), strings.TrimSpace(tc.params.Name))
			}
			if track.WaypointCount() != tc.params.WaypointCount {
				t.Errorf("WaypointCount = %d, want %d", track.WaypointCount(), tc.params.WaypointCount)
			}
		})
	}
}

func TestNewCruiseTrack_NegativeCountClamped(t *testing.T) {
	track, err := NewCruiseTrack(CruiseTrackParams{ID: "track-1", Name: "Patrol A", WaypointCount: -3})
	if err != nil {
		t.Fatalf("NewCruiseTrack: %v", err)
	}
	if track.WaypointCount() != 0 {
		t.Errorf("WaypointCount = %d, want 0 for a negative input", track.WaypointCount())
	}
}

func TestNewSnapShotRecord_Validation(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		params  SnapShotRecordParams
		wantErr bool
	}{
		{"complete", SnapShotRecordParams{DeviceID: "34020000011310000001", ChannelID: "1", CapturedAt: now}, false},
		{"missing device id", SnapShotRecordParams{ChannelID: "1"}, true},
		{"missing channel id", SnapShotRecordParams{DeviceID: "34020000011310000001"}, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			rec, err := NewSnapShotRecord(tc.params)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NewSnapShotRecord: got nil error, want one for %+v", tc.params)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewSnapShotRecord: %v", err)
			}
			if rec.DeviceID() != tc.params.DeviceID {
				t.Errorf("DeviceID = %q, want %q", rec.DeviceID(), tc.params.DeviceID)
			}
			if rec.ChannelID() != tc.params.ChannelID {
				t.Errorf("ChannelID = %q, want %q", rec.ChannelID(), tc.params.ChannelID)
			}
			if !rec.CapturedAt().Equal(tc.params.CapturedAt) {
				t.Errorf("CapturedAt = %v, want %v", rec.CapturedAt(), tc.params.CapturedAt)
			}
		})
	}
}

func TestNewSnapShotRecord_ZeroTimeDefaultsToNow(t *testing.T) {
	before := time.Now().UTC()
	rec, err := NewSnapShotRecord(SnapShotRecordParams{DeviceID: "34020000011310000001", ChannelID: "1"})
	if err != nil {
		t.Fatalf("NewSnapShotRecord: %v", err)
	}
	after := time.Now().UTC()
	if rec.CapturedAt().IsZero() {
		t.Fatal("CapturedAt is zero, want a default timestamp")
	}
	if rec.CapturedAt().Before(before) || rec.CapturedAt().After(after) {
		t.Errorf("CapturedAt = %v, want between %v and %v", rec.CapturedAt(), before, after)
	}
}
