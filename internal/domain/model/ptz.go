// Package model — PTZ Control and Telemetry MANSCDP commands.
package model

import (
	"fmt"
	"strings"
)

// PTZControl is the wire shape of a PTZ control command.
type PTZControl struct {
	CmdType   string
	SN        uint32
	DeviceID  string
	ChannelID string
	Command   string
	Speed     int
	Priority  int
}

// NewPTZControl builds a PTZ control command.
func NewPTZControl(deviceID, channelID, command string, sn uint32, speed, priority int) PTZControl {
	return PTZControl{
		CmdType:   CmdTypeDeviceControl,
		SN:        sn,
		DeviceID:  strings.TrimSpace(deviceID),
		ChannelID: strings.TrimSpace(channelID),
		Command:   strings.TrimSpace(command),
		Speed:     speed,
		Priority:  priority,
	}
}

// String renders a log-safe one-line summary.
func (p PTZControl) String() string {
	return fmt.Sprintf("PTZControl<device_id=%s channel=%s cmd=%s speed=%d>",
		p.DeviceID, p.ChannelID, p.Command, p.Speed)
}

// Telemetry is the wire shape of a device telemetry report.
type Telemetry struct {
	DeviceID  string
	ChannelID string
	EventTime string
	DataItems []TelemetryItem
}

// TelemetryItem is one telemetry data point.
type TelemetryItem struct {
	Name  string
	Value string
}

// NewTelemetry builds a telemetry report.
func NewTelemetry(deviceID, channelID, eventTime string, items []TelemetryItem) Telemetry {
	return Telemetry{
		DeviceID:  strings.TrimSpace(deviceID),
		ChannelID: strings.TrimSpace(channelID),
		EventTime: strings.TrimSpace(eventTime),
		DataItems: items,
	}
}

// String renders a log-safe one-line summary.
func (t Telemetry) String() string {
	return fmt.Sprintf("Telemetry<device_id=%s items=%d>", t.DeviceID, len(t.DataItems))
}
