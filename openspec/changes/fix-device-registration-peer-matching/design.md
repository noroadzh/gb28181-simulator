# Design

## Context

### 根因分析（基于部署机 10.96.1.125 抓包）

**抓包证据**（2026-10-10，Docker 部署，`gbsim-device` ↔ `gbsim-platform` 同 compose 网络）：

| 时间 (UTC) | 方向 | 消息 | Call-ID | 观察方 |
|---|---|---|---|---|
| 08:47:47.233 | t | REGISTER (CSeq 1) | `ce05ec93…` | device 抓包 |
| 08:52:13.235 | t | REGISTER (CSeq 1) | `b8e2cfe2…` | device 抓包 |
| 08:52:13.235 | r | REGISTER (CSeq 1) | `b8e2cfe2…` | platform 抓包 |
| 08:52:13.236 | t | 401 Unauthorized + WWW-Authenticate | `b8e2cfe2…` | platform 抓包 |
| 08:52:13.236 | r | 401 Unauthorized + WWW-Authenticate | `b8e2cfe2…` | device 抓包 |

platform 收到 REGISTER 后 1.2 ms 内返回 401 挑战；device 的 capture 也记录到该 401。但**注册事务最终超时失败**。之后没有出现 CSeq 2 的带 `Authorization` 的重发。

**代码路径**（`internal/app/device_registrar.go`）：

```go
// line 170-190
for {
    msg, peer, err := tr.Receive(ctx)
    if err != nil { ... StageTimeout ... }
    if h, ok := msg.Header("Call-ID"); !ok || h.Value() != callID {
        continue
    }
    // 186-190：问题所在
    if peer != reg.Server() {
        continue   // ← 静默丢弃
    }
    ...
}
```

**为什么永远不相等**：
- `reg.Server()`（`model.Registration.Server()`）返回配置文件中的字符串原文，本例为 `gbsim-platform:5060`（Docker 服务名）。
- `peer` 来自 `siptransport.Transport.Receive()`（`internal/adapter/siptransport/transport.go:142-159`），内部读 `msg.Source()`。gosip 在 UDP 读取路径（`transport/connection_pool.go handleMessage`）通过 `ReadFromUDP` 拿到远端 socket 地址，写入 `msg.SetSource()` —— 一定是 **IP 字面量**（`172.26.0.2:5060`），因为 UDP 报文本身只携带 IP，主机名信息早在 DNS 解析阶段就已丢失。
- `"172.26.0.2:5060" != "gbsim-platform:5060"` 恒为 true → **所有响应都被 continue 丢弃** → 超时。

**影响面**：该过滤模式在 `device_registrar.go` 中出现 1 次（本次修复点）。`device_heartbeat.go` 若采用同一模式同样受影响（实现时确认并同步修复）。

## Goals / Non-Goals

**Goals**
- 修复对端匹配，使得"配置中使用主机名 / Docker 服务名 / 域名"时事务能正常推进到 401 应答与 200 OK。
- 保持"配置中使用 IP 字面量"时的行为与之前完全一致。
- 提供单元级可验证的 `samePeer` 函数与测试覆盖（IP 对 IP、主机名对 IP、双主机名、端口不等）。

**Non-Goals**
- 不改 transport 返回的 peer 表示。
- 不加 DNS 缓存 / TTL 处理。
- 不重构 Registrar 状态机。

## Decisions

### Decision 1: 新增 `samePeer(peer, server string) bool` 纯函数，替换字符串相等判断

实现规则（按顺序短路）：
1. 用 `net.SplitHostPort` 分别拆出 `peerHost, peerPort` 与 `serverHost, serverPort`；任一拆分失败则回退到整体字符串比较（保守，保持旧语义）。
2. 端口字符串相等（端口就是十进制数字字符串，无等价变体）。
3. 双方都是 IP 字面量：`net.ParseIP` + `.Equal`（兼容 IPv4/IPv6 映射形式）。
4. host 字符串本身相等（同为主机名）。
5. 至少一方是主机名：对该方调用 `net.LookupHost` 解析出 IP 集合，与另一方的 IP 字面量或解析结果求交集；解析失败则返回 false。

