# platform-small-node 规范

## Purpose

platform-small（小平台）身份的行为规范。本 capability 覆盖它作为级联链中间那一环的
双向身份：对下是受理注册、应答目录、清扫超时的上级平台（UAS），对上是注册、保活、续期
的下级平台（UAC），两段配置可独立声明，生命周期作为一个节点推进与回卷。

---

## Requirements

### 需求：小平台同时承担级联链上下两半身份

`platform-small` 节点必须（MUST）在自身 transport 上受理下游注册，且当声明了上级平台时必须（SHALL）向上级注册：它在一端作为 UAS 受理，在另一端作为 UAC 注册。担任平台本身就意味着受理——省略 `platform:` 段取的是默认受理，绝非"不服务"；向上注册是可选的，正是它将节点变成级联链的一环。两半必须（MUST）都跑在节点自身的 transport 上，因此两个 platform-small 节点绝不共享信令状态。

级联拓扑 `A → B → C` 中的 B 必须既是 C 的上级（受理注册）又是 A 的下级（注册上报），两半互不
干扰，且各自走自己那个监听端口。

#### 场景：两半同时存在时依次建立

- **WHEN** 一个 platform-small 节点同时声明了 `platform:`（受理）与 `registration:`（注册）
  并被启动
- **THEN** 它先在自身 transport 上受理下级注册
- **AND** 随后向声明的上级平台发出 REGISTER（含 401 挑战与 Digest 应答）
- **AND** 两半都成功后节点处于 `online`

#### 场景：只有受理配置时只做上级

- **WHEN** 一个 platform-small 节点只声明了 `platform:`（或省略该段而取域默认）
- **THEN** 它受理下级注册，不向任何上级注册，也不启动保活

#### 场景：省略受理配置时仍以默认值受理

- **WHEN** 一个 platform-small 节点只声明了 `registration:` 而未声明 `platform:`
- **THEN** 它向上级注册并保活
- **AND** 同时以域默认的 realm 与有效期窗口受理下级注册——"是平台"就意味着受理

#### 场景：两半共用一个监听口，消息按方向分捡

- **WHEN** 一个 platform-small 节点在同一监听口上既受理又注册
- **THEN** 到达的请求交给受理半，到达的响应交给注册半，两半互不取走对方的消息
- **AND** 两半都从同一 socket 发出，对端只看到一个节点一个地址

#### 场景：两个 platform-small 互不影响

- **WHEN** 两个 platform-small 节点同时在线，各自有下级与上级
- **THEN** 每个节点的在线表、注册结果与保活都只属于它自己，一方的失败不影响另一方

### 需求：小平台向上级注册的流程与设备一致

配置了 `registration:` 的 `platform-small` 节点必须（MUST）完成与设备相同的注册事务——REGISTER、应答上级的 `401` Digest 挑战、取得授予的有效期——并必须（MUST）随后以周期性 MESSAGE 保活与到期前重注册保持该注册开放。注册失败必须（MUST）使节点进入 `fault`，且不留任何半启动状态。

小平台作为下级向上级注册时，走的是与设备完全相同的注册/保活/续期流程：它不会因为"自己也是
平台"而在协议上享有特权。

#### 场景：注册到上级并取得有效期

- **WHEN** 一个配置了 `registration:` 的 platform-small 节点被启动
- **THEN** 它完成 REGISTER 事务（必要时应答 401 挑战）
- **AND** 记录上级授予的有效期，并在该有效期过半前重注册

#### 场景：上级不可达时节点异常

- **WHEN** 注册超时、被拒绝或返回 5xx
- **THEN** 节点进入 `fault`，已建立的下级受理一并回滚，端口释放
- **AND** 失败原因写入节点结果，不含凭据

#### 场景：保活随节点停止

- **WHEN** 停止一个在线的 platform-small 节点
- **THEN** 保活与重注册立即停止，向上发出注销（`Expires: 0`）后不再发送任何消息

### 需求：小平台受理并服务自己的下游

`platform-small` 节点必须（MUST）在自身 transport 上受理下游的 REGISTER，以其 realm 挑战它们，拒绝不持有的账号，并把通过鉴权的设备记录进自己的在线设备表。它必须（MUST）以该表应答 MANSCDP `Catalog` 查询，且必须（MUST）清扫授权有效期已过的行。该行为必须（MUST）与 platform-large 节点的受理完全一致，仅在于向谁提供。

