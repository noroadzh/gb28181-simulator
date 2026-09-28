// Package model — Position and alarm snapshot.
package model

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Position is a device's geographic position carried in MediaStatus.
type Position struct {
	longitude float64
	latitude  float64
	speed     float64
}

// NewPosition builds a Position with basic range validation (WGS-84).
func NewPosition(longitude, latitude, speed float64) (Position, error) {
	if longitude < -180 || longitude > 180 {
		return Position{}, fmt.Errorf("model: longitude %v out of [-180,180]", longitude)
	}
	if latitude < -90 || latitude > 90 {
		return Position{}, fmt.Errorf("model: latitude %v out of [-90,90]", latitude)
	}
	if speed < 0 {
		return Position{}, fmt.Errorf("model: speed %v must be >= 0", speed)
	}
	return Position{longitude: longitude, latitude: latitude, speed: speed}, nil
}

// Longitude returns WGS-84 longitude in decimal degrees.
func (p Position) Longitude() float64 { return p.longitude }

// Latitude returns WGS-84 latitude in decimal degrees.
func (p Position) Latitude() float64 { return p.latitude }

// Speed returns the movement speed in m/s.
func (p Position) Speed() float64 { return p.speed }

// positionWire is the JSON/YAML wire shape of Position.
type positionWire struct {
	Longitude float64 `json:"longitude" yaml:"longitude"`
	Latitude  float64 `json:"latitude" yaml:"latitude"`
	Speed     float64 `json:"speed" yaml:"speed"`
}

// MarshalJSON renders the position for HTTP payloads.
func (p Position) MarshalJSON() ([]byte, error) {
	return json.Marshal(positionWire{Longitude: p.longitude, Latitude: p.latitude, Speed: p.speed})
}

// UnmarshalJSON restores a position from its HTTP payload shape.
func (p *Position) UnmarshalJSON(data []byte) error {
	var w positionWire
	if err := json.Unmarshal(data, &w); err != nil {
		return fmt.Errorf("model: bad position payload: %w", err)
	}
	out, err := NewPosition(w.Longitude, w.Latitude, w.Speed)
	if err != nil {
		return fmt.Errorf("model: bad position payload: %w", err)
	}
	*p = out
	return nil
}

// MarshalYAML renders the position for config files.
func (p Position) MarshalYAML() (interface{}, error) {
	return positionWire{Longitude: p.longitude, Latitude: p.latitude, Speed: p.speed}, nil
}

// UnmarshalYAML restores a position from its config file shape.
func (p *Position) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var w positionWire
	if err := unmarshal(&w); err != nil {
		return fmt.Errorf("model: bad position config: %w", err)
	}
	out, err := NewPosition(w.Longitude, w.Latitude, w.Speed)
	if err != nil {
		return fmt.Errorf("model: bad position config: %w", err)
	}
	*p = out
	return nil
}

// String renders a log-safe one-line summary.
func (p Position) String() string {
	return fmt.Sprintf("Position<lon=%v lat=%v speed=%v>", p.longitude, p.latitude, p.speed)
}

// AlarmSnapshot is one in-memory alarm event a device emitted.
type AlarmSnapshot struct {
	id          string
	deviceID    string
	channelID   string
	priority    int
	method      int
	description string
	eventTime   string
}

// NewAlarmSnapshot builds an AlarmSnapshot. id and deviceID are required.
func NewAlarmSnapshot(id, deviceID, channelID string, priority, method int, description, eventTime string) (AlarmSnapshot, error) {
	id = strings.TrimSpace(id)
	deviceID = strings.TrimSpace(deviceID)
	if id == "" {
		return AlarmSnapshot{}, fmt.Errorf("model: alarm snapshot without id")
	}
	if deviceID == "" {
		return AlarmSnapshot{}, fmt.Errorf("model: alarm snapshot without device id")
	}
	return AlarmSnapshot{
		id:          id,
		deviceID:    deviceID,
		channelID:   strings.TrimSpace(channelID),
		priority:    priority,
		method:      method,
		description: strings.TrimSpace(description),
		eventTime:   strings.TrimSpace(eventTime),
	}, nil
}

