// Package model — DownstreamDevice.
package model

import (
	"fmt"
	"strings"
	"time"
)

// DownstreamDevice is one row of a platform node's online device table: a
// device that registered successfully and has not left since.
//
// It is a value object like Registration: immutable, constructed through
// NewDownstreamDevice, and updated by returning a copy. It carries no
// credentials — a platform knows how to reach a device, never its secret.
type DownstreamDevice struct {
	deviceID  string
	addr      string // "host:port" the registration arrived from
	contact   string // the Contact the device declared
	transport string // "udp" / "tcp"
	gbVersion string // e.g. "2016"; empty when the device stated none

	registeredAt time.Time
	expiresAt    time.Time
	lastSeenAt   time.Time
	granted      uint32 // seconds the platform granted
}

// DownstreamDeviceParams is the flat input for NewDownstreamDevice. Only
// DeviceID and Addr are required; the rest describe the registration and
// may be empty.
type DownstreamDeviceParams struct {
	DeviceID  string
	Addr      string
	Contact   string
	Transport string
	GBVersion string
	Now       time.Time
}

// NewDownstreamDevice builds a table row for a device that just registered.
// DeviceID and Addr are mandatory: without them the platform could neither
// name nor answer the device.
func NewDownstreamDevice(p DownstreamDeviceParams) (DownstreamDevice, error) {
	deviceID := strings.TrimSpace(p.DeviceID)
	if deviceID == "" {
		return DownstreamDevice{}, fmt.Errorf("model: empty device id for a downstream device")
	}
	addr := strings.TrimSpace(p.Addr)
	if addr == "" {
		return DownstreamDevice{}, fmt.Errorf("model: empty source address for device %s", deviceID)
	}
	now := p.Now
	if now.IsZero() {
		return DownstreamDevice{}, fmt.Errorf("model: zero registration time for device %s", deviceID)
	}
	return DownstreamDevice{
		deviceID:     deviceID,
		addr:         addr,
		contact:      strings.TrimSpace(p.Contact),
		transport:    strings.TrimSpace(p.Transport),
		gbVersion:    strings.TrimSpace(p.GBVersion),
		registeredAt: now,
		lastSeenAt:   now,
	}, nil
}

// DeviceID returns the downstream's GB/T 28181 id.
func (d DownstreamDevice) DeviceID() string { return d.deviceID }

// Addr returns the address the registration arrived from ("host:port").
func (d DownstreamDevice) Addr() string { return d.addr }

// Contact returns the Contact the device declared, or "" when it sent none.
func (d DownstreamDevice) Contact() string { return d.contact }

// Transport returns "udp" / "tcp", or "" when unknown.
func (d DownstreamDevice) Transport() string { return d.transport }

// GBVersion returns the protocol version the device stated, or "".
func (d DownstreamDevice) GBVersion() string { return d.gbVersion }

// RegisteredAt returns when the current registration was granted.
func (d DownstreamDevice) RegisteredAt() time.Time { return d.registeredAt }

// ExpiresAt returns when the granted lifetime lapses; zero until one is
// granted.
func (d DownstreamDevice) ExpiresAt() time.Time { return d.expiresAt }

// LastSeenAt returns the last time the device was heard from.
func (d DownstreamDevice) LastSeenAt() time.Time { return d.lastSeenAt }

// GrantedExpiry returns the lifetime, in seconds, the platform granted.
func (d DownstreamDevice) GrantedExpiry() uint32 { return d.granted }

// HasDevice reports whether d was produced by NewDownstreamDevice.
func (d DownstreamDevice) HasDevice() bool { return d.deviceID != "" }

// WithGranted returns a copy whose registration was granted for granted
// seconds at at: the row's registration, expiry and last-seen times all
// move together, because a successful registration is the freshest evidence
// that the device is there.
func (d DownstreamDevice) WithGranted(granted uint32, at time.Time) DownstreamDevice {
	cp := d
	cp.granted = granted
	cp.registeredAt = at
	cp.lastSeenAt = at
	if granted == 0 {
		cp.expiresAt = time.Time{}
		return cp
	}
	cp.expiresAt = at.Add(time.Duration(granted) * time.Second)
	return cp
}

// WithSeen returns a copy whose last-seen time moved to at. Used when a
// device checks in without re-registering — a keepalive — which is evidence
// that it is there but not a reason to extend the lifetime it was granted.
func (d DownstreamDevice) WithSeen(at time.Time) DownstreamDevice {
	cp := d
	cp.lastSeenAt = at
	return cp
}

// WithContact returns a copy carrying the Contact the device declared.
func (d DownstreamDevice) WithContact(contact string) DownstreamDevice {
	cp := d
	cp.contact = strings.TrimSpace(contact)
	return cp
}

// String renders a log-safe one-line summary; never contains secrets.
func (d DownstreamDevice) String() string {
	return fmt.Sprintf("DownstreamDevice<id=%s addr=%s expires_in=%ds>", d.deviceID, d.addr, d.granted)
}