```go
func samePeer(peer, server string) bool {
    peerHost, peerPort, errP := net.SplitHostPort(peer)
    serverHost, serverPort, errS := net.SplitHostPort(server)
    if errP != nil || errS != nil {
        return peer == server // 回退：无法按 host:port 语义比较时保持旧行为
    }
    if peerPort != serverPort {
        return false
    }
    peerIP := net.ParseIP(peerHost)
    serverIP := net.ParseIP(serverHost)
    switch {
    case peerIP != nil && serverIP != nil:
        return peerIP.Equal(serverIP)
    case peerHost == serverHost:
        return true
    case peerIP != nil: // server 是主机名
        ips, err := net.LookupHost(serverHost)
        return err == nil && containsIP(ips, peerIP)
    case serverIP != nil: // peer 是主机名（罕见，但对称处理）
        ips, err := net.LookupHost(peerHost)
        return err == nil && containsIP(ips, serverIP)
    default: // 双方都是主机名
        serverIPs, errS := net.LookupHost(serverHost)
        if errS != nil {
            return false
        }
        peerIPs, errP := net.LookupHost(peerHost)
        if errP != nil {
            return false
        }
        return intersectIPs(peerIPs, serverIPs)
    }
}
```

**为什么选纯函数**：便于单测（无需起 socket）；`device_registrar.go` 与可能的 `device_heartbeat.go` 共用；不改 `Registration` 模型。

**为什么不做 DNS 缓存**：注册 / 重注册 / 注销 / 心跳响应判定每事务最多触发一次 LookupHost。注册频率低（周期 ≥ 60s，续订在有效期半程），容器内 DNS 查询通常 < 1 ms（本地 dnsmasq / 内嵌 resolver），不构成瓶颈。引入缓存会带来 TTL 与失效语义复杂度，收益不成比例。

**替代方案（否决）**：
- *去掉 peer 过滤只看 Call-ID*：会失去"响应必须来自配置的平台"这层防护（例如恶意第三方伪造相同 Call-ID 的响应）。Call-ID 只是 128 位随机数，不是认证凭据。
- *在 `Registration` 上预解析 server IP 存起来*：需要改 domain 模型并处理解析时机（进程启动 vs 每事务），且 Docker 场景下服务 IP 可能因重启变化；每次事务解析最稳。
- *比较 `Via` 的 `received` 参数*：依赖平台是否填该参数（gosip 会填，但不保证所有平台都填），不可靠。

### Decision 2: 心跳过滤逻辑同步对齐

`device_heartbeat.go` 若存在同样的字符串比较（实现时以 `grep -n "peer != \|Server()" internal/app/device_heartbeat.go` 确认），同步改用 `samePeer`。保持"注册与心跳对端匹配语义一致"，避免修了注册又让心跳在主机名配置下超时回落 fault。

### Decision 3: 测试策略

- `samePeer` 单测（表驱动）：覆盖 (IP,IP) 相等/不等、(IP,主机名) / (主机名,IP) 用 `localhost` / `127.0.0.1`、(主机名,主机名) 同名同 IP、端口不等直接 false、缺端口回退整体比较。
- Registrar 集成路径：用现有测试基建（fake transport）让 `Receive` 返回 peer = `127.0.0.1:<port>`，而 `reg.Server()` = `localhost:<port>`，验证 401 挑战仍能被应答、事务能走到 200 OK。
- 回归：`go test ./...` + `go vet ./...` 全绿。

## Risks / Trade-offs

- [Risk] `LookupHost` 在 DNS 不可用时阻塞（默认超时通常数秒）→ Mitigation：只影响"配置为主机名且 DNS 异常"的组合；此时注册本身也发不出去（`Send` 需要解析 server），行为一致退化。
- [Risk] Docker 服务名解析结果随容器重建变化 → Mitigation：每次事务重新解析，无缓存，天然跟随 DNS 当前值。
- [Risk] IPv6 zone（如 `fe80::1%eth0`）可能使 `SplitHostPort` / `ParseIP` 语义分歧 → Mitigation：当前部署均为 IPv4（抓包证实），`samePeer` 单测覆盖 IPv4；IPv6 场景留待实际需要时扩展。
- [Trade-off] 每次 401 应答触发一次 DNS 查询，微秒至毫秒级开销 → 可忽略（注册 / 心跳每 60s 级别）。

## Migration Plan

无数据迁移。部署步骤：
1. 重新构建 linux/amd64 二进制（或 Docker 镜像）。
2. 更新 10.96.1.125 上的容器（`docker compose build && up -d`，或直接替换二进制后重启）。
3. 冒烟验证：`POST /v1/nodes/34020000001310000001/start` → `GET /v1/nodes/34020000001310000001` 应显示 `status=online`（而非 fault）。
4. 平台侧确认 `gbsim-platform` 的 capture 出现 `Authorization` 头的 REGISTER 与最终 200 OK。
