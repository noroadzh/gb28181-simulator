# platform-small-node Specification

## Purpose

platform-small（小平台）身份的行为规范。本 capability 覆盖它作为级联链中间那一环的
双向身份：对下是受理注册、应答目录、清扫超时的上级平台（UAS），对上是注册、保活、续期
的下级平台（UAC），两段配置可独立声明，生命周期作为一个节点推进与回卷。

---

## Requirements

### Requirement: Platform-small node plays both halves of a cascade

A `platform-small` node SHALL accept downstream registrations on its own transport and SHALL
register with an upstream platform when one is declared: it acts as a UAS on its own transport and
as a UAC against its upstream. Serving is inherent to being a platform — an omitted `platform:`
section means "serve with the defaults", never "do not serve" — while the upstream registration is
optional and is what makes the node a link in a cascade. Both halves SHALL run over the node's own
transport, so two platform-small nodes never share signalling state.

级联拓扑 `A → B → C` 中的 B 必须既是 C 的上级（受理注册）又是 A 的下级（注册上报），两半互不
干扰，且各自走自己那个监听端口。

#### Scenario: 两半同时存在时依次建立

- **WHEN** 一个 platform-small 节点同时声明了 `platform:`（受理）与 `registration:`（注册）
  并被启动
- **THEN** 它先在自身 transport 上受理下级注册
- **AND** 随后向声明的上级平台发出 REGISTER（含 401 挑战与 Digest 应答）
- **AND** 两半都成功后节点处于 `online`

#### Scenario: 只有受理配置时只做上级

- **WHEN** 一个 platform-small 节点只声明了 `platform:`（或省略该段而取域默认）
- **THEN** 它受理下级注册，不向任何上级注册，也不启动保活

#### Scenario: 省略受理配置时仍以默认值受理

- **WHEN** 一个 platform-small 节点只声明了 `registration:` 而未声明 `platform:`
- **THEN** 它向上级注册并保活
- **AND** 同时以域默认的 realm 与有效期窗口受理下级注册——"是平台"就意味着受理

#### Scenario: 两半共用一个监听口，消息按方向分捡

- **WHEN** 一个 platform-small 节点在同一监听口上既受理又注册
- **THEN** 到达的请求交给受理半，到达的响应交给注册半，两半互不取走对方的消息
- **AND** 两半都从同一 socket 发出，对端只看到一个节点一个地址

#### Scenario: 两个 platform-small 互不影响

- **WHEN** 两个 platform-small 节点同时在线，各自有下级与上级
- **THEN** 每个节点的在线表、注册结果与保活都只属于它自己，一方的失败不影响另一方

### Requirement: Platform-small node registers with its upstream like a device

A `platform-small` node configured with `registration:` SHALL complete the same registration
transaction a device does — REGISTER, answer the upstream's `401` Digest challenge, take the
granted lifetime — and SHALL then keep that registration open with periodic MESSAGE keepalives and
a re-registration before the granted lifetime lapses. A registration failure SHALL fault the node
and leave no half-started state behind.

小平台作为下级向上级注册时，走的是与设备完全相同的注册/保活/续期流程：它不会因为"自己也是
平台"而在协议上享有特权。

#### Scenario: 注册到上级并取得有效期

- **WHEN** 一个配置了 `registration:` 的 platform-small 节点被启动
- **THEN** 它完成 REGISTER 事务（必要时应答 401 挑战）
- **AND** 记录上级授予的有效期，并在该有效期过半前重注册

#### Scenario: 上级不可达时节点异常

- **WHEN** 注册超时、被拒绝或返回 5xx
- **THEN** 节点进入 `fault`，已建立的下级受理一并回滚，端口释放
- **AND** 失败原因写入节点结果，不含凭据

#### Scenario: 保活随节点停止

- **WHEN** 停止一个在线的 platform-small 节点
- **THEN** 保活与重注册立即停止，向上发出注销（`Expires: 0`）后不再发送任何消息

### Requirement: Platform-small node accepts and serves its own downstreams

