# Proposal

## Why

平台现在已经能拉流（#41）并录像（#42），但**上级平台经本平台拉下级设备的画面**仍然不通：本平台收到指向下级通道的 INVITE 时只走通用的对话分支，回 200 却不转任何媒体。级联场景里这是最常被用到的路径，也是「模拟平台冒充真实网关」最容易露怯的地方（上级看到 200 却没有画面）。

路线图编号：#43 `cascade-media-relay`。

## What Changes

- **级联媒体中继（B2BUA）**：上级对本平台的某通道发起 `m=video` INVITE 且该通道属于某个在线下级时，本平台不再自己应答媒体，而是：一面与上级建立对话 A（自己的接收端口），一面向下级发起对话 B（自己的发送/接收端点），把 B 收到的 PS 帧转发到 A。
- **传输桥接**：两条腿的传输独立——上级用 UDP 而下级用附录 D TCP（或反之）都能工作。
- **对话成对传播**：上级 BYE/CANCEL 结束 B；下级 BYE 结束 A 并向上级发 BYE；任一腿建立失败时以可辨识状态码回答上级（下级不可达 502、下级拒绝 480、无匹配下级 404）。
- **录制联动**：中继的 PS 流同样进入实时会话总线，因此自动被 #42 的录像库记录，并可用于 `/v1/flv` 预览。
- **多级级联**：中继只做一跳，下一跳仍是同一逻辑；`X-RoutePath` / `X-PreferredPath` 的既有语义不变。

## Capabilities

### New Capabilities

- `cascade-media-relay`: 平台作为 B2BUA 转发实时视频：按 INVITE 目标通道解析下级、双对话建立与 PS/RTP 桥接、失败的成对终止。

### Modified Capabilities

- `platform-large-node`: 平台受理指向下级通道的实时视频 INVITE 时改为中继而非本地应答。
- `cascade-routing`: 补充媒体转发与既有 MESSAGE 转发并存的路径规则。

## Impact

- `internal/app`：INVITE 分派新增中继分支；中继对话表（上级 Call-ID ↔ 下级 Call-ID）；PS 帧转发与 BYE/CANCEL 成对传播；复用 `live_session` 的会话总线与 `mediaSink`。
- `internal/interface/http`：无新端点（可通过既有 live/recordings 观察结果）。
- 测试：app 单测（双对话、桥接、BYE 双向、失败映射）+ e2e（上级经平台拉下级流）。

## Non-goals

- 不做多跳媒体转码或转封装（PS 原样转发）。
- 不做音频/对讲的中继（本 change 只做实时视频）。
- 不做 SRTP/TLS 媒体。
- 不改设备节点的应答行为。
- 不实现「下级是另一个模拟平台」时的特殊优化——仍是正常的 SIP 级联。
