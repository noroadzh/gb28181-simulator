# Design

## Context

设备侧已有对讲（音频）与回放（时间窗）两条 INVITE 路径，都以「每对话一个 UDP 端口 + BYE 释放」实现；媒体管道 `MediaService.PacketizeOutbound` 能把任意媒体源打成 PS/RTP。本项只是把**实时视频**接到同一套模式上。

## Decisions

### Decision 1: 设备分支按 SDP 分派

`p.isDevice` 时：body 含 `s=Playback` → 回放；`m=audio` → 对讲；`m=video` → 实时视频；其余 488。顺序固定，避免回放（也是 video）被当成直播。

### Decision 2: 通道来自 To 的 user 部分

实时 INVITE 没有 `u=` 行；国标约定 `To: <sip:<channelID>@…>`。取 To 的 user，为空则 400。媒体源仍按「通道级优先、节点级兜底」解析，无源 404。

### Decision 3: 复用 PacketizeOutbound 与每对话端口

应答里给出本节点新开的 UDP 端口（`a=sendonly`、`PS/90000`），随后把源持续打到 offer 的 `c=` + `m=video` 端点。BYE 关闭该端口，写失败即结束发送。

### Decision 4: 文件源需要有 loop

实时语义要求源不结束；`file` 源未开 `loop` 时会在文件末尾自然停流（端口关闭、对话留在应用层）。这是配置问题而非错误，文档写明；合成源永不结束。

## Risks / Trade-offs

- **R1 真实平台可能要求 UDP 之外的传输**（TCP/TLS 媒体）→ 本项只做 UDP；附录 D 与 SRTP 在 #40 的后续任务。
- **R2 编码必须是平台可解的 PS/96** → 设备发送的是源的原编码，不做转码；与现有直播 FLV 一致。
- **R3 同一通道被多路拉流** → 每对话独立端口，互不影响；源被多次打开（文件源会重复读），可接受。

### Decision 5: GB35114 用进程级身份

一个进程扮演一个网关，SM2 密钥对属于进程而非每个模拟节点：配置放在 `security.gb35114`（`enabled` / `note_integrity` / `sm2_private_key_file` / `sm2_public_key_file`），启动时读入并校验，随后一次性注入 acceptor 与 registrar。逐节点密钥需要节点表 schema 与管理 API 的额外设计，当前没有这个需求；进程级身份让「开一个模拟网关做互认证」成为一条配置。密钥文件读取放在 `platform/config`（不依赖 adapter），SM2 解析在主程序装配处完成，保持分层。

### Decision 6: 设备侧也验证平台签名

`SecurityInfoSigner` 已经让平台对**设备发出的 Digest response** 签名；设备只要用同一身份的公钥验证该签名，就完成了真正的互认证。因此 registrar 在 2xx 分支调用作者器（`port.SecurityInfoVerifier`，adapter 实现）验证 `SecurityInfo`，失败即注册失败。没有该头或没有配置公钥时原样放行——MD5 平台与未启用 GB35114 的部署行为不变。

### Decision 7: CANCEL 按事务状态分两种结局

RFC 3261 §9.2 把 CANCEL 的语义绑在 INVITE 事务上：INVITE 尚未产生最终响应时，CANCEL 得 200、INVITE 得 487 Request Terminated；一旦最终响应已发出，事务不复存在，CANCEL 得 481。acceptor 在读循环里为 INVITE 登记 `pendingInvites`（node + Call-ID），处理完立即清除，因此 487 分支只在处理期间被 CANCEL 追上时才走——本 UAS 同步应答，所以正常情形是 481。

### Decision 8: 对话方法不再被 device 的 405 门拦下

device 早先只接受 MESSAGE，其余一律 405。媒体对话落地后 BYE/CANCEL 是平台关闭对话的唯一手段，405 会让平台无法挂断；因此设备门放行 INVITE/ACK/BYE/CANCEL，405 只留给真正的无关方法（OPTIONS/SUBSCRIBE/INFO）。Allow 相应改为 `MESSAGE, INVITE, ACK, BYE, CANCEL`。re-INVITE 则先关闭同一 Call-ID 的旧媒体管线再建新的，避免旧 goroutine 继续向旧端点发送。
