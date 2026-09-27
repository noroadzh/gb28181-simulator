// Package model — MediaStatus.
package model

import (
	"fmt"
	"strings"
)

// VideoParam holds H.264/H.265 video stream parameters.
type VideoParam struct {
	Width     uint32
	Height    uint32
	Bitrate   uint32 // kbps
	FrameRate uint32
	Codec     string // "H.264" or "H.265"
}

// AudioParam holds G.711 audio stream parameters.
type AudioParam struct {
	SampleRate uint32 // Hz, e.g. 8000
	Bitrate    uint32 // kbps
	Codec      string // "PCMU", "PCMA", "G.722"
}

// RecordStatus is the recording state of a channel.
type RecordStatus int

const (
	RecordStatusIdle RecordStatus = iota
	RecordStatusRecording
	RecordStatusPlayback
)

// String renders a RecordStatus for logs.
func (s RecordStatus) String() string {
	switch s {
	case RecordStatusIdle:
		return "idle"
	case RecordStatusRecording:
		return "recording"
	case RecordStatusPlayback:
		return "playback"
	default:
		return fmt.Sprintf("RecordStatus(%d)", int(s))
	}
}

// MediaStatus holds the media state a device reported.
type MediaStatus struct {
	DeviceID     string
	ChannelID    string
	Video        *VideoParam
	Audio        *AudioParam
	RecordStatus RecordStatus
	Position     *Position
}

// String renders a log-safe summary.
func (m MediaStatus) String() string {
	rec := m.RecordStatus.String()
	vid := "<no video>"
	if m.Video != nil {
		vid = fmt.Sprintf("%dx%d@%dfps %dkbps %s",
			m.Video.Width, m.Video.Height, m.Video.FrameRate, m.Video.Bitrate, m.Video.Codec)
	}
	return fmt.Sprintf("MediaStatus<device=%s channel=%s video=%s record=%s>",
		m.DeviceID, m.ChannelID, vid, rec)
}

// ParseMediaStatus parses a raw MANSCDP MediaStatus XML payload into a
// MediaStatus value. Returns an error if the payload does not contain the
// required DeviceID element.
func ParseMediaStatus(xmlPayload string) (MediaStatus, error) {
	ms := MediaStatus{}
	// Simple field extraction without an XML parser dependency.
	// The codec adapter owns the full XML parsing; this is for domain use.
	if strings.Contains(xmlPayload, "<DeviceID>") {
		start := strings.Index(xmlPayload, "<DeviceID>") + len("<DeviceID>")
		end := strings.Index(xmlPayload, "</DeviceID>")
		if end > start {
			ms.DeviceID = strings.TrimSpace(xmlPayload[start:end])
		}
	}
	if ms.DeviceID == "" {
		return MediaStatus{}, fmt.Errorf("model: MediaStatus without DeviceID")
	}
	return ms, nil
}
