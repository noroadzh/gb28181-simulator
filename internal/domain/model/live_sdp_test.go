package model

import "testing"

// Task 1.1: the offer bytes are the contract a real device parses, so they
// are asserted literally.
func TestLiveOfferBytes_Golden(t *testing.T) {
	udp, err := LiveOfferBytes(LiveOffer{ChannelID: "34020000001310000001001", ConnIP: "10.0.0.5", RTPPort: 40000})
	if err != nil {
		t.Fatalf("LiveOfferBytes: %v", err)
	}
	const wantUDP = "v=0\r\n" +
		"o=34020000001310000001001 0 0 IN IP4 10.0.0.5\r\n" +
		"s=Play\r\n" +
		"c=IN IP4 10.0.0.5\r\n" +
		"t=0 0\r\n" +
		"m=video 40000 RTP/AVP 96\r\n" +
		"a=recvonly\r\n" +
		"a=rtpmap:96 PS/90000\r\n"
	if udp != wantUDP {
		t.Errorf("UDP offer:\n got %q\nwant %q", udp, wantUDP)
	}

	tcp, err := LiveOfferBytes(LiveOffer{
		ChannelID: "34020000001310000001001", ConnIP: "10.0.0.5", RTPPort: 40001,
		Proto: "TCP/RTP/AVP", Setup: "passive",
	})
	if err != nil {
		t.Fatalf("LiveOfferBytes: %v", err)
	}
	const wantTCP = "v=0\r\n" +
		"o=34020000001310000001001 0 0 IN IP4 10.0.0.5\r\n" +
		"s=Play\r\n" +
		"c=IN IP4 10.0.0.5\r\n" +
		"t=0 0\r\n" +
		"m=video 40001 TCP/RTP/AVP 96\r\n" +
		"a=recvonly\r\n" +
		"a=setup:passive\r\n" +
		"a=rtpmap:96 PS/90000\r\n"
	if tcp != wantTCP {
		t.Errorf("TCP offer:\n got %q\nwant %q", tcp, wantTCP)
	}
}

func TestLiveOfferBytes_RejectsIncompleteInput(t *testing.T) {
	cases := map[string]LiveOffer{
		"no channel": {ConnIP: "10.0.0.5", RTPPort: 40000},
		"no address": {ChannelID: "ch", RTPPort: 40000},
		"bad port":   {ChannelID: "ch", ConnIP: "10.0.0.5", RTPPort: 0},
	}
	for name, in := range cases {
		if _, err := LiveOfferBytes(in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestParseLiveAnswer(t *testing.T) {
	body := "v=0\r\no=34020000001310000001001 0 0 IN IP4 10.0.0.9\r\n" +
		"s=Play\r\nc=IN IP4 10.0.0.9\r\nt=0 0\r\n" +
		"m=video 40002 RTP/AVP 96\r\na=sendonly\r\ny=0100001234\r\n"
	got, err := ParseLiveAnswer(body)
	if err != nil {
		t.Fatalf("ParseLiveAnswer: %v", err)
	}
	if got.ConnIP != "10.0.0.9" || got.Port != 40002 || got.Proto != "RTP/AVP" {
		t.Errorf("answer = %+v", got)
	}
	if got.SSRC != 100001234 {
		t.Errorf("SSRC = %d, want 100001234", got.SSRC)
	}
}

func TestParseLiveAnswer_Defaults(t *testing.T) {
	// No c= and no y= are both legal: the caller falls back to the
	// signalling peer and skips SSRC filtering. Port 0 is legal for TCP.
	got, err := ParseLiveAnswer("v=0\r\nm=video 0 TCP/RTP/AVP 96\r\na=setup:active\r\n")
	if err != nil {
		t.Fatalf("ParseLiveAnswer: %v", err)
	}
	if got.ConnIP != "" || got.SSRC != 0 {
		t.Errorf("answer = %+v, want empty address and zero SSRC", got)
	}
	if got.Proto != "TCP/RTP/AVP" || got.Setup != "active" {
		t.Errorf("transport = %q setup = %q", got.Proto, got.Setup)
	}
}

func TestParseLiveAnswer_RejectsBrokenInput(t *testing.T) {
	for name, body := range map[string]string{
		"no m=video":   "v=0\r\nc=IN IP4 10.0.0.9\r\n",
		"bad port":     "v=0\r\nm=video abc RTP/AVP 96\r\n",
		"out of range": "v=0\r\nm=video 70000 RTP/AVP 96\r\n",
	} {
		if _, err := ParseLiveAnswer(body); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
