package app

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// Invite opens a talk dialog with the device that owns the channel and waits
// for its 2xx answer. The transaction runs here (the acceptor is the node's
// only socket reader), keyed the same way as record queries.
//
// A non-2xx answer is reported as unsupported: the device understood the
// INVITE and refused it, which is a capability answer rather than a timeout.
func (a *Acceptor) Invite(ctx context.Context, id model.NodeID, target, addr, body string) (string, string, error) {
	callID, resp, err := a.invite(ctx, id, target, addr, body, port.ErrTalkUnsupported)
	if err != nil {
		return "", "", err
	}
	remote, perr := parseRTPEndpoint(resp.Body())
	if perr != nil {
		return "", "", fmt.Errorf("%w: answer from %s: %v",
			port.ErrControlTargetUnreachable, target, perr)
	}
	return callID, remote, nil
}

// InvitePlayback opens a historical playback dialog. Its answer names a video
// stream rather than the talk audio endpoint, so only the 2xx matters here.
func (a *Acceptor) InvitePlayback(ctx context.Context, id model.NodeID, target, addr, body string) (string, error) {
	callID, _, err := a.invite(ctx, id, target, addr, body, port.ErrPlaybackUnsupported)
	if err != nil {
		return "", err
	}
	return callID, nil
}

// invite is the shared INVITE transaction: build, send, wait for the 2xx, and
// report a refusal with the capability's own sentinel.
func (a *Acceptor) invite(ctx context.Context, id model.NodeID, target, addr, body string, unsupported error) (string, model.Message, error) {
	p, ok := a.platformByID(id)
	if !ok || p.tr == nil {
		return "", model.Message{}, fmt.Errorf("%w: node %s is not serving", port.ErrControlTargetUnreachable, id)
	}
	callID := randomCallID()
	req, err := model.NewRequest("INVITE", fmt.Sprintf("sip:%s@%s", target, addr), []model.Header{
		model.NewHeader("From", fmt.Sprintf("<sip:%s@%s>;tag=%s", id.String(), p.realm, randomTag())),
		model.NewHeader("To", fmt.Sprintf("<sip:%s@%s>", target, addr)),
		model.NewHeader("Call-ID", callID),
		model.NewHeader("CSeq", "1 INVITE"),
		model.NewHeader("Contact", fmt.Sprintf("<sip:%s@%s>", id.String(), p.tr.LocalAddr())),
		model.NewHeader("Max-Forwards", "70"),
		model.NewHeader("Content-Type", "application/sdp"),
	}, body)
	if err != nil {
		return "", model.Message{}, fmt.Errorf("app: build INVITE: %w", err)
	}

	key, answer, ok := a.responses.put(id, req)
	if !ok {
		return "", model.Message{}, fmt.Errorf("%w: INVITE carries no transaction identity", unsupported)
	}
	if err := p.tr.Send(ctx, req, addr); err != nil {
		a.responses.drop(key)
		return "", model.Message{}, fmt.Errorf("app: send INVITE to %s: %w: %w",
			addr, port.ErrControlTargetUnreachable, err)
	}

	wait := a.controlWait
	if wait <= 0 {
		wait = talkTimeout
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case resp := <-answer:
		if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
			return "", model.Message{}, fmt.Errorf("%w: device answered %d %s",
				unsupported, resp.StatusCode(), resp.StatusText())
		}
		return callID, resp, nil
	case <-ctx.Done():
		a.responses.drop(key)
		return "", model.Message{}, ctx.Err()
	case <-timer.C:
		a.responses.drop(key)
		a.log.Warn("INVITE timed out", "node_id", id.String(), "target", target, "wait", wait)
		return "", model.Message{}, fmt.Errorf("%w: INVITE to %s after %s", port.ErrControlTimeout, target, wait)
	}
}

// Bye ends the dialog opened by Invite. It is fire-and-forget: the BYE is the
// end of the conversation, and a device that does not answer it is gone
// anyway.
func (a *Acceptor) Bye(ctx context.Context, id model.NodeID, target, addr, callID string) error {
	p, ok := a.platformByID(id)
	if !ok || p.tr == nil {
		return fmt.Errorf("%w: node %s is not serving", port.ErrControlTargetUnreachable, id)
	}
	req, err := model.NewRequest("BYE", fmt.Sprintf("sip:%s@%s", target, addr), []model.Header{
		model.NewHeader("From", fmt.Sprintf("<sip:%s@%s>;tag=%s", id.String(), p.realm, randomTag())),
		model.NewHeader("To", fmt.Sprintf("<sip:%s@%s>", target, addr)),
		model.NewHeader("Call-ID", callID),
		model.NewHeader("CSeq", "2 BYE"),
		model.NewHeader("Max-Forwards", "70"),
	}, "")
	if err != nil {
		return fmt.Errorf("app: build BYE: %w", err)
	}
	if err := p.tr.Send(ctx, req, addr); err != nil {
		return fmt.Errorf("app: send BYE to %s: %w: %w",
			addr, port.ErrControlTargetUnreachable, err)
	}
	return nil
}

// parseRTPEndpoint reads the device's RTP endpoint out of an SDP answer:
// the c= connection address and the m=audio port. Both are required — an
// answer without them says nothing about where audio should go.
func parseRTPEndpoint(sdp string) (string, error) {
	host := ""
	port := ""
	for _, line := range strings.Split(sdp, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "c=IN IP4 "):
			host = strings.TrimSpace(strings.TrimPrefix(line, "c=IN IP4 "))
		case strings.HasPrefix(line, "m=audio "):
			fields := strings.Fields(strings.TrimPrefix(line, "m=audio "))
			if len(fields) > 0 {
				port = fields[0]
			}
		}
	}
	if host == "" || port == "" || port == "0" {
		return "", fmt.Errorf("answer carries no RTP endpoint (c=%q m=audio %q)", host, port)
	}
	return net.JoinHostPort(host, port), nil
}
