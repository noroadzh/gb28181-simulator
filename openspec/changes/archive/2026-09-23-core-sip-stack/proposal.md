# Proposal

## Why

路线图第二步 (`core-sip-stack`)。在已有项目骨架 (Change 1) 的基础上，提供一个 **节点无关的 GB/T 28181 SIP/SDP/Digest 底层库**：
后续 14 个 change（Node 抽象、三种身份、媒体、级联、2022 扩展、GB35114、异常流、Web、场景引擎）都将基于本 change 的 API 构建。
底层稳定，行为层（Change 5/6/7）才能独立可测、可演进。

## What Changes

- 新增 `internal/sip` 包：基于 `github.com/ghettovoice/gosip`（emiago 维护、module 路径仍为 `github.com/ghettovoice/gosip`，纯 Go、CGO_ENABLED=0 可编译），封装 GB/T 28181-2016 §C 必需的 SIP 报文构建、序列化、Via branch 唯一性生成、Max-Forwards 与 Content-Length 注入、ContentType `Application/SDP` 协商、保留所有 headers
- 新增 `internal/sdp` 包：基于 `github.com/pion/sdp`（RFC 4566）做 GB/T 28181 §K 兼容层 —— 解析时**先剥离**附录 K 特有行 `y=`（SSRC）与 `f=`（`v/A/V/M` 选项），用 pion 解析 RFC 4566 部分，再把剥离字段回填到结构体；marshal 时按 GB/T 28181 行序（v/o/s/c/t/m/a/y/f/a…）回填
- 新增 `internal/auth` 包：RFC 2617 / RFC 7616 / GB/T 28181 标准 Digest 认证——server 侧生成 `WWW-Authenticate` challenge（含 `qop=auth`、随机 nonce），client 侧 `Authorization` 计算（response = MD5/MD5-sess，支持 `qop=auth`/`qop=auth-int`、`algorithm=MD5`）；预留 `algorithm=SM3` hook 供 Change 12 (gb35114-security) 替换
- 新增 `internal/siptransport` 包：基于 `gosip/transport` 的 **进程内多实例 transport listener 抽象**，支持：
  - 节点可绑定**任意本地地址**（127.0.0.1、0.0.0.0、内网 IP、多 NIC）；多个 transport 共存于同一进程
  - TCP/UDP/TLS listener 按需启停（每个 Node 一个 transport 句柄）
  - 上层仅看到 `Send/Receive/Close` 接口，**无任何 UAC/UAS 语义**
- 新增 `cmd/sipprobe` 命令行工具（仅供开发/CI 冒烟测试，不入产品）：
  - `--bind 127.0.0.1:5060` 启动一个 transport listener
  - `--send-to udp:127.0.0.1:5061 --sip-message "INVITE sip:..."` 发送一条 SIP 报文
  - `--expect-status 100` 等待一次响应并打印（用于 golden 测试）
- 新增黄金（golden）测试套件：固化一组真实抓包样本（向 Change 13 异常流与抓包统一使用），覆盖 REGISTER/INVITE/200/401/407/BYE/CANCEL/ACK
- **BREAKING**：将 `internal/logger` 与本 change 通过 `sip/audit` 子包对接 —— 所有入/出报文默认 trace 级，记录完整 SIP 原文（已经具备脱敏；wire 长度截断 4 KiB）

## Capabilities

### New Capabilities

- `core-sip-stack`: 节点无关的 GB/T 28181 SIP 底层库 —— 消息构建/解析、SDP 编解码（兼容 §K）、Digest 认证、多实例 transport listener、审计日志契约。后续 Change 4-15 共享此 API；Node/身份/媒体/级联不在本 change 范围内。

### Modified Capabilities

（无 — 仅新增）

## Impact

- **新增代码**：~12 个 Go 包（sip、sip/audit、sdp、auth、siptransport、sipprobe 命令）+ 黄金测试样本 + golden fixture，预计 < 3500 行
- **新增依赖**：
  - `github.com/ghettovoice/gosip`（SIP 底层库，纯 Go；CGO_ENABLED=0 可编译通过；已确认）
  - `github.com/pion/sdp`（RFC 4566 SDP；纯 Go；pion 生态依赖 `github.com/pkg/errors` 一项间接依赖）
  - **不引入**：gRPC、protobuf、YAML 新库（配置已由 Change 1 viper 提供）
- **后续 change 的影响**：
  - Change 4 (`node-abstraction`) 直接 `embed *siptransport.Transport`，并通过 `sip.Message` 类型表达身份与状态
  - Change 5/6/7 (device/platform-large/platform-small) 在 Node 抽象上叠加 UAC/UAS 行为；SIP 报文、SDP、Digest 业务代码**不再重写**
  - Change 11 (gb28181-2022-extensions) 在本 change 的 `internal/sip` 之上增加 `X-GB-Ver` 协商钩子
  - Change 12 (gb35114-security) 替换 `internal/auth` 的 `algorithm` 字段（`SM3`）并补充 SM2 信封
  - Change 13 (exception-and-capture) 复用 `sip/audit` 的 trace 日志流生成 pcap
  - Change 14 (web-management-ui) 通过 `WS /api/sip/stream` 暴露 `sip/audit` 给前端抓包面板
- **不引入 CGO 依赖**：`gosip`/`pion/sdp` 均纯 Go；`CGO_ENABLED=0 go build` 在 Linux/macOS/Windows 全矩阵通过
- **不修改 `internal/logger` 与 `internal/config` 的对外接口**：仅增加调用方（不破坏 Change 1 的测试矩阵）

## Non-Goals（明确不属于本 change）

- **不实现任何节点身份**：device / platform-small / platform-large 留待 Change 4-7
- **不实现 REGISTER/INVITE/SUBSCRIBE 等业务行为**：本 change 仅暴露"发包/收包"原语
- **不实现 MANSCDP+ / Catalog / Alarm / PTZ / MediaStatus 报文体**：留待 Change 3 (`core-manscdp-and-ps`)
- **不实现媒体层**：PS 封装、RTP 分包、SDP `m=` 媒体协商的传输落地留待 Change 8 (`media-sources`)
- **不实现 GB/T 28181-2022 扩展字段**（X-GB-Ver 协商、看守位、巡航轨迹等）：留待 Change 11
- **不实现 GB35114 安全扩展**（SM2 互认证、SM3 Note）：留待 Change 12
- **不实现级联路径 / X-RoutePath / X-PreferredPath**：留待 Change 9 (`cascade-and-multi-instance`)
- **不实现异常流（401/403/404/407/408/480/486/487/488/500/503、CANCEL、re-INVITE）业务编排**：留待 Change 13
- **不实现抓包导出 / pcap**：留待 Change 13（但本 change 的 `sip/audit` 提供原始字节流，作为 Change 13 的输入）
- **不实现 Web 端对 SIP 流的实时呈现**：留待 Change 14
- **不实现 YAML 场景驱动**：留待 Change 15
- **不展开 Node 节点多进程 / 集群拓扑**：仅同一进程内多 transport 多 listener

## 路线图定位

| 字段 | 值 |
|---|---|
| 路线图阶段编号 | **Change 2 / 15** |
| 前置依赖 | Change 1 (`project-skeleton`，已归档) |
| 解锁的下一 change | Change 3 (`core-manscdp-and-ps`)、Change 4 (`node-abstraction`)、Change 11 (`gb28181-2022-extensions`)、Change 12 (`gb35114-security`)、Change 13 (`exception-and-capture`)、Change 14 (`web-management-ui`) 均可在本 change 之上推进 |
| 第一阶段终点 | 否（属第二阶段："协议底层"，仍非"可仿真产品"） |