// Package manscdp renders MANSCDP+ (GB/T 28181) message bodies. It is the
// first piece of the XML control protocol this simulator needs: the
// keepalive notify a device sends to stay online. Further commands join this
// package as their changes land.
package manscdp

import (
	"encoding/xml"
	"fmt"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// xmlHeader is the declaration platforms expect on every MANSCDP body. It is
// emitted by hand rather than by encoding/xml, which only writes it through
// Marshal (not MarshalIndent) and offers no control over the encoding
// attribute.
const xmlHeader = `<?xml version="1.0" encoding="UTF-8"?>` + "\n"

// CmdTypeKeepalive is the MANSCDP command carried by a keepalive notify.
const CmdTypeKeepalive = "Keepalive"

// keepaliveNotify is the wire shape of a keepalive. Field order is the order
// of the struct, which is what makes the output stable enough for a golden
// test.
type keepaliveNotify struct {
	XMLName  xml.Name `xml:"Notify"`
	CmdType  string   `xml:"CmdType"`
	SN       uint32   `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	Status   string   `xml:"Status"`
}

// KeepaliveCodecAdapter renders keepalive bodies. It holds no state: every
// notify is fully described by its value.
type KeepaliveCodecAdapter struct{}

// NewKeepaliveCodec returns a codec ready to render keepalives.
func NewKeepaliveCodec() *KeepaliveCodecAdapter { return &KeepaliveCodecAdapter{} }

// MarshalKeepalive renders k as a MANSCDP notify, declaration included and
// terminated by a newline. A keepalive that was never built by
// model.NewKeepalive is rejected rather than rendered as an empty notify.
func (c *KeepaliveCodecAdapter) MarshalKeepalive(k model.Keepalive) (string, error) {
	if !k.HasKeepalive() {
		return "", fmt.Errorf("manscdp: keepalive without a device id")
	}
	if !k.HasStatus() {
		return "", fmt.Errorf("manscdp: keepalive without a status")
	}
	body, err := xml.MarshalIndent(keepaliveNotify{
		CmdType:  CmdTypeKeepalive,
		SN:       k.SN(),
		DeviceID: k.DeviceID(),
		Status:   k.Status(),
	}, "", "  ")
	if err != nil {
		// encoding/xml only fails on values it cannot represent; a
		// validated device id cannot trigger it, but the error is
		// wrapped rather than dropped.
		return "", fmt.Errorf("manscdp: marshal keepalive: %w", err)
	}
	return xmlHeader + string(body) + "\n", nil
}

// Compile-time check that the adapter satisfies the domain port.
var _ port.KeepaliveCodec = (*KeepaliveCodecAdapter)(nil)
