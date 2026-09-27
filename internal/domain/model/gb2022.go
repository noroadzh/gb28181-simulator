// Package model — GB/T 28181-2022 incremental MANSCDP commands and enrichments.
package model

import (
	"fmt"
	"strings"
	"time"
)

// --- CmdType constants for 2022-only commands ---------------------------------

// CmdTypeHomePosition is the MANSCDP HomePosition command (query or set).
const CmdTypeHomePosition = "HomePosition"

// CmdTypeCruiseTrackList is the MANSCDP CruiseTrackList query command.
const CmdTypeCruiseTrackList = "CruiseTrackList"

// CmdTypeSnapShot is the MANSCDP SnapShot capture command.
const CmdTypeSnapShot = "SnapShot"

// --- HomePosition -------------------------------------------------------------

// HomePositionParams carries the fields needed to build a HomePosition.
type HomePositionParams struct {
	DeviceID  string
	Longitude string
	Latitude  string
	Altitude  string
	Azimuth   string
}

// HomePosition is a validated 2022 HomePosition value object.
type HomePosition struct {
	deviceID  string
	longitude string
	latitude  string
	altitude  string
	azimuth   string
}

// NewHomePosition builds a HomePosition, validating required fields.
func NewHomePosition(params HomePositionParams) (HomePosition, error) {
	params.DeviceID = strings.TrimSpace(params.DeviceID)
	params.Longitude = strings.TrimSpace(params.Longitude)
	params.Latitude = strings.TrimSpace(params.Latitude)
	if params.DeviceID == "" {
		return HomePosition{}, fmt.Errorf("model: HomePosition without DeviceID")
	}
	if params.Longitude == "" {
		return HomePosition{}, fmt.Errorf("model: HomePosition without Longitude")
	}
	if params.Latitude == "" {
		return HomePosition{}, fmt.Errorf("model: HomePosition without Latitude")
	}
	return HomePosition{
		deviceID:  params.DeviceID,
		longitude: params.Longitude,
		latitude:  params.Latitude,
		altitude:  strings.TrimSpace(params.Altitude),
		azimuth:   strings.TrimSpace(params.Azimuth),
	}, nil
}

// DeviceID returns the device id.
func (h HomePosition) DeviceID() string { return h.deviceID }

// Longitude returns the longitude string.
func (h HomePosition) Longitude() string { return h.longitude }

// Latitude returns the latitude string.
func (h HomePosition) Latitude() string { return h.latitude }

// Altitude returns the altitude string.
func (h HomePosition) Altitude() string { return h.altitude }

// Azimuth returns the azimuth string.
func (h HomePosition) Azimuth() string { return h.azimuth }

// HasAzimuth reports whether an azimuth value is present.
func (h HomePosition) HasAzimuth() bool { return h.azimuth != "" }

// WithAzimuth returns a copy of the HomePosition with a new azimuth.
// Pass an empty string to clear the azimuth.
func (h HomePosition) WithAzimuth(azimuth string) HomePosition {
	h.azimuth = strings.TrimSpace(azimuth)
	return h
}

// --- CruiseTrack --------------------------------------------------------------

// CruiseTrackParams carries the fields needed to build a CruiseTrack.
type CruiseTrackParams struct {
	ID            string
	Name          string
	WaypointCount int
}

// CruiseTrack is a validated 2022 CruiseTrack entry.
type CruiseTrack struct {
	id            string
	name          string
	waypointCount int
}

// NewCruiseTrack builds a CruiseTrack, validating required fields.
// Negative WaypointCount values are clamped to zero.
func NewCruiseTrack(params CruiseTrackParams) (CruiseTrack, error) {
	params.ID = strings.TrimSpace(params.ID)
	params.Name = strings.TrimSpace(params.Name)
	if params.ID == "" {
		return CruiseTrack{}, fmt.Errorf("model: CruiseTrack without ID")
	}
	if params.Name == "" {
		return CruiseTrack{}, fmt.Errorf("model: CruiseTrack without Name")
	}
	wc := params.WaypointCount
	if wc < 0 {
		wc = 0
	}
	return CruiseTrack{
		id:            params.ID,
		name:          params.Name,
		waypointCount: wc,
	}, nil
}

// ID returns the track id.
func (c CruiseTrack) ID() string { return c.id }

// Name returns the track name.
func (c CruiseTrack) Name() string { return c.name }

// WaypointCount returns the number of waypoints in the track.
func (c CruiseTrack) WaypointCount() int { return c.waypointCount }

// --- SnapShot ----------------------------------------------------------------

// SnapShotRecordParams carries the fields needed to build a SnapShot record.
type SnapShotRecordParams struct {
	DeviceID   string
	ChannelID  string
	CapturedAt time.Time
}

