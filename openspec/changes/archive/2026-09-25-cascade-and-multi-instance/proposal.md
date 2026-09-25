# Proposal

## Why

The simulator currently supports individual device and platform nodes but lacks a mechanism for:
1. Tracing and forwarding SIP messages across a multi-hop cascade chain (A → B → C).
2. Simulating complex topologies with multiple interconnected nodes simultaneously.

GB/T 28181-2016 §6 defines `X-RoutePath` and `X-PreferredPath` headers for cascade routing. Without these, realistic multi-level GB28181 cascades cannot be exercised or tested.

## What Changes

- Implement `X-RoutePath` and `X-PreferredPath` header injection and forwarding for SIP REGISTER/INVITE/MESSAGE/BYE across cascade hops.
- Add a `CascadeRouter` port interface and implementation that rewrites route headers per hop and selects the preferred downstream path.
- Extend the `Node` model with a `RouteTable` and `CascadeParent` field to support arbitrary topology lookup.
- Add `NodeProfile.CascadeParent` and `NodeProfile.CascadeChildren` config fields, enabling YAML-defined multi-level topologies (e.g., `platform-large → platform-small → device`).
- Implement cascade forwarding logic for MANSCDP MESSAGE bodies (Catalog, DeviceInfo, RecordInfo, Alarm, MediaStatus) through the cascade chain.
- Add integration tests with a 3-node topology to verify header correctness and end-to-end message propagation.

## Capabilities

### New Capabilities
- `cascade-routing`: X-RoutePath / X-PreferredPath injection, hop-by-hop forwarding, and preferred path selection across multi-level cascades.
- `multi-node-topology`: YAML-defined multi-node topologies with arbitrary cascade relationships; NodeRegistry extended with topology-aware lookup.

### Modified Capabilities
- `node-abstraction`: Add `RouteTable` and `CascadeParent` fields to `Node` model; extend `NodeProfile` with cascade configuration fields.
- `platform-small-node`: After cascade routing lands, a platform-small MUST forward downstream notifies (Alarm, MediaStatus) to its upstream parent when `CascadeParent` is set.

## Impact

- **New files**: `domain/port/cascade.go`, `domain/model/cascade.go`, `adapter/cascade/*`, `app/cascade/*`.
- **Modified files**: `domain/model/node.go`, `domain/model/node_profile.go`, `internal/adapter/siptransport/transport.go` (header injection hook).
- **New tests**: `cascade_test.go` with 3-node topology fixture.
- **Config**: `NodeProfile` gains `cascade_parent`, `cascade_children` fields.
