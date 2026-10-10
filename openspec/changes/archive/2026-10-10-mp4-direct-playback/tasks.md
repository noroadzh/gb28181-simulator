# Tasks

## 1. 后端媒体文件直出端点

- [x] 1.1 新建 `internal/interface/http/media_file.go`：实现 `GET /v1/nodes/:id/channels/:ch/media-file` handler——通道级 `GetChannelMedia` 未配置时回退节点级 `GetMedia`；无配置 404；`Kind != file` 400；`os.Open` + `http.ServeContent` 输出（Range/206、按扩展名 Content-Type）。审查加固：`.mp4` 扩展名白名单 + `filepath.Clean`、500 响应脱敏
- [x] 1.2 在 `internal/interface/http/server.go` 的 `registerRoutes` 注册新路由（放在通道级端点分组附近）
- [x] 1.3 新建 `internal/interface/http/media_file_test.go`：覆盖 200 直出、Range 请求 206 与 Content-Range、通道级回退节点级、非 file 源 400、无配置 404、文件缺失 404、local_file 别名归一七个场景；`go test ./internal/interface/http/ -run MediaFile` 7/7 通过

## 2. 前端播放分派

- [x] 2.1 `web/src/api.js` 新增 `mediaFileUrl(nodeId, ch)` 返回 `/v1/nodes/{id}/channels/{ch}/media-file`（相对路径，与 flvUrl 同风格）
- [x] 2.2 `web/src/views/ChannelDetailView.vue` 重构播放入口：`startPlay()` 先 `getChannelMedia`（has=false 回退 `getMedia`）解析配置；`kind==='file'`（兼容 local_file 别名）且 path 以 `.mp4` 结尾（忽略大小写）时原生模式（`video.src` + error 监听一次性回退 flv.js），否则走既有 `startFlv()`；`stopPlay()`/`onBeforeUnmount` 统一清理两种模式资源；代次计数器防 await 竞态与卸载后 null ref
- [x] 2.3 控制栏与"拉流地址"卡片、"复制播放地址"按钮按当前模式展示/复制实际 URL（原生 media-file 或 flv）；`cd web && npm run build` 构建通过

## 3. 集成验证与收尾

- [x] 3.1 重建 embed 产物并运行后端回归：`go build ./... && go test ./internal/...`，30 包全部 ok 无回归
- [x] 3.2 端到端验证：服务端场景（200 直出/Range 206/回退/400/404/别名归一）已由 httptest 单测全覆盖；浏览器端 UI 播放手工验收移交用户（需真实 MP4 源与浏览器环境）
- [ ] 3.3 运行 `openspec validate mp4-direct-playback --strict` 通过后，按归档流程同步 spec 并提交（含 openspec 工件）
