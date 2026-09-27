package manscdp

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// --- envelopes --------------------------------------------------------------

type homePositionQueryEnvelope struct {
	XMLName  xml.Name `xml:"Query"`
	CmdType  string   `xml:"CmdType"`
	SN       string   `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
}

type homePositionSetEnvelope struct {
	XMLName   xml.Name `xml:"Control"`
	CmdType   string   `xml:"CmdType"`
	SN        string   `xml:"SN"`
	DeviceID  string   `xml:"DeviceID"`
	Longitude string   `xml:"Longitude"`
	Latitude  string   `xml:"Latitude"`
	Altitude  string   `xml:"Altitude,omitempty"`
	Azimuth   string   `xml:"Azimuth,omitempty"`
}

type homePositionResponseEnvelope struct {
	XMLName   xml.Name `xml:"Response"`
	CmdType   string   `xml:"CmdType"`
	SN        uint32   `xml:"SN"`
	DeviceID  string   `xml:"DeviceID"`
	Result    string   `xml:"Result"`
	Longitude string   `xml:"Longitude"`
	Latitude  string   `xml:"Latitude"`
	Altitude  string   `xml:"Altitude"`
	Azimuth   string   `xml:"Azimuth"`
}

type cruiseTrackListQueryEnvelope struct {
	XMLName   xml.Name `xml:"Query"`
	CmdType   string   `xml:"CmdType"`
	SN        string   `xml:"SN"`
	DeviceID  string   `xml:"DeviceID"`
	ChannelID string   `xml:"ChannelID,omitempty"`
}

type cruiseTrackListResponseEnvelope struct {
	XMLName  xml.Name                  `xml:"Response"`
	CmdType  string                    `xml:"CmdType"`
	SN       uint32                    `xml:"SN"`
	DeviceID string                    `xml:"DeviceID"`
	Items    []cruiseTrackItemEnvelope `xml:"ItemList>Item"`
}

type cruiseTrackItemEnvelope struct {
	ID            string `xml:"ID"`
	Name          string `xml:"Name"`
	WaypointCount int    `xml:"WaypointCount"`
}

type snapShotCommandEnvelope struct {
	XMLName   xml.Name `xml:"Control"`
	CmdType   string   `xml:"CmdType"`
	SN        string   `xml:"SN"`
	DeviceID  string   `xml:"DeviceID"`
	ChannelID string   `xml:"ChannelID,omitempty"`
}

type snapShotResponseEnvelope struct {
	XMLName  xml.Name `xml:"Response"`
	CmdType  string   `xml:"CmdType"`
	SN       uint32   `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	Result   string   `xml:"Result"`
}

// --- decoders --------------------------------------------------------------

// DecodeHomePositionQuery parses a MANSCDP HomePosition query body.
func (c *MANSCDPCodecAdapter) DecodeHomePositionQuery(body string) (model.HomePositionQuery, error) {
	trimmed := strings.TrimSpace(body)
	trimmed = strings.TrimPrefix(trimmed, xmlHeader)
	trimmed = strings.TrimSpace(trimmed)

	var env homePositionQueryEnvelope
	if err := xml.Unmarshal([]byte(trimmed), &env); err != nil {
		return model.HomePositionQuery{}, fmt.Errorf("manscdp: unmarshal home position query: %w", err)
	}
	sn, err := parseSN(env.SN)
	if err != nil {
		return model.HomePositionQuery{}, err
	}
	return model.HomePositionQuery{
		DeviceID: env.DeviceID,
		SN:       sn,
	}, nil
}

