# Design: local_file kind alias normalization

## Context

前端上传文件后自动绑定的 kind 是 `local_file`，是 `file` 的人类可读别名（OpenSpec `channel-web-ui/spec.md` 设计文档已约定该值）。后端 MediaSourceFactory 在 `cmd/gb28181-simulator/main.go:405` 只注册了 `file`/`rtsp`/`hls`/`synthetic` 四个分支，导致上传绑定的媒体无法播放。

## Decision: 在 `MediaConfig.Normalize()` 层归一化

**将 `local_file` 替换为 `file` 的职责放在 `internal/domain/model/media.go` 的 `Normalize()` 方法**。

### Rationale

| 方案 | 优点 | 缺点 |
|------|------|------|
| 在 `MediaConfig.Validate()` 层做 alias | 写入即修正，清晰 | Validate 语义是"校验"，应报告错误而非静默改写 |
| 在 `handlePutMedia` / `handlePutChannelMedia` handler 层做 alias | 入口明确 | 无法覆盖数据库恢复路径；两处修改，可遗漏 |
| 在 `MediaConfig.Normalize()` 层做 alias（**已采用**） | 所有写入路径都已调用 Normalize，单点一次修复，覆盖运行时 + 持久化恢复 | 正常行为无副作用，最小改动 |

### 实现

**文件**：`internal/domain/model/media.go`

```go
func (c MediaConfig) Normalize() MediaConfig {
    if c.Kind == "local_file" {
        c.Kind = SourceKindFile
    }
    if c.MTU <= 0 {
        c.MTU = 1400
    }
    if c.FPS <= 0 {
        c.FPS = 25
    }
    if c.Clock == 0 {
        c.Clock = 90000
    }
    return c
}
```

**调用路径**（均调用 `cfg.Normalize()`，一次修改全面生效）：

1. `NodeService.SetMedia()` → `node_service.go:867`
2. `NodeService.SetChannelMedia()` → `node_service.go:917`
3. 数据库恢复（Bootstrap → restoreNodeMedia）→ `node_service.go:1049`

### 测试

**文件**：`internal/domain/model/media_test.go`

新增 `TestMediaConfigNormalizeLocalFileAlias`：
- 验证 `Kind: "local_file"` 归一化后为 `"file"`
- 验证 `Path` 保持原值
- 验证归一化后可通过 `Validate()` 且工厂可打开

### 部署

Docker 二进制构建，`Dockerfile.binary` 从 `binary/` 目录 COPY 新版本，`docker compose build && docker compose up -d` 完成滚动升级。

## Alternatives Considered

### 方案：前端改为使用 `file`

`ChannelListView.vue` 和 `parseMediaInput()` 中把所有 `local_file` 改为 `file`。

- 已否决：`local_file` 是 OpenSpec 设计的约定（`channel-web-ui/spec.md`），前端保持可读别名是合理的；只改一端导致规范倒挂。
