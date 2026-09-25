package manscdp

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// presetQueryEnvelope is the wire shape of a Preset query.
type presetQueryEnvelope struct {
	XMLName   xml.Name `xml:"Query"`
	CmdType   string   `xml:"CmdType"`
	SN        string   `xml:"SN"`
	DeviceID  string   `xml:"DeviceID"`
	ChannelID string   `xml:"ChannelID,omitempty"`
}

// presetSetEnvelope is the wire shape of a Preset set command.
type presetSetEnvelope struct {
	XMLName    xml.Name `xml:"Control"`
	CmdType    string   `xml:"CmdType"`
	SN         string   `xml:"SN"`
	DeviceID   string   `xml:"DeviceID"`
	ChannelID  string   `xml:"ChannelID,omitempty"`
	PresetIndex int    `xml:"PresetIndex,omitempty"`
	Name       string   `xml:"Name,omitempty"`
}

// presetListResponseEnvelope is the wire shape of a Preset list response.
type presetListResponseEnvelope struct {
	XMLName xml.Name `xml:"Response"`
	CmdType string   `xml:"CmdType"`
	SN      uint32   `xml:"SN"`
	DeviceID string  `xml:"DeviceID"`
	SumNum  int      `xml:"SumNum"`
	Items   []presetItemEnvelope `xml:"PresetList>Item"`
}

// presetAckEnvelope is the wire shape of a Preset set acknowledgement response.
type presetAckEnvelope struct {
	XMLName  xml.Name `xml:"Response"`
	CmdType  string   `xml:"CmdType"`
	SN       uint32   `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	Result   string   `xml:"Result"`
}

// presetItemEnvelope is the wire shape of one Preset item.
type presetItemEnvelope struct {
	PresetIndex int    `xml:"PresetIndex,omitempty"`
	Name        string `xml:"Name,omitempty"`
}

// DecodePresetQuery parses a MANSCDP Preset query body.
func (c *MANSCDPCodecAdapter) DecodePresetQuery(body string) (model.PresetQuery, error) {
	trimmed := strings.TrimSpace(body)
	trimmed = strings.TrimPrefix(trimmed, xmlHeader)
	trimmed = strings.TrimSpace(trimmed)

	var env presetQueryEnvelope
	if err := xml.Unmarshal([]byte(trimmed), &env); err != nil {
		return model.PresetQuery{}, fmt.Errorf("manscdp: unmarshal preset query: %w", err)
	}
	sn, err := parseSN(env.SN)
	if err != nil {
		return model.PresetQuery{}, err
	}
	return model.NewPresetQuery(env.DeviceID, env.ChannelID, sn), nil
}

// DecodePresetSet parses a MANSCDP Preset set command body.
func (c *MANSCDPCodecAdapter) DecodePresetSet(body string) (model.PresetSet, error) {
	trimmed := strings.TrimSpace(body)
	trimmed = strings.TrimPrefix(trimmed, xmlHeader)
	trimmed = strings.TrimSpace(trimmed)

	var env presetSetEnvelope
	if err := xml.Unmarshal([]byte(trimmed), &env); err != nil {
		return model.PresetSet{}, fmt.Errorf("manscdp: unmarshal preset set: %w", err)
	}
	sn, err := parseSN(env.SN)
	if err != nil {
		return model.PresetSet{}, err
	}
	return model.NewPresetSet(env.DeviceID, env.ChannelID, sn, env.PresetIndex, env.Name), nil
}

// MarshalPresetList renders a Preset list response.
func (c *MANSCDPCodecAdapter) MarshalPresetList(resp model.PresetListResponse) (string, error) {
	if !IsValidDeviceID(resp.DeviceID) {
		return "", fmt.Errorf("manscdp: preset list response without a device id")
	}
	items := make([]presetItemEnvelope, 0, len(resp.Items))
	for _, it := range resp.Items {
		items = append(items, presetItemEnvelope{
			PresetIndex: it.PresetIndex,
			Name:        it.Name,
		})
	}
	body, err := xml.MarshalIndent(presetListResponseEnvelope{
		CmdType:  model.CmdTypePresetQuery,
		SN:       resp.SN,
		DeviceID: resp.DeviceID,
		SumNum:   resp.SumNum,
		Items:    items,
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("manscdp: marshal preset list: %w", err)
	}
	return xmlHeader + string(body) + "\n", nil
}

// MarshalPresetAck renders a Preset set acknowledgement response.
func (c *MANSCDPCodecAdapter) MarshalPresetAck(ack model.PresetAck) (string, error) {
	if !IsValidDeviceID(ack.DeviceID) {
		return "", fmt.Errorf("manscdp: preset ack without a device id")
	}
	body, err := xml.MarshalIndent(presetAckEnvelope{
		CmdType:  model.CmdTypePreset,
		SN:       ack.SN,
		DeviceID: ack.DeviceID,
		Result:   ack.Result,
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("manscdp: marshal preset ack: %w", err)
	}
	return xmlHeader + string(body) + "\n", nil
}
