// Package model — DeviceInfo MANSCDP command.
package model

import (
	"fmt"
	"strings"
)

// DeviceInfoQuery is the wire shape of a DeviceInfo query request.
type DeviceInfoQuery struct {
	CmdType  string
	SN       uint32
	DeviceID string
}

// DeviceInfoResponse is the wire shape of a DeviceInfo query response.
type DeviceInfoResponse struct {
	CmdType  string
	SN       uint32
	DeviceID string
	SumNum   int
	Items    []DeviceInfoItem
}

// DeviceInfoItem is one entry in a DeviceInfo response list.
type DeviceInfoItem struct {
	deviceID     string
	name         string
	manufacturer string
	model        string
	firmware     string
	runMode      int
	civilCode    string
	address      string
	parental     int
	safetyWay    int
	registerWay  int
	secrecy      int
	status       string
}

// DeviceInfoItemParams is the flat input for NewDeviceInfoItem.
type DeviceInfoItemParams struct {
	DeviceID     string
	Name         string
	Manufacturer string
	Model        string
	Firmware     string
	RunMode      int
	CivilCode    string
	Address      string
	Parental     int
	SafetyWay    int
	RegisterWay  int
	Secrecy      int
	Status       string
}

// NewDeviceInfoItem builds one DeviceInfo entry.
func NewDeviceInfoItem(p DeviceInfoItemParams) (DeviceInfoItem, error) {
	deviceID := strings.TrimSpace(p.DeviceID)
	if deviceID == "" {
		return DeviceInfoItem{}, fmt.Errorf("model: device info item without a device id")
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = deviceID
	}
	manufacturer := strings.TrimSpace(p.Manufacturer)
	if manufacturer == "" {
		manufacturer = "gb28181-simulator"
	}
	model := strings.TrimSpace(p.Model)
	if model == "" {
		model = "simulator"
	}
	status := strings.TrimSpace(p.Status)
	if status == "" {
		status = "ON"
	}
	civilCode := strings.TrimSpace(p.CivilCode)
	if civilCode == "" && len(deviceID) >= 6 {
		civilCode = deviceID[:6]
	}
	registerWay := p.RegisterWay
	if registerWay == 0 {
		registerWay = 1
	}
	return DeviceInfoItem{
		deviceID:     deviceID,
		name:         name,
		manufacturer: manufacturer,
		model:        model,
		firmware:     strings.TrimSpace(p.Firmware),
		runMode:      p.RunMode,
		civilCode:    civilCode,
		address:      strings.TrimSpace(p.Address),
		parental:     p.Parental,
		safetyWay:    p.SafetyWay,
		registerWay:  registerWay,
		secrecy:      p.Secrecy,
		status:       status,
	}, nil
}

// DeviceID returns the GB/T 28181 id.
func (d DeviceInfoItem) DeviceID() string { return d.deviceID }

// Name returns the device's display name.
func (d DeviceInfoItem) Name() string { return d.name }

// Manufacturer returns the reported manufacturer.
func (d DeviceInfoItem) Manufacturer() string { return d.manufacturer }

// Model returns the reported model.
func (d DeviceInfoItem) Model() string { return d.model }

// Firmware returns the firmware version.
func (d DeviceInfoItem) Firmware() string { return d.firmware }

// RunMode returns the run mode.
func (d DeviceInfoItem) RunMode() int { return d.runMode }

// CivilCode returns the administrative region code.
func (d DeviceInfoItem) CivilCode() string { return d.civilCode }

// Address returns the address the platform knows the device at.
func (d DeviceInfoItem) Address() string { return d.address }

// Parental reports whether the device has sub-devices.
func (d DeviceInfoItem) Parental() int { return d.parental }

// SafetyWay reports the signalling safety mode.
func (d DeviceInfoItem) SafetyWay() int { return d.safetyWay }

// RegisterWay reports how the device joined.
func (d DeviceInfoItem) RegisterWay() int { return d.registerWay }

// Secrecy reports the secrecy class.
func (d DeviceInfoItem) Secrecy() int { return d.secrecy }

// Status returns the device status.
func (d DeviceInfoItem) Status() string { return d.status }

// String renders a log-safe one-line summary.
func (d DeviceInfoItem) String() string {
	return fmt.Sprintf("DeviceInfoItem<device_id=%s status=%s>", d.deviceID, d.status)
}

// NewDeviceInfoQuery builds a DeviceInfo query.
func NewDeviceInfoQuery(deviceID string, sn uint32) DeviceInfoQuery {
	return DeviceInfoQuery{
		CmdType:  CmdTypeDeviceInfo,
		SN:       sn,
		DeviceID: strings.TrimSpace(deviceID),
	}
}

// NewDeviceInfoResponse builds a DeviceInfo response.
func NewDeviceInfoResponse(deviceID string, sn uint32, items []DeviceInfoItem) (DeviceInfoResponse, error) {
	id := strings.TrimSpace(deviceID)
	if id == "" {
		return DeviceInfoResponse{}, fmt.Errorf("model: device info response without a device id")
	}
	cp := make([]DeviceInfoItem, len(items))
	copy(cp, items)
	return DeviceInfoResponse{
		CmdType:  CmdTypeDeviceInfo,
		SN:       sn,
		DeviceID: id,
		SumNum:   len(items),
		Items:    cp,
	}, nil
}