// SnapShotRecord is a validated 2022 SnapShot capture event record.
type SnapShotRecord struct {
	deviceID   string
	channelID  string
	capturedAt time.Time
}

// NewSnapShotRecord builds a SnapShotRecord, validating required fields.
// A zero CapturedAt is replaced with time.Now().UTC.
func NewSnapShotRecord(params SnapShotRecordParams) (SnapShotRecord, error) {
	params.DeviceID = strings.TrimSpace(params.DeviceID)
	params.ChannelID = strings.TrimSpace(params.ChannelID)
	if params.DeviceID == "" {
		return SnapShotRecord{}, fmt.Errorf("model: SnapShotRecord without DeviceID")
	}
	if params.ChannelID == "" {
		return SnapShotRecord{}, fmt.Errorf("model: SnapShotRecord without ChannelID")
	}
	ts := params.CapturedAt
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	return SnapShotRecord{
		deviceID:   params.DeviceID,
		channelID:  params.ChannelID,
		capturedAt: ts,
	}, nil
}

// DeviceID returns the device id.
func (s SnapShotRecord) DeviceID() string { return s.deviceID }

// ChannelID returns the channel id.
func (s SnapShotRecord) ChannelID() string { return s.channelID }

// CapturedAt returns the capture timestamp.
func (s SnapShotRecord) CapturedAt() time.Time { return s.capturedAt }

// --- TeleBoot -----------------------------------------------------------------

// IsTeleBoot reports whether a DeviceControl command contains a TeleBoot element.
func IsTeleBoot(cmd PTZControl) bool {
	return strings.EqualFold(strings.TrimSpace(cmd.Command), "TeleBoot")
}

// --- StorageCardState ---------------------------------------------------------

// StorageCardState is the state of a device's storage card.
type StorageCardState string

const (
	// StorageCardNormal indicates the storage card is functioning normally.
	StorageCardNormal StorageCardState = "Normal"
	// StorageCardError indicates the storage card has an error.
	StorageCardError StorageCardState = "Error"
	// StorageCardAbsent indicates no storage card is present.
	StorageCardAbsent StorageCardState = "Absent"
)

// --- PTZPrecisePosition -------------------------------------------------------

// PTZPrecisePosition enriches a position/status response with sub-degree precision
// fields for 2022 peers.
type PTZPrecisePosition struct {
	Azimuth   float64 // degrees, sub-degree precision
	Elevation float64 // degrees, sub-degree precision
	Zoom      float64 // sub-unit precision
}

// NewPTZPrecisePosition builds a PTZPrecisePosition.
func NewPTZPrecisePosition(azimuth, elevation, zoom float64) PTZPrecisePosition {
	return PTZPrecisePosition{
		Azimuth:   azimuth,
		Elevation: elevation,
		Zoom:      zoom,
	}
}

// --- SectionType --------------------------------------------------------------

// SectionType is the capture-section type attribute for Annex O catalog items.
type SectionType int

const (
	// SectionTypeNone indicates no section-type attribute is emitted.
	SectionTypeNone SectionType = 0
	// SectionTypeCrossroads is the sample value "23" (crossroads) from Annex O.
	SectionTypeCrossroads SectionType = 23
)

// String renders SectionType as the wire value.
func (s SectionType) String() string {
	if s == SectionTypeNone {
		return ""
	}
	return fmt.Sprintf("%d", s)
}

// --- 2022 wire-level types (decoded messages) --------------------------------

// HomePositionQuery is a decoded HomePosition query message.
type HomePositionQuery struct {
	DeviceID string
	SN       uint32
}

// HomePositionSet is a decoded HomePosition set command.
type HomePositionSet struct {
	SN        uint32
	DeviceID  string
	Longitude string
	Latitude  string
	Altitude  string
	Azimuth   string
}

// HomePositionResponse is the wire acknowledgement for a HomePosition command.
// For a set command only Result is meaningful; for a query it carries the
// stored guard position coordinates.
type HomePositionResponse struct {
	CmdType   string
	DeviceID  string
	SN        uint32
	Result    string
	Longitude string
	Latitude  string
	Altitude  string
	Azimuth   string
}

// CruiseTrackListQuery is a decoded CruiseTrackList query message.
type CruiseTrackListQuery struct {
	DeviceID  string
	ChannelID string
	SN        uint32
}

// CruiseTrackListResponse is the wire response for a CruiseTrackList query.
type CruiseTrackListResponse struct {
	CmdType  string
	DeviceID string
	SN       uint32
	Items    []CruiseTrack
}

// SnapShotCommand is a decoded SnapShot command message.
type SnapShotCommand struct {
	DeviceID  string
	ChannelID string
	SN        uint32
}

// SnapShotResponse is the wire acknowledgement for a SnapShot command.
type SnapShotResponse struct {
	CmdType  string
	DeviceID string
	SN       uint32
	Result   string
}
