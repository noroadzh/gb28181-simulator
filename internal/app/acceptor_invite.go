package app

// INVITE handling: the media-session entry point. handleInvite answers an
// incoming INVITE by creating or reusing the Dialog for its Call-ID, wiring
// the inbound media pipeline, and replying 200 OK. The negotiation details
// (SDP offer/answer, PS/RTP flow) live in the adapter layer; this file owns
// the SIP-side bookkeeping and the dialog wiring.

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// handleInvite answers an incoming INVITE request. For now it creates or
// reuses a Dialog for the Call-ID, echoes the received body as the answer,
// and returns 200 OK. A real media pipeline will be wired in Change #8.
func (a *Acceptor) handleInvite(
	ctx context.Context,
	p *platform,
	req model.Message,
	_ string,
) (model.Message, error) {
	a.log.Debug("INVITE received",
		"node_id", p.id.String(), "call_id", headerValue(req, "Call-ID"))
	// A device is a camera: it answers talk (audio), historical playback
	// (video with a time window) and live viewing (plain video) itself, each
	// with its own RTP port. Anything else keeps the device's usual behaviour.
	if p.isDevice {
		if strings.Contains(req.Body(), "s=Playback") {
			return a.answerDevicePlaybackInvite(p, req)
		}
		if mediaType, _, _, mErr := parseSDPMetadata(req.Body()); mErr == nil && mediaType == "video" {
			return a.answerDeviceVideoInvite(p, req)
		}
		return a.answerDeviceTalkInvite(p, req)
	}
	// A platform serves recorded playback from its own library; anything
	// else on the platform side keeps the generic dialog handling below.
	if a.recordings != nil && strings.Contains(req.Body(), "s=Playback") {
		return a.answerPlatformPlaybackInvite(p, req)
	}
	callID := headerValue(req, "Call-ID")
	if callID == "" {
		return buildResponse(400, "Bad Request", req, nil, "")
	}
	if a.dialogs != nil {
		if _, err := a.dialogs.Get(callID); !err {
			a.dialogs.Create(callID)
			a.log.Debug("INVITE dialog created", "node_id", p.id.String(), "call_id", callID)
		}
	}
	mediaType, portStr, _, sdpErr := parseSDPMetadata(req.Body())
	switch {
	case sdpErr != nil:
		a.log.Warn("INVITE SDP unreadable", "node_id", p.id.String(),
			"call_id", callID, "error", sdpErr.Error())
		if a.dialogs != nil {
			a.dialogs.Terminate(callID)
		}
		return buildResponse(400, "Bad Request", req, nil, "")
	case mediaType == "":
		a.log.Debug("INVITE accepted without media section",
			"node_id", p.id.String(), "call_id", callID)
	default:
		a.createInboundPipeline(ctx, p, callID, mediaType, portStr, req.Body())
		a.watchInviteExpiry(p, callID)
	}
	hdrs := []model.Header{
		model.NewHeader("Contact", "<sip:"+p.id.String()+"@"+p.realm+">"),
		allowHeader(),
	}
	return buildResponse(200, "OK", req, hdrs, req.Body())
}

// watchInviteExpiry schedules a 30 s timer for the INVITE dialog. If the ACK
// is not received in time the dialog is terminated, which tears down the
// inbound media pipeline through the manager's onDelete callback.
//
// The watcher goroutine exits on whichever of three signals fires first:
// the timer firing, an explicit cancellation (ACK arrived), or the parent
// node stopping. That third arm is the one the previous implementation
// missed, and is the reason this used to leak a goroutine per cancel.
func (a *Acceptor) watchInviteExpiry(p *platform, callID string) {
	// A re-INVITE refreshes the wait for its own ACK; the previous watcher
	// must not fire on the new dialog.
	a.cancelInviteExpiry(callID)
	timer := time.NewTimer(30 * time.Second)
	done := make(chan struct{})

	a.inviteMu.Lock()
	a.inviteTimers[callID] = timer
	a.inviteWatchers[callID] = done
	a.inviteMu.Unlock()

	go func() {
		select {
		case <-timer.C:
			a.log.Debug("INVITE expired without ACK, terminating dialog",
				"node_id", p.id.String(), "call_id", callID)
			if a.dialogs != nil {
				a.dialogs.Terminate(callID)
			}
		case <-done:
			// Cancelled by ACK or call-site termination; nothing to do.
		case <-p.done:
			// The node that owned this dialog stopped serving (Stop and
			// Close both wait for p.done), so the dialog table is being
			// unwound by whoever stopped it. Tearing down from here
			// would report a media fault for a clean shutdown.
		case <-a.ctx.Done():
			// The whole acceptor is shutting down: same reasoning, one
			// level up.
		}
		a.inviteMu.Lock()
		delete(a.inviteTimers, callID)
		delete(a.inviteWatchers, callID)
		a.inviteMu.Unlock()
	}()
}

