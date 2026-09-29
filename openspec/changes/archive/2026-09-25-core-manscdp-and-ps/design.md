# Design

## Context

See `proposal.md` - Why.

当前 `internal/adapter/manscdp/` 已有 Catalog、Keepalive、MediaStatus、Subscribe 四类命令的编解码；`internal/adapter/media/` 已有 PS 封装/解封装、RTP 组包/拆包的零散实现。本 change 的目标不是推翻现有实现，而是在现有模式上补齐缺失的命令类型，并把 PS/RTP 能力从"零散文件"收束为可验证的契约。

## Goals / Non-Goals

**Goals:**
- 补齐 GB/T 28181 §9 规定的 MANSCDP 命令类型，使 codec 能处理真实设备/平台间的全部控制流
- 锁定 PS 封装/解封装的字节级行为，防止后续 media-source 或 RTP 层变化时产生静默失配
- 为后续级联、动态目录、抓包等能力提供协议基础

**Non-Goals:**
- 不实现任何 SIP 信令逻辑（已有 core-sip-stack）
- 不实现媒体源（已有 media-sources）
- 不引入 CGO 依赖
- 不做 TLS 安全扩展（留待 Change 12）

## Decisions

### D1: MANSCDP codec 继续沿用值对象 + xml.Marshal 模式

**Decision:** 新增命令类型继续沿用现有 `xml.MarshalIndent` 模式，每个命令对应一组 wire struct + 一组 domain model 转换函数。

**Alternatives considered:**
- 引入第三方 XML 库：项目现有编码器基于标准库 `encoding/xml`，引入新库会增加依赖和心智负担，且标准库已满足 GB28181 的简单 XML 结构。
- 代码生成：命令类型虽多，但结构简单，手写可维护性更好，且能保证 golden test 的确定性。

### D2: PS 封装/解封装以 media-sources 的现有实现为基线

**Decision:** 本 change 在 `internal/adapter/media/ps_depacketizer.go` 和 `ps_packetizer.go` 现有实现上补充缺失的边界行为（短包拒绝、音视频 stream ID 区分、错误传播），不重新实现。

**Alternatives considered:**
- 用纯 Go 第三方库替代：项目要求纯 Go 无 CGO，现有实现已满足要求，无需外部库。
- 把 PS/RTP 逻辑下沉到 domain 层：PS/RTP 是协议细节，应留在 adapter 层，domain 层只暴露 `InboundPipeline` / `OutboundPipeline` 端口。

### D3: 字节级 golden test 作为唯一权威

**Decision:** 所有 MANSCDP 编解码、PS 封装/解封装、RTP 分包/收包都用 golden test 锁定，不接受“看起来对”的断言。

**Alternatives considered:**
- 字段级断言： golden test 更可靠，能捕获边界条件（如 `omitempty` 导致的字段丢失）。
- 性能基准测试：本 change 优先保证正确性，性能优化留待 profiling 数据驱动。

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| MANSCDP XML 结构存在厂商私有扩展 | codec 对未知字段使用 `,omitempty` 或 `any`，不影响已知结构解析 |
| PS 封装需要处理可变码率导致 PTS 间隔不规则 | golden test 使用固定码率测试数据，VBR 边界留待 media-sources 实际使用时验证 |
| RTP 分包 MTU 选择影响网络行为 | 默认 1400 字节，通过 `RTPConfig` 可配置，不硬编码 |

## Migration Plan

1. 新增 `internal/adapter/manscdp/device_info.go`、`record_info.go`、`alarm.go`、`config.go`、`ptz.go`、`preset.go` 六个编解码文件
2. 新增 `internal/domain/model/device_info.go`、`record_info.go`、`alarm.go`、`ptz.go`、`preset.go` 等值对象
3. 扩展 `internal/adapter/media/` 的 PS/RTP golden test，补充短包拒绝、音视频 stream ID、去重等场景
4. 全量 `go test ./...` 绿色后归档

## Open Questions

- MANSCDP DeviceInfo 查询的 `StartTime` / `EndTime` 字段是 RFC 格式还是 GB 扩展格式？当前实现假设 RFC 格式，后续如有 GB 扩展需单独处理。
- PTZ 命令的 `Speed` 字段范围是 1–255 还是 0–255？当前实现假设 1–255，如厂商支持 0 需调整。
