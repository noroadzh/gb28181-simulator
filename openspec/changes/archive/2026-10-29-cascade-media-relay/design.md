# Design

## Context

平台有两种既有的 INVITE 处理：设备侧（talk / playback / live，`answerDevice*Invite`）与平台侧通用分支（`handleInvite` 尾部：建 dialog、建 `nullESWriter` 入站管道、回 200，媒体丢弃）。#41 给平台加了主动拉流（`StartLive` → `liveSession` + PS 总线），#42 在总线旁挂了录像。

级联 MESSAGE 转发已经在 `cascade_relay` 里按 `relayKey{node, callID, cseq}` 做事务级转发；INVITE 从未进入那张表。

## Goals / Non-Goals

**Goals:**

- 上级对下级通道的实时视频 INVITE 走中继，媒体真正到达上级。
- 两侧传输独立协商；任一侧的结束传播到另一侧。
- 中继流复用 #41 的会话总线，从而自动获得 FLV 预览与 #42 的录像。

**Non-Goals:**

- 音频/对讲中继、SRTP/TLS、转码、下级为模拟平台时的特殊捷径。
- 不改 MESSAGE 的级联转发。

## Decisions

### Decision 1: 复用实时会话，而不是另建一套媒体路径

中继的「下级腿」就是一次 `StartLive`：它已经负责发 INVITE、收 RTP、解 PS、写总线、处理 BYE 与超时。差别只在「谁在消费总线」——中继把总线同时接到一个 RTP 发送器（发给上级），并让它与上级对话绑定。

*备选*：为级联写独立的媒体转发管线。否决：会复制 UDP/TCP 接收、SSRC 过滤、看门狗与清理，且录像/预览要再接线一次。

### Decision 2: 上级腿用 `mediaSink` 反向

#40 的 `mediaSink` 描述「向对端发 RTP」；中继的上级腿需要的是「从对端收 RTP」加上「向对端发 RTP」。因此中继为上级腿绑定一个 `answerDeviceVideoInvite` 式的接收端口（平台自己应答 offer），再把总线上的 PS 帧经 `NewRTPizer` 打包发给上级端点。发送侧与 #42 的录像回放共用同一套 RTP 打包。

### Decision 3: 中继表以「上级 Call-ID」为主键

同一通道可能被多个上级同时拉取，下级侧每次中继都是一次独立的 `StartLive`（按 (节点, 通道) 去重会误杀）。因此中继不走 `liveSession.key`，而是：
- 每条中继创建自己的下级会话，key 用 `(node, channel, upstreamCallID)`；
- 上级 Call-ID → 中继记录（下级 Call-ID、发送 sink、接收端口）。

这要求 `StartLive` 拆出一个可指定会话标识的内核，或在 `liveSession` 上允许显式 key。采用后者：`startLiveWithKey`，`StartLive` 是它的公开包装。

*备选*：按通道去重、共享下级会话。否决：第二个上级的 BYE 会杀掉第一个的画面。

### Decision 4: 失败的映射与清理

下级不可达（`ErrControlTargetUnreachable`）→ 502；下级拒绝（`ErrLiveRefused`）→ 480；超时（`ErrControlTimeout`）→ 504；无目标（`ErrLiveNoTarget`）→ 404。任何失败在返回前关闭已开的接收端口，不留半对话。

### Decision 5: 结束成对传播

上级 BYE：读循环既有的 BYE 钩子按 Call-ID 找到中继记录 → 结束下级会话（发 BYE）→ 释放 sink。
下级 BYE：`StartLive` 的会话结束回调 → 若该会话挂着中继，向上级发 BYE 并清理。
CANCEL/超时沿用同一入口。

### Decision 6: 预览与录像

中继的下级会话与普通拉流共用总线存储，因此 `LivePS` 与录像器都能看到它。为避免同一通道「中继会话」与「平台自拉会话」互相覆盖，`LivePS` 在有多条会话时取最近活跃的一条，并在日志中标注来源。

## Risks / Trade-offs

- **一通道多上级** → 每个上级一条下级会话，设备侧会收到多条 INVITE；真实网关也如此。设备不支持并发时可后续加共享。
- **PS 原样转发，不重打时间戳** → RTP 序列号/时间戳由发送端重新生成，PS 的 PTS 保持不变；上级按 PTS 播放。
- **中继与本地拉流同时存在时的录像归属** → 都写入同一 (节点, 通道) 的录像库，分段按时间区分。
- **接收端口的媒体时钟** → 与 #42 一致，不做墙钟节流。

## Migration Plan

纯增量：INVITE 分派新增分支，MESSAGE 路径不变。关闭方式为不配置级联下级；回滚即移除分支。

## Open Questions

无。
