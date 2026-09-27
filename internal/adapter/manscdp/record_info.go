package manscdp

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// recordInfoQueryEnvelope is the wire shape of a RecordInfo query.
type recordInfoQueryEnvelope struct {
	XMLName   xml.Name `xml:"Query"`
	CmdType   string   `xml:"CmdType"`
	SN        string   `xml:"SN"`
	DeviceID  string   `xml:"DeviceID"`
	ChannelID string   `xml:"ChannelID,omitempty"`
	StartTime string   `xml:"StartTime,omitempty"`
	EndTime   string   `xml:"EndTime,omitempty"`
	Secrecy   int      `xml:"Secrecy,omitempty"`
}

// recordInfoResponseEnvelope is the wire shape of a RecordInfo response.
type recordInfoResponseEnvelope struct {
	XMLName    xml.Name                 `xml:"Response"`
	CmdType    string                   `xml:"CmdType"`
	SN         uint32                   `xml:"SN"`
	DeviceID   string                   `xml:"DeviceID"`
	SumNum     int                      `xml:"SumNum"`
	DeviceList []recordInfoItemEnvelope `xml:"DeviceList>Item"`
}

// recordInfoItemEnvelope is the wire shape of one RecordInfo item.
type recordInfoItemEnvelope struct {
	Name         string `xml:"Name,omitempty"`
	DeviceID     string `xml:"DeviceID,omitempty"`
	ChannelID    string `xml:"ChannelID,omitempty"`
	StartTime    string `xml:"StartTime,omitempty"`
	EndTime      string `xml:"EndTime,omitempty"`
	Secrecy      int    `xml:"Secrecy,omitempty"`
	FilePath     string `xml:"FilePath,omitempty"`
	VideoCodec   string `xml:"VideoCodec,omitempty"`
	AudioCodec   string `xml:"AudioCodec,omitempty"`
	VideoBitrate int    `xml:"VideoBitrate,omitempty"`
	AudioBitrate int    `xml:"AudioBitrate,omitempty"`
}

// recordInfoItemEnvelopeToModel converts a wire envelope to a domain model item.
func recordInfoItemEnvelopeToModel(env recordInfoItemEnvelope) model.RecordInfoItem {
	return model.NewRecordInfoItem(model.RecordInfoItemParams{
		Name:         env.Name,
		DeviceID:     env.DeviceID,
		ChannelID:    env.ChannelID,
		StartTime:    env.StartTime,
		EndTime:      env.EndTime,
		Secrecy:      env.Secrecy,
		FilePath:     env.FilePath,
		VideoCodec:   env.VideoCodec,
		AudioCodec:   env.AudioCodec,
		VideoBitrate: env.VideoBitrate,
		AudioBitrate: env.AudioBitrate,
	})
}

// DecodeRecordInfoQuery parses a MANSCDP RecordInfo query body.
func (c *MANSCDPCodecAdapter) DecodeRecordInfoQuery(body string) (model.RecordInfoQuery, error) {
	trimmed := strings.TrimSpace(body)
	trimmed = strings.TrimPrefix(trimmed, xmlHeader)
	trimmed = strings.TrimSpace(trimmed)

	var env recordInfoQueryEnvelope
	if err := xml.Unmarshal([]byte(trimmed), &env); err != nil {
		return model.RecordInfoQuery{}, fmt.Errorf("manscdp: unmarshal record info query: %w", err)
	}
	sn, err := parseSN(env.SN)
	if err != nil {
		return model.RecordInfoQuery{}, err
	}
	return model.NewRecordInfoQuery(env.DeviceID, sn, env.StartTime, env.EndTime), nil
}

// DecodeRecordInfoResponse parses a MANSCDP RecordInfo response body.
func (c *MANSCDPCodecAdapter) DecodeRecordInfoResponse(body string) (model.RecordInfoResponse, error) {
	trimmed := strings.TrimSpace(body)
	trimmed = strings.TrimPrefix(trimmed, xmlHeader)
	trimmed = strings.TrimSpace(trimmed)

	var env recordInfoResponseEnvelope
	if err := xml.Unmarshal([]byte(trimmed), &env); err != nil {
		return model.RecordInfoResponse{}, fmt.Errorf("manscdp: unmarshal record info response: %w", err)
	}
	items := make([]model.RecordInfoItem, len(env.DeviceList))
	for i, ie := range env.DeviceList {
		items[i] = recordInfoItemEnvelopeToModel(ie)
	}
	resp, err := model.NewRecordInfoResponse(env.DeviceID, env.SN, items)
	if err != nil {
		return model.RecordInfoResponse{}, fmt.Errorf("manscdp: build record info response: %w", err)
	}
	return resp, nil
}

// MarshalRecordInfoResponse renders a RecordInfo answer.
func (c *MANSCDPCodecAdapter) MarshalRecordInfoResponse(resp model.RecordInfoResponse) (string, error) {
	if !IsValidDeviceID(resp.DeviceID) {
		return "", fmt.Errorf("manscdp: record info response without a device id")
	}
	items := make([]recordInfoItemEnvelope, 0, len(resp.Items))
	for _, it := range resp.Items {
		items = append(items, recordInfoItemEnvelope{
			Name:         it.Name(),
			DeviceID:     it.DeviceID(),
			ChannelID:    it.ChannelID(),
			StartTime:    it.StartTime(),
			EndTime:      it.EndTime(),
			Secrecy:      it.Secrecy(),
			FilePath:     it.FilePath(),
			VideoCodec:   it.VideoCodec(),
			AudioCodec:   it.AudioCodec(),
			VideoBitrate: it.VideoBitrate(),
			AudioBitrate: it.AudioBitrate(),
		})
	}
	body, err := xml.MarshalIndent(recordInfoResponseEnvelope{
		CmdType:    model.CmdTypeRecordInfo,
		SN:         resp.SN,
		DeviceID:   resp.DeviceID,
		SumNum:     resp.SumNum,
		DeviceList: items,
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("manscdp: marshal record info response: %w", err)
	}
	return xmlHeader + string(body) + "\n", nil
}
