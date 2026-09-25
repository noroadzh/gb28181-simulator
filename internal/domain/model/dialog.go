// Package model — Dialog / Session.
package model

import (
	"fmt"
	"sync"
	"time"
)

// DialogState tracks where an INVITE dialog is in its lifecycle.
type DialogState int

const (
	// DialogIdle is the initial state before any INVITE is seen.
	DialogIdle DialogState = iota
	// DialogCalling means an INVITE was sent and we are waiting for a
	// provisional response (1xx) or final response.
	DialogCalling
	// DialogConfirmed means a 2xx final response was received and the
	// media pipeline is established.
	DialogConfirmed
	// DialogTerminated means BYE or CANCEL ended the dialog.
	DialogTerminated
)

func (s DialogState) String() string {
	switch s {
	case DialogIdle:
		return "idle"
	case DialogCalling:
		return "calling"
	case DialogConfirmed:
		return "confirmed"
	case DialogTerminated:
		return "terminated"
	}
	return fmt.Sprintf("DialogState(%d)", s)
}

// Dialog represents one SIP INVITE dialog (RFC 3261 §12). It is intentionally
// small: the simulator does not need full dialog state, only the handful of
// fields the media pipeline and the serving loop need to match BYE to INVITE.
type Dialog struct {
	mu sync.Mutex

	// CallID is the dialog's Call-ID header value.
	CallID string
	// LocalTag is the From tag we generated.
	LocalTag string
	// RemoteTag is the To tag the peer generated, empty until the first
	// non-100 response arrives.
	RemoteTag string
	// State is the current dialog lifecycle state.
	State DialogState
	// Method is the request method that created the dialog (always INVITE
	// for this simulator).
	Method string
	// CreatedAt is when the dialog was first observed.
	CreatedAt time.Time
	// ExpiresAt is when the dialog should be terminated if no BYE arrives.
	ExpiresAt time.Time
	// Media is the negotiated media description (SDP).
	Media *Session
}

// NewDialog builds a fresh dialog in the calling state.
func NewDialog(callID, localTag, method string, ttl time.Duration) Dialog {
	now := time.Now()
	return Dialog{
		CallID:    callID,
		LocalTag:  localTag,
		State:     DialogCalling,
		Method:    method,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
}

// WithRemoteTag sets the remote tag and transitions to confirmed if the
// response is final. It returns a pointer so the caller can atomically swap.
func (d *Dialog) WithRemoteTag(tag string, final bool) *Dialog {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.RemoteTag = tag
	if final && d.State == DialogCalling {
		d.State = DialogConfirmed
	}
	return d
}

// WithMedia attaches the negotiated media description.
func (d *Dialog) WithMedia(s *Session) *Dialog {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Media = s
	return d
}

// WithState transitions the dialog state. Illegal transitions (idle →
// terminated) are silently dropped.
func (d *Dialog) WithState(next DialogState) *Dialog {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch d.State {
	case DialogIdle:
		if next == DialogCalling {
			d.State = next
		}
	case DialogCalling:
		if next == DialogConfirmed || next == DialogTerminated {
			d.State = next
		}
	case DialogConfirmed:
		if next == DialogTerminated {
			d.State = next
		}
	}
	return d
}

// DialogID returns the RFC 3261 dialog identifier: Call-ID + local-tag +
// remote-tag. The order matches the standard.
func (d *Dialog) DialogID() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.CallID + ";" + d.LocalTag + ";" + d.RemoteTag
}

// IsTerminated reports whether the dialog is over.
func (d *Dialog) IsTerminated() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.State == DialogTerminated || time.Now().After(d.ExpiresAt)
}

// String renders a log-safe summary.
func (d *Dialog) String() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return fmt.Sprintf("Dialog<call_id=%s state=%s method=%s>",
		d.CallID, d.State, d.Method)
}
