# Tasks

## 1. Version Gate Foundation

- [x] 1.1 Add `Is2022()` predicate on `DownstreamDevice` (true iff `GBVersion == "2022"`) in `internal/domain/model/downstream_device.go`; unit test covers `"2022"`, `""`, and `"2016"` inputs. Verify: `go test ./internal/domain/model/ -run TestDownstreamDeviceIs2022 -count=1` passes
- [x] 1.2 In `handleRegister` (`internal/app/acceptor.go`), append `X-GB-Ver` to the `200 OK` response headers only when the inbound REGISTER carried it; existing header-less flow must remain byte-identical. Verify: extend `device_registrar`/acceptor registration test asserting both cases, `go test ./internal/app/ -run Register -count=1` passes

## 2. 2022 Model & Codec Pair

- [x] 2.1 Add `internal/domain/model/gb2022.go`: `HomePosition` (longitude, latitude, altitude, azimuth), `CruiseTrack` (id, name, waypoint count), `SnapShotRecord` (deviceID, channelID, capturedAt); constructors validate required fields. Verify: unit tests for constructors (valid/invalid) pass
- [x] 2.2 Add `internal/adapter/manscdp/gb2022.go` with XML encode/decode for HomePosition query/response and set command, CruiseTrackList response, SnapShot command/response; create golden fixtures under `internal/adapter/manscdp/testdata/` (`home-position-response.xml`, `cruise-track-list.xml`, `snapshot-command.xml`). Verify: byte-level golden tests round-trip each fixture, `go test ./internal/adapter/manscdp/ -run GB2022 -count=1` passes

## 3. Acceptor Branches (version-gated)

- [x] 3.1 Add `handleHomePosition()` branch: query returns profile guard position, set command validates coordinates (OK / ERROR per spec), updates in-memory profile. Verify: acceptor test with 2022 peer passes both flows, malformed set returns ERROR
- [x] 3.2 Add `handleCruiseTrackList()` branch: returns configured cruise tracks for the requested channel. Verify: acceptor test asserts both tracks listed for a two-track profile
- [x] 3.3 Add `handleSnapShot()` branch: answers OK and records a `SnapShotRecord` on the node. Verify: acceptor test asserts OK response and one stored record with current timestamp
- [x] 3.4 Add TeleBoot branch inside existing `handleDeviceControl`: log reboot request, answer OK. Verify: acceptor test with DeviceControl+TeleBoot passes; existing control tests unchanged
- [x] 3.5 Gate all new branches with `Is2022()`: a 2016 peer receiving HomePosition/CruiseTrackList/SnapShot gets today's not-implemented behaviour. Verify: acceptor test asserts 2016 peer never receives 2022 responses

## 4. Optional Fields in Existing Bodies

- [x] 4.1 Extend DeviceStatus response encoder with `StorageCardStatus` (omitted when empty); emit only when profile declares it and peer `Is2022()`. Verify: golden test asserts field present for 2022 peer, absent for 2016 peer
- [x] 4.2 Extend catalog item encoder with `SectionType` attribute (omitted when unset); acceptor emits it only to 2022 peers. Verify: catalog golden tests cover both peers, 2016 output byte-identical to pre-change fixture
- [x] 4.3 Add precise PTZ position fields (azimuth/elevation/zoom with sub-degree precision) to position/status responses when profile declares them and peer `Is2022()`. Verify: encoder test asserts precise azimuth 121.4731 rendered; omitted for 2016 peer

## 5. SDP Capability Modules

- [x] 5.1 Add `MediaCapabilities []string` to `NodeProfile` (validated values `H265`/`AAC`/`G7221`, empty by default) in `internal/domain/model/node_profile.go` and `internal/platform/config/config.go`. Verify: profile validation test rejects unknown module names, accepts the three known ones
- [x] 5.2 Add `WithCapabilityModules([]string)` option to the SDP builder; appends `a=rtpmap:100 H265/90000`, `a=rtpmap:97 AAC/44100` (per module config), `a=rtpmap:99 G7221/8000` after media-level defaults. Verify: golden SDP tests lock output with modules on and off, byte-exact
- [x] 5.3 Wire gating in the acceptor INVITE path: pass profile modules to the SDP builder only when peer `Is2022()`. Verify: acceptor INVITE test asserts rtpmap line present for 2022 peer, absent for 2016 peer, absent when profile declares nothing

## 6. Regression & Integration

- [x] 6.1 Run the full suite and confirm every pre-existing golden test (SIP/SDP/PS/MANSCDP) still passes unchanged, proving 2016 behaviour is byte-identical. Verify: `CGO_ENABLED=0 go test -race -count=1 ./...` exits 0
- [x] 6.2 Add one end-to-end test: 2022 device registers against platform-large (X-GB-Ver echoed), queries HomePosition and catalog SectionType, INVITE SDP carries the enabled H.265 rtpmap. Verify: test in `internal/adapter/siptest/` passes

## 7. Verify & Archive

- [x] 7.1 Run `openspec validate --changes "gb28181-2022-extensions" --strict`. Verify: validator exits 0 with no errors
- [x] 7.2 Run verify-change against artifacts and confirm all tasks are complete. Verify: verification output lists no incomplete tasks
- [x] 7.3 Archive the change to `openspec/changes/archive/` and update `docs/roadmap-15-steps.md` status for #9, #10, #11. Verify: `openspec list` no longer shows the change; roadmap table shows #9/#10/#11 as archived
