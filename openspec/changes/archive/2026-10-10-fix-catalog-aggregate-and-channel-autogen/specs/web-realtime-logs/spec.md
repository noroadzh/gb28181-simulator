# web-realtime-logs Delta

## ADDED Requirements

### Requirement: Web 界面实时日志查看页面

系统 MUST 在 Web 管理界面提供实时日志查看页面（路由 `/logs`），并在侧边栏提供「实时日志」菜单入口。页面 MUST 通过既有 WebSocket 端点 `GET /v1/logs/stream` 订阅日志流，无需登录服务器即可查看运行日志。

#### Scenario: 侧边栏入口可跳转

- **WHEN** 用户在 Web 管理界面查看侧边栏菜单
- **THEN** 存在「实时日志」菜单项，点击后路由切换到 `/logs`
- **AND** 页面展示日志流内容

### Requirement: 日志级别筛选

实时日志页面 MUST 支持按级别（`trace` / `debug` / `info` / `warn` / `error`）多选筛选。未选中任何级别时 MUST 显示为空列表而非全部记录。

#### Scenario: 按级别过滤

- **WHEN** 用户在级别多选中仅勾选 `warn` 与 `error`
- **THEN** 列表仅展示级别为 warn 或 error 的记录
- **AND** trace / debug / info 级别的记录被过滤掉

### Requirement: 关键字过滤

实时日志页面 MUST 支持关键字输入框，对日志消息文本进行包含匹配过滤。关键字变化时 MUST 立即对后续新到的记录生效（对已展示的历史记录可选择性生效）。

#### Scenario: 关键字过滤

- **WHEN** 用户在关键字输入框输入 `REGISTER`
- **THEN** 新到的日志记录中，仅消息文本包含 `REGISTER` 的记录被展示

### Requirement: 暂停与恢复

实时日志页面 MUST 提供「暂停 / 继续」按钮。暂停期间 MUST 停止向列表追加新记录（WebSocket 可保持连接、后台丢弃或恢复后跳过，实现自选），恢复后 MUST 继续追加新记录。

#### Scenario: 暂停期间不追加

- **WHEN** 用户点击「暂停」按钮
- **AND** 后端继续产生日志
- **THEN** 列表内容保持不变

#### Scenario: 恢复后继续追加

- **WHEN** 用户在暂停状态下点击「继续」按钮
- **THEN** 新产生的日志记录恢复追加到列表尾部

### Requirement: 内存容量上限

实时日志页面 MUST 将内存中保留的日志条数限制在可配置上限内（默认 1000 条）。超过上限时 MUST 从头部丢弃最旧的记录，保证页面长时间运行不因日志累积导致内存膨胀或渲染卡顿。

#### Scenario: 超过上限丢弃最旧记录

- **WHEN** 页面已展示 1000 条记录且新记录持续到达
- **THEN** 列表长度保持 1000 条，最早的记录被移除
- **AND** 页面无卡顿或内存持续增长

### Requirement: 自动滚动

实时日志页面 MUST 提供「自动滚动」开关。开启时，新记录追加后视口 MUST 自动滚动到底部；关闭时，视口位置 MUST 保持不变，便于用户阅读历史日志。

#### Scenario: 开启自动滚动跟随到底部

- **WHEN** 自动滚动开关开启且新日志到达
- **THEN** 视口滚动到列表底部，最新记录可见

#### Scenario: 关闭自动滚动保持位置

- **WHEN** 自动滚动开关关闭且新日志到达
- **THEN** 视口位置保持不变，用户正在阅读的历史记录不跳动

### Requirement: 连接状态与断线重连提示

实时日志页面 MUST 展示当前 WebSocket 连接状态（已连接 / 已断开 / 连接中）。连接断开时 MUST 给出可见提示，且 SHOULD 支持用户手动或自动重连。

#### Scenario: 断线提示

- **WHEN** WebSocket 连接意外断开
- **THEN** 页面顶部或角落出现「连接已断开」状态提示
- **AND** 用户可点击重连按钮或等待自动重连恢复日志流
