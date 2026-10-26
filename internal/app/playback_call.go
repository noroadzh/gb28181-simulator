package app

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// playbackStream is the platform side of one historical playback: what the FLV
// endpoint needs (channel + window) and the dialog it opened.
type playbackStream struct {
	node    model.NodeID
	channel string
	target  string
	addr    string
	start   time.Time
	end     time.Time
	callID  string
	rtp     net.PacketConn
}

// StartPlaybackStream opens a playback dialog for sn and records its window.
// The offer names a platform UDP port for the recording; the FLV endpoint
// serves the node's source for the same window (see the change's design: the
// stream is produced from the source, not by re-parsing RTP).
func (s *NodeService) StartPlaybackStream(ctx context.Context, id model.NodeID, channelID, sn string, start, end time.Time) error {
	node, ok := s.registry.Get(ctx, id)
	if !ok {
		return model.ErrUnknownNode
	}
	if node.ID().Kind() != model.NodeKindDevice {
		return fmt.Errorf("%w: node %s is not a device", port.ErrPlaybackUnsupported, id)
	}
	if s.acceptor == nil {
		return fmt.Errorf("%w: playback is not wired", port.ErrPlaybackUnsupported)
	}
	target, addr, err := s.controlTarget(ctx, node, channelID)
	if err != nil {
		return err
	}

	conn, lerr := net.ListenPacket("udp4", "0.0.0.0:0")
	if lerr != nil {
		return fmt.Errorf("app: allocate playback RTP port: %w", lerr)
	}
	localIP := node.Profile().Addr()
	if host, _, splitErr := net.SplitHostPort(localIP); splitErr == nil {
		localIP = host
	}
	offer := model.PlaybackSDP{
		DeviceID: channelID,
		SN:       sn,
		ConnIP:   localIP,
		RTPPort:  conn.LocalAddr().(*net.UDPAddr).Port,
		Start:    start,
		End:      end,
	}
	body, berr := model.PlaybackSDPBytes(offer)
	if berr != nil {
		_ = conn.Close()
		return fmt.Errorf("%w: %v", port.ErrPlaybackUnsupported, berr)
	}

	callID, ierr := s.acceptor.InvitePlayback(ctx, id, target, addr, body)
	if ierr != nil {
		_ = conn.Close()
		return ierr
	}

	s.streamsMu.Lock()
	s.playbackStreams[sn] = &playbackStream{
		node: id, channel: channelID, target: target, addr: addr,
		start: start, end: end, callID: callID, rtp: conn,
	}
	s.streamsMu.Unlock()
	s.log.Info("playback stream started",
		"node_id", id.String(), "channel", channelID, "sn", sn,
		"start", start.Format(time.RFC3339), "end", end.Format(time.RFC3339))
	return nil
}

// PlaybackWindow returns the window of an active playback stream.
func (s *NodeService) PlaybackWindow(sn string) (start, end time.Time, ok bool) {
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()
	stream, ok := s.playbackStreams[sn]
	if !ok {
		return time.Time{}, time.Time{}, false
	}
	return stream.start, stream.end, true
}

// StopPlaybackStream ends a stream, tells the device, and releases the port.
func (s *NodeService) StopPlaybackStream(sn string) {
	s.streamsMu.Lock()
	stream := s.playbackStreams[sn]
	delete(s.playbackStreams, sn)
	s.streamsMu.Unlock()
	if stream == nil {
		return
	}
	if stream.rtp != nil {
		_ = stream.rtp.Close()
	}
	if s.acceptor != nil {
		_ = s.acceptor.Bye(context.Background(), stream.node, stream.target, stream.addr, stream.callID)
	}
}
