# Tasks

## 1. 后端 HTTP handler 修复

- [x] 1.1 修改 `handleChannelList`（`internal/interface/http/channels.go`）：在 `GetChannelMedia` 返回 `has=false` 时回退调用 `s.nodes.GetMedia(ctx, id)`，将回退得到的 `cfg`/`has` 传给 `newChannelResponseWithMedia`；验证：`go build ./...` 通过
- [x] 1.2 修改 `handleChannelDetail`（同名文件）：同样追加节点级 fallback；验证：`go build ./...` 通过
- [x] 1.3 确认两处 fallback 与 `handleMediaFile` 的错误处理一致（`GetMedia` 出错时记录日志并保持 `has=false`，不返回 5xx）；验证：代码审查对照 `media_file.go:42-53`

## 2. 单元测试

- [x] 2.1 在 `internal/interface/http/channels_test.go` 新增测试：通道级无配置、节点级有配置时，`GET /v1/nodes/:id/channels/:ch` 返回 `has_media=true` 且 `media_kind` 等于节点级 kind；验证：`go test ./internal/interface/http/ -run TestChannelDetail -v` 通过
- [x] 2.2 在 `channels_test.go` 新增测试：同等条件下 `GET /v1/nodes/:id/channels` 列表项 `has_media=true`；验证：`go test ./internal/interface/http/ -run TestChannelList -v` 通过
- [x] 2.3 新增测试：通道级已配置时优先返回通道级（不被节点级覆盖）；验证：对应测试用例通过
- [x] 2.4 新增测试：两级均未配置时 `has_media=false` 且 `media_kind` 省略；验证：对应测试用例通过

## 3. 回归与验证

- [x] 3.1 运行全量 http 包测试确认无回归：`go test ./internal/interface/http/...`；验证：全部通过
- [x] 3.2 运行 `openspec validate fix-channel-media-fallback --strict`；验证：校验通过无错误
- [x] 3.3 （可选，若用户要求部署）本地交叉编译 linux/amd64 并部署到 10.96.1.125，实测 platform-large 通道详情页显示媒体源已配置；验证：远端 GET 通道详情 `has_media=true`（a182189 已部署，device 节点 34020000001310000001 的通道 34020000130000000001 实测 has_media=true, media_kind=file）
