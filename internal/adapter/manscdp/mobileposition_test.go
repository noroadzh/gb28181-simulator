package manscdp

import (
	"strings"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

const mobilePositionNotifyGolden = `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
	`<Notify>` + "\n" +
	`  <CmdType>MobilePosition</CmdType>` + "\n" +
	`  <SN>1</SN>` + "\n" +
	`  <DeviceID>34020000001320000001</DeviceID>` + "\n" +
	`  <Time>2010-11-11T19:38:50</Time>` + "\n" +
	`  <Longitude>125.12345</Longitude>` + "\n" +
	`  <Latitude>45.12345</Latitude>` + "\n" +
	`  <Speed>0.1</Speed>` + "\n" +
	`</Notify>` + "\n"

func TestMarshalMobilePositionNotify_Golden(t *testing.T) {
	c := NewMANSCDPCodec()
	mp, err := model.NewMobilePositionNotify(model.MobilePositionNotifyParams{
		SN:        1,
		DeviceID:  "34020000001320000001",
		Longitude: 125.12345,
		Latitude:  45.12345,
		Speed:     0.1,
		Time:      "2010-11-11T19:38:50",
	})
	if err != nil {
		t.Fatalf("NewMobilePositionNotify: %v", err)
	}
	got, err := c.MarshalMobilePositionNotify(mp)
	if err != nil {
		t.Fatalf("MarshalMobilePositionNotify: %v", err)
	}
	if got != mobilePositionNotifyGolden {
		t.Fatalf("MarshalMobilePositionNotify mismatch\n--- got ---\n%q\n--- want ---\n%q", got, mobilePositionNotifyGolden)
	}
}

func TestMarshalMobilePositionNotify_RejectsEmptyDeviceID(t *testing.T) {
	c := NewMANSCDPCodec()
	_, err := c.MarshalMobilePositionNotify(model.MobilePositionNotify{})
	if err == nil {
		t.Fatal("MarshalMobilePositionNotify: want error for empty DeviceID, got nil")
	}
}

func TestDecodeMobilePositionNotify(t *testing.T) {
	c := NewMANSCDPCodec()
	got, err := c.DecodeMobilePositionNotify(mobilePositionNotifyGolden)
	if err != nil {
		t.Fatalf("DecodeMobilePositionNotify: %v", err)
	}
	if got.CmdType() != model.CmdTypeMobilePosition {
		t.Errorf("CmdType = %q, want %q", got.CmdType(), model.CmdTypeMobilePosition)
	}
	if got.DeviceID() != "34020000001320000001" {
		t.Errorf("DeviceID = %q, want 34020000001320000001", got.DeviceID())
	}
	if got.SN() != 1 {
		t.Errorf("SN = %d, want 1", got.SN())
	}
	if got.Time() != "2010-11-11T19:38:50" {
		t.Errorf("Time = %q, want 2010-11-11T19:38:50", got.Time())
	}
	if got.Longitude() != 125.12345 {
		t.Errorf("Longitude = %v, want 125.12345", got.Longitude())
	}
	if got.Latitude() != 45.12345 {
		t.Errorf("Latitude = %v, want 45.12345", got.Latitude())
	}
	if got.Speed() != 0.1 {
		t.Errorf("Speed = %v, want 0.1", got.Speed())
	}
}

func TestDecodeMobilePositionNotify_IgnoresUnknownElements(t *testing.T) {
	body := `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<Notify>` + "\n" +
		`  <CmdType>MobilePosition</CmdType>` + "\n" +
		`  <SN>1</SN>` + "\n" +
		`  <DeviceID>34020000001320000001</DeviceID>` + "\n" +
		`  <Time>2010-11-11T19:38:50</Time>` + "\n" +
		`  <Longitude>125.12345</Longitude>` + "\n" +
		`  <Latitude>45.12345</Latitude>` + "\n" +
		`  <Speed>0.1</Speed>` + "\n" +
		`  <Direction>90</Direction>` + "\n" +
		`  <Altitude>100.0</Altitude>` + "\n" +
		`</Notify>` + "\n"
	c := NewMANSCDPCodec()
	got, err := c.DecodeMobilePositionNotify(body)
	if err != nil {
		t.Fatalf("DecodeMobilePositionNotify: %v", err)
	}
	if got.CmdType() != model.CmdTypeMobilePosition {
		t.Errorf("CmdType = %q, want %q", got.CmdType(), model.CmdTypeMobilePosition)
	}
	if got.Longitude() != 125.12345 || got.Latitude() != 45.12345 {
		t.Errorf("coordinates corrupted by unknown elements: %v", got)
	}
}

func TestDecodeMobilePositionNotify_NeverPanics(t *testing.T) {
	c := NewMANSCDPCodec()
	bodies := []string{
		"", "\x00", "<?xml", "<", "<>", "<Notify>", "</Notify>",
		`<?xml version="1.0"?><Notify><SN>1</SN><DeviceID>a</DeviceID><CmdType>MobilePosition</CmdType></Notify>`,
		`<?xml version="1.0"?><Notify><SN>1</SN><DeviceID>a</DeviceID><CmdType>MobilePosition</CmdType><Longitude>abc</Longitude><Latitude>0</Latitude></Notify>`,
	}
	for _, b := range bodies {
		if _, err := c.DecodeMobilePositionNotify(b); err == nil {
			t.Logf("decoded %q: no error (fine, it may be valid)", b)
		}
	}
}

func TestMobilePositionNotify_RoundTrip(t *testing.T) {
	c := NewMANSCDPCodec()
	mp, err := model.NewMobilePositionNotify(model.MobilePositionNotifyParams{
		SN:        1,
		DeviceID:  "34020000001320000001",
		Longitude: 125.12345,
		Latitude:  45.12345,
		Speed:     0.1,
		Time:      "2010-11-11T19:38:50",
	})
	if err != nil {
		t.Fatalf("NewMobilePositionNotify: %v", err)
	}
	body, err := c.MarshalMobilePositionNotify(mp)
	if err != nil {
		t.Fatalf("MarshalMobilePositionNotify: %v", err)
	}
	got, err := c.DecodeMobilePositionNotify(body)
	if err != nil {
		t.Fatalf("DecodeMobilePositionNotify round-trip: %v", err)
	}
	// Trim time because round-trip preserves it but the input may be a
	// future-era timestamp a test forgot to sanitize.
	if got.CmdType() != mp.CmdType() || got.DeviceID() != mp.DeviceID() ||
		got.SN() != mp.SN() || got.Time() != mp.Time() {
		t.Errorf("round-trip mismatch: %v → %v", mp, got)
	}
}

func TestMobilePositionNotify_Validation(t *testing.T) {
	cases := []struct {
		name    string
		p       model.MobilePositionNotifyParams
		wantErr bool
	}{
		{"valid", model.MobilePositionNotifyParams{SN: 1, DeviceID: "a", Longitude: 0, Latitude: 0, Speed: 0, Time: "2026-01-01T00:00:00"}, false},
		{"no device id", model.MobilePositionNotifyParams{SN: 1, Longitude: 0, Latitude: 0, Speed: 0, Time: "2026-01-01T00:00:00"}, true},
		{"longitude low", model.MobilePositionNotifyParams{SN: 1, DeviceID: "a", Longitude: -181, Latitude: 0, Speed: 0, Time: "2026-01-01T00:00:00"}, true},
		{"longitude high", model.MobilePositionNotifyParams{SN: 1, DeviceID: "a", Longitude: 181, Latitude: 0, Speed: 0, Time: "2026-01-01T00:00:00"}, true},
		{"latitude low", model.MobilePositionNotifyParams{SN: 1, DeviceID: "a", Longitude: 0, Latitude: -91, Speed: 0, Time: "2026-01-01T00:00:00"}, true},
		{"latitude high", model.MobilePositionNotifyParams{SN: 1, DeviceID: "a", Longitude: 0, Latitude: 91, Speed: 0, Time: "2026-01-01T00:00:00"}, true},
		{"negative speed", model.MobilePositionNotifyParams{SN: 1, DeviceID: "a", Longitude: 0, Latitude: 0, Speed: -1, Time: "2026-01-01T00:00:00"}, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := model.NewMobilePositionNotify(tc.p)
			if (err != nil) != tc.wantErr {
				t.Fatalf("NewMobilePositionNotify error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestDecodeMobilePositionNotify_Validation(t *testing.T) {
	c := NewMANSCDPCodec()
	mp, _ := model.NewMobilePositionNotify(model.MobilePositionNotifyParams{
		SN: 1, DeviceID: "a", Longitude: 0, Latitude: 0, Speed: 0, Time: "2026-01-01T00:00:00",
	})
	body, _ := c.MarshalMobilePositionNotify(mp)

	// tamper with longitude to invalid value
	bad := strings.Replace(body, "<Longitude>0</Longitude>", "<Longitude>abc</Longitude>", 1)
	if _, err := c.DecodeMobilePositionNotify(bad); err == nil {
		t.Fatal("want error for invalid longitude, got nil")
	}
}