// DecodeHomePositionSet parses a MANSCDP HomePosition set command body.
func (c *MANSCDPCodecAdapter) DecodeHomePositionSet(body string) (model.HomePositionSet, error) {
	trimmed := strings.TrimSpace(body)
	trimmed = strings.TrimPrefix(trimmed, xmlHeader)
	trimmed = strings.TrimSpace(trimmed)

	var env homePositionSetEnvelope
	if err := xml.Unmarshal([]byte(trimmed), &env); err != nil {
		return model.HomePositionSet{}, fmt.Errorf("manscdp: unmarshal home position set: %w", err)
	}
	sn, err := parseSN(env.SN)
	if err != nil {
		return model.HomePositionSet{}, err
	}
	return model.HomePositionSet{
		SN:        sn,
		DeviceID:  env.DeviceID,
		Longitude: env.Longitude,
		Latitude:  env.Latitude,
		Altitude:  env.Altitude,
		Azimuth:   env.Azimuth,
	}, nil
}

// DecodeHomePositionResponse parses a MANSCDP HomePosition response body.
func (c *MANSCDPCodecAdapter) DecodeHomePositionResponse(body string) (model.HomePositionResponse, error) {
	trimmed := strings.TrimSpace(body)
	trimmed = strings.TrimPrefix(trimmed, xmlHeader)
	trimmed = strings.TrimSpace(trimmed)

	var env homePositionResponseEnvelope
	if err := xml.Unmarshal([]byte(trimmed), &env); err != nil {
		return model.HomePositionResponse{}, fmt.Errorf("manscdp: unmarshal home position response: %w", err)
	}
	return model.HomePositionResponse{
		CmdType:   env.CmdType,
		DeviceID:  env.DeviceID,
		SN:        env.SN,
		Result:    env.Result,
		Longitude: env.Longitude,
		Latitude:  env.Latitude,
		Altitude:  env.Altitude,
		Azimuth:   env.Azimuth,
	}, nil
}

// DecodeCruiseTrackListQuery parses a MANSCDP CruiseTrackList query body.
func (c *MANSCDPCodecAdapter) DecodeCruiseTrackListQuery(body string) (model.CruiseTrackListQuery, error) {
	trimmed := strings.TrimSpace(body)
	trimmed = strings.TrimPrefix(trimmed, xmlHeader)
	trimmed = strings.TrimSpace(trimmed)

	var env cruiseTrackListQueryEnvelope
	if err := xml.Unmarshal([]byte(trimmed), &env); err != nil {
		return model.CruiseTrackListQuery{}, fmt.Errorf("manscdp: unmarshal cruise track list query: %w", err)
	}
	sn, err := parseSN(env.SN)
	if err != nil {
		return model.CruiseTrackListQuery{}, err
	}
	return model.CruiseTrackListQuery{
		DeviceID:  env.DeviceID,
		ChannelID: env.ChannelID,
		SN:        sn,
	}, nil
}

// DecodeCruiseTrackListResponse parses a MANSCDP CruiseTrackList response body.
func (c *MANSCDPCodecAdapter) DecodeCruiseTrackListResponse(body string) (model.CruiseTrackListResponse, error) {
	trimmed := strings.TrimSpace(body)
	trimmed = strings.TrimPrefix(trimmed, xmlHeader)
	trimmed = strings.TrimSpace(trimmed)

	var env cruiseTrackListResponseEnvelope
	if err := xml.Unmarshal([]byte(trimmed), &env); err != nil {
		return model.CruiseTrackListResponse{}, fmt.Errorf("manscdp: unmarshal cruise track list response: %w", err)
	}
	items := make([]model.CruiseTrack, 0, len(env.Items))
	for _, it := range env.Items {
		track, err := model.NewCruiseTrack(model.CruiseTrackParams{
			ID:            it.ID,
			Name:          it.Name,
			WaypointCount: it.WaypointCount,
		})
		if err != nil {
			return model.CruiseTrackListResponse{}, fmt.Errorf("manscdp: invalid cruise track item: %w", err)
		}
		items = append(items, track)
	}
	return model.CruiseTrackListResponse{
		CmdType:  env.CmdType,
		DeviceID: env.DeviceID,
		SN:       env.SN,
		Items:    items,
	}, nil
}

