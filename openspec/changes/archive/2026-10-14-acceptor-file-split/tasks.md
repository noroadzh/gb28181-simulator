# Tasks

## 1. 骨架搭建

- [x] 1.1 创建 `internal/app/acceptor_*.go` 空文件骨架（**12 个**文件）
  - 验收：`ls internal/app/acceptor_*.go | wc -l` == 12
  - 实际：options / run / register / messages / catalog / subscribe / invite / device_cmds / sip_helpers / fault / media / sweep
- [x] 1.2 验证 `go build ./internal/app/...` 通过（空文件不影响编译）
  - 验收：`go build ./internal/app/...` exit 0

## 2. 第一批搬移（簇 2/3/6/7，共 39 函数，无相互依赖）

- [x] 2.1 搬移 fault 注入（簇 2）到 `acceptor_fault.go`：`faultGate`/`faultUnsupported`/`recordFault`/`cannedReason` + `sipReasons` 表
  - 验收：`go build ./internal/app/...` && `go test ./internal/app/ -run Fault` 通过
- [x] 2.2 搬移过期清扫（簇 3）到 `acceptor_sweep.go`：`sweepLoop`/`sweep`/`sweepIntervalFor`
  - 验收：`go build ./internal/app/...` && `go test ./internal/app/ -run Sweep` 通过
- [x] 2.3 搬移 REGISTER 认证（簇 6）到 `acceptor_register.go`：`handle`/`challenge`/`grant`/`requestedExpires`/`sipUser`/`transportOf`/`hostOfContact`
  - 验收：`go build ./internal/app/...` && `go test ./internal/app/ -run Register` 通过
- [x] 2.4 搬移媒体管线（簇 7）到 `acceptor_media.go`：`createInboundPipeline`/`parseSDPMetadata`/`extractMediaAddress` + `pipelineEntry`/`inboundOnlyPipeline`/`outboundPipelineEntry`/`closePipeline`/`nullESWriter`
  - 验收：`go build ./internal/app/...` && `go test ./internal/app/ -run Media` 通过
- [x] 2.5 搬移辅助函数到 `acceptor_sip_helpers.go`：`allowHeader`/`deviceAllowHeader`/`httpDate`
  - 验收：`go build ./internal/app/...` 通过

## 3. 第二批搬移（簇 5：Catalog / 订阅 / NOTIFY）

- [x] 3.1 搬移 `acceptor_subscribe.go`：`handleSubscribe`/`subscribersFor`/`notifyCatalogChange`/`notifyDeviceStatus`/`notifyMobilePosition`/`notifySnapshot`/`renderCatalogBody`/`renderDeviceStatusBody`/`renderMobilePositionBody`/`renderSnapshotBody`/`renderAlarmBody`/`renderPresetBody`/`renderHomePositionBody`/`renderCruiseTrackBody`/`flushPendingNotify`/`pendingEventNotify` + `catalogSub` 相关
  - 验收：`go build ./internal/app/...` && `go test ./internal/app/ -run Subscribe` 通过
- [x] 3.2 搬移 `acceptor_catalog.go`：`answerCatalog`/`channelCatalogItems`/`buildCatalogMediaByDeviceID`/`handleCatalogResponse`/`downstreamChannels`
  - 验收：`go build ./internal/app/...` && `go test ./internal/app/ -run Catalog` 通过

## 4. 第三批搬移（簇 4：MESSAGE 分发 + 命令处理）

- [x] 4.1 搬移 `acceptor_messages.go`：`handleMessage`（分发 switch） + `handleInfo`/`routeMANSRTSP`/`infoRouting`/`splitChannel`/`formatNPT`
  - 验收：`go build ./internal/app/...` && `go test ./internal/app/ -run Message` 通过
- [x] 4.2 搬移 `acceptor_device_cmds.go`：`handleDeviceInfo`/`handleRecordInfo`/`handleAlarm`/`handleDeviceControl`/`handlePresetQuery`/`handleHomePosition`/`handleCruiseTrackList`/`handleSnapShot`/`handlePlaybackControl`/`is2022`
  - 验收：`go build ./internal/app/...` && `go test ./internal/app/ -run DeviceCmd` 通过

## 5. 削薄根文件 + 清理

- [x] 5.1 削薄 `acceptor.go`：保留 `Acceptor` struct、`platform` struct、`NewAcceptor`、`With*` 链式设置（迁至 `acceptor_options.go`）、`Serve`/`ServeDevice`/`Stop`/`Close`、`run` 读循环（迁至 `acceptor_run.go`）
  - 验收：`wc -l internal/app/acceptor.go` ≤ 500
  - 实际：**153 行**——保留 `Serving`/`io.Closer` 断言与 5 个生命周期入口（`Serve` / `ServeDevice` / `Stop` / `Serving` / `Close`）
- [x] 5.2 删除空遗留目录 `internal/app/{acceptor,handlers,messages}/`
  - 验收：三个目录不存在
- [x] 5.3 验证 `go build ./...` 全量编译通过
  - 验收：`go build ./...` exit 0
- [x] 5.4 验证测试文件零改动：`git diff --stat -- '*_test.go' | grep -v '0'`
  - 验收：无输出（所有测试文件变更行数为 0）

## 6. 全量回归

- [x] 6.1 `go test ./...` 全部通过
  - 验收：exit 0
- [x] 6.2 spec 校验通过
  - 实际：归档 change 自身 `openspec validate 2026-10-14-acceptor-file-split` exit 0；
        因无 active change，根目录 `openspec validate --strict` 报 "No items found"（设计上不算失败）。
        切到 `openspec validate --all` 校验已归档的 44 个 spec 全部通过。
- [x] 6.3 lint 校验
  - 实际：`golangci-lint run ./...` 退出码非 0，但**本 change 涉及的 12 个新文件 + 削薄后的 acceptor.go 共 13 个文件 0 issue**；
        33 个历史 issue 分布在 `cmd/gb28181-simulator/main.go`、`internal/app/streaming/flv.go`、
        `internal/adapter/media/ps_muxer.go` 等未触及文件，**与本次重构无关**。
        另：项目 `go.mod` 为 `go 1.25` 而本地 `golangci-lint` 走 `go 1.24`，存在版本不一致告警。

## 7. 提交

- [x] 7.1 提交重构（一次性提交：13 个 Go 文件 + 归档 change 目录 + tasks/proposal 修正）
  - 实际：commit `7c16fcf` `refactor(app): 拆分 acceptor.go 为 12 个同包文件，行为零变化`
- [x] 7.2 归档 change
  - 实际：change 目录位于 `openspec/changes/archive/2026-10-14-acceptor-file-split/`，仅 proposal.md + tasks.md（无 specs.md，因本 change 是行为零变化的重构，`skip_specs: true`）
