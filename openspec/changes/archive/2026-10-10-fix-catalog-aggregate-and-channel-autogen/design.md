# Design

## 1. Catalog 聚合下游通道

### 1.1 现状

`internal/app/acceptor.go` 的 `answerCatalog`：

```go
// 现状（伪代码）
items := []CatalogItem{}
for _, d := range a.devices.List(ctx, p.id) {
    items = append(items, NewCatalogItemFromDevice(d))
}
if a.registry != nil {
    if node, ok := a.registry.Get(ctx, p.id); ok {
        for _, ch := range node.Profile().Channels() {
            items = append(items, catalogItem(ch))
        }
    }
}
```

`DownstreamDevice` 没有 channels 字段，平台 profile channels 与设备服务 channels 完全脱钩。第三方拉取时看到的是 1 条设备记录，丢失 7 个通道。

### 1.2 新设计

```go
items := []CatalogItem{}
for _, d := range a.devices.List(ctx, p.id) {
    items = append(items, NewCatalogItemFromDevice(d))
    // 新增：聚合该设备关联节点的通道
    if a.registry != nil {
        if childNode, ok := a.registry.Get(ctx, d.DeviceID); ok {
            channels := childNode.Profile().Channels()
            sort.Slice(channels, func(i, j int) bool {
                return channels[i].ID < channels[j].ID
            })
            for _, ch := range channels {
                items = append(items, NewCatalogItemFromChannel(ch, parentID=d.DeviceID))
            }
        }
    }
}
// 平台自身 profile 中的 channels（原有逻辑）
if a.registry != nil {
    if node, ok := a.registry.Get(ctx, p.id); ok {
        for _, ch := range node.Profile().Channels() {
            items = append(items, catalogItem(ch))
        }
    }
}
```

**字段映射**（通道作为目录项）：

| 通道字段 | 目录项字段 | 说明 |
|---|---|---|
| `Channel.ID` | `DeviceID` | 国标 20 位通道编号 |
| `Channel.Name` | `Name` | 通道名称 |
| `Channel.Status` | `Status` | `ON` / `OFF` |
| `Channel.ParentalID` | `ParentID` | 所属设备 ID |
| `Channel.Manufacturer` | `Manufacturer` | 取节点 vendor |
| `Channel.Model` | `Model` | 固定为 `Channel` |
| `d.DeviceID` | `ParentID` | 父设备 ID |
| 前 6 位行政区码 | `CivilCode` | 从 channel.ID 前 6 位截取 |

**排序**：先按 `ParentID` 升序，再按 `DeviceID` 升序，确保第三方拿到稳定顺序。

**容错**：`childNode` 不存在时（设备注册但节点未在本平台实例上），只返回设备记录，不报错；这一场景多见于多实例集群，但本平台单机版也会出现「注册成功但 Profile 通道为空」的情况。

### 1.3 注册失败可观测

`internal/app/acceptor.go` 中处理 `Register` 请求的分支：

- 401（密码错）：在 warn 日志中增加 `client_ip` / `transport` / `expected_user` / `actual_user`（脱敏），便于排障。
- 404（未知账号）：info 日志携带 `client_ip` / `requested_user`。
- 成功：info 日志携带 `client_ip` / `device_id` / `transport` / `expires`。

字段通过 `slog.Logger.With("component", "internal/app", "subsystem", "sip_acceptor")` 注入，与现有日志体系保持一致。

## 2. 通道 ID 自动生成

### 2.1 规则

GB/T 28181 20 位 ID 规范：`6 位行政区域码 + 2 位行业码 + 2 位类型码（设备/通道） + 4 位序号`。本实现采用简化版：

- 行政区域码：从 `node.ID` 前 6 位截取（如 `34020000001310000001` → `340200`）。
- 行业码：固定 `00`。
- 类型码：固定 `13`（通道）。
- 序号：6 位十进制，从当前节点已有通道数 `+1` 开始，冲突则 `+1` 重试，最多 100 次。

格式：`{region}{industry}{type}{seq:06d}` → 共 20 位。

例：节点 `34020000001310000001` 新增第一个通道 → `340200 00 13 000001` = `3402000013000001`（修正为 18 位）。为保持 20 位，实际规则：

- 行政区域码 6 位 + 行业码 2 位 + 类型码 2 位 + 序号 6 位 + 通道标识 4 位 → 不行，总长 20。

为简化为 20 位且兼容已有 6 位区域码约定，本实现采用：

```
{region6}{industry2}{type2}{seq4}  // 共 14 位，前补 region6 重新拼接：实际生成规则
```

最终采用「**保持 20 位国标格式**」：行政区域码 6 + 行业码 2 + 类型码 2 + 序列号 10 = 20 位。

