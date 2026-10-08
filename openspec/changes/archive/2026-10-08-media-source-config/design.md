# design: media-source-config

## Overview

媒体源链路在适配器层、domain 层、config 层、MediaService 都已经完成。本 change
聚焦三块：

1. **App 层 wiring 收尾**（`main.go`）+ `acceptor` 入站管线补完
2. **HTTP API 三个端点**（`NodeView` 接口 + `NodeService` 实现 + handlers + routes）
3. **Web UI 媒体源面板**（API 封装 + store + 节点详情 tab）

设计原则：
- **复用现有结构**：`NodeConfig.Media *NodeMediaConfig`、`NodeProfile.SetMediaConfig`、
  `model.MediaConfig.Normalize/Validate` 全部已有，不重写
- **wiring 优先于新功能**：`SetInboundFactories` + `WithMediaService` 是必须补的，
  缺了 `NewInboundPipeline` 立即报错
- **范围克制**：FileSource Loop 实现，不引入 ES 帧录像、不引入 ES 帧持久化

## 1. main.go wiring 收尾

文件：`cmd/gb28181-simulator/main.go`

**当前结构**（line 317–380）：
```go
acceptor, err := app.NewAcceptor(processCtx, ...)            // line 317
acceptor.WithCascadeHandler(cascadeHandler)
...
acceptor.WithDialogs(...).WithPlayback(...).WithSubscribe(...).WithMediaStatus(...).WithNodeRegistry(registry)

mediaService = app.NewMediaService(                          // line 374
    func(cfg model.MediaConfig) port.MediaSource { return media.NewFileSource(cfg) },  // 写死
    ...
)
```

**修改 1**：line 375 工厂改为 switch 分发（见 §1.1）
**修改 2**：在 line 380 之后追加两行（见 §1.2）

### 1.1 MediaSourceFactory 分发

```go
func(cfg model.MediaConfig) port.MediaSource {
    switch cfg.Kind {
    case model.SourceKindFile:
        return media.NewFileSource(cfg)
    case model.SourceKindRTSP:
        return media.NewRTSPSource(cfg)
    case model.SourceKindHLS:
        return media.NewHLSSource(cfg)
    case model.SourceKindSynthetic:
        return media.NewSyntheticSource(cfg)
    default:
        return nil
    }
}
```

### 1.2 SetInboundFactories + WithMediaService

```go
mediaService.SetInboundFactories(
    func(ssrc uint32) port.RTPDeizer { return media.NewRTPDeizer(ssrc) },
    func() port.PSDepacketizer { return media.NewPSDepacketizer() },
)
if err := acceptor.WithMediaService(mediaService).Close(); err != nil {  // err 忽略
    _ = mediaService
}
```

> 实际写法：`acceptor.WithMediaService(mediaService)`（返回 `*Acceptor` 链式）。
> 由于 `WithMediaService` 是位置无关的（`a.mediaService` 是字段），main.go 可在
> `mediaService` 实例化之后的任意位置调用——不必挪 acceptor 创建顺序。

## 2. acceptor 入站管线

文件：`internal/app/acceptor.go`

### 2.1 `handleInvite` 修正（line 859）

**当前**：
```go
mediaType, _, _, sdpErr := parseSDPMetadata(req.Body())
```

**改为**：
```go
mediaType, portStr, _, sdpErr := parseSDPMetadata(req.Body())
// 后续在 default 分支：a.createInboundPipeline(ctx, p, callID, mediaType, portStr, req.Body())
```

### 2.2 `createInboundPipeline` 修正

**当前签名**：
```go
func (a *Acceptor) createInboundPipeline(_ context.Context, _ *platform, callID, mediaType, sdpBody string)
```

**改为**：
```go
func (a *Acceptor) createInboundPipeline(ctx context.Context, p *platform, callID, mediaType, portStr, sdpBody string)
```

**内部逻辑**：
1. `address = extractMediaAddress(sdpBody)` → IP
2. 若 `address == ""` 或 `portStr == ""` → 退出（无媒体描述）
3. `udpAddr, err := net.ResolveUDPAddr("udp", address+":"+portStr)`
4. `udpConn, err := net.DialUDP("udp", nil, udpAddr)`
5. `media := a.mediaService`
6. `cfg, ok := p.profile.MediaConfig()`（**注意**：当前 `platform` 结构不一定有 profile；
   若不可用，本步骤降级为"不调 OpenSource，只保留 nullESWriter"，避免阻塞 dialog 流程）
7. 若 ok：`go func() { _ = media.PacketizeOutbound(ctx, cfg, func(pkt model.RTPPacket) error {
        _, werr := udpConn.Write(pkt.Bytes()); return werr
   })}()`
