// Package model — Keepalive.
package model

import "fmt"

// KeepaliveStatusOK is the only status a healthy device reports in its
// keepalive notify. GB/T 28181 keeps the element for future error states;
// this simulator only ever sends a healthy one.
const KeepaliveStatusOK = "OK"

// Keepalive is one GB/T 28181 keepalive: the payload of the periodic
// MANSCDP notify a device sends to prove it is still there. It is immutable
// — the sequence number belongs to the sending session, not to the value, so
// the caller stamps each one as it goes out.
type Keepalive struct {
	deviceID string // 20-digit device id
	sn       uint32 // notify sequence number, monotonically increasing
	status   string
}

// NewKeepalive validates deviceID and returns a healthy keepalive carrying
// sn. The status is always OK: a device that cannot report OK is not
// simulated by sending a sad keepalive, it is faulted instead.
func NewKeepalive(deviceID string, sn uint32) (Keepalive, error) {
	id, err := ParseNodeID(deviceID)
	if err != nil {
		return Keepalive{}, fmt.Errorf("model: keepalive device id: %w", err)
	}
	return Keepalive{deviceID: id.String(), sn: sn, status: KeepaliveStatusOK}, nil
}

// HasKeepalive reports whether k was produced by NewKeepalive.
func (k Keepalive) HasKeepalive() bool { return k.deviceID != "" }

// HasStatus reports whether k carries a status to report.
func (k Keepalive) HasStatus() bool { return k.status != "" }

// DeviceID returns the 20-digit id the notify is about.
func (k Keepalive) DeviceID() string { return k.deviceID }

// SN returns the notify sequence number.
func (k Keepalive) SN() uint32 { return k.sn }

// Status returns the reported status: always "OK".
func (k Keepalive) Status() string { return k.status }

// String renders a log-safe one-line summary.
func (k Keepalive) String() string {
	return fmt.Sprintf("Keepalive<device_id=%s sn=%d status=%s>", k.deviceID, k.sn, k.status)
}
