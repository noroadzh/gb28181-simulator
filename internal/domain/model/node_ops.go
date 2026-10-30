package model

// Runtime operations state added by device-control-coverage: the arming
// state, the configured heartbeat interval override, the last upgrade target
// and the last reboot time. All of it is in-memory and deliberately not
// persisted — a restart returns the node to its configured defaults.

import "time"

// WithAlarmArmed returns a copy with the arming state set.
func (p NodeProfile) WithAlarmArmed(armed bool) NodeProfile {
	cp := p
	cp.alarmArmed = armed
	cp.alarmArmedSet = true
	return cp
}

// AlarmArmed reports the arming state and whether it was ever set. A node
// that was never armed answers "disarmed" without pretending it was told.
func (p NodeProfile) AlarmArmed() (armed, set bool) { return p.alarmArmed, p.alarmArmedSet }

// WithHeartbeatOverride records a heartbeat interval delivered through
// ConfigDownload. A zero duration clears the override.
func (p NodeProfile) WithHeartbeatOverride(d time.Duration) NodeProfile {
	cp := p
	if d <= 0 {
		cp.heartbeatOverride = nil
		return cp
	}
	v := d
	cp.heartbeatOverride = &v
	return cp
}

// HeartbeatOverride returns the configured heartbeat interval, if any.
func (p NodeProfile) HeartbeatOverride() (time.Duration, bool) {
	if p.heartbeatOverride == nil {
		return 0, false
	}
	return *p.heartbeatOverride, true
}

// WithUpgradeTarget records the firmware version a platform asked for.
func (p NodeProfile) WithUpgradeTarget(version string) NodeProfile {
	cp := p
	cp.upgradeTarget = version
	return cp
}

// UpgradeTarget returns the last requested firmware version, if any.
func (p NodeProfile) UpgradeTarget() string { return p.upgradeTarget }

// WithReboot marks the node as having been asked to reboot at now.
func (p NodeProfile) WithReboot(now time.Time) NodeProfile {
	cp := p
	t := now
	cp.lastReboot = &t
	return cp
}

// LastReboot returns when a reboot was last requested, if ever.
func (p NodeProfile) LastReboot() (time.Time, bool) {
	if p.lastReboot == nil {
		return time.Time{}, false
	}
	return *p.lastReboot, true
}
