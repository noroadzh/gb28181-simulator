# Tasks

## 1. Domain Model Extensions

- [x] 1.1 Extend `NodeProfile` with dynamic `Channels []Channel` (each with `DeviceID`, `Name`, `Status`) and `Alarms []AlarmSnapshot`; validate round-trip in existing profile test
- [x] 1.2 Add `RecordItem` model (`StartTime`, `EndTime`, `FilePath`, `FileSize`) and `Position` model (`Longitude`, `Latitude`, `Speed`) to `internal/domain/model/`; add unit tests for JSON/YAML round-trip
- [x] 1.3 Extend `MediaStatus` model with optional `Position` fields; verify existing encoder still produces valid XML without position

## 2. MANSCDP Encoder Refinements

- [x] 2.1 Update `internal/adapter/manscdp/alarm.go` encoder to include optional priority and description fields; add golden test for Alarm XML
- [x] 2.2 Update `internal/adapter/manscdp/record_info.go` encoder to emit `RecordItem` list; add test covering empty and populated lists
- [x] 2.3 Update `internal/adapter/manscdp/media_status.go` encoder to emit position fields when present; add test with/without position
- [x] 2.4 Update `internal/adapter/manscdp/device_info.go` and `preset.go` if needed for optional fields coverage

## 3. App Layer Acceptors

- [x] 3.1 Add `handleDeviceInfo()` to `Acceptor`: returns static DeviceInfo from profile; test with 200 OK response
- [x] 3.2 Add `handleAlarm()` to `Acceptor`: acknowledges alarm, appends to in-memory `NodeProfile.Alarms`; test push + ack flow
- [x] 3.3 Add `handleRecordInfo()` to `Acceptor`: returns generated RecordItem list based on profile config and requested time range; test with synthetic records
- [x] 3.4 Add `handleDeviceControl()` to `Acceptor`: logs PTZ command, returns 200 OK; test valid and invalid commands
- [x] 3.5 Add `handlePresetQuery()` to `Acceptor`: returns preset list from profile; test with empty and populated presets
- [x] 3.6 Wire new handlers into `handleMessage` switch in `acceptor.go`

## 4. Runtime Trigger API

- [x] 4.1 Add HTTP handler `POST /nodes/{id}/alarm` to inject an Alarm event on the target node
- [x] 4.2 Add HTTP handler `POST /nodes/{id}/channels/{ch}/status` to toggle channel online/offline
- [x] 4.3 Add HTTP handler `POST /nodes/{id}/position` to update device position
- [x] 4.4 Wire new handlers into existing Echo router in `internal/interface/http/nodes.go`

## 5. Dynamic Catalog Support

- [x] 5.1 Modify `answerCatalog()` to read from `NodeProfile.Channels` instead of static list; verify online/offline status is respected
- [x] 5.2 Add unit test: add a channel to profile, call answerCatalog, assert new channel appears in response

## 6. End-to-End & Integration Tests

- [x] 6.1 Add acceptor integration test for `handleDeviceInfo` verifying XML response
- [x] 6.2 Add acceptor integration test for `handleAlarm` verifying ack and in-memory storage
- [x] 6.3 Add acceptor integration test for `handleRecordInfo` with time-range filter
- [x] 6.4 Add acceptor integration test for runtime API: POST alarm, assert MANSCDP message emitted
- [x] 6.5 Run `go test ./... -count=1` and confirm all packages pass

## 7. Verify & Archive

- [x] 7.1 Run `openspec validate --changes "dynamic-sim-features" --strict`
- [x] 7.2 Run `openspec verify` (or equivalent) against artifacts
- [x] 7.3 Archive change to `openspec/changes/archive/`
