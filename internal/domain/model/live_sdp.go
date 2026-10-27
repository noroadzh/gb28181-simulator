// Package model — the SDP of a platform-initiated live view.
package model

import (
	"fmt"
	"strconv"
	"strings"
)

// LiveOffer is the platform side of a live-view INVITE: which channel is
// being pulled, where the platform listens, and over which transport.
type LiveOffer struct {
	ChannelID string
	ConnIP    string
	RTPPort   int
	// Proto is "RTP/AVP" (UDP, the default) or "TCP/RTP/AVP" (附录 D).
	Proto string
	// Setup is the a=setup value for TCP. The platform listens, so it
	// offers "passive" and the device answers "active".
	Setup string
}

// LiveOfferBytes renders the INVITE body.
//
// Shape (platform pulling a live stream):
//
//	v=0
//	o=<channelID> 0 0 IN IP4 <ip>
//	s=Play
//	c=IN IP4 <ip>
//	t=0 0
//	m=video <port> RTP/AVP 96
//	a=recvonly
//	a=rtpmap:96 PS/90000
func LiveOfferBytes(o LiveOffer) (string, error) {
	if strings.TrimSpace(o.ChannelID) == "" {
		return "", fmt.Errorf("model: live offer without a channel id")
	}
	if strings.TrimSpace(o.ConnIP) == "" {
		return "", fmt.Errorf("model: live offer without a connection address")
	}
	if o.RTPPort <= 0 || o.RTPPort > 65535 {
		return "", fmt.Errorf("model: live offer port %d out of range", o.RTPPort)
	}
	proto := o.Proto
	if proto == "" {
		proto = "RTP/AVP"
	}
	var b strings.Builder
	line := func(format string, args ...any) {
		fmt.Fprintf(&b, format+"\r\n", args...)
	}
	line("v=0")
	line("o=%s 0 0 IN IP4 %s", o.ChannelID, o.ConnIP)
	line("s=Play")
	line("c=IN IP4 %s", o.ConnIP)
	line("t=0 0")
	line("m=video %d %s 96", o.RTPPort, proto)
	line("a=recvonly")
	if o.Setup != "" {
		line("a=setup:%s", o.Setup)
	}
	line("a=rtpmap:96 PS/90000")
	return b.String(), nil
}

// LiveAnswer is what a device says about a live stream it is about to send.
type LiveAnswer struct {
	ConnIP string
	Port   int
	Proto  string
	Setup  string
	// SSRC comes from the y= line. Zero means the device declared none, so
	// the receiver must not filter by SSRC.
	SSRC uint32
}

// ParseLiveAnswer reads the fields the platform acts on. Everything else is
// ignored on purpose: a real device's answer carries attributes this
// simulator has no use for, and rejecting them would make pulling brittle.
// A missing c= is allowed — the caller falls back to the signalling peer's
// address.
func ParseLiveAnswer(text string) (LiveAnswer, error) {
	var out LiveAnswer
	haveM := false
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		switch {
		case strings.HasPrefix(line, "c=IN IP4 "):
			out.ConnIP = strings.TrimSpace(strings.TrimPrefix(line, "c=IN IP4 "))
		case strings.HasPrefix(line, "a=setup:"):
			out.Setup = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "a=setup:")))
		case strings.HasPrefix(line, "y="):
			if n, err := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "y=")), 10, 32); err == nil {
				out.SSRC = uint32(n)
			}
		case strings.HasPrefix(line, "m=video "):
			fields := strings.Fields(strings.TrimPrefix(line, "m=video "))
			if len(fields) == 0 {
				return LiveAnswer{}, fmt.Errorf("model: live answer m=video without a port")
			}
			port, err := strconv.Atoi(fields[0])
			// Port 0 is legal on a TCP answer: the device is the active
			// side and sends on the connection it opens, so it names no
			// receiving port. A UDP caller must still reject it.
			if err != nil || port < 0 || port > 65535 {
				return LiveAnswer{}, fmt.Errorf("model: live answer port %q is not a media port", fields[0])
			}
			out.Port = port
			if len(fields) > 1 {
				out.Proto = fields[1]
			}
			haveM = true
		}
	}
	if !haveM {
		return LiveAnswer{}, fmt.Errorf("model: live answer has no m=video line")
	}
	return out, nil
}
