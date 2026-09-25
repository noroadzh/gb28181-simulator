// Package app — Dialog.
//
// DialogManager is the app-layer component that tracks SIP Dialogs on a
// platform-small node. It lives in app rather than domain because Dialog is
// a protocol-session concept; the domain layer keeps only pure value objects.
//
// The model.Dialog value object is itself concurrency-safe (its methods
// take their own lock), so the manager only guards the map. We never mutate
// a Dialog in place; we replace the map entry with a WithX-returned copy.
package app

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

const (
	defaultDialogTimeout = 30 * time.Second
	dialogTagPrefix      = "sim"
)

// DialogManager tracks SIP Dialogs by Call-ID.
type DialogManager struct {
	mu       sync.Mutex
	dialogs  map[string]*model.Dialog
	timeout  time.Duration
	onDelete func(callID string)
}

// DialogManagerConfig configures the DialogManager.
type DialogManagerConfig struct {
	Timeout  time.Duration       // zero means 30s
	OnDelete func(callID string) // optional callback when a Dialog is deleted
}

// NewDialogManager builds a DialogManager.
func NewDialogManager(cfg DialogManagerConfig) *DialogManager {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultDialogTimeout
	}
	return &DialogManager{
		dialogs:  make(map[string]*model.Dialog),
		timeout:  timeout,
		onDelete: cfg.OnDelete,
	}
}

// Create creates a new Dialog in calling state and stores it.
func (m *DialogManager) Create(callID string) *model.Dialog {
	d := model.NewDialog(callID, dialogTag(), "INVITE", m.timeout)
	m.mu.Lock()
	m.dialogs[callID] = &d
	m.mu.Unlock()
	return &d
}

// Get returns the Dialog pointer for the given Call-ID. If the dialog
// has expired, it is removed and (nil, false) is returned. The optional
// onDelete callback is fired for expired dialogs so callers can clean up
// associated resources (e.g. inbound media pipelines).
func (m *DialogManager) Get(callID string) (*model.Dialog, bool) {
	m.mu.Lock()
	d, ok := m.dialogs[callID]
	if !ok {
		m.mu.Unlock()
		return nil, false
	}
	if d.IsTerminated() {
		delete(m.dialogs, callID)
		onDelete := m.onDelete
		m.mu.Unlock()
		if onDelete != nil {
			onDelete(callID)
		}
		return nil, false
	}
	m.mu.Unlock()
	return d, ok
}

// SetOnDelete configures a callback that is invoked whenever a dialog
// entry is removed from the manager, whether by Terminate() or by
// expiration detected in Get().
func (m *DialogManager) SetOnDelete(fn func(callID string)) {
	m.mu.Lock()
	m.onDelete = fn
	m.mu.Unlock()
}

// Confirm advances the Dialog to confirmed and attaches media.
func (m *DialogManager) Confirm(callID string, media *model.Session) (*model.Dialog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.dialogs[callID]
	if !ok {
		return nil, fmt.Errorf("dialog: no dialog for %s", callID)
	}
	d.WithState(model.DialogConfirmed).WithMedia(media)
	return d, nil
}

// SetRemoteTag records the peer's To-tag and returns the updated copy.
func (m *DialogManager) SetRemoteTag(callID, tag string, final bool) (*model.Dialog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.dialogs[callID]
	if !ok {
		return nil, fmt.Errorf("dialog: no dialog for %s", callID)
	}
	d.WithRemoteTag(tag, final)
	return d, nil
}

// Terminate marks the Dialog terminated and removes it from the manager.
func (m *DialogManager) Terminate(callID string) {
	m.mu.Lock()
	delete(m.dialogs, callID)
	onDelete := m.onDelete
	m.mu.Unlock()
	if onDelete != nil {
		onDelete(callID)
	}
}

// Range iterates over all active Dialogs; return false to stop.
func (m *DialogManager) Range(fn func(callID string, d *model.Dialog) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, d := range m.dialogs {
		if !fn(id, d) {
			return
		}
	}
}

// Count returns the number of active Dialogs.
func (m *DialogManager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.dialogs)
}

// dialogTag generates a fresh dialog local-tag of the form "sim-XXXXXXXX".
func dialogTag() string {
	const alphabet = "0123456789abcdef"
	var b = make([]byte, 8)
	if _, err := rand.Read(b); err == nil {
		out := make([]byte, 8)
		for i, c := range b {
			out[i] = alphabet[int(c)%len(alphabet)]
		}
		return dialogTagPrefix + "-" + hex.EncodeToString(out)[:8]
	}
	now := time.Now().UnixNano()
	out := fmt.Sprintf("%x", now)
	if len(out) > 8 {
		out = out[len(out)-8:]
	}
	return dialogTagPrefix + "-" + out
}