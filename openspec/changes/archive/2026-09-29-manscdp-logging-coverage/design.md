# Design

## Context

`internal/app/acceptor.go` 已经持有注入的 `*slog.Logger`（字段 `a.log`，构造时通过 `log.With("component", "internal/app", "subsystem", "sip_acceptor")` 加标签）。5 个 MANSCDP handler（Alarm / Catalog / Keepalive / DeviceControl PTZ / PresetQuery）已在成功路径调用 `a.log.Debug(...)`。

剩下 7 个 handler 的成功路径完全没有日志：MediaStatus、PlaybackControl、DeviceInfo、RecordInfo、HomePosition Query 分支、CruiseTrackList、SnapShot。诊断时只能依赖错误日志或 pcap 抓包。

codec 包（`internal/adapter/manscdp/`）是无状态 XML 编解码器，仓库惯例（`adapter/sip`、`adapter/sdp`）保持无日志，避免给 codec 注入 logger 造成调用方归属混乱。

## Goals / Non-Goals

**Goals:**
- 把 7 个缺失 Debug 日志的 handler 成功路径补齐，沿用 acceptor 现有 logger
- 新增 spec 场景 "MANSCDP 业务面成功路径可观测" 约束契约
- 最小化改动：1 个文件、~7 行插入

**Non-Goals:**
- 不修改 codec 包
- 不新增 logger 模块（沿用 `subsystem: "sip_acceptor"`，由 acceptor 的 `With("subsystem", ...)` 已携带的标签即可在 `manscdp_inbound` 子树检索）
- 不动 W2 集成测试
- 不为已有日志的 5 个 handler 加新日志

## Decisions

### 决策 1：日志放在 handler 内部成功路径（return resp, true 之前）

每条日志仅 1 行，使用 `a.log.Debug(...)`，字段用 `node_id`、`device_id`、`sn`、`count`、`channel` 等结构化键。

理由：acceptor 已有 logger 注入，成功路径是事件发生的最近点，记录时所有解码结果变量都在 scope 内，无需层层传参。

### 决策 2：PlaybackControl 修正误导性日志

现有代码在 `handlePlaybackControl` 在 playback port 缺失时输出 "playback control notify (port absent)"，但 playback 实际未发生，运维易误读。修改为：
- port present：新增 `a.log.Debug("playback control received", "node_id", ..., "device_id", notify.DeviceID())`
- port absent：现有错误日志保留（删除"misleading"的措辞），明确为 warn 级别

理由：消除"playback 收到 vs port 缺失"语义混淆，便于运维快速判断是否需要排查 channel 配置。

### 决策 3：SnapShot 与 PlaybackControl 解耦日志

SnapShot handler 当前在 `playback` port 不为 nil 时打印"capture pending"日志，但即使 port 为 nil，命令接收本身也是业务事件。补一条独立的 `a.log.Debug("snapshot command received", "node_id", ..., "device_id", cmd.DeviceID, "channel", cmd.ChannelID)`，与 capture 副作用解耦。

理由：snap 命令是否被接收是平台调试关心的事实，capture 是否成功是另一回事。

## Risks / Trade-offs

- **日志量**：debug 级别在生产默认 info 阈值下不输出；仅在 `HUB_LEVEL=debug` 或 `log.modules."internal/app".level=debug` 时可见，符合"默认关闭、诊断可开"原则
- **结构化字段一致性**：7 条日志字段名需统一（`node_id` / `device_id` / `sn` / `count` / `channel`），由 spec Scenario "MANSCDP 业务面成功路径可观测" 强制约束
- **未触及 codec 包** 是有意取舍：若未来出现"解码成功但内容可疑"的场景（如 SN=0、DeviceID 空字符串），需新开 change 设计 codec 层 logger 注入；当前 change 不预防性引入