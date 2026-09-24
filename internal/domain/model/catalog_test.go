package model

import (
	"strings"
	"testing"
	"time"
)

func testRow(t *testing.T, deviceID string) DownstreamDevice {
	t.Helper()
	d, err := NewDownstreamDevice(DownstreamDeviceParams{
		DeviceID: deviceID,
		Addr:     "127.0.0.1:15060",
		Contact:  "<sip:" + deviceID + "@127.0.0.1:15060>",
		Now:      time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewDownstreamDevice: %v", err)
	}
	return d.WithGranted(3600, d.RegisteredAt())
}

func TestNewCatalogItem(t *testing.T) {
	item, err := NewCatalogItem(CatalogItemParams{DeviceID: "34020000011310000001"})
	if err != nil {
		t.Fatalf("NewCatalogItem: %v", err)
	}
	// What an upstream cannot act on must still be filled in: an empty
	// manufacturer or status breaks parsers on the other side.
	if item.DeviceID() != "34020000011310000001" {
		t.Errorf("device id = %q", item.DeviceID())
	}
	if item.Name() != "34020000011310000001" {
		t.Errorf("name = %q, want the device id", item.Name())
	}
	if item.Manufacturer() != CatalogManufacturer || item.Model() != CatalogModel {
		t.Errorf("manufacturer/model = %q/%q, want the placeholders",
			item.Manufacturer(), item.Model())
	}
	if item.Status() != CatalogStatusON {
		t.Errorf("status = %q, want ON", item.Status())
	}
	if item.CivilCode() != "340200" {
		t.Errorf("civil code = %q, want the first six digits of the device id", item.CivilCode())
	}
	if item.RegisterWay() != 1 {
		t.Errorf("register way = %d, want 1 (standard registration)", item.RegisterWay())
	}
}

func TestNewCatalogItem_Overrides(t *testing.T) {
	item, err := NewCatalogItem(CatalogItemParams{
		DeviceID:     "34020000011310000001",
		Name:         "lobby camera",
		Manufacturer: "acme",
		Model:        "cam-1",
		Owner:        "ops",
		CivilCode:    "340201",
		Address:      "127.0.0.1:15061",
		Status:       CatalogStatusON,
	})
	if err != nil {
		t.Fatalf("NewCatalogItem: %v", err)
	}
	if item.Name() != "lobby camera" || item.Manufacturer() != "acme" ||
		item.Model() != "cam-1" || item.Owner() != "ops" ||
		item.CivilCode() != "340201" || item.Address() != "127.0.0.1:15061" {
		t.Errorf("item = %+v, want every override honoured", item)
	}
}

func TestNewCatalogItem_RequiresDeviceID(t *testing.T) {
	if _, err := NewCatalogItem(CatalogItemParams{}); err == nil {
		t.Fatal("expected an error for an item without a device id")
	}
}

// The catalog is derived from the online device table, never kept beside it:
// one source of truth for "who is connected".
func TestNewCatalogFromDevices(t *testing.T) {
	rows := []DownstreamDevice{
		testRow(t, "34020000011310000002"),
		testRow(t, "34020000011310000001"),
	}
	catalog, err := NewCatalogFromDevices("34020000002000000001", 7, rows)
	if err != nil {
		t.Fatalf("NewCatalogFromDevices: %v", err)
	}
	if catalog.DeviceID() != "34020000002000000001" || catalog.SN() != 7 {
		t.Errorf("catalog = %s, want the platform id and the query's SN", catalog)
	}
	if catalog.SumNum() != 2 {
		t.Fatalf("SumNum = %d, want 2", catalog.SumNum())
	}
	// Order is the caller's (the table lists in device order); the codec
	// must not reorder what the use case already sorted.
	if catalog.Items()[0].DeviceID() != "34020000011310000002" {
		t.Errorf("first item = %q, want the order preserved", catalog.Items()[0].DeviceID())
	}
	if catalog.Items()[0].Address() != "127.0.0.1:15060" {
		t.Errorf("address = %q, want the row's address", catalog.Items()[0].Address())
	}
	if !catalog.HasCatalog() {
		t.Error("a built catalog reports itself as unbuilt")
	}
}

// An empty table is a legitimate answer, not an error: the platform is
// there, it simply has nobody online.
func TestNewCatalog_EmptyTable(t *testing.T) {
	catalog, err := NewCatalogFromDevices("34020000002000000001", 8, nil)
	if err != nil {
		t.Fatalf("NewCatalogFromDevices: %v", err)
	}
	if catalog.SumNum() != 0 || len(catalog.Items()) != 0 {
		t.Errorf("SumNum = %d with %d items, want 0/0", catalog.SumNum(), len(catalog.Items()))
	}
}

func TestNewCatalog_RequiresDeviceID(t *testing.T) {
	if _, err := NewCatalog("", 1, nil); err == nil {
		t.Fatal("expected an error for a catalog without a device id")
	}
}

func TestCatalog_StringIsLogSafe(t *testing.T) {
	catalog, err := NewCatalogFromDevices("34020000002000000001", 1, []DownstreamDevice{
		testRow(t, "34020000011310000001"),
	})
	if err != nil {
		t.Fatalf("NewCatalogFromDevices: %v", err)
	}
	summary := strings.ToLower(catalog.String())
	for _, secret := range []string{"password", "authorization"} {
		if strings.Contains(summary, secret) {
			t.Errorf("String() carries %q: %s", secret, catalog.String())
		}
	}
	if !strings.Contains(catalog.String(), "34020000002000000001") {
		t.Errorf("String() omits the platform id: %s", catalog.String())
	}
}
