// Package app implements the SIP acceptor handling device registration, message
// routing, and media pipeline management for GB/T 28181 simulation.
//
// This file contains MESSAGE routing and command dispatch (Cluster 4): handleMessage
// (dispatch switch), handleInfo, routeMANSRTSP, infoRouting, splitChannel, formatNPT.
package app

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// contentTypeMANSCDP is what a MANSCDP body announces itself as, in the
// spelling GB/T 28181 uses. Every MANSCDP answer this package builds —
// keepalive, catalog, alarm, device control, NOTIFY — carries it.
const contentTypeMANSCDP = "Application/MANSCDP+XML"

// handleMessage answers the MANSCDP messages a downstream sends. The second
// result is false when there is nothing to answer: an unreadable body, or a
// command this platform does not serve. A platform that cannot read a
// message stays silent rather than answering an error the peer will not
// understand.
func (a *Acceptor) handleMessage(
	ctx context.Context,
	p *platform,
	req model.Message,
	peer string,
) (model.Message, bool) {
	notify, err := a.manscdp.DecodeNotify(req.Body())
	if err != nil {
		a.log.Debug("ignoring an unreadable MESSAGE",
			"node_id", p.id.String(), "peer", peer, "error", err.Error())
		return model.Message{}, false
	}
	switch {
	case notify.IsKeepalive():
		return a.refresh(ctx, p, req, notify)
	case notify.IsCatalogQuery():
		return a.answerCatalog(ctx, p, req, notify)
	case notify.IsMediaStatus():
		return a.handleMediaStatus(ctx, p, req, notify)
	case notify.IsPlaybackControl():
		return a.handlePlaybackControl(ctx, p, req, notify)
	case notify.IsDeviceInfo():
		return a.handleDeviceInfo(ctx, p, req, notify)
	case notify.IsRecordInfo():
		return a.handleRecordInfo(ctx, p, req, notify)
	case notify.IsAlarm():
		return a.handleAlarm(ctx, p, req, notify)
	case notify.IsDeviceControl():
		return a.handleDeviceControl(ctx, p, req, notify)
	case notify.IsPresetQuery():
		return a.handlePresetQuery(ctx, p, req, notify)
	case notify.IsHomePosition():
		if !a.is2022(ctx, p, notify.DeviceID()) {
			return model.Message{}, false
		}
		return a.handleHomePosition(ctx, p, req, notify)
	case notify.IsCruiseTrackList():
		if !a.is2022(ctx, p, notify.DeviceID()) {
			return model.Message{}, false
		}
		return a.handleCruiseTrackList(ctx, p, req, notify)
	case notify.IsSnapShot():
		if !a.is2022(ctx, p, notify.DeviceID()) {
			return model.Message{}, false
		}
		return a.handleSnapShot(ctx, p, req, notify)
	default:
		a.log.Debug("ignoring an unsupported command",
			"node_id", p.id.String(), "cmd", notify.CmdType(), "device_id", notify.DeviceID())
		return model.Message{}, false
	}
}

// refresh answers a keepalive: the device's row is stamped as seen and the
// granted lifetime is left alone, because a heartbeat is not a
// re-registration and must not extend it.
//
// A keepalive from a device that is not in the table is ignored rather than
// answered: a platform that grants nothing cannot say "OK" to it.
func (a *Acceptor) refresh(
	ctx context.Context,
	p *platform,
	req model.Message,
	notify model.Notify,
) (model.Message, bool) {
	deviceID := notify.DeviceID()
	dev, ok := a.devices.Lookup(ctx, p.id, deviceID)
	if !ok {
		a.log.Warn("ignoring a keepalive from a device that is not registered",
			"node_id", p.id.String(), "device_id", deviceID)
		return model.Message{}, false
	}
	now := a.clock.Now()
	if err := a.devices.Upsert(ctx, p.id, dev.WithSeen(now)); err != nil {
		a.log.Warn("cannot refresh a downstream",
			"node_id", p.id.String(), "device_id", deviceID, "error", err.Error())
		return model.Message{}, false
	}
	a.log.Debug("downstream keepalive", "node_id", p.id.String(),
		"device_id", deviceID, "sn", notify.SN())
	resp, err := buildResponse(200, "OK", req, nil, "")
	if err != nil {
		a.log.Warn("cannot answer a keepalive",
			"node_id", p.id.String(), "device_id", deviceID, "error", err.Error())
		return model.Message{}, false
	}
	return resp, true
}

