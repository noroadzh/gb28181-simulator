package manscdp

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// deviceInfoQueryEnvelope is the wire shape of a DeviceInfo query.
type deviceInfoQueryEnvelope struct {
	XMLName   xml.Name `xml:"Query"`
	CmdType   string   `xml:"CmdType"`
	SN        string   `xml:"SN"`
	DeviceID  string   `xml:"DeviceID"`
	StartTime string   `xml:"StartTime,omitempty"`
	EndTime   string   `xml:"EndTime,omitempty"`
}

// deviceInfoResponseEnvelope is the wire shape of a DeviceInfo response.
type deviceInfoResponseEnvelope struct {
	XMLName    xml.Name `xml:"Response"`
	CmdType    string   `xml:"CmdType"`
	SN         uint32   `xml:"SN"`
	DeviceID   string   `xml:"DeviceID"`
	SumNum     int      `xml:"SumNum"`
	DeviceList []deviceInfoItemEnvelope `xml:"DeviceList>Item"`
}

// deviceInfoItemEnvelope is the wire shape of one DeviceInfo item.
type deviceInfoItemEnvelope struct {
	DeviceID     string `xml:"DeviceID"`
	Name         string `xml:"Name"`
	Manufacturer string `xml:"Manufacturer"`
	Model        string `xml:"Model"`
	Firmware     string `xml:"Firmware,omitempty"`
	RunMode      int    `xml:"RunMode,omitempty"`
	CivilCode    string `xml:"CivilCode,omitempty"`
	Address      string `xml:"Address,omitempty"`
	Parental     int    `xml:"Parental,omitempty"`
	SafetyWay    int    `xml:"SafetyWay,omitempty"`
	RegisterWay  int    `xml:"RegisterWay,omitempty"`
	Secrecy      int    `xml:"Secrecy,omitempty"`
	Status       string `xml:"Status,omitempty"`
}

// deviceInfoItemEnvelopeToModel converts a wire envelope to a domain model item.
func deviceInfoItemEnvelopeToModel(env deviceInfoItemEnvelope) model.DeviceInfoItem {
	item, _ := model.NewDeviceInfoItem(model.DeviceInfoItemParams{
		DeviceID:     env.DeviceID,
		Name:         env.Name,
		Manufacturer: env.Manufacturer,
		Model:        env.Model,
		Firmware:     env.Firmware,
		RunMode:      env.RunMode,
		CivilCode:    env.CivilCode,
		Address:      env.Address,
		Parental:     env.Parental,
		SafetyWay:    env.SafetyWay,
		RegisterWay:  env.RegisterWay,
		Secrecy:      env.Secrecy,
		Status:       env.Status,
	})
	return item
}

// DecodeDeviceInfoQuery parses a MANSCDP DeviceInfo query body.
func (c *MANSCDPCodecAdapter) DecodeDeviceInfoQuery(body string) (model.DeviceInfoQuery, error) {
	trimmed := strings.TrimSpace(body)
	trimmed = strings.TrimPrefix(trimmed, xmlHeader)
	trimmed = strings.TrimSpace(trimmed)

	var env deviceInfoQueryEnvelope
	if err := xml.Unmarshal([]byte(trimmed), &env); err != nil {
		return model.DeviceInfoQuery{}, fmt.Errorf("manscdp: unmarshal device info query: %w", err)
	}
	sn, err := parseSN(env.SN)
	if err != nil {
		return model.DeviceInfoQuery{}, err
	}
	return model.NewDeviceInfoQuery(env.DeviceID, sn), nil
}

// DecodeDeviceInfoResponse parses a MANSCDP DeviceInfo response body.
func (c *MANSCDPCodecAdapter) DecodeDeviceInfoResponse(body string) (model.DeviceInfoResponse, error) {
	trimmed := strings.TrimSpace(body)
	trimmed = strings.TrimPrefix(trimmed, xmlHeader)
	trimmed = strings.TrimSpace(trimmed)

	var env deviceInfoResponseEnvelope
	if err := xml.Unmarshal([]byte(trimmed), &env); err != nil {
		return model.DeviceInfoResponse{}, fmt.Errorf("manscdp: unmarshal device info response: %w", err)
	}
	items := make([]model.DeviceInfoItem, len(env.DeviceList))
	for i, ie := range env.DeviceList {
		items[i] = deviceInfoItemEnvelopeToModel(ie)
	}
	resp, err := model.NewDeviceInfoResponse(env.DeviceID, env.SN, items)
	if err != nil {
		return model.DeviceInfoResponse{}, fmt.Errorf("manscdp: build device info response: %w", err)
	}
	return resp, nil
}

// MarshalDeviceInfoResponse renders a DeviceInfo answer.
func (c *MANSCDPCodecAdapter) MarshalDeviceInfoResponse(resp model.DeviceInfoResponse) (string, error) {
	if strings.TrimSpace(resp.DeviceID) == "" || len(strings.TrimSpace(resp.DeviceID)) != 20 {
		return "", fmt.Errorf("manscdp: device info response without a device id")
	}
	items := make([]deviceInfoItemEnvelope, 0, len(resp.Items))
	for _, it := range resp.Items {
		items = append(items, deviceInfoItemEnvelope{
			DeviceID:     it.DeviceID(),
			Name:         it.Name(),
			Manufacturer: it.Manufacturer(),
			Model:        it.Model(),
			Firmware:     it.Firmware(),
			RunMode:      it.RunMode(),
			CivilCode:    it.CivilCode(),
			Address:      it.Address(),
			Parental:     it.Parental(),
			SafetyWay:    it.SafetyWay(),
			RegisterWay:  it.RegisterWay(),
			Secrecy:      it.Secrecy(),
			Status:       it.Status(),
		})
	}
	body, err := xml.MarshalIndent(deviceInfoResponseEnvelope{
		CmdType:    model.CmdTypeDeviceInfo,
		SN:         resp.SN,
		DeviceID:   resp.DeviceID,
		SumNum:     resp.SumNum,
		DeviceList: items,
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("manscdp: marshal device info response: %w", err)
	}
	return xmlHeader + string(body) + "\n", nil
}

// IsValidDeviceID reports whether id looks like a GB/T 28181 20-byte id.
func IsValidDeviceID(id string) bool {
	return strings.TrimSpace(id) != "" && len(strings.TrimSpace(id)) == 20
}
