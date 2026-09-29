# Spec Delta

## Purpose

platform-small 在 #7 第一部分已具备双向身份与级联链路。本 delta 新增 4 项主动能力：
SUBSCRIBE 目录订阅、INVITE 点播、OPTIONS 保活、MediaStatus 上报，使其从"身份打通"
升级为"完整媒体与控制中继"。

## ADDED Requirements

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
