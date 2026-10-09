package model

import "time"

// PTZCommand models a single PTZ control request. The combination of
// Direction, Action, Speed (0..255) and a PresetID (>= 1) is enough for the
// MANSCDP <Control> PTZCmd body. When PresetID > 0 the Direction/Action
// are ignored by the device and the named preset is recalled.
type PTZCommand struct {
	Direction string // "left" / "right" / "up" / "down" / "zoom-in" / "zoom-out" / ...
	Action    string // "start" / "stop"
	Speed     int    // 0..255 (MANSCDP speed byte)
	PresetID  int    // 0 = none, 1..255 = recall preset
}

// RecordInfo describes one playback-eligible record on a device. The
// device's RecordInfo response uses these fields to populate the Web UI's
// record list.
type RecordInfo struct {
	DeviceID  string    `json:"deviceID"`
	ChannelID string    `json:"channelID"`
	Name      string    `json:"name"`
	StartTime time.Time `json:"startTime"`
	EndTime   time.Time `json:"endTime"`
	Secrecy   int       `json:"secrecy"`
	Type      string    `json:"type"`     // "time" | "alarm" | "manual" | "all"
	FilePath  string    `json:"filePath"` // optional: local file path on device
}

// PlaybackRequest is the input to StartPlayback: the time range and the
// initial playback rate. Speed follows the MANSCDP convention: 1.0 normal,
// 0 paused, negative for reverse, ±Inf and NaN refused.
type PlaybackRequest struct {
	StartTime time.Time
	EndTime   time.Time
	Speed     float64
}

// PlaybackHandle is the opaque session identifier returned by StartPlayback
// and required by ControlPlayback. SessionID is the SSRC/dialog id assigned
// by the acceptor; TransportAddr is the device-side RTP endpoint the
// platform should target (e.g. "192.0.2.10:5004"). An empty SessionID
// indicates the call has not been started.
type PlaybackHandle struct {
	SessionID     string
	TransportAddr string
	StartTime     time.Time
	EndTime       time.Time
	Speed         float64
}

// PlaybackCommand models a single playback control action (play/pause/
// seek/scale). Seek is a relative offset in seconds; Scale=0 means pause
// without changing the position.
type PlaybackCommand struct {
	Action string  // "play" | "pause" | "seek" | "stop"
	Seek   float64 // seconds offset; ignored unless Action == "seek"
	Scale  float64 // playback rate; 0 means pause
}

// TalkSession models an active voice intercom session. SessionID is
// assigned by TalkAcceptor and is the key for all subsequent control
// operations (audio push, stop). LocalRTPPort is the platform-side UDP
// port the Web UI streams audio into; 0 indicates an audio-receive-only
// session. RemoteRTPPort is the device's RTP port the platform sends to.
type TalkSession struct {
	SessionID     string
	NodeID        NodeID
	ChannelID     string
	AudioCodec    string // "PCMU" | "PCMA"
	LocalRTPPort  int
	RemoteRTPPort int
	StartedAt     time.Time
}