A `platform-small` node SHALL accept REGISTERs from its downstreams on its own transport,
challenge them in its realm, refuse accounts it does not hold, and record the devices that get
through in an online device table of its own. It SHALL answer a MANSCDP `Catalog` query with that
table, and SHALL sweep out rows whose granted lifetime lapsed. The behaviour SHALL be identical to
a platform-large node's acceptance, differing only in which nodes it is offered to.

小平台的受理行为与大平台一致：挑战、拒绝、落表、应答目录、超时踢线。差别只在"谁能是它"。

#### Scenario: 下级注册进小平台的在线表

- **WHEN** 一个设备向 platform-small 节点注册并通过鉴权
- **THEN** 该设备出现在该节点的在线表中，可读于 `GET /v1/nodes/{id}/devices`

#### Scenario: 上级向小平台查询目录

- **WHEN** 上级平台向该 platform-small 节点下发 `CmdType = Catalog` 的查询
- **THEN** 小平台以**自己**在线表作答（按 device id 升序、回抄 SN），不把别处的设备掺进来

#### Scenario: 失联下级被踢下线

- **WHEN** 某条下级记录的授权有效期已过且未重注册
- **THEN** 清扫将其移出在线表，后续目录应答不再包含它

### Requirement: Platform-small serving and registration are independently configurable

The system SHALL accept `platform:` (realm, accounts, expires window) on a `platform-small` entry
and SHALL accept `registration:` on the same entry; both sections remain optional with the
documented defaults. Configuration validation MUST NOT reject a `platform-small` entry for
declaring either or both, and MUST still reject malformed values in either section.

`platform:` 不再是 platform-large 专属；`registration:` 也不再是 device 专属。两者对
platform-small 同时开放，校验规则不变。

#### Scenario: 两段并存通过校验

- **WHEN** 一个 platform-small 条目同时声明 `platform:` 与 `registration:`
- **THEN** 配置加载成功，两段都生效

#### Scenario: 非法值仍然被拒

- **WHEN** `platform.accounts` 重复 username 或空密码、`realm` 为空、有效期三档不满足
  `0 < min ≤ default ≤ max`，或 `registration` 缺少 server / 非法心跳参数
- **THEN** 配置加载返回错误并指出条目序号与字段名，进程不启动

#### Scenario: 省略时取默认值

- **WHEN** platform-small 条目省略 `platform:` 段
- **THEN** realm 取该节点 domain，有效期窗口取 60 / 3600 / 86400 秒，节点仍受理注册

### Requirement: Platform-small lifecycle advances and unwinds as one node

Starting a platform-small node SHALL advance it to `online` once, however many halves it
establishes: a second half MUST NOT fail because the node is already `online`. If any half fails,
the node SHALL fault and the other half SHALL be unwound. Stopping SHALL end serving, stop
keepalives, unregister from the upstream and leave the node `offline`.

节点只有一个状态机：两半是同一个节点的两个动作，第二次推进不得撞上状态机的边，任何一半失败
都要把另一半回滚。

#### Scenario: 第二半不再重复推进状态

- **WHEN** 受理已把节点推到 `online`，随后注册成功
- **THEN** 注册只记录结果与启动保活，不再要求 `registered → online` 转换，也不报错

#### Scenario: 注册失败时受理被回滚

- **WHEN** 受理已建立，随后的向上注册失败
- **THEN** 受理 goroutine 停止、在线表清空，节点进入 `fault`

#### Scenario: 停止后节点干净离线

- **WHEN** 停止一个两半都在运行的 platform-small 节点
- **THEN** 受理停止、保活停止、向上注销完成，节点状态为 `offline`，无残留 goroutine

### Requirement: Platform-small node subscribes to downstream directory changes

A `platform-small` node SHALL support sending MANSCDP `Subscribe` requests with `CmdType = Catalog`
to its online downstreams and SHALL consume the resulting `Notify` (`CmdType = Catalog`) messages
to keep an internal catalogue view in addition to its online device table. The catalogue view SHALL
be used to answer upstream catalog queries with the latest known status, not only a registration-time
snapshot. A `Subscribe` failure (timeout, 4xx, 5xx) SHALL be retried with exponential backoff and
SHALL NOT fault the node.

订阅让小平台的目录不再只是启动时拍的一张快照：变更会通过 Notify 进来。失败可重试，
不应让节点 fault。

