package app

// INVITE handling: the media-session entry point. handleInvite answers an
// incoming INVITE by creating or reusing the Dialog for its Call-ID, wiring
// the inbound media pipeline, and replying 200 OK. The negotiation details
// (SDP offer/answer, PS/RTP flow) live in the adapter layer; this file owns
// the SIP-side bookkeeping and the dialog wiring.

import (
	"context"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// handleInvite answers an incoming INVITE request. For now it creates or
// reuses a Dialog for the Call-ID, echoes the received body as the answer,
// and returns 200 OK. A real media pipeline will be wired in Change #8.
func (a *Acceptor) handleInvite(
	ctx context.Context,
	p *platform,
	req model.Message,
	_ string,
) (model.Message, error) {
	a.log.Debug("INVITE received",
		"node_id", p.id.String(), "call_id", headerValue(req, "Call-ID"))
	callID := headerValue(req, "Call-ID")
	if callID == "" {
		return buildResponse(400, "Bad Request", req, nil, "")
	}
	if a.dialogs != nil {
		if _, err := a.dialogs.Get(callID); !err {
			a.dialogs.Create(callID)
			a.log.Debug("INVITE dialog created", "node_id", p.id.String(), "call_id", callID)
		}
	}
	mediaType, portStr, _, sdpErr := parseSDPMetadata(req.Body())
	switch {
	case sdpErr != nil:
		a.log.Warn("INVITE SDP unreadable", "node_id", p.id.String(),
			"call_id", callID, "error", sdpErr.Error())
		if a.dialogs != nil {
			a.dialogs.Terminate(callID)
		}
		return buildResponse(400, "Bad Request", req, nil, "")
	case mediaType == "":
		a.log.Debug("INVITE accepted without media section",
			"node_id", p.id.String(), "call_id", callID)
	default:
		a.createInboundPipeline(ctx, p, callID, mediaType, portStr, req.Body())
		a.watchInviteExpiry(p, callID)
	}
	hdrs := []model.Header{
		model.NewHeader("Contact", "<sip:"+p.id.String()+"@"+p.realm+">"),
		allowHeader(),
	}
	return buildResponse(200, "OK", req, hdrs, req.Body())
}

// watchInviteExpiry schedules a 30 s timer for the INVITE dialog. If the ACK
// is not received in time the dialog is terminated, which tears down the
// inbound media pipeline through the manager's onDelete callback.
//
// The watcher goroutine exits on whichever of three signals fires first:
// the timer firing, an explicit cancellation (ACK arrived), or the parent
// node stopping. That third arm is the one the previous implementation
// missed, and is the reason this used to leak a goroutine per cancel.
func (a *Acceptor) watchInviteExpiry(p *platform, callID string) {
	timer := time.NewTimer(30 * time.Second)
	done := make(chan struct{})

	a.inviteMu.Lock()
	a.inviteTimers[callID] = timer
	a.inviteWatchers[callID] = done
	a.inviteMu.Unlock()

	go func() {
		select {
		case <-timer.C:
			a.log.Debug("INVITE expired without ACK, terminating dialog",
				"node_id", p.id.String(), "call_id", callID)
			if a.dialogs != nil {
				a.dialogs.Terminate(callID)
			}
		case <-done:
			// Cancelled by ACK or call-site termination; nothing to do.
		case <-p.done:
			// The node that owned this dialog stopped serving (Stop and
			// Close both wait for p.done), so the dialog table is being
			// unwound by whoever stopped it. Tearing down from here
			// would report a media fault for a clean shutdown.
		case <-a.ctx.Done():
			// The whole acceptor is shutting down: same reasoning, one
			// level up.
		}
		a.inviteMu.Lock()
		delete(a.inviteTimers, callID)
		delete(a.inviteWatchers, callID)
		a.inviteMu.Unlock()
	}()
}

// cancelInviteExpiry stops a pending INVITE expiry timer and removes it.
// Closing the done channel is what wakes the watcher goroutine; timer.Stop
// alone would not, because Stop on an unfired timer does not drain the
// channel that the goroutine is reading.
func (a *Acceptor) cancelInviteExpiry(callID string) {
	a.inviteMu.Lock()
	timer, timerOK := a.inviteTimers[callID]
	done, doneOK := a.inviteWatchers[callID]
	delete(a.inviteTimers, callID)
	delete(a.inviteWatchers, callID)
	a.inviteMu.Unlock()

	if !timerOK && !doneOK {
		return
	}
	if doneOK {
		close(done)
	}
	if timerOK {
		timer.Stop()
	}
}

// handleAck advances the dialog for an INVITE transaction but does not answer:
// ACK carries no response in the SIP request/response model. Confirming the
// dialog also cancels the INVITE expiry timer, so a confirmed dialog is not
// torn down 30 s later by a stale timer.
func (a *Acceptor) handleAck(
	_ context.Context,
	p *platform,
	req model.Message,
	_ string,
) {
	a.log.Debug("ACK received",
		"node_id", p.id.String(), "call_id", headerValue(req, "Call-ID"))
	callID := headerValue(req, "Call-ID")
	if callID == "" || a.dialogs == nil {
		return
	}
	if d, err := a.dialogs.Get(callID); err && !d.IsTerminated() {
		if _, err := a.dialogs.Confirm(callID, nil); err != nil {
			a.log.Warn("ACK could not confirm dialog",
				"node_id", p.id.String(), "call_id", callID, "error", err.Error())
			return
		}
		a.cancelInviteExpiry(callID)
	}
}

// handleBye terminates the dialog identified by the Call-ID and answers with
// 200 OK.
func (a *Acceptor) handleBye(
	_ context.Context,
	p *platform,
	req model.Message,
	_ string,
) (model.Message, error) {
	a.log.Debug("BYE received",
		"node_id", p.id.String(), "call_id", headerValue(req, "Call-ID"))
	callID := headerValue(req, "Call-ID")
	if callID != "" && a.dialogs != nil {
		a.dialogs.Terminate(callID)
		a.closePipeline(callID)
		a.log.Debug("BYE terminated dialog", "node_id", p.id.String(), "call_id", callID)
	}
	return buildResponse(200, "OK", req, nil, "")
}

// handleOptions answers a keepalive probe. The Allow header advertises what
// the platform serves; a minimal 200 OK is enough for the peer to mark the
// link healthy.
func (a *Acceptor) handleOptions(
	_ context.Context,
	_ *platform,
	req model.Message,
	_ string,
) (model.Message, error) {
	hdrs := []model.Header{
		allowHeader(),
		model.NewHeader("Accept", "Application/MANSCDP+XML"),
	}
	return buildResponse(200, "OK", req, hdrs, "")
}