8. 注册到 `a.pipelines[callID]`，绑定关闭回调

> **关于 platform 持有 profile**：`platform` 结构当前只含 SIP 状态，不持有
> `model.NodeProfile`（NodeService 持有）。如需让 acceptor 在 INVITE 响应时
> 读取 MediaConfig，最小改动是：让 `NodeService.Create` 时把 profile
> 挂到 `platform.profile` 字段（或 `NodeService.QueryMedia(id)` 暴露给 acceptor）。
>
> **本 change 选最简路径**：新增 `internal/app/registry.go` 中的 `NodeRegistry` 接口
> （或在 acceptor 已有的 `a.registry` 字段复用），从 `acptor` 持 `port.NodeRegistry`，
> `handleInvite` 调 `registry.Get(ctx, p.id)` 拿 node，调 `node.Profile().MediaConfig()`
> 拿配置。

### 2.3 `nullESWriter` 保留

入站 RTP 解包后得到的 ES 帧目前无 sink（无录像/转储需求），保留
`nullESWriter{}` 让 dialog 流程不报错。后续 change 实现"录像"时再换为真实 sink。

## 3. HTTP API

### 3.1 NodeView 接口（`internal/interface/http/nodes.go`）

在现有 12 个方法后追加：

```go
GetMedia(ctx context.Context, id model.NodeID) (model.MediaConfig, bool, error)
SetMedia(ctx context.Context, id model.NodeID, cfg model.MediaConfig) error
ClearMedia(ctx context.Context, id model.NodeID) error
```

### 3.2 NodeService 实现（`internal/app/node_service.go`）

```go
func (s *NodeService) GetMedia(ctx context.Context, id model.NodeID) (model.MediaConfig, bool, error) {
    n, ok := s.registry.Get(ctx, id)
    if !ok { return model.MediaConfig{}, false, model.ErrUnknownNode }
    cfg, ok := n.Profile().MediaConfig()
    return cfg, ok, nil
}

func (s *NodeService) SetMedia(ctx context.Context, id model.NodeID, cfg model.MediaConfig) error {
    cfg = cfg.Normalize()
    if err := cfg.Validate(); err != nil { return err }
    _, err := s.registry.MutateProfile(ctx, id, func(p model.NodeProfile) (model.NodeProfile, error) {
        p.SetMediaConfig(cfg)
        return p, nil
    })
    return err
}

func (s *NodeService) ClearMedia(ctx context.Context, id model.NodeID) error {
    return s.SetMedia(ctx, id, model.MediaConfig{}) // empty kind = no source
}
```

> `s.registry.MutateProfile` 假定 `NodeRegistry` 已有；如未提供，改为
> 直接 `s.nodes[id].Profile().SetMediaConfig(cfg)`，取决于 NodeService 内部结构。
> 实现时按现有 API 调整。

### 3.3 HTTP handlers

```go
func (s *Server) handleGetMedia(c echo.Context) error {
    id, err := parseNodeID(c)
    if err != nil { return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()}) }
    cfg, ok, err := s.nodes.GetMedia(c.Request().Context(), id)
    switch {
    case err != nil && errors.Is(err, model.ErrUnknownNode):
        return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
    case err != nil:
        return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
    case !ok:
        return c.NoContent(http.StatusNoContent)
    }
    return c.JSON(http.StatusOK, cfg)
}

func (s *Server) handlePutMedia(c echo.Context) error {
    id, err := parseNodeID(c)
    if err != nil { return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()}) }
    var cfg model.MediaConfig
    if err := c.Bind(&cfg); err != nil {
        return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid json: " + err.Error()})
    }
    if err := s.nodes.SetMedia(c.Request().Context(), id, cfg); err != nil {
        return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
    }
    return c.JSON(http.StatusOK, cfg)
}

func (s *Server) handleDeleteMedia(c echo.Context) error {
    id, err := parseNodeID(c)
    if err != nil { return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()}) }
    if err := s.nodes.ClearMedia(c.Request().Context(), id); err != nil {
        if errors.Is(err, model.ErrUnknownNode) {
            return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
        }
        return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
    }
    return c.NoContent(http.StatusNoContent)
}
```

### 3.4 路由注册

文件：`internal/interface/http/server.go`（或 routes 文件）

```go
e.GET("/v1/nodes/:id/media", s.handleGetMedia)
e.PUT("/v1/nodes/:id/media", s.handlePutMedia)
e.DELETE("/v1/nodes/:id/media", s.handleDeleteMedia)
```

## 4. FileSource Loop

文件：`internal/adapter/media/file_source.go`

当前 `Open` 一次 `os.Open`，读完 EOF 关闭。Loop 行为要求：
- 检测到 EOF 时**重新打开**同一路径
- 持续到 ESReader 自身 `Close`

