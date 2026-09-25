// Package model — Notify.
package model

import (
	"fmt"
	"strings"
)

// The MANSCDP command types a platform understands. A platform reads far
// fewer commands than it writes: for now it accepts a keepalive and a
// catalog query, and answers the latter.
const (
	// CmdTypeKeepalive is the keepalive notify a downstream sends.
	CmdTypeKeepalive = "Keepalive"
	// CmdTypeCatalog is the catalog query or subscription. The MANSCDP
	// CmdType value is the same for both; the distinction is the XML
	// envelope (Query vs Subscribe), which is handled by the codec.
	CmdTypeCatalog = "Catalog"
	// CmdTypeSubscribe is the MANSCDP subscription notify.
	CmdTypeSubscribe = "Subscribe"
	// CmdTypeMediaStatus is the media status notify.
	CmdTypeMediaStatus = "MediaStatus"
	// CmdTypePlaybackControl is the playback control command.
	CmdTypePlaybackControl = "PlaybackControl"
)

// Notify is a MANSCDP notify a platform received: what the peer said,
// reduced to the fields the use case acts on.
//
// It is the incoming counterpart of Keepalive, which is what a device
// sends; Notify is deliberately looser (any command type, any status)
// because a platform must survive reading what other people's devices
// wrote.
type Notify struct {
	cmdType  string
	sn       uint32
	deviceID string
	status   string
}

// NewNotify builds a received notify. CmdType and DeviceID are mandatory:
// without them the platform could neither route the command nor know who
// sent it. SN and Status may be absent — not every command carries a
// meaningful status, and a query without an SN is still worth a log line.
func NewNotify(cmdType, deviceID string, sn uint32, status string) (Notify, error) {
	cmdType = strings.TrimSpace(cmdType)
	if cmdType == "" {
		return Notify{}, fmt.Errorf("model: notify without a command type")
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return Notify{}, fmt.Errorf("model: notify without a device id")
	}
	return Notify{
		cmdType:  cmdType,
		sn:       sn,
		deviceID: deviceID,
		status:   strings.TrimSpace(status),
	}, nil
}

// CmdType returns the MANSCDP command the notify carries.
func (n Notify) CmdType() string { return n.cmdType }

// SN returns the notify sequence number, 0 when it carried none.
func (n Notify) SN() uint32 { return n.sn }

// DeviceID returns the id the notify is about: for a keepalive the sender,
// for a catalog query the platform being asked.
func (n Notify) DeviceID() string { return n.deviceID }

// Status returns the reported status, or "" when the command has none.
func (n Notify) Status() string { return n.status }

// IsKeepalive reports whether the notify is a downstream keepalive.
func (n Notify) IsKeepalive() bool { return n.cmdType == CmdTypeKeepalive }

// IsCatalogQuery reports whether the notify asks the platform for its
// catalog or subscribes to catalog updates. The MANSCDP CmdType is the
// same; the caller must distinguish query from subscription by the XML
// envelope, which the codec handles before calling this.
func (n Notify) IsCatalogQuery() bool { return n.cmdType == CmdTypeCatalog }

// IsMediaStatus reports whether the notify carries media parameters.
func (n Notify) IsMediaStatus() bool { return n.cmdType == CmdTypeMediaStatus }

// IsPlaybackControl reports whether the notify controls playback.
func (n Notify) IsPlaybackControl() bool { return n.cmdType == CmdTypePlaybackControl }

// HasSN reports whether the notify carried a sequence number, which a
// catalog answer has to echo.
func (n Notify) HasSN() bool { return n.sn != 0 }

// HasNotify reports whether n was produced by NewNotify.
func (n Notify) HasNotify() bool { return n.cmdType != "" && n.deviceID != "" }

// String renders a log-safe one-line summary; never contains secrets.
func (n Notify) String() string {
	return fmt.Sprintf("Notify<cmd=%s device_id=%s sn=%d status=%s>",
		n.cmdType, n.deviceID, n.sn, n.status)
}
