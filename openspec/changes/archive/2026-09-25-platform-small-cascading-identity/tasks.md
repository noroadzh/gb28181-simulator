# Tasks

## 1. 用例：platform-small 的双向分派

- [x] 1.1 `internal/app/node_service.go`：`Start` 对 `NodeKindPlatformSmall` 先调用 `serve`（受理下级），
      再在声明了 `registration:` 时调用 `register`（向上注册）；验证：platform-small 启动后既是 UAS
      又是 UAC（e2e 4.1）
- [x] 1.2 platform-small 未声明 `platform:` 时仍以 `DefaultPlatformServing(domain)` 受理；验证：单
      元用例断言省略段也进入受理分支
- [x] 1.3 状态推进幂等：新增内部 `advanceOnline(ctx, id)`——已是 `registered`/`online` 时跳过对应
      `MarkRegistered`/`MarkOnline`；`serve` 与 `register` 都改用它；验证：`node_status` 的转换表不
      变，第二半不再返回 `ErrIllegalTransition`
- [x] 1.4 任一半失败即回滚另一半：`register` 失败时先 `stopServing(id)`（停受理 goroutine + 清在线
      表）再 `fault`，日志写明是哪一半失败；验证：单元用例断言失败后 `Serving()` 归零、在线表空、
      状态 `fault`
- [x] 1.5 单元：`internal/app/node_service_platform_small_test.go` 覆盖只受理 / 受理+注册 / 注册失败
      回滚 / 两半共用一次状态推进；`go test ./internal/app/ -run PlatformSmall` 通过

## 2. 停止与生命周期

- [x] 2.1 `Stop` 对 platform-small 同时停受理（`stopServing`）、停保活与重注册（`stopKeeping`），并
      向上注销；验证：单元用例断言停止后无残留 goroutine、节点 `offline`
- [x] 2.2 重复 `Start` / `Stop` 幂等：再次 Start 替换旧受理 goroutine 而不是并存；验证：
      `Serving()` 始终为 1
- [x] 2.3 一个 platform-small 故障不影响同进程另一个 platform-small；验证：单元用例两节点并发起停

## 3. 配置与装配

- [x] 3.1 `internal/platform/config/config.go`：`NodePlatformConfig` 与 `Registration` 的注释改为对
      platform-small 同样生效；无新增键
- [x] 3.2 `configs/config.example.yaml` 增补一个 platform-small 级联示例（同时含 `platform:` 与
      `registration:` 两段）
- [x] 3.3 单元：platform-small 条目两段并存加载成功；`platform.accounts` 重复/空密码、非法有效期
      窗口、`registration` 缺 server 仍被拒；`go test ./internal/platform/config/` 通过
- [x] 3.4 复核 `cmd/gb28181-simulator/main.go`：Acceptor / Registrar / Keeper 的装配与 kind 无关，
      本 change 无需改动；`go build ./...` 通过

## 4. e2e：三级级联

- [x] 4.1 `internal/adapter/siptest/platform_small_e2e_test.go`：device → platform-small →
      platform-large 三级链路——small 注册到 large 并保活、device 注册到 small 并保活；断言 large 的
      在线表含 small、small 的在线表含 device
- [x] 4.2 e2e：由 platform-large 侧向 platform-small 下发裸 `MESSAGE`（`CmdType = Catalog`）查询；
      断言收 200 且 body 解析出 device 条目（`SumNum = 1`）
- [x] 4.3 e2e：platform-small 的上级没有它的账号 → 启动失败进入 `fault`，且已建立的受理被回滚
      （small 在线表为空、不再受理）
- [x] 4.4 e2e：停止 platform-small → large 侧清扫后不再含 small（或收到注销），small 侧在线表清空、
      状态 `offline`

## 5. 收尾

- [x] 5.1 `go test ./... -race -count=1` 全绿
- [x] 5.2 `README.md`：补 platform-small 身份与级联拓扑（`platform:` + `registration:` 同时声明即中继）
- [x] 5.3 `docs/architecture.md`：补 platform-small 章节（分派顺序、幂等推进、失败回滚）
- [x] 5.4 `openspec validate "platform-small-cascading-identity" --strict` 通过
- [x] 5.5 同步主 spec：新建 `openspec/specs/platform-small-node/spec.md`（合并 ADDED 需求）
- [x] 5.6 归档到 `openspec/changes/archive/2026-09-25-platform-small-cascading-identity/`
- [x] 5.7 提交 commit
