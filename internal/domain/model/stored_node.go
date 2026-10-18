// Package model — StoredNode.
package model

import (
	"fmt"
	"time"
)

// StoredNode is the persisted shape of one node: its definition row plus
// the optional registration and platform-serving rows that hang off it.
//
// It lives in the domain rather than in the storage adapter so that the
// persistence port can speak it without the port depending on an adapter
// package (design D1 layering: adapter → domain is allowed, the reverse is
// not).
//
// The Has* flags distinguish "this node registers but we failed to rebuild
// the registration" from "this node was never configured to register" —
// only the latter is a legitimate state a caller may write back as-is.
type StoredNode struct {
	// Identity, as stored in the nodes table.
	ID     NodeID
	Kind   NodeKind
	Domain string
	Addr   string
	Vendor string

	// Cascade graph, from the same table.
	CascadeParent   string
	CascadeChildren []string

	// Optional child rows.
	Registration    Registration
	HasRegistration bool
	Platform        PlatformServing
	HasPlatform     bool

	// CatalogPollIntervalMs is the MANSCDP catalog query interval in
	// milliseconds. It lives on the platform row, but a platform-large
	// node that also registers reads it back off the registration row,
	// so the store resolves the two into this one field.
	CatalogPollIntervalMs int

	// CreatedAt / UpdatedAt are the row timestamps the store maintains;
	// writers never set them, readers (the HTTP layer) surface them as
	// read-only fields. A StoredNode built by hand carries zero times,
	// which the HTTP layer renders as the zero RFC3339 value.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Profile rebuilds the live NodeProfile this stored definition describes,
// walking the same builder contract the composition root uses when it
// seeds nodes from YAML. A field that fails to rebuild is an error, never
// a silent drop: a stored definition that loses its registration on the
// way back into memory would come online as a different node than the one
// the user configured.
func (sn StoredNode) Profile() (NodeProfile, error) {
	p, err := NewNodeProfile(sn.ID.String(), sn.Addr, sn.Domain, sn.Vendor)
	if err != nil {
		return NodeProfile{}, fmt.Errorf("model: rebuild profile for %s: %w", sn.ID, err)
	}
	if sn.CascadeParent != "" {
		if p, err = p.WithCascadeParent(sn.CascadeParent); err != nil {
			return NodeProfile{}, fmt.Errorf("model: rebuild profile for %s: %w", sn.ID, err)
		}
	}
	if len(sn.CascadeChildren) > 0 {
		if p, err = p.WithCascadeChildren(sn.CascadeChildren); err != nil {
			return NodeProfile{}, fmt.Errorf("model: rebuild profile for %s: %w", sn.ID, err)
		}
	}
	if sn.HasRegistration {
		if p, err = p.WithRegistration(sn.Registration); err != nil {
			return NodeProfile{}, fmt.Errorf("model: rebuild profile for %s: %w", sn.ID, err)
		}
	}
	if sn.HasPlatform {
		if p, err = p.WithPlatformServing(sn.Platform); err != nil {
			return NodeProfile{}, fmt.Errorf("model: rebuild profile for %s: %w", sn.ID, err)
		}
	}
	return p, nil
}
