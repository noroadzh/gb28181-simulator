# Proposal

## Why

设备节点启动注册时直接失败，Web 界面报错：

```
app: register node 34020000001310000001: app: registration failed at timeout:
app: timed out waiting for a registration response: context deadline exceeded
```

抓包证据（部署机 10.96.1.125，`gbsim-device` 与 `gbsim-platform` 两个容器互通）显示：
- platform 在 1 ms 内对 REGISTER 返回了带 `WWW-Authenticate` 的 401 挑战；
- device 的抓包（`/v1/nodes/:id/capture`，direction=`r`）中也收到了该 401；
- 但注册事务仍然超时失败。

根因在 `internal/app/device_registrar.go` 第 186–190 行的对端（peer）过滤逻辑：

```go
if peer != reg.Server() {
    ... // continue 丢弃
}
```

`reg.Server()` 是配置文件中的字符串（如 `gbsim-platform:5060`，Docker 服务名），而 `peer` 是 transport 层 `Receive()` 返回的真实 UDP 源地址（`msg.Source()`，由 gosip 的 `ReadFromUDP` 填充，一定是 IP 形式，如 `172.26.0.2:5060`）。两者字符串形式永远不相等 → 每一条响应（包括 401 挑战）都被静默丢弃 → 等到 5 秒超时报错。

配置里 server 字段一旦使用主机名 / Docker 服务名 / 域名（而非 IP 字面量），此 bug 必现。这同样影响心跳（MESSAGE 保活）与注销（UNREGISTER / Expires:0）等所有走同一过滤路径的事务——它们的响应也会被丢弃。

## What Changes

- **修复** `internal/app/device_registrar.go` 中响应过滤的对端匹配逻辑：从"字符串严格相等"改为"地址语义等价"——按 `host:port` 拆分，端口必须相等；host 部分支持 IP 字面量语义比较（`net.ParseIP` + `Equal`）与主机名 / IP 混合时的 DNS 解析后比较。
- **新增** `samePeer` 纯函数，放在 `internal/app/device_registrar.go` 内（包内私有），并配套单元测试。
- **同步修改** `internal/app/device_heartbeat.go`（若同样存在字符串比较过滤逻辑）保持行为一致。
- **Spec 变更**：`device-node` capability 中「需求：每个节点经自身 transport 注册且互不串扰」改为明确说明对端匹配规则（端口相等 + host 语义等价），并新增"配置使用主机名时响应可被正确关联"的场景。

## Capabilities

### Modified Capabilities

- `device-node`：明确注册 / 保活 / 注销事务的对端匹配语义——不再要求配置地址与实际来源地址字符串相等，要求按地址语义（端口 + 主机）等价判断。

## Impact

- 受影响代码：`internal/app/device_registrar.go`（核心修复）；`internal/app/device_heartbeat.go`（如同样存在此逻辑）
- 新增测试：`internal/app/device_registrar_peer_test.go`（`samePeer` 单测 + Registrar 集成路径用例）
- 部署影响：Docker 容器内使用服务名的部署方式（如 `docker-compose.yml` 中 `gbsim-platform:5060`）从"必现超时"变为可用
- 兼容性：配置地址为 IP 字面量时行为完全不变；仅修复了主机名场景

## Non-goals

- 不引入 DNS 缓存（LookupHost 每事务调用一次，注册 / 心跳 / 注销频率低，开销可忽略）
- 不修改 transport 层 `Receive()` 返回的 peer 格式（保持 `host:port` 字符串不变）
- 不改变 `Registration.Server()` 的存储格式（保持配置原文，方便日志排障）
- 不在此 change 中处理"首次 REGISTER 未到达平台"问题（抓包显示第二次尝试已可达，首包丢失属于 UDP 语义 / 启动时序，另行评估）
