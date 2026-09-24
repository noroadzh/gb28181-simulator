package model

import (
	"testing"
	"time"
)

func TestNewDownstreamDevice_Validation(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		params  DownstreamDeviceParams
		wantErr bool
	}{
		{"complete", DownstreamDeviceParams{DeviceID: "34020000011310000001", Addr: "127.0.0.1:15060", Now: now}, false},
		{"missing device id", DownstreamDeviceParams{Addr: "127.0.0.1:15060", Now: now}, true},
		{"missing address", DownstreamDeviceParams{DeviceID: "34020000011310000001", Now: now}, true},
		{"zero time", DownstreamDeviceParams{DeviceID: "34020000011310000001", Addr: "127.0.0.1:15060"}, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dev, err := NewDownstreamDevice(tc.params)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NewDownstreamDevice: got nil error, want one for %+v", tc.params)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewDownstreamDevice: %v", err)
			}
			if !dev.HasDevice() {
				t.Error("HasDevice() = false, want true")
			}
			if dev.GrantedExpiry() != 0 || !dev.ExpiresAt().IsZero() {
				t.Errorf("a freshly built row has expiry %ds / %v, want 0 / zero",
					dev.GrantedExpiry(), dev.ExpiresAt())
			}
			if !dev.LastSeenAt().Equal(now) || !dev.RegisteredAt().Equal(now) {
				t.Errorf("timestamps = %v / %v, want both %v", dev.RegisteredAt(), dev.LastSeenAt(), now)
			}
		})
	}
}

// Re-registering refreshes one row rather than adding another: the device
// id is the table's key.
func TestDownstreamDevice_WithGrantedRefreshes(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	dev, err := NewDownstreamDevice(DownstreamDeviceParams{
		DeviceID: "34020000011310000001", Addr: "127.0.0.1:15060", Contact: "<sip:34020000011310000001@127.0.0.1:15060>",
		Transport: "udp", GBVersion: "2016", Now: now,
	})
	if err != nil {
		t.Fatalf("NewDownstreamDevice: %v", err)
	}
	later := now.Add(30 * time.Minute)
	got := dev.WithGranted(3600, later)
	if got.GrantedExpiry() != 3600 {
		t.Errorf("granted = %d, want 3600", got.GrantedExpiry())
	}
	if !got.RegisteredAt().Equal(later) || !got.LastSeenAt().Equal(later) {
		t.Errorf("timestamps = %v / %v, want both %v", got.RegisteredAt(), got.LastSeenAt(), later)
	}
	if want := later.Add(time.Hour); !got.ExpiresAt().Equal(want) {
		t.Errorf("expires at = %v, want %v", got.ExpiresAt(), want)
	}
	// The original is untouched: value objects do not mutate.
	if dev.GrantedExpiry() != 0 {
		t.Errorf("original granted = %d, want 0", dev.GrantedExpiry())
	}
	if got.Contact() == "" || got.Transport() != "udp" || got.GBVersion() != "2016" {
		t.Errorf("granting dropped the registration details: %s / %s / %s",
			got.Contact(), got.Transport(), got.GBVersion())
	}
}

func TestDownstreamDevice_WithGrantedZeroClearsExpiry(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	dev, err := NewDownstreamDevice(DownstreamDeviceParams{DeviceID: "34020000011310000001", Addr: "127.0.0.1:15060", Now: now})
	if err != nil {
		t.Fatalf("NewDownstreamDevice: %v", err)
	}
	got := dev.WithGranted(0, now)
	if !got.ExpiresAt().IsZero() {
		t.Errorf("expires at = %v, want zero for a grant of 0", got.ExpiresAt())
	}
}

func TestDownstreamDevice_WithSeenAndContact(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	dev, err := NewDownstreamDevice(DownstreamDeviceParams{DeviceID: "34020000011310000001", Addr: "127.0.0.1:15060", Now: now})
	if err != nil {
		t.Fatalf("NewDownstreamDevice: %v", err)
	}
	later := now.Add(45 * time.Second)
	got := dev.WithSeen(later).WithContact("  <sip:34020000011310000001@127.0.0.1:15060>  ")
	if !got.LastSeenAt().Equal(later) {
		t.Errorf("last seen = %v, want %v", got.LastSeenAt(), later)
	}
	if !got.RegisteredAt().Equal(now) {
		t.Errorf("registered at = %v, want it unchanged at %v", got.RegisteredAt(), now)
	}
	if got.Contact() != "<sip:34020000011310000001@127.0.0.1:15060>" {
		t.Errorf("contact = %q, want it trimmed", got.Contact())
	}
}

// The summary travels into logs, so it must never smuggle a secret in.
func TestDownstreamDevice_StringIsLogSafe(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	dev, err := NewDownstreamDevice(DownstreamDeviceParams{DeviceID: "34020000011310000001", Addr: "127.0.0.1:15060", Now: now})
	if err != nil {
		t.Fatalf("NewDownstreamDevice: %v", err)
	}
	if s := dev.String(); s == "" {
		t.Error("String() is empty")
	}
}