// contentTypeMANSRTSP is what an INFO carrying MANSRTSP announces its
// body as. The text is the GB/T 28181-2022 §G spelling.
const contentTypeMANSRTSP = "Application/MANSRTSP"

// handleInfo routes an INFO request by Content-Type (task 7.3):
//
//  1. Application/MANSRTSP (or body starting with PLAY/PAUSE) → MANSRTSP
//     playback control, mapped to PlaybackPort.
//  2. Application/MANSCDP+XML with CmdType=MediaStatus → reuse the
//     MediaStatus handler that MESSAGE used to dispatch.
//  3. Anything else → 200 OK with a debug log, so a probing client is
//     not penalised for an unknown body shape.
func (a *Acceptor) handleInfo(
	ctx context.Context,
	p *platform,
	req model.Message,
	peer string,
) (model.Message, error) {
	ct := headerValue(req, "Content-Type")
	body := req.Body()
	switch {
	case isMANSRTSP(ct, body):
		return a.routeMANSRTSP(ctx, p, req, peer, body)
	case strings.EqualFold(ct, contentTypeMANSCDP) && looksLikeMediaStatus(body):
		notify, err := a.manscdp.DecodeNotify(body)
		if err != nil {
			a.log.Debug("INFO/MANSCDP body not parseable",
				"node_id", p.id.String(), "peer", peer, "error", err.Error())
			return buildResponse(200, "OK", req, nil, "")
		}
		if a.mediaStatus == nil {
			return buildResponse(200, "OK", req, nil, "")
		}
		report, rErr := model.NewMediaStatusReport(
			notify.DeviceID(), notify.Status(), notify.SN())
		if rErr != nil {
			a.log.Warn("INFO media-status report malformed",
				"node_id", p.id.String(), "device_id", notify.DeviceID(), "error", rErr.Error())
			return buildResponse(200, "OK", req, nil, "")
		}
		if err := a.mediaStatus.HandleMediaStatus(ctx, report); err != nil {
			a.log.Warn("INFO media-status handler failed",
				"node_id", p.id.String(), "device_id", notify.DeviceID(), "error", err.Error())
		}
		return buildResponse(200, "OK", req, nil, "")
	default:
		a.log.Debug("INFO with unknown body shape; answering 200 OK",
			"node_id", p.id.String(), "peer", peer, "content_type", ct, "size", len(body))
		return buildResponse(200, "OK", req, nil, "")
	}
}

// isMANSRTSP reports whether the body should be parsed as MANSRTSP. The
// Content-Type hint is preferred; the request-line sniff is the fallback
// for clients that omit the header (a 2022 peer may, an older one always
// does).
func isMANSRTSP(contentType, body string) bool {
	if strings.EqualFold(contentType, contentTypeMANSRTSP) {
		return true
	}
	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(trimmed, "PLAY ") || strings.HasPrefix(trimmed, "PAUSE ") {
		return true
	}
	return false
}

// looksLikeMediaStatus cheaply matches the body to a MediaStatus notify
// without paying the parser cost. The full decode is done once the
// envelope is identified.
func looksLikeMediaStatus(body string) bool {
	return strings.Contains(body, "<CmdType>MediaStatus</CmdType>")
}

