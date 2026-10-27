// Package model — the SDP of a GB/T 28181 historical playback INVITE.
package model

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// playbackTimeFormat is the timestamp layout the t= line uses in a playback
// SDP: local wall-clock time, seconds resolution.
const playbackTimeFormat = "2006-01-02T15:04:05"

// PlaybackSDP describes one historical playback offer/answer: which device,
// which recording (sn), which time window, and where the media should go.
type PlaybackSDP struct {
	DeviceID string
	SN       string
	ConnIP   string
	RTPPort  int
	Start    time.Time
	End      time.Time
	// Mode is the a= direction attribute: "recvonly" for the platform's
	// offer, "sendonly" for the device's answer. Empty means recvonly.
	Mode string
	// Proto is the m= transport: "RTP/AVP" (UDP, the default) or
	// "TCP/RTP/AVP" (GB/T 28181 附录 D). Empty means RTP/AVP.
	Proto string
	// Setup is the a=setup value when the media travels over TCP:
	// "active", "passive" or "actpass". Empty means the attribute is
	// absent, which the receiver reads as "passive" per 附录 D.
	Setup string
}

// PlaybackSDPBytes renders the body that goes into the INVITE (or its 200 OK).
//
// Shape (GB/T 28181 historical playback):
//
//	v=0
//	o=<deviceID> 0 0 IN IP4 <ip>
//	s=Playback
//	u=<deviceID>:<sn>
//	c=IN IP4 <ip>
//	t=<start> <end>
//	m=video <port> RTP/AVP 96
//	a=recvonly
//	a=rtpmap:96 PS/90000
//
// A platform offers with a=recvonly (it receives); the device answers with the
// same shape and its own address.
func PlaybackSDPBytes(p PlaybackSDP) (string, error) {
	if strings.TrimSpace(p.DeviceID) == "" {
		return "", fmt.Errorf("model: playback sdp without a device id")
	}
	if strings.TrimSpace(p.SN) == "" {
		return "", fmt.Errorf("model: playback sdp without an sn")
	}
	if strings.TrimSpace(p.ConnIP) == "" {
		return "", fmt.Errorf("model: playback sdp without a connection address")
	}
	if p.RTPPort <= 0 || p.RTPPort > 65535 {
		return "", fmt.Errorf("model: playback sdp port %d out of range", p.RTPPort)
	}
	if p.Start.IsZero() || p.End.IsZero() || !p.Start.Before(p.End) {
		return "", fmt.Errorf("model: playback window %s..%s is not a valid range",
			p.Start.Format(playbackTimeFormat), p.End.Format(playbackTimeFormat))
	}
	var b strings.Builder
	line := func(format string, args ...any) {
		fmt.Fprintf(&b, format+"\r\n", args...)
	}
	line("v=0")
	line("o=%s 0 0 IN IP4 %s", p.DeviceID, p.ConnIP)
	line("s=Playback")
	line("u=%s:%s", p.DeviceID, p.SN)
	line("c=IN IP4 %s", p.ConnIP)
	line("t=%s %s", p.Start.Format(playbackTimeFormat), p.End.Format(playbackTimeFormat))
	proto := p.Proto
	if proto == "" {
		proto = "RTP/AVP"
	}
	line("m=video %d %s 96", p.RTPPort, proto)
	mode := p.Mode
	if mode == "" {
		mode = "recvonly"
	}
	line("a=%s", mode)
	if p.Setup != "" {
		line("a=setup:%s", p.Setup)
	}
	line("a=rtpmap:96 PS/90000")
	return b.String(), nil
}

// ParsePlaybackSDP reads the fields this simulator acts on. It is deliberately
// strict about what it uses (u=, t=, c=, m=video) and ignores the rest:
// rejecting an answer for an extra attribute would make the simulator brittle
// against real platforms.
func ParsePlaybackSDP(text string) (PlaybackSDP, error) {
	var out PlaybackSDP
	var haveU, haveT, haveC, haveM bool
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		switch {
		case strings.HasPrefix(line, "u="):
			value := strings.TrimPrefix(line, "u=")
			id, sn, ok := strings.Cut(value, ":")
			if !ok || strings.TrimSpace(id) == "" || strings.TrimSpace(sn) == "" {
				return PlaybackSDP{}, fmt.Errorf("model: playback sdp u= %q is not <device>:<sn>", value)
			}
			out.DeviceID, out.SN = strings.TrimSpace(id), strings.TrimSpace(sn)
			haveU = true
		case strings.HasPrefix(line, "c=IN IP4 "):
			out.ConnIP = strings.TrimSpace(strings.TrimPrefix(line, "c=IN IP4 "))
			haveC = true
		case strings.HasPrefix(line, "t="):
			fields := strings.Fields(strings.TrimPrefix(line, "t="))
			if len(fields) != 2 {
				return PlaybackSDP{}, fmt.Errorf("model: playback sdp t= needs a start and an end, got %q", line)
			}
			start, err := time.ParseInLocation(playbackTimeFormat, fields[0], time.Local)
			if err != nil {
				return PlaybackSDP{}, fmt.Errorf("model: playback start %q: %w", fields[0], err)
			}
			end, err := time.ParseInLocation(playbackTimeFormat, fields[1], time.Local)
			if err != nil {
				return PlaybackSDP{}, fmt.Errorf("model: playback end %q: %w", fields[1], err)
			}
			if !start.Before(end) {
				return PlaybackSDP{}, fmt.Errorf("model: playback window %s..%s is not a valid range", fields[0], fields[1])
			}
			out.Start, out.End = start, end
			haveT = true
		case strings.HasPrefix(line, "a=setup:"):
			out.Setup = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "a=setup:")))
		case strings.HasPrefix(line, "m=video "):
			fields := strings.Fields(strings.TrimPrefix(line, "m=video "))
			if len(fields) == 0 {
				return PlaybackSDP{}, fmt.Errorf("model: playback sdp m=video without a port")
			}
			port, err := strconv.Atoi(fields[0])
			if err != nil || port <= 0 || port > 65535 {
				return PlaybackSDP{}, fmt.Errorf("model: playback sdp port %q is not a media port", fields[0])
			}
			out.RTPPort = port
			// m= fields after the port: transport, then formats.
			if len(fields) > 1 {
				out.Proto = fields[1]
			}
			haveM = true
		}
	}
	switch {
	case !haveU:
		return PlaybackSDP{}, fmt.Errorf("model: playback sdp has no u= line")
	case !haveC:
		return PlaybackSDP{}, fmt.Errorf("model: playback sdp has no c= line")
	case !haveT:
		return PlaybackSDP{}, fmt.Errorf("model: playback sdp has no t= line")
	case !haveM:
		return PlaybackSDP{}, fmt.Errorf("model: playback sdp has no m=video line")
	}
	return out, nil
}
