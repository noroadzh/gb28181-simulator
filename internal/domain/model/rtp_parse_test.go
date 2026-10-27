package model

import (
	"bytes"
	"testing"
)

func TestParseRTPPacket_RoundTrip(t *testing.T) {
	in := RTPPacket{
		Sequence: 7, SSRC: 0x01000012, PayloadType: 96, Marker: true,
		Timestamp: 90000, Payload: []byte{0x00, 0x00, 0x01, 0xBA},
	}
	got, err := ParseRTPPacket(in.Bytes())
	if err != nil {
		t.Fatalf("ParseRTPPacket: %v", err)
	}
	if got.Sequence != in.Sequence || got.SSRC != in.SSRC || got.PayloadType != in.PayloadType ||
		got.Marker != in.Marker || got.Timestamp != in.Timestamp {
		t.Errorf("header = %+v, want %+v", got, in)
	}
	if !bytes.Equal(got.Payload, in.Payload) {
		t.Errorf("payload = % X, want % X", got.Payload, in.Payload)
	}
}

func TestParseRTPPacket_SkipsCSRCList(t *testing.T) {
	b := []byte{0x82, 0x60, 0x00, 0x01, 0, 0, 0, 0, 0, 0, 0, 1,
		0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22, // two CSRCs
		0x00, 0x00, 0x01, 0xBA}
	got, err := ParseRTPPacket(b)
	if err != nil {
		t.Fatalf("ParseRTPPacket: %v", err)
	}
	if !bytes.Equal(got.Payload, []byte{0x00, 0x00, 0x01, 0xBA}) {
		t.Errorf("payload = % X", got.Payload)
	}
}

func TestParseRTPPacket_RejectsUnsupportedShapes(t *testing.T) {
	cases := map[string][]byte{
		"short":             {0x80, 0x60},
		"version 1":         {0x40, 0x60, 0, 1, 0, 0, 0, 0, 0, 0, 0, 1, 1},
		"padding":           {0xA0, 0x60, 0, 1, 0, 0, 0, 0, 0, 0, 0, 1, 1},
		"extension":         {0x90, 0x60, 0, 1, 0, 0, 0, 0, 0, 0, 0, 1, 1},
		"header only":       {0x80, 0x60, 0, 1, 0, 0, 0, 0, 0, 0, 0, 1},
		"csrc beyond bytes": {0x81, 0x60, 0, 1, 0, 0, 0, 0, 0, 0, 0, 1},
	}
	for name, b := range cases {
		if _, err := ParseRTPPacket(b); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
