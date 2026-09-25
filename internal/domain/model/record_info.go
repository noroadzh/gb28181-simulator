// Package model — RecordInfo MANSCDP command.
package model

import (
	"fmt"
	"strings"
)

// RecordInfoQuery is the wire shape of a RecordInfo query request.
type RecordInfoQuery struct {
	CmdType   string
	SN        uint32
	DeviceID  string
	StartTime string
	EndTime   string
	Secrecy   int
}

// RecordInfoResponse is the wire shape of a RecordInfo query response.
type RecordInfoResponse struct {
	CmdType  string
	SN       uint32
	DeviceID string
	SumNum   int
	Items    []RecordInfoItem
}

// RecordInfoItem is one entry in a RecordInfo response list.
type RecordInfoItem struct {
	name         string
	deviceID     string
	channelID    string
	startTime    string
	endTime      string
	secrecy      int
	filePath     string
	videoCodec   string
	audioCodec   string
	videoBitrate int
	audioBitrate int
}

// RecordInfoItemParams is the flat input for NewRecordInfoItem.
type RecordInfoItemParams struct {
	Name         string
	DeviceID     string
	ChannelID    string
	StartTime    string
	EndTime      string
	Secrecy      int
	FilePath     string
	VideoCodec   string
	AudioCodec   string
	VideoBitrate int
	AudioBitrate int
}

// NewRecordInfoItem builds one RecordInfo entry.
func NewRecordInfoItem(p RecordInfoItemParams) RecordInfoItem {
	return RecordInfoItem{
		name:         strings.TrimSpace(p.Name),
		deviceID:     strings.TrimSpace(p.DeviceID),
		channelID:    strings.TrimSpace(p.ChannelID),
		startTime:    strings.TrimSpace(p.StartTime),
		endTime:      strings.TrimSpace(p.EndTime),
		secrecy:      p.Secrecy,
		filePath:     strings.TrimSpace(p.FilePath),
		videoCodec:   strings.TrimSpace(p.VideoCodec),
		audioCodec:   strings.TrimSpace(p.AudioCodec),
		videoBitrate: p.VideoBitrate,
		audioBitrate: p.AudioBitrate,
	}
}

// Name returns the record name.
func (r RecordInfoItem) Name() string { return r.name }

// DeviceID returns the device id.
func (r RecordInfoItem) DeviceID() string { return r.deviceID }

// ChannelID returns the channel id.
func (r RecordInfoItem) ChannelID() string { return r.channelID }

// StartTime returns the start time.
func (r RecordInfoItem) StartTime() string { return r.startTime }

// EndTime returns the end time.
func (r RecordInfoItem) EndTime() string { return r.endTime }

// Secrecy returns the secrecy class.
func (r RecordInfoItem) Secrecy() int { return r.secrecy }

// FilePath returns the record file path.
func (r RecordInfoItem) FilePath() string { return r.filePath }

// VideoCodec returns the video codec.
func (r RecordInfoItem) VideoCodec() string { return r.videoCodec }

// AudioCodec returns the audio codec.
func (r RecordInfoItem) AudioCodec() string { return r.audioCodec }

// VideoBitrate returns the video bitrate.
func (r RecordInfoItem) VideoBitrate() int { return r.videoBitrate }

// AudioBitrate returns the audio bitrate.
func (r RecordInfoItem) AudioBitrate() int { return r.audioBitrate }

// String renders a log-safe one-line summary.
func (r RecordInfoItem) String() string {
	return fmt.Sprintf("RecordInfoItem<name=%s start=%s end=%s>", r.name, r.startTime, r.endTime)
}

// NewRecordInfoQuery builds a RecordInfo query.
func NewRecordInfoQuery(deviceID string, sn uint32, startTime, endTime string) RecordInfoQuery {
	return RecordInfoQuery{
		CmdType:   CmdTypeRecordInfo,
		SN:        sn,
		DeviceID:  strings.TrimSpace(deviceID),
		StartTime: strings.TrimSpace(startTime),
		EndTime:   strings.TrimSpace(endTime),
	}
}

// NewRecordInfoResponse builds a RecordInfo response.
func NewRecordInfoResponse(deviceID string, sn uint32, items []RecordInfoItem) (RecordInfoResponse, error) {
	id := strings.TrimSpace(deviceID)
	if id == "" {
		return RecordInfoResponse{}, fmt.Errorf("model: record info response without a device id")
	}
	cp := make([]RecordInfoItem, len(items))
	copy(cp, items)
	return RecordInfoResponse{
		CmdType:  CmdTypeRecordInfo,
		SN:       sn,
		DeviceID: id,
		SumNum:   len(items),
		Items:    cp,
	}, nil
}
