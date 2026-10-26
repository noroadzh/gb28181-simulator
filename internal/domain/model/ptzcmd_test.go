package model

import (
	"encoding/base64"
	"testing"
)

func decodePTZ(t *testing.T, encoded string) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 decode %q: %v", encoded, err)
	}
	if len(raw) != 8 {
		t.Fatalf("command is %d bytes, want 8", len(raw))
	}
	return raw
}

// Byte-level golden for the common cases: the header bytes, the instruction
// byte, which speed fields are populated, and the checksum.
func TestEncodePTZCmd_GoldenBytes(t *testing.T) {
	cases := []struct {
		name      string
		direction string
		speed     int
		want      []byte
	}{
		{"up", "up", 128, []byte{0xA5, 0x0F, 0x01, 0x08, 0x00, 0x80, 0x00, 0x3D}},
		{"down", "down", 64, []byte{0xA5, 0x0F, 0x01, 0x04, 0x00, 0x40, 0x00, 0xF9}},
		{"left", "left", 255, []byte{0xA5, 0x0F, 0x01, 0x02, 0xFF, 0x00, 0x00, 0xB6}},
		{"upright", "upright", 10, []byte{0xA5, 0x0F, 0x01, 0x09, 0x0A, 0x0A, 0x00, 0xD2}},
		{"zoomin", "zoomin", 3, []byte{0xA5, 0x0F, 0x01, 0x10, 0x00, 0x00, 0x30, 0xF5}},
		{"stop", "stop", 200, []byte{0xA5, 0x0F, 0x01, 0x00, 0x00, 0x00, 0x00, 0xB5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := EncodePTZCmd(tc.direction, tc.speed)
			if err != nil {
				t.Fatalf("EncodePTZCmd(%q): %v", tc.direction, err)
			}
			got := decodePTZ(t, encoded)
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("byte %d = 0x%02X, want 0x%02X (full: % X)", i, got[i], tc.want[i], got)
				}
			}
		})
	}
}

func TestEncodePTZCmd_RefusesUnmappedDirections(t *testing.T) {
	for _, dir := range []string{"irisin", "irisout", "", "preset_call"} {
		if _, err := EncodePTZCmd(dir, 64); err == nil {
			t.Errorf("EncodePTZCmd(%q) accepted a direction the encoding cannot express", dir)
		}
	}
}

func TestEncodePTZCmd_ClampsSpeed(t *testing.T) {
	encoded, err := EncodePTZCmd("up", 9999)
	if err != nil {
		t.Fatalf("EncodePTZCmd: %v", err)
	}
	if got := decodePTZ(t, encoded)[5]; got != 0xFF {
		t.Errorf("tilt speed = 0x%02X, want 0xFF after clamping", got)
	}
}
