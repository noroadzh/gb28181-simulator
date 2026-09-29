# Design

## Context

#7 第一部分已经让 platform-small 能作为级联中间环：向下受理、向上注册、目录应答、清扫超时。
但分拣器是"按方向"的临时方案，且 serving loop 只 dispatch REGISTER 和 MESSAGE，INVITE、
SUBSCRIBE、OPTIONS、ACK、BYE 全部被静默丢弃。

MANSCDP 只解码 Keepalive 和 Catalog，MediaStatus/Subscribe/PlaybackControl 缺失。
媒体层只有出站管道，没有入站组装。

## Goals

- 让 platform-small 能处理 INVITE/SUBSCRIBE/OPTIONS/ACK/BYE 等 SIP 方法。
- 让 platform-small 能向自己的下级发送目录订阅并接收变更通知。
- 让 platform-small 能把上级 INVITE 的媒体流组装成本地可消费的 ES 帧。
- 让 platform-small 能向上级发送 OPTIONS 保活探测并区分响应类型。
- 让 platform-small 能解析并上报/查询 MediaStatus。

## Non-Goals

- 级联转发头（X-RoutePath / X-PreferredPath）——路线图 #9。
- 动态目录与报警/录像查询 —— 路线图 #10。
- GB/T 28181-2022 增量能力 —— 路线图 #11。
- SM2/SM3 安全互认证 —— 路线图 #12。
- 异常流注入、抓包面板 —— 路线图 #13。
- Web 管理界面 —— 路线图 #14。
- YAML 场景脚本引擎 —— 路线图 #15。

## Decisions

### 1. 升级 splitTransport 为事务分拣

当前按方向分拣是临时方案。INVITE/SUBSCRIBE 需要按 Call-ID 级别匹配请求与响应，因此将
`splitTransport` 扩展为 `transactionSplitTransport`，支持 serving 半和 registering 半各自
注册事务处理器。

- 理由：按方向分捡只够用因为小平台在上游侧只收响应；一旦加上上游来的请求（INVITE /
  SUBSCRIBE），方向分捡会把请求也送注册半，导致 serving 半收不到。
- 备选 A：给 Transport 加按 Call-ID 的事务登记。那是真正的 SIP 事务层，本 change
  只做最小升级。
- 备选 B：给两半各配一个监听口。违反 spec 的"两半都走自己的 transport"。
- 结论：升级为事务分拣，serving 半优先认领，剩余报文再按方向分拣。

### 2. Dialog/Session 跟踪放在 app 层

新建 `internal/app/dialog.go`，维护 `Call-ID → DialogState` 映射，管理 INVITE 事务状态机
（calling → confirmed → terminated）。不放在 domain 层因为 Dialog 是协议会话概念，
domain 保持纯值对象。

- 理由：Dialog 生命周期与 SIP 事务紧密耦合，domain 层不应知道 SIP 细节。
- 备选：放在 adapter 层。adapter 层通常只做编解码和 transport，不适合管理状态机。
- 结论：app 层，靠近 serving loop 但独立于它。

### 3. MANSCDP 扩展保持向后兼容

在 `model.Notify` 中新增 `CmdTypeSubscribe`、`CmdTypeMediaStatus` 常量，`DecodeNotify`
对未知命令类型返回可解析的 Notify（不报错），让 use case 层决定是否处理。

- 理由：现有调用方只关心 Keepalive 和 Catalog，新增类型不应破坏它们。
- 备选：返回错误。会破坏任何未来收到未知命令的设备。
- 结论：解析宽松，渲染严格。

### 4. 入站媒体管道组装在 app 层

新建 `internal/app/media_service.go` 的 `InboundPipeline` 方法，将 `RTPDeizer`、
`PSDepacketizer`、`ESWriteCloser` 按端口组装，由 Dialog 管理生命周期（INVITE 200 OK 后
启动，BYE/超时后关闭）。

- 理由：媒体管道是 app 层的编排责任，domain port 只定义契约，adapter 提供实现。
- 备选：放在 domain 层。domain 不该知道 RTP/PS 的具体组装顺序。
- 结论：app 层组装，port 层定义契约。

### 5. 新增 domain port 接口

`PlaybackPort`（控制播放/停止/查询）、`SubscribePort`（目录订阅管理）、`MediaStatusPort`
（媒体状态上报），由 adapter 实现，app 层通过 port 调用。

- 理由：app 层不直接 import adapter，通过 port 解耦。
- 备选：硬编码 adapter 调用。违反六边形架构。
- 结论：新增 port，compile-time 断言确保 adapter 实现。

### 6. Keeper 扩展 OPTIONS 保活探测

在现有 MESSAGE keepalive 基础上，增加向上级周期性发送 OPTIONS 探测。200 正常、408 超时、
5xx 故障分别处理，作为 MESSAGE 的补充。

- 理由：MESSAGE 保活只读响应，无法检测"注册有效但上级无响应"的情况。
- 备选：只保留 MESSAGE。国标要求 OPTIONS 保活。
- 结论：双轨保活，OPTIONS 作为补充。

## Risks / Trade-offs

- **Dialog 映射使用 sync.Map 或分片锁**，避免单点锁争用。
- **RTPDeizer 按 SSRC+payload_type 分组**（修复现有 warning），避免多流混入。
- **所有 SIP 响应异步发送**，失败仅打 warn 不中断 serving loop。
- **事务超时使用 context + ticker**，goroutine 泄漏防护。

## Migration Plan

- 配置向后兼容：新增 OPTIONS 保活探测有默认值，不破坏现有配置。
- MANSCDP 编解码向后兼容：新增命令类型不破坏现有解析。
- 新增 port 接口不影响现有调用方。

## Open Questions

- INVITE 点播时 SDP 中的媒体格式是否只支持 PS over RTP？（当前实现支持，后续可扩展）
- OPTIONS 保活探测的周期是否与 MESSAGE keepalive 独立？（是，分别配置）
