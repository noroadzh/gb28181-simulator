# Proposal: dynamic-sim-features

## Why
The simulator currently supports static device and platform behaviors: a device always responds with the same catalog, keepalive is the only proactive message, and playback control is a stub. Real-world GB/T 28181 deployments require dynamic behaviors: alarm notifications, changing channel status, record queries, and mobile position updates. Without these, test scenarios for VMS/platform integrations remain shallow.

## What Changes
- Complete MANSCDP acceptor wiring for DeviceInfo, Alarm, RecordInfo, PTZ/DeviceControl, and PresetQuery commands already present in the adapter layer.
- Add dynamic catalog support: devices can update channel status/online state without restart.
- Add alarm notification emission: devices can proactively push Alarm events to their parent platform.
- Add record query and playback control: devices respond with RecordItem lists and handle playback start/stop.
- Add mobile position reporting: devices can emit MediaStatus with position data.
- Introduce a lightweight scenario/config layer that lets YAML or runtime API trigger the above dynamic behaviors.

## Capabilities

### New Capabilities
- `dynamic-catalog`: Channel add/remove and status changes reflected in live Catalog responses.
- `alarm-notification`: Device-side Alarm push and platform-side acknowledgment.
- `record-query-and-playback`: RecordInfo query response and PlaybackControl start/stop.
- `mobile-position`: MediaStatus with device GPS/position fields.
- `scenario-trigger`: Minimal runtime trigger (timer or API) to drive the above behaviors.

### Modified Capabilities
- `device-node`: Device node gains dynamic channel list, alarm emitter, and record list.
- `platform-small-node`: Platform-small now handles inbound Alarm/DeviceInfo/RecordInfo/PTZ/PresetQuery from downstream devices.

## Impact
- **New files**: `internal/domain/model/alarm.go`, `record_item.go`, `position.go`, `internal/app/scenario/*`, HTTP handlers for scenario trigger.
- **Modified files**: `internal/app/acceptor.go` (new command branches), `internal/domain/model/node.go` (dynamic state), `internal/adapter/manscdp/*` (encoder refinements).
- **New tests**: acceptor integration tests for each new command branch; scenario timer tests.
