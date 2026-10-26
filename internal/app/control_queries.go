package app

import (
	"context"
	"fmt"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// recordTimeFormat is the timestamp layout GB/T 28181 record queries use.
const recordTimeFormat = "2006-01-02T15:04:05"

// formatRecordTime renders a query bound; the zero time means "unbounded" and
// is omitted from the body.
func formatRecordTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(recordTimeFormat)
}

// platformByID returns the serving state of one node.
func (a *Acceptor) platformByID(id model.NodeID) (*platform, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.serving[id.String()]
	return p, ok
}

// QueryRecords asks a device for the recordings it holds and waits for the
// answer. The transaction runs here, not in NodeService, because the acceptor
// is the only reader of the node's socket: a query whose answer nobody reads
// would always time out.
//
// The request goes to addr (a device node addressed to itself, or a
// downstream device), and the answer is matched by node + Call-ID + CSeq, the
// same identity the relay table uses.
func (a *Acceptor) QueryRecords(
	ctx context.Context,
	id model.NodeID,
	target, addr, channelID string,
	start, end time.Time,
) ([]model.RecordInfoItem, error) {
	p, ok := a.platformByID(id)
	if !ok || p.tr == nil {
		return nil, fmt.Errorf("%w: node %s is not serving", port.ErrControlTargetUnreachable, id)
	}
	if a.manscdp == nil {
		return nil, port.RecordUnsupported(id, channelID, "no MANSCDP codec wired")
	}

	sn := uint32(time.Now().UnixNano())
	body, err := a.manscdp.MarshalRecordInfoQuery(
		model.NewRecordInfoQuery(target, sn, formatRecordTime(start), formatRecordTime(end)), sn)
	if err != nil {
		return nil, port.RecordUnsupported(id, channelID, err.Error())
	}
	msg, err := buildDeviceControl(p.realm, id.String(), target, addr, body)
	if err != nil {
		return nil, fmt.Errorf("app: build record query: %w", err)
	}

	key, answer, ok := a.responses.put(id, msg)
	if !ok {
		return nil, port.RecordUnsupported(id, channelID, "record query without Call-ID/CSeq")
	}
	if err := p.tr.Send(ctx, msg, addr); err != nil {
		a.responses.drop(key)
		return nil, fmt.Errorf("app: send record query to %s: %w: %w",
			addr, port.ErrControlTargetUnreachable, err)
	}

	wait := a.controlWait
	if wait <= 0 {
		wait = 5 * time.Second
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case resp := <-answer:
		decoded, derr := a.manscdp.DecodeRecordInfoResponse(resp.Body())
		if derr != nil {
			return nil, port.RecordUnsupported(id, channelID, "unreadable RecordInfo answer: "+derr.Error())
		}
		return decoded.Items, nil
	case <-ctx.Done():
		a.responses.drop(key)
		return nil, ctx.Err()
	case <-timer.C:
		a.responses.drop(key)
		a.log.Warn("record query timed out",
			"node_id", id.String(), "target", target, "wait", wait)
		return nil, fmt.Errorf("%w: record query to %s after %s", port.ErrControlTimeout, target, wait)
	}
}
