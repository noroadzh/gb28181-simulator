# Proposal

对应路线图阶段编号：**#7 `platform-small-node`（共 15 阶段）的第二部分**。本 change
补充第一阶段（`2026-09-25-platform-small-cascading-identity`）未覆盖的 4 项主动能力：
SUBSCRIBE 目录订阅、INVITE 点播、OPTIONS 保活、MediaStatus 上报，使 platform-small
从"身份与级联链路"升级为"完整媒体与控制中继"。

## Non-goals

- 级联转发头（`X-RoutePath` / `X-PreferredPath`）——路线图 #9。
- 动态目录汇聚（把学到的下级通道报给上级）。本 change 只做单向订阅与转发。
- 完整 Web 管理界面——路线图 #14。
- 异常流注入与抓包导出——路线图 #13。

## Why

路线图 #7 第一部分已经让 platform-small 能作为级联链中间的 B：向下受理注册、向上
注册并保活。但这只是"身份打通"。实际仿真中，平台之间还需要：

1. **目录订阅**：上级平台需要知道下级有哪些设备，以及这些设备何时上线/离线。
   GB/T 28181 通过 `SUBSCRIBE` + `NOTIFY` 实现目录推送。
2. **INVITE 点播**：上级平台或 NVR 向 platform-small 发 INVITE 点播其下级设备的
   实时或历史视频流，platform-small 必须能应答并转发媒体。
3. **OPTIONS 保活**：级联链中的 platform-small 需要向上游主动探测链路活性，
   而不仅仅是被动接收 keepalive。
4. **MediaStatus 上报**：设备或平台上报媒体流状态（分辨率、码率、帧率等），
   platform-small 需要能解析并转发。

缺少这 4 项能力，platform-small 只能做"哑管道"——信令能过，媒体不通，状态不可见。

## What Changes

- **SUBSCRIBE 目录订阅**：platform-small 作为上级，收到下级的 SUBSCRIBE 后记录
  订阅关系，当下级设备列表变化时通过 NOTIFY 推送。
- **INVITE 点播**：platform-small 作为被叫方，收到上游 INVITE 后解析 SDP，
   启动入站媒体管道（RTPDeizer → PSDepacketizer → ESWriteCloser），应答 200 OK
   + SDP，并在 BYE 或超时时清理。
- **OPTIONS 保活**：platform-small 作为下级，定期向上游发送 OPTIONS 探测，
  根据 200/408 响应判断链路状态。
- **MediaStatus 上报**：platform-small 解析下级的 MediaStatus MANSCDP 消息，
  更新设备媒体状态，并按需向上级转发。
- **事务分拣升级**：将 splitTransport 从"按方向分拣"升级为"按 Call-ID 事务匹配"，
  使 INVITE/SUBSCRIBE 的请求与响应能正确路由到对应的半区。

## Capabilities

### New Capabilities

- `platform-small-node`: 在现有双向身份基础上，新增主动能力——目录订阅、INVITE 点播、
  OPTIONS 保活、MediaStatus 上报，以及事务级消息分拣。

### Modified Capabilities

（无。所有新增都是对 `platform-small-node` capability 的扩展，不改变既有 device /
platform-large / media-sources 的 spec。）

## Impact

- `internal/app/acceptor.go`：新增 `handleInvite` / `handleSubscribe` / `handleOptions`
- `internal/app/node_split_transport.go`：升级为事务级分拣
- `internal/app/dialog.go`：新增（INVITE 状态机与会话跟踪）
- `internal/app/media_service.go`：新增入站管道组装
- `internal/adapter/manscdp/`：新增 Subscribe / MediaStatus / PlaybackControl 编解码
- `internal/domain/model/`：新增 Dialog / Session / MediaStatus 值对象
- `internal/domain/port/`：新增 PlaybackPort / SubscribePort / MediaStatusPort
- `internal/adapter/media/`：入站适配器与现有出站管道对接
