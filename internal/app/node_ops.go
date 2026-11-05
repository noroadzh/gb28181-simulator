package app

// NodeService side of device-control-coverage: the operations the HTTP layer
// exposes. Each one either reads/updates the node locally (for a device) or
// forwards the same command to the downstream that owns the target.

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/noroadzh/gb28181-simulator/internal/domain/model"
	"github.com/noroadzh/gb28181-simulator/internal/domain/port"
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
// channel key. transport is "udp" (default) or "tcp" (GB/T 28181 附录 D).
func (s *NodeService) StartDownload(ctx context.Context, id model.NodeID, channelID string, start, end time.Time, transport string) (string, error) {
	if s.recordings == nil {
		return "", fmt.Errorf("%w: recording library is not wired", port.ErrRecordingUnsupported)
	}
	if s.liveDeizer == nil {
		return "", fmt.Errorf("%w: no RTP deizer wired", port.ErrLiveUnsupported)
	}
	if !start.Before(end) {
		return "", fmt.Errorf("%w: download window %s..%s is not a range", ErrOpsUnsupported, start, end)
	}
	transport = strings.ToLower(strings.TrimSpace(transport))
	switch transport {
	case "", "udp":
		transport = "udp"
	case "tcp":
	default:
		return "", fmt.Errorf("%w: download transport %q is not udp or tcp", ErrOpsUnsupported, transport)
	}
	node, ok := s.registry.Get(ctx, id)
	if !ok {
		return "", model.ErrUnknownNode
	}
	target, addr, err := s.liveTarget(ctx, node, channelID)
	if err != nil {
		return "", err
	}

	localIP := hostOf(node.Profile().Addr())
	offer := model.PlaybackSDP{
		DeviceID: channelID, SN: fmt.Sprintf("%d", time.Now().UnixNano()),
		ConnIP: localIP, Start: start, End: end, Mode: "recvonly", Session: "Download",
	}

	var (
		udp net.PacketConn
		tcp net.Listener
	)
	if transport == "tcp" {
		ln, lerr := net.Listen("tcp", "0.0.0.0:0")
		if lerr != nil {
			return "", fmt.Errorf("app: listen for tcp download media: %w", lerr)
		}
		tcp = ln
		offer.RTPPort = ln.Addr().(*net.TCPAddr).Port
		offer.Proto = "TCP/RTP/AVP"
		offer.Setup = "passive"
	} else {
		conn, lerr := net.ListenPacket("udp4", "0.0.0.0:0")
		if lerr != nil {
			return "", fmt.Errorf("app: open download port: %w", lerr)
		}
		udp = conn
		offer.RTPPort = conn.LocalAddr().(*net.UDPAddr).Port
	}

	body, err := model.PlaybackSDPBytes(offer)
	if err != nil {
		if udp != nil {
			_ = udp.Close()
		}
		if tcp != nil {
			_ = tcp.Close()
		}
		return "", err
	}
	callID, err := s.acceptor.InviteDownload(ctx, id, target, addr, channelID, body)
	if err != nil {
		if udp != nil {
			_ = udp.Close()
		}
		if tcp != nil {
			_ = tcp.Close()
		}
		return "", err
	}
	if transport == "tcp" {
		go s.receiveDownloadTCP(tcp, id, channelID, end)
	} else {
		go s.receiveDownloadUDP(udp, id, channelID, end)
	}
	s.log.Info("download started",
		"node_id", id.String(), "channel", channelID,
		"transport", transport, "port", offer.RTPPort, "call_id", callID)
	return callID, nil
}

// receiveDownloadUDP writes downloaded PS frames from a UDP RTP port.
func (s *NodeService) receiveDownloadUDP(conn net.PacketConn, id model.NodeID, channelID string, end time.Time) {
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
		if !s.appendDownloadFrame(deizer, id, key, channelID, buf[:n]) {
			return
		}
	}
}

// receiveDownloadTCP accepts one TCP media connection and reads length-
// prefixed RTP packets (附录 D), writing PS frames into the library.
func (s *NodeService) receiveDownloadTCP(ln net.Listener, id model.NodeID, channelID string, end time.Time) {
	defer func() { _ = ln.Close() }()
	_ = ln.(*net.TCPListener).SetDeadline(end)
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	deizer := s.liveDeizer(0)
	key := "download:" + channelID
	var prefix [2]byte
	for {
		if time.Now().After(end) {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		if _, err := io.ReadFull(conn, prefix[:]); err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		size := int(binary.BigEndian.Uint16(prefix[:]))
		if size == 0 {
			continue
		}
		payload := make([]byte, size)
		if _, err := io.ReadFull(conn, payload); err != nil {
			return
		}
		if !s.appendDownloadFrame(deizer, id, key, channelID, payload) {
			return
		}
	}
}

// appendDownloadFrame returns false when the library write fails and the
// receiver should stop.
func (s *NodeService) appendDownloadFrame(deizer port.RTPDeizer, id model.NodeID, key, channelID string, datagram []byte) bool {
	pkt, perr := model.ParseRTPPacket(datagram)
	if perr != nil {
		return true
	}
	ps, derr := deizer.Write(pkt)
	if derr != nil || ps.Payload == nil {
		return true
	}
	if aerr := s.recordings.Append(context.Background(), id, key, ps); aerr != nil {
		s.log.Warn("download write failed", "node_id", id.String(), "channel", channelID, "error", aerr.Error())
		return false
	}
	return true
}
