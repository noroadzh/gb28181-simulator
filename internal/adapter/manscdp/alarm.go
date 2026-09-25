package manscdp

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// alarmNotifyEnvelope is the wire shape of an Alarm notify.
type alarmNotifyEnvelope struct {
	XMLName      xml.Name `xml:"Notify"`
	CmdType      string   `xml:"CmdType"`
	SN           string   `xml:"SN"`
	DeviceID     string   `xml:"DeviceID"`
	ChannelID    string   `xml:"ChannelID,omitempty"`
	AlarmPriority int     `xml:"AlarmPriority,omitempty"`
	AlarmMethod  int      `xml:"AlarmMethod,omitempty"`
	EventType    string   `xml:"EventType,omitempty"`
	EventTime    string   `xml:"EventTime,omitempty"`
	Description  string   `xml:"Description,omitempty"`
}

// alarmAckEnvelope is the wire shape of an Alarm acknowledgement response.
type alarmAckEnvelope struct {
	XMLName  xml.Name `xml:"Response"`
	CmdType  string   `xml:"CmdType"`
	SN       uint32   `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	Result   string   `xml:"Result"`
}

// DecodeAlarmNotify parses a MANSCDP Alarm notify body.
func (c *MANSCDPCodecAdapter) DecodeAlarmNotify(body string) (model.AlarmNotify, error) {
	trimmed := strings.TrimSpace(body)
	trimmed = strings.TrimPrefix(trimmed, xmlHeader)
	trimmed = strings.TrimSpace(trimmed)

	var env alarmNotifyEnvelope
	if err := xml.Unmarshal([]byte(trimmed), &env); err != nil {
		return model.AlarmNotify{}, fmt.Errorf("manscdp: unmarshal alarm notify: %w", err)
	}
	sn, err := parseSN(env.SN)
	if err != nil {
		return model.AlarmNotify{}, err
	}
	params := model.AlarmNotifyParams{
		SN:            sn,
		DeviceID:      env.DeviceID,
		ChannelID:     env.ChannelID,
		AlarmPriority: env.AlarmPriority,
		AlarmMethod:   env.AlarmMethod,
		EventType:     env.EventType,
		EventTime:     env.EventTime,
		Description:   env.Description,
	}
	return model.NewAlarmNotify(params)
}

// MarshalAlarmAck renders an Alarm acknowledgement response.
func (c *MANSCDPCodecAdapter) MarshalAlarmAck(ack model.AlarmAck) (string, error) {
	if !IsValidDeviceID(ack.DeviceID) {
		return "", fmt.Errorf("manscdp: alarm ack without a device id")
	}
	body, err := xml.MarshalIndent(alarmAckEnvelope{
		CmdType:  model.CmdTypeAlarm,
		SN:       ack.SN,
		DeviceID: ack.DeviceID,
		Result:   ack.Result,
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("manscdp: marshal alarm ack: %w", err)
	}
	return xmlHeader + string(body) + "\n", nil
}
