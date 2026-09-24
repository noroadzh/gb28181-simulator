// Package model — Catalog.
package model

import (
	"fmt"
	"strings"
)

// What a platform reports about a device it cannot know better. A real
// platform reads these from the device's own catalog; this simulator has no
// channel model yet, so it fills them with a recognisable constant rather
// than an empty string an upstream might choke on.
const (
	// CatalogManufacturer is the manufacturer reported for every entry.
	CatalogManufacturer = "gb28181-simulator"
	// CatalogModel is the model reported for every entry.
	CatalogModel = "simulator"
	// CatalogStatusON is the status of a device that is in the online table.
	CatalogStatusON = "ON"
)

// CatalogItem is one entry of a catalog answer: a device the platform
// currently has online, described the way GB/T 28181 §9 wants it.
//
// Fields a real platform would take from the device's own catalog
// (Parental, SafetyWay, RegisterWay, Secrecy) are reported as fixed values
// until a channel model exists.
type CatalogItem struct {
	deviceID     string
	name         string
	manufacturer string
	model        string
	owner        string
	civilCode    string
	address      string
	status       string

	parental    int
	safetyWay   int
	registerWay int
	secrecy     int
}

// CatalogItemParams is the flat input for NewCatalogItem. Only DeviceID is
// required; everything else has the documented default.
type CatalogItemParams struct {
	DeviceID     string
	Name         string
	Manufacturer string
	Model        string
	Owner        string
	CivilCode    string
	Address      string
	Status       string

	Parental    int
	SafetyWay   int
	RegisterWay int
	Secrecy     int
}

// NewCatalogItem builds one catalog entry. DeviceID is mandatory: it is the
// only field an upstream can act on. CivilCode falls back to the first six
// digits of the device id, which is the administrative region code GB/T
// 28181 puts there.
func NewCatalogItem(p CatalogItemParams) (CatalogItem, error) {
	deviceID := strings.TrimSpace(p.DeviceID)
	if deviceID == "" {
		return CatalogItem{}, fmt.Errorf("model: catalog item without a device id")
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = deviceID
	}
	manufacturer := strings.TrimSpace(p.Manufacturer)
	if manufacturer == "" {
		manufacturer = CatalogManufacturer
	}
	model := strings.TrimSpace(p.Model)
	if model == "" {
		model = CatalogModel
	}
	status := strings.TrimSpace(p.Status)
	if status == "" {
		status = CatalogStatusON
	}
	civilCode := strings.TrimSpace(p.CivilCode)
	if civilCode == "" && len(deviceID) >= 6 {
		civilCode = deviceID[:6]
	}
	// GB/T 28181: 1 is "registered through the standard protocol", which
	// is the only way a device reaches this simulator's online table. An
	// unspecified value (0) therefore means 1 rather than "unknown".
	registerWay := p.RegisterWay
	if registerWay == 0 {
		registerWay = 1
	}
	return CatalogItem{
		deviceID:     deviceID,
		name:         name,
		manufacturer: manufacturer,
		model:        model,
		owner:        strings.TrimSpace(p.Owner),
		civilCode:    civilCode,
		address:      strings.TrimSpace(p.Address),
		status:       status,
		parental:     p.Parental,
		safetyWay:    p.SafetyWay,
		registerWay:  registerWay,
		secrecy:      p.Secrecy,
	}, nil
}

// NewCatalogItemFromDevice renders one row of the online device table as a
// catalog entry. The table is the platform's only source of truth about who
// is connected, so the catalog is derived from it rather than kept twice.
func NewCatalogItemFromDevice(d DownstreamDevice) (CatalogItem, error) {
	return NewCatalogItem(CatalogItemParams{
		DeviceID: d.DeviceID(),
		Name:     d.DeviceID(),
		Address:  d.Addr(),
		Status:   CatalogStatusON,
	})
}

// DeviceID returns the entry's GB/T 28181 id.
func (c CatalogItem) DeviceID() string { return c.deviceID }

// Name returns the device's display name.
func (c CatalogItem) Name() string { return c.name }

// Manufacturer returns the reported manufacturer.
func (c CatalogItem) Manufacturer() string { return c.manufacturer }

// Model returns the reported model.
func (c CatalogItem) Model() string { return c.model }

// Owner returns the reported owner, or "".
func (c CatalogItem) Owner() string { return c.owner }

// CivilCode returns the administrative region code.
func (c CatalogItem) CivilCode() string { return c.civilCode }

// Address returns the address the platform knows the device at.
func (c CatalogItem) Address() string { return c.address }

// Status returns "ON" for every device in the online table.
func (c CatalogItem) Status() string { return c.status }

// Parental reports whether the device has sub-devices (0 until a channel
// model exists).
func (c CatalogItem) Parental() int { return c.parental }

// SafetyWay reports the signalling safety mode.
func (c CatalogItem) SafetyWay() int { return c.safetyWay }

// RegisterWay reports how the device joined: 1, registered.
func (c CatalogItem) RegisterWay() int { return c.registerWay }

// Secrecy reports the secrecy class.
func (c CatalogItem) Secrecy() int { return c.secrecy }

// HasItem reports whether c was produced by NewCatalogItem.
func (c CatalogItem) HasItem() bool { return c.deviceID != "" }

// String renders a log-safe one-line summary.
func (c CatalogItem) String() string {
	return fmt.Sprintf("CatalogItem<device_id=%s status=%s>", c.deviceID, c.status)
}

// Catalog is a platform's answer to a catalog query: who it has online,
// stamped with the query's sequence number so the asker can match it to the
// question.
type Catalog struct {
	deviceID string // the platform answering
	sn       uint32
	items    []CatalogItem
}

// NewCatalog builds a catalog answer. The platform id is mandatory — it is
// the DeviceID of the response — while SN may be 0 when the query carried
// none, and an empty item list is a legitimate answer (nothing online).
func NewCatalog(deviceID string, sn uint32, items []CatalogItem) (Catalog, error) {
	id := strings.TrimSpace(deviceID)
	if id == "" {
		return Catalog{}, fmt.Errorf("model: catalog without a device id")
	}
	cp := make([]CatalogItem, len(items))
	copy(cp, items)
	return Catalog{deviceID: id, sn: sn, items: cp}, nil
}

// NewCatalogFromDevices renders the whole online device table as a catalog
// answer, which is the only way this simulator builds one.
func NewCatalogFromDevices(platformID string, sn uint32, devices []DownstreamDevice) (Catalog, error) {
	items := make([]CatalogItem, 0, len(devices))
	for _, d := range devices {
		item, err := NewCatalogItemFromDevice(d)
		if err != nil {
			return Catalog{}, err
		}
		items = append(items, item)
	}
	return NewCatalog(platformID, sn, items)
}

// DeviceID returns the id of the platform answering.
func (c Catalog) DeviceID() string { return c.deviceID }

// SN returns the sequence number echoed from the query.
func (c Catalog) SN() uint32 { return c.sn }

// Items returns the catalog entries, in the order they were given.
func (c Catalog) Items() []CatalogItem { return c.items }

// SumNum returns how many entries the catalog carries — the count the XML
// reports.
func (c Catalog) SumNum() int { return len(c.items) }

// HasCatalog reports whether c was produced by NewCatalog.
func (c Catalog) HasCatalog() bool { return c.deviceID != "" }

// String renders a log-safe one-line summary.
func (c Catalog) String() string {
	return fmt.Sprintf("Catalog<device_id=%s sn=%d sum=%d>", c.deviceID, c.sn, len(c.items))
}
