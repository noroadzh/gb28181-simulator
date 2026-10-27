package app

// CANCEL handling. A platform abandons a pending INVITE with a CANCEL that
// carries the INVITE's Call-ID and CSeq; RFC 3261 §9.2 then requires two
// answers: 200 OK to the CANCEL, and 487 Request Terminated as the INVITE's
// final response. Once the INVITE has a final response the transaction is
// gone, and a late CANCEL gets 481 Call/Transaction Does Not Exist.

import (
	"context"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// pendingInvite is one INVITE transaction whose final response has not been
// sent yet.
type pendingInvite struct {
	peer    string
	request model.Message
}

func pendingInviteKey(nodeID model.NodeID, callID string) string {
	return nodeID.String() + "|" + callID
}

// markPendingInvite records an INVITE before its handler runs, so a CANCEL
// racing that handler can terminate it.
func (a *Acceptor) markPendingInvite(nodeID model.NodeID, callID string, req model.Message, peer string) {
	if callID == "" {
		return
	}
	a.inviteMu.Lock()
	if a.pendingInvites == nil {
		a.pendingInvites = make(map[string]pendingInvite)
	}
	a.pendingInvites[pendingInviteKey(nodeID, callID)] = pendingInvite{peer: peer, request: req}
	a.inviteMu.Unlock()
}

// takePendingInvite removes and returns the INVITE a CANCEL refers to.
func (a *Acceptor) takePendingInvite(nodeID model.NodeID, callID string) (pendingInvite, bool) {
	a.inviteMu.Lock()
	defer a.inviteMu.Unlock()
	key := pendingInviteKey(nodeID, callID)
	pi, ok := a.pendingInvites[key]
	if ok {
		delete(a.pendingInvites, key)
	}
	return pi, ok
}

// clearPendingInvite drops the transaction once its final response has been
// sent. A CANCEL arriving after this finds nothing and gets 481.
func (a *Acceptor) clearPendingInvite(nodeID model.NodeID, callID string) {
	if callID == "" {
		return
	}
	a.inviteMu.Lock()
	delete(a.pendingInvites, pendingInviteKey(nodeID, callID))
	a.inviteMu.Unlock()
}

// handleCancel answers a CANCEL and, when the INVITE is still pending, ends
// that INVITE with 487.
//
// This UAS answers INVITEs synchronously, so in practice a CANCEL arrives
// behind the final response and the 481 branch is the normal outcome. The
// pending branch exists because a slow handler (a source that takes time to
// open) makes the race real, and RFC 3261 requires it.
func (a *Acceptor) handleCancel(
	ctx context.Context,
	p *platform,
	req model.Message,
	peer string,
) (model.Message, error) {
	callID := headerValue(req, "Call-ID")
	if callID == "" {
		return buildResponse(400, "Bad Request", req, nil, "")
	}
	pi, ok := a.takePendingInvite(p.id, callID)
	if !ok {
		a.log.Debug("CANCEL without a pending INVITE",
			"node_id", p.id.String(), "call_id", callID, "peer", peer)
		return buildResponse(481, "Call/Transaction Does Not Exist", req, nil, "")
	}
	a.log.Debug("CANCEL terminates a pending INVITE",
		"node_id", p.id.String(), "call_id", callID, "peer", pi.peer)
	// The 487 is the INVITE's final response, addressed to whoever sent it.
	terminated, terr := buildResponse(487, "Request Terminated", pi.request, nil, "")
	if terr != nil {
		return buildResponse(500, "Server Internal Error", req, nil, "")
	}
	if resp, dst, ok := a.routeOutbound(p, terminated, pi.peer); ok {
		if err := p.tr.Send(ctx, resp, dst); err != nil {
			a.log.Warn("cannot send 487 to the cancelled INVITE",
				"node_id", p.id.String(), "call_id", callID,
				"peer", dst, "error", err.Error())
		}
	}
	return buildResponse(200, "OK", req, nil, "")
}
