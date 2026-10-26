// Package model — the 8-byte PTZ command of GB/T 28181.
package model

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// PTZ instruction bits in byte 4. The eight direction bits are unambiguous
// across implementations; the zoom and focus bits follow the most widely used
// mapping. Iris and preset recall need bits this layout does not have, so
// EncodePTZCmd refuses them rather than guessing.
const (
	ptzRight     byte = 0x01
	ptzLeft      byte = 0x02
	ptzDown      byte = 0x04
	ptzUp        byte = 0x08
	ptzZoomIn    byte = 0x10
	ptzZoomOut   byte = 0x20
	ptzFocusNear byte = 0x40
	ptzFocusFar  byte = 0x80
)

// ptzDirectionBits maps the API's direction vocabulary onto the instruction
// byte. Compound directions are the OR of their two axes, exactly as the
// standard encodes a diagonal.
var ptzDirectionBits = map[string]byte{
	"up":        ptzUp,
	"down":      ptzDown,
	"left":      ptzLeft,
	"right":     ptzRight,
	"upleft":    ptzUp | ptzLeft,
	"upright":   ptzUp | ptzRight,
	"downleft":  ptzDown | ptzLeft,
	"downright": ptzDown | ptzRight,
	"zoomin":    ptzZoomIn,
	"zoomout":   ptzZoomOut,
	"focusnear": ptzFocusNear,
	"focusfar":  ptzFocusFar,
	"stop":      0x00,
}

// EncodePTZCmd renders one PTZ instruction as the base64 string a MANSCDP
// DeviceControl <Cmd> carries.
//
// Layout: b0 0xA5; b1 0x0F; b2 0x01; b3 instruction; b4 pan speed; b5 tilt
// speed; b6 zoom speed in the high nibble; b7 checksum (byte sum of b0..b6).
// Speeds are clamped to 0..255; the horizontal speed is used for left/right,
// the vertical speed for up/down, and both for a diagonal.
func EncodePTZCmd(direction string, speed int) (string, error) {
	bits, ok := ptzDirectionBits[strings.ToLower(strings.TrimSpace(direction))]
	if !ok {
		return "", fmt.Errorf("model: unsupported ptz direction %q (iris and preset recall need fields this encoding does not define)", direction)
	}
	if speed < 0 {
		speed = 0
	}
	if speed > 255 {
		speed = 255
	}

	cmd := make([]byte, 8)
	cmd[0] = 0xA5
	cmd[1] = 0x0F
	cmd[2] = 0x01
	cmd[3] = bits
	if bits&(ptzLeft|ptzRight) != 0 {
		cmd[4] = byte(speed)
	}
	if bits&(ptzUp|ptzDown) != 0 {
		cmd[5] = byte(speed)
	}
	if bits&(ptzZoomIn|ptzZoomOut) != 0 {
		cmd[6] = byte(speed&0x0F) << 4
	}
	for _, b := range cmd[:7] {
		cmd[7] += b
	}
	return base64.StdEncoding.EncodeToString(cmd), nil
}