实现：新增包内 `loopingReader`：

```go
type loopingReader struct {
    path  string
    cfg   model.MediaConfig
    cur   io.ReadCloser
    eofReached bool
}

func (r *loopingReader) Read(p []byte) (int, error) {
    if r.cur == nil {
        f, err := os.Open(r.path)
        if err != nil { return 0, err }
        r.cur = f
    }
    n, err := r.cur.Read(p)
    if err == io.EOF {
        if !r.cfg.Loop {
            return n, io.EOF
        }
        _ = r.cur.Close()
        r.cur = nil
        // 立即重试一次
        f, oerr := os.Open(r.path)
        if oerr != nil { return 0, oerr }
        r.cur = f
        return r.Read(p)
    }
    return n, err
}

func (r *loopingReader) Close() error {
    if r.cur != nil {
        return r.cur.Close()
    }
    return nil
}
```

`Open` 改为：
```go
file, err := os.Open(f.config.Path)
if err != nil { return nil, err }
f.file = file  // 仍保留外层 handle 用于 Close

if n, _ := file.ReadAt(head, 0); n >= 8 && string(head[4:8]) == "ftyp" {
    demuxer, err := NewMP4Demuxer(file)
    if err != nil { _ = file.Close(); return nil, err }
    // MP4 demuxer 与 loopingReader 协同复杂，本次仅给 raw ES 路径加 Loop
    return &mp4ReadCloser{d: demuxer}, nil
}
return &loopingReader{path: f.config.Path, cfg: f.config, cur: file}, nil
```

> 范围限定：仅 raw ES 文件（`ftyp` 不匹配）支持 Loop；MP4 demuxer 路径
> 暂不支持 Loop（避免引入 demuxer seek 复杂度）。

## 5. Web UI

### 5.1 API 封装（`web/src/api.js`）

```js
export const getMedia = (id) =>
  api('GET', `/v1/nodes/${id}/media`)
export const putMedia = (id, body) =>
  api('PUT', `/v1/nodes/${id}/media`, body)
export const deleteMedia = (id) =>
  api('DELETE', `/v1/nodes/${id}/media`)
```

### 5.2 store（`web/src/stores/nodes.js`）

```js
const mediaConfigs = ref({})          // { [nodeId]: MediaConfig | null }
const mediaLoaded   = ref({})          // { [nodeId]: bool }
const loadMedia = async (id) => { ... }
const saveMedia = async (id, cfg) => { ... }
const removeMedia = async (id) => { ... }
```

### 5.3 NodesView.vue 标签页

在节点详情 dialog 的 `el-tabs` 末尾新增：
```html
<el-tab-pane label="媒体源" name="media">
  <!-- 仅 device 节点显示 -->
  <template v-if="node.kind === 'device'">
    <MediaPanel :node-id="node.id" />
  </template>
</el-tab-pane>
```

**`MediaPanel.vue`**（新文件 `web/src/components/MediaPanel.vue`）：
- 未配置：显示「设置媒体源」按钮 → 展开表单
- 已配置：显示当前状态卡片 + 「修改」/「删除」按钮
- 表单字段：kind（el-select 4 选项）、path（el-input，kind=file 时 placeholder 不同）、
  loop（el-switch）、mtu/fps/ssrc/clock（el-input-number）
- 提交调 `saveMedia`；删除调 `removeMedia`

## 6. 测试策略

### 单元测试

| 文件 | 测试内容 |
|------|---------|
| `internal/adapter/media/file_source_test.go` | `TestFileSource_Loop`：写测试文件 + 读 2N 字节 == 读 N 字节 × 2 |
| `internal/app/acceptor_test.go` | `TestCreateInboundPipeline_OpensOutbound`：mock mediaService，验证 (ip, port) 拼装、UDP 写入 |
| `internal/app/node_service_test.go` | `TestNodeService_GetSetClearMedia`：profile mutate 正确性 |
| `internal/interface/http/nodes_test.go` | `TestHandleGetPutDeleteMedia`：mock NodeView 三个端点 |
| `internal/platform/config/config_test.go` | （已存在，验证 media 段解析；本次无需新增） |

### 集成测试

| 文件 | 测试内容 |
|------|---------|
| `internal/app/media_e2e_test.go` | （已存在，验证 PacketizeOutbound 端到端） |

### 冒烟探针

| Capability | 探针 |
|-----------|------|
| `media-source-config` | `go test ./internal/app/... ./internal/interface/http/... ./internal/adapter/media/... -count=1 -race` 通过 |
| `media-source-ui` | `make web` 构建成功，`vite build` 无错误 |