小平台的受理行为与大平台一致：挑战、拒绝、落表、应答目录、超时踢线。差别只在"谁能是它"。

#### 场景：下级注册进小平台的在线表

- **WHEN** 一个设备向 platform-small 节点注册并通过鉴权
- **THEN** 该设备出现在该节点的在线表中，可读于 `GET /v1/nodes/{id}/devices`

#### 场景：上级向小平台查询目录

- **WHEN** 上级平台向该 platform-small 节点下发 `CmdType = Catalog` 的查询
- **THEN** 小平台以**自己**在线表作答（按 device id 升序、回抄 SN），不把别处的设备掺进来

#### 场景：失联下级被踢下线

- **WHEN** 某条下级记录的授权有效期已过且未重注册
- **THEN** 清扫将其移出在线表，后续目录应答不再包含它

### 需求：小平台的服务与注册能力独立可配置

系统必须（MUST）接受 `platform-small` 节点配置中的 `platform:`（realm、accounts、expires window），且必须（MUST）在同一条目上接受 `registration:`；两段仍为可选并以文档声明的默认值生效。配置校验必须（MUST NOT）因同时声明两者或其中之一而拒绝 `platform-small` 条目，但仍必须拒绝两段中任何非法值。

`platform:` 不再是 platform-large 专属；`registration:` 也不再是 device 专属。两者对
platform-small 同时开放，校验规则不变。

#### 场景：两段并存通过校验

- **WHEN** 一个 platform-small 条目同时声明 `platform:` 与 `registration:`
- **THEN** 配置加载成功，两段都生效

#### 场景：非法值仍然被拒

- **WHEN** `platform.accounts` 重复 username 或空密码、`realm` 为空、有效期三档不满足
  `0 < min ≤ default ≤ max`，或 `registration` 缺少 server / 非法心跳参数
- **THEN** 配置加载返回错误并指出条目序号与字段名，进程不启动

#### 场景：省略时取默认值

- **WHEN** platform-small 条目省略 `platform:` 段
- **THEN** realm 取该节点 domain，有效期窗口取 60 / 3600 / 86400 秒，节点仍受理注册

### 需求：小平台生命周期作为单一节点推进与回滚

启动一个 platform-small 节点时，MUST 将其推进到 `online`（无论建立了多少个 half）：
第二个 half MUST NOT 因节点已是 `online` 而失败。若任意 half 失败，节点 MUST 故障，
另一半 MUST 回滚。停止 MUST 终止服务、停止 keepalives、向上级注销并将节点留在
`offline`。

节点只有一个状态机：两半是同一个节点的两个动作，第二次推进不得撞上状态机的边，任何一半失败
都要把另一半回滚。

#### 场景：第二半不再重复推进状态

- **WHEN** 受理已把节点推到 `online`，随后注册成功
- **THEN** 注册只记录结果与启动保活，不再要求 `registered → online` 转换，也不报错

#### 场景：注册失败时受理被回滚

- **WHEN** 受理已建立，随后的向上注册失败
- **THEN** 受理 goroutine 停止、在线表清空，节点进入 `fault`

#### 场景：停止后节点干净离线

- **WHEN** 停止一个两半都在运行的 platform-small 节点
- **THEN** 受理停止、保活停止、向上注销完成，节点状态为 `offline`，无残留 goroutine

### 需求：小平台订阅下游目录变更

`platform-small` 节点必须（MUST）支持向在线下游发送 `CmdType = Catalog` 的 MANSCDP `Subscribe` 请求，且必须（MUST）消费由此产生的 `Notify`（`CmdType = Catalog`）消息，以在在线设备表之外维护内部目录视图。该目录视图必须（SHALL）用于应答上级目录查询，采用最新已知状态，而非启动时快照。`Subscribe` 失败（超时、4xx、5xx）必须（MUST）以指数退避重试，且必须（MUST NOT）使节点进入 `fault`。

订阅让小平台的目录不再只是启动时拍的一张快照：变更会通过 Notify 进来。失败可重试，
不应让节点 fault。

