# Tasks: device 保活与注销

## 1. 值对象：心跳参数

- [x] 1.1 `internal/domain/model/registration.go`：给 `RegistrationParams` 增加 `HeartbeatInterval` / `HeartbeatTimeout` / `HeartbeatMaxFailures`，零值取默认（60s / 5s / 3）
  - 验收：常量 `DefaultHeartbeatInterval = 60s`、`DefaultHeartbeatTimeout = 5s`、`DefaultHeartbeatMaxFailures = 3` 导出；`NewRegistration` 对零值补默认
- [x] 1.2 增加访问器 `HeartbeatInterval()` / `HeartbeatTimeout()` / `MaxHeartbeatFailures()` 与校验（interval>0、timeout>0、timeout<interval、maxFailures≥1）
  - 验收：非法组合返回带字段名的错误；`registration_test.go` 覆盖默认值与四类非法值
- [x] 1.3 `internal/domain/model/keepalive.go`：新增 `Keepalive` 值对象（`DeviceID` 20 位、`SN`、`Status`）+ `NewKeepalive` 与访问器
  - 验收：非法 DeviceID / Status 返回错误；单测覆盖

## 2. 端口：Keepalive 编解码与 Ticker

- [x] 2.1 `internal/domain/port/keepalive.go`：新增 `KeepaliveCodec` 端口（`MarshalKeepalive(model.Keepalive) (string, error)`）
  - 验收：端口文件带文档注释；`port_test.go` 的 compile-time 断言风格沿用
- [x] 2.2 `internal/domain/port/clock.go`：新增 `Ticker`（`C() <-chan time.Time` / `Stop()`）与 `TickerFactory`
  - 验收：注释说明"为什么需要可注入"（确定性测试 + 可中断）
- [x] 2.3 `internal/platform/clock`：真实 ticker 实现与 `RealTicker()` 工厂
  - 验收：`Stop()` 幂等；单测验证真实 ticker 至少投递一次

## 3. MANSCDP Keepalive 编解码（adapter）

- [x] 3.1 新建 `internal/adapter/manscdp`：`Keepalive` 通知的 XML 序列化（`<Notify><CmdType>Keepalive</CmdType><SN/><DeviceID/><Status>OK</Status></Notify>`，带 XML 声明）
  - 验收：实现 `port.KeepaliveCodec`；compile-time 断言 `var _ port.KeepaliveCodec = ...`
- [x] 3.2 golden test：心跳 body 的字节级比对（`testdata/keepalive.xml`）
  - 验收：golden 文件存在且与序列化输出逐字节相等；字段顺序固定
- [x] 3.3 XML 特殊字符转义与非法输入
  - 验收：DeviceID 含 `&` / `<` 时返回错误或正确转义（二者择一并写进测试）；不产生畸形 XML

## 4. app：保活调度器 Keeper

- [x] 4.1 `internal/app/keeper.go`：`Keeper` 结构与构造（依赖 `port.NodeRegistry`、`port.NodeLifecycle`、`*Registrar`、`port.KeepaliveCodec`、`port.Clock`、`port.TickerFactory`、`*slog.Logger`）
  - 验收：app 包不 import adapter（`go list -deps ./internal/app/...` 断言仍绿）
- [x] 4.2 `Start`：为节点起一个 goroutine，按 `HeartbeatInterval` 建立 ticker
  - 验收：同一节点重复 `Start` 不产生第二个 goroutine；`Stop` 幂等
- [x] 4.3 心跳事务：构造 MESSAGE（新 Call-ID、递增 SN、`Content-Type: Application/MANSCDP+XML`）、发送、在 `HeartbeatTimeout` 内等匹配响应（Call-ID + peer 双重过滤）
  - 验收：2xx 归零计数；超时/非 2xx 计一次失败；不匹配响应被忽略
- [x] 4.4 连续失败达 `MaxHeartbeatFailures` → 停本节点后台任务 → `lifecycle.Fail` → 退出 goroutine
  - 验收：错误含失败次数与阈值；节点 `fault` 且端口释放
- [x] 4.5 重注册：每拍用 `clock.Now()` 与 `nextRenewAt` 比较触发，成功更新 `RegistrationResult` 并重算时刻（不改状态）
  - 验收：到期时刻 = `min(granted/2, granted-60s)`；平台缩短过期时按新值重算（测试覆盖 3600 → 600）
