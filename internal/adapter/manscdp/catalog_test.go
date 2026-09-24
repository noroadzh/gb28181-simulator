package manscdp

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// The codec must stay usable through the port alone: the app layer imports
// port, never this package.
var _ port.MANSCDPCodec = (*MANSCDPCodecAdapter)(nil)

func newDeviceForCatalog(t *testing.T, id string) model.DownstreamDevice {
	t.Helper()
	d, err := model.NewDownstreamDevice(model.DownstreamDeviceParams{
		DeviceID: id,
		Addr:     "127.0.0.1:15060",
		Now:      time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewDownstreamDevice: %v", err)
	}
	return d
}

// A catalog answer is what an upstream parses, so its bytes are the
// contract. Byte-for-byte golden files keep a refactor from quietly changing
// the wire format.
func TestMarshalCatalog_Golden(t *testing.T) {
	cases := []struct {
		name     string
		golden   string
		devices  []string
		sn       uint32
		platform string
	}{
		{"one device", "catalog_one.xml",
			[]string{"34020000011310000001"}, 7, "34020000002000000001"},
		{"empty table", "catalog_empty.xml",
			nil, 8, "34020000002000000001"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			devices := make([]model.DownstreamDevice, 0, len(tc.devices))
			for _, id := range tc.devices {
				devices = append(devices, newDeviceForCatalog(t, id))
			}
			catalog, err := model.NewCatalogFromDevices(tc.platform, tc.sn, devices)
			if err != nil {
				t.Fatalf("NewCatalogFromDevices: %v", err)
			}
			got, err := NewMANSCDPCodec().MarshalCatalog(catalog)
			if err != nil {
				t.Fatalf("MarshalCatalog: %v", err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", tc.golden))
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			if got != string(want) {
				t.Errorf("catalog bytes changed.\n--- got ---\n%s\n--- want (%s) ---\n%s",
					got, tc.golden, want)
			}
		})
	}
}

// Whatever the codec writes, an upstream reads back by decoding it; the
// answer must survive its own round trip.
func TestMarshalCatalog_RoundTrip(t *testing.T) {
	devices := []model.DownstreamDevice{
		newDeviceForCatalog(t, "34020000011310000002"),
		newDeviceForCatalog(t, "34020000011310000001"),
	}
	catalog, err := model.NewCatalogFromDevices("34020000002000000001", 11, devices)
	if err != nil {
		t.Fatalf("NewCatalogFromDevices: %v", err)
	}
	c := NewMANSCDPCodec()
	body, err := c.MarshalCatalog(catalog)
	if err != nil {
		t.Fatalf("MarshalCatalog: %v", err)
	}
	var res catalogResponse
	if err := xml.Unmarshal([]byte(body), &res); err != nil {
		t.Fatalf("decode own output: %v", err)
	}
	if res.CmdType != model.CmdTypeCatalog {
		t.Errorf("cmd type = %q, want Catalog", res.CmdType)
	}
	if res.SN != 11 {
		t.Errorf("sn = %d, want 11", res.SN)
	}
	if res.DeviceID != "34020000002000000001" {
		t.Errorf("device id = %q, want the platform id", res.DeviceID)
	}
	if res.SumNum != 2 || res.DeviceList.Num != 2 {
		t.Errorf("counts = %d/%d, want 2/2", res.SumNum, res.DeviceList.Num)
	}
	if len(res.DeviceList.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(res.DeviceList.Items))
	}
	if res.DeviceList.Items[0].DeviceID != "34020000011310000002" {
		t.Errorf("first item = %s, order must follow the table, not be sorted here",
			res.DeviceList.Items[0].DeviceID)
	}
	if res.DeviceList.Items[0].Status != model.CatalogStatusON {
		t.Errorf("status = %q, want ON", res.DeviceList.Items[0].Status)
	}
}

// An unbuilt catalog is a programming error, not an empty answer: rendering
// it would put a response without a DeviceID on the wire.
func TestMarshalCatalog_RejectsUnbuilt(t *testing.T) {
	if _, err := NewMANSCDPCodec().MarshalCatalog(model.Catalog{}); err == nil {
		t.Fatal("MarshalCatalog accepted a catalog that was never built")
	}
}

// The platform id is the DeviceID of the answer; a blank one cannot be
// rendered either.
func TestMarshalCatalog_RejectsBlankPlatform(t *testing.T) {
	if _, err := model.NewCatalog("   ", 1, nil); err == nil {
		t.Fatal("NewCatalog accepted a blank device id")
	}
}
