# 日志配置（log）

> 本文档是日志子系统的用户文档入口。覆盖 `log.*` 字段、`log.modules` 前缀匹配规则、`PATCH /v1/config/log` 接口契约、`component/subsystem` 标签对照、`ErrorAggregator` 摘要语义，以及典型排障步骤。
>
> 代码基准：`internal/platform/observability/logging`、`internal/platform/config/config.go`、`internal/interface/http/log_config.go`（commit `4305604` / `91544ef` / `d30835c` 之后）。后续代码变更涉及日志面时，请同步更新本文件与 `openspec/specs/logging-coverage/spec.md`。

## 目录

1. [配置参考](#1-配置参考)
2. [模块级级别（log.modules）](#2-模块级级别logmodules)
3. [运行时热更新（PATCH /v1/config/log）](#3-运行时热更新patch-v1configlog)
4. [组件与子系统标签对照](#4-组件与子系统标签对照)
5. [媒体热循环与错误摘要](#5-媒体热循环与错误摘要)
6. [排障示例](#6-排障示例)

---

## 1. 配置参考

`configs/config.example.yaml` 中的 `log` 段是启动期日志配置入口。运行时级别调整见 [§3](#3-运行时热更新patch-v1configlog)。

| 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `level` | string | `info` | 全局默认级别。可选值 `trace`/`debug`/`info`/`warn`/`error`，大小写不敏感；未知值回退到 `info`。 |
| `file` | string | `$XDG_STATE_HOME/gb28181-simulator/logs/app.log` | 日志文件路径；空串走 OS 默认 XDG 目录。 |
| `add_source` | bool | `false` | 是否在每条记录中附带 `source.file:line`（`runtime.Caller` 信息）。生产环境建议关闭以避免路径泄露。 |
| `redact_keys` | []string | `["password", "secret", "private_key", "authorization"]` | 命中键名的字符串字段值会被替换为 `"***REDACTED***"`，再写入文件与 WebSocket 流。 |
| `modules` | map[string]string | `{}` | 可选，按 `component`/`subsystem` 划分的模块级级别覆盖。详见 [§2](#2-模块级级别logmodules)。 |

> **环境变量**：当前实现未绑定 `GB28181_LOG_*` 环境变量；要覆盖日志配置请改 `configs/config.example.yaml` 或通过 PATCH 接口（[§3](#3-运行时热更新patch-v1configlog)）。后续若引入 env 绑定会在本文档同步更新。

---

## 2. 模块级级别（log.modules）

### 语法

```yaml
log:
  level: info
  modules:
    "internal/app": warn           # 整个 internal/app 默认收 warn
    "internal/app/sip_acceptor": debug  # 上面被覆盖后这里反而更高（更详细）
```

`modules` 是 `map[string]Level`。**key 是 `component` 或 `subsystem` 属性值**，与底层 Go 包的 `slog.Logger.With("component", "...", "subsystem", "...")` 注入路径一致（见 [§4](#4-组件与子系统标签对照)）。

### 匹配规则：最长前缀

`MultiHandler` 在每条记录上遍历所有 `component`/`subsystem` 属性，在 `moduleLevels` 中查找最长前缀匹配；命中则用其 level，未命中则用全局默认 `log.level`。前缀边界只接受 `key + "."` 或 `key + ":"`（如 `internal/app.`、`internal/app:`），确保 `internal/app` 不会误匹配 `internal/application`。

### 示例

```yaml
log:
  level: info
  modules:
    "internal/app": warn               # 收口所有 app 子模块到 warn
    "internal/app/sip_acceptor": debug # 但 SIP 接入层需要更详细，覆盖回 debug
    "internal/adapter/cascade": debug  # 单独调试级联
```

> **注意**：历史上 `configs/config.example.yaml` 注释使用 `internal/app/sip_acceptor` 这样的"拼接 key"作为示例。由于 `component` 实际为 `internal/app` 而 `subsystem` 实际为 `sip_acceptor`，`internal/app/sip_acceptor` 不会匹配任何记录。建议显式拆成两行：`"internal/app": warn` + `"sip_acceptor": debug`。

### 空 modules 条目

`modules: {"foo": ""}` 表示把 `foo` 重置回默认级别（该 key 仍存在但值为空字符串）；详见 [§3 PATCH 400 语义](#请求响应契约)。

---

## 3. 运行时热更新（PATCH /v1/config/log）

`PATCH /v1/config/log` 端点允许运维无需重启进程即可调整全局默认级别与按 `component/subsystem` 划分的模块级别。

### 请求/响应契约

**请求**（`application/json`）：

```json
{
  "level": "debug",                                  // 可选
  "modules": {                                       // 可选
    "internal/app": "debug",
    "internal/adapter/media": "warn"
  }
}
```

- `level` 与 `modules` 至少需有一个非空；两者都为空返回 400。
- `level` 取值仅限 `trace|debug|info|warn|error`，大小写不敏感；未知值返回 400。
- `modules` 字典中**任一 value 为未知级别**，整请求 400。
- `modules` 字典中**任一 key 对应空 value**，把该 key 视为"重置为默认级别"，仍生效。

**响应（200 OK）**：

```json
{
  "level": "debug",
  "modules": {
    "internal/app": "Debug",
    "internal/adapter/media": "Warn"
  }
}
```

回显**当前生效**的默认级别与 modules 字典（canonical 大小写），便于客户端核对。

**PATCH 是替换而非合并**：第二次 PATCH 只传 `modules: {"foo":"debug"}` 时，会清空所有未列出的旧 modules 项。若想保留旧项，必须在请求体里完整列出期望的 modules 字典。

### 400 语义

| 场景 | 状态码 | 响应体 |
|---|---|---|
| 请求体为空 `{}` | 400 | `{"error": "request must include `level` and/or `modules`"}` |
| `level` 为未知级别（如 `"verbose"`） | 400 | `{"error": "invalid level \"verbose\": must be one of trace|debug|info|warn|error"}` |
| 某个 module 的 level 为未知值 | 400 | `{"error": "invalid level for module \"internal/app\": ..."}` |
| JSON 解析失败 | 400 | `{"error": "invalid request body: ..."}` |
| 内部错误（如 logger 未初始化） | 500 | `{"error": "<UpdateLevels 错误>"}` |

### 内存生效 / 重启回退

PATCH 修改的是进程内 `MultiHandler.moduleLevels` 与 `defaultLevel` 字段，**不写盘**。重启后回退到 `configs/config.example.yaml` 中 `log.level` 与 `log.modules` 的声明值。

冒烟用例与并发安全性见 `internal/interface/http/log_config_test.go`（10 个用例）以及 `docs/smoke-test.md` §7。

---

## 4. 组件与子系统标签对照

每条记录注入的 `component` / `subsystem` 属性对来源于各模块构造函数中对 `slog.Logger.With("component", "...", "subsystem", "...")` 的调用。表格列出当前实现中的全部注入点（基准 commit `91544ef`）。

| 模块 | 路径 | `component` | `subsystem` |
|---|---|---|---|
| SIP 接入层 | `internal/app/acceptor.go` | `internal/app` | `sip_acceptor` |
| 心跳保活 | `internal/app/keeper.go` | `internal/app` | `keepalive_keeper` |
| 设备注册 | `internal/app/device_registrar.go` | `internal/app` | `device_registrar` |
| 节点服务 | `internal/app/node_service.go` | `internal/app` | `node_service` |
| 故障存储 | `internal/app/faults.go` | `internal/app` | `fault_store` |
| 级联处理器 | `internal/adapter/cascade/handler.go` | `internal/adapter/cascade` | `cascade_handler` |
| 媒体采集环形缓冲 | `internal/adapter/capture/ring.go` | `internal/adapter/capture` | `capture_store` |
| 媒体错误聚合 | `internal/adapter/media/error_aggregator.go` | `internal/adapter/media` | — |

> `ps_packetizer.go` / `rtpizer.go` 不自行注入日志 tag，而是把错误交给 `ErrorAggregator`，因此其错误最终也落在 `component=internal/adapter/media` 之下（见 [§5](#5-媒体热循环与错误摘要)）。

**用法示例**：

```yaml
# 只看 SIP 接入层的全部细节（含 INVITE/ACK/BYE Debug），其他保持 warn
log:
  level: warn
  modules:
    "internal/app": debug
    "sip_acceptor": trace
```

或者 PATCH：

```bash
curl -X PATCH http://127.0.0.1:18080/v1/config/log \
  -H 'Content-Type: application/json' \
  -d '{"level":"warn","modules":{"internal/app":"debug","sip_acceptor":"trace"}}'
```

---

## 5. 媒体热循环与错误摘要

`internal/adapter/media` 组件的 `ErrorAggregator`（`error_aggregator.go`）对高频重复错误做 60 秒滑动窗口聚合，避免日志洪水。具体语义：

- **窗口**：`DefaultWindow = 60s`（可通过 `NewErrorAggregatorWithWindow` 自定义，但非正数会被 clamp 回 60s）。
- **累积策略**：同一 `signature` 在窗口内的所有错误仅递增计数器与刷新 `sample`（最近一条错误文本）+ `lastSeen`，**不立即输出日志**。
- **触发输出**：
  1. 窗口期满后下一条同类错误到达 → 先 `reportLocked` 输出当前窗口汇总（`Warn` 级：计数、`first_seen`、`last_seen`、`sample`），再开新窗口；
  2. 后台 ticker 调 `Sweep()` 主动清掉超时窗口 → 输出汇总（即便错误流已停止）；
  3. 进程退出时调 `Flush()` → 立即汇总所有活跃窗口，避免丢最后一次爆发。
- **签名**（调用方传入的字符串）：
  - `ps-mux`：PS（Program Stream）封装失败（如 PTS/DTS 越界、SPS/PPS 缺失）。
  - `rtp-send`：RTP 发送链路失败（如 socket 写入错误、远端断连）。
- **输出位置**：所有汇总记录通过 `aggregatorLog` 注入 `component: "internal/adapter/media"` 标签（无 `subsystem`），因此可通过 `modules` 单独控制其级别（见 [§2 示例](#示例)）。

下游消费侧可以通过 `component=internal/adapter/media` 与汇总记录字段（`signature` / `count` / `first_seen` / `last_seen` / `sample`）聚合错误趋势，无需拉取全量明细。

> 注意：聚合器本身**不会**记录原始错误。原始错误只在累计计数器时刷新到 `sample`，且每 60s 才汇总一次。定位期如需全量记录，请临时把 `internal/adapter/media` 提到 `trace`（见 [§6.3](#63-媒体热循环错误定位)），但聚合器依旧只输出汇总，需要从原始调用方（PS 封装器、RTP sender）找到全量 trace 的注入点。

---

## 6. 排障示例

### 6.1 临时把 SIP 接入层调到 trace，定位 INVITE 解析异常

启动时（`configs/config.example.yaml`）保持生产默认值：

```yaml
log:
  level: warn
  modules: {}
```

定位期：

```bash
# 全局降到 debug，sip_acceptor 提到 trace；其他模块保持 warn
curl -X PATCH http://127.0.0.1:18080/v1/config/log \
  -H 'Content-Type: application/json' \
  -d '{"level":"debug","modules":{"sip_acceptor":"trace"}}'
```

观察 `/v1/logs/stream` WebSocket 流（`internal/interface/http/server.go`）：

```bash
websocat ws://127.0.0.1:18080/v1/logs/stream | jq 'select(.subsystem=="sip_acceptor")'
```

定位完成后恢复默认（PATCH 是替换式，必须重传完整 modules 字典，否则空 modules 会清空所有模块级别）：

```bash
# 恢复全局 warn + 无模块覆盖
curl -X PATCH http://127.0.0.1:18080/v1/config/log \
  -H 'Content-Type: application/json' \
  -d '{"level":"warn","modules":{}}'
```

### 6.2 等价静态配置（无需 PATCH）

若需要把级别固定到生产环境，可在 `configs/config.example.yaml` 中预先声明：

```yaml
log:
  level: warn
  modules:
    "internal/app": info          # 应用层降噪但保留必要的事件流
    "internal/adapter/cascade": warn
    "internal/adapter/capture": warn
    "internal/adapter/media": warn
```

启动后无需任何 PATCH 调用。

### 6.3 媒体热循环错误定位

若 `/v1/logs/stream` 中 `component=internal/adapter/media` 的记录频繁出现，先按 `signature` 聚合：

```bash
websocat ws://127.0.0.1:18080/v1/logs/stream \
  | jq -r 'select(.component=="internal/adapter/media") | .signature' \
  | sort | uniq -c | sort -rn
```

若 `ps-mux` 或 `rtp-send` 占主导，临时把 media 提到 trace，捕获原始错误（非汇总）：

```bash
curl -X PATCH http://127.0.0.1:18080/v1/config/log \
  -H 'Content-Type: application/json' \
  -d '{"modules":{"internal/adapter/media":"trace"}}'
```

随后回到 warn 关闭噪声：

```bash
curl -X PATCH http://127.0.0.1:18080/v1/config/log \
  -H 'Content-Type: application/json' \
  -d '{"level":"warn","modules":{"internal/adapter/media":"warn"}}'
```

---

## 附录 A：相关 spec 与测试入口

- 能力契约：`openspec/specs/logging-coverage/spec.md`（源自 `enhance-logging-coverage` 已归档 change）
- 实现：`internal/platform/observability/logging/{handler,level,logger}.go`
- HTTP 端点：`internal/interface/http/log_config.go`（+ `_test.go`）
- 冒烟用例：`docs/smoke-test.md` §7
- 路线图：§"日志配置用户文档（docs/logging.md）"（编号 #18）