package app

// Platform recording: RecordInfo answers and playback INVITEs served from the
// platform's own recording library. The device side keeps its fixed catalogue
// and its media-source playback.

import (
	"context"
	"net"
	"strings"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// recordStampFormat is the timestamp spelling RecordInfo items use.
const recordStampFormat = "20060102T150405"

// parseRecordBound reads a query bound. Real platforms send either the
// dashed query layout or the compact item layout; both are accepted, and an
// unreadable bound means "unbounded" rather than "no records".
func parseRecordBound(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range []string{"2006-01-02T15:04:05", recordStampFormat} {
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}

// libraryRecordItems turns the platform's recordings into RecordInfo items
// for the queried channel and window.
func (a *Acceptor) libraryRecordItems(query model.RecordInfoQuery, p *platform, notify model.Notify) []model.RecordInfoItem {
	recs := a.recordings.List(context.Background(), p.id, notify.DeviceID(),
		parseRecordBound(query.StartTime), parseRecordBound(query.EndTime))
	items := make([]model.RecordInfoItem, 0, len(recs))
	for _, rec := range recs {
		items = append(items, model.NewRecordInfoItem(model.RecordInfoItemParams{
			Name:      rec.ID,
			DeviceID:  notify.DeviceID(),
			ChannelID: notify.DeviceID(),
			StartTime: rec.Start.Format(recordStampFormat),
			EndTime:   rec.End.Format(recordStampFormat),
			FilePath:  rec.Path,
		}))
	}
	return items
}

// answerPlatformPlaybackInvite serves a recorded window to a platform that
// asked for it over SIP. The dialog shape mirrors the device side: the answer
// names our own port, and the media goes to the endpoint the offer named.
func (a *Acceptor) answerPlatformPlaybackInvite(p *platform, req model.Message) (model.Message, error) {
	callID := headerValue(req, "Call-ID")
	if callID == "" {
		return buildResponse(400, "Bad Request", req, nil, "")
	}
	offer, err := model.ParsePlaybackSDP(req.Body())
	if err != nil {
		a.log.Warn("platform cannot read a playback INVITE",
			"node_id", p.id.String(), "error", err.Error())
		return buildResponse(400, "Bad Request", req, nil, "")
	}
	recs := a.recordings.List(context.Background(), p.id, offer.DeviceID, offer.Start, offer.End)
	if len(recs) == 0 {
		a.log.Debug("platform has no recording for the requested window",
			"node_id", p.id.String(), "channel", offer.DeviceID,
			"start", offer.Start.Format(time.RFC3339), "end", offer.End.Format(time.RFC3339))
		return buildResponse(404, "Not Found", req, []model.Header{allowHeader()}, "")
	}
	if a.mediaService == nil {
		a.log.Warn("platform playback skipped: no media service wired", "node_id", p.id.String())
		return buildResponse(500, "Server Internal Error", req, nil, "")
	}

	sink, serr := openMediaSink(offerTransport{Proto: offer.Proto, Setup: offer.Setup},
		offer.ConnIP, offer.RTPPort)
	if serr != nil {
		a.log.Warn("platform cannot open the playback transport",
			"node_id", p.id.String(), "error", serr.Error())
		return buildResponse(488, "Not Acceptable Here", req, nil, "")
	}
	localPort, proto, setup := sink.Answer()
	answer := model.PlaybackSDP{
		DeviceID: offer.DeviceID,
		SN:       offer.SN,
		ConnIP:   localHostOf(p.tr.LocalAddr()),
		RTPPort:  localPort,
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

	go a.streamRecordingPlayback(callID, p, sink, offer)

	hdrs := []model.Header{
		model.NewHeader("Contact", "<sip:"+p.id.String()+"@"+p.realm+">"),
		model.NewHeader("Content-Type", "application/sdp"),
		allowHeader(),
	}
	a.log.Debug("platform answered a playback INVITE",
		"node_id", p.id.String(), "channel", offer.DeviceID,
		"sn", offer.SN, "recordings", len(recs))
	return buildResponse(200, "OK", req, hdrs, body)
}

// streamRecordingPlayback reads the recorded window and pushes it as PS/RTP.
// v1 does not pace to wall clock: the goal is that the peer receives the
// complete recording, not that it arrives at the original rate.
func (a *Acceptor) streamRecordingPlayback(callID string, p *platform, sink mediaSink, offer model.PlaybackSDP) {
	rtpizer, err := a.mediaService.NewRTPizer(0)
	if err != nil {
		a.log.Warn("platform playback has no RTP packetizer", "error", err.Error())
		a.closeDevicePlayback(callID)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	frames, cleanup, err := a.recordings.Stream(ctx, p.id, offer.DeviceID, offer.Start, offer.End)
	if err != nil {
		a.log.Warn("platform playback cannot open the recording",
			"node_id", p.id.String(), "channel", offer.DeviceID, "error", err.Error())
		a.closeDevicePlayback(callID)
		return
	}
	defer cleanup()

	for {
		select {
		case <-ctx.Done():
			a.closeDevicePlayback(callID)
			return
		case ps, ok := <-frames:
			if !ok {
				a.log.Debug("platform playback finished",
					"node_id", p.id.String(), "channel", offer.DeviceID, "sn", offer.SN)
				a.closeDevicePlayback(callID)
				return
			}
			pkts, perr := rtpizer.Packetize(ps)
			if perr != nil {
				continue
			}
			for _, pkt := range pkts {
				if err := sink.Write(pkt.Bytes()); err != nil {
					cancel()
					a.closeDevicePlayback(callID)
					return
				}
			}
		}
	}
}

func localHostOf(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}
