# Tasks: platform-large 双半与通道聚合

阶段：#6（主线）/ #7（复用）/ #9（次级）

## 8. 收尾

- [ ] 8.2 `golangci-lint run ./...` 无 error 级告警。验收：CI lint 任务通过。**2026-10-27 核查**：本机 golangci-lint 由 go1.24 构建，无法分析 go1.25 项目，只能由 CI（`.github/workflows/ci.yml` 的 golangci-lint-action）验证；未在本地复现。

## 核查记录（2026-10-27）

归档时遗留的条目已逐条复核，功能均已交付，原条目移除：

- 1.1 / 2.1 / 2.2：`internal/app/node_service_platform_large_test.go` 覆盖双半启动三情形与停止两侧（含 `Expires: 0` 告别）；`unregisterUpstream` 以 `wants` 判定，不再看 NodeKind。
- 1.2 / 4.1 / 4.2：`internal/interface/http/channels_platform_large_test.go` 覆盖 platform-large 加/删/列表通道；20 位 ID 回 201，19 位与非数字回 400。
- 3.1 / 3.2 / 3.3：`TestAcceptor_CatalogAggregatesChannels`；`internal/domain/model/catalog.go` 的 CatalogItem 默认值（Manufacturer=`gb28181-simulator`、Model=`simulator`，与条目里的占位常量不同但语义一致）；`internal/adapter/manscdp/testdata/catalog_{devices_and_channels,devices_only,channels_only}.xml` 三份 golden。
- 5.1 / 5.2：`configs/examples/platform-large-cascading.yaml`；`docs/web-ui-guide.md` 的 platform-large 小节已说明 `registration:` 与 `channels:`。
- 7.1 / 7.2：`internal/adapter/siptest/platform_messaging_e2e_test.go` 的 `TestE2E_PlatformLargeCatalogIncludesChannels` 与 `TestE2E_PlatformLargeUpstreamKeepalive`。
- 8.1：2026-10-27 `make test`（`-race -cover`）退出码 0。
- 8.3：`openspec verify` 子命令已不存在，归档前验证改由 `/opsx:verify` 承担。
