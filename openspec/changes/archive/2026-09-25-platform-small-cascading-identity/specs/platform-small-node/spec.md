# Spec Delta

## Purpose

platform-small 是级联链中间那一环：对上是注册上去的下级平台，对下是受理注册的上级平台。
本能力规定它如何同时扮演这两半、两段如何独立配置、以及生命周期如何作为"一个节点"推进与
回卷。

## ADDED Requirements

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
