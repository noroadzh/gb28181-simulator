// Package model — Subscribe / MediaStatus / PlaybackControl.
package model

import (
	"fmt"
	"strings"
)

// SubscribeInfo carries a parsed MANSCDP subscription request.
type SubscribeInfo struct {
	deviceID string
	sn       uint32
	channelID string
}

// NewSubscribeInfo builds a parsed subscription.
func NewSubscribeInfo(deviceID string, sn uint32, channelID string) (SubscribeInfo, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return SubscribeInfo{}, fmt.Errorf("model: subscribe without device id")
	}
	return SubscribeInfo{
		deviceID:  deviceID,
		sn:        sn,
		channelID: strings.TrimSpace(channelID),
	}, nil
}

// DeviceID returns the subscriber id.
func (s SubscribeInfo) DeviceID() string { return s.deviceID }

// SN returns the sequence number.
func (s SubscribeInfo) SN() uint32 { return s.sn }

// ChannelID returns the channel id, or "" when the request was for the
// device itself.
func (s SubscribeInfo) ChannelID() string { return s.channelID }

// HasChannel reports whether the subscription targets a specific channel.
func (s SubscribeInfo) HasChannel() bool { return s.channelID != "" }

// String renders a log-safe summary.
func (s SubscribeInfo) String() string {
	return fmt.Sprintf("Subscribe<device=%s sn=%d channel=%s>",
		s.deviceID, s.sn, s.channelID)
}

// MediaStatusReport carries a parsed MANSCDP MediaStatus notify.
type MediaStatusReport struct {
	deviceID string
	sn       uint32
	status   string
}

// NewMediaStatusReport builds a parsed media status report.
func NewMediaStatusReport(deviceID, status string, sn uint32) (MediaStatusReport, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return MediaStatusReport{}, fmt.Errorf("model: media status without device id")
	}
	return MediaStatusReport{
		deviceID: deviceID,
		status:   strings.TrimSpace(status),
		sn:       sn,
	}, nil
}

// DeviceID returns the reporting device id.
func (m MediaStatusReport) DeviceID() string { return m.deviceID }

// SN returns the sequence number.
func (m MediaStatusReport) SN() uint32 { return m.sn }

// Status returns the reported media status (OK / ERROR / …).
func (m MediaStatusReport) Status() string { return m.status }

// String renders a log-safe summary.
func (m MediaStatusReport) String() string {
	return fmt.Sprintf("MediaStatus<device=%s sn=%d status=%s>",
		m.deviceID, m.sn, m.status)
}

// PlaybackControl carries a parsed MANSCDP PlaybackControl command.
type PlaybackControl struct {
	deviceID  string
	sn        uint32
	channelID string
	command   string // Play / Stop / Pause / …
	startTime string
	endTime   string
}

// NewPlaybackControl builds a parsed playback control.
func NewPlaybackControl(deviceID, command, channelID, startTime, endTime string, sn uint32) (PlaybackControl, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return PlaybackControl{}, fmt.Errorf("model: playback control without device id")
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return PlaybackControl{}, fmt.Errorf("model: playback control without command")
	}
	return PlaybackControl{
		deviceID:  deviceID,
		command:   command,
		sn:        sn,
		channelID: strings.TrimSpace(channelID),
		startTime: strings.TrimSpace(startTime),
		endTime:   strings.TrimSpace(endTime),
	}, nil
}

// DeviceID returns the target device id.
func (p PlaybackControl) DeviceID() string { return p.deviceID }

// SN returns the sequence number.
func (p PlaybackControl) SN() uint32 { return p.sn }

// ChannelID returns the playback channel id.
func (p PlaybackControl) ChannelID() string { return p.channelID }

// Command returns the playback command (Play / Stop / …).
func (p PlaybackControl) Command() string { return p.command }

// StartTime returns the requested start time (ISO 8601).
func (p PlaybackControl) StartTime() string { return p.startTime }

// EndTime returns the requested end time (ISO 8601).
func (p PlaybackControl) EndTime() string { return p.endTime }

// String renders a log-safe summary.
func (p PlaybackControl) String() string {
	return fmt.Sprintf("PlaybackControl<device=%s cmd=%s channel=%s start=%s end=%s sn=%d>",
		p.deviceID, p.command, p.channelID, p.startTime, p.endTime, p.sn)
}