// routeMANSRTSP parses the body and dispatches the resulting playback
// command. An unparseable body or a port that refuses the call gets
// answered 400 so the client knows it was the body that was bad, not
// the dialog.
func (a *Acceptor) routeMANSRTSP(
	ctx context.Context,
	p *platform,
	req model.Message,
	peer string,
	body string,
) (model.Message, error) {
	cmd, err := port.Parse(body)
	if err != nil {
		a.log.Warn("INFO/MANSRTSP body not parseable",
			"node_id", p.id.String(), "peer", peer, "error", err.Error())
		return buildResponse(400, "Bad Request", req, nil, "")
	}
	if a.playback == nil {
		a.log.Debug("INFO/MANSRTSP arrived without a playback port; 200 OK only",
			"node_id", p.id.String(), "peer", peer, "command", cmd.Command)
		return buildResponse(200, "OK", req, nil, "")
	}
	deviceID, channelID, ok := infoRouting(req)
	if !ok {
		a.log.Warn("INFO/MANSRTSP missing device id; answering 400",
			"node_id", p.id.String(), "peer", peer)
		return buildResponse(400, "Bad Request", req, nil, "")
	}
	switch cmd.Command {
	case port.CommandPlay:
		start := ""
		end := ""
		if cmd.HasSeek {
			start = formatNPT(cmd.SeekFrom)
			if cmd.SeekTo >= 0 {
				end = formatNPT(cmd.SeekTo)
			}
		}
		if _, err := a.playback.Play(ctx, deviceID, channelID, start, end, cmd.Scale); err != nil {
			a.log.Warn("PlaybackPort.Play failed",
				"node_id", p.id.String(), "peer", peer, "device_id", deviceID,
				"error", err.Error())
			if errors.Is(err, port.ErrPlaybackUnsupported) {
				return buildResponse(400, "Bad Request", req, nil, "")
			}
			return buildResponse(481, "Call/Transaction Does Not Exist", req, nil, "")
		}
	case port.CommandPause:
		// PlaybackPort has no Pause verb; PAUSE is expressed as
		// SetScale(0) on the session named by the Call-ID, falling back
		// to a session-less request that the port may refuse (right
		// answer for a free-floating pause).
		sessionID := req.CallID()
		if err := a.playback.SetScale(ctx, sessionID, 0); err != nil {
			a.log.Warn("PlaybackPort.SetScale failed",
				"node_id", p.id.String(), "peer", peer, "device_id", deviceID,
				"session_id", sessionID, "error", err.Error())
			if errors.Is(err, port.ErrPlaybackUnsupported) {
				return buildResponse(400, "Bad Request", req, nil, "")
			}
			return buildResponse(481, "Call/Transaction Does Not Exist", req, nil, "")
		}
	}
	return buildResponse(200, "OK", req, nil, "")
}

// infoRouting extracts the downstream identity an INFO command applies
// to. The Request-URI is the GB/T 28181 device id; the To header is the
// fallback for clients that built the URI wrong.
func infoRouting(req model.Message) (deviceID, channelID string, ok bool) {
	if u := req.URI(); u != nil {
		s := u.String()
		if i := strings.LastIndex(s, ":"); i >= 0 {
			user := s[i+1:]
			if j := strings.IndexAny(user, "@>;"); j > 0 {
				user = user[:j]
			}
			deviceID, channelID = splitChannel(user)
			if deviceID != "" {
				return deviceID, channelID, true
			}
		}
	}
	if to := headerValue(req, "To"); to != "" {
		if i := strings.Index(to, ":sip:"); i >= 0 {
			user := to[i+len(":sip:"):]
			if j := strings.IndexAny(user, "@>;"); j > 0 {
				user = user[:j]
			}
			deviceID, channelID = splitChannel(user)
			if deviceID != "" {
				return deviceID, channelID, true
			}
		}
	}
	return "", "", false
}

// splitChannel splits the optional channel suffix the GB/T 28181 device
// id carries, e.g. "34020000001320000001-02" → ("34020000001320000001",
// "02").
func splitChannel(s string) (string, string) {
	i := strings.Index(s, "-")
	if i < 0 {
		return s, ""
	}
	return s[:i], s[i+1:]
}

// formatNPT renders a number of seconds the way MANSRTSP expects.
func formatNPT(seconds float64) string {
	if seconds == 0 {
		return "0"
	}
	return strconv.FormatFloat(seconds, 'f', -1, 64)
}
