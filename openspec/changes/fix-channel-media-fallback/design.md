# Design

## Context

platform-large 节点在 Web UI 通道详情页看到 `has_media: false`，而设备节点 YAML 中配置了 `media_source`。根因探明：HTTP API 层的 `handleChannelList` 和 `handleChannelDetail` 只调用 `s.channels.GetChannelMedia()` 查询通道级配置，缺失节点级 fallback。`handleMediaFile` 已正确实现 fallback（media_file.go:42-53），只需将同一模式复制到两个 channel handler。

当前代码（channels.go:129-132, 299-300）：

```
for _, ch := range chs {
    cfg, has, _ := s.channels.GetChannelMedia(...)
    out = append(out, newChannelResponseWithMedia(ch, cfg, has, false))  // 无 fallback
}
```

正确参照（media_file.go:42-53）：

```
cfg, has, err := s.channels.GetChannelMedia(...)
if !has {
    cfg, has, err = s.nodes.GetMedia(ctx, id)  // 关键回退
}
```

## Goals / Non-Goals

**Goals:**
- 修复 `handleChannelList` 和 `handleChannelDetail` 的节点级媒体源 fallback
- 与 `handleMediaFile` 保持行为一致
- 不改变已有 API 路径、请求/响应结构（仅填充字段值）
- 补充 fallback 场景的单元测试

**Non-Goals:**
- 不修改 `NodeService.GetChannelMedia` 接口（仍只返回通道级）
- 不修改 `NodeProfile` 内部逻辑
- 不修改 catalog 同步（MANSCDP XML 不携带媒体源）
- 不修改前端 UI（UI 侧已正确使用响应字段，只需后端修正确值）

## Decisions

### Decision: 在 HTTP handler 层实现 fallback，而非 NodeService 层

**选项 A（采用）**：在 `handleChannelList` 和 `handleChannelDetail` 内追加 fallback 逻辑，风格与 `handleMediaFile` 完全一致。

**选项 B**：将 `NodeService.GetChannelMedia` 内部改用 `NodeProfile.MediaConfigForChannel()`（已实现 channel 优先 + node fallback）。`NodeService.GetChannelMedia` 是公共接口，其他模块依赖它，改变返回值语义可能有副作用。

**结论**：采用选项 A，最小侵入，风险最低。

### Decision: 复用 `s.nodes.GetMedia`，不新写接口

`s.nodes` 已经在 HTTP server 中注入，`s.nodes.GetMedia(ctx, id)` 返回 `MediaConfig` 和 `has`，签名完全匹配 fallback 需求。

### Decision: 测试策略

- 扩展现有 `channels_test.go` 中 `autoIDChannelView` 的 mock：`autoIDChannelView` 已有 `GetChannelMedia` 永远返回 `(zero, false, nil)`，只需在测试用例中额外 mock `NodeService` 的 `GetMedia` 方法，构造"通道级无配置、节点级有配置"的场景，断言响应中 `has_media: true`。
- 不需要新建测试文件，在现有 `channels_test.go` 中增加两个测试函数即可。

## Risks / Trade-offs

- **风险**：`s.nodes` 为 nil 时 fallback 调用 panic（与 `handleMediaFile` 同样风险）。参照 `handleMediaFile` 的处理方式，若 `s.nodes.GetMedia` 返回错误则记录日志并保持 `has=false`，不导致请求失败。
- **风险**：`GetChannelMedia` 的错误被静默丢弃（当前代码即如此）。这是现有代码的问题，不在本 change 范围内扩大。

## Migration Plan

1. **实现**：在 `handleChannelList` 和 `handleChannelDetail` 中各追加 3 行 fallback 代码
2. **测试**：在 `channels_test.go` 中新增两个测试函数
3. **验证**：本地 `go test ./internal/interface/http/...`，远端 deploy 并验证
4. **回滚**：若发现问题，revert 代码改动即可（无 schema 迁移）
