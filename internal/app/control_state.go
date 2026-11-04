package app

import (
	"strings"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// DeviceControlState is the simulator's observable device-side control
// surface: PTZ pose, iris, preset, recording flag, and I-frame requests.
// It is process memory only — restarts clear it.
type DeviceControlState struct {
	Pan           float64
	Tilt          float64
	Zoom          float64
	Moving        string // last non-stop direction, empty when stopped
	Iris          int    // relative open-ness; irisin ++, irisout --
	PresetID      int
	Recording     bool
	IFrameCount   int
	LastIFrameAt  time.Time
}

func (a *Acceptor) ensureControlState() {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	if a.controlState == nil {
		a.controlState = make(map[string]*DeviceControlState)
	}
}

// ControlState returns a copy of the control state for id, or zero/false.
func (a *Acceptor) ControlState(id model.NodeID) (DeviceControlState, bool) {
	a.ensureControlState()
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	st, ok := a.controlState[id.String()]
	if !ok || st == nil {
		return DeviceControlState{}, false
	}
	return *st, true
}

func (a *Acceptor) mutateControlState(id model.NodeID, fn func(*DeviceControlState)) DeviceControlState {
	a.ensureControlState()
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	key := id.String()
	st := a.controlState[key]
	if st == nil {
		st = &DeviceControlState{}
		a.controlState[key] = st
	}
	fn(st)
	return *st
}

func applyPTZDirection(st *DeviceControlState, direction string) {
	d := strings.ToLower(strings.TrimSpace(direction))
	switch d {
	case "stop":
		st.Moving = ""
		return
	case "up":
		st.Tilt++
	case "down":
		st.Tilt--
	case "left":
		st.Pan--
	case "right":
		st.Pan++
	case "upleft":
		st.Tilt++
		st.Pan--
	case "upright":
		st.Tilt++
		st.Pan++
	case "downleft":
		st.Tilt--
		st.Pan--
	case "downright":
		st.Tilt--
		st.Pan++
	case "zoomin", "in":
		st.Zoom++
	case "zoomout", "out":
		st.Zoom--
	case "irisin", "iris-open":
		st.Iris++
	case "irisout", "iris-close":
		st.Iris--
	case "focusnear", "focus-near", "focusfar", "focus-far":
		// Focus has no separate pose axis; still mark as moving for stop.
	default:
		// Unknown encoded PTZCmd: treat as generic move for observability.
	}
	if d != "" && d != "stop" {
		st.Moving = d
	}
}
