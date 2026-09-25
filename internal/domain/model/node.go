// Package model — Node.
package model

import (
	"fmt"
	"strings"
)

// NodeIDLen is the fixed length of a GB/T 28181 identifier: 8-digit centre
// code + 2-digit industry code + 3-digit type code + 1-digit network id +
// 6-digit serial.
const NodeIDLen = 20

// NodeKind enumerates the three identities the simulator can emulate. The
// zero value is Unknown and is never returned by a successful
// ParseNodeID: an unrecognised type-code segment is an error, not a default
// (design D1).
type NodeKind int

const (
	// NodeKindUnknown is the zero value; never produced by ParseNodeID.
	NodeKindUnknown NodeKind = iota
	// NodeKindDevice is a front-end device (camera, DVR, NVR, encoder).
	NodeKindDevice
	// NodeKindPlatformLarge is an upper-level / central platform.
	NodeKindPlatformLarge
	// NodeKindPlatformSmall is a lower-level / access platform.
	NodeKindPlatformSmall
)

// nodeKindByTypeCode maps the 3-digit type-code segment (bytes 11-13 of the
// 20-digit id, 1-based) to a NodeKind. Values follow GB/T 28181-2016 Annex
// B. Segments outside this set are rejected rather than guessed, so Change
// 5/6/7 can add identity-specific rules without silently widening what
// counts as a valid id.
var nodeKindByTypeCode = map[string]NodeKind{
	"111": NodeKindDevice,        // digital video recorder
	"112": NodeKindDevice,        // video server
	"113": NodeKindDevice,        // encoder
	"118": NodeKindDevice,        // network video recorder
	"131": NodeKindDevice,        // IP camera
	"132": NodeKindDevice,        // IP camera
	"200": NodeKindPlatformLarge, // central / upper-level platform
	"216": NodeKindPlatformSmall, // access / lower-level platform
}

// nodeKindNames is the inverse spelling used by configuration and JSON.
var nodeKindNames = map[NodeKind]string{
	NodeKindDevice:        "device",
	NodeKindPlatformLarge: "platform-large",
	NodeKindPlatformSmall: "platform-small",
}

// String returns the configuration/JSON spelling of the kind, or "" for
// NodeKindUnknown.
func (k NodeKind) String() string {
	return nodeKindNames[k]
}

// ParseNodeKind maps the configuration spelling to a NodeKind. The empty
// string is rejected so an omitted `kind:` field cannot pass silently.
func ParseNodeKind(s string) (NodeKind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "device":
		return NodeKindDevice, nil
	case "platform-large":
		return NodeKindPlatformLarge, nil
	case "platform-small":
		return NodeKindPlatformSmall, nil
	case "":
		return NodeKindUnknown, fmt.Errorf("model: empty node kind")
	default:
		return NodeKindUnknown, fmt.Errorf("model: unknown node kind %q", s)
	}
}

// NodeID is an immutable validated 20-digit GB/T 28181 identifier. The raw
// encoding is kept verbatim so String() round-trips exactly what the caller
// supplied.
type NodeID struct {
	raw  string
	kind NodeKind
}

// ParseNodeID validates raw and builds a NodeID. It rejects anything that is
// not exactly 20 ASCII digits, and any type-code segment (bytes 11-13) that
// does not map to a known NodeKind. Every error names the reason and the
// observed length so configuration loaders can report the offending entry.
func ParseNodeID(raw string) (NodeID, error) {
	if len(raw) != NodeIDLen {
		return NodeID{}, fmt.Errorf("model: illegal node id %q: length %d, want %d",
			raw, len(raw), NodeIDLen)
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return NodeID{}, fmt.Errorf(
				"model: illegal node id %q: length %d, byte %d is %q, want digit",
				raw, len(raw), i+1, raw[i:i+1])
		}
	}
	code := raw[10:13]
	kind, ok := nodeKindByTypeCode[code]
	if !ok {
		return NodeID{}, fmt.Errorf(
			"model: illegal node id %q: length %d, type code %q (bytes 11-13) maps to no known kind",
			raw, len(raw), code)
	}
	return NodeID{raw: raw, kind: kind}, nil
}

// String returns the verbatim 20-digit encoding.
func (id NodeID) String() string { return id.raw }

// Kind returns the identity implied by the type-code segment.
func (id NodeID) Kind() NodeKind { return id.kind }

// TypeCode returns the 3-digit type-code segment (bytes 11-13).
func (id NodeID) TypeCode() string {
	if len(id.raw) != NodeIDLen {
		return ""
	}
	return id.raw[10:13]
}