// cancelInviteExpiry stops a pending INVITE expiry timer and removes it.
// Closing the done channel is what wakes the watcher goroutine; timer.Stop
// alone would not, because Stop on an unfired timer does not drain the
// channel that the goroutine is reading.
func (a *Acceptor) cancelInviteExpiry(callID string) {
	a.inviteMu.Lock()
	timer, timerOK := a.inviteTimers[callID]
	done, doneOK := a.inviteWatchers[callID]
	delete(a.inviteTimers, callID)
	delete(a.inviteWatchers, callID)
	a.inviteMu.Unlock()

	if !timerOK && !doneOK {
		return
	}
	if doneOK {
		close(done)
	}
	if timerOK {
		timer.Stop()
	}
}

// handleAck advances the dialog for an INVITE transaction but does not answer:
// ACK carries no response in the SIP request/response model. Confirming the
// dialog also cancels the INVITE expiry timer, so a confirmed dialog is not
// torn down 30 s later by a stale timer.
func (a *Acceptor) handleAck(
	_ context.Context,
	p *platform,
	req model.Message,
	_ string,
) {
	a.log.Debug("ACK received",
		"node_id", p.id.String(), "call_id", headerValue(req, "Call-ID"))
	callID := headerValue(req, "Call-ID")
	if callID == "" || a.dialogs == nil {
		return
	}
	if d, err := a.dialogs.Get(callID); err && !d.IsTerminated() {
		if _, err := a.dialogs.Confirm(callID, nil); err != nil {
			a.log.Warn("ACK could not confirm dialog",
				"node_id", p.id.String(), "call_id", callID, "error", err.Error())
			return
		}
		a.cancelInviteExpiry(callID)
	}
}

// handleBye terminates the dialog identified by the Call-ID and answers with
// 200 OK.
func (a *Acceptor) handleBye(
	_ context.Context,
	p *platform,
	req model.Message,
	_ string,
) (model.Message, error) {
	a.log.Debug("BYE received",
		"node_id", p.id.String(), "call_id", headerValue(req, "Call-ID"))
	callID := headerValue(req, "Call-ID")
	if callID != "" && a.dialogs != nil {
		a.dialogs.Terminate(callID)
		a.closePipeline(callID)
		a.log.Debug("BYE terminated dialog", "node_id", p.id.String(), "call_id", callID)
	}
	return buildResponse(200, "OK", req, nil, "")
}

// handleOptions answers a keepalive probe. The Allow header advertises what
// the platform serves; a minimal 200 OK is enough for the peer to mark the
// link healthy.
func (a *Acceptor) handleOptions(
	_ context.Context,
	_ *platform,
	req model.Message,
	_ string,
) (model.Message, error) {
	hdrs := []model.Header{
		allowHeader(),
		model.NewHeader("Accept", "Application/MANSCDP+XML"),
	}
	return buildResponse(200, "OK", req, hdrs, "")
}

// answerDeviceTalkInvite answers an audio INVITE from a device node. The node
// opens its own RTP port and puts it in the answer: echoing the offer's port
// would point both sides at the same socket and no audio would ever flow.
//
// A non-audio INVITE is refused with 488 — the device-as-camera contract this
// change writes down.
func (a *Acceptor) answerDeviceTalkInvite(p *platform, req model.Message) (model.Message, error) {
	mediaType, _, _, err := parseSDPMetadata(req.Body())
	if err != nil || mediaType != "audio" {
		a.log.Debug("device refuses a non-audio INVITE",
			"node_id", p.id.String(), "media", mediaType)
		return buildResponse(488, "Not Acceptable Here", req,
			[]model.Header{allowHeader()}, "")
	}
	callID := headerValue(req, "Call-ID")
	if callID == "" {
		return buildResponse(400, "Bad Request", req, nil, "")
	}

	conn, lerr := net.ListenPacket("udp4", "0.0.0.0:0")
	if lerr != nil {
		a.log.Warn("device cannot open a talk audio port",
			"node_id", p.id.String(), "error", lerr.Error())
		return buildResponse(500, "Server Internal Error", req, nil, "")
	}
	host := "127.0.0.1"
	if h, _, splitErr := net.SplitHostPort(p.tr.LocalAddr()); splitErr == nil {
		host = h
	}
	port := conn.LocalAddr().(*net.UDPAddr).Port

	a.deviceTalksMu.Lock()
	if old := a.deviceTalks[callID]; old != nil {
		_ = old.Close()
	}
	a.deviceTalks[callID] = conn
	a.deviceTalksMu.Unlock()

	answer := buildTalkOffer(host, port)
	hdrs := []model.Header{
		model.NewHeader("Contact", "<sip:"+p.id.String()+"@"+p.realm+">"),
		model.NewHeader("Content-Type", "application/sdp"),
		allowHeader(),
	}
	a.log.Debug("device answered a talk INVITE",
		"node_id", p.id.String(), "call_id", callID, "rtp_port", port)
	return buildResponse(200, "OK", req, hdrs, answer)
}

