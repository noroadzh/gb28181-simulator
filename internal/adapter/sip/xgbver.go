// XGBVer header carries the X-GB-Ver used by Change 11's GB/T 28181-2022
// extension. It is a single-line extension header defined in §H.2 of the
// 2022 amendment.
package sip

import (
	"github.com/ghettovoice/gosip/sip"
)

// XGBVerHeader is a string-typed header following the UserAgentHeader
// pattern from gosip's own headers.go.
type XGBVerHeader string

func (h *XGBVerHeader) Name() string      { return "X-GB-Ver" }
func (h *XGBVerHeader) Value() string     { return string(*h) }
func (h *XGBVerHeader) String() string    { return "X-GB-Ver: " + string(*h) }
func (h *XGBVerHeader) Clone() sip.Header { v := *h; return &v }
func (h *XGBVerHeader) Equals(o interface{}) bool {
	switch v := o.(type) {
	case *XGBVerHeader:
		return v != nil && *v == *h
	case XGBVerHeader:
		return v == *h
	}
	return false
}

func newXGBVer(v string) sip.Header { h := XGBVerHeader(v); return &h }