// NodeProfile is an immutable node identity together with everything needed
// to bind its signalling listener: the signalling address, the home domain
// and the vendor string. The kind is derived from the id so it can never
// disagree with the encoding.
type NodeProfile struct {
	id     NodeID
	addr   string
	domain string
	vendor string

	// registration and result are optional: nil means "this node does not
	// register" / "it has not registered yet". They are pointers so the
	// zero NodeProfile keeps its original meaning.
	registration *Registration
	result       *RegistrationResult

	// serving is how a platform node accepts its own downstreams; nil
	// means "not configured", which a platform-large node fills with
	// defaults at start-up.
	serving *PlatformServing

	// cascadeParent is the deviceID of the upstream node this node
	// forwards traffic to when it is not the final destination. Empty
	// means "no upstream" (the top of a topology).
	cascadeParent string
	// cascadeChildren is the ordered list of deviceIDs this node routes
	// traffic to when it receives a message whose destination is not
	// itself. Empty means "direct only" — no forwarding is configured.
	cascadeChildren []string
}

// NewNodeProfile validates id with ParseNodeID and requires a non-empty
// signalling address and home domain. Vendor is optional (empty allowed).
func NewNodeProfile(id, addr, domain, vendor string) (NodeProfile, error) {
	nodeID, err := ParseNodeID(id)
	if err != nil {
		return NodeProfile{}, err
	}
	if strings.TrimSpace(addr) == "" {
		return NodeProfile{}, fmt.Errorf("model: empty signalling address for node %s", id)
	}
	if strings.TrimSpace(domain) == "" {
		return NodeProfile{}, fmt.Errorf("model: empty home domain for node %s", id)
	}
	return NodeProfile{id: nodeID, addr: addr, domain: domain, vendor: vendor}, nil
}

// ID returns the node identity.
func (p NodeProfile) ID() NodeID { return p.id }

// Kind returns the identity kind (delegates to the id).
func (p NodeProfile) Kind() NodeKind { return p.id.Kind() }

// Addr returns the signalling address the node listens on.
func (p NodeProfile) Addr() string { return p.addr }

// Domain returns the home domain the node registers against.
func (p NodeProfile) Domain() string { return p.domain }

// Vendor returns the vendor string; may be empty.
func (p NodeProfile) Vendor() string { return p.vendor }

// WithAddr returns a copy with a new signalling address.
func (p NodeProfile) WithAddr(addr string) (NodeProfile, error) {
	if strings.TrimSpace(addr) == "" {
		return p, fmt.Errorf("model: empty signalling address for node %s", p.id)
	}
	cp := p
	cp.addr = addr
	return cp, nil
}

// WithDomain returns a copy with a new home domain.
func (p NodeProfile) WithDomain(domain string) (NodeProfile, error) {
	if strings.TrimSpace(domain) == "" {
		return p, fmt.Errorf("model: empty home domain for node %s", p.id)
	}
	cp := p
	cp.domain = domain
	return cp, nil
}

// WithVendor returns a copy with a new vendor string.
func (p NodeProfile) WithVendor(vendor string) NodeProfile {
	cp := p
	cp.vendor = vendor
	return cp
}

// WithRegistration returns a copy that registers with the platform
// described by reg. An unconstructed (zero) Registration is refused, so a
// mis-built configuration surfaces here instead of silently disabling
// registration.
func (p NodeProfile) WithRegistration(reg Registration) (NodeProfile, error) {
	if !reg.HasRegistration() {
		return p, fmt.Errorf("model: empty registration for node %s", p.id)
	}
	cp := p
	cp.registration = &reg
	return cp, nil
}

// Registration returns how the node registers, and whether it registers at
// all. The second result is false for a node never configured to.
func (p NodeProfile) Registration() (Registration, bool) {
	if p.registration == nil {
		return Registration{}, false
	}
	return *p.registration, true
}

// WithRegistrationResult returns a copy carrying the outcome of a
// registration. An unconstructed result is refused.
func (p NodeProfile) WithRegistrationResult(res RegistrationResult) (NodeProfile, error) {
	if !res.HasResult() {
		return p, fmt.Errorf("model: empty registration result for node %s", p.id)
	}
	cp := p
	cp.result = &res
	return cp, nil
}

// RegistrationResult returns the last registration outcome, and whether
// there is one.
func (p NodeProfile) RegistrationResult() (RegistrationResult, bool) {
	if p.result == nil {
		return RegistrationResult{}, false
	}
	return *p.result, true
}

// WithPlatformServing returns a copy that serves downstreams as described.
// An unconstructed serving description is refused.
func (p NodeProfile) WithPlatformServing(serving PlatformServing) (NodeProfile, error) {
	if !serving.HasServing() {
		return p, fmt.Errorf("model: empty platform serving for node %s", p.id)
	}
	cp := p
	cp.serving = &serving
	return cp, nil
}

// PlatformServing returns how the node serves its downstreams, and whether
// it was configured to. The second result is false for a node that never
// declared a `platform:` section; a platform-large node then falls back to
// DefaultPlatformServing(domain).
func (p NodeProfile) PlatformServing() (PlatformServing, bool) {
	if p.serving == nil {
		return PlatformServing{}, false
	}
	return *p.serving, true
}

