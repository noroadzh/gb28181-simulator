# proposal: media-source-config — 媒体源端到端配置

## 阶段

路线图 **#8 media-sources** 适配器层已交付（四种 MediaSource + PS/RTP 管道），
本 change 完成最后两段收尾：

1. **App 层 wiring**（把已就绪的组件连起来）+ 入口补全
2. **运行时配置入口**（HTTP API + Web UI）

> 本 change 严格说"已完成六成"——配置解析、domain 模型、MediaService、NodeService 大部分
> 方法已存在；本 change **只补 wiring 与缺失的运行时 API/UI**。

## 问题陈述

媒体源链路在 app 层有 **3 处 wiring 断点 + 3 处入口缺失**：

### Wiring 断点

| 位置 | 现状 | 后果 |
|------|------|------|
| `cmd/gb28181-simulator/main.go:317` | `acceptor = app.NewAcceptor(...)` 创建在 `mediaService`（line 374）之前 | 编译期无法传 `*MediaService` 给 acceptor；当前 main.go **没有调用** `acceptor.WithMediaService(mediaService)` |
| `mediaService` 实例化后 | 未调用 `mediaService.SetInboundFactories(...)` | 任何 `NewInboundPipeline` 立即返回 `inbound factories not wired` |
| `MediaSourceFactory` 工厂（line 375） | 写死 `media.NewFileSource(cfg)`，未按 `cfg.Kind` 分发 | rtsp/hls/synthetic 永远不会被实例化 |

### 入口缺失

| 位置 | 现状 |
|------|------|
| `internal/app/node_service.go` | 无 `GetMedia` / `SetMedia` / `ClearMedia` 方法（仅 file/rtsp/hls/synthetic 在 domain 层可装载，但运行时无入口切换） |
| `internal/interface/http/nodes.go` NodeView | 12 个方法，缺上述 3 个 |
| `internal/interface/http/server.go` 路由 | 无 `/v1/nodes/:id/media` 三条路由 |
| `web/src/views/NodesView.vue` | 节点详情 dialog 无「媒体源」标签页 |
| `internal/adapter/media/file_source.go` | 无 Loop 行为（`cfg.Loop` 字段存在但被忽略） |

### 副作用：`createInboundPipeline` 链路（已有但部分可用）

`internal/app/acceptor.go:2583` `createInboundPipeline` 已能：
- 通过 `a.mediaService` 拿到 `InboundPipeline`（一旦 SetInboundFactories 被调用）
- 从 SDP 提取 `address`（c= 行 IP）
- 注册到 `a.pipelines[callID]`，由 `closePipeline` 在 BYE 时清理

但有两处实际失效：
- 写的是 `nullESWriter{}`——收到的 RTP 包直接丢弃
- `parseSDPMetadata` 返回的 `portStr` 在 `handleInvite:859` 被 `_` 丢弃，pipeline 拿不到 RTP 推送目标端口

## 方案

### 1. App 层 wiring 收尾（main.go）

在 `mediaService` 实例化（main.go:374）后追加两行：

```go
mediaService.SetInboundFactories(
    func(ssrc uint32) port.RTPDeizer { return media.NewRTPDeizer(ssrc) },
    func() port.PSDepacketizer { return media.NewPSDepacketizer() },
)
acceptor.WithMediaService(mediaService)
```

`acceptor` 在 line 317 创建、`mediaService` 在 line 374 创建——创建顺序本身允许
`WithMediaService` 在 mediaService 实例化之后被调用；只是当前 main.go **没写这两行**。

### 2. MediaSourceFactory 按 kind 分发

替换 main.go:375 的写死工厂为：

```go
func(cfg model.MediaConfig) port.MediaSource {
    switch cfg.Kind {
    case model.SourceKindFile:     return media.NewFileSource(cfg)
    case model.SourceKindRTSP:     return media.NewRTSPSource(cfg)
    case model.SourceKindHLS:      return media.NewHLSSource(cfg)
    case model.SourceKindSynthetic: return media.NewSyntheticSource(cfg)
    default:                        return nil
    }
}
```

### 3. accept 入站管线（`createInboundPipeline`）

`acceptor.go:2583` 现状调用 `extractMediaAddress(sdpBody)` 只取 IP；同步改用
`parseSDPMetadata` 拿 `portStr`，组合成 `host:port` 作为 RTP 推送目标。
但因这是**入站**管线（我们接收 INVITE 来自上级），管线当前不需要"推送"——它需要
**回放媒体源**给上级，等价于：

1. 拿到节点 `NodeProfile.MediaConfig`
2. 调 `mediaService.OpenSource` + `PacketizeOutbound`，onRTP 回调里
   `udpConn.WriteTo(packet.Bytes(), udpAddr)` 推给 SDP 中的 (ip, port)