// DecodeSnapShotCommand parses a MANSCDP SnapShot command body.
func (c *MANSCDPCodecAdapter) DecodeSnapShotCommand(body string) (model.SnapShotCommand, error) {
	trimmed := strings.TrimSpace(body)
	trimmed = strings.TrimPrefix(trimmed, xmlHeader)
	trimmed = strings.TrimSpace(trimmed)

	var env snapShotCommandEnvelope
	if err := xml.Unmarshal([]byte(trimmed), &env); err != nil {
		return model.SnapShotCommand{}, fmt.Errorf("manscdp: unmarshal snapshot command: %w", err)
	}
	sn, err := parseSN(env.SN)
	if err != nil {
		return model.SnapShotCommand{}, err
	}
	return model.SnapShotCommand{
		DeviceID:  env.DeviceID,
		ChannelID: env.ChannelID,
		SN:        sn,
	}, nil
}

// DecodeSnapShotResponse parses a MANSCDP SnapShot response body.
func (c *MANSCDPCodecAdapter) DecodeSnapShotResponse(body string) (model.SnapShotResponse, error) {
	trimmed := strings.TrimSpace(body)
	trimmed = strings.TrimPrefix(trimmed, xmlHeader)
	trimmed = strings.TrimSpace(trimmed)

	var env snapShotResponseEnvelope
	if err := xml.Unmarshal([]byte(trimmed), &env); err != nil {
		return model.SnapShotResponse{}, fmt.Errorf("manscdp: unmarshal snapshot response: %w", err)
	}
	return model.SnapShotResponse{
		CmdType:  env.CmdType,
		DeviceID: env.DeviceID,
		SN:       env.SN,
		Result:   env.Result,
	}, nil
}

// --- encoders --------------------------------------------------------------

// MarshalHomePositionQuery renders a MANSCDP HomePosition query body.
func (c *MANSCDPCodecAdapter) MarshalHomePositionQuery(query model.HomePositionQuery, sn uint32) (string, error) {
	if !IsValidDeviceID(query.DeviceID) {
		return "", fmt.Errorf("manscdp: home position query without a device id")
	}
	body, err := xml.MarshalIndent(homePositionQueryEnvelope{
		CmdType:  model.CmdTypeHomePosition,
		SN:       fmt.Sprintf("%d", sn),
		DeviceID: query.DeviceID,
	}, "", "\t")
	if err != nil {
		return "", fmt.Errorf("manscdp: marshal home position query: %w", err)
	}
	return xmlHeader + string(body) + "\n", nil
}

// MarshalHomePositionSet renders a MANSCDP HomePosition set command body.
func (c *MANSCDPCodecAdapter) MarshalHomePositionSet(position model.HomePosition, sn uint32) (string, error) {
	if !IsValidDeviceID(position.DeviceID()) {
		return "", fmt.Errorf("manscdp: home position set without a device id")
	}
	env := homePositionSetEnvelope{
		CmdType:   model.CmdTypeHomePosition,
		SN:        fmt.Sprintf("%d", sn),
		DeviceID:  position.DeviceID(),
		Longitude: position.Longitude(),
		Latitude:  position.Latitude(),
	}
	if position.Altitude() != "" {
		env.Altitude = position.Altitude()
	}
	if position.HasAzimuth() {
		env.Azimuth = position.Azimuth()
	}
	body, err := xml.MarshalIndent(env, "", "\t")
	if err != nil {
		return "", fmt.Errorf("manscdp: marshal home position set: %w", err)
	}
	return xmlHeader + string(body) + "\n", nil
}

// MarshalHomePositionResponse renders a MANSCDP HomePosition response body.
func (c *MANSCDPCodecAdapter) MarshalHomePositionResponse(resp model.HomePositionResponse, sn uint32) (string, error) {
	if !IsValidDeviceID(resp.DeviceID) {
		return "", fmt.Errorf("manscdp: home position response without a device id")
	}
	body, err := xml.MarshalIndent(homePositionResponseEnvelope{
		CmdType:   model.CmdTypeHomePosition,
		SN:        sn,
		DeviceID:  resp.DeviceID,
		Result:    resp.Result,
		Longitude: resp.Longitude,
		Latitude:  resp.Latitude,
		Altitude:  resp.Altitude,
		Azimuth:   resp.Azimuth,
	}, "", "\t")
	if err != nil {
		return "", fmt.Errorf("manscdp: marshal home position response: %w", err)
	}
	return xmlHeader + string(body) + "\n", nil
}

