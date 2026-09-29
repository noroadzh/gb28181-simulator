# Proposal

对应路线图阶段编号：**#7 `platform-small-node`（共 15 阶段）的第二部分**。本 change
交付该阶段的"主动中继能力"：目录订阅、点播回放、保活探测与媒体状态上报，把 platform-small
从"被动的身份与级联链路"升级为"完整媒体与控制中继"。

## Non-goals

- 级联转发头（`X-RoutePath` / `X-PreferredPath`）——路线图 #9。
- 动态目录与报警/录像查询（移动位置、报警事件、历史录像）——路线图 #10。
- GB/T 28181-2022 增量能力与版本协商 —— 路线图 #11。
- SM2/SM3 安全互认证 —— 路线图 #12。
- 异常流注入、抓包面板、pcap 导出 —— 路线图 #13。
- Web 管理界面 —— 路线图 #14。
- YAML 场景脚本引擎与报告 —— 路线图 #15。

## Why

#7 第一部分已经把 platform-small 建成了级联链中间环：它向下受理注册、应答目录、清扫超时，
向上注册、保活、续期。但国标里的"平台"不只是被动应答——它还要能向自己的下级发送目录订阅、
在收到上级 INVITE 时能把媒体流转发给本地设备或文件、向上级汇报通道的媒体状态、以及用
OPTIONS 探测上级活性。

没有这些主动能力，platform-small 只是一个"带在线表的转发壳"：它能被查到，但不能主动维护
目录一致性；它能被点播，但只能把媒体原样推出去，不能把上级 INVITE 和下级源组装成一条完整
管道；它知道自己的设备在线，但不知道它们的媒体通道是活跃还是静默。

这一 change 补齐 platform-small 的主动中继能力，使其成为国标级联中真正可用的中间平台。

## What Changes

- **SUBSCRIBE 目录订阅**：platform-small 向自己的下级设备发送 `Subscribe`（`CmdType = Catalog`），
  接收并缓存下级目录变更通知，在上级查询目录时能回答包含实时变更的结果，而不仅是启动时拍的一张
  快照。
- **INVITE 点播与媒体管道组装**：处理上级发来的 `INVITE` 请求，解析 SDP，建立 Dialog/Session
  跟踪，组装入站媒体管道（`RTPDeizer → PSDepacketizer → ESWriteCloser`），并把点播请求按
  设备 ID 转发给对应下级。
- **OPTIONS 保活探测**：向上级平台周期性发送 `OPTIONS` 保活探测，区分 200（正常）、408（超时）、
  5xx（故障）响应，作为 MESSAGE keepalive 的补充，在注册仍有效但上级无响应时提前发现问题。
- **MediaStatus 上报**：解析下级设备上报的 `Notify`（`CmdType = MediaStatus`），记录通道状态，
  并在收到上级查询时能作答；向上级发送 MediaStatus 查询与变更通知。

## Capabilities

### New Capabilities

- `platform-small-node`（补充）：主动中继能力——目录订阅、INVITE 点播与媒体管道组装、
  OPTIONS 保活探测、MediaStatus 上报与查询。

### Modified Capabilities

- `platform-small-node`：扩展原 spec，新增主动能力需求；原身份与级联链路需求保持不变。

## Impact

- `internal/app/dialog.go`（新增）：Dialog/Session 管理，维护 Call-ID → DialogState 映射，
  管理 INVITE 事务状态机。
- `internal/app/media_service.go`：新增 `InboundPipeline` 方法，组装入站媒体管道。
- `internal/app/acceptor.go`：扩展 `ServingHandler`，新增 INVITE/SUBSCRIBE/OPTIONS/ACK/BYE 方法
  处理。
- `internal/app/node_split_transport.go`：升级 `splitTransport` 为事务分拣，支持 Call-ID 级别
  请求/响应匹配。
- `internal/app/keeper.go`：扩展 `Keeper`，增加向上级发送 OPTIONS 保活探测。
- `internal/adapter/manscdp/codec.go`、`notify.go`、`catalog.go`：新增 Subscribe/MediaStatus/
  PlaybackControl 编解码适配器。
- `internal/domain/model/notify.go`：新增 `CmdTypeSubscribe`、`CmdTypeMediaStatus` 常量。
- `internal/domain/port/media.go`、`port.go`：新增 `PlaybackPort`、`SubscribePort`、
  `MediaStatusPort` 接口。
- `internal/app/media_service.go`：新增入站媒体管道组装。
- `internal/app/node_service.go`：组装 Dialog 管理与入站管道。
- `openspec/specs/platform-small-node/spec.md`：合并 ADDED 需求。
