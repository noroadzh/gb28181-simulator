# Tasks

## 1. Insert Debug logs in `internal/app/acceptor.go`

- [x] 1.1 In `handleMediaStatus` (≈L1539), before `return resp, true`, add `a.log.Debug("media-status received", "node_id", p.id.String(), "device_id", notify.DeviceID(), "status", notify.Status())`
- [x] 1.2 In `handlePlaybackControl` (≈L1570), when `playback` port is present, add `a.log.Debug("playback control received", "node_id", p.id.String(), "device_id", notify.DeviceID())`; demote existing "port absent" log from debug to warn to avoid semantic confusion
- [x] 1.3 In `handleDeviceInfo` (≈L1596), before marshal, add `a.log.Debug("device-info query answered", "node_id", p.id.String(), "device_id", notify.DeviceID(), "sn", notify.SN())`
- [x] 1.4 In `handleRecordInfo` (≈L1647), before marshal, add `a.log.Debug("record-info query answered", "node_id", p.id.String(), "device_id", notify.DeviceID(), "sn", notify.SN(), "count", len(items))`
- [x] 1.5 In `handleHomePosition` Query branch (≈L1781), before `return resp, true`, add `a.log.Debug("home-position query answered", "node_id", p.id.String(), "device_id", notify.DeviceID(), "sn", notify.SN())`
- [x] 1.6 In `handleCruiseTrackList` (≈L1888), before `return resp, true`, add `a.log.Debug("cruise-track-list query answered", "node_id", p.id.String(), "device_id", notify.DeviceID(), "sn", notify.SN(), "count", len(items))`
- [x] 1.7 In `handleSnapShot` (≈L1934), before playback port invocation, add `a.log.Debug("snapshot command received", "node_id", p.id.String(), "device_id", cmd.DeviceID, "channel", cmd.ChannelID)` (independent of playback port presence)

## 2. Validate

- [x] 2.1 `go vet ./internal/app/...` passes
- [x] 2.2 `go build ./...` succeeds
- [x] 2.3 `openspec validate manscdp-logging-coverage --strict --type change --no-interactive` passes

## 3. Archive and sync roadmap

- [ ] 3.1 Update `docs/roadmap-15-steps.md` row #16: status `🔨 进行中` → `✅ 已归档（2026-09-29）`,说明列改为 "acceptor 7 处成功路径补 Debug 日志（codec 包按惯例保持无日志）"
- [ ] 3.2 `openspec archive manscdp-logging-coverage -y`
- [ ] 3.3 Verify `openspec list --specs` includes the updated `logging-coverage` (now contains the "MANSCDP 业务面成功路径可观测" Scenario)