#### Scenario: 下级注册后发送目录订阅

- **WHEN** 一个下级设备通过 platform-small 注册成功并进入在线表
- **THEN** platform-small 在可配置延迟后向该设备发送 `Subscribe`（`CmdType = Catalog`），
  设置 `Expires` 为域默认目录订阅周期

#### Scenario: 收到下级目录变更通知

- **WHEN** 平台收到下级发来的 `Notify`（`CmdType = Catalog`）且 SN 大于本地记录
- **THEN** 合并变更到本地目录视图，记录 `SN`，并在收到上级目录查询时返回最新视图

#### Scenario: 订阅失败重试

- **WHEN** `Subscribe` 请求超时或收到 4xx/5xx
- **THEN** 平台按指数退避重试，最长重试间隔不超过 5 分钟，且节点不进入 `fault`

#### Scenario: 订阅在停止时撤销

- **WHEN** 一个 platform-small 节点被停止
- **THEN** 它向所有在线下级发送 `Subscribe`（`Expires: 0`）撤销订阅，然后才停止受理

### Requirement: Platform-small node handles upstream INVITE for live stream

A `platform-small` node SHALL handle `INVITE` requests from its upstream platform by parsing the
SDP, allocating a Dialog/Session, and assembling an inbound media pipeline
(`RTPDeizer → PSDepacketizer → ESWriteCloser`). The Dialog SHALL be tracked by `Call-ID` and
SHALL transition through `calling → confirmed → terminated`. The node SHALL respond with
`200 OK` after the pipeline is bound and SHALL terminate cleanly on `BYE` or session timeout.

INVITE 让小平台真正成为中继：上级来的媒体流被组装成本地 ES 帧；下级去时也用同样的组装。

#### Scenario: 上级 INVITE 建立 Dialog

- **WHEN** platform-small 收到上级发来的 `INVITE`（含 SDP）
- **THEN** 解析 SDP，分配 `Call-ID` 对应的 Dialog，进入 `calling`
- **AND** 组装入站媒体管道并绑定到该 Dialog
- **AND** 发送 `200 OK`（含 SDP 应答）后进入 `confirmed`

#### Scenario: ACK 确认媒体传输

- **WHEN** platform-small 在 `confirmed` 状态下收到 `ACK`
- **THEN** 入站媒体管道开始接收 RTP 包并交付给下级或本地消费者

#### Scenario: BYE 清理 Dialog 与管道

- **WHEN** platform-small 收到 `BYE` 对应一个已确认的 Dialog
- **THEN** 关闭入站媒体管道，删除 Dialog 映射，发送 `200 OK`

#### Scenario: 超时未 ACK 自动终止

- **WHEN** Dialog 在 `calling` 状态停留超过配置超时（默认 30 秒）未收到 `ACK`
- **THEN** Dialog 进入 `terminated`，管道关闭，资源释放

### Requirement: Platform-small node probes upstream with OPTIONS keepalive

A `platform-small` node configured with `registration:` SHALL periodically send `OPTIONS` requests
to its upstream platform, in addition to MESSAGE keepalives. The node SHALL distinguish between
`200` (normal), `408` (timeout), and `5xx` (fault) responses and SHALL log a warning after
configurable consecutive failures without faulting the node while the registration is still valid.
This complements MESSAGE keepalives by detecting cases where the upstream is unreachable but the
registration has not yet lapsed.

OPTIONS 让小平台在注册仍有效但上级无响应时提前发现。

#### Scenario: 周期性 OPTIONS 探测

- **WHEN** 一个 platform-small 节点注册成功并在线
- **THEN** 它以可配置周期（默认 60 秒）向上级发送 `OPTIONS`，响应 `200` 计为正常

#### Scenario: OPTIONS 连续失败告警

- **WHEN** 连续 N 次（默认 3 次）OPTIONS 探测未收到 `2xx` 响应
- **THEN** 记录警告日志（不含凭据），节点仍维持当前状态

#### Scenario: OPTIONS 不替代 MESSAGE 保活

- **WHEN** 节点处于在线状态
- **THEN** MESSAGE 保活与 OPTIONS 探测并行运行，任一独立超时不会取消另一个

