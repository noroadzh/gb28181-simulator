# Design

## Architecture Overview

本次改动为纯前端 UX 修复 + 后端 1 个接口的约束放宽。不涉及分层架构调整，不引入新的依赖或服务。所有改动在现有 `internal/interface/http`、`web/src/views`、`web/src/router` 范围内进行。

## Component Changes

### 1. Router Redirect (Frontend)

**File**: `web/src/router/index.js`

**Change**: 将根路径重定向从 `/nodes` 改为 `/dashboard`，使用户打开页面即看到仪表盘。

```javascript
{ path: '/', redirect: '/dashboard' }
```

**Rationale**: 当前默认页是节点概览，用户希望打开即看到仪表盘（系统状态概览、统计卡片、通道列表、日志）。

### 2. Account List Field Name Fix (Frontend)

**File**: `web/src/views/AccountsView.vue`

**Change**: 将表格列 `prop="Username"` 与 `prop="CreatedAt"` 改为 `prop="username"` 与 `prop="created_at"`。

**Rationale**: 后端通过 `port.AccountInfo` 结构体返回 JSON，Go JSON 序列化使用结构体字段名作为 key：
```go
type AccountInfo struct {
    Username  string
    CreatedAt string
}
```
默认情况下 Go 序列化为 `Username` 与 `CreatedAt`（首字母大写），但 Element Plus 的 `el-table-column` 通过 `prop` 字段映射数据时区分大小写不敏感，前端写的是 PascalCase，而后端由于标签（如 `json:"username"`）的配置可能返回小写字段名，导致字段值为 undefined。

**Fix**: 统一使用小写 `username` 与 `created_at`，与 Go 结构体标签保持一致。

### 3. Empty Password Support (Backend)

**File**: `internal/interface/http/accounts.go`

**Change**: 移除 `handleAccountAdd` 与 `handleAccountSetPassword` 中的密码非空校验。

```go
// Before
if req.Username == "" || req.Password == "" {
    return c.JSON(http.StatusBadRequest, errorBody{Error: "username and password are required"})
}

// After
if req.Username == "" {
    return c.JSON(http.StatusBadRequest, errorBody{Error: "username is required"})
}
```

**Rationale**: GB/T 28181 标准允许设备以空密码注册。某些部署场景（内网测试、演示环境）下空密码是合法配置。当前硬校验会拒绝这类注册请求，影响真实使用场景。

**Impact**: 
- 接口约束放宽为向后兼容扩展（原本密码非空时仍正常工作）
- SQLite 存储层无需修改（已支持空字符串）
- CredentialStore.Lookup 返回 `Credentials{Username, Realm, Password}`，password 字段为空字符串时 acceptor 会按空密码处理 Digest 认证

### 4. Play Promise Race Fix (Frontend)

**File**: `web/src/views/ChannelDetailView.vue`

**Change**: 改用 flv.js `METADATA_PARSED` 事件触发 `play()` 调用，避免 `load()` 与 `play()` 之间的竞态。

**Pattern**:
```javascript
// Before
player.load()
player.play().then(...).catch(...) // ← 中断风险

// After
player.load()
player.on(flvjs.Events.METADATA_PARSED, () => {
    player.play().then(...).catch(...)
})
```

**Rationale**: 
- `player.play()` 返回 Promise
- 在 `load()` 完成前调用 `play()` 会导致浏览器返回 `AbortError`（因为 `<video>` 元素尚未就绪）
- 通过 `METADATA_PARSED` 事件等待媒体元数据解析完成后再触发 `play()`，避免 Promise 中断
- `toggleFlv()` 中需要先 `await player.play()` 完成再调用 `pause()`，或使用 `player.pause()` 同步调用

### 5. Upload Entry Direct Access (Frontend)

**File**: `web/src/views/ChannelListView.vue`

**Change**: 将"媒体源"按钮从嵌套下拉菜单拆分为两个独立按钮：
- "媒体源"（手动输入 URL）
- "上传"（直接上传文件）

**Rationale**: 当前下拉菜单嵌套过深，用户需要点两次才能进入上传对话框，操作路径不直观。改为独立按钮后，用户一次点击即可到达上传页面。

### 6. Device Node Accounts Page Warning (Frontend)

**File**: `web/src/views/AccountsView.vue`

**Change**: 当选中的节点不是 platform 类型时，显示明确的 el-alert 警告。

```html
<el-alert
  v-if="nodeId && !isPlatform"
  type="warning"
  title="账号管理仅适用于 platform 类型节点，当前节点类型为 device"
  :closable="false"
/>
```

**Rationale**: 
- Device 进程没有 platform 节点，打开账号页会看到空表格，用户容易困惑
- 通过警告信息明确告知用户当前节点类型不支持账号管理

## Data Flow

### Empty Password Registration

```
Browser ──POST /v1/platforms/:id/accounts──▶ accounts.go handler
                                              ↓
                                          s.accounts.AddAccount(nodeID, username, "")
                                              ↓
                                          SQLite INSERT with empty password
                                              ↓
                                          Acceptor Lookup returns Credentials{Password: ""}
                                              ↓
                                          Device REGISTER with empty password
                                              ↓
                                          ✓ Authentication succeeds
```

## Testing Strategy

- **后端**：现有 `accounts.go` 测试覆盖空密码场景（新增 `TestAddAccount_EmptyPassword` 测试用例）
- **前端**：手动验证播放中断修复（上传本地 mp4 文件，播放成功无报错）
- **集成**：通过 Web UI 验证账号列表显示正确（无 undefined 字符串）

## Migration & Compatibility

- **API 兼容性**：`POST /v1/platforms/:id/accounts` 与 `PUT .../password` 放宽 password 约束为向后兼容扩展
- **数据库**：无需 schema 变更
- **前端**：路由默认页变更会影响所有用户，但可通过浏览器返回键回到旧默认页（不构成 breaking change）
