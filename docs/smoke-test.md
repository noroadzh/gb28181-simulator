# GB28181 Simulator — 冒烟测试文档

> 适用版本：`fix-problems-and-smoke-deploy-docs` 引入的冒烟基线（阶段 1 项目骨架 + 阶段 14 Web UI 后的第一个工程治理窗口）。
> 文档状态：与 `scripts/smoke.sh`、`internal/test/e2e/smoke_test.go`、`docs/smoke-results.json` 同步维护。

---

## 1. 概述与边界

冒烟测试（smoke test）回答一个最小但关键的问题：**"刚构建好的二进制能否在端到端意义上对外提供有效服务？"** 它与单元测试的边界如下：

| 维度 | 单元测试 (`go test`) | 冒烟测试 (`make smoke`) |
|---|---|---|
| 范围 | 包内函数 / 字节级断言（SIP/SDP/PS） | 跨包、跨语言（Go + Node）的端到端可用性 |
| 速度 | 数十秒（带 `-race`） | ≤ 8 分钟（含 5 平台交叉编译） |
| 失败信号 | 哪一行断言失败 | 哪个子系统（后端/前端/平台）不可达 |
| 执行人 | CI 矩阵 + 开发者本地 `make test` | CI `smoke` job + 上线前运维复测 |

冒烟**不**做的工作：
- 不验证 SIP 消息字节序列（属于 `internal/adapter/sip` 的 golden test）
- 不验证 PS 封装帧结构（属于 `internal/adapter/media` 的 golden test）
- 不验证 MANSCDP+ XML 解析（属于 `internal/adapter/manscdp` 的 golden test）

冒烟**做**的工作：
- 后端代码整体通过编译
- 全部单元测试（带 `-race`）通过
- 跨 5 个平台（Linux amd64/arm64、macOS amd64/arm64、Windows amd64）成功产出二进制
- 前端 `npm ci + npm run build` 成功且产物落 `web/dist/`
- 二进制（或同等 HTTP server）能响应 `/healthz`、`/v1/version`、`/v1/nodes/<id>/faults`、`/v1/logs/stream`、`PATCH /v1/config/log` 五个端点

---

## 2. 测试矩阵（19 个 capability ↔ 冒烟探针）

下表把仓库现有的 19 个 capability 与冒烟探针对应起来，便于"哪个能力挂了直接定位"。

| # | Capability | 冒烟探针 | 关联步骤 |
|---|---|---|---|
| 1 | `capture-and-pcap` | `go test ./internal/adapter/capture/...` 通过 | step 1: `go-test` |
| 2 | `capture-panel` | `go test ./internal/interface/http/...` 通过 | step 1: `go-test` |
| 3 | `cascade-routing` | `go test ./internal/app/...` 通过 | step 1: `go-test` |
| 4 | `core-manscdp-and-ps` | `go test ./internal/adapter/manscdp/... ./internal/adapter/media/...` 通过 | step 1: `go-test` |
| 5 | `core-sip-stack` | `go test ./internal/adapter/sip/... ./internal/adapter/sdp/... ./internal/adapter/auth/...` 通过 | step 1: `go-test` |
| 6 | `device-node` | `go test ./internal/app/...` 通过 | step 1: `go-test` |
| 7 | `dynamic-catalog-alarm-and-playback` | `go test ./internal/app/...` 通过 | step 1: `go-test` |
| 8 | `enterprise-skeleton` | `go vet ./...` 零输出 | step 2: `go-vet` |
| 9 | `exception-injection` | `TestSmokeFaultsEndpoint` 通过 | step 6: `e2e-smoke` |
| 10 | `fault-panel` | 同上 | step 6: `e2e-smoke` |
| 11 | `gb28181-2022` | `go build` 产物含 X-GB-Ver 协商代码 | step 3/4: `go-build` / `cross-compile` |
| 12 | `gb35114-security` | `go test ./internal/adapter/security/...` 通过 | step 1: `go-test` |
| 13 | `media-sources` | `go test ./internal/adapter/media/...` 通过 | step 1: `go-test` |
| 14 | `node-abstraction` | `go test ./internal/domain/model/...` 通过 | step 1: `go-test` |
| 15 | `platform-large-node` | `go test ./internal/app/...` 通过 | step 1: `go-test` |
| 16 | `platform-small-node` | 同上 | step 1: `go-test` |
| 17 | `project-skeleton` | `TestSmokeHealthz` + `TestSmokeVersion` + `TestSmokeWebSocketLogs` 通过 | step 6: `e2e-smoke` |
| 18 | `scenario-engine` | `go test ./internal/adapter/scenario/... ./internal/app/...` 通过 | step 1: `go-test` |
| 19 | `scenario-manager` | 同上 | step 1: `go-test` |

> 注意：`capture-panel`、`fault-panel` 这类 capability 的"前端"页面（Vue 组件）由 step 5 (`web-build`) 验证：一旦 `npm run build` 成功，`web/dist/` 的内容通过 `embed.FS` 在 step 3 嵌入二进制——任何前端编译错误都会让 step 5 失败，从而连带让 step 3/4 失败。