### Requirement: Platform-small node reports and queries MediaStatus

A `platform-small` node SHALL consume MANSCDP `Notify` messages with `CmdType = MediaStatus`
from its downstreams and SHALL maintain a per-channel status view. The node SHALL be able to
respond to upstream `MediaStatus` queries with the latest known status and SHALL forward
`MediaStatus` notifications from downstreams to the upstream when relevant.

MediaStatus 让上级能看到媒体通道的实时状态（在线/离线、录制中等），而不仅看注册表。

#### Scenario: 收到下级 MediaStatus 通知

- **WHEN** 平台收到下级发来的 `Notify`（`CmdType = MediaStatus`）
- **THEN** 解析 `DeviceID` 与 `NotifyType`（`121`/`122`），更新本地通道状态视图

#### Scenario: 上级查询 MediaStatus

- **WHEN** 上级下发 `Query`（`CmdType = MediaStatus`）
- **THEN** 平台以本地通道状态视图作答，按设备/通道分组，回抄 `SN`

#### Scenario: MediaStatus 解析失败不阻塞

- **WHEN** 下级发来的 MediaStatus XML 缺少必填字段或格式非法
- **THEN** 记录警告日志，丢弃该通知，不影响其它通知或当前在线表

### Requirement: Platform-small handles INVITE for media playback

A `platform-small` node SHALL accept an INVITE from its upstream platform addressed to one of its
downstream devices, parse the SDP offer, and answer `200 OK` with an SDP answer that carries the
node's own media address. The node SHALL establish a Dialog tracked by Call-ID, start an inbound
media pipeline (RTP listener → RTPDeizer → PSDepacketizer → ES sink) bound to that Dialog's
lifetime, and SHALL tear it down on BYE or on a dialog timeout. An INVITE with an unreadable or
empty SDP body SHALL be answered `400 Bad Request`.

platform-small 收到上游 INVITE 点播时，必须解析 SDP、应答 200 OK 并启动入站媒体管道；
BYE 或超时后必须清理管道并释放端口。

#### Scenario: 上游 INVITE 点播成功建立

- **WHEN** 上级平台向 platform-small 发送 INVITE，请求某设备的实时视频流
- **THEN** platform-small 解析 SDP，应答 `200 OK` + 本端 SDP
- **AND** 在 SDP 声明的端口上启动 RTP 监听
- **AND** 建立 Dialog 跟踪，等待 ACK

#### Scenario: ACK 确认后 Dialog 进入 confirmed

- **WHEN** 上级平台发送 ACK 确认 200 OK
- **THEN** Dialog 状态从 `proceeding` 推进到 `confirmed`
- **AND** 入站媒体管道持续运行直到 BYE

#### Scenario: BYE 清理媒体资源

- **WHEN** 任一方发送 BYE 结束点播
- **THEN** platform-small 停止 RTP 监听、关闭 PS/RTP 解封装器
- **AND** 应答 `200 OK`，Dialog 进入 `terminated`
- **AND** 监听端口被释放

#### Scenario: 无效 SDP 被拒绝

- **WHEN** INVITE 的 SDP 体不可读或缺少 `m=` 行
- **THEN** platform-small 应答 `400 Bad Request`，不建立 Dialog、不分配端口

#### Scenario: Dialog 超时自动清理

- **WHEN** INVITE 后 30 秒内未收到 ACK
- **THEN** Dialog 进入 `terminated`，已分配的端口与管道被释放

### Requirement: Platform-small handles SUBSCRIBE for catalog push

A `platform-small` node SHALL accept a SUBSCRIBE from its upstream for the `catalog` event,
record the subscription with its Expires value, and answer `200 OK`. When a downstream device
registers with or is swept from the platform-small, the node SHALL send a NOTIFY to each active
subscriber with the updated device list. A SUBSCRIBE for an event type other than `catalog` SHALL
be answered `489 Bad Event`.

platform-small 收到上级 SUBSCRIBE 目录订阅时，必须记录订阅关系，并在设备列表变化时
通过 NOTIFY 推送更新。

#### Scenario: 订阅目录成功

