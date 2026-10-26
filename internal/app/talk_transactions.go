package app

import (
	"context"
	"fmt"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// Invite opens a dialog with the device that owns the channel and waits for
// its 2xx answer. The transaction runs here (the acceptor is the node's only
// socket reader), keyed the same way as record queries.
//
// A non-2xx answer is reported as unsupported: the device understood the
// INVITE and refused it, which is a capability answer rather than a timeout.
func (a *Acceptor) Invite(ctx context.Context, id model.NodeID, target, addr, body string) (string, error) {
	p, ok := a.platformByID(id)
	if !ok || p.tr == nil {
		return "", fmt.Errorf("%w: node %s is not serving", port.ErrControlTargetUnreachable, id)
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
		return "", fmt.Errorf("app: build INVITE: %w", err)
	}

	key, answer, ok := a.responses.put(id, req)
	if !ok {
		return "", fmt.Errorf("%w: INVITE carries no transaction identity", port.ErrTalkUnsupported)
	}
	if err := p.tr.Send(ctx, req, addr); err != nil {
		a.responses.drop(key)
		return "", fmt.Errorf("app: send INVITE to %s: %w: %w",
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
		if resp.StatusCode() >= 200 && resp.StatusCode() < 300 {
			return callID, nil
		}
		return "", fmt.Errorf("%w: device answered %d %s",
			port.ErrTalkUnsupported, resp.StatusCode(), resp.StatusText())
	case <-ctx.Done():
		a.responses.drop(key)
		return "", ctx.Err()
	case <-timer.C:
		a.responses.drop(key)
		a.log.Warn("talk INVITE timed out", "node_id", id.String(), "target", target, "wait", wait)
		return "", fmt.Errorf("%w: INVITE to %s after %s", port.ErrControlTimeout, target, wait)
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