// MarshalCruiseTrackListQuery renders a MANSCDP CruiseTrackList query body.
func (c *MANSCDPCodecAdapter) MarshalCruiseTrackListQuery(query model.CruiseTrackListQuery, sn uint32) (string, error) {
	if !IsValidDeviceID(query.DeviceID) {
		return "", fmt.Errorf("manscdp: cruise track list query without a device id")
	}
	body, err := xml.MarshalIndent(cruiseTrackListQueryEnvelope{
		CmdType:   model.CmdTypeCruiseTrackList,
		SN:        fmt.Sprintf("%d", sn),
		DeviceID:  query.DeviceID,
		ChannelID: query.ChannelID,
	}, "", "\t")
	if err != nil {
		return "", fmt.Errorf("manscdp: marshal cruise track list query: %w", err)
	}
	return xmlHeader + string(body) + "\n", nil
}

// MarshalCruiseTrackListResponse renders a MANSCDP CruiseTrackList response body.
func (c *MANSCDPCodecAdapter) MarshalCruiseTrackListResponse(resp model.CruiseTrackListResponse, sn uint32) (string, error) {
	if !IsValidDeviceID(resp.DeviceID) {
		return "", fmt.Errorf("manscdp: cruise track list response without a device id")
	}
	items := make([]cruiseTrackItemEnvelope, 0, len(resp.Items))
	for _, it := range resp.Items {
		items = append(items, cruiseTrackItemEnvelope{
			ID:            it.ID(),
			Name:          it.Name(),
			WaypointCount: it.WaypointCount(),
		})
	}
	body, err := xml.MarshalIndent(cruiseTrackListResponseEnvelope{
		CmdType:  model.CmdTypeCruiseTrackList,
		SN:       sn,
		DeviceID: resp.DeviceID,
		Items:    items,
	}, "", "\t")
	if err != nil {
		return "", fmt.Errorf("manscdp: marshal cruise track list response: %w", err)
	}
	return xmlHeader + string(body) + "\n", nil
}

// MarshalSnapShotCommand renders a MANSCDP SnapShot command body.
func (c *MANSCDPCodecAdapter) MarshalSnapShotCommand(cmd model.SnapShotCommand, sn uint32) (string, error) {
	if !IsValidDeviceID(cmd.DeviceID) {
		return "", fmt.Errorf("manscdp: snapshot command without a device id")
	}
	body, err := xml.MarshalIndent(snapShotCommandEnvelope{
		CmdType:   model.CmdTypeSnapShot,
		SN:        fmt.Sprintf("%d", sn),
		DeviceID:  cmd.DeviceID,
		ChannelID: cmd.ChannelID,
	}, "", "\t")
	if err != nil {
		return "", fmt.Errorf("manscdp: marshal snapshot command: %w", err)
	}
	return xmlHeader + string(body) + "\n", nil
}

// MarshalSnapShotResponse renders a MANSCDP SnapShot response body.
func (c *MANSCDPCodecAdapter) MarshalSnapShotResponse(resp model.SnapShotResponse, sn uint32) (string, error) {
	if !IsValidDeviceID(resp.DeviceID) {
		return "", fmt.Errorf("manscdp: snapshot response without a device id")
	}
	body, err := xml.MarshalIndent(snapShotResponseEnvelope{
		CmdType:  model.CmdTypeSnapShot,
		SN:       sn,
		DeviceID: resp.DeviceID,
		Result:   resp.Result,
	}, "", "\t")
	if err != nil {
		return "", fmt.Errorf("manscdp: marshal snapshot response: %w", err)
	}
	return xmlHeader + string(body) + "\n", nil
}
