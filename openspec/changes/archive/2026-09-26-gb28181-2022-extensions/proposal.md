# Proposal

## Why

The simulator speaks GB/T 28181-2016 semantics end to end, but a real
integration target increasingly requires GB/T 28181-2022 behaviour: the
X-GB-Ver handshake (Annex I), the 2022-only MANSCDP command family
(HomePosition, CruiseTrackList, SnapShot, storage-card status, TeleBoot,
precise PTZ position), and H.265/AAC/G.722.1 media-capability declaration.
Without these the simulator cannot stand in for a 2022-class device or
platform, which is the stated goal of roadmap step #11.

## What Changes

- Roadmap step **#11 `gb28181-2022-extensions`**.
- Complete the X-GB-Ver negotiation semantics: the platform (acceptor) echoes
  the negotiated version in its `200 OK` registration response and keeps the
  recorded downstream version available for version-gated behaviour.
- Version gating: 2022-only responses and extensions are emitted only to
  downstream peers whose recorded `GBVersion` is `2022`; 2016 peers keep
  today's wire format unchanged.
- New 2022 MANSCDP command branches on the device acceptor:
  - `HomePosition` query/set (guard position for PTZ).
  - `CruiseTrackList` query (cruise track roster).
  - `SnapShot` command acknowledgement with a recorded capture record.
  - storage-card status inside `DeviceStatus` responses (2022 field).
  - `TeleBoot` soft-reboot inside `DeviceControl` handling.
  - precise PTZ position fields in position responses.
- SDP media-capability modules: NodeProfile gains an optional media
  capabilities list (H.265 / AAC / G.722.1) that is declared in INVITE/200 OK
  SDP (`a=rtpmap` entries) only when enabled and only to 2022 peers.
- Annex O capture-section type: catalog items may carry the 2022
  `BusinessGroup`/section-type attribute when configured.

## Capabilities

### New Capabilities

- `gb28181-2022`: X-GB-Ver negotiation semantics, version-gated behaviour,
  and the 2022 MANSCDP command family (HomePosition, CruiseTrackList,
  SnapShot, storage-card status, TeleBoot, precise PTZ position) plus SDP
  media-capability declaration.

### Modified Capabilities

- `platform-large-node`: registration acceptance now echoes the negotiated
  `X-GB-Ver` in the `200 OK` response and stores the downstream version as
  first-class registration state.
- `media-sources`: SDP generation can advertise H.265/AAC/G.722.1 capability
  modules from node profile configuration when enabled.

## Non-goals

- GB 35114 SM2/SM3 security (roadmap #12).
- Exception flows and packet capture (roadmap #13).
- Web management UI (roadmap #14) and YAML scenario engine (roadmap #15).
- Full firmware-transfer simulation for `DeviceUpgrade`: the upgrade command
  is only logged and acknowledged; no binary payload is modelled.
- Catalog subscription/notification rework: already delivered by
  `dynamic-sim-features`; this change only reuses it.

## Impact

- **New files**: `internal/domain/model/gb2022.go` (HomePosition,
  CruiseTrack, SnapShotRecord models), `internal/adapter/manscdp/gb2022.go`
  (encoders/decoders + golden fixtures), acceptor handlers may live in
  `internal/app/acceptor.go` new branches.
- **Modified files**: `internal/app/acceptor.go` (registration echo + new
  MESSAGE branches + version gating helper), `internal/domain/model/node.go`
  / `node_profile.go` (media capabilities + catalog section type),
  `internal/adapter/sdp/*` (capability rtpmap emission),
  `internal/platform/config/config.go` (profile fields).
- **Tests**: golden XML tests for every new MANSCDP body, acceptor branch
  tests per command, version-gating tests (2016 peer must NOT receive 2022
  fields), SDP capability emission tests.
- **No dependency changes**; pure Go standard library additions only.
