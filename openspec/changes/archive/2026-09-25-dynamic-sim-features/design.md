# Design: dynamic-sim-features

## Context
The simulator has basic device/platform nodes with static catalog and keepalive-only proactive messages. MANSCDP encoder/decoder scaffolding exists for Alarm, DeviceInfo, RecordInfo, PTZ, and PresetQuery, but the app-layer acceptor only handles Keepalive, CatalogQuery, MediaStatus, and PlaybackControl. There is no mechanism to simulate device state changes, alarm events, or scheduled behaviors.

## Goals / Non-Goals

**Goals:**
- Wire all existing MANSCDP command decoders into the acceptor so devices respond realistically.
- Allow runtime updates to device channel list and status.
- Add alarm push emission and RecordInfo query response.
- Add mobile position data to MediaStatus.
- Provide a minimal trigger mechanism (timer + HTTP API) to drive dynamic behaviors without a heavy scenario engine.

**Non-Goals:**
- Full YAML scenario scripting engine (reserved for Change 15).
- Real media streaming or file I/O for playback (simulate with empty/dummy SDP).
- Persistent storage of alarms or records (in-memory only for simulator).

## Decisions

### Decision: Extend NodeProfile with dynamic state

Add `Channels []Channel` (with `Status` field) and `Alarms []Alarm` to `NodeProfile`. The acceptor reads these for Catalog and Alarm responses. Updates happen via HTTP API or in-process method calls.

**Rationale:** NodeProfile is already the configuration/identity holder. Keeping dynamic state on the profile keeps the domain model simple and avoids a separate state store for a simulator.

**Alternatives considered:**
- Separate `DeviceState` struct injected into Node — rejected: adds indirection without benefit for single-process simulation.
- SQLite-backed state — rejected: overkill; Change 14 covers persistence.

### Decision: Accept Alarm, DeviceInfo, RecordInfo, PTZ, PresetQuery in acceptor

Add new `handle*` methods on `Acceptor`:
- `handleDeviceInfo()` → returns static device info from profile
- `handleAlarm()` → acknowledges alarm; optionally stores in in-memory list
- `handleRecordInfo()` → returns a generated list of RecordItem based on profile config
- `handleDeviceControl()` → logs PTZ command, returns 200 OK
- `handlePresetQuery()` → returns preset list from profile

**Rationale:** The adapter layer already marshals these XML bodies; the acceptor is the only missing piece. Each handler is small (<<100 LOC) and testable.

**Alternatives considered:**
- Dispatch table of `func(string) error` — rejected: typed handlers are clearer for a small set.

### Decision: Add Position to MediaStatus

Extend `model.MediaStatus` with `Longitude`, `Latitude`, `Speed` (all optional float32). The existing `MediaStatus` encoder/decoder in `manscdp` is updated to emit these fields when present.

**Rationale:** GB/T 28181 allows position in MediaStatus; adding optional fields is backward-compatible.

**Alternatives considered:**
- New `PositionStatus` command — rejected: MediaStatus already carries device status; extending it is simpler.

### Decision: Trigger via Clock + HTTP API

Use the existing `port.Clock` and `port.TickerFactory` to schedule behavior triggers. Expose a minimal HTTP handler:
- `POST /nodes/{id}/alarm` → inject an Alarm event
- `POST /nodes/{id}/channels/{ch}/status` → toggle channel online/offline
- `POST /nodes/{id}/position` → update position

**Rationale:** Avoids building a full scenario engine in Change 10. The API is sufficient for manual and automated test scripts.

**Alternatives considered:**
- Cron-like YAML scenario files — rejected: better suited for Change 15.

## Migration Plan
1. Extend `NodeProfile` with `Channels` and `Alarms` fields.
2. Update `MediaStatus` model with optional position fields.
3. Add acceptor handlers for DeviceInfo, Alarm, RecordInfo, DeviceControl, PresetQuery.
4. Update MANSCDP encoders to include position when present.
5. Add HTTP API endpoints under `internal/interface/http/`.
6. Add unit and integration tests for each new handler.

## Open Questions
- Should RecordInfo return real time ranges or synthetic data? → Synthetic fixed windows for simulator.
- Should Alarm events be persisted across restarts? → No, in-memory only.