---

## 3. 执行步骤

### 3.1 本地执行

```bash
# 一键冒烟（开发者常用入口；与 scripts/smoke.sh 等价）
make smoke

# 直接调用脚本（CI runner 用法，避免 Make 环境差异）
bash scripts/smoke.sh
```

### 3.2 在 CI 中触发

push 到 `main` 或 PR 触发 `.github/workflows/ci.yml`：
- 现有 `lint`、`test`、`build` 三个 job 先行
- 新增 `smoke` job（依赖 `lint`、`test`）随后执行 `bash scripts/smoke.sh`
- 产物 `docs/smoke-results.json` 与 `build/` 通过 `actions/upload-artifact@v4` 上传

CI run 详情页可在 "Artifacts" 区下载：
- `smoke-report` → `docs/smoke-results.json`（结构化报告）
- `smoke-binaries` → `build/` 下五个平台的二进制

### 3.3 在生产前手动复测

运维在上线前可使用任意一台 Linux amd64 主机跑一次 `make smoke`，把 `docs/smoke-results.json` 附到变更单或工单中。

---

## 4. 结果解读

### 4.1 JSON 结构

```json
{
  "version": "1.0",
  "timestamp": "2026-09-28T11:42:00Z",
  "overall": "passed",
  "total_duration_ms": 234567,
  "steps": [
    {
      "name": "go-test",
      "status": "passed",
      "duration_ms": 42310,
      "artifacts": [],
      "error": null
    },
    {
      "name": "go-vet",
      "status": "passed",
      "duration_ms": 2150,
      "artifacts": [],
      "error": null
    },
    ...
  ]
}
```

### 4.2 字段语义

| 字段 | 类型 | 含义 |
|---|---|---|
| `version` | string | 报告 schema 版本，当前固定 `"1.0"` |
| `timestamp` | string (RFC3339) | 冒烟启动 UTC 时间 |
| `overall` | enum | `"passed"` 或 `"failed"`；前者要求所有 step 都 `passed` |
| `total_duration_ms` | int | 整个流程总耗时（含跨平台编译） |
| `steps[].name` | string | 步骤标识；六个固定值：`go-test`、`go-vet`、`go-build`、`cross-compile`、`web-build`、`e2e-smoke` |
| `steps[].status` | enum | `"passed"` / `"failed"` |
| `steps[].duration_ms` | int | 该步骤耗时 |
| `steps[].artifacts` | array[string] | 该步骤产物路径；通常只有 `cross-compile` 步骤非空 |
| `steps[].error` | string \| null | 失败时的摘要；成功时为 `null` |

### 4.3 性能基线

| 步骤 | 单平台开发机（MacBook M1） | CI (ubuntu-latest) |
|---|---|---|
| go-test | 25–45s | 30–60s |
| go-vet | 1–3s | 1–3s |
| go-build | 5–10s | 5–10s |
| cross-compile | 30–90s | 60–180s |
| web-build | 10–20s | 20–40s |
| e2e-smoke | 1–2s | 1–2s |
| **合计** | **70–170s** | **120–300s** |

任一耗时超出基线 2 倍应纳入调查（首次 build 缓存为空除外）。

---

## 5. 失败排查

按 6 个步骤分桶给出排查清单：

### 5.1 go-test 失败

| 现象 | 优先排查 |
|---|---|
| 数据竞争（race detector 报 `WARNING: DATA RACE`） | 该包中共享变量没有 `sync.Mutex`/atomic；查看 `go test -race` 输出的堆栈定位 race |
| golden test 失败（`internal/portland/testdata/*.golden` 不匹配） | 是否误改协议字节序；检查该文件最近一次修改；用 `make sip-test` 缩小范围 |
| flaky test（CI 通过、本地失败） | 多半与 time.Now / 超时相关；本地加 `-count=10` 重现 |

### 5.2 go-vet 失败

| 现象 | 优先排查 |
|---|---|
| `printf` 格式串与参数类型不匹配 | `go vet -printfuncs` 输出定位函数 |
| `shadow` 告警（变量名遮蔽） | 重命名内层变量 |
| `nilness` 分析告警 | 多数情形是新代码未做 nil check；本次 fix 已修复 `s.nodes == nil` 一类问题 |

### 5.3 go-build / cross-compile 失败

| 现象 | 优先排查 |
|---|---|
| `undefined: symbol` | 模块 import 缺失；运行 `go mod tidy` 复核 |
| CGO 相关错误 | 项目约束是 **无 CGO**，确认未引入 cgo 依赖；若必要改成 `pure Go` 替代 |
| 跨平台编译 `darwin/arm64` 失败 | 检查是否使用了 amd64 特定的 assembly；本项目 `internal/adapter/media` 已知无此类问题 |

### 5.4 web-build 失败

| 现象 | 优先排查 |
|---|---|
| `npm ci` 网络超时 | 检查 Node 版本（要求 18+，CI 用 20）；重试或换镜像源 |
| `vue-tsc` 类型错误 | `web/src/` 下的 `.vue` 文件类型不匹配；本地 `cd web && npm run build` 复现 |
| `vite build` OOM | 极端情况，分批引入第三方库或升级 CI runner |

