package manscdp

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// mobilePositionNotifyEnvelope is the wire shape of a MobilePosition notify.
// Coordinates ride as strings so the rendering is exactly the shortest
// fixed-point spelling — no exponent notation a strict parser would choke
// on, no lost digits a float formatter could introduce.
type mobilePositionNotifyEnvelope struct {
	XMLName   xml.Name `xml:"Notify"`
	CmdType   string   `xml:"CmdType"`
	SN        string   `xml:"SN"`
	DeviceID  string   `xml:"DeviceID"`
	Time      string   `xml:"Time,omitempty"`
	Longitude string   `xml:"Longitude,omitempty"`
	Latitude  string   `xml:"Latitude,omitempty"`
	Speed     string   `xml:"Speed,omitempty"`
}

// fixed renders a coordinate the way GB/T 28181 platforms spell them:
// plain decimal degrees, as many decimals as the value needs.
func fixed(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// MarshalMobilePositionNotify renders a MobilePosition notify body,
// declaration included and terminated by a newline.
func (c *MANSCDPCodecAdapter) MarshalMobilePositionNotify(mp model.MobilePositionNotify) (string, error) {
	if mp.DeviceID() == "" {
		return "", fmt.Errorf("manscdp: mobile position notify without a device id")
	}
	body, err := xml.MarshalIndent(mobilePositionNotifyEnvelope{
		CmdType:   model.CmdTypeMobilePosition,
		SN:        strconv.FormatUint(uint64(mp.SN()), 10),
		DeviceID:  mp.DeviceID(),
		Time:      mp.Time(),
		Longitude: fixed(mp.Longitude()),
		Latitude:  fixed(mp.Latitude()),
		Speed:     fixed(mp.Speed()),
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("manscdp: marshal mobile position notify: %w", err)
	}
	return xmlHeader + string(body) + "\n", nil
}

// DecodeMobilePositionNotify parses a MobilePosition notify body.
func (c *MANSCDPCodecAdapter) DecodeMobilePositionNotify(body string) (model.MobilePositionNotify, error) {
	trimmed := strings.TrimSpace(body)
	trimmed = strings.TrimPrefix(trimmed, xmlHeader)
	trimmed = strings.TrimSpace(trimmed)

	var env mobilePositionNotifyEnvelope
	if err := xml.Unmarshal([]byte(trimmed), &env); err != nil {
		return model.MobilePositionNotify{}, fmt.Errorf("manscdp: unmarshal mobile position notify: %w", err)
	}
	sn, err := parseSN(env.SN)
	if err != nil {
		return model.MobilePositionNotify{}, err
	}
	longitude, err := strconv.ParseFloat(strings.TrimSpace(env.Longitude), 64)
	if err != nil {
		return model.MobilePositionNotify{}, fmt.Errorf("manscdp: mobile position longitude: %w", err)
	}
	latitude, err := strconv.ParseFloat(strings.TrimSpace(env.Latitude), 64)
	if err != nil {
		return model.MobilePositionNotify{}, fmt.Errorf("manscdp: mobile position latitude: %w", err)
	}
	speed, err := strconv.ParseFloat(strings.TrimSpace(env.Speed), 64)
	if err != nil {
		speed = 0
	}
	return model.NewMobilePositionNotify(model.MobilePositionNotifyParams{
		SN:        sn,
		DeviceID:  env.DeviceID,
		Longitude: longitude,
		Latitude:  latitude,
		Speed:     speed,
		Time:      env.Time,
	})
}