// WithCascadeParent returns a copy that forwards upstream traffic to
// parentID. The value is validated with ParseNodeID so a typo surfaces at
// configuration time, not on the wire.
func (p NodeProfile) WithCascadeParent(parentID string) (NodeProfile, error) {
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return p, fmt.Errorf("model: empty cascade parent for node %s", p.id)
	}
	if _, err := ParseNodeID(parentID); err != nil {
		return p, err
	}
	cp := p
	cp.cascadeParent = parentID
	return cp, nil
}

// CascadeParent returns the upstream deviceID, or "" when this node has no
// upstream configured.
func (p NodeProfile) CascadeParent() string { return p.cascadeParent }

// WithCascadeChildren returns a copy whose downstream routing order is
// children. Duplicates are rejected at configuration time: a duplicated
// child would make PreferredPath resolution ambiguous.
func (p NodeProfile) WithCascadeChildren(children []string) (NodeProfile, error) {
	seen := make(map[string]struct{}, len(children))
	cleaned := make([]string, 0, len(children))
	for _, c := range children {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, err := ParseNodeID(c); err != nil {
			return p, err
		}
		if _, dup := seen[c]; dup {
			return p, fmt.Errorf("model: duplicate cascade child %s for node %s", c, p.id)
		}
		seen[c] = struct{}{}
		cleaned = append(cleaned, c)
	}
	if len(cleaned) == 0 {
		return p, nil // clearing the list is a valid no-op
	}
	cp := p
	cp.cascadeChildren = cleaned
	return cp, nil
}

// CascadeChildren returns the downstream routing order; nil when none.
func (p NodeProfile) CascadeChildren() []string {
	if len(p.cascadeChildren) == 0 {
		return nil
	}
	return append([]string(nil), p.cascadeChildren...)
}

// HasCascadeParent reports whether the node forwards traffic upstream.
func (p NodeProfile) HasCascadeParent() bool { return p.cascadeParent != "" }

// HasCascadeChildren reports whether the node routes traffic downstream.
func (p NodeProfile) HasCascadeChildren() bool { return len(p.cascadeChildren) > 0 }

// HasCascadeRouting reports whether the node participates in any cascade
// topology at all.
func (p NodeProfile) HasCascadeRouting() bool {
	return p.HasCascadeParent() || p.HasCascadeChildren()
}

// String renders a log-safe one-line summary; never contains secrets.
func (p NodeProfile) String() string {
	return fmt.Sprintf("NodeProfile<id=%s kind=%s addr=%s domain=%s vendor=%q>",
		p.id.String(), p.id.Kind(), p.addr, p.domain, p.vendor)
}

// Node is an immutable node: an identity plus a lifecycle status. Status
// changes go through WithStatus, which enforces the transition table.
type Node struct {
	profile NodeProfile
	status  Status
}

// NewNode returns a node in StatusIdle.
func NewNode(profile NodeProfile) Node {
	return Node{profile: profile, status: StatusIdle}
}

// Profile returns the node identity and signalling details.
func (n Node) Profile() NodeProfile { return n.profile }

// Status returns the current lifecycle status.
func (n Node) Status() Status { return n.status }

// ID is a convenience accessor for the node id.
func (n Node) ID() NodeID { return n.profile.ID() }

// WithStatus returns a copy advanced to to. An illegal transition returns
// ErrIllegalTransition (judgeable with errors.Is) and leaves the receiver
// untouched; it never panics.
func (n Node) WithStatus(to Status) (Node, error) {
	if !n.status.CanTransition(to) {
		return n, fmt.Errorf("%w: %s -> %s", ErrIllegalTransition, n.status, to)
	}
	cp := n
	cp.status = to
	return cp, nil
}

// Registration is a convenience accessor for the profile's registration
// settings.
func (n Node) Registration() (Registration, bool) { return n.profile.Registration() }

// RegistrationResult is a convenience accessor for the profile's last
// registration outcome.
func (n Node) RegistrationResult() (RegistrationResult, bool) {
	return n.profile.RegistrationResult()
}

// PlatformServing is a convenience accessor for the profile's serving
// configuration.
func (n Node) PlatformServing() (PlatformServing, bool) { return n.profile.PlatformServing() }

// WithRegistrationResult returns a copy of the node whose profile records
// res. It is separate from WithStatus because a registration outcome is
// data, not a lifecycle step: recording one never moves the status.
func (n Node) WithRegistrationResult(res RegistrationResult) (Node, error) {
	profile, err := n.profile.WithRegistrationResult(res)
	if err != nil {
		return n, err
	}
	cp := n
	cp.profile = profile
	return cp, nil
}

// String renders a log-safe one-line summary.
func (n Node) String() string {
	return fmt.Sprintf("Node<id=%s status=%s>", n.ID().String(), n.status)
}