- [x] 4.6 重注册失败按指数退避（5s 起、上限 60s）重试，成功后归零
  - 验收：退避序列可被测试断言（5s/10s/20s/40s/60s/60s…）；退避期间不重复发起
- [x] 4.7 `Stop` / `Close`：停单个 / 停全部，goroutine 在端口释放前退出
  - 验收：`-race` 无告警；`Close` 后无 goroutine 残留

## 5. app：注销事务

- [x] 5.1 `Registrar.Unregister(ctx, tr, node, reg)`：`Expires: 0` 的 REGISTER，同样应答 401，复用 `RegisterStage` 分期错误
  - 验收：401 流程与 `Register` 一致；非 2xx 与超时分别落在 `response` / `timeout` 阶段
- [x] 5.2 `NodeService.Unregister`：状态校验（非 `registered`/`online` → `ErrIllegalTransition`）→ 发注销事务 → 成功则先 `keeper.Stop` 再 `lifecycle.Stop`
  - 验收：失败时节点保持 `online` 且心跳继续；成功后 `offline` 且端口释放
- [x] 5.3 单元与集成测试：成功 / 401 / 超时 / 5xx / 非法迁移五条路径
  - 验收：全部用 scripted transport，无 sleep

## 6. 接线：NodeService 与组合根

- [x] 6.1 `NodeService` 增加 `keeper` 字段与 `WithKeeper`，`Start` 成功后启动、`Stop`/`fault` 时停止
  - 验收：`Start` 失败回落 `fault` 的路径不残留后台任务
- [x] 6.2 `cmd/gb28181-simulator/main.go`：装配 `manscdp` 适配器、`clock.RealTicker`、`Keeper`，并注册进 `servicectx` 关闭链
  - 验收：进程关闭时后台任务退出；`go build ./...` 通过

## 7. 配置

- [x] 7.1 `internal/platform/config/config.go`：`NodeRegistrationConfig` 增加 `heartbeat_interval` / `heartbeat_timeout` / `heartbeat_max_failures`，并接入既有校验分支
  - 验收：非法值报出条目序号与字段名；缺省取默认值
- [x] 7.2 `configs/config.example.yaml` 增加带注释的心跳参数示例
  - 验收：示例可被加载且解析出默认值/声明值
- [x] 7.3 配置层单测（新增 `heartbeat_config_test.go`）
  - 验收：缺省、声明值、四类非法值各一条用例

## 8. HTTP 接口

- [x] 8.1 `internal/interface/http/nodes.go`：`NodeView` 加 `Unregister`，新增 `handleNodeUnregister` 复用 `controlNode`
  - 验收：404 / 409 / 502（含 `stage`）语义与 start/stop 一致
- [x] 8.2 `internal/interface/http/server.go:75` 附近注册 `POST /v1/nodes/:id/unregister`
  - 验收：路由存在且返回预期状态码
- [x] 8.3 HTTP 层测试：注销成功 / 失败（含 stage）/ 未在线 409 三条用例
  - 验收：错误体不含凭据明文

## 9. 测试基建与端到端

- [x] 9.1 `internal/adapter/siptest/uas.go`：`Serve` 支持 MESSAGE（默认 200 OK），新增 `WithSilentMessages()` 与 `Keepalives()`
  - 验收：既有 REGISTER 行为不变；心跳可被记录与关闭应答
- [x] 9.2 端到端：两节点注册后各自保活、互不串扰，均收到自己的 200 OK
  - 验收：每个 UAS 只收到自己节点的心跳
- [x] 9.3 端到端：`WithSilentMessages()` 下连续 3 次失败 → `fault` 且端口释放
  - 验收：状态与端口可复用均被断言
- [x] 9.4 端到端：注销成功后 UAS 侧不再收到心跳
  - 验收：等待窗口后 `Received()` 长度不再变化

## 10. 收尾

- [x] 10.1 `gofmt -l` 干净、`go vet ./...`、`go test ./...` 与 `go test -race ./...` 全绿
  - 验收：三条命令均无输出错误
- [x] 10.2 `openspec validate device-node-keepalive --strict` 通过
  - 验收：delta 与主 spec 的 MODIFIED 需求同名
- [x] 10.3 更新 `docs/architecture.md` 与 `README.md` 的能力清单（保活/重注册/注销）
  - 验收：文档与实际行为一致
