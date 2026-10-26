# Design

## Context

见 `proposal.md`。相关既有件：`channel-http-api` 已定成功路径；`internal/adapter/manscdp` 有目录/报警编解码；`internal/adapter/flv` 已能拉 RTP 转 FLV；`NodeService` 里并存一套无人调用的旧桩（`SendPTZ`/`ListRecords`/`StartPlayback`/`ControlPlayback`）。

## Decisions

### Decision 1: 归零先行，且用既有哨兵类型

`PTZ` 返回 `port.PTZUnsupported(...)`、`Records` 返回 `port.RecordUnsupported(...)`；HTTP 层用 `errors.As` 映射到 501。不新造错误类型——这两个哨兵已存在且语义正确。

### Decision 2: 下发目标只认「本节点已知的在线下级」

PTZ / 录像查询 / 回放的目标是 device 节点自身（本进程内的 channel），经该节点已绑定的 transport 发 MESSAGE。不做跨平台转发（Non-goal）。找不到在线节点 → 502。

### Decision 3: 回放会话由 NodeService 持有

会话表 `map[nodeID|channelID]playbackSession{handle, transport, cancel}`。重复 play 先 teardown 旧会话；teardown 释放端口并发 BYE；空闲超时自动回收。这样做而不是把状态塞进 HTTP 层，便于 scenario 引擎复用。

### Decision 4: FLV 历史流复用现有媒体网关

回放流与直播流同样是 RTP→FLV，复用 `internal/adapter/flv`；差异只在「源的建立方式」（INVITE 拉历史而非读本地文件）。若探查发现网关与源强绑定，先交付 play/pause/teardown/speed 的控制面，把 FLV 历史流拆为后续任务并在 tasks 中显式标注。

### Decision 5: 删除旧桩

`SendPTZ`/`ListRecords`/`StartPlayback`/`ControlPlayback` 无调用方，直接删除，避免两套语义并存误导读者。

### Decision 6: 录像查询的请求/应答事务放在 acceptor

acceptor 是节点 socket 的唯一读者，事务放别处必然收不到应答。它维护一张与级联中继同构的表：键为 (nodeID, Call-ID, CSeq)，值是等待者通道；等待上限 controlWait（默认 5s），超时返回 ErrControlTimeout（HTTP 504），发送失败返回 ErrControlTargetUnreachable（502）。NodeService 只做目标解析并委托。

## Risks / Trade-offs

- **R1 真实设备应答差异** → RecordInfo 应答的字段缺失要按空值处理而不是整条丢弃，并记 warn。
- **R2 FLV 历史流工作量可能超预期** → Decision 4 的拆分兜底。
- **R3 归零期间 UI 报错** → 这是有意的：501 是诚实的失败，比静默 200 更易被接受。
