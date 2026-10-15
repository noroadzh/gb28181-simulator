// Package app implements the SIP acceptor handling device registration, message
// routing, and media pipeline management for GB/T 28181 simulation.
//
// This file contains fault injection logic (Cluster 2): faultGate, faultUnsupported,
// recordFault, cannedReason, and the sipReasons table.
package app

import (
	"math/rand"
	"net/http"
	"slices"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// faultDecision is the gate's tri-state verdict for one inbound request
// (design D6): serve through the normal handlers, skip silently, or answer
// with the canned response the gate returns.
type faultDecision int

const (
	faultServe  faultDecision = iota
	faultSkip                 // swallow the request, nothing is sent
	faultAnswer               // send the returned canned response
)

// faultGate consults the node's installed fault profile before the request
// is dispatched. Evaluation order follows the profile contract: blackhole,
// then probabilistic drop, then delay, then canned. Skip and answer
// verdicts are counted on the store when it carries the counters extension.
// A node with no profile — every fixture's default — returns faultServe
// without touching the rand source or the clock, which is what keeps the
// no-fault byte path identical.
func (a *Acceptor) faultGate(p *platform, req model.Message, peer string) (model.Message, faultDecision) {
	profile, ok := a.faults.Get(a.ctx, p.id)
	if !ok || profile.IsZero() {
		return model.Message{}, faultServe
	}
	method := req.Method()
	if slices.Contains(profile.Blackhole, method) {
		a.recordFault(p.id, model.FaultBlackhole)
		return model.Message{}, faultSkip
	}
	if profile.Drop > 0 && rand.Float64() < profile.Drop {
		a.recordFault(p.id, model.FaultDrop)
		a.log.Info("fault profile dropped a request",
			"node_id", p.id.String(), "method", method, "peer", peer)
		return model.Message{}, faultSkip
	}
	// Delay withholds every answer — canned and served alike.
	if !profile.Delay.IsZero() {
		d := profile.Delay.Base
		if profile.Delay.Jitter > 0 {
			d += time.Duration(rand.Int63n(int64(profile.Delay.Jitter) + 1))
		}
		a.recordFault(p.id, model.FaultDelay)
		time.Sleep(d)
	}
	if status, canned := profile.Canned[method]; canned {
		a.recordFault(p.id, model.FaultCannedResponse)
		resp, err := buildResponse(status, cannedReason(status), req, nil, "")
		if err != nil {
			a.log.Warn("cannot render the canned fault answer",
				"node_id", p.id.String(), "method", method,
				"status", status, "error", err.Error())
			return model.Message{}, faultSkip
		}
		return resp, faultAnswer
	}
	return model.Message{}, faultServe
}

// faultUnsupported answers a method the node does not serve with the
// profile's UnsupportedMethod status when one is configured; without a
// profile the request stays a silent drop, the historical behaviour.
func (a *Acceptor) faultUnsupported(p *platform, req model.Message) (model.Message, bool) {
	if a.faults == nil {
		return model.Message{}, false
	}
	profile, ok := a.faults.Get(a.ctx, p.id)
	if !ok || profile.UnsupportedMethod == 0 {
		return model.Message{}, false
	}
	a.recordFault(p.id, model.FaultUnsupportedMethod)
	resp, err := buildResponse(profile.UnsupportedMethod,
		cannedReason(profile.UnsupportedMethod), req, nil, "")
	if err != nil {
		a.log.Warn("cannot render the unsupported-method answer",
			"node_id", p.id.String(), "status", profile.UnsupportedMethod,
			"error", err.Error())
		return model.Message{}, false
	}
	return resp, true
}

// recordFault bumps the store's counter for an action that actually fired.
// A bare port.FaultStore has nowhere to put counters, so the extension is
// discovered structurally and its absence never changes the gate outcome.
func (a *Acceptor) recordFault(nodeID model.NodeID, action model.FaultAction) {
	if rec, ok := a.faults.(interface {
		Record(nodeID model.NodeID, action model.FaultAction)
	}); ok {
		rec.Record(nodeID, action)
	}
}

// cannedReason returns a reason phrase for a canned fault status. SIP
// shares most phrases with HTTP; the handful that differ are tabled here
// and the rest fall back to http.StatusText.
func cannedReason(status int) string {
	if r, ok := sipReasons[status]; ok {
		return r
	}
	if r := http.StatusText(status); r != "" {
		return r
	}
	return "Fault"
}

var sipReasons = map[int]string{
	480: "Temporarily Unavailable",
	486: "Busy Here",
	500: "Server Internal Error",
	502: "Bad Gateway",
	503: "Service Unavailable",
	504: "Server Time-out",
	600: "Busy Everywhere",
	603: "Decline",
	604: "Does Not Exist Anywhere",
}
