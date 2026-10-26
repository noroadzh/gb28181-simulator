package model

import (
	"strings"
	"testing"
	"time"
)

func playbackWindow() (time.Time, time.Time) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)
	return start, start.Add(5 * time.Minute)
}

// Byte-level golden: the body a real platform or device would put on the wire.
func TestPlaybackSDPBytes_Golden(t *testing.T) {
	start, end := playbackWindow()
	body, err := PlaybackSDPBytes(PlaybackSDP{
		DeviceID: "34020000001310000001",
		SN:       "12345",
		ConnIP:   "127.0.0.1",
		RTPPort:  50000,
		Start:    start,
		End:      end,
	})
	if err != nil {
		t.Fatalf("PlaybackSDPBytes: %v", err)
	}

	want := "v=0\r\n" +
		"o=34020000001310000001 0 0 IN IP4 127.0.0.1\r\n" +
		"s=Playback\r\n" +
		"u=34020000001310000001:12345\r\n" +
		"c=IN IP4 127.0.0.1\r\n" +
		"t=2026-10-01T00:00:00 2026-10-01T00:05:00\r\n" +
		"m=video 50000 RTP/AVP 96\r\n" +
		"a=recvonly\r\n" +
		"a=rtpmap:96 PS/90000\r\n"
	if body != want {
		t.Fatalf("playback SDP mismatch:\n got: %q\nwant: %q", body, want)
	}
}

// Construct and parse are inverses: what the platform offers is what the
// device reads back.
func TestPlaybackSDP_RoundTrip(t *testing.T) {
	start, end := playbackWindow()
	in := PlaybackSDP{
		DeviceID: "34020000001310000001",
		SN:       "12345",
		ConnIP:   "10.0.0.7",
		RTPPort:  40002,
		Start:    start,
		End:      end,
	}
	body, err := PlaybackSDPBytes(in)
	if err != nil {
		t.Fatalf("PlaybackSDPBytes: %v", err)
	}
	out, err := ParsePlaybackSDP(body)
	if err != nil {
		t.Fatalf("ParsePlaybackSDP: %v", err)
	}
	if out.DeviceID != in.DeviceID || out.SN != in.SN || out.ConnIP != in.ConnIP || out.RTPPort != in.RTPPort {
		t.Errorf("parsed %+v, want %+v", out, in)
	}
	if !out.Start.Equal(in.Start) || !out.End.Equal(in.End) {
		t.Errorf("window %s..%s, want %s..%s",
			out.Start, out.End, in.Start, in.End)
	}
}

func TestPlaybackSDP_RejectsBrokenInput(t *testing.T) {
	start, end := playbackWindow()
	if _, err := PlaybackSDPBytes(PlaybackSDP{DeviceID: "", SN: "1", ConnIP: "127.0.0.1", RTPPort: 1, Start: start, End: end}); err == nil {
		t.Error("accepted a body without a device id")
	}
	if _, err := PlaybackSDPBytes(PlaybackSDP{DeviceID: "34020000001310000001", SN: "1", ConnIP: "127.0.0.1", RTPPort: 1, Start: end, End: start}); err == nil {
		t.Error("accepted a reversed time window")
	}

	base := "v=0\r\nc=IN IP4 127.0.0.1\r\nt=2026-10-01T00:00:00 2026-10-01T00:05:00\r\nm=video 50000 RTP/AVP 96\r\n"
	broken := map[string]string{
		"no u=":      base,
		"bad u=":     strings.Replace(base, "v=0\r\n", "v=0\r\nu=no-colon\r\n", 1),
		"no t=":      strings.Replace(base, "t=2026-10-01T00:00:00 2026-10-01T00:05:00\r\n", "", 1),
		"bad window": strings.Replace(base, "t=2026-10-01T00:00:00 2026-10-01T00:05:00", "t=2026-10-01T00:05:00 2026-10-01T00:00:00", 1),
		"no m=video": strings.Replace(base, "m=video 50000 RTP/AVP 96\r\n", "", 1),
	}
	for name, body := range broken {
		if _, err := ParsePlaybackSDP(body); err == nil {
			t.Errorf("%s: parsed without error", name)
		}
	}
}

// The device answers with a=sendonly; the offer keeps the default recvonly.
func TestPlaybackSDPBytes_Mode(t *testing.T) {
	start, end := playbackWindow()
	body, err := PlaybackSDPBytes(PlaybackSDP{
		DeviceID: "34020000001310000001", SN: "1", ConnIP: "127.0.0.1", RTPPort: 50001,
		Start: start, End: end, Mode: "sendonly",
	})
	if err != nil {
		t.Fatalf("PlaybackSDPBytes: %v", err)
	}
	if !strings.Contains(body, "a=sendonly") {
		t.Errorf("answer is not sendonly:\n%q", body)
	}
	if strings.Contains(body, "a=recvonly") {
		t.Errorf("answer still carries recvonly:\n%q", body)
	}
}