```go
// internal/interface/http/channels.go
func generateChannelID(nodeID string, existing []model.Channel) string {
    region := nodeID
    if len(region) >= 6 {
        region = region[:6]
    }
    for i := 1; i <= 100; i++ {
        seq := fmt.Sprintf("%010d", i)
        candidate := region + "00" + "13" + seq // 总 20 位
        // 检查是否已存在
        taken := false
        for _, ch := range existing {
            if ch.ID == candidate {
                taken = true
                break
            }
        }
        if !taken {
            return candidate
        }
    }
    // 极端兜底：使用纳秒时间戳后缀
    return region + "00" + "13" + fmt.Sprintf("%010d", time.Now().UnixNano()%10000000000)
}
```

`existing` 来源：`a.nodeService.ListChannels(ctx, nodeID)`。

### 2.2 API 契约变更

**请求**（`POST /v1/nodes/:id/channels`）：

```json
{ "name": "Cam-1", "status": "ON" }  // 旧：必须带 channel_id
```

**响应（201 Created）**：

```json
{
  "id": "34020000130000000001",
  "name": "Cam-1",
  "parent_id": "34020000001310000001",
  "status": "ON",
  "auto_generated": true  // 新增字段，前端可提示
}
```

若请求体显式带 `channel_id`，仍然优先使用，并在响应中设 `auto_generated: false`。

## 3. Loop 类型校验

### 3.1 校验位置

`internal/app/node_service.go` 的 `SetMedia` 与 `SetChannelMedia`：

```go
func (s *NodeService) SetMedia(ctx context.Context, nodeID string, cfg model.MediaConfig) error {
    if cfg.Loop && cfg.Kind != "file" {
        return fmt.Errorf("loop is only supported for file sources, got kind=%q", cfg.Kind)
    }
    // ... 原逻辑
}
```

`SetChannelMedia` 同步处理。

### 3.2 API 契约

`PUT /v1/nodes/:id/media` body：

```json
{ "kind": "rtsp", "path": "rtsp://x", "loop": true }
```

→ `400 Bad Request`：

```json
{ "error": "loop is only supported for file sources, got kind=\"rtsp\"" }
```

前端 `MediaPanel.vue`：当 `kind != "file"` 时 `loop` 开关 disabled，并 tooltip 提示「仅 file 类型支持循环播放」。

## 4. Web 实时日志页面

### 4.1 路由

```js
// web/src/router/index.js
{
  path: '/logs',
  name: 'logs',
  component: () => import('@/views/LogView.vue'),
  meta: { title: '实时日志', icon: 'Document' }
}
```

### 4.2 页面布局

- 顶部工具栏：级别多选（trace/debug/info/warn/error）、关键字输入、自动滚动开关、清屏按钮、暂停/继续按钮、连接状态指示器。
- 主区域：虚拟滚动列表（`el-virtual-list` 或简单的 `overflow:auto` + 数组截断），每行展示时间戳、级别（带颜色）、component、subsystem、message。
- 容量上限：内存中最多保留 1000 条记录，超过后从头部丢弃；用户暂停时停止追加，恢复后继续追加。

### 4.3 数据流

```js
// web/src/views/LogView.vue (核心逻辑)
const MAX_LINES = 1000
const lines = ref([])
const paused = ref(false)
const autoScroll = ref(true)
const filters = ref({ levels: ['info','warn','error'], keyword: '' })

let ws = null
onMounted(() => {
  ws = api.logsStream(msg => {
    if (paused.value) return
    if (!filters.value.levels.includes(msg.level)) return
    if (filters.value.keyword && !msg.msg.includes(filters.value.keyword)) return
    lines.value.push(msg)
    if (lines.value.length > MAX_LINES) lines.value.splice(0, lines.value.length - MAX_LINES)
    if (autoScroll.value) nextTick(() => { containerRef.value.scrollTop = containerRef.value.scrollHeight })
  })
})
onUnmounted(() => { ws?.close() })
```

### 4.4 侧边栏入口

`web/src/App.vue` 的菜单数组新增：

```js
{ index: '/logs', title: '实时日志', icon: 'Document' }
```

## 5. 部署

`docs/deploy.md`（若不存在则新建）追加：

- 通过 `./scripts/release.sh`（如不存在则用 makefile `service-build`）构建 Linux/amd64 二进制。
- rsync 到服务器 `/slow2/gb28181-simulator/binary/gb28181-simulator-linux-amd64`。
- `ssh root@10.96.1.125 "cd /slow2/gb28181-simulator && ./deploy.sh"` 重建并重启容器。
- 验证：`docker ps` 看容器 healthy，`docker logs gbsim-platform | tail -20` 看启动日志。
