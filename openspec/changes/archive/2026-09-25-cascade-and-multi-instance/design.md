# Design

## Context

Each `Node` instance already has its own `SIPClient`, `SIPServer`, `NodeProfile`, and `NodeRegistry`. The transport layer injects headers per `NodeProfile`. The existing `Node` model allows arbitrary identity types (device / platform-small / platform-large) and per-node port allocation.

See proposal.md for motivation.

## Goals / Non-Goals

**Goals:**
- Enable multi-level cascade routing (A → B → C → D) using GB/T 28181 §6 X-RoutePath / X-PreferredPath headers.
- Allow multiple Node instances to coexist in one process with independent state and ports.
- Forward upstream/downstream notifies across cascade links with correct header state.
- Detect and prevent routing loops via path trace analysis.

**Non-Goals:**
- Multi-process or network-transparent node distribution (single-process only).
- Replacing existing sip/transport header injection — this change extends it, not replaces.
- TLS or certificate handling for cascade links.

## Decisions

### Decision: Add `CascadeRoute` model

```go
type CascadeRoute struct {
    FromDeviceID string
    ToDeviceID   string
    HopIndex     int
    RoutePath    []string // deviceIDs visited in order
    PreferredPath []string // ordered list of desired downstream hops
}
```

**Rationale:** X-RoutePath and X-PreferredPath are simple comma-separated deviceID lists. Modeling them as string slices keeps parsing trivial and serialization direct. No need for a complex routing table.

**Alternatives considered:**
- Dedicated path-manipulation library — rejected: too heavy, only 2 headers.
- Modify `SIPTransaction` to carry route state — rejected: introduces statefulness where stateless header passthrough suffices.

### Decision: Use `NodeRegistry` for topology lookup

Extend `NodeProfile` with `CascadeParent` (single deviceID) and `CascadeChildren` (deviceID list). `NodeRegistry.RegisterNode` validates that no circular parent relationships are created at registration time.

**Rationale:** A registry is already the single source of truth for "which node owns which deviceID". Adding cascade fields to the profile keeps topology close to identity without a new subsystem.

**Alternatives considered:**
- Separate `CascadeTopology` struct — rejected: creates a parallel registry with sync risk.
- Dynamic topology via callbacks — rejected: unnecessary for a simulator with static YAML-defined topologies.

### Decision: Implement forwarding in a `CascadeHandler` port

Add a new `CascadeHandler` to `domain/port/cascade.go`. The transport layer calls `handler.Forward(nodeID, msg)` instead of directly calling `client.SendRequest`. The handler resolves the next hop from X-PreferredPath or CascadeChildren and injects X-RoutePath before forwarding.

**Rationale:** Decouples cascade logic from SIP transport, keeps `transport.go` focused on wire-level concerns, and makes the handler independently testable.

**Alternatives considered:**
- Inline forwarding in `transport.go` — rejected: would make transport aware of topology details.
- Rely on existing `Node.SendRequest` — rejected: no path state awareness.

### Decision: Loop detection via path-set membership

Before forwarding, the CascadeHandler checks whether the current node's deviceID already appears in X-RoutePath. If so, it halts forwarding and returns a 482 or logs the drop.

**Rationale:** Set membership check is O(n) but the path is short (< 10 hops in real GB28181). This is deterministic, requires no external state, and catches both circular and self-targeted loops.

**Alternatives considered:**
- Time-based loop detection (TTL) — rejected: unnecessary for a simulator with no real clock drift.
- Global routing state table — rejected: overkill for single-process.

## Risks / Trade-offs

| Risk | Mitigation |
|------|-----------|
| CascadeChildren / CascadeParent create a circular dependency at registration | NodeRegistry.RegisterNode validates acyclic graph at insert; returns error on cycle |
| X-RoutePath parsing edge cases (whitespace, trailing comma) | Use a dedicated `ParseCascadeHeaders` helper with robust string splitting and trimming |
| Multi-node port conflicts on single host | Each NodeProfile already specifies a distinct local port; add validation at registration |
| Upstream notify forwarding creates duplicate messages | Only forward if the message's target deviceID is NOT the current node's ID |

## Migration Plan

1. Extend `NodeProfile` with `CascadeParent` and `CascadeChildren` fields (no breaking change — new fields are optional).
2. Add `CascadeRoute` model to `internal/domain/model/cascade.go`.
3. Add `CascadeHandler` port to `internal/domain/port/cascade.go`.
4. Implement cascade handler in `internal/adapter/cascade/handler.go`.
5. Wire into `siptransport.Transport` as a post-send hook for outbound messages.
6. Add integration tests: 3-node topology (platform-large → platform-small → device), verify X-RoutePath and X-PreferredPath end-to-end.

## Open Questions

- Should X-PreferredPath support fallback to direct routing if the preferred path is unreachable? → Decide in tasks phase.
- Should cascade headers be stripped at the final destination, or preserved for logging? → Decide in tasks phase.