// closeDeviceTalk releases the audio port of a finished dialog.
func (a *Acceptor) closeDeviceTalk(callID string) {
	a.deviceTalksMu.Lock()
	conn := a.deviceTalks[callID]
	delete(a.deviceTalks, callID)
	a.deviceTalksMu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

// answerDevicePlaybackInvite answers a historical playback INVITE: the node
// opens its own sending port and answers with the same time window and
// a=sendonly. A channel with no media source is 404 — the platform asked for
// a recording this device cannot produce.
func (a *Acceptor) answerDevicePlaybackInvite(p *platform, req model.Message) (model.Message, error) {
	callID := headerValue(req, "Call-ID")
	if callID == "" {
		return buildResponse(400, "Bad Request", req, nil, "")
	}
	offer, err := model.ParsePlaybackSDP(req.Body())
	if err != nil {
		a.log.Warn("device cannot read a playback INVITE",
			"node_id", p.id.String(), "error", err.Error())
		return buildResponse(400, "Bad Request", req, nil, "")
	}
	if !a.deviceHasMediaSource(p, offer.DeviceID) {
		a.log.Debug("device has no source for the requested playback",
			"node_id", p.id.String(), "channel", offer.DeviceID)
		return buildResponse(404, "Not Found", req,
			[]model.Header{allowHeader()}, "")
	}

	sink, serr := openMediaSink(offerTransport{Proto: offer.Proto, Setup: offer.Setup},
		offer.ConnIP, offer.RTPPort)
	if serr != nil {
		a.log.Warn("device cannot open the playback transport",
			"node_id", p.id.String(), "error", serr.Error())
		return buildResponse(488, "Not Acceptable Here", req, nil, "")
	}
	host := "127.0.0.1"
	if h, _, splitErr := net.SplitHostPort(p.tr.LocalAddr()); splitErr == nil {
		host = h
	}
	port, proto, setup := sink.Answer()
	answer := model.PlaybackSDP{
		DeviceID: offer.DeviceID,
		SN:       offer.SN,
		ConnIP:   host,
		RTPPort:  port,
		Start:    offer.Start,
		End:      offer.End,
		Mode:     "sendonly",
		Proto:    proto,
		Setup:    setup,
	}
	body, berr := model.PlaybackSDPBytes(answer)
	if berr != nil {
		_ = sink.Close()
		return buildResponse(500, "Server Internal Error", req, nil, "")
	}

	a.deviceTalksMu.Lock()
	if a.devicePlaybacks == nil {
		a.devicePlaybacks = map[string]mediaSink{}
	}
	if old := a.devicePlaybacks[callID]; old != nil {
		_ = old.Close()
	}
	a.devicePlaybacks[callID] = sink
	a.deviceTalksMu.Unlock()
	go a.streamPlayback(callID, p, sink, offer)

	hdrs := []model.Header{
		model.NewHeader("Contact", "<sip:"+p.id.String()+"@"+p.realm+">"),
		model.NewHeader("Content-Type", "application/sdp"),
		allowHeader(),
	}
	a.log.Debug("device answered a playback INVITE",
		"node_id", p.id.String(), "channel", offer.DeviceID,
		"sn", offer.SN, "rtp_port", answer.RTPPort)
	return buildResponse(200, "OK", req, hdrs, body)
}

// deviceMediaConfig returns the source the node would play for channelID: a
// per-channel source wins, then the node-level one.
func (a *Acceptor) deviceMediaConfig(p *platform, channelID string) (model.MediaConfig, bool) {
	if a.registry == nil {
		return model.MediaConfig{}, false
	}
	node, ok := a.registry.Get(context.Background(), p.id)
	if !ok {
		return model.MediaConfig{}, false
	}
	if cfg, ok := node.Profile().ChannelMediaConfig(channelID); ok {
		return cfg, true
	}
	return node.Profile().MediaConfig()
}

func (a *Acceptor) deviceHasMediaSource(p *platform, channelID string) bool {
	_, ok := a.deviceMediaConfig(p, channelID)
	return ok
}

// closeDevicePlaybackIf is the re-INVITE-safe counterpart of
// closeDevicePlayback: it leaves a newer sink installed by a renegotiation
// alone.
func (a *Acceptor) closeDevicePlaybackIf(callID string, sink mediaSink) {
	a.deviceTalksMu.Lock()
	if a.devicePlaybacks[callID] != sink {
		a.deviceTalksMu.Unlock()
		return
	}
	delete(a.devicePlaybacks, callID)
	a.deviceTalksMu.Unlock()
	_ = sink.Close()
}

// closeDevicePlayback releases the playback port of a finished dialog. The
// port is keyed by Call-ID, the only dialog identity a BYE carries.
func (a *Acceptor) closeDevicePlayback(callID string) {
	a.deviceTalksMu.Lock()
	sink := a.devicePlaybacks[callID]
	delete(a.devicePlaybacks, callID)
	a.deviceTalksMu.Unlock()
	if sink != nil {
		_ = sink.Close()
	}
}

// streamPlayback sends the recording to the endpoint the offer named, from now
// until the offer's end time (or until the port is closed by a BYE). The
// source is read from its beginning: the file demuxer has no time seek, so a
// window that starts later is served from the first frame rather than not at
// all — the log says so at debug level.
func (a *Acceptor) streamPlayback(callID string, p *platform, sink mediaSink, offer model.PlaybackSDP) {
	if a.mediaService == nil {
		a.log.Debug("playback stream skipped: no media service wired",
			"node_id", p.id.String(), "sn", offer.SN)
		return
	}
	cfg, ok := a.deviceMediaConfig(p, offer.DeviceID)
	if !ok {
		return
	}
	window := time.Until(offer.End)
	if window <= 0 {
		a.log.Debug("playback window already closed", "node_id", p.id.String(), "sn", offer.SN)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), window)
	defer cancel()

	_, proto, _ := sink.Answer()
	a.log.Debug("playback stream starting",
		"node_id", p.id.String(), "sn", offer.SN,
		"dst", net.JoinHostPort(offer.ConnIP, strconv.Itoa(offer.RTPPort)),
		"transport", proto, "window", window.String())
	err := a.mediaService.PacketizeOutbound(ctx, cfg, func(pkt model.RTPPacket) error {
		return sink.Write(pkt.Bytes())
	})
	if err != nil && ctx.Err() == nil {
		a.log.Debug("playback stream ended early",
			"node_id", p.id.String(), "sn", offer.SN, "error", err.Error())
	}
	a.closeDevicePlaybackIf(callID, sink)
}

