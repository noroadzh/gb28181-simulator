# Proposal

## Why

`internal/app/acceptor.go` 处理 12 类 MANSCDP 业务通知/查询/控制时，**7 个 handler 的成功路径完全没有 Debug 日志**：MediaStatus、PlaybackControl、DeviceInfo、RecordInfo、HomePosition Query、CruiseTrackList、SnapShot。诊断"为何平台收不到某通知"或"为何某命令静默生效"时，运维与开发只能依赖错误路径日志和 pcap 抓包，成功路径完全不可观测。

本 change 落实路线图 **#16 manscdp-logging-coverage**，把成功路径纳入 `logging` 包的可观测契约。

## What Changes

- 在 `internal/app/acceptor.go` 的 7 个 MANSCDP handler 成功路径插入 `a.log.Debug(...)`：
  1. `handleMediaStatus`：解码后的 status 转发至 SSE 端口前
  2. `handlePlaybackControl`：port present 时记录 device_id（修正现有 "port absent" 日志的误导语义）
  3. `handleDeviceInfo`：查询响应 marshal 前
  4. `handleRecordInfo`：返回的 items 切片 marshal 前
  5. `handleHomePosition` Query 分支：marshal 路径返回前
  6. `handleCruiseTrackList`：查询响应 marshal 前
  7. `handleSnapShot`：playback port 存在性之外，独立记录 snapshot 命令接收
- 模块标签沿用 acceptor 已有 `subsystem: "manscdp_inbound"`，无需新模块注册
- 不修改 `internal/adapter/manscdp/` 包：codec 是无状态编解码器，按仓库惯例（`adapter/sip`、`adapter/sdp`）不应持有 logger；日志属于调用方（acceptor）职责

## Capabilities

### Modified Capabilities

- `logging-coverage`: 新增 Scenario "MANSCDP 业务面成功路径可观测"，约束 7 个 handler 在成功路径输出 Debug 级日志并含 `node_id`、`device_id`、`sn` 等结构化字段

## Non-goals

- 不修改 `internal/adapter/manscdp/` 包（stateless codec 保持无日志，违反惯例会带来 logger 注入污染）
- 不为 5 个已有日志的 handler（Alarm / Catalog / Keepalive / DeviceControl PTZ / PresetQuery）增加新日志
- 不引入新的 logging 模块名（沿用 acceptor 的 `manscdp_inbound` subsystem）
- 不涉及 W2 集成测试（属于另立 change 范畴）

## 路线图对应

- 阶段编号：**#16** `manscdp-logging-coverage`
- 状态推进：`⬜ 待实施` → `🔨 进行中` → `✅ 已归档（2026-09-29）`

## 实际工作量修正

路线图原描述 "alarm/catalog/keepalive/notify/ptz 等 18 个文件补 trace/debug 日志调用点" 基于初版排查。实际定位（2026-09-29）确认 codec 包按惯例不记日志，所有缺口集中在 `acceptor.go` 单文件 7 处 Debug。本次 change 完成核心可观测契约；codec 内部补日志作为独立 follow-up（如未来需要）另开 change。

路线图 `说明` 列需在归档时同步更新为 "acceptor 7 处成功路径补 Debug 日志（codec 包按惯例保持无日志）"，并在 commit message 中引用 `openspec list --specs` 输出。