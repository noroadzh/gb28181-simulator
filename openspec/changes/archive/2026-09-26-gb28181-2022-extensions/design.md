# Design

## Context

The simulator already covers the full 2016 feature set: SIP/SDP/Digest
transport, MANSCDP encode/decode, PS/RTP media pipeline, four media sources,
node abstraction with three identities, dynamic catalog/alarm/record/position
features, and cascade routing (X-RoutePath / X-PreferredPath). The transport
layer already parses `X-GB-Ver` on inbound REGISTER, stores it in
`DownstreamDevice.GBVersion`, and the SIP builder can attach it outbound —
but no behaviour currently depends on the recorded version, and the 2022-only
MANSCDP command family does not exist yet.

See proposal.md for motivation and scope.

## Goals / Non-Goals

**Goals:**
- Close the X-GB-Ver loop: echo the negotiated version in the registration
  `200 OK` and make the stored `GBVersion` the single input for
  version-gated behaviour.
- Implement the 2022 MANSCDP command family as separate, individually
  testable acceptor branches: HomePosition, CruiseTrackList, SnapShot,
  storage-card status in DeviceStatus, TeleBoot, precise PTZ position,
  Annex O section type in catalog items.
- Add SDP media-capability modules (H.265 / AAC / G.722.1) that are
  declared only when enabled and only toward 2022 peers.
- Keep 2016 wire behaviour byte-identical to today's output.

**Non-Goals:**
- GB 35114 security (roadmap #12), exception flows (roadmap #13),
  Web UI (roadmap #14), scenario engine (roadmap #15).
- Binary image transfer for SnapShot: the simulator records the capture
  event; it never produces a JPEG payload.
- Firmware transfer for DeviceUpgrade/TeleBoot: TeleBoot is acknowledged and
  logged; no actual SIP stack restart occurs beyond a state-machine tick.
- Reworking catalog subscription: `dynamic-sim-features` already delivers
  SUBSCRIBE/NOTIFY with Expires management; this change only tags items.

## Decisions

### Decision: Version gate as a small helper on DownstreamDevice

Expose `func (d DownstreamDevice) Is2022() bool` (true iff
`GBVersion == "2022"`) instead of scattering string comparisons.

**Rationale:** One predicate keeps the gate semantic explicit, is trivially
unit-testable, and leaves room to add e.g. `"2022-1"` later without touching
call sites.

**Alternatives considered:**
- Numeric version enum — rejected: the standard identifies the version by
  header string; an enum adds a mapping layer for no benefit.
- Pass the raw string everywhere — rejected: invites `== "2022"` typos.

### Decision: Echo X-GB-Ver in the registration 200 OK

`handleRegister` appends `model.NewHeader("X-GB-Ver", gbVersion)` to the
response headers when the inbound request carried the header.

**Rationale:** The 2022 amendment (Annex I) requires the platform to declare
the negotiated version in its answer; echoing the request value is the
simplest correct interpretation for a simulator that supports both versions.

**Alternatives considered:**
- Always send `X-GB-Ver: 2022` — rejected: would claim 2022 semantics toward
  2016 peers and break byte-identical 2016 output.
- Configurable per-node version claim — deferred: no current scenario needs
  a node to advertise a version different from the peer's request.

### Decision: 2022 command family in one model + codec file pair

`internal/domain/model/gb2022.go` holds `HomePosition`, `CruiseTrack`,
`SnapShotRecord`, and storage-card/precise-PTZ field carriers;
`internal/adapter/manscdp/gb2022.go` holds their XML encode/decode plus
golden fixtures under `testdata/`.

**Rationale:** The 2022 commands share optional-field ergonomics and are
always consumed together by the same acceptor branches. One file pair keeps
the golden tests adjacent and avoids seven near-identical single-type files.

**Alternatives considered:**
- One file per command (mirroring `alarm.go` / `keepalive.go`) — rejected
  for now: each 2022 type is small (2–5 fields); the files would be mostly
  boilerplate. Split can happen later without API change.

### Decision: DeviceStatus/catalog extension via optional struct fields

`DeviceStatusResponse` gains `StorageCardStatus string` (omitted when empty),
catalog items gain `SectionType string` (omitted when empty). Encoders use
`xml:",omitempty"` semantics already established in the codebase.

**Rationale:** Optional fields with omitempty are the established pattern
for 2016 bodies (e.g. MediaStatus position fields from
dynamic-sim-features); reusing it means zero new encoder machinery, and the
byte-identical 2016 guarantee is enforced by the existing golden tests plus
new "field must be absent" assertions.

**Alternatives considered:**
- Separate 2022 response types wrapping the 2016 ones — rejected: doubles
  the type surface for two optional fields.

### Decision: SDP capability modules as profile config + builder option

`NodeProfile.MediaCapabilities []string` (values `H265`, `AAC`, `G7221`,
validated at profile load) drives a new `WithCapabilityModules([]string)`
build option on the SDP builder. The acceptor reads the peer's recorded
version and only passes the modules through when `Is2022()`.

**Rationale:** Keeps the SDP builder version-agnostic (it just renders what
it is given) and concentrates gating in the app layer where the peer's
version is already known. Golden tests can assert both gated and ungated
output deterministically.

**Alternatives considered:**
- Gate inside the SDP builder by inspecting SIP headers — rejected: couples
  the SDP layer to registration state it cannot see.
- Derive capabilities from media-source codec detection — rejected: a source
  may legitimately emit H.264 while the profile advertises an H.265 module
  for INVITE negotiation; conflating them would break the source
  abstraction.

### Decision: TeleBoot as a DeviceControl branch, not a new CmdType

Inside the existing `handleDeviceControl`, a `TeleBoot` element triggers the
reboot log path.

**Rationale:** The 2022 amendment defines TeleBoot as a control element, not
a new command; modelling it as a branch keeps the command dispatch table
unchanged.

**Alternatives considered:**
- New `CmdType=TeleBoot` — rejected: real platforms send DeviceControl with
  a TeleBoot child; a separate CmdType would not interoperate.

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| 2022 fields leak into 2016 responses via shared structs | `omitempty` + dedicated "must be absent" golden tests per 2016 scenario; version gate predicate is the only toggle |
| HomePosition set/query divergence (spec says OK/ERROR) | Single handler owns both directions; ERROR path covered by malformed-coordinate unit test |
| SDP capability line ordering differs between gate on/off | Builder appends capability rtpmap lines after the media-level defaults; golden tests lock both outputs byte-exactly |
| Recorded GBVersion stales if a device downgrades without re-registering | Accept: registration is the only version declaration point per Annex I; keepalive carries no version |
| Catalog item SectionType confuses 2016 parsers | Attribute omitted entirely for non-2022 peers (spec scenario), not merely empty |

## Migration Plan

1. Add `Is2022()` helper and wire the echo in `handleRegister` (no
   behaviour change for header-less peers).
2. Add `gb2022.go` model + codec with golden fixtures (pure additions).
3. Add acceptor branches behind the version gate (new CmdType cases).
4. Extend DeviceStatus/catalog encoders with optional fields.
5. Add `MediaCapabilities` profile field + SDP builder option.
6. Run full `go test ./... -count=1`; 2016 golden outputs must be unchanged.

Rollback: every step is additive; reverting the single acceptor commit
restores 2016-only behaviour.

## Open Questions

None — scope, wire shapes, and gating policy are fixed by the specs; the
remaining choices (payload type numbers for capability modules) follow the
proposal's stated values (H.265=100, AAC=97, G.722.1=99).
