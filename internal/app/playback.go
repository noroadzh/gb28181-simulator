package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// playbackSession is one active playback on a node|channel. target/addr are
// kept so teardown reaches the same device the play went to, even when the
// node's device table has changed since.
type playbackSession struct {
	target string
	addr   string
	cancel context.CancelFunc
}

// playbackAction maps the API vocabulary onto the MANSCDP command word.
// "speed" is a play at a different rate, carried by the Scale element.
func playbackAction(action string) (command string, isSpeed bool, ok bool) {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "play":
		return "Play", false, true
	case "pause":
		return "Pause", false, true
	case "teardown":
		return "Teardown", false, true
	case "speed":
		return "Play", true, true
	}
	return "", false, false
}

// Playback issues a PLAY/PAUSE/TEARDOWN command for the channel. One session
// per channel: a second play first tears the previous one down, and a session
// that sees no further control is reclaimed after playbackIdle.
func (s *NodeService) Playback(ctx context.Context, id model.NodeID, channelID, action string, scale int) error {
	node, ok := s.registry.Get(ctx, id)
	if !ok {
		return model.ErrUnknownNode
	}
	if s.controlCodec == nil {
		return fmt.Errorf("%w: playback is not wired", port.ErrPlaybackUnsupported)
	}
	command, isSpeed, ok := playbackAction(action)
	if !ok {
		return fmt.Errorf("%w: unknown action %q", port.ErrPlaybackUnsupported, action)
	}
	tr := s.lifecycle.Transport(id)
	if tr == nil {
		return fmt.Errorf("%w: node has no bound listener", port.ErrPlaybackUnsupported)
	}
	key := id.String() + "|" + channelID

	target, addr, err := s.controlTarget(ctx, node, channelID)
	if err != nil {
		return err
	}

	scaleText := ""
	if isSpeed && scale != 0 {
		scaleText = strconv.Itoa(scale)
	}

	switch command {
	case "Teardown":
		// endPlayback already sends the Teardown for a live session; a
		// teardown of nothing is still answered with one, so a retry after a
		// lost response is harmless and idempotent.
		if s.lookupPlayback(key) != nil {
			s.endPlayback(ctx, tr, node, key, channelID)
			return nil
		}
		return s.sendPlayback(ctx, tr, node, target, addr, channelID, command, "")
	case "Pause":
		if sess := s.lookupPlayback(key); sess != nil {
			target, addr = sess.target, sess.addr
		} else {
			return fmt.Errorf("%w: no active playback on %s", port.ErrPlaybackUnsupported, channelID)
		}
	default: // Play
		// A second play replaces the session rather than leaking a port.
		s.endPlayback(ctx, tr, node, key, channelID)
		if err := s.sendPlayback(ctx, tr, node, target, addr, channelID, command, scaleText); err != nil {
			return err
		}
		s.armPlaybackReclaim(id, node, key, target, addr, channelID, tr)
		return nil
	}

	if err := s.sendPlayback(ctx, tr, node, target, addr, channelID, command, scaleText); err != nil {
		return err
	}
	// Any control refreshes the idle timer of the session it belongs to.
	s.armPlaybackReclaim(id, node, key, target, addr, channelID, tr)
	return nil
}

// sendPlayback renders and sends one PlaybackControl command.
func (s *NodeService) sendPlayback(
	ctx context.Context,
	tr port.SIPTransport,
	node model.Node,
	target, addr, channelID, command, scale string,
) error {
	sn := uint32(s.clock.Now().UnixNano())
	ctrl, err := model.NewPlaybackControl(target, command, channelID, "", "", sn)
	if err != nil {
		return fmt.Errorf("%w: %v", port.ErrPlaybackUnsupported, err)
	}
	body, err := s.controlCodec.MarshalPlaybackControl(ctrl, scale)
	if err != nil {
		return fmt.Errorf("%w: %v", port.ErrPlaybackUnsupported, err)
	}
	msg, err := buildDeviceControl(node.Profile().Domain(), node.ID().String(), target, addr, body)
	if err != nil {
		return fmt.Errorf("app: build playback control: %w", err)
	}
	if err := tr.Send(ctx, msg, addr); err != nil {
		return fmt.Errorf("app: send playback control to %s: %w: %w",
			addr, port.ErrControlTargetUnreachable, err)
	}
	return nil
}

// armPlaybackReclaim (re)starts the idle timer of a session. The timer is
// cancelled by the next control or by teardown, so only a genuinely idle
// session reclaims itself.
func (s *NodeService) armPlaybackReclaim(
	id model.NodeID,
	node model.Node,
	key, target, addr, channelID string,
	tr port.SIPTransport,
) {
	s.playbackMu.Lock()
	if old := s.playback[key]; old != nil {
		old.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	sess := &playbackSession{target: target, addr: addr, cancel: cancel}
	s.playback[key] = sess
	s.playbackMu.Unlock()

	idle := s.playbackIdle
	if idle <= 0 {
		idle = time.Minute
	}
	go func() {
		timer := time.NewTimer(idle)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		s.playbackMu.Lock()
		current := s.playback[key] == sess
		if current {
			delete(s.playback, key)
		}
		s.playbackMu.Unlock()
		if !current {
			return
		}
		_ = s.sendPlayback(context.Background(), tr, node, target, addr, channelID, "Teardown", "")
		s.log.Info("playback reclaimed after idle",
			"node_id", id.String(), "channel", channelID)
	}()
}

// lookupPlayback returns the active session for a node|channel, if any.
func (s *NodeService) lookupPlayback(key string) *playbackSession {
	s.playbackMu.Lock()
	defer s.playbackMu.Unlock()
	return s.playback[key]
}

// endPlayback tears down and forgets the session for key, if there is one.
func (s *NodeService) endPlayback(
	ctx context.Context,
	tr port.SIPTransport,
	node model.Node,
	key, channelID string,
) {
	s.playbackMu.Lock()
	sess := s.playback[key]
	delete(s.playback, key)
	s.playbackMu.Unlock()
	if sess == nil {
		return
	}
	sess.cancel()
	_ = s.sendPlayback(ctx, tr, node, sess.target, sess.addr, channelID, "Teardown", "")
}
