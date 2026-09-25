# Proposal

## Why

GB/T 28181 信令交互严重依赖 MANSCDP XML 消息体（目录查询、设备信息、报警、配置下发、云台控制等）。当前 codec 只覆盖了 Catalog、Keepalive、MediaStatus、Subscribe 四类，无法完整模拟真实设备与平台间的控制流；同时 PS 封装/解封装与 RTP 分包虽然已有零散文件，但缺少统一的能力规约与验收契约。本 change 补齐这两块核心协议能力，为后续级联、动态目录、抓包等能力提供协议基础。

## What Changes

- 扩展 `internal/adapter/manscdp/`：新增 DeviceInfo、RecordInfo、Alarm、ConfigDownload、PTZ/Telemetry、Preset 的编解码方法与单测
- 扩展 `internal/adapter/media/`：完善 PS 打包器、PS 解包器、RTP 组包/拆包，补充单元测试与 golden test
- 新增 `internal/domain/model/` 相关值对象：DeviceInfo、RecordInfo、Alarm、PTZ 等
- 更新 `openspec/specs/`：新增 `core-manscdp-and-ps` 能力 spec，要求后续实现可验证

## Capabilities

### New Capabilities

- `core-manscdp-and-ps`: MANSCDP XML 全量编解码与 PS/RTP 封装能力。覆盖 DeviceInfo、RecordInfo、Alarm、ConfigDownload、PTZ/Telemetry、Preset 命令的编解码，以及 PS 打包/解包、RTP 分包/组包。

### Modified Capabilities

- 无现有 spec 需要修改。

## Impact

- 新增约 20 个 Go 文件（codec + domain model + tests）
- 新增测试数据：`internal/adapter/manscdp/testdata/*.xml`
- 纯新增能力，无 BREAKING 变更
- 依赖 core-sip-stack（#2）与 enterprise-skeleton（#4）已归档
