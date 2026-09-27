// Package model — ConfigDownload MANSCDP command.
package model

import (
	"fmt"
	"strings"
)

// ConfigCommand is the wire shape of a ConfigDownload command.
type ConfigCommand struct {
	CmdType    string
	SN         uint32
	DeviceID   string
	ConfigType string
}

// NewConfigCommand builds a ConfigDownload command.
func NewConfigCommand(deviceID, configType string, sn uint32) ConfigCommand {
	return ConfigCommand{
		CmdType:    CmdTypeConfigDownload,
		SN:         sn,
		DeviceID:   strings.TrimSpace(deviceID),
		ConfigType: strings.TrimSpace(configType),
	}
}

// String renders a log-safe one-line summary.
func (c ConfigCommand) String() string {
	return fmt.Sprintf("ConfigCommand<device_id=%s type=%s>", c.DeviceID, c.ConfigType)
}

// ConfigAck is the wire shape of a ConfigDownload acknowledgement response.
type ConfigAck struct {
	CmdType  string
	SN       uint32
	DeviceID string
	Result   string
}

// NewConfigAck builds a ConfigDownload acknowledgement response.
func NewConfigAck(deviceID string, sn uint32, result string) ConfigAck {
	return ConfigAck{
		CmdType:  CmdTypeConfigDownload,
		SN:       sn,
		DeviceID: strings.TrimSpace(deviceID),
		Result:   strings.TrimSpace(result),
	}
}
