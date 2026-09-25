package manscdp

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// ptzControlEnvelope is the wire shape of a PTZ control command.
type ptzControlEnvelope struct {
	XMLName   xml.Name `xml:"Control"`
	CmdType   string   `xml:"CmdType"`
	SN        uint32   `xml:"SN"`
	DeviceID  string   `xml:"DeviceID"`
	ChannelID string   `xml:"ChannelID,omitempty"`
	Command   string   `xml:"Cmd,omitempty"`
	Speed     int      `xml:"Speed,omitempty"`
	Priority  int      `xml:"Priority,omitempty"`
}

// DecodePTZControl parses a MANSCDP PTZ control command body.
func (c *MANSCDPCodecAdapter) DecodePTZControl(body string) (model.PTZControl, error) {
	trimmed := strings.TrimSpace(body)
	trimmed = strings.TrimPrefix(trimmed, xmlHeader)
	trimmed = strings.TrimSpace(trimmed)

	var env ptzControlEnvelope
	if err := xml.Unmarshal([]byte(trimmed), &env); err != nil {
		return model.PTZControl{}, fmt.Errorf("manscdp: unmarshal ptz control: %w", err)
	}
	return model.NewPTZControl(env.DeviceID, env.ChannelID, env.Command, env.SN, env.Speed, env.Priority), nil
}

// MarshalPTZControl renders a PTZ control command body.
func (c *MANSCDPCodecAdapter) MarshalPTZControl(control model.PTZControl) (string, error) {
	if !IsValidDeviceID(control.DeviceID) {
		return "", fmt.Errorf("manscdp: ptz control without a device id")
	}
	body, err := xml.MarshalIndent(ptzControlEnvelope{
		CmdType:   model.CmdTypeDeviceControl,
		SN:        control.SN,
		DeviceID:  control.DeviceID,
		ChannelID: control.ChannelID,
		Command:   control.Command,
		Speed:     control.Speed,
		Priority:  control.Priority,
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("manscdp: marshal ptz control: %w", err)
	}
	return xmlHeader + string(body) + "\n", nil
}