- **WHEN** 上级平台发送 SUBSCRIBE，`Event: catalog`，`Expires: 3600`
- **THEN** platform-small 记录订阅关系，应答 `200 OK`
- **AND** 在订阅有效期内，设备列表变化时发送 NOTIFY

#### Scenario: 设备上线触发 NOTIFY

- **WHEN** 一个下级设备注册成功
- **AND** 存在活跃的目录订阅
- **THEN** platform-small 向每个订阅者发送 NOTIFY，携带最新设备列表

#### Scenario: 订阅过期

- **WHEN** 订阅的 Expires 到期
- **THEN** 订阅关系被移除，不再发送 NOTIFY

#### Scenario: 非 catalog 事件被拒

- **WHEN** SUBSCRIBE 的 Event 头不是 `catalog`
- **THEN** platform-small 应答 `489 Bad Event`

### Requirement: Platform-small sends OPTIONS keepalive to upstream

A `platform-small` node configured with `registration.options.enabled: true` SHALL periodically
send an OPTIONS request to its upstream platform to probe the link's liveness. A `200 OK` response
SHALL reset the failure counter; a timeout or `408` SHALL increment it. When the counter reaches
`heartbeat_max_failures`, the node SHALL fault. The OPTIONS interval SHALL be configurable with a
default of 60 seconds.

platform-small 作为下级可定期向上游发送 OPTIONS 探测链路活性，连续超时触发节点 fault。

#### Scenario: OPTIONS 探测成功

- **WHEN** platform-small 向上级发送 OPTIONS
- **AND** 收到 `200 OK`
- **THEN** 失败计数清零，链路状态标记为正常

#### Scenario: 连续超时触发 fault

- **WHEN** OPTIONS 连续 `heartbeat_max_failures` 次未收到响应或收到 `408`
- **THEN** 节点进入 `fault`，端口释放

#### Scenario: 非配置时不发送 OPTIONS

- **WHEN** `registration.options.enabled` 未声明或为 `false`
- **THEN** platform-small 不发送 OPTIONS，保活仅依赖 MESSAGE keepalive

### Requirement: Platform-small parses and forwards MediaStatus

A `platform-small` node SHALL parse a MANSCDP `MediaStatus` notify from its downstream devices,
update the device's media state in its online table, and — when the node itself is registered with
an upstream — forward the status to the upstream platform via a MESSAGE carrying a MediaStatus
notify body. An unreadable MediaStatus body SHALL be ignored without answering an error.

platform-small 解析下级 MediaStatus 上报，更新设备媒体状态，并在自身有上级时转发。

#### Scenario: 解析并记录媒体状态

- **WHEN** 下级设备发送 MESSAGE，`CmdType = MediaStatus`
- **THEN** platform-small 解析视频参数（分辨率、码率、帧率），更新在线表中的媒体状态
- **AND** 应答 `200 OK`

#### Scenario: 向上级转发媒体状态

- **WHEN** platform-small 自身已注册到上级，且收到下级 MediaStatus
- **THEN** platform-small 构造 MediaStatus NOTIFY，通过 MESSAGE 发送给上级

#### Scenario: 不可读的 MediaStatus 被忽略

- **WHEN** MediaStatus 消息体不可解析
- **THEN** platform-small 不应答错误，打 debug 日志后继续 serving

### Requirement: Platform-small message dispatch is transaction-aware

A `platform-small` node SHALL route SIP messages by transaction (Call-ID) rather than only by
direction. Requests SHALL be delivered to the serving half; responses SHALL be matched to a
registered transaction handler by Call-ID and delivered to the half that originated the
transaction. Unmatched responses SHALL be silently dropped.

platform-small 的消息分拣按事务（Call-ID）匹配，而非仅按方向分拣。

#### Scenario: INVITE 响应路由到正确的事务处理器

- **WHEN** 上级平台应答 INVITE 的 200 OK 到达
- **THEN** 分拣器按 Call-ID 找到 INVITE 注册的事务处理器，将响应交给它
- **AND** 不会被 serving 半的 REGISTER/MESSAGE 循环取走

#### Scenario: 未匹配的响应被丢弃

- **WHEN** 一个响应的 Call-ID 在事务表中找不到注册者
- **THEN** 该响应被静默丢弃，不触发任何处理
