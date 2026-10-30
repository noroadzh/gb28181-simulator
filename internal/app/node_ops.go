package app

// NodeService side of device-control-coverage: the operations the HTTP layer
// exposes. Each one either reads/updates the node locally (for a device) or
// forwards the same command to the downstream that owns the target.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// ErrOpsUnsupported marks an operation this node cannot serve.
var ErrOpsUnsupported = errors.New("app: operation unsupported")

// DeviceStatus reports whether the node is online.
func (s *NodeService) DeviceStatus(ctx context.Context, id model.NodeID) (bool, error) {
	node, ok := s.registry.Get(ctx, id)
	if !ok {
		return false, model.ErrUnknownNode
	}
	switch node.Status() {
	case model.StatusOnline, model.StatusRegistered:
		return true, nil
	default:
		return false, nil
	}
}

// Alarms returns the alarms the node recorded in [start, end].
func (s *NodeService) Alarms(ctx context.Context, id model.NodeID, start, end time.Time) ([]model.AlarmSnapshot, error) {
	node, ok := s.registry.Get(ctx, id)
	if !ok {
		return nil, model.ErrUnknownNode
	}
	all := node.Profile().Alarms()
	out := make([]model.AlarmSnapshot, 0, len(all))
	for _, snap := range all {
		if !start.IsZero() || !end.IsZero() {
			at, err := time.ParseInLocation("2006-01-02T15:04:05", snap.EventTime(), time.Local)
			if err != nil {
				// An alarm with an unreadable timestamp is still an alarm;
				// a window query simply cannot place it.
				continue
			}
			if !start.IsZero() && at.Before(start) {
				continue
			}
			if !end.IsZero() && at.After(end) {
				continue
			}
		}
		out = append(out, snap)
	}
	return out, nil
}

// SetAlarmMode arms or disarms the node.
func (s *NodeService) SetAlarmMode(ctx context.Context, id model.NodeID, armed bool) error {
	if _, ok := s.registry.Get(ctx, id); !ok {
		return model.ErrUnknownNode
	}
	_, err := s.registry.MutateProfile(ctx, id, func(p model.NodeProfile) (model.NodeProfile, error) {
		return p.WithAlarmArmed(armed), nil
	})
	return err
}

// AlarmMode reports the arming state.
func (s *NodeService) AlarmMode(ctx context.Context, id model.NodeID) (armed bool, set bool, err error) {
	node, ok := s.registry.Get(ctx, id)
	if !ok {
		return false, false, model.ErrUnknownNode
	}
	armed, set = node.Profile().AlarmArmed()
	return armed, set, nil
}

// Config returns the node's basic configuration block.
func (s *NodeService) Config(ctx context.Context, id model.NodeID) (model.BasicConfig, error) {
	node, ok := s.registry.Get(ctx, id)
	if !ok {
		return model.BasicConfig{}, model.ErrUnknownNode
	}
	prof := node.Profile()
	cfg := model.BasicConfig{
		Name: id.String(), Model: "simulator", Firmware: "1.0.0",
		Manufacturer: prof.Vendor(), ChannelCount: len(prof.Channels()),
	}
	if reg, wants := prof.Registration(); wants {
		cfg.HeartbeatInterval = int(reg.HeartbeatInterval().Seconds())
		cfg.HeartbeatTimeout = int(reg.HeartbeatTimeout().Seconds())
	}
	if hb, ok := prof.HeartbeatOverride(); ok {
		cfg.HeartbeatInterval = int(hb.Seconds())
	}
	return cfg, nil
}

// SetConfig applies the writable parts of a configuration block.
func (s *NodeService) SetConfig(ctx context.Context, id model.NodeID, cfg model.BasicConfig) error {
	if cfg.HeartbeatInterval <= 0 {
		return fmt.Errorf("%w: heartbeat interval must be positive", ErrOpsUnsupported)
	}
	_, err := s.registry.MutateProfile(ctx, id, func(p model.NodeProfile) (model.NodeProfile, error) {
		return p.WithHeartbeatOverride(time.Duration(cfg.HeartbeatInterval) * time.Second), nil
	})
	return err
}

