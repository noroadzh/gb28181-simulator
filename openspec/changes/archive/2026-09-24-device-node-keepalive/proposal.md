# Change: device 保活与注销

## Why

Change 5 的第一部分（`device-node-registration`）让 device 节点能完成 REGISTER 事务并进入 `online`。
但真实设备一旦上线就必须持续证明自己还活着：GB/T 28181 用 **MESSAGE 心跳**（MANSCDP `Keepalive`
通知）维持在线，用 **过期前重注册** 维持注册，用 **Expires: 0 的 REGISTER** 优雅下线。缺失这三者时，
节点注册成功后即"静止"——平台会在心跳超时后把它踢掉，注册过期后同样失效，而停止节点只能本地释放端口、
无法通知平台。

本 change 补齐 device 身份的"在线维持"与"优雅下线"，把 `#5 device-node` 收尾。

## What Changes

- **新增 MANSCDP `Keepalive` 通知的编解码**（`internal/adapter/manscdp`），经新的 domain 端口注入 app 层
  —— 项目此前没有任何 MANSCDP XML 能力，心跳体无法构造。
- **新增可注入的定时器端口**（`port.Ticker` + `platform/clock` 的真实与 fake 实现）—— 项目此前没有
  周期任务/goroutine 抽象，心跳与重注册无法确定性测试。
- **新增 app 层保活调度器**（`Keeper`）：节点注册成功后启动一个 goroutine，按周期发 MESSAGE 心跳、
  在注册过期前半程触发重注册（失败按指数退避重试），并在节点停止/注销/故障时先停 goroutine 再释放端口。
- **新增注销事务**：`Registrar.Unregister` 发 `Expires: 0` 的 REGISTER（同样应答 401 挑战），
  收到 2xx 后推进 `online → offline`；新增 `POST /v1/nodes/{id}/unregister` 动作。
- **心跳失败可配置阈值**：连续 N 次（默认 3）未收到 200 OK 才回落 `fault` 并释放端口，
  单次失败仅告警，避免网络抖动误杀在线节点。
- **配置扩展**：`nodes[].registration` 增加 `heartbeat_interval`（默认 60s）、`heartbeat_timeout`
  （默认 5s）、`heartbeat_max_failures`（默认 3），全部可选且校验。
- **测试基建扩展**：`siptest` 测试 UAS 支持 MESSAGE（默认回 200 OK，可配置为不回应以测失败路径）。

## Non-goals

- **不做** platform-large 侧的心跳超时踢线与在线设备表（路线图 #6）。
- **不做** 目录上报、报警订阅、点播/回放等其余 device 行为（后续 change）。
- **不做** 完整 MANSCDP+ 命令集——本 change 只实现 `Keepalive` 一个通知类型，编解码层留好扩展位。
- **不做** 多平台同时注册（一设备多主，#9 级联）——心跳只发给注册所用的那个平台。
- **不做** 心跳/重注册的持久化与进程重启恢复。

## Impact

- **Affected specs**: `device-node`（新增保活、重注册、注销、后台任务生命周期、配置需求）；
  `node-abstraction`（HTTP API 需求增加 `/unregister` 动作）。
- **Affected code**: `internal/domain/model`（Registration 增加心跳参数）、`internal/domain/port`
  （Keepalive 编解码端口、Ticker 端口）、`internal/adapter/manscdp`（新包）、`internal/platform/clock`
  （ticker 实现）、`internal/app`（Keeper 调度器、Registrar.Unregister、NodeService 接线）、
  `internal/interface/http`（新路由）、`internal/platform/config`、`cmd/gb28181-simulator`（组合根）。
- **依赖**：不新增第三方依赖（XML 用标准库 `encoding/xml`）。
- **兼容性**：心跳参数全部可选，缺省取国标常用值；未配置注册的节点行为完全不变。
