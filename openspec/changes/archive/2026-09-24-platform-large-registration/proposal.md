# Proposal: platform-large 注册受理与在线设备表

## Why

路线图 #5 之后，模拟器能扮演 device：向平台注册、发心跳、到期重注册、优雅注销。但那个"平台"
一直是测试用的 `siptest.UAS`，进程内没有任何身份能真正**受理**下级的注册。`kind: platform-large`
的节点虽然已在配置示例里出现（`34020000002000000001`），启动后却只是绑定一个 listener 就停在
`registering`：它既不回 401，也不认识任何下级。

于是今天没法在一台机器、一个进程里验证"我的 device 实现真的符合平台预期"——只能对假 UAS 自测。
本 change 让 platform-large 节点成为真正的 UAS：受理下级的 REGISTER、按 GB/T 28181 的 Digest
流程挑战并校验、协商有效期、把通过的下级记进在线设备表，并把这张表经 HTTP 暴露出来。

拆分的理由：注册受理是平台侧一切能力（心跳踢线、目录查询、级联转发）的地基，但它本身已是一个
完整的协议往返 + 状态记账；心跳接收与踢线、MANSCDP 目录查询各自还能独立成 change，因此本 change
只做地基，其余留作 #6 的第二部分。

## What Changes

- **新增** `platform-large-node` capability：platform-large 节点作为 UAS 受理下级 REGISTER。
  - 收 REGISTER → 无/坏 `Authorization` 时回 **401** 并带 `WWW-Authenticate` Digest 挑战；
  - 收到带 `Authorization` 的重发 → 用配置的 `accounts` 校验 → 通过回 **200 OK**（`Expires`、
    `Contact`、`Date`），username 未知或 response 不符回 **403**（GB/T 28181 §L.2：不重复挑战）；
  - `Expires` 按平台的 min/default/max 三档钳制；`Expires: 0` 视为注销，回 200 OK 并出表。
- **新增** 在线设备表：按节点分区记录下级的 deviceID、来源地址、Contact、传输、注册时刻、
  授予有效期与最后活跃时刻；随节点停止清空，节点间互不串扰。
- **新增** HTTP 查询：`GET /v1/nodes/{id}/devices`、`GET /v1/nodes/{id}/devices/{deviceID}`。
- **扩展** 节点配置：`nodes[]` 条目新增可选 `platform:` 段（realm、accounts、min/default/max
  expires），零值取默认，非法值按既有范式报错。
- **MODIFIED** `node-abstraction`：配置段与 HTTP 端点两处需求随之扩展。

## Capabilities

### New Capabilities

- `platform-large-node`：大平台身份的注册受理、鉴权、有效期协商与在线设备表（及其声明式配置）。

### Modified Capabilities

- `node-abstraction`：`nodes[]` 新增可选 `platform:` 段；HTTP 新增设备查询端点；platform-large
  节点 Start 后推进到 `online`（"平台在服务"）。

## Impact

- 纯增量：不配置 `platform:` 段的节点行为完全不变；device 侧（#5）一行不改。
- 新增依赖仅标准库与已有适配器（服务端 Digest 的 `Responder` / `Challenger` 早已落地），
  不引入新第三方库、不引入 CGO。
- 每个 platform-large 节点多一个常驻 `Receive` goroutine，随节点 Stop / 进程退出收口。
- 组合根新增 `credstore` / `devicereg` / `Acceptor` 三个装配点，`Acceptor` 以 `io.Closer`
  注册关闭，保证无 goroutine 残留。