#### 场景：下级注册后发送目录订阅

- **WHEN** 一个下级设备通过 platform-small 注册成功并进入在线表
- **THEN** platform-small 在可配置延迟后向该设备发送 `Subscribe`（`CmdType = Catalog`），
  设置 `Expires` 为域默认目录订阅周期

#### 场景：收到下级目录变更通知

- **WHEN** 平台收到下级发来的 `Notify`（`CmdType = Catalog`）且 SN 大于本地记录
- **THEN** 合并变更到本地目录视图，记录 `SN`，并在收到上级目录查询时返回最新视图

#### 场景：订阅失败重试

- **WHEN** `Subscribe` 请求超时或收到 4xx/5xx
- **THEN** 平台按指数退避重试，最长重试间隔不超过 5 分钟，且节点不进入 `fault`

#### 场景：订阅在停止时撤销

- **WHEN** 一个 platform-small 节点被停止
- **THEN** 它向所有在线下级发送 `Subscribe`（`Expires: 0`）撤销订阅，然后才停止受理

### 需求：小平台处理上级实时点播 INVITE

`platform-small` 节点必须（MUST）处理来自上级平台的 `INVITE` 请求：解析 SDP、分配 Dialog/Session、组装入站媒体管道（`RTPDeizer → PSDepacketizer → ESWriteCloser`）。Dialog 必须（MUST）以 `Call-ID` 跟踪，且必须（MUST）经历 `calling → confirmed → terminated` 状态迁移。节点必须（MUST）在管道绑定后应答 `200 OK`，并必须在 `BYE` 或会话超时时干净终止。

INVITE 让小平台真正成为中继：上级来的媒体流被组装成本地 ES 帧；下级去时也用同样的组装。

#### 场景：上级 INVITE 建立 Dialog

- **WHEN** platform-small 收到上级发来的 `INVITE`（含 SDP）
- **THEN** 解析 SDP，分配 `Call-ID` 对应的 Dialog，进入 `calling`
- **AND** 组装入站媒体管道并绑定到该 Dialog
- **AND** 发送 `200 OK`（含 SDP 应答）后进入 `confirmed`

#### 场景：ACK 确认媒体传输

- **WHEN** platform-small 在 `confirmed` 状态下收到 `ACK`
- **THEN** 入站媒体管道开始接收 RTP 包并交付给下级或本地消费者

#### 场景：BYE 清理 Dialog 与管道

- **WHEN** platform-small 收到 `BYE` 对应一个已确认的 Dialog
- **THEN** 关闭入站媒体管道，删除 Dialog 映射，发送 `200 OK`

#### 场景：超时未 ACK 自动终止

- **WHEN** Dialog 在 `calling` 状态停留超过配置超时（默认 30 秒）未收到 `ACK`
- **THEN** Dialog 进入 `terminated`，管道关闭，资源释放

### 需求：小平台以 OPTIONS 探测上级链路

配置了 `registration:` 的 `platform-small` 节点必须（MUST）在 MESSAGE 保活之外周期性向上级平台发送 `OPTIONS` 请求。节点必须（MUST）区分 `200`（正常）、`408`（超时）与 `5xx`（故障）响应，并在可配置的连续失败后记录警告日志，但在注册仍有效时不使节点进入 `fault`。这是对 MESSAGE 保活的补充：用于发现上级不可达但注册尚未到期的情形。

OPTIONS 让小平台在注册仍有效但上级无响应时提前发现。

#### 场景：周期性 OPTIONS 探测

- **WHEN** 一个 platform-small 节点注册成功并在线
- **THEN** 它以可配置周期（默认 60 秒）向上级发送 `OPTIONS`，响应 `200` 计为正常

#### 场景：OPTIONS 连续失败告警

- **WHEN** 连续 N 次（默认 3 次）OPTIONS 探测未收到 `2xx` 响应
- **THEN** 记录警告日志（不含凭据），节点仍维持当前状态

#### 场景：OPTIONS 不替代 MESSAGE 保活

- **WHEN** 节点处于在线状态
- **THEN** MESSAGE 保活与 OPTIONS 探测并行运行，任一独立超时不会取消另一个

### 需求：小平台上报与查询 MediaStatus

