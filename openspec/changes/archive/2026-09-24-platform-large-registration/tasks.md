# Tasks: platform-large 注册受理与在线设备表

## 1. 模型与端口（domain）

- [x] 1.1 新增 `internal/domain/model/expires_policy.go`：`ExpiresPolicy{min,def,max}` 值对象 + `NewExpiresPolicy`（零值取 60/3600/86400，校验 `0 < min ≤ def ≤ max`）+ 访问器 `Min()`/`Default()`/`Max()`
- [x] 1.2 实现 `ExpiresPolicy.Negotiate(requested uint32) uint32`：0 → 0（注销），其余钳制到窗口内；`requested` 为 0 以外但平台仅缺省时给 `Default()`
- [x] 1.3 新增 `internal/domain/model/downstream_device.go`：`DownstreamDevice` 值对象（deviceID、addr、contact、transport、gbVersion、registeredAt、expiresAt、lastSeenAt）+ `NewDownstreamDevice` 校验（deviceID/addr 非空）与访问器
- [x] 1.4 新增 `internal/domain/model/platform_serving.go`：`PlatformServing{realm, policy}` + 构造器与访问器；realm 为空时由调用方（配置装配）填节点 domain
- [x] 1.5 `NodeProfile` 增加 `WithPlatformServing(ps)` 与 `PlatformServing() (PlatformServing, bool)`（参照既有 `WithRegistration` 的写法）
- [x] 1.6 新增 `internal/domain/port/device.go`：`DownstreamRegistry` 端口（Upsert / Remove / Lookup / List / Clear，全部带 nodeID，注明并发安全）
- [x] 1.7 新增 `internal/domain/port/credentials.go`：`CredentialStore` 端口 `Lookup(nodeID, username) (model.Credentials, bool)`
- [x] 1.8 补 `internal/domain/model` 单测：ExpiresPolicy 的钳制表与非法窗口、DownstreamDevice 校验、PlatformServing 构造

## 2. 适配器（adapter）

- [x] 2.1 新增 `internal/adapter/devicereg/registry.go`：内存在线设备表（`sync.RWMutex` + `map[nodeID]map[deviceID]DownstreamDevice`），实现 `port.DownstreamRegistry`
- [x] 2.2 `devicereg.List` 按 deviceID 升序返回，`Clear` 幂等；`Lookup` 未命中返回 false
- [x] 2.3 新增 `internal/adapter/credstore/store.go`：内存凭据存储（`sync.RWMutex` + 两级 map），`Add(nodeID, cred)` 拒绝重复 username 与空 realm
- [x] 2.4 `credstore.Lookup` 命中返回 `model.Credentials`，未命中返回 false；不得在任何错误/日志中带出密码
- [x] 2.5 两个适配器各加 `var _ port.X = (*T)(nil)` 编译期断言与 doc.go 说明
- [x] 2.6 适配器单测：分区隔离（两节点互不串扰）、并发读写（`-race`）、重复 username 被拒、空表/未命中行为

## 3. Acceptor 用例（app）

- [x] 3.1 新增 `internal/app/acceptor.go`：`NewAcceptor(clock, challenger, authenticator, creds, devices, log)`，字段 nil 时报错（快速失败）
- [x] 3.2 实现 `Serve(id, tr, realm string, policy model.ExpiresPolicy) error`：为该节点起一个 goroutine 并登记 session；重复 Serve 幂等（替换旧 session 前先停旧的）
- [x] 3.3 受理循环：`Receive` → 非 REGISTER 记 debug 后丢弃 → REGISTER 交给 `handle` → `Send(resp, peer)`；`Receive` 出错（ctx 取消 / transport 关闭）即退出
- [x] 3.4 响应构造：回抄请求 `Via` / `From` / `To`（带 tag）/ `Call-ID` / `CSeq`，200 时补 `Contact`、`Expires`、`Date`
- [x] 3.5 401 分支：无 `Authorization` 或解析失败 → 401 + `WWW-Authenticate`（由 `Challenger` 的 realm/nonce/qop/algorithm 拼装），每次新 nonce
- [x] 3.6 403 分支：username 未知、`Verify` 返回 `ErrInvalidResponse` → 403，不重挑战；日志只含 username 与 node_id
- [x] 3.7 200 分支：校验通过 → 协商 `Expires` → 回 200 OK → `devices.Upsert`（写入来源地址、Contact、传输、注册时刻、授予有效期、最后活跃时刻）
- [x] 3.8 `Expires: 0` 分支：回 200 OK（`Expires: 0`）且 `devices.Remove`（幂等，设备不存在不报错）
- [x] 3.9 实现 `Stop(id)`（幂等，停 goroutine 并从 session 表移除）与 `Close() error`（停全部，满足 `io.Closer`）
- [x] 3.10 关键事件打结构化日志：受理成功 / 拒绝（401、403）/ 注销，均带 `node_id`；不记录密码与完整 Authorization

