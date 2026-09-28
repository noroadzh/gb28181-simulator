# 提案：mp4 容器接入与 SIP 订阅/INFO 缺口补齐

## 背景

本变更是路线图 **#8（`media-sources`）、#10（`dynamic-sim-features`）、#11（`gb28181-2022-extensions`）三个阶段完成后的缺口补齐增强**，对应路线图阶段编号 **#8+#10+#11**（增量补丁，不新增阶段）。Change 8 交付了四种媒体源的统一抽象，但本地文件源只接受裸 Annex-B 字节流；Change 10 交付了目录/报警/移动位置/回放的动态行为，但平台侧入站 SUBSCRIBE 仅接受 `catalog` 事件，报警与移动位置订阅直接被 489 拒绝；Change 11 交付了 2022 扩展能力协商，但 `INFO` 方法（2022 §G MediaStatus 通知与 MANSRTSP 回放控制的标准载体）在 acceptor 的方法分发中不存在，走到 default 分支被静默丢弃——与 `exception-injection` spec 中"默认静默丢弃"的旧行为耦合，而 RFC 3261 要求不认识的方法必须回 `405 Method Not Allowed`。

用户确认本期范围：**本地文件容器只做 mp4**（其余容器后续再说）；**三个 SIP 缺口全部补齐**（入站订阅扩展到 alarm/position、405+INFO 应答、INFO+MANSRTSP）。

## 变更内容

1. **mp4 容器 demuxer（本地文件源增强）** — `FileSource.Open` 对文件做魔数嗅探（offset 4 处 `ftyp` box）：命中 mp4 时经由新增的 `MP4Demuxer` 解析 moov/trak/stbl，将 AVCC 长度前缀 NALU 转为 Annex-B 起始码格式，AAC 从 esds 读 ASC 并生成 ADTS 帧，按 `stts/stsc/stsz/stco` 逐 sample 产出并携带容器 PTS（换算到 90 kHz 域）；非 mp4 文件走既有裸流路径，字节级行为不变。EOF 后自动循环回到首个 sample。引入 `github.com/abema/go-mp4`（纯 Go，无 CGO）。

2. **逐帧 PTS 的可选接口** — `port` 新增可选接口 `ESFrameReader`（签名与 `ESReader.Read` 一致）；`MediaService.OpenSource` 对 `Open` 返回的 reader 做类型断言，实现该接口（即 mp4 demuxer）时直接作为 `ESReader` 使用以保留容器真实 PTS，否则照旧包装 `StreamESReader`（FPS 合成 PTS）。`port` 与 `app` 均不感知 mp4。

3. **入站 SUBSCRIBE 扩展到 alarm/mobileposition** — `handleSubscribe` 的事件白名单从 `{catalog}` 扩展为 `{catalog, alarm, mobileposition}`（大小写不敏感）；订阅者结构 `catalogSub` 增加事件类型字段；`SubscribePort` 接口签名扩展 `event` 参数（现有 stub 实现同步更新，breaking 安全）。alarm 订阅的初始 NOTIFY 为空体（无挂起报警时）；mobileposition 订阅的初始 NOTIFY 携带该通道 Profile 的当前位置（无位置配置时为空体）。报警触发（`AlarmNotify` MESSAGE 路径）与位置更新（`SetPosition` runtime API）分别向对应事件的活跃订阅者推送 NOTIFY。

4. **未知方法回 405 + INFO 方法分发** — acceptor 方法分发的 default 分支从"静默丢弃 + debug 日志"改为回 `405 Method Not Allowed`，`Allow` 头列出全部支持方法；`faultUnsupported` 钩子保持更高优先级（fault 配置了应答仍按 fault）。新增 `case "INFO"`：按 `Content-Type` 分发——`Application/MANSRTSP`（或 body 以 `PLAY`/`PAUSE` 开头）走 MANSRTSP 回放控制，`Application/MANSCDP+XML` 且 `CmdType=MediaStatus` 走既有 `handleMediaStatus`，其余 200 OK 宽容应答。INVITE/OPTIONS 响应中的 `Allow` 头补上 `INFO`。

5. **MANSRTSP 解析器与 PlaybackPort 扩展** — 新 `internal/adapter/mansrtsp` 包解析 2022 §G body：请求行（`PLAY`/`PAUSE`）、`Scale`、`Range: npt=` 头；映射到 `PlaybackPort`（接口扩展 `Scale`/`Seek` 参数，`PlaybackState` 增加 `Scale` 字段）。同时把 2016 风格 `PlaybackControl` MESSAGE 路径从"仅 200 OK"接通到 playback port（现状缺口一并修复）。

## 能力边界

### 新增能力

- `media-sources`（ADDED）：本地文件源支持 mp4 容器——魔数嗅探、AVCC→Annex-B、AAC→ADTS、容器 PTS、EOF 循环。
- `core-sip-stack`（ADDED）：未知方法回 405 + Allow；`INFO` 方法按 Content-Type 分发（MANSRTSP 回放控制 / MANSCDP MediaStatus）。
- `dynamic-catalog-alarm-and-playback`（ADDED）：入站 SUBSCRIBE 接受 `alarm` 与 `mobileposition` 事件并按事件类型推送 NOTIFY；初始 NOTIFY 语义。

### 修改的能力

- `exception-injection`（MODIFIED）：`不支持方法的应答可配置` 需求的默认行为从"静默丢弃 + debug 日志"改为"回 405 Method Not Allowed + Allow 头"；`UnsupportedMethod` 显式配置仍优先。

## 非目标

- mkv / ts / flv 等其他容器 — 用户明确本期只做 mp4。
- 出站 SUBSCRIBE（设备侧主动向上级订阅）— `SubscribePort` 仍是 stub，本次仅改签名不补实现。
- MANSRTSP 的 `TEARDOWN`、`OPTIONS`、绝对时间 Range（`clock=`）— 本期仅 `PLAY`/`PAUSE`/`Scale`/`npt` Range。
- 报警/位置订阅的 dialog 内 re-SUBSCRIBE 刷新与 NOTIFY 事件包状态机（订阅超时沿用既有 per-Call-ID 定时器模型）。
- mp4 的加密轨道、多视频轨、B 帧乱序重排（PTS/DTS 分离输出）。

## 影响范围

- `internal/domain/port` — `subscribe.go` 签名扩展 event；`media.go` 新增 `ESFrameReader` 可选接口；`playback.go` 扩展 Scale/Seek。
- `internal/domain/model` — 新增 `MobilePositionNotify` 值对象。
- `internal/adapter/media` — `file_source.go` 魔数嗅探分支；新增 `mp4_demuxer.go`。
- `internal/adapter/mansrtsp` — 新增包：MANSRTSP body 解析器（含 golden test）。
- `internal/adapter/manscdp` — 新增 `MobilePositionNotify` marshaler。
- `internal/adapter/subscribe`、`internal/adapter/playback` — stub 同步新签名。
- `internal/app` — `acceptor.go`（SUBSCRIBE 白名单、NOTIFY 分发、405 default、INFO case）；`media_service.go`（ESFrameReader 断言）。
- `cmd/gb28181-simulator/main.go` — factory 无需改动（嗅探在 FileSource 内部完成）。
- `go.mod` — 新增 `github.com/abema/go-mp4`。
