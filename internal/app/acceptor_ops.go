package app

// Handlers for the operations commands added by device-control-coverage:
// DeviceStatus, Broadcast, alarm query, ConfigDownload, DeviceUpgrade and the
// DeviceControl elements GuardCmd/TeleBoot.
//
// A device answers about itself. A platform answers about itself and forwards
// anything addressed to a downstream in its device table.

import (
	"context"
	"net"
	"strings"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// opsForwardWait bounds a forwarded MESSAGE transaction.
const opsForwardWait = 5 * time.Second

// answerMANSCDP wraps a rendered body in a 200 OK.
func (a *Acceptor) answerMANSCDP(req model.Message, body string) (model.Message, bool) {
	resp, err := buildResponse(200, "OK", req, []model.Header{
		model.NewHeader("Content-Type", contentTypeMANSCDP),
	}, body)
	if err != nil {
		a.log.Warn("cannot answer an operations command", "error", err.Error())
		return model.Message{}, false
	}
	return resp, true
}

// handleDeviceStatus answers a DeviceStatus query.
func (a *Acceptor) handleDeviceStatus(ctx context.Context, p *platform, req model.Message, notify model.Notify, peer string) (model.Message, bool) {
	query, err := a.manscdp.DecodeDeviceStatusQuery(req.Body())
	if err != nil {
		a.log.Warn("cannot read a DeviceStatus query", "node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	target := strings.TrimSpace(query.DeviceID)
	if target == "" {
		target = notify.DeviceID()
	}
	if !p.isDevice && target != "" && target != p.id.String() {
		return a.forwardOps(p, req, target, peer, func() (string, error) {
			return a.manscdp.MarshalDeviceStatusResponse(model.DeviceStatusResponse{
				SN: query.SN, DeviceID: target, Result: model.ResultError,
				Online: "OFFLINE", Status: "ERROR",
			})
		})
	}
	result, online, status := model.DeviceStatusResult(true)
	body, err := a.manscdp.MarshalDeviceStatusResponse(model.DeviceStatusResponse{
		SN: query.SN, DeviceID: p.id.String(), Result: result, Online: online, Status: status,
		DeviceTime: time.Now().Format("2006-01-02T15:04:05"),
	})
	if err != nil {
		return model.Message{}, false
	}
	return a.answerMANSCDP(req, body)
}

// handleBroadcast answers a Broadcast command and, on a device, starts the
// audio dialog the standard puts on the device's side of it.
func (a *Acceptor) handleBroadcast(ctx context.Context, p *platform, req model.Message, notify model.Notify, peer string) (model.Message, bool) {
	cmd, err := a.manscdp.DecodeBroadcast(req.Body())
	if err != nil {
		a.log.Warn("cannot read a Broadcast", "node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	target := strings.TrimSpace(cmd.TargetID)
	if target == "" {
		target = notify.DeviceID()
	}
	if !p.isDevice && target != "" && target != p.id.String() {
		return a.forwardOps(p, req, target, peer, func() (string, error) {
			return a.manscdp.MarshalBroadcastAck(model.BroadcastAck{
				SN: cmd.SN, TargetID: target, Result: model.ResultError,
			})
		})
	}
	body, err := a.manscdp.MarshalBroadcastAck(model.BroadcastAck{
		SN: cmd.SN, TargetID: target, Result: model.ResultOK,
	})
	if err != nil {
		return model.Message{}, false
	}
	resp, ok := a.answerMANSCDP(req, body)
	if ok && p.isDevice {
		// The ack goes back first; the audio dialog is the device's next
		// step and must not block the read loop that answers it.
		go a.startBroadcastAudio(p, cmd)
	}
	return resp, ok
}

// startBroadcastAudio opens the device→platform audio dialog a broadcast
// implies. The simulator has no microphone: the dialog is established and
// held, which is what an unconfigured device looks like on the wire.
func (a *Acceptor) startBroadcastAudio(p *platform, cmd model.BroadcastCommand) {
	host := "127.0.0.1"
	if h, _, err := splitHostPort(p.tr.LocalAddr()); err == nil {
		host = h
	}
	// The dialog goes to the platform the device is registered with: that
	// is the address a broadcast source lives at in this simulator.
	addr := ""
	if node, ok := a.registry.Get(context.Background(), p.id); ok {
		if reg, wants := node.Profile().Registration(); wants {
			addr = reg.Server()
		}
	}
	if addr == "" {
		a.log.Warn("broadcast: device has no upstream to call", "node_id", p.id.String())
		return
	}
	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		a.log.Warn("broadcast: cannot open an audio port", "node_id", p.id.String(), "error", err.Error())
		return
	}
	rtpPort := conn.LocalAddr().(*net.UDPAddr).Port
	body := buildTalkOffer(host, rtpPort)
	ctx, cancel := context.WithTimeout(context.Background(), opsForwardWait)
	defer cancel()
	callID, _, err := a.invite(ctx, p.id, cmd.SourceID, addr, body, port.ErrTalkUnsupported)
	if err != nil {
		_ = conn.Close()
		a.log.Warn("broadcast: audio INVITE failed",
			"node_id", p.id.String(), "target", cmd.SourceID, "error", err.Error())
		return
	}
	a.deviceTalksMu.Lock()
	if a.deviceTalks == nil {
		a.deviceTalks = map[string]net.PacketConn{}
	}
	a.deviceTalks[callID] = conn
	a.deviceTalksMu.Unlock()
	a.log.Info("broadcast audio started", "node_id", p.id.String(),
		"target", cmd.SourceID, "call_id", callID)
}

// handleAlarmQuery answers an alarm query from the node's alarm log.
func (a *Acceptor) handleAlarmQuery(ctx context.Context, p *platform, req model.Message, notify model.Notify, peer string) (model.Message, bool) {
	query, err := a.manscdp.DecodeAlarmQuery(req.Body())
	if err != nil {
		a.log.Warn("cannot read an Alarm query", "node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	target := strings.TrimSpace(query.DeviceID)
	if target == "" {
		target = notify.DeviceID()
	}
	if !p.isDevice && target != "" && target != p.id.String() {
		return a.forwardOps(p, req, target, peer, func() (string, error) {
			return a.manscdp.MarshalAlarmQueryResponse(model.AlarmQueryResponse{SN: query.SN, DeviceID: target})
		})
	}

	start, end := parseRecordBound(query.StartTime), parseRecordBound(query.EndTime)
	items := make([]model.AlarmRecord, 0)
	if node, ok := a.registry.Get(ctx, p.id); ok {
		for _, snap := range node.Profile().Alarms() {
			at := parseRecordBound(snap.EventTime())
			if !start.IsZero() && at.Before(start) {
				continue
			}
			if !end.IsZero() && (at.IsZero() || at.After(end)) {
				continue
			}
			if query.StartPriority > 0 && snap.Priority() < query.StartPriority {
				continue
			}
			if query.EndPriority > 0 && snap.Priority() > query.EndPriority {
				continue
			}
			items = append(items, model.AlarmRecord{
				DeviceID: snap.DeviceID(), AlarmTime: snap.EventTime(),
				Priority: snap.Priority(), Method: snap.Method(), Description: snap.Description(),
			})
		}
	}
	body, err := a.manscdp.MarshalAlarmQueryResponse(model.AlarmQueryResponse{
		SN: query.SN, DeviceID: p.id.String(), SumNum: len(items), Items: items,
	})
	if err != nil {
		return model.Message{}, false
	}
	return a.answerMANSCDP(req, body)
}

// handleConfigDownload reads or writes the basic configuration block.
func (a *Acceptor) handleConfigDownload(ctx context.Context, p *platform, req model.Message, notify model.Notify, peer string) (model.Message, bool) {
	if notify.IsControl() {
		set, err := a.manscdp.DecodeConfigSet(req.Body())
		if err != nil {
			a.log.Warn("cannot read a config write", "node_id", p.id.String(), "error", err.Error())
			return model.Message{}, false
		}
		if !p.isDevice && set.DeviceID != "" && set.DeviceID != p.id.String() {
			return a.forwardOps(p, req, set.DeviceID, peer, func() (string, error) {
				return a.manscdp.MarshalConfigResponse(model.ConfigResponse{
					SN: set.SN, DeviceID: set.DeviceID, ConfigType: set.ConfigType, Result: model.ResultError,
				})
			})
		}
		result := model.ResultOK
		if !strings.EqualFold(strings.TrimSpace(set.ConfigType), "BasicParam") || set.Basic.HeartbeatInterval <= 0 {
			result = model.ResultError
		} else {
			a.applyHeartbeatOverride(ctx, p.id, time.Duration(set.Basic.HeartbeatInterval)*time.Second)
		}
		body, err := a.manscdp.MarshalConfigResponse(model.ConfigResponse{
			SN: set.SN, DeviceID: p.id.String(), ConfigType: set.ConfigType, Result: result,
		})
		if err != nil {
			return model.Message{}, false
		}
		return a.answerMANSCDP(req, body)
	}

	query, err := a.manscdp.DecodeConfigQuery(req.Body())
	if err != nil {
		a.log.Warn("cannot read a config query", "node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	if !p.isDevice && query.DeviceID != "" && query.DeviceID != p.id.String() {
		return a.forwardOps(p, req, query.DeviceID, peer, func() (string, error) {
			return a.manscdp.MarshalConfigResponse(model.ConfigResponse{
				SN: query.SN, DeviceID: query.DeviceID, ConfigType: query.ConfigType, Result: model.ResultError,
			})
		})
	}
	if cfgType := strings.TrimSpace(query.ConfigType); cfgType != "" && !strings.EqualFold(cfgType, "BasicParam") {
		body, _ := a.manscdp.MarshalConfigResponse(model.ConfigResponse{
			SN: query.SN, DeviceID: p.id.String(), ConfigType: cfgType, Result: model.ResultError,
		})
		return a.answerMANSCDP(req, body)
	}
	body, err := a.manscdp.MarshalConfigResponse(model.ConfigResponse{
		SN: query.SN, DeviceID: p.id.String(), ConfigType: "BasicParam",
		Result: model.ResultOK, Basic: a.basicConfigOf(ctx, p.id),
	})
	if err != nil {
		return model.Message{}, false
	}
	return a.answerMANSCDP(req, body)
}

// basicConfigOf reads what the node can report about itself.
func (a *Acceptor) basicConfigOf(ctx context.Context, id model.NodeID) model.BasicConfig {
	cfg := model.BasicConfig{Name: id.String(), Model: "simulator", Firmware: "1.0.0"}
	if node, ok := a.registry.Get(ctx, id); ok {
		prof := node.Profile()
		cfg.ChannelCount = len(prof.Channels())
		cfg.Manufacturer = prof.Vendor()
		if reg, wants := prof.Registration(); wants {
			cfg.HeartbeatInterval = int(reg.HeartbeatInterval().Seconds())
			cfg.HeartbeatTimeout = int(reg.HeartbeatTimeout().Seconds())
		}
		if hb, ok := prof.HeartbeatOverride(); ok {
			cfg.HeartbeatInterval = int(hb.Seconds())
		}
	}
	return cfg
}

// applyHeartbeatOverride records a ConfigDownload heartbeat interval. The
// running keepalive loop is not replaced at runtime; the new value is what
// the next start uses.
func (a *Acceptor) applyHeartbeatOverride(ctx context.Context, id model.NodeID, d time.Duration) {
	if a.registry == nil {
		return
	}
	if _, err := a.registry.MutateProfile(ctx, id, func(p model.NodeProfile) (model.NodeProfile, error) {
		return p.WithHeartbeatOverride(d), nil
	}); err != nil {
		a.log.Warn("cannot record the heartbeat interval", "node_id", id.String(), "error", err.Error())
	}
}

// handleUpgrade answers a DeviceUpgrade command and records the target.
func (a *Acceptor) handleUpgrade(ctx context.Context, p *platform, req model.Message, notify model.Notify, peer string) (model.Message, bool) {
	cmd, err := a.manscdp.DecodeUpgrade(req.Body())
	if err != nil {
		a.log.Warn("cannot read an upgrade command", "node_id", p.id.String(), "error", err.Error())
		return model.Message{}, false
	}
	if !p.isDevice && cmd.DeviceID != "" && cmd.DeviceID != p.id.String() {
		return a.forwardOps(p, req, cmd.DeviceID, peer, func() (string, error) {
			return a.manscdp.MarshalOpsAck(model.OpsAck{
				SN: cmd.SN, DeviceID: cmd.DeviceID, Result: model.ResultError,
			}, model.CmdTypeDeviceUpgrade)
		})
	}
	if a.registry != nil {
		if _, err := a.registry.MutateProfile(ctx, p.id, func(prof model.NodeProfile) (model.NodeProfile, error) {
			return prof.WithUpgradeTarget(cmd.Firmware), nil
		}); err != nil {
			a.log.Warn("cannot record the upgrade target", "node_id", p.id.String(), "error", err.Error())
		}
	}
	a.log.Info("upgrade requested", "node_id", p.id.String(), "firmware", cmd.Firmware)
	body, err := a.manscdp.MarshalOpsAck(model.OpsAck{
		SN: cmd.SN, DeviceID: p.id.String(), Result: model.ResultOK,
	}, model.CmdTypeDeviceUpgrade)
	if err != nil {
		return model.Message{}, false
	}
	return a.answerMANSCDP(req, body)
}

// handleGuardCmd applies a GuardCmd DeviceControl element.
func (a *Acceptor) handleGuardCmd(ctx context.Context, p *platform, req model.Message) (model.Message, bool) {
	armed := strings.Contains(req.Body(), "SetGuard")
	if a.registry != nil {
		if _, err := a.registry.MutateProfile(ctx, p.id, func(prof model.NodeProfile) (model.NodeProfile, error) {
			return prof.WithAlarmArmed(armed), nil
		}); err != nil {
			a.log.Warn("cannot record the arming state", "node_id", p.id.String(), "error", err.Error())
		}
	}
	a.log.Info("alarm mode changed", "node_id", p.id.String(), "armed", armed)
	return a.replyOK(req)
}

// handleTeleBoot records a reboot request. The node keeps serving: the
// simulator's reboot is a fact to report, not a reason to drop the caller.
func (a *Acceptor) handleTeleBoot(ctx context.Context, p *platform, req model.Message) (model.Message, bool) {
	if a.registry != nil {
		now := time.Now()
		if _, err := a.registry.MutateProfile(ctx, p.id, func(prof model.NodeProfile) (model.NodeProfile, error) {
			return prof.WithReboot(now), nil
		}); err != nil {
			a.log.Warn("cannot record the reboot", "node_id", p.id.String(), "error", err.Error())
		}
	}
	a.log.Info("reboot requested (TeleBoot)", "node_id", p.id.String())
	return a.replyOK(req)
}

// forwardOps sends a request to a downstream device and answers the peer when
// the device replies. It must not wait here: the acceptor's read loop is the
// only reader of this node's socket, and the device's answer can only arrive
// by that loop. The transaction therefore runs in its own goroutine, which
// sends the upstream answer itself; the caller reports "not answered" so the
// loop does not send a second response.
func (a *Acceptor) forwardOps(p *platform, req model.Message, target, peer string, fallback func() (string, error)) (model.Message, bool) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), opsForwardWait+time.Second)
		defer cancel()
		dev, ok := a.Device(ctx, p.id, target)
		if !ok {
			a.answerOpsAsync(p, req, peer, fallback, target)
			return
		}
		msg, err := buildDeviceControl(p.realm, p.id.String(), target, dev.Addr(), req.Body())
		if err != nil {
			a.answerOpsAsync(p, req, peer, fallback, target)
			return
		}
		key, answer, ok := a.responses.put(p.id, msg)
		if !ok {
			a.answerOpsAsync(p, req, peer, fallback, target)
			return
		}
		if err := p.tr.Send(ctx, msg, dev.Addr()); err != nil {
			a.responses.drop(key)
			a.log.Warn("cannot forward an ops command",
				"node_id", p.id.String(), "target", target, "error", err.Error())
			a.answerOpsAsync(p, req, peer, fallback, target)
			return
		}
		wait := a.controlWait
		if wait <= 0 {
			wait = opsForwardWait
		}
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case resp := <-answer:
			a.sendOpsAnswer(p, req, peer, resp.Body())
		case <-ctx.Done():
			a.responses.drop(key)
			a.answerOpsAsync(p, req, peer, fallback, target)
		case <-timer.C:
			a.responses.drop(key)
			a.log.Warn("forwarded ops command timed out",
				"node_id", p.id.String(), "target", target, "wait", wait)
			a.answerOpsAsync(p, req, peer, fallback, target)
		}
	}()
	return model.Message{}, false
}

// answerOpsAsync renders the fallback answer and sends it to the peer.
func (a *Acceptor) answerOpsAsync(p *platform, req model.Message, peer string, fallback func() (string, error), _ string) {
	body, err := fallback()
	if err != nil {
		return
	}
	a.sendOpsAnswer(p, req, peer, body)
}

// sendOpsAnswer builds the response for req and sends it to peer.
func (a *Acceptor) sendOpsAnswer(p *platform, req model.Message, peer, body string) {
	resp, err := buildResponse(200, "OK", req, []model.Header{
		model.NewHeader("Content-Type", contentTypeMANSCDP),
	}, body)
	if err != nil {
		return
	}
	if out, dst, ok := a.routeOutbound(p, resp, peer); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := p.tr.Send(ctx, out, dst); err != nil {
			a.log.Warn("cannot send an ops answer", "node_id", p.id.String(), "error", err.Error())
		}
	}
}

// replyOK answers a command that carries no body of its own.
func (a *Acceptor) replyOK(req model.Message) (model.Message, bool) {
	resp, err := buildResponse(200, "OK", req, nil, "")
	if err != nil {
		return model.Message{}, false
	}
	return resp, true
}