`platform-small` 节点必须（MUST）消费来自下游的 `CmdType = MediaStatus` 的 MANSCDP `Notify` 消息，并必须（MUST）维护按通道划分的状态视图。节点必须（MUST）能以最新已知状态响应上游 `MediaStatus` 查询，且必须（MUST）在相关时把下游的 `MediaStatus` 通知转发给上级。

MediaStatus 让上级能看到媒体通道的实时状态（在线/离线、录制中等），而不仅看注册表。

#### 场景：收到下级 MediaStatus 通知

- **WHEN** 平台收到下级发来的 `Notify`（`CmdType = MediaStatus`）
- **THEN** 解析 `DeviceID` 与 `NotifyType`（`121`/`122`），更新本地通道状态视图

#### 场景：上级查询 MediaStatus

- **WHEN** 上级下发 `Query`（`CmdType = MediaStatus`）
- **THEN** 平台以本地通道状态视图作答，按设备/通道分组，回抄 `SN`

#### 场景：MediaStatus 解析失败不阻塞

- **WHEN** 下级发来的 MediaStatus XML 缺少必填字段或格式非法
- **THEN** 记录警告日志，丢弃该通知，不影响其它通知或当前在线表

### 需求：小平台处理媒体点播 INVITE

`platform-small` 节点必须（MUST）接受来自上级平台、指向其某台下游设备的 `INVITE`，解析 SDP offer，并以携带本节点媒体地址的 SDP answer 应答 `200 OK`。节点必须（MUST）建立以 `Call-ID` 跟踪的 Dialog，启动绑定到该 Dialog 生命周期的入站媒体管道（RTP 监听器 → RTPDeizer → PSDepacketizer → ES 接收器），且必须（MUST）在 `BYE` 或 Dialog 超时时清理。不可读或为空的 SDP 体的 INVITE 必须（MUST）以 `400 Bad Request` 应答。

platform-small 收到上游 INVITE 点播时，必须解析 SDP、应答 200 OK 并启动入站媒体管道；
BYE 或超时后必须清理管道并释放端口。

#### 场景：上游 INVITE 点播成功建立

- **WHEN** 上级平台向 platform-small 发送 INVITE，请求某设备的实时视频流
- **THEN** platform-small 解析 SDP，应答 `200 OK` + 本端 SDP
- **AND** 在 SDP 声明的端口上启动 RTP 监听
- **AND** 建立 Dialog 跟踪，等待 ACK

#### 场景：ACK 确认后 Dialog 进入 confirmed

- **WHEN** 上级平台发送 ACK 确认 200 OK
- **THEN** Dialog 状态从 `proceeding` 推进到 `confirmed`
- **AND** 入站媒体管道持续运行直到 BYE

#### 场景：BYE 清理媒体资源

- **WHEN** 任一方发送 BYE 结束点播
- **THEN** platform-small 停止 RTP 监听、关闭 PS/RTP 解封装器
- **AND** 应答 `200 OK`，Dialog 进入 `terminated`
- **AND** 监听端口被释放

#### 场景：无效 SDP 被拒绝

- **WHEN** INVITE 的 SDP 体不可读或缺少 `m=` 行
- **THEN** platform-small 应答 `400 Bad Request`，不建立 Dialog、不分配端口

#### 场景：Dialog 超时自动清理

- **WHEN** INVITE 后 30 秒内未收到 ACK
- **THEN** Dialog 进入 `terminated`，已分配的端口与管道被释放

### 需求：小平台处理目录推送 SUBSCRIBE

`platform-small` 节点必须（MUST）接受来自上级的 `catalog` 事件 `SUBSCRIBE`，以其 `Expires` 记录订阅关系并应答 `200 OK`。当下游设备注册到或从小平台被清扫时，节点必须（MUST）向每个活跃订阅者发送携带最新设备列表的 `NOTIFY`。事件类型不是 `catalog` 的 `SUBSCRIBE` 必须（SHALL）以 `489 Bad Event` 应答。

platform-small 收到上级 SUBSCRIBE 目录订阅时，必须记录订阅关系，并在设备列表变化时
通过 NOTIFY 推送更新。

#### 场景：订阅目录成功

