package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// ---- minimal mp4 byte generator -------------------------------------------
//
// Builds a two-track mp4 entirely from hardcoded bytes: one video track
// (avc1, timescale 90000, two samples in two chunks) and one audio track
// (mp4a/AAC-LC, timescale 48000, one sample). The generator is the golden
// fixture: any demuxer byte drift breaks the assertions below.

func u8(v byte) []byte { return []byte{v} }
func u16(v uint16) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, v)
	return b
}
func u32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}
func u64(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

// box wraps payloads in a size+type header.
func box(typ string, payloads ...[]byte) []byte {
	var body []byte
	for _, p := range payloads {
		body = append(body, p...)
	}
	out := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(out[0:4], uint32(8+len(body)))
	copy(out[4:8], typ)
	out = append(out, body...)
	return out
}

// fullbox wraps payloads in a size+type+version+flags header.
func fullbox(typ string, ver byte, flags uint32, payloads ...[]byte) []byte {
	hdr := make([]byte, 4)
	hdr[0] = ver
	hdr[1] = byte(flags >> 16)
	hdr[2] = byte(flags >> 8)
	hdr[3] = byte(flags)
	return box(typ, append([][]byte{hdr}, payloads...)...)
}

// buildMinimalMP4 renders the fixture. Chunk offsets are patched after the
// first pass, once the mdat payload start is known.
func buildMinimalMP4(t *testing.T) []byte {
	t.Helper()

	// avcC: AVCDecoderConfigurationRecord with one 4-byte SPS and one
	// 4-byte PPS; nalLenSize derived from byte 4 low bits = 3 → 4.
	avcCPayload := bytes.Join([][]byte{
		u8(0x01),                     // configurationVersion
		u8(0x42), u8(0x00), u8(0x0A), // profile, compat, level
		u8(0xFF),                         // 6 reserved bits + nalLenSize-1 = 3
		u8(0xE1),                         // reserved + numSPS = 1
		u16(4), {0x67, 0x42, 0x00, 0x0A}, // SPS
		u8(1),                            // numPPS
		u16(4), {0x68, 0xCE, 0x38, 0x80}, // PPS
	}, nil)
	avcC := box("avcC", avcCPayload)
	avc1 := box("avc1", bytes.Join([][]byte{
		make([]byte, 6), u16(1), // SampleEntry: reserved + data_ref_idx
		u16(0), u16(0), make([]byte, 12), // pre_defined, reserved, pre_defined[3]
		u16(320), u16(240), // width, height
		u32(0x00480000), u32(0x00480000), // h/v resolution
		u32(0),                 // reserved
		u16(1),                 // frame_count
		make([]byte, 32),       // compressorname
		u16(0x18), u16(0xFFFF), // depth, pre_defined
		avcC,
	}, nil))

	// esds: ver/flags + ES_Descriptor(0x03) > DecoderConfigDescriptor(0x04)
	// > DecoderSpecificInfo(0x05) carrying the ASC [0x11, 0x90]
	// (AAC-LC, 48 kHz, stereo).
	asc := []byte{0x11, 0x90}
	dsi := box2(0x05, asc) // tag 0x05, len 2
	dcdPayload := bytes.Join([][]byte{
		u8(0x40),                     // objectTypeIndication: MPEG-4 Audio
		u8(0x15), {0x00, 0x00, 0x00}, // streamType etc + bufferSizeDB
		u32(192000), u32(128000), // maxBitrate, avgBitrate
	}, nil)
	dcdPayload = append(dcdPayload, dsi...)
	dcd := box2(0x04, dcdPayload)
	esPayload := append([]byte{0x00, 0x01, 0x00}, dcd...) // ES_ID + flags
	es := box2(0x03, esPayload)
	esds := box("esds", u32(0), es)

	mp4a := box("mp4a", bytes.Join([][]byte{
		make([]byte, 6), u16(1), // SampleEntry: reserved + data_ref_idx
		make([]byte, 8), // reserved[2]
		u16(2), u16(16), // channelcount, samplesize
		u16(0), u16(0), // pre_defined, reserved
		u32(48000 << 16), // samplerate 16.16
		esds,
	}, nil))

	videoTrack := func(chunk1, chunk2 uint64) []byte {
		stts := fullbox("stts", 0, 0, u32(1), u32(2), u32(3000))
		stsc := fullbox("stsc", 0, 0, u32(1), u32(1), u32(2), u32(1))
		stsz := fullbox("stsz", 0, 0, u32(0), u32(2), u32(10), u32(14))
		stco := fullbox("stco", 0, 0, u32(2), u32(uint32(chunk1)), u32(uint32(chunk2)))
		stsd := fullbox("stsd", 0, 0, u32(1), avc1)
		stbl := box("stbl", stts, stsc, stsz, stco, stsd)
		minf := box("minf", stbl)
		hdlr := fullbox("hdlr", 0, 0, u32(0), []byte("vide"), make([]byte, 12), u8(0))
		mdhd := fullbox("mdhd", 0, 0, u32(0), u32(0), u32(90000), u32(6000), u16(0x55C4), u16(0))
		return box("trak", box("mdia", mdhd, hdlr, minf))
	}

	audioTrack := func(chunk uint64) []byte {
		stts := fullbox("stts", 0, 0, u32(1), u32(1), u32(1024))
		stsc := fullbox("stsc", 0, 0, u32(1), u32(1), u32(1), u32(1))
		stsz := fullbox("stsz", 0, 0, u32(0), u32(1), u32(8))
		stco := fullbox("stco", 0, 0, u32(1), u32(uint32(chunk)))
		stsd := fullbox("stsd", 0, 0, u32(1), mp4a)
		stbl := box("stbl", stts, stsc, stsz, stco, stsd)
		minf := box("minf", stbl)
		hdlr := fullbox("hdlr", 0, 0, u32(0), []byte("soun"), make([]byte, 12), u8(0))
		mdhd := fullbox("mdhd", 0, 0, u32(0), u32(0), u32(48000), u32(1024), u16(0x55C4), u16(0))
		return box("trak", box("mdia", mdhd, hdlr, minf))
	}

	ftyp := box("ftyp", []byte("isom"), u32(0x200), []byte("isom"))

	videoChunk1 := bytes.Join([][]byte{u32(6), {0x65, 0x01, 0x02, 0x03, 0x04, 0x05}}, nil)
	videoChunk2 := bytes.Join([][]byte{
		u32(3), {0x41, 0x09, 0x09},
		u32(3), {0x41, 0x08, 0x08},
	}, nil)
	audioChunk := bytes.Repeat([]byte{0xAA}, 8)
	mdatPayloadLen := len(videoChunk1) + len(videoChunk2) + len(audioChunk)

	moov := box("moov", videoTrack(0, 0), audioTrack(0))
	mdatStart := len(ftyp) + len(moov)
	payloadStart := mdatStart + 8

	moov = box("moov",
		videoTrack(uint64(payloadStart), uint64(payloadStart+len(videoChunk1))),
		audioTrack(uint64(payloadStart+len(videoChunk1)+len(videoChunk2))),
	)
	mdat := box("mdat", videoChunk1, videoChunk2, audioChunk)
	if len(mdat) != 8+mdatPayloadLen {
		t.Fatalf("mdat size drifted: %d", len(mdat))
	}
	return bytes.Join([][]byte{ftyp, moov, mdat}, nil)
}

// box2 is a descriptor "box": one tag byte, one expandable length byte,
// payload. Descriptor lengths here never exceed 127, so a single length
// byte suffices for the fixture.
func box2(tag byte, payload []byte) []byte {
	return append([]byte{tag, byte(len(payload))}, payload...)
}

// writeTempMP4 materialises the fixture as a real file — the demuxer reads
// via ReadAt on an *os.File.
func writeTempMP4(t *testing.T) *os.File {
	t.Helper()
	data := buildMinimalMP4(t)
	path := filepath.Join(t.TempDir(), "minimal.mp4")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// ---- golden tests ----------------------------------------------------------

// expected frames of the fixture, in merged PTS order.
var (
	goldenVideo1 = bytes.Join([][]byte{
		{0x00, 0x00, 0x00, 0x01}, {0x67, 0x42, 0x00, 0x0A}, // SPS
		{0x00, 0x00, 0x00, 0x01}, {0x68, 0xCE, 0x38, 0x80}, // PPS
		{0x00, 0x00, 0x00, 0x01}, {0x65, 0x01, 0x02, 0x03, 0x04, 0x05},
	}, nil)
	goldenAudio = bytes.Join([][]byte{
		{0xFF, 0xF1, 0x4C, 0x80, 0x01, 0xFF, 0xFC}, // ADTS
		bytes.Repeat([]byte{0xAA}, 8),
	}, nil)
	goldenVideo2 = bytes.Join([][]byte{
		{0x00, 0x00, 0x00, 0x01}, {0x41, 0x09, 0x09},
		{0x00, 0x00, 0x00, 0x01}, {0x41, 0x08, 0x08},
	}, nil)
)

func TestMP4Demux_Golden(t *testing.T) {
	d, err := NewMP4Demuxer(writeTempMP4(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()

	want := []struct {
		payload []byte
		pts     uint64
		kind    string
	}{
		{goldenVideo1, 0, "video"},    // video sample 1: PTS 0 × 90000/90000
		{goldenAudio, 0, "audio"},     // audio sample: PTS 0, tie keeps writer order
		{goldenVideo2, 3000, "video"}, // video sample 2: delta 3000 @ 90 kHz
	}
	for i, w := range want {
		frame, err := d.ReadFrame(ctx)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if !bytes.Equal(frame.Payload, w.payload) {
			t.Fatalf("frame %d payload:\n got %x\nwant %x", i, frame.Payload, w.payload)
		}
		if frame.PTS != w.pts {
			t.Fatalf("frame %d pts = %d, want %d", i, frame.PTS, w.pts)
		}
		if string(frame.Kind) != w.kind {
			t.Fatalf("frame %d kind = %s, want %s", i, frame.Kind, w.kind)
		}
	}

	// Loop: past the last sample the demuxer restarts from sample 1 with
	// its container PTS — never io.EOF.
	frame, err := d.ReadFrame(ctx)
	if err != nil {
		t.Fatalf("loop frame: %v", err)
	}
	if !bytes.Equal(frame.Payload, goldenVideo1) || frame.PTS != 0 {
		t.Fatalf("loop frame mismatch: pts=%d payload=%x", frame.PTS, frame.Payload)
	}

	if err := d.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := d.ReadFrame(ctx); !errors.Is(err, io.EOF) {
		t.Fatalf("read after close: %v, want io.EOF", err)
	}
}

func TestMP4Demux_Golden_ReadCloserPath(t *testing.T) {
	// The io.ReadCloser surface must deliver the same bytes, frame after
	// frame, without ever signalling EOF.
	d, err := NewMP4Demuxer(writeTempMP4(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	rc := &mp4ReadCloser{d: d}
	defer rc.Close()

	cycle := bytes.Join([][]byte{goldenVideo1, goldenAudio, goldenVideo2}, nil)
	buf := make([]byte, len(cycle))
	if _, err := io.ReadFull(rc, buf); err != nil {
		t.Fatalf("read full cycle: %v", err)
	}
	if !bytes.Equal(buf, cycle) {
		t.Fatalf("ReadCloser cycle mismatch:\n got %x\nwant %x", buf, cycle)
	}
}

func TestMP4Demux_RejectsGarbage(t *testing.T) {
	// Broken input must surface as an error, never a panic.
	tmp := filepath.Join(t.TempDir(), "garbage.mp4")
	if err := os.WriteFile(tmp, bytes.Repeat([]byte{0xDE, 0xAD}, 64), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	f, err := os.Open(tmp)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	if _, err := NewMP4Demuxer(f); err == nil {
		t.Fatal("garbage accepted, want error")
	}
}

func TestMP4Demux_AVCCParameterSets(t *testing.T) {
	avcC := bytes.Join([][]byte{
		u8(0x01), u8(0x64), u8(0x00), u8(0x1F), u8(0xFF), u8(0xE1),
		u16(2), {0x67, 0x99},
		u8(2),
		u16(1), {0x68},
		u16(1), {0x69},
	}, nil)
	got, err := avcParameterSets(avcC)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := bytes.Join([][]byte{
		{0, 0, 0, 1}, {0x67, 0x99},
		{0, 0, 0, 1}, {0x68},
		{0, 0, 0, 1}, {0x69},
	}, nil)
	if !bytes.Equal(got, want) {
		t.Fatalf("parameter sets:\n got %x\nwant %x", got, want)
	}
}

func TestMP4Demux_ADTSHeader(t *testing.T) {
	// ASC [0x11, 0x90]: AAC-LC, 48 kHz, stereo; frame length 15.
	got := adtsHeader([]byte{0x11, 0x90}, 15)
	want := []byte{0xFF, 0xF1, 0x4C, 0x80, 0x01, 0xFF, 0xFC}
	if !bytes.Equal(got, want) {
		t.Fatalf("adts:\n got %x\nwant %x", got, want)
	}
}