// ID returns the unique alarm event id.
func (a AlarmSnapshot) ID() string { return a.id }

// DeviceID returns the emitting device id.
func (a AlarmSnapshot) DeviceID() string { return a.deviceID }

// ChannelID returns the channel id, or "" for a device-level alarm.
func (a AlarmSnapshot) ChannelID() string { return a.channelID }

// Priority returns the alarm priority (1=lowest, 4=highest).
func (a AlarmSnapshot) Priority() int { return a.priority }

// Method returns the alarm method code.
func (a AlarmSnapshot) Method() int { return a.method }

// Description returns the human-readable alarm description.
func (a AlarmSnapshot) Description() string { return a.description }

// EventTime returns the alarm event time (YYYYMMDDTHHMMSS).
func (a AlarmSnapshot) EventTime() string { return a.eventTime }

// String renders a log-safe one-line summary.
func (a AlarmSnapshot) String() string {
	return fmt.Sprintf("AlarmSnapshot<id=%s device=%s priority=%d>", a.id, a.deviceID, a.priority)
}

// MobilePositionNotify is the wire shape of a MobilePosition notify: the
// last-known geographic position of a mobile device, pushed to subscribers.
type MobilePositionNotify struct {
	cmdType   string
	sn        uint32
	deviceID  string
	longitude float64
	latitude  float64
	speed     float64
	time      string
}

// MobilePositionNotifyParams is the flat input for NewMobilePositionNotify.
type MobilePositionNotifyParams struct {
	SN        uint32
	DeviceID  string
	Longitude float64
	Latitude  float64
	Speed     float64
	Time      string
}

// NewMobilePositionNotify builds a MobilePosition notify. The device id is
// required; the coordinates are validated against the same WGS-84 ranges a
// Position uses, so a body that could never be plotted is refused here
// rather than after it crossed the wire.
func NewMobilePositionNotify(p MobilePositionNotifyParams) (MobilePositionNotify, error) {
	deviceID := strings.TrimSpace(p.DeviceID)
	if deviceID == "" {
		return MobilePositionNotify{}, fmt.Errorf("model: mobile position notify without a device id")
	}
	if p.Longitude < -180 || p.Longitude > 180 {
		return MobilePositionNotify{}, fmt.Errorf("model: longitude %v out of [-180,180]", p.Longitude)
	}
	if p.Latitude < -90 || p.Latitude > 90 {
		return MobilePositionNotify{}, fmt.Errorf("model: latitude %v out of [-90,90]", p.Latitude)
	}
	if p.Speed < 0 {
		return MobilePositionNotify{}, fmt.Errorf("model: speed %v must be >= 0", p.Speed)
	}
	return MobilePositionNotify{
		cmdType:   CmdTypeMobilePosition,
		sn:        p.SN,
		deviceID:  deviceID,
		longitude: p.Longitude,
		latitude:  p.Latitude,
		speed:     p.Speed,
		time:      strings.TrimSpace(p.Time),
	}, nil
}

// CmdType returns the MANSCDP command type.
func (m MobilePositionNotify) CmdType() string { return m.cmdType }

// SN returns the notify sequence number.
func (m MobilePositionNotify) SN() uint32 { return m.sn }

// DeviceID returns the device id the position belongs to.
func (m MobilePositionNotify) DeviceID() string { return m.deviceID }

// Longitude returns the WGS-84 longitude in decimal degrees.
func (m MobilePositionNotify) Longitude() float64 { return m.longitude }

// Latitude returns the WGS-84 latitude in decimal degrees.
func (m MobilePositionNotify) Latitude() float64 { return m.latitude }

// Speed returns the movement speed in m/s.
func (m MobilePositionNotify) Speed() float64 { return m.speed }

// Time returns the position sample time (YYYY-MM-DDTHH:MM:SS).
func (m MobilePositionNotify) Time() string { return m.time }

// String renders a log-safe one-line summary.
func (m MobilePositionNotify) String() string {
	return fmt.Sprintf("MobilePositionNotify<device_id=%s lon=%v lat=%v speed=%v>",
		m.deviceID, m.longitude, m.latitude, m.speed)
}