// answerDeviceVideoInvite answers a live-viewing INVITE. The device opens its
// own sending port and streams the channel's media source to the endpoint the
// offer named, until BYE.
//
// The channel is the user part of To: a live INVITE carries no u= line.
func (a *Acceptor) answerDeviceVideoInvite(p *platform, req model.Message) (model.Message, error) {
	callID := headerValue(req, "Call-ID")
	channelID := toUser(req)
	if callID == "" || channelID == "" {
		a.log.Debug("device cannot route a video INVITE",
			"node_id", p.id.String(), "call_id", callID, "channel", channelID)
		return buildResponse(400, "Bad Request", req, nil, "")
	}
	offer, derr := parseVideoOffer(req.Body())
	if derr != nil {
		a.log.Warn("device cannot read a video offer",
			"node_id", p.id.String(), "error", derr.Error())
		return buildResponse(400, "Bad Request", req, nil, "")
	}
	cfg, ok := a.deviceMediaConfig(p, channelID)
	if !ok {
		a.log.Debug("device has no source for the requested channel",
			"node_id", p.id.String(), "channel", channelID)
		return buildResponse(404, "Not Found", req, []model.Header{allowHeader()}, "")
	}

	// GB/T 28181 附录 D: the offer decides between UDP and TCP media, and
	// who opens the connection. A transport we cannot serve is a 488,
	// not a silent fallback to UDP.
	sink, serr := openMediaSink(offer.offerTransport, offer.Host, offer.Port)
	if serr != nil {
		a.log.Warn("device cannot open the video transport",
			"node_id", p.id.String(), "error", serr.Error())
		return buildResponse(488, "Not Acceptable Here", req, nil, "")
	}
	localHost := "127.0.0.1"
	if h, _, splitErr := net.SplitHostPort(p.tr.LocalAddr()); splitErr == nil {
		localHost = h
	}
	localPort, proto, setup := sink.Answer()
	answer := fmt.Sprintf("v=0\r\no=%s 0 0 IN IP4 %s\r\ns=Play\r\nc=IN IP4 %s\r\nt=0 0\r\n"+
		"m=video %d %s 96\r\na=sendonly\r\n", p.id.String(), localHost, localHost, localPort, proto)
	if setup != "" {
		answer += "a=setup:" + setup + "\r\n"
	}
	answer += "a=rtpmap:96 PS/90000\r\n"

	a.deviceTalksMu.Lock()
	if a.deviceVideos == nil {
		a.deviceVideos = map[string]mediaSink{}
	}
	if old := a.deviceVideos[callID]; old != nil {
		_ = old.Close()
	}
	a.deviceVideos[callID] = sink
	a.deviceTalksMu.Unlock()

	go a.streamLiveVideo(callID, p, sink, cfg)

	hdrs := []model.Header{
		model.NewHeader("Contact", "<sip:"+p.id.String()+"@"+p.realm+">"),
		model.NewHeader("Content-Type", "application/sdp"),
		allowHeader(),
	}
	a.log.Debug("device answered a live video INVITE",
		"node_id", p.id.String(), "channel", channelID, "rtp_port", localPort)
	return buildResponse(200, "OK", req, hdrs, answer)
}

