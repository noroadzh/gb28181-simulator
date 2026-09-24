# Proposal

对应路线图阶段编号：**#7 `platform-small-node`（共 15 阶段）的第一部分**。本 change
交付该阶段的"身份与级联链路"，该阶段其余部分（SUBSCRIBE 目录订阅、INVITE 点播、
OPTIONS 保活、MediaStatus 上报）留给后续 change。

## Non-goals

- 主动能力：SUBSCRIBE 目录订阅、INVITE 点播、OPTIONS 保活、MediaStatus 上报——
  #7 的后续 change。本 change 只把身份与级联链路打通。
- 目录在平台之间的同步（把学到的下级通道报给上级）。本 change 的目录应答只回答
  "注册到我的那些设备"，跨级目录汇聚需要订阅与通道表。
- 新的 HTTP 端点、新的配置键。
- 级联转发头（`X-RoutePath` / `X-PreferredPath`）——路线图 #9。
- 按 SIP 事务（Call-ID / branch）分捡消息。本 change 只按方向分捡（请求给受理半、
  响应给注册半），因为小平台目前在上游侧只收响应。

## Why

`platform-small`（类型码 216）今天只是一个枚举值：节点能创建、能起停，但 `Start` 对它
既不注册也不受理，配置里的 `platform:` 段也被文档标注为"只对 platform-large 有意义"。
于是国标里最常见的一种拓扑拼不出来——多级级联 `A → B → C` 中的 B：它在上级眼里是
一个注册上来的下级（UAC），在下级眼里是一个受理注册、应答目录查询的上级（UAS）。缺了
这个身份，级联链中间那一环只能靠"两台 platform-large 首尾相接地假装"，而假装出来的
节点不会注册、不会被清扫、也不会出现在上级的在线表里。

这一 change 给 `platform-small` 装上双向身份：向下受理、向上注册，两段配置可单独或同时
存在。它是后续 #7 主动能力（SUBSCRIBE 目录订阅、INVITE 点播、MediaStatus）的前提——
一个学不到下级的平台没有可上报的通道。

## What Changes

- **`platform-small` 启动时同时扮演两半。** 先以 UAS 身份在自己的 transport 上受理下级
  注册（复用 `Acceptor`），再以 UAC 身份注册到配置的上级平台（复用 `Registrar`），并在
  注册成功后交给 `Keeper` 保活与续期。
- **两段配置独立可选。** 只声明 `registration:` 是"纯下级平台"；只声明（或省略）
  `platform:` 是"只受理的小平台"；两者都声明就是级联中继。省略 `platform:` 时取域默认
  值，与 platform-large 一致。
- **生命周期推进幂等。** `serve` 与 `register` 各自都会把节点推到 `online`；第二次推进
  不得因状态机无 `online → online` 边而失败。任一半失败都 faults 整个节点并回滚另一半。
- **停止是两半一起停。** 停受理（先停 goroutine 再释放端口，清在线表）、停保活、向上
  注销。
- **配置校验放开。** `platform:` 段对 `platform-small` 同样有效；`registration:` 对
  platform-small 同样有效（此前只有 device 会走注册分支）。
- **可观测性不变。** 在线设备端点 `GET /v1/nodes/{id}/devices` 对 platform-small 可用，
  返回的是注册到它的下级；日志沿用既有字段，不含凭据。

## Capabilities

### New Capabilities

- `platform-small-node`: platform-small 身份的双向级联——作为 UAS 受理下级注册、作为 UAC
  注册到上级并保活，两段可独立配置，生命周期与停止语义。

### Modified Capabilities

（无。`node-abstraction` 已声明三种身份的协议行为由后续 change 填充，其需求不变；
`device-node` 与 `platform-large-node` 的既有需求也不受影响。）

## Impact

- `internal/app/node_service.go`：`Start` / `Stop` 的按 kind 分支，状态推进的幂等处理。
- `internal/platform/config/config.go`：`platform:` 段的注释与校验说明（无新增键）。
- `internal/app/node_service_*_test.go`、`internal/adapter/siptest/`（新增三级级联 e2e）。
- `README.md`、`docs/architecture.md`：补 platform-small 身份与级联拓扑。
