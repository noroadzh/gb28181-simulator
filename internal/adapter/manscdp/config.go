package manscdp

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// configCommandEnvelope is the wire shape of a ConfigDownload command.
type configCommandEnvelope struct {
	XMLName    xml.Name `xml:"Query"`
	CmdType    string   `xml:"CmdType"`
	SN         string   `xml:"SN"`
	DeviceID   string   `xml:"DeviceID"`
	ConfigType string   `xml:"ConfigType,omitempty"`
	ItemNum    int      `xml:"ItemNum,omitempty"`
}

// configAckEnvelope is the wire shape of a ConfigDownload ack response.
type configAckEnvelope struct {
	XMLName  xml.Name `xml:"Response"`
	CmdType  string   `xml:"CmdType"`
	SN       uint32   `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	Result   string   `xml:"Result"`
}

// DecodeConfigCommand parses a MANSCDP ConfigDownload command body.
func (c *MANSCDPCodecAdapter) DecodeConfigCommand(body string) (model.ConfigCommand, error) {
	trimmed := strings.TrimSpace(body)
	trimmed = strings.TrimPrefix(trimmed, xmlHeader)
	trimmed = strings.TrimSpace(trimmed)

	var env configCommandEnvelope
	if err := xml.Unmarshal([]byte(trimmed), &env); err != nil {
		return model.ConfigCommand{}, fmt.Errorf("manscdp: unmarshal config command: %w", err)
	}
	sn, err := parseSN(env.SN)
	if err != nil {
		return model.ConfigCommand{}, err
	}
	return model.NewConfigCommand(env.DeviceID, env.ConfigType, sn), nil
}

// MarshalConfigAck renders a ConfigDownload ack response.
func (c *MANSCDPCodecAdapter) MarshalConfigAck(ack model.ConfigAck) (string, error) {
	if !IsValidDeviceID(ack.DeviceID) {
		return "", fmt.Errorf("manscdp: config ack without a device id")
	}
	body, err := xml.MarshalIndent(configAckEnvelope{
		CmdType:  model.CmdTypeConfigDownload,
		SN:       ack.SN,
		DeviceID: ack.DeviceID,
		Result:   ack.Result,
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("manscdp: marshal config ack: %w", err)
	}
	return xmlHeader + string(body) + "\n", nil
}
