# Proposal: media-source-local-file-alias

## Summary

修复前端上传文件后自动绑定媒体源时使用的 kind 值与后端 MediaSourceFactory 不匹配的问题。根因是前端 `ChannelListView` 上传文件后发送 `kind: "local_file"`，但后端 `MediaSourceFactory` 仅认识 `file`/`rtsp`/`hls`/`synthetic` 四种，导致 FLV 流媒体端点 404。修复方案：在 `MediaConfig.Normalize()` 中将 `local_file` 归一化为 `file`，使前端别名与后端工厂无缝对接。

## Background

### 故障描述

用户通过 Web UI 通道列表上传 MP4 文件并自动绑定为通道媒体源后，点击"开始播放"按钮，FLV 流端点返回 404，后端日志：

```
streaming: open source: app: no media source factory for kind "local_file"
```

浏览器 Network 面板：

```
GET /v1/flv/<nodeID>/<channelID> 404 (Not Found)
[IOController] > Loader error, code = 404
```

### 根因分析

1. `ChannelListView.vue:93` 上传成功后调用 `api.putChannelMedia(nodeId, ch, { kind: 'local_file', path })`
2. `ChannelListView.vue:176` `parseMediaInput()` 将 `file://` 或绝对路径映射为 `kind: 'local_file'`
3. `MediaPanel.vue` 使用 `kind: 'file'`，与后端一致
4. 后端 `MediaSourceFactory` (main.go:405) 仅注册了 `file`/`rtsp`/`hls`/`synthetic` 四种
5. `MediaConfig.Normalize()` 未处理 `local_file` 别名，导致 `Kind="local_file"` 原样进入 `MediaService.Subscribe()`
6. 工厂方法返回 `nil` → "no media source factory for kind" → HTTP 404

### 影响范围

- 前端通过通道列表上传文件后自动绑定的媒体源无法播放
- 直接使用 `MediaPanel` 手动填写 kind 为 `file` 的通道不受影响
- RTSP/HLS/Synthetic 媒体源不受影响

## Goals

- [ ] 后端接受并正确处理 `local_file` kind（折算为 `file`）
- [ ] 已有数据库中存储的 `local_file` 记录可正常播放
- [ ] 不破坏现有 `file`/`rtsp`/`hls`/`synthetic` 的行为
- [ ] 补充单元测试覆盖归一化逻辑

## Non-Goals

- 不修改前端代码（别名是合理的可读命名）
- 不引入新的 SourceKind
- 不改变 MediaPanel 的 `file` 行为

## Approach

在 `internal/domain/model/media.go` 的 `MediaConfig.Normalize()` 方法中，将 `Kind == "local_file"` 的值替换为 `SourceKindFile ("file")`。理由：

1. **语义一致**：`local_file` 是 `file` 的人类可读别名（相对 "remote_file" 而言），语义完全重叠
2. **最小改动**：无需新增 SourceKind，无需修改前端，无需改动工厂注册
3. **集中归一化**：`Normalize()` 已在 `SetMedia()` / `SetChannelMedia()` / 数据库恢复等所有入口处被调用，一次修改全部生效
4. **OpenSpec 对齐**：设计文档（channel-web-ui/spec.md）已明确说明上传后绑定 `{kind:"local_file"}`，归一化是该设计的实现补全

## Risks

- **低**：修改仅涉及字符串比较和赋值，零外部依赖
- **回归风险**：无。`file`/`rtsp`/`hls`/`synthetic` 均不触发此分支
