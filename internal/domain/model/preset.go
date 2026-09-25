// Package model — Preset MANSCDP commands.
package model

import (
	"fmt"
	"strings"
)

// PresetQuery is the wire shape of a preset position query.
type PresetQuery struct {
	CmdType  string
	SN       uint32
	DeviceID string
	ChannelID string
}

// NewPresetQuery builds a Preset query.
func NewPresetQuery(deviceID, channelID string, sn uint32) PresetQuery {
	return PresetQuery{
		CmdType:   CmdTypePresetQuery,
		SN:        sn,
		DeviceID:  strings.TrimSpace(deviceID),
		ChannelID: strings.TrimSpace(channelID),
	}
}

// PresetItem is one preset entry in a query response.
type PresetItem struct {
	PresetIndex int
	Name        string
}

// PresetListResponse is the wire shape of a preset query response.
type PresetListResponse struct {
	CmdType  string
	SN       uint32
	DeviceID string
	SumNum   int
	Items    []PresetItem
}

// NewPresetListResponse builds a Preset list response.
func NewPresetListResponse(deviceID string, sn uint32, items []PresetItem) (PresetListResponse, error) {
	id := strings.TrimSpace(deviceID)
	if id == "" {
		return PresetListResponse{}, fmt.Errorf("model: preset list response without a device id")
	}
	cp := make([]PresetItem, len(items))
	copy(cp, items)
	return PresetListResponse{
		CmdType:  CmdTypePresetQuery,
		SN:       sn,
		DeviceID: id,
		SumNum:   len(items),
		Items:    cp,
	}, nil
}

// String renders a log-safe one-line summary.
func (p PresetListResponse) String() string {
	return fmt.Sprintf("PresetListResponse<device_id=%s sum=%d>", p.DeviceID, p.SumNum)
}

// CmdTypePreset is the MANSCDP CmdType for Preset set commands.
const CmdTypePreset = "Preset"

// PresetSet is the wire shape of a preset set command.
type PresetSet struct {
	CmdType    string
	SN         uint32
	DeviceID   string
	ChannelID  string
	PresetIndex int
	Name       string
}

// NewPresetSet builds a Preset set command.
func NewPresetSet(deviceID, channelID string, sn uint32, presetIndex int, name string) PresetSet {
	return PresetSet{
		CmdType:    CmdTypePreset,
		SN:         sn,
		DeviceID:   strings.TrimSpace(deviceID),
		ChannelID:  strings.TrimSpace(channelID),
		PresetIndex: presetIndex,
		Name:       strings.TrimSpace(name),
	}
}

// String renders a log-safe one-line summary.
func (p PresetSet) String() string {
	return fmt.Sprintf("PresetSet<device_id=%s channel=%s index=%d>",
		p.DeviceID, p.ChannelID, p.PresetIndex)
}

// PresetAck is the wire shape of a preset set acknowledgement response.
type PresetAck struct {
	CmdType  string
	SN       uint32
	DeviceID string
	Result   string
}

// NewPresetAck builds a Preset set acknowledgement response.
func NewPresetAck(deviceID string, sn uint32, result string) PresetAck {
	return PresetAck{
		CmdType:  CmdTypePreset,
		SN:       sn,
		DeviceID: strings.TrimSpace(deviceID),
		Result:   strings.TrimSpace(result),
	}
}
