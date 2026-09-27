// Package model — Alarm MANSCDP command.
package model

import (
	"fmt"
	"strings"
)

// AlarmNotify is the wire shape of an Alarm notify.
type AlarmNotify struct {
	cmdType       string
	sn            uint32
	deviceID      string
	channelID     string
	alarmPriority int
	alarmMethod   int
	eventType     string
	eventTime     string
	description   string
	extInfo       string
}

// AlarmNotifyParams is the flat input for NewAlarmNotify.
type AlarmNotifyParams struct {
	SN            uint32
	DeviceID      string
	ChannelID     string
	AlarmPriority int
	AlarmMethod   int
	EventType     string
	EventTime     string
	Description   string
	ExtInfo       string
}

// NewAlarmNotify builds an Alarm notify.
func NewAlarmNotify(p AlarmNotifyParams) (AlarmNotify, error) {
	deviceID := strings.TrimSpace(p.DeviceID)
	if deviceID == "" {
		return AlarmNotify{}, fmt.Errorf("model: alarm notify without a device id")
	}
	return AlarmNotify{
		cmdType:       CmdTypeAlarm,
		sn:            p.SN,
		deviceID:      deviceID,
		channelID:     strings.TrimSpace(p.ChannelID),
		alarmPriority: p.AlarmPriority,
		alarmMethod:   p.AlarmMethod,
		eventType:     strings.TrimSpace(p.EventType),
		eventTime:     strings.TrimSpace(p.EventTime),
		description:   strings.TrimSpace(p.Description),
		extInfo:       strings.TrimSpace(p.ExtInfo),
	}, nil
}

// CmdType returns the MANSCDP command type.
func (a AlarmNotify) CmdType() string { return a.cmdType }

// SN returns the notify sequence number.
func (a AlarmNotify) SN() uint32 { return a.sn }

// DeviceID returns the device id.
func (a AlarmNotify) DeviceID() string { return a.deviceID }

// ChannelID returns the channel id.
func (a AlarmNotify) ChannelID() string { return a.channelID }

// AlarmPriority returns the alarm priority.
func (a AlarmNotify) AlarmPriority() int { return a.alarmPriority }

// AlarmMethod returns the alarm method.
func (a AlarmNotify) AlarmMethod() int { return a.alarmMethod }

// EventType returns the alarm event type.
func (a AlarmNotify) EventType() string { return a.eventType }

// EventTime returns the alarm event time.
func (a AlarmNotify) EventTime() string { return a.eventTime }

// Description returns the alarm description.
func (a AlarmNotify) Description() string { return a.description }

// ExtInfo returns the extended alarm info.
func (a AlarmNotify) ExtInfo() string { return a.extInfo }

// String renders a log-safe one-line summary.
func (a AlarmNotify) String() string {
	return fmt.Sprintf("AlarmNotify<device_id=%s event=%s time=%s>", a.deviceID, a.eventType, a.eventTime)
}

// AlarmAck is the wire shape of an Alarm acknowledgement response.
type AlarmAck struct {
	CmdType  string
	SN       uint32
	DeviceID string
	Result   string
}

// NewAlarmAck builds an Alarm acknowledgement response.
func NewAlarmAck(deviceID string, sn uint32, result string) AlarmAck {
	return AlarmAck{
		CmdType:  CmdTypeAlarm,
		SN:       sn,
		DeviceID: strings.TrimSpace(deviceID),
		Result:   strings.TrimSpace(result),
	}
}