3. 绑定到 dialog 生命周期，BYE 时关 source

本次 change 范围：
- `handleInvite` 修：不再丢弃 `portStr`，改传给 `createInboundPipeline`
- `createInboundPipeline` 修：拼 `udpAddr`，从节点 profile 取 MediaConfig，调 `PacketizeOutbound`
- `nullESWriter` 保留作为**入站 RTP 解包**的 sink（收到的 ES 流目前无处可写，本次
  change 范围不实现"录像"或"ES 帧转储"——这是后续 change #X 的事）

### 4. HTTP API（新增）

| Method | Path | 语义 |
|--------|------|------|
| `GET` | `/v1/nodes/:id/media` | 返回 MediaConfig JSON；未配置返回 204 |
| `PUT` | `/v1/nodes/:id/media` | 设置/替换 MediaConfig；请求体同 MediaConfig JSON |
| `DELETE` | `/v1/nodes/:id/media` | 清除节点 MediaConfig（kind 置空） |

错误码：
- 未知节点 → 404
- MediaConfig 为空（GET）→ 204
- 配置非法（PUT）→ 400 + JSON error body
- 设置成功 → 200；清除成功 → 204

### 5. FileSource Loop（`file_source.go`）

当前 `Open` 一次打开文件，读到 EOF 即 `io.EOF`。Loop 行为：在 ESReader 层包装，
检测到 EOF 且 `cfg.Loop == true` 时重新 `os.Open` 同一路径，从头继续读。

实现位置：包内新增 `loopingReader` 结构，ESReader 关闭时不真关底层文件，
仅 EOF 触发 reset。

### 6. Web UI 媒体源面板

`NodesView.vue` 节点详情 dialog 新增 `el-tab-pane`：
- 当前状态卡片（kind / path / loop / 关键参数）
- 设置表单（kind `el-select`、path `el-input`、loop `el-switch`、mtu/fps/ssrc/clock `el-input-number`）
- 按钮：保存（PUT）、删除（DELETE）
- 仅 device 节点显示

前端新文件/修改：
- `web/src/api.js` 新增 `getMedia` / `putMedia` / `deleteMedia`
- `web/src/stores/nodes.js` 新增 `mediaConfigs` state + `loadMedia` / `saveMedia` / `removeMedia`
- `web/src/views/NodesView.vue` 新增 tab

## 验收标准

1. `go build ./...` 通过；main.go 包含 `acceptor.WithMediaService(mediaService)` 与
   `mediaService.SetInboundFactories(...)` 两行
2. `config.yaml` 含 `media:` 段时，进程启动后该节点的 `NodeProfile.MediaConfig` 非空
3. `GET /v1/nodes/:id/media` 返回配置内容或 204
4. `PUT /v1/nodes/:id/media` 成功设置后，节点 profile 中 MediaConfig 立即生效
5. device 节点 INVITE 触发后，`a.pipelines[callID]` 包含从 `mediaService.OpenSource` 出来的
   reader；BYE 时清理
6. Web UI 节点详情 → 媒体源 tab 可设置/清除 MediaConfig；4 种 kind 全部可选
7. `FileSource` 在 `cfg.Loop=true` 时重复读取同一文件
8. `openspec validate media-source-config --strict` 通过
9. `openspec validate --strict`（全仓库）通过

## Non-goals

- 不实现历史回放（PLAY with Range）——仅实现实时流
- 不实现双流（主码流+子码流）——单 MediaConfig 对应一个媒体源
- 不实现 SRTP/TLS 媒体加密——SDP a=crypto 在后续 change 处理
- 不实现入站 RTP 的"录像/转储"——收到的 ES 帧无 sink（保留 nullESWriter）
- 不改动 HLS ts demuxer / MP4 demuxer 内部实现
- 不引入 CGO 依赖

## 依赖

- 内部依赖：`internal/adapter/media/`（已有 4 源）、`internal/app/media_service.go`（已有）、
  `internal/domain/model/node.go`（已有 `SetMediaConfig` / `MediaConfig`）
- 无外部新依赖

## 风险评估

| 风险 | 等级 | 缓解 |
|------|------|------|
| wiring 顺序：acceptor 创建早于 mediaService | 低 | 当前 main.go 已如此排布，只需在 mediaService 之后加两行即可 |
| `SetInboundFactories` 缺 wiring | 低 | 同上 |
| `MediaSourceFactory` 写死 file | 低 | 替换为 switch 即可 |
| `createInboundPipeline` 的 portStr 丢失 | 中 | `handleInvite:859` 改用 `portStr` 变量 |
| FileSource Loop 与 `Close` 交互 | 中 | 包装 reader 不持有 file handle，Close 只标记结束 |
| Web UI tab 改动可能影响现有 SPA | 低 | 仅在 NodesView.vue 内新增 tab，不碰其他页面 |
