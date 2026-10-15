// Package app implements the SIP acceptor handling device registration, message
// routing, and media pipeline management for GB/T 28181 simulation.
//
// This file contains device command handling (Cluster 4): handleDeviceInfo,
// handleRecordInfo, handleAlarm, handleDeviceControl, handlePresetQuery,
// handleHomePosition, handleCruiseTrackList, handleSnapShot, handlePlaybackControl,
// is2022.
package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// handleMediaStatus processes a downstream MediaStatus notify. The port is
// optional: when absent the notify is acknowledged but otherwise ignored.
func (a *Acceptor) handleMediaStatus(
	ctx context.Context,
	p *platform,
	req model.Message,
	notify model.Notify,
) (model.Message, bool) {
	if a.mediaStatus != nil {
		report, err := model.NewMediaStatusReport(
			notify.DeviceID(),
			notify.Status(),
			notify.SN(),
		)
		if err != nil {
			a.log.Warn("media-status report malformed",
				"node_id", p.id.String(), "device_id", notify.DeviceID(), "error", err.Error())
		} else if err := a.mediaStatus.HandleMediaStatus(ctx, report); err != nil {
			a.log.Warn("media-status handler failed",
				"node_id", p.id.String(), "device_id", notify.DeviceID(), "error", err.Error())
		}
	}
	resp, err := buildResponse(200, "OK", req, nil, "")
	if err != nil {
		a.log.Warn("cannot answer a MediaStatus",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	a.log.Debug("media-status received", "node_id", p.id.String(),
		"device_id", notify.DeviceID(), "sn", notify.SN(), "status", notify.Status())
	return resp, true
}

// handlePlaybackControl forwards a downstream PlaybackControl notify to the
// playback port. When the port is absent the notify is merely acknowledged.
func (a *Acceptor) handlePlaybackControl(
	_ context.Context,
	p *platform,
	req model.Message,
	notify model.Notify,
) (model.Message, bool) {
	if a.playback != nil {
		// PlaybackControl detail is carried in the MANSCDP body; the notify
		// gives us the device id, sn and a status hint. Real command fields
		// are extracted by the adapter when needed.
		a.log.Debug("playback control received",
			"node_id", p.id.String(), "device_id", notify.DeviceID(), "sn", notify.SN())
	} else {
		a.log.Warn("playback control notify dropped: playback port not configured",
			"node_id", p.id.String(), "device_id", notify.DeviceID())
	}
	resp, err := buildResponse(200, "OK", req, nil, "")
	if err != nil {
		a.log.Warn("cannot answer a PlaybackControl",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	return resp, true
}

// handleDeviceInfo answers a DeviceInfo query with the static facts the
// device registry holds about the requester: vendor, model, firmware.
// A query without an SN is refused silently (the answer could not be
// correlated anyway).
func (a *Acceptor) handleDeviceInfo(
	ctx context.Context,
	p *platform,
	req model.Message,
	notify model.Notify,
) (model.Message, bool) {
	if !notify.HasSN() {
		a.log.Warn("ignoring a DeviceInfo query without a sequence number",
			"node_id", p.id.String(), "device_id", notify.DeviceID())
		return model.Message{}, false
	}
	_ = ctx
	_ = p
	item, itemErr := model.NewDeviceInfoItem(model.DeviceInfoItemParams{
		DeviceID:     notify.DeviceID(),
		Name:         "GB28181 Simulator Device",
		Manufacturer: "Simulator",
		Model:        "virtual-camera-1",
		Firmware:     "1.0.0",
	})
	if itemErr != nil {
		a.log.Warn("cannot build a device info item",
			"node_id", p.id.String(), "error", itemErr.Error())
		return model.Message{}, false
	}
	info := model.DeviceInfoResponse{
		DeviceID: notify.DeviceID(),
		SN:       notify.SN(),
		SumNum:   1,
		Items:    []model.DeviceInfoItem{item},
	}
	body, err := a.manscdp.MarshalDeviceInfoResponse(info)
	if err != nil {
		a.log.Warn("cannot render a device info answer",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	resp, err := buildResponse(200, "OK", req, []model.Header{
		model.NewHeader("Content-Type", contentTypeMANSCDP),
	}, body)
	if err != nil {
		a.log.Warn("cannot answer a DeviceInfo query",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	a.log.Debug("device-info query answered", "node_id", p.id.String(),
		"device_id", notify.DeviceID(), "sn", notify.SN(), "items", len(info.Items))
	return resp, true
}

// handleRecordInfo answers a RecordInfo query with the synthetic record
// list the device reports for the requested time range. An empty StartTime
// or EndTime in the query is treated as "no bound" on that side.
func (a *Acceptor) handleRecordInfo(
	_ context.Context,
	p *platform,
	req model.Message,
	notify model.Notify,
) (model.Message, bool) {
	if !notify.HasSN() {
		a.log.Warn("ignoring a RecordInfo query without a sequence number",
			"node_id", p.id.String(), "device_id", notify.DeviceID())
		return model.Message{}, false
	}
	query, qErr := a.manscdp.DecodeRecordInfoQuery(req.Body())
	if qErr != nil {
		a.log.Warn("cannot read a record info query",
			"node_id", p.id.String(), "error", qErr.Error())
		return model.Message{}, false
	}
	all := []model.RecordInfoItem{model.NewRecordInfoItem(model.RecordInfoItemParams{
		Name:      "recording-1",
		DeviceID:  notify.DeviceID(),
		StartTime: "20260925T000000",
		EndTime:   "20260925T010000",
		FilePath:  "/records/" + notify.DeviceID() + "/20260925T000000.mp4",
	})}
	items := all
	if query.StartTime != "" {
		filtered := make([]model.RecordInfoItem, 0, len(items))
		for _, it := range items {
			if it.EndTime() > query.StartTime {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}
	if query.EndTime != "" {
		filtered := make([]model.RecordInfoItem, 0, len(items))
		for _, it := range items {
			if it.StartTime() <= query.EndTime {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}
	respBody := model.RecordInfoResponse{
		DeviceID: notify.DeviceID(),
		SN:       notify.SN(),
		SumNum:   len(items),
		Items:    items,
	}
	body, err := a.manscdp.MarshalRecordInfoResponse(respBody)
	if err != nil {
		a.log.Warn("cannot render a record info answer",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	resp, err := buildResponse(200, "OK", req, []model.Header{
		model.NewHeader("Content-Type", contentTypeMANSCDP),
	}, body)
	if err != nil {
		a.log.Warn("cannot answer a RecordInfo query",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	a.log.Debug("record-info query answered", "node_id", p.id.String(),
		"device_id", notify.DeviceID(), "sn", notify.SN(), "count", len(items))
	return resp, true
}

// handleAlarm acknowledges an Alarm notify a downstream pushed. The event is
// appended to the serving node's in-memory alarm log when a node registry is
// attached, so the runtime API and Web UI can observe what arrived on the
// wire; without one it is only logged.
func (a *Acceptor) handleAlarm(
	ctx context.Context,
	p *platform,
	req model.Message,
	_ model.Notify,
) (model.Message, bool) {
	alarm, err := a.manscdp.DecodeAlarmNotify(req.Body())
	if err != nil {
		a.log.Warn("cannot read an alarm notify",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	if a.registry != nil {
		snap, snapErr := model.NewAlarmSnapshot(
			fmt.Sprintf("%s-%d", alarm.DeviceID(), alarm.SN()),
			alarm.DeviceID(), alarm.ChannelID(),
			alarm.AlarmPriority(), alarm.AlarmMethod(),
			alarm.Description(), alarm.EventTime(),
		)
		if snapErr == nil {
			if _, mErr := a.registry.MutateProfile(ctx, p.id, func(np model.NodeProfile) (model.NodeProfile, error) {
				return np.AppendAlarm(snap)
			}); mErr != nil {
				a.log.Debug("alarm snapshot not stored",
					"node_id", p.id.String(), "error", mErr.Error())
			}
		}
	}
	ack := model.NewAlarmAck(alarm.DeviceID(), alarm.SN(), "OK")
	body, err := a.manscdp.MarshalAlarmAck(ack)
	if err != nil {
		a.log.Warn("cannot render an alarm ack",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	a.log.Debug("alarm received", "node_id", p.id.String(),
		"device_id", alarm.DeviceID(), "event", alarm.EventType(),
		"time", alarm.EventTime())
	resp, err := buildResponse(200, "OK", req, []model.Header{
		model.NewHeader("Content-Type", contentTypeMANSCDP),
	}, body)
	if err != nil {
		a.log.Warn("cannot answer an Alarm notify",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	// Forward this alarm to every alarm subscriber: the upstream peer
	// subscribed to alarm events is waiting for the live alarm stream.
	a.notifyAlarmSubscribers(ctx, p, req.Body())
	return resp, true
}

// is2022 reports whether the device identified by deviceID is registered on p
// and negotiated GB/T 28181-2022 semantics. Unknown or 2016 devices return
// false, which is the gate used by the 2022 command handlers.
func (a *Acceptor) is2022(ctx context.Context, p *platform, deviceID string) bool {
	dev, ok := a.devices.Lookup(ctx, p.id, deviceID)
	return ok && dev.Is2022()
}

// handleHomePosition answers a GB/T 28181-2022 HomePosition command.
// A query returns the profile guard position; a set validates coordinates
// and updates the node profile. A 2016 peer never reaches this handler
// because handleMessage gates it.
func (a *Acceptor) handleHomePosition(
	ctx context.Context,
	p *platform,
	req model.Message,
	notify model.Notify,
) (model.Message, bool) {
	// Query takes precedence: if the body can be parsed as a query the peer
	// asked for the guard position.
	if query, qErr := a.manscdp.DecodeHomePositionQuery(req.Body()); qErr == nil {
		body, mErr := a.marshalHomePositionQuery(p, query, notify.SN())
		if mErr != nil {
			return model.Message{}, false
		}
		resp, err := buildResponse(200, "OK", req, []model.Header{
			model.NewHeader("Content-Type", contentTypeMANSCDP),
		}, body)
		if err != nil {
			return model.Message{}, false
		}
		a.log.Debug("home-position query answered", "node_id", p.id.String(),
			"device_id", notify.DeviceID(), "sn", notify.SN())
		return resp, true
	}
	// Otherwise treat it as a set command.
	set, sErr := a.manscdp.DecodeHomePositionSet(req.Body())
	if sErr != nil {
		a.log.Warn("cannot read a HomePosition set",
			"node_id", p.id.String(), "error", sErr.Error())
		return model.Message{}, false
	}
	// Validate coordinates: required fields must be present and non-empty.
	if set.Longitude == "" || set.Latitude == "" {
		errBody, _ := a.manscdp.MarshalHomePositionResponse(model.HomePositionResponse{
			DeviceID: set.DeviceID,
			SN:       notify.SN(),
			Result:   "ERROR",
		}, notify.SN())
		resp, err := buildResponse(200, "OK", req, []model.Header{
			model.NewHeader("Content-Type", contentTypeMANSCDP),
		}, errBody)
		if err != nil {
			return model.Message{}, false
		}
		return resp, true
	}
	if a.registry != nil {
		if _, err := a.registry.MutateProfile(ctx, p.id, func(np model.NodeProfile) (model.NodeProfile, error) {
			pos, posErr := model.NewHomePosition(model.HomePositionParams{
				DeviceID:  set.DeviceID,
				Longitude: set.Longitude,
				Latitude:  set.Latitude,
				Altitude:  set.Altitude,
				Azimuth:   set.Azimuth,
			})
			if posErr != nil {
				return np, posErr
			}
			return np.WithHomePosition(pos)
		}); err != nil {
			a.log.Warn("cannot store a guard position",
				"node_id", p.id.String(), "error", err.Error())
		}
	}
	okBody, _ := a.manscdp.MarshalHomePositionResponse(model.HomePositionResponse{
		DeviceID: set.DeviceID,
		SN:       notify.SN(),
		Result:   "OK",
	}, notify.SN())
	resp, err := buildResponse(200, "OK", req, []model.Header{
		model.NewHeader("Content-Type", contentTypeMANSCDP),
	}, okBody)
	if err != nil {
		return model.Message{}, false
	}
	return resp, true
}

// marshalHomePositionQuery renders a HomePosition query answer from the
// profile guard position when one is configured; otherwise it answers with
// an empty response body.
func (a *Acceptor) marshalHomePositionQuery(
	p *platform,
	query model.HomePositionQuery,
	sn uint32,
) (string, error) {
	if a.registry != nil {
		if node, ok := a.registry.Get(a.ctx, p.id); ok {
			if pos, hasPos := node.Profile().HomePosition(); hasPos {
				resp := model.HomePositionResponse{
					DeviceID:  pos.DeviceID(),
					Longitude: pos.Longitude(),
					Latitude:  pos.Latitude(),
					Altitude:  pos.Altitude(),
					Azimuth:   pos.Azimuth(),
				}
				return a.manscdp.MarshalHomePositionResponse(resp, sn)
			}
		}
	}
	// No position configured — return a minimal OK body.
	return a.manscdp.MarshalHomePositionResponse(model.HomePositionResponse{
		DeviceID: query.DeviceID,
		SN:       sn,
		Result:   "OK",
	}, sn)
}

// handleCruiseTrackList answers a GB/T 28181-2022 CruiseTrackList query with
// the cruise tracks configured on the node profile.
func (a *Acceptor) handleCruiseTrackList(
	ctx context.Context,
	p *platform,
	req model.Message,
	notify model.Notify,
) (model.Message, bool) {
	_, err := a.manscdp.DecodeCruiseTrackListQuery(req.Body())
	if err != nil {
		a.log.Warn("cannot read a CruiseTrackList query",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	items := []model.CruiseTrack{}
	if a.registry != nil {
		if node, ok := a.registry.Get(ctx, p.id); ok {
			items = node.Profile().CruiseTracks()
			if items == nil {
				items = []model.CruiseTrack{}
			}
		}
	}
	resp := model.CruiseTrackListResponse{
		DeviceID: notify.DeviceID(),
		SN:       notify.SN(),
		Items:    items,
	}
	body, err := a.manscdp.MarshalCruiseTrackListResponse(resp, notify.SN())
	if err != nil {
		a.log.Warn("cannot render a CruiseTrackList response",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	a.log.Debug("answering a cruise track list query", "node_id", p.id.String(),
		"device_id", notify.DeviceID(), "tracks", len(items))
	respMsg, err := buildResponse(200, "OK", req, []model.Header{
		model.NewHeader("Content-Type", contentTypeMANSCDP),
	}, body)
	if err != nil {
		return model.Message{}, false
	}
	return respMsg, true
}

// handleSnapShot acknowledges a GB/T 28181-2022 SnapShot capture command.
// The simulator does not produce a JPEG payload; it records the capture
// event on the node profile and answers OK.
func (a *Acceptor) handleSnapShot(
	ctx context.Context,
	p *platform,
	req model.Message,
	notify model.Notify,
) (model.Message, bool) {
	cmd, err := a.manscdp.DecodeSnapShotCommand(req.Body())
	if err != nil {
		a.log.Warn("cannot read a SnapShot command",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	if a.registry != nil {
		if _, err := a.registry.MutateProfile(ctx, p.id, func(np model.NodeProfile) (model.NodeProfile, error) {
			rec, recErr := model.NewSnapShotRecord(model.SnapShotRecordParams{
				DeviceID:  cmd.DeviceID,
				ChannelID: cmd.ChannelID,
			})
			if recErr != nil {
				return np, recErr
			}
			return np.AppendSnapShot(rec)
		}); err != nil {
			a.log.Warn("cannot record a snapshot",
				"node_id", p.id.String(), "error", err.Error())
		}
	}
	a.log.Debug("snapshot captured", "node_id", p.id.String(),
		"device_id", cmd.DeviceID, "channel", cmd.ChannelID)
	body, err := a.manscdp.MarshalSnapShotResponse(model.SnapShotResponse{
		DeviceID: cmd.DeviceID,
		SN:       notify.SN(),
		Result:   "OK",
	}, notify.SN())
	if err != nil {
		a.log.Warn("cannot render a SnapShot response",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	resp, err := buildResponse(200, "OK", req, []model.Header{
		model.NewHeader("Content-Type", contentTypeMANSCDP),
	}, body)
	if err != nil {
		return model.Message{}, false
	}
	return resp, true
}

// handlePresetQuery answers a PresetQuery with the preset list the node's
// profile carries. Without a node registry (or for a node the registry does
// not know) the answer is an empty list, which is still a valid answer for
// a device that exposes no presets.
func (a *Acceptor) handlePresetQuery(
	ctx context.Context,
	p *platform,
	req model.Message,
	notify model.Notify,
) (model.Message, bool) {
	if !notify.HasSN() {
		a.log.Warn("ignoring a PresetQuery without a sequence number",
			"node_id", p.id.String(), "device_id", notify.DeviceID())
		return model.Message{}, false
	}
	presets := []model.PresetItem{}
	if a.registry != nil {
		if node, ok := a.registry.Get(ctx, p.id); ok {
			presets = node.Profile().Presets()
			if presets == nil {
				presets = []model.PresetItem{}
			}
		}
	}
	list, err := model.NewPresetListResponse(notify.DeviceID(), notify.SN(), presets)
	if err != nil {
		a.log.Warn("cannot build a preset list answer",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	body, err := a.manscdp.MarshalPresetList(list)
	if err != nil {
		a.log.Warn("cannot render a preset list answer",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	a.log.Debug("answering a preset query", "node_id", p.id.String(),
		"sn", notify.SN(), "sum", list.SumNum)
	resp, err := buildResponse(200, "OK", req, []model.Header{
		model.NewHeader("Content-Type", contentTypeMANSCDP),
	}, body)
	if err != nil {
		a.log.Warn("cannot answer a PresetQuery",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	return resp, true
}

// handleDeviceControl acknowledges a PTZ DeviceControl command. The
// simulator does not move cameras; it only logs the motion requested.
// A 2022 TeleBoot element inside the control body is treated as a reboot
// request and logged accordingly.
func (a *Acceptor) handleDeviceControl(
	_ context.Context,
	p *platform,
	req model.Message,
	_ model.Notify,
) (model.Message, bool) {
	// TeleBoot is a 2022 control element with its own XML tag. Detect it
	// before the generic PTZ decoder so we can log the reboot separately.
	if strings.Contains(req.Body(), "<TeleBoot>") {
		a.log.Debug("TeleBoot reboot requested", "node_id", p.id.String())
		resp, err := buildResponse(200, "OK", req, nil, "")
		if err != nil {
			a.log.Warn("cannot answer a TeleBoot",
				"node_id", p.id.String(), "error", err.Error())
			return model.Message{}, false
		}
		return resp, true
	}
	control, err := a.manscdp.DecodePTZControl(req.Body())
	if err != nil {
		a.log.Warn("cannot read a device control",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	a.log.Debug("PTZ control received", "node_id", p.id.String(),
		"device_id", control.DeviceID)
	resp, err := buildResponse(200, "OK", req, nil, "")
	if err != nil {
		a.log.Warn("cannot answer a DeviceControl",
			"node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	return resp, true
}
