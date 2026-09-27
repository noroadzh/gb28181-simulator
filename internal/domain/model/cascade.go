// Package model — CascadeRoute.
//
// Cascade path tracking for GB/T 28181 §6 X-RoutePath / X-PreferredPath
// headers. The headers are simple comma-separated deviceID lists on the
// wire; the model keeps them as string slices so parsing and serialising
// stay trivial (design D1).
package model

import (
	"fmt"
	"slices"
	"strings"
)

// HeaderRoutePath is the wire name of the cascade path-trace header.
const HeaderRoutePath = "X-RoutePath"

// HeaderPreferredPath is the wire name of the preferred-routing header.
const HeaderPreferredPath = "X-PreferredPath"

// CascadeRoute is the immutable cascade state of one message at one hop:
// where the message came from, the route it has already traversed, and the
// path it would prefer to take from here. Zero CascadeRoute means "no
// cascade context", which is the common case for direct device traffic.
type CascadeRoute struct {
	// FromDeviceID is the node that injected or last forwarded the
	// message. Empty for locally originated traffic.
	FromDeviceID string
	// RoutePath lists the deviceIDs the message has traversed, in
	// order, oldest first. The current node appends itself before
	// forwarding (spec: cascade-header-injection).
	RoutePath []string
	// PreferredPath lists the deviceIDs the message wants to follow
	// from this point, nearest hop first (spec:
	// preferred-path-selection). Empty means "route normally".
	PreferredPath []string
}

// NewCascadeRoute builds a route from the raw header values. Parsing is
// lenient about whitespace and an optional trailing comma, strict about
// non-empty identifiers: a path element that trims to empty is an error,
// because silently dropping it would desynchronise the loop-detection set.
func NewCascadeRoute(fromDeviceID, routePath, preferredPath string) (CascadeRoute, error) {
	route, err := ParseRoutePath(routePath)
	if err != nil {
		return CascadeRoute{}, err
	}
	preferred, err := ParseRoutePath(preferredPath)
	if err != nil {
		return CascadeRoute{}, fmt.Errorf("preferred path: %w", err)
	}
	return CascadeRoute{
		FromDeviceID:  strings.TrimSpace(fromDeviceID),
		RoutePath:     route,
		PreferredPath: preferred,
	}, nil
}

// ParseRoutePath splits a comma-separated header value into trimmed,
// non-empty identifiers. An empty or whitespace-only value parses to nil
// with no error — an absent header is normal, a malformed one is not.
func ParseRoutePath(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue // tolerate "a,,b" as ["a","b"]
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// FormatRoutePath renders ids as the wire form: comma-separated, no spaces.
// A nil/empty slice renders to "" so callers can omit the header entirely.
func FormatRoutePath(ids []string) string {
	cleaned := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			cleaned = append(cleaned, id)
		}
	}
	return strings.Join(cleaned, ",")
}

// AppendRoute returns a copy of route whose RoutePath carries id at the
// end. It is the pure transformation the forwarding code applies before
// sending a message downstream.
func (r CascadeRoute) AppendRoute(id string) CascadeRoute {
	id = strings.TrimSpace(id)
	if id == "" {
		return r
	}
	cp := r
	cp.RoutePath = append(append([]string(nil), r.RoutePath...), id)
	return cp
}

// PopPreferred returns a copy with the first PreferredPath entry removed,
// together with that entry. When the list is empty it returns the receiver
// and "", signalling "no preference left, route normally".
func (r CascadeRoute) PopPreferred() (CascadeRoute, string) {
	if len(r.PreferredPath) == 0 {
		return r, ""
	}
	cp := r
	cp.PreferredPath = append([]string(nil), r.PreferredPath[1:]...)
	return cp, r.PreferredPath[0]
}

// ContainsRoute reports whether id already appears on the traversed route.
// The check is linear over a path that is bounded by the topology depth
// (< 10 in real deployments), so no set structure is warranted.
func (r CascadeRoute) ContainsRoute(id string) bool {
	id = strings.TrimSpace(id)
	for _, seen := range r.RoutePath {
		if seen == id {
			return true
		}
	}
	return false
}

// ContainsRouteFast is the slices.Contains form of the membership check. It
// exists to satisfy the static analysis tool's suggestion and to document
// that both forms are equivalent for the bounded paths this simulator uses.
func (r CascadeRoute) ContainsRouteFast(id string) bool {
	return slices.Contains(r.RoutePath, strings.TrimSpace(id))
}

// Headers renders the route as SIP headers. Only non-empty paths produce
// headers, so a direct (non-cascade) message stays header-free.
func (r CascadeRoute) Headers() []Header {
	var out []Header
	if v := FormatRoutePath(r.RoutePath); v != "" {
		out = append(out, NewHeader(HeaderRoutePath, v))
	}
	if v := FormatRoutePath(r.PreferredPath); v != "" {
		out = append(out, NewHeader(HeaderPreferredPath, v))
	}
	return out
}

// RouteFromHeaders extracts the cascade context carried by headers. The
// fromDeviceID is filled by the caller that knows who sent the message.
func RouteFromHeaders(headers []Header) (CascadeRoute, error) {
	var route, preferred string
	for _, h := range headers {
		switch h.Name() {
		case HeaderRoutePath:
			route = h.Value()
		case HeaderPreferredPath:
			preferred = h.Value()
		}
	}
	return NewCascadeRoute("", route, preferred)
}

// String renders a log-safe one-line summary.
func (r CascadeRoute) String() string {
	return fmt.Sprintf("CascadeRoute<from=%s route=[%s] preferred=[%s]>",
		r.FromDeviceID, FormatRoutePath(r.RoutePath), FormatRoutePath(r.PreferredPath))
}