- **WHEN** 上级平台发送 SUBSCRIBE，`Event: catalog`，`Expires: 3600`
- **THEN** platform-small 记录订阅关系，应答 `200 OK`
- **AND** 在订阅有效期内，设备列表变化时发送 NOTIFY

#### 场景：设备上线触发 NOTIFY

- **WHEN** 一个下级设备注册成功
- **AND** 存在活跃的目录订阅
- **THEN** platform-small 向每个订阅者发送 NOTIFY，携带最新设备列表

#### 场景：订阅过期

- **WHEN** 订阅的 Expires 到期
- **THEN** 订阅关系被移除，不再发送 NOTIFY

#### 场景：非 catalog 事件被拒

- **WHEN** SUBSCRIBE 的 Event 头不是 `catalog`
- **THEN** platform-small 应答 `489 Bad Event`

### 需求：小平台向上级发送 OPTIONS 保活

配置了 `registration.options.enabled: true` 的 `platform-small` 节点必须（MUST）周期性向上级平台发送 `OPTIONS` 请求以探测链路活性。收到 `200 OK` 必须（MUST）清零失败计数；超时或 `408` 必须（MUST）递增。计数达到 `heartbeat_max_failures` 时节点必须（MUST）进入 `fault`。OPTIONS 间隔必须（MUST）可配置，默认 60 秒。

platform-small 作为下级可定期向上游发送 OPTIONS 探测链路活性，连续超时触发节点 fault。

#### 场景：OPTIONS 探测成功

- **WHEN** platform-small 向上级发送 OPTIONS
- **AND** 收到 `200 OK`
- **THEN** 失败计数清零，链路状态标记为正常

#### 场景：连续超时触发 fault

- **WHEN** OPTIONS 连续 `heartbeat_max_failures` 次未收到响应或收到 `408`
- **THEN** 节点进入 `fault`，端口释放

#### 场景：非配置时不发送 OPTIONS

- **WHEN** `registration.options.enabled` 未声明或为 `false`
- **THEN** platform-small 不发送 OPTIONS，保活仅依赖 MESSAGE keepalive

### 需求：小平台解析并转发 MediaStatus

`platform-small` 节点必须（MUST）解析来自下游设备的 MANSCDP `MediaStatus` notify，在其在线表中更新设备的媒体状态，且在节点自身已注册到上级时——通过携带 MediaStatus notify 体的 MESSAGE 将状态转发给上级。不可读的 MediaStatus 体必须（MUST）被忽略而不应答错误。

platform-small 解析下级 MediaStatus 上报，更新设备媒体状态，并在自身有上级时转发。

#### 场景：解析并记录媒体状态

- **WHEN** 下级设备发送 MESSAGE，`CmdType = MediaStatus`
- **THEN** platform-small 解析视频参数（分辨率、码率、帧率），更新在线表中的媒体状态
- **AND** 应答 `200 OK`

#### 场景：向上级转发媒体状态

- **WHEN** platform-small 自身已注册到上级，且收到下级 MediaStatus
- **THEN** platform-small 构造 MediaStatus NOTIFY，通过 MESSAGE 发送给上级

#### 场景：不可读的 MediaStatus 被忽略

- **WHEN** MediaStatus 消息体不可解析
- **THEN** platform-small 不应答错误，打 debug 日志后继续 serving

### 需求：小平台按事务分拣消息

`platform-small` 节点必须（MUST）按事务（Call-ID）而非仅按方向路由 SIP 消息。请求必须（MUST）交付给受理半；响应必须（MUST）按 Call-ID 与已注册的事务处理器匹配，并交付给发起该事务的那一半。未匹配的响应必须（MUST）被静默丢弃。

platform-small 的消息分拣按事务（Call-ID）匹配，而非仅按方向分拣。

#### 场景：INVITE 响应路由到正确的事务处理器

- **WHEN** 上级平台应答 INVITE 的 200 OK 到达
- **THEN** 分拣器按 Call-ID 找到 INVITE 注册的事务处理器，将响应交给它
- **AND** 不会被 serving 半的 REGISTER/MESSAGE 循环取走

#### 场景：未匹配的响应被丢弃

- **WHEN** 一个响应的 Call-ID 在事务表中找不到注册者
- **THEN** 该响应被静默丢弃，不触发任何处理