// Upgrade records the firmware a platform asked for and answers OK.
func (s *NodeService) Upgrade(ctx context.Context, id model.NodeID, version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return fmt.Errorf("%w: empty firmware version", ErrOpsUnsupported)
	}
	return s.recordOps(ctx, id, func(p model.NodeProfile) (model.NodeProfile, error) {
		return p.WithUpgradeTarget(version), nil
	})
}

// Reboot records a reboot request; the node keeps serving.
func (s *NodeService) Reboot(ctx context.Context, id model.NodeID) error {
	now := s.clock.Now()
	return s.recordOps(ctx, id, func(p model.NodeProfile) (model.NodeProfile, error) {
		return p.WithReboot(now), nil
	})
}

func (s *NodeService) recordOps(ctx context.Context, id model.NodeID,
	fn func(model.NodeProfile) (model.NodeProfile, error)) error {
	if _, ok := s.registry.Get(ctx, id); !ok {
		return model.ErrUnknownNode
	}
	if _, err := s.registry.MutateProfile(ctx, id, fn); err != nil {
		return err
	}
	s.log.Info("operations command applied", "node_id", id.String())
	return nil
}

// StartDownload pulls a recorded window from the device that owns the
// channel and writes it into the recording library under a per-download
// channel key. The device is asked with an `s=Download` INVITE whose SDP
// names the port the platform will receive on.
func (s *NodeService) StartDownload(ctx context.Context, id model.NodeID, channelID string, start, end time.Time) (string, error) {
	if s.recordings == nil {
		return "", fmt.Errorf("%w: recording library is not wired", port.ErrRecordingUnsupported)
	}
	if s.liveDeizer == nil {
		return "", fmt.Errorf("%w: no RTP deizer wired", port.ErrLiveUnsupported)
	}
	if !start.Before(end) {
		return "", fmt.Errorf("%w: download window %s..%s is not a range", ErrOpsUnsupported, start, end)
	}
	node, ok := s.registry.Get(ctx, id)
	if !ok {
		return "", model.ErrUnknownNode
	}
	target, addr, err := s.liveTarget(ctx, node, channelID)
	if err != nil {
		return "", err
	}
	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		return "", fmt.Errorf("app: open download port: %w", err)
	}
	port := conn.LocalAddr().(*net.UDPAddr).Port
	body, err := model.PlaybackSDPBytes(model.PlaybackSDP{
		DeviceID: channelID, SN: fmt.Sprintf("%d", time.Now().UnixNano()),
		ConnIP: hostOf(node.Profile().Addr()), RTPPort: port,
		Start: start, End: end, Mode: "recvonly", Session: "Download",
	})
	if err != nil {
		_ = conn.Close()
		return "", err
	}
	callID, err := s.acceptor.InviteDownload(ctx, id, target, addr, channelID, body)
	if err != nil {
		_ = conn.Close()
		return "", err
	}
	go s.receiveDownload(conn, id, channelID, end)
	return callID, nil
}

// receiveDownload writes the downloaded PS frames into the library. The key
// carries a download: prefix so a download never mixes with a live recording
// of the same channel.
func (s *NodeService) receiveDownload(conn net.PacketConn, id model.NodeID, channelID string, end time.Time) {
	defer func() { _ = conn.Close() }()
	deizer := s.liveDeizer(0)
	buf := make([]byte, 65536)
	key := "download:" + channelID
	for {
		if time.Now().After(end) {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		pkt, perr := model.ParseRTPPacket(buf[:n])
		if perr != nil {
			continue
		}
		ps, derr := deizer.Write(pkt)
		if derr != nil || ps.Payload == nil {
			continue
		}
		if aerr := s.recordings.Append(context.Background(), id, key, ps); aerr != nil {
			s.log.Warn("download write failed", "node_id", id.String(), "channel", channelID, "error", aerr.Error())
			return
		}
	}
}
