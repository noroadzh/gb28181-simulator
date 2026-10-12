# Tasks: platform-large 双半与通道聚合

阶段：#6（主线）/ #7（复用）/ #9（次级）

## 1. Probe 与现状核查

- [ ] 1.1 Probe: 核查 `unregisterUpstream`（`internal/app/node_service.go:586-588`）对 platform-large + wants=true 的行为——若 platform-large 也走 startPlatformSmall，是否仍能向上级发 `Expires: 0` 告别。验收：编写一个 unit test 让 platform-large 在 startPlatformSmall 后被 Stop，确认离开时 registrar 收到一条 `Expires: 0` 的 REGISTER；若不发，标记 fail 进入 2.x 修复路径。

- [ ] 1.2 Probe: 核查 `internal/interface/http/channels.go:170-234` 对 platform-large 的实际行为——理论上 `AddChannel` 不按 kind 拦，但仍需端到端验证。验收：现有 unit test 覆盖 device 加通道的用例，运行时通过 `curl -X POST localhost:8080/v1/nodes/{platform-large-id}/channels` 看返回码；记录返回码与是否落库。

## 2. 节点启动路径修复（platform-large 双半）

- [ ] 2.1 在 `node_service.go:324-327` 的 platform-large 分支加上 `wants` 判断：有 `registration:` 时调用 `startPlatformSmall`，无 `registration:` 时维持 `serve`。验收：unit test 覆盖三种情形——`registration` 缺省 → `serve` 被调一次；`registration` 存在 + 上级可达 → 两半都成功；`registration` 存在 + 上级不可达 → 节点进入 fault 且监听端口被释放。

- [ ] 2.2 修复 1.1 中暴露的 `unregisterUpstream` NodeKind 判断。验收：1.1 改写的 unit test 现在通过；若不需修复则在原代码上加 `wants` 替代 `NodeKindPlatformSmall` 判断，注释解释原因。

## 3. Catalog 聚合通道

- [ ] 3.1 在 `internal/app/acceptor.go` 的 Catalog 渲染路径中并入节点自身通道。验收：新增 unit test `TestAcceptor_CatalogAggregatesChannels` —— 注册两条下级 + 节点自身一条通道 → Catalog 响应 DeviceList 三条按 device id 升序；空通道场景；运行时 `go test ./internal/app/...` 通过。

- [ ] 3.2 编写通道→CatalogItem 字段映射函数（含 Manufacturer="SIMULATOR" / Model="VIRTUAL" / Secrecy=0 等固定值）。验收：函数被 3.1 测试覆盖；映射一致性由 golden test 在 `internal/adapter/manscdp` 验证。

- [ ] 3.3 添加 Catalog 响应的 golden test（`internal/adapter/manscdp`）。验收：覆盖三种字节级快照——含下级 + 通道、仅含下级、仅含通道空设备表。每种快照的 XML 字节序列稳定。

## 4. 通道 HTTP 端点校验增强

- [ ] 4.1 在 `internal/interface/http/channels.go:handleChannelAdd` 入口校验 channel_id 是否为 20 位数字。验收：unit test 覆盖合法 20 位 ID 回 201、非数字 ID 回 400、19 位字符 ID 回 400。

- [ ] 4.2 在 `internal/interface/http/channels_test.go` 补齐 platform-large 节点加 / 删 / 列表通道用例。验收：测试覆盖 POST 201 / DELETE 204 / GET 200 含该通道三个用例；测试文件单独标记 `// for platform-large change`。

## 5. 配置与文档

- [ ] 5.1 在 `configs/examples/` 下新增 `platform-large-cascading.yaml`：一个 platform-large 节点声明 `platform:` 段、`registration:` 指向一个不存在的上游、`channels:` 通过命令行或 API 注入的示例。验收：`go run ./cmd/simulator -config configs/examples/platform-large-cascading.yaml` 启动后状态为 fault（因上游不可达），日志显示期望的双半启动序列。

- [ ] 5.2 更新 `docs/web-ui-guide.md`（如有问题）：在 platform-large 节点类型说明处追加"platform-large 现在可声明 `registration:` 与 `channels:`"。验收：文档小节标题明确，文末指向本 change 名。

## 7. 端到端验证

- [ ] 7.1 端到端集成测试 `TestE2E_PlatformLargeCatalogIncludesChannels`。验收：用 `scriptedTransport` 模拟一个上游平台发 `MESSAGE(Catalog)` 到 platform-large（已加 3 条通道），验证响应 DeviceList 含 3 条通道且按 device id 升序；XML 字节序列由 golden test 二次校验。

- [ ] 7.2 端到端验证 platform-large 向上级注册后能稳定保活。验收：scriptedTransport 模拟上游发 `MESSAGE(Keepalive)` 三次后中止；验证 platform-large 节点在故障检测前维持 online。

## 8. 收尾

- [ ] 8.1 `go test ./...` 全绿。验收：所有 unit / integration 测试通过，golden test 全部匹配。

- [ ] 8.2 `golangci-lint run ./...` 无 error 级告警。验收：CI lint 任务通过。

- [ ] 8.3 把本 change 在 `/opsx:archive` 前用 `/opsx:verify` 跑一遍。验收：`openspec verify "platform-large-upstream-and-channels"` 返回 0。