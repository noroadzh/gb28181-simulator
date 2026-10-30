// Package model — operations commands added by device-control-coverage:
// DeviceStatus, Broadcast, alarm query, ConfigDownload, DeviceUpgrade.
package model

import (
	"fmt"
	"strings"
)

// --- DeviceStatus ---------------------------------------------------------

// CmdTypeDeviceStatus is the MANSCDP device-status command.
const CmdTypeDeviceStatus = "DeviceStatus"

// CmdTypeBroadcast is the MANSCDP voice-broadcast command.
const CmdTypeBroadcast = "Broadcast"

// CmdTypeDeviceUpgrade is the MANSCDP firmware-upgrade command.
const CmdTypeDeviceUpgrade = "DeviceUpgrade"

// DeviceStatusQuery is a DeviceStatus question.
type DeviceStatusQuery struct {
	SN       uint32
	DeviceID string
}

// DeviceStatusResponse is the answer: whether the device is on and healthy.
type DeviceStatusResponse struct {
	SN         uint32
	DeviceID   string
	Result     string // "OK" or "ERROR"
	Online     string // "ONLINE" / "OFFLINE"
	Status     string // "OK" / "ERROR"
	DeviceTime string
}

// --- Broadcast ------------------------------------------------------------

// BroadcastCommand is what a platform sends to start a broadcast.
type BroadcastCommand struct {
	SN       uint32
	SourceID string
	TargetID string
}

// BroadcastAck is the device's answer.
type BroadcastAck struct {
	SN       uint32
	TargetID string
	Result   string
}

// --- Alarm query ----------------------------------------------------------

// AlarmQuery asks a device for the alarms it saw in a window.
type AlarmQuery struct {
	SN            uint32
	DeviceID      string
	StartPriority int
	EndPriority   int
	StartTime     string
	EndTime       string
}

// AlarmRecord is one alarm in an AlarmQuery answer.
type AlarmRecord struct {
	DeviceID    string
	AlarmTime   string
	Priority    int
	Method      int
	Description string
}

// AlarmQueryResponse is the answer to an AlarmQuery.
type AlarmQueryResponse struct {
	SN       uint32
	DeviceID string
	SumNum   int
	Items    []AlarmRecord
}

// --- ConfigDownload -------------------------------------------------------

// BasicConfig is the basic-parameter configuration a device reports. The
// JSON tags are the HTTP contract; the XML shape lives in the codec.
type BasicConfig struct {
	Name              string `json:"name,omitempty"`
	Manufacturer      string `json:"manufacturer,omitempty"`
	Model             string `json:"model,omitempty"`
	Firmware          string `json:"firmware,omitempty"`
	ChannelCount      int    `json:"channel_count,omitempty"`
	HeartbeatInterval int    `json:"heartbeat_interval,omitempty"`
	HeartbeatTimeout  int    `json:"heartbeat_timeout,omitempty"`
}

// ConfigQuery asks for a configuration block by name.
type ConfigQuery struct {
	SN         uint32
	DeviceID   string
	ConfigType string
}

// ConfigResponse carries a configuration block.
type ConfigResponse struct {
	SN         uint32
	DeviceID   string
	ConfigType string
	Result     string
	Basic      BasicConfig
}

// ConfigSet is a configuration write.
type ConfigSet struct {
	SN         uint32
	DeviceID   string
	ConfigType string
	Basic      BasicConfig
}

// --- Upgrade --------------------------------------------------------------

// UpgradeCommand asks a device to move to Firmware.
type UpgradeCommand struct {
	SN       uint32
	DeviceID string
	Firmware string
}

// OpsAck is the generic "I did it / I did not" answer for operations that
// carry no body of their own.
type OpsAck struct {
	SN       uint32
	DeviceID string
	Result   string
}

// ResultOK and ResultError are the only two result values GB/T 28181 uses on
// these answers.
const (
	ResultOK    = "OK"
	ResultError = "ERROR"
)

// --- helpers --------------------------------------------------------------

// DeviceStatusResult picks the result words from a node status string.
func DeviceStatusResult(online bool) (result, onlineWord, status string) {
	if online {
		return ResultOK, "ONLINE", "OK"
	}
	return ResultOK, "OFFLINE", "ERROR"
}

// ValidateDeviceID rejects an empty or obviously malformed id.
func ValidateDeviceID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("model: device id is empty")
	}
	return nil
}