### 5.5 e2e-smoke 失败

| 现象 | 优先排查 |
|---|---|
| `/healthz` 返回非 200 | server 未正常启动；检查端口绑定、Embed FS、构建顺序 |
| WebSocket 握手失败 | 路由未注册；检查 `server.go` 中 `e.GET("/v1/logs/stream", ...)` 存在 |
| 故障端点 500 | `s.nodes == nil` 守卫生效；检查 e2e 测试用的 NodeView 是否初始化（本次 fix 已统一用 `s.nodeExists`） |

### 5.6 排查命令快查表

```bash
# 单步重跑（不重新跑全部 6 步）
go test -race -count=1 ./internal/test/e2e/...                  # 单独 e2e
cd web && npm run build                                          # 单独 web
CGO_ENABLED=0 go test -race -count=1 ./internal/adapter/sip/...  # 单独 SIP 子集

# 直接读 JSON（无 jq）
python3 -c "import json; print(json.dumps(json.load(open('docs/smoke-results.json')), indent=2))"
```

---

## 6. 附录

### 6.1 历史报告归档

每次 CI run 都会生成一份独立报告。建议在生产环境将"被审批通过的报告"归档到：

```
docs/smoke-archive/
  smoke-2026-09-28-114200.json    # 时间戳命名：smoke-YYYYMMDD-HHmmss.json
  smoke-2026-09-29-093000.json
  ...
```

归档脚本（运维可选）：

```bash
mkdir -p docs/smoke-archive
TS=$(date -u +%Y%m%d-%H%M%S)
cp docs/smoke-results.json docs/smoke-archive/smoke-${TS}.json
```

### 6.2 退出码约定

| 退出码 | 含义 |
|---|---|
| 0 | 全部 6 步 `passed` |
| 1 | 第一步失败的子命令退出码（脚本透传） |
| 2 | `smoke.sh` 自身异常（参数缺失、JSON 写入失败） |

### 6.3 相关文件清单

| 路径 | 角色 |
|---|---|
| `scripts/smoke.sh` | 冒烟主脚本 |
| `Makefile` (`smoke` 目标) | `make smoke` 入口 |
| `internal/test/e2e/smoke_test.go` | e2e 测试源码 |
| `.github/workflows/ci.yml` (`smoke` job) | CI 集成 |
| `docs/smoke-results.json` | 当前运行结果（gitignore） |
| `docs/smoke-archive/` | 历史报告归档（gitignore） |

---

## 7. 运行时日志级别热更新（PATCH /v1/config/log）

端点支持运维无需重启进程即可调整全局默认级别与按 `component/subsystem` 划分的模块级别（最长前缀匹配）。改动仅作用于内存；重启后会回退到 `log.level` 与 `log.modules` 配置值。

### 7.1 curl 示例

```bash
# 把全局默认降到 debug，并把 SIP 接入层提到 trace
curl -X PATCH http://127.0.0.1:18080/v1/config/log \
  -H 'Content-Type: application/json' \
  -d '{"level":"debug","modules":{"internal/app/sip_acceptor":"trace"}}'

# 响应示例：回显落地的有效级别
# {"level":"debug","modules":{"internal/app/sip_acceptor":"trace"}}

# 只调节某一个模块（默认级别不动）
curl -X PATCH http://127.0.0.1:18080/v1/config/log \
  -H 'Content-Type: application/json' \
  -d '{"modules":{"internal/adapter/cascade":"warn"}}'
```

### 7.2 冒烟用例

| 场景 | 请求体 | 期望状态码 | 期望响应 |
|---|---|---|---|
| 正常：只调默认级别 | `{"level":"debug"}` | 200 | `{"level":"debug","modules":{...}}` |
| 正常：只调模块级别 | `{"modules":{"internal/app":"trace"}}` | 200 | `level="info"`（默认不变） |
| 正常：同时调两级 | `{"level":"warn","modules":{"internal/adapter/cascade":"debug"}}` | 200 | 两者都回显 |
| 异常：空 body | `{}` | 400 | `{"error":"request must include ... "}` |
| 异常：未知级别 | `{"level":"verbose"}` | 400 | `{"error":"invalid level ..."}` |
| 异常：模块级别未知 | `{"modules":{"internal/app":"notice"}}` | 400 | `{"error":"invalid level for module ..."}` |
| 异常：JSON 损坏 | `not json` | 400 | `{"error":"invalid request body: ..."}` |

### 7.3 验收要点

1. PATCH 成功后，`/v1/logs/stream` WebSocket 立刻按新阈值过滤；不再需要重启。
2. 进程重启后，回退到 `configs/config.example.yaml` 中声明的 `log.modules`。
3. 测试覆盖见 `internal/interface/http/log_config_test.go`（10 个用例：level-only / modules-only / 同时调 / 空 body / 未知级别 / 未知模块级别 / 损坏 JSON / 空模块字符串 / 并发 PATCH / image 内大小写规范化）。