# Tasks

## 1. Catalog 拉取路径核实

- [ ] 1.1 核实 platform-large 是否已实现对下级 device 的 catalog 拉取机制（主动 SUBSCRIBE/Query 或被动 NOTIFY），并记录在 `design.md` Open Questions 答复区；缺失则将 task 1.2 转为必修
  - 验收：在 `internal/app/` 找到 catalog 拉取或 catalog 通知处理函数，路径记录在 design.md；缺失则在 task 1.2 实施最小可行的 `MESSAGE/Query catalog` 拉取周期（30s）

## 2. XML 扩展段编解码（manscdp 包）

- [ ] 2.1 在 `internal/adapter/manscdp/catalog.go` 新增 `Extension` / `MediaSource` 结构体并在 `catalogItem` 中加 `Extension *Extension `xml:"Extension"`` 字段
  - 验收：`go build ./internal/adapter/manscdp/...` 通过
- [ ] 2.2 在 `MANSCDPCodecAdapter` 新增 `MarshalCatalogWithMedia(catalog, mediaByDeviceID map[string]MediaConfig)` 方法：按 `mediaByDeviceID` 决定是否填充 `Extension.MediaSource`；既有 `MarshalCatalog` 内部调用 `MarshalCatalogWithMedia(catalog, nil)` 保持行为兼容
  - 验收：`go test ./internal/adapter/manscdp/...` 既有测试不挂
- [ ] 2.3 新增 `MANSCDPCodecAdapter.DecodeCatalogResponse(body string) (model.Catalog, error)`：复用既有 `Response` 结构但挂载扩展段解析，扩展段解析失败 warn 日志后继续
  - 验收：`go test ./internal/adapter/manscdp/...` 新增至少 2 个测试：①标准响应解析；②含扩展段响应解析且 Extension.MediaSource 字段被填充
- [ ] 2.4 新增 `TestCatalogResponseWithExtension_Golden`：构造固定媒体源配置，调用 `MarshalCatalogWithMedia` 与 `DecodeCatalogResponse`，断言输出 XML 字节序列与 `testdata/catalog_with_extension.golden.xml` 一致
  - 验收：`go test ./internal/adapter/manscdp/ -run TestCatalogResponseWithExtension_Golden -update` 生成 golden 文件；二次运行（无 -update）通过字节级比对

## 3. Device 端 catalog 响应注入扩展段

- [ ] 3.1 修改 `internal/app/acceptor.go` 的 `channelCatalogItems` 与 `downstreamChannels`，为每个 `CatalogItem` 携带其通道的有效 `MediaConfig`（按 `MediaConfigForChannel(channelID)` 规则：channel 优先 → node fallback）
  - 验收：单元测试 `TestAcceptor_CatalogIncludesProfileChannels` 仍通过（兼容旧行为）
- [ ] 3.2 新增 `TestAcceptor_CatalogItemCarriesChannelMediaSource`：device profile 配 `mediaConfigByChannel["34020000130000000001"] = {Kind: "file", Path: "/uploads/a.mp4"}`，触发 `channelCatalogItems`，断言返回的 `CatalogItem` 包含预期 `MediaSource` 字段
  - 验收：`go test ./internal/app/ -run TestAcceptor_CatalogItemCarriesChannelMediaSource` 通过
- [ ] 3.3 新增 `TestAcceptor_CatalogItemFallsBackToNodeMedia`：device profile 仅节点级 `mediaConfig = {Kind: "rtsp", Path: "rtsp://cam/stream"}`，触发 `channelCatalogItems`，断言每个通道的 `CatalogItem` 均携带节点级 `MediaSource`
  - 验收：`go test ./internal/app/ -run TestAcceptor_CatalogItemFallsBackToNodeMedia` 通过
- [ ] 3.4 新增 `TestAcceptor_CatalogItemNoMediaNoExtension`：device profile 媒体源全空，断言 `CatalogItem.Extension == nil`
  - 验收：`go test ./internal/app/ -run TestAcceptor_CatalogItemNoMediaNoExtension` 通过

## 4. Platform 端 catalog 解析回填

- [ ] 4.1 在 `internal/app/acceptor.go` 新增 helper `applyMediaSourcesFromCatalog(node model.NodeID, items []model.CatalogItem)`：遍历 Items，识别 `Extension.MediaSource`，调用 `node.Profile().SetChannelMediaConfig(channelID, cfg)`，device id 长度非法则 warn 跳过
  - 验收：函数有 GoDoc 与单元测试 `TestApplyMediaSourcesFromCatalog_*` 覆盖 ①正常回填 ②清除 ③非法 device id 跳过
- [ ] 4.2 在 `acceptor` 收到 device catalog 响应（catalog 拉取路径，依赖 task 1.1 的核实结果）的解析回调处插入 `applyMediaSourcesFromCatalog` 调用
  - 验收：新增集成测试 `TestAcceptor_PlatformParsesCatalogResponseAndFillsChannelMedia`：构造标准 catalog 响应 body（含扩展段），调用 platform 端处理函数，断言 `node.Profile().ChannelMediaConfig(channelID)` 返回扩展段描述的 `MediaConfig`
- [ ] 4.3 新增 `TestAcceptor_PlatformCatalogResponseMalformedExtensionSucceeds`：构造扩展段属性缺失的 catalog 响应，断言整体 catalog 解析成功且该 channel media 不被回填
  - 验收：`go test ./internal/app/ -run TestAcceptor_PlatformCatalogResponseMalformedExtensionSucceeds` 通过
- [ ] 4.4 新增 `TestAcceptor_PlatformCatalogResponseMissingExtensionNoOp`：构造标准无扩展段 catalog 响应，断言 platform 通道 media 配置保持原值不变
  - 验收：`go test ./internal/app/ -run TestAcceptor_PlatformCatalogResponseMissingExtensionNoOp` 通过

## 5. 端到端回归

- [ ] 5.1 全量回归：`go test ./...` 通过
  - 验收：所有包测试无失败
- [ ] 5.2 `openspec validate --strict` 通过
  - 验收：CLI exit 0，无 warning
- [ ] 5.3 （可选）在测试环境 10.96.1.125 部署并验证：device yaml 配置 file 媒体源 → 30s 内 platform 通道详情页可见 `has_media: true`
  - 验收：手工 curl 验证 `GET /v1/nodes/34020000002000000001/channels/34020000130000000001` 返回 `has_media: true, media_kind: "file"`

## 6. 文档与提交

- [ ] 6.1 在 `README.md` 或 `docs/cascade.md`（如有）添加「级联媒体源传播」一节，说明扩展段协议与降级兼容
  - 验收：文档渲染通过 markdown 校验
- [ ] 6.2 单次提交，commit message: `feat(manscdp): catalog Item 携带 MediaSource 扩展段，实现 device→platform 媒体源传播`
  - 验收：`git log -1` 显示该提交，含单元测试与 golden test
