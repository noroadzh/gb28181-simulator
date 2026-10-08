# tasks: media-source-config

## 1. App 层 wiring 收尾（main.go）

- [ ] 1.1 修改 `cmd/gb28181-simulator/main.go:375`：`MediaSourceFactory` 改为按 `cfg.Kind` 分发 4 种源（file/rtsp/hls/synthetic），未匹配返回 nil
- [ ] 1.2 在 `mediaService` 实例化（line 380）之后追加 `mediaService.SetInboundFactories(media.NewRTPDeizer, media.NewPSDepacketizer)`
- [ ] 1.3 追加 `acceptor.WithMediaService(mediaService)`
- [ ] 1.4 验证 `go build ./...` 通过

## 2. accept 入站管线（`acceptor.go`）

- [ ] 2.1 `internal/app/acceptor.go:859`：`parseSDPMetadata` 返回值由 `_, _, _` 改为 `portStr` 命名变量
- [ ] 2.2 修改 `createInboundPipeline` 签名：新增 `portStr string` 参数；内部用 `address+":"+portStr` 组装 `net.UDPAddr`
- [ ] 2.3 在 `createInboundPipeline` 内调 `mediaService.PacketizeOutbound`（`OnRTP` 回调中写 `udpConn`）；媒体源从节点 profile（通过 `a.registry`）取 `MediaConfig`
- [ ] 2.4 保留 `nullESWriter` 作为入站 ES 帧的临时 sink（无录像需求）
- [ ] 2.5 `closePipeline` 已存在，确保 BYE 流程触发清理（验证）
- [ ] 2.6 单测：`acceptor_test.go` 新增 `TestCreateInboundPipeline_OpensOutbound`（mock mediaService + profile）

## 3. FileSource Loop

- [ ] 3.1 `internal/adapter/media/file_source.go`：在包内新增 `loopingReader` 结构（path + cfg + 当前 handle）
- [ ] 3.2 `loopingReader.Read` 检测到 EOF 且 `cfg.Loop==true` 时重新 `os.Open` 同一路径并继续读
- [ ] 3.3 `FileSource.Open` 在 raw ES 路径（`ftyp` 不匹配）返回 `&loopingReader{...}` 包装
- [ ] 3.4 MP4 demuxer 路径（`ftyp` 匹配）暂不支持 Loop，注释说明
- [ ] 3.5 单测：`file_source_test.go` 新增 `TestFileSource_Loop`：写 1KB 文件，循环读 3 次得到 3KB 内容且顺序连续

## 4. HTTP API

- [ ] 4.1 `internal/interface/http/nodes.go` 的 `NodeView` 接口追加 3 个方法：`GetMedia` / `SetMedia` / `ClearMedia`
- [ ] 4.2 `internal/app/node_service.go` 实现上述 3 个方法（用 `s.registry.Get` + `node.Profile().MediaConfig()` + `registry.MutateProfile`）
- [ ] 4.3 `internal/interface/http/nodes.go` 新增 3 个 handler：`handleGetMedia` / `handlePutMedia` / `handleDeleteMedia`
- [ ] 4.4 `internal/interface/http/server.go`（或 routes 文件）注册 3 条路由：`/v1/nodes/:id/media` 的 GET/PUT/DELETE
- [ ] 4.5 错误码：未配置 GET → 204；非法配置 PUT → 400；未知节点 → 404
- [ ] 4.6 单测：`nodes_test.go` 新增 `TestHandleGetPutDeleteMedia`（mock NodeView）

## 5. Web UI 媒体源面板

- [ ] 5.1 `web/src/api.js` 新增 `getMedia` / `putMedia` / `deleteMedia` 三个 API 封装
- [ ] 5.2 `web/src/stores/nodes.js` 新增 `mediaConfigs` / `mediaLoaded` state + `loadMedia` / `saveMedia` / `removeMedia` action
- [ ] 5.3 `web/src/components/MediaPanel.vue` 新建：未配置/已配置两种状态视图 + Element Plus 表单
- [ ] 5.4 `web/src/views/NodesView.vue` 节点详情 dialog 末尾新增 `el-tab-pane label="媒体源"`，仅 device 节点显示
- [ ] 5.5 表单字段：kind `el-select`（4 选项）、path `el-input`（kind=file 时 placeholder="文件绝对路径"，rtsp/hls 时 placeholder="URL"）、loop `el-switch`、mtu/fps/ssrc/clock `el-input-number`
- [ ] 5.6 按钮：保存（PUT）、取消、删除（DELETE，仅已配置时显示）

## 6. 测试与验证

- [ ] 6.1 `go test ./... -count=1 -race` 全部通过
- [ ] 6.2 `make web`（或 `cd web && npm run build`）构建成功，无错误
- [ ] 6.3 `go run ./cmd/gb28181-simulator -config configs/config.example.yaml` 启动成功
- [ ] 6.4 端到端：device 节点 `media: { kind: synthetic, fps: 25 }` 启动；platform 节点 INVITE 触发；抓包面板（`/v1/nodes/<id>/capture`）能看到 RTP 流量
- [ ] 6.5 `openspec validate media-source-config --strict` 通过
- [ ] 6.6 `openspec validate --strict`（全仓库）通过

## 7. 归档

- [ ] 7.1 `docs/roadmap-15-steps.md` 把 #8 media-sources 行的「✅ 已归档」标记为「⏪ 已拆分：#8 + #19 media-source-config」（或新增一行说明）
- [ ] 7.2 同步 `configs/config.example.yaml` 加入 media 段示例（如未加）
- [ ] 7.3 `git add` 全部相关文件并 `git commit`
- [ ] 7.4 `openspec archive media-source-config --yes` 归档
- [ ] 7.5 验证 archive 后 `openspec/changes/media-source-config/` 移到 `archive/<YYYY-MM-DD-media-source-config>/`

## 验收标准

- [x] 设计阶段：proposal/design/tasks/specs 完成且 openspec validate --strict 通过
- [ ] 实现阶段：上述 §1–§5 全部任务完成
- [ ] 验证阶段：§6 全部探针通过
- [ ] 归档阶段：§7 全部完成，git 推送完成
