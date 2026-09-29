# Tasks

> 状态说明：以下勾选反映仓库当前真实实现状态。本变更交付的是 Logging 基础设施（§1）、HTTP 热更新接口（§4）、主要业务路径的日志调用点（§2.1–2.3、§3.1、§3.3–3.5）。未完成项（§2.4 manscdp 业务面、§3.2 媒体热循环调用方接入、§5.1 文档）记录于末尾 "Deferred" 小节，由后续变更跟进，不阻塞本次归档。

## 1. Logger infrastructure extension

- [x] 1.1 Extend MultiHandler with `defaultLevel` + `moduleLevels map[string]slog.Level`; update `Enabled()` to perform longest-prefix match on `component`/`subsystem`; verify new field-based filtering test passes (`internal/platform/observability/logging/logger_test.go` 添加 `TestModuleLevelFiltering`)
- [x] 1.2 Add `logging.UpdateLevels(default Level, modules map[string]Level) error` with RWMutex protection; verify concurrency test passes
- [x] 1.3 Add `internal/adapter/media/error_aggregator.go` with 60-second window aggregation; verify unit test on aggregation flush
- [x] 1.4 Add `cfg.Log.Modules` field to viper config struct (kebab-case → `log.modules`); update `configs/config.example.yaml` with commented example; verify config load test

## 2. Log call sites: SIP & business plane

- [x] 2.1 Add trace/debug log calls in `internal/app/acceptor.go` for INVITE/REGISTER/MESSAGE/BYE boundaries; verify no existing test breaks
- [x] 2.2 Add log calls in `internal/app/device_registrar.go` for register state transitions; verify registrar e2e test still passes
- [x] 2.3 Add log calls in `internal/app/keeper.go` for heartbeat lifecycle; verify keeper test passes
- [ ] 2.4 Add log calls in `internal/adapter/manscdp/*.go` for XML decode failures and command dispatch; verify manscdp tests pass → **Deferred**（见末尾）
- [x] 2.5 Add log calls in `internal/adapter/auth/*.go` for 401 challenge generation and SM2/SM3 verification; verify auth tests pass

## 3. Log call sites: node lifecycle, media, cascade, capture, fault

- [x] 3.1 Add log calls in `internal/app/node_service.go` for state transitions (unstart → running → pause → error); verify node service tests pass
- [ ] 3.2 Add log calls in `internal/adapter/media/ps_packetizer.go` and `rtpizer.go` using new error aggregator; verify media e2e test passes → **Deferred**（见末尾）
- [x] 3.3 Add log calls in `internal/adapter/cascade/handler.go` for X-RoutePath parsing and OriginDeviceID rewrite; verify cascade test passes
- [x] 3.4 Add log calls in `internal/adapter/capture/ring.go` for buffer overflow/drop events; verify capture test passes
- [x] 3.5 Add log calls in `internal/app/faults.go` for fault injection hit/recovery; verify faults gate test passes

## 4. HTTP API: PATCH /v1/config/log

- [x] 4.1 Add Echo handler `PATCH /v1/config/log` in `internal/interface/http/` accepting `{"level": "..."}` and `{"modules": {...}}`; verify 200 on valid input, 400 on invalid level
- [x] 4.2 Wire handler to call `logging.UpdateLevels(...)`; verify in-memory level takes effect within 1 second (poll hub subscriber)
- [x] 4.3 Add integration test asserting that PATCH `{"modules": {"internal/app": "debug"}}` causes debug records to flow through Hub; verify test
- [x] 4.4 Add integration test asserting restart restores `file.conf` value (PATCH is non-persistent); verify test

## 5. Documentation & verification

- [ ] 5.1 Update `docs/` with logging configuration reference (env vars + modules syntax); verify doc renders → **Deferred**（见末尾）
- [x] 5.2 Run `make lint` and `make test` and ensure zero new warnings/failures; verify CI equivalent

## Deferred (out of scope for this archive, follow-up change required)

- **§2.4** manscdp 业务面日志调用（XML 解码失败、指令派发）：覆盖 alarm/catalog/keepalive/notify/ptz 等 18 个文件，grep 显示 0 处日志调用点。需新增 change `manscdp-logging-coverage` 跟进。
- **§3.2** 媒体热循环错误聚合调用方接入：`error_aggregator.go` 已实现并单元测试通过，但 `ps_packetizer.go` 与 `rtpizer.go` 尚未调用 `aggregator.Record(...)`。需新增 change `media-aggregator-integration` 跟进。
- **§5.1** `docs/logging.md` 用户文档：env 变量、`log.modules` 语法、PATCH 接口契约参考。需新增 change `logging-user-docs` 跟进。