## 4. 接线：NodeService、配置与组合根

- [x] 4.1 `internal/app/node_service.go` 增加 `WithAcceptor(a *Acceptor) (*NodeService, error)`
- [x] 4.2 `Start`：platform-large 节点在 `lifecycle.Start` 后 `acceptor.Serve(...)` 并 `Advance(online)`；受理起不来走既有 `fault` 路径（释放端口）
- [x] 4.3 `Stop` 与 `fault`：`acceptor.Stop(id)` → `devices.Clear(id)` → `lifecycle.Stop`（先停 goroutine 再释放端口）
- [x] 4.4 `internal/platform/config/config.go`：`NodeConfig` 增加 `*NodePlatformConfig`（realm、accounts、min/default/max_expires），字段校验并入 `ValidateNodes`，错误形如 `config: nodes[i].platform.<field>`
- [x] 4.5 配置装配：platform 段 → `model.NewPlatformServing` + `model.NewExpiresPolicy`，并把 accounts 灌进 `credstore`（密码不进日志）
- [x] 4.6 `configs/config.example.yaml`：platform-large 示例条目补 `platform:` 段与注释
- [x] 4.7 `cmd/gb28181-simulator/main.go`：装配 `credstore` / `devicereg` / `Acceptor`（复用 `sipauth.NewResponder(nil)` 与 `NewChallenger(nil)`），`defer acceptor.Close()`
- [x] 4.8 配置单测：解析生效 / 省略取缺省 / 非法（空 realm、重复 username、空密码、窗口不单调）三类

## 5. HTTP 设备查询

- [x] 5.1 `NodeView` 增加 `Devices(ctx, id) ([]model.DownstreamDevice, error)` 与 `Device(ctx, id, deviceID) (model.DownstreamDevice, error)`
- [x] 5.2 `internal/interface/http/nodes.go` 新增 `handleNodeDevices` / `handleNodeDevice`：未知节点或未知设备 → 404 + JSON 错误体，列表按 deviceID 升序
- [x] 5.3 `internal/interface/http/server.go` 注册 `GET /v1/nodes/:id/devices` 与 `GET /v1/nodes/:id/devices/:deviceID`
- [x] 5.4 HTTP 单测：空表返回空数组、有设备时字段齐全且有序、未知节点/设备 404
- [x] 5.5 设备视图的 JSON 不得包含任何凭据字段

## 6. 测试与文档

- [x] 6.1 `internal/app/acceptor_test.go`：scripted transport 覆盖 401（无凭据 / 坏 Authorization）、403（未知用户 / response 不符）、200 入表、重注册刷新、Expires:0 出表、非 REGISTER 丢弃
- [x] 6.2 `internal/app/node_service_test.go` 补充：platform-large 启动 → online、停止 → 表清空 + goroutine 退出（用 fake acceptor 或真实 Acceptor + fake transport）
- [x] 6.3 `internal/adapter/siptest/platform_acceptance_e2e_test.go`：device 节点 → platform-large 节点完成 401 挑战并出现在在线表；停止平台后表清空
- [x] 6.4 e2e 断言用轮询等待（`waitFor`）而非固定 sleep；心跳周期默认 60s，测试窗口内不触发 device 侧失败回落
- [x] 6.5 `README.md` 与 `docs/architecture.md` 增加 platform-large 受理与在线设备表段落
- [x] 6.6 `gofmt` 全量、`go build ./...`、`go test ./... -race` 全绿；校验 app 层未依赖 adapter（`go list -deps ./internal/app/...`）
- [x] 6.7 `openspec validate platform-large-registration --strict` 通过；按 verify → 同步主 specs → 归档 → 单个 commit 收尾