// closeDeviceVideoIf releases the dialog's sending transport only while sink
// is still the current one. A re-INVITE replaces the entry; the old stream
// ending afterwards must not close its successor.
func (a *Acceptor) closeDeviceVideoIf(callID string, sink mediaSink) {
	a.deviceTalksMu.Lock()
	if a.deviceVideos[callID] != sink {
		a.deviceTalksMu.Unlock()
		return
	}
	delete(a.deviceVideos, callID)
	a.deviceTalksMu.Unlock()
	_ = sink.Close()
}

// streamLiveVideo sends the channel source to the platform until the port is
// closed (BYE) or the source ends.
func (a *Acceptor) streamLiveVideo(callID string, p *platform, sink mediaSink, cfg model.MediaConfig) {
	if a.mediaService == nil {
		a.log.Debug("live video stream skipped: no media service wired",
			"node_id", p.id.String(), "call_id", callID)
		return
	}
	_, proto, _ := sink.Answer()
	a.log.Debug("live video stream starting",
		"node_id", p.id.String(), "call_id", callID, "transport", proto)
	err := a.mediaService.PacketizeOutbound(context.Background(), cfg, func(pkt model.RTPPacket) error {
		return sink.Write(pkt.Bytes())
	})
	if err != nil {
		a.log.Debug("live video stream ended",
			"node_id", p.id.String(), "call_id", callID, "error", err.Error())
	}
	a.closeDeviceVideoIf(callID, sink)
}

// closeDeviceVideo releases the sending port of a finished live dialog.
func (a *Acceptor) closeDeviceVideo(callID string) {
	a.deviceTalksMu.Lock()
	sink := a.deviceVideos[callID]
	delete(a.deviceVideos, callID)
	a.deviceTalksMu.Unlock()
	if sink != nil {
		_ = sink.Close()
	}
}

// liveVideoOffer is the endpoint and transport of a live video INVITE.
type liveVideoOffer struct {
	Host string
	Port int
	offerTransport
}

// parseVideoOffer reads the platform's endpoint and transport out of a live
// video offer. The transport is optional: an m-line without a proto is UDP,
// the pre-附录 D shape every platform speaks.
func parseVideoOffer(body string) (liveVideoOffer, error) {
	var out liveVideoOffer
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		switch {
		case strings.HasPrefix(line, "c=IN IP4 "):
			out.Host = strings.TrimSpace(strings.TrimPrefix(line, "c=IN IP4 "))
		case strings.HasPrefix(line, "a=setup:"):
			out.Setup = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "a=setup:")))
		case strings.HasPrefix(line, "m=video "):
			fields := strings.Fields(strings.TrimPrefix(line, "m=video "))
			if len(fields) > 0 {
				if n, err := strconv.Atoi(fields[0]); err == nil {
					out.Port = n
				}
			}
			// m= fields after the port: transport, then formats.
			if len(fields) > 1 {
				out.Proto = fields[1]
			}
		}
	}
	if out.Host == "" || out.Port <= 0 || out.Port > 65535 {
		return liveVideoOffer{}, fmt.Errorf("video offer lacks c= or a usable m=video port")
	}
	return out, nil
}

// toUser returns the user part of the To header, which names the channel a
// live INVITE targets.
func toUser(msg model.Message) string {
	if h, ok := msg.Header("To"); ok {
		return sipUser(h.Value())
	}
	return ""
}
