# node-lifecycle-controls 规范

## Purpose

为三种节点身份（device / platform-small / platform-large）提供统一的生命周期可视化与控制能力：节点卡片按当前状态与身份渲染「启动/重试」「停止」「注销」操作按钮，通过定时轮询呈现状态推进过程，并支持进程启动时按 `auto_start` 配置自动启动全部节点，消除"节点永远停在 idle 且无处启动"的运维断层。

## Requirements

### Requirement: 节点卡片操作按钮

系统 MUST 在 Web 管理界面的节点列表中为每个节点卡片渲染生命周期操作按钮，按钮的显隐 MUST 依据节点当前状态决定：

- `idle` 或 `offline` 状态的节点 MUST 显示「启动」按钮；
- `fault` 状态的节点 MUST 显示「重试」按钮（点击等价于再次启动）；
- `registering`、`registered` 或 `online` 状态的节点 MUST 显示「停止」按钮；
- `registering` 状态的节点 MUST NOT 显示「启动」按钮（避免并发重复启动）。

操作结果 MUST 通过按钮 loading 态与消息提示反馈用户；启动、停止或注销失败时 MUST 展示后端返回的错误信息与失败阶段（若有）。

#### Scenario: 对 idle 节点点击启动

- **WHEN** 用户在 `idle` 状态的节点卡片上点击「启动」按钮
- **THEN** 前端调用该节点的启动端点，按钮进入 loading 态直至请求返回，成功后节点状态刷新为 `registering`（或注册完成后的后续状态）

#### Scenario: 对 fault 节点点击重试

- **WHEN** 用户在 `fault` 状态的节点卡片上点击「重试」按钮
- **THEN** 前端调用与「启动」相同的端点，节点从 `fault` 经 `idle` 推进到 `registering`，无需额外的重置端点

#### Scenario: 注销失败展示阶段信息

- **WHEN** 用户点击「注销」按钮且后端注册事务在某个阶段失败
- **THEN** 前端展示错误消息，内容包含后端返回的错误描述与失败阶段名称（如 `challenge`、`timeout`）

### Requirement: 注销按钮按身份显隐

「注销」按钮 MUST 仅对具备上游注册关系的节点显示：`device` 或 `platform-small` 节点在 `online` 状态且配置了上游注册时显示「注销」；`platform-large` 节点 MUST NOT 显示「注销」按钮，因为大平台是被注册方、不存在上游注册关系。

#### Scenario: 大平台节点不显示注销按钮

- **WHEN** 用户查看一个 `platform-large` 节点卡片的操作栏
- **THEN** 卡片不渲染「注销」按钮，无论其处于何种状态

#### Scenario: 在线设备节点显示注销按钮

- **WHEN** 一个配置了上游注册的 `device` 节点处于 `online` 状态
- **THEN** 该节点卡片渲染「注销」按钮，点击后向上级发送 `Expires: 0` 的注销请求

### Requirement: 节点状态轮询

节点列表页面 MUST 以不超过 5 秒的间隔定时重新获取节点列表，使节点状态的推进（注册进度、在线、离线、故障）在不刷新页面的情况下对用户可见。轮询 MUST 在组件卸载时停止。

#### Scenario: 状态变化自动呈现

- **WHEN** 某节点的状态在后端由 `registering` 推进为 `online`，用户停留在节点列表页面
- **THEN** 在下一个轮询周期内（不超过 5 秒），页面上的该节点状态标签自动更新为 `online`，无需手动刷新

#### Scenario: 组件卸载停止轮询

- **WHEN** 用户从节点列表页面导航离开
- **THEN** 定时轮询停止，不再向后端发起节点列表请求

### Requirement: 节点状态标签配色

节点状态标签 MUST 为六种状态提供可区分的视觉样式：`online` 显示为成功色，`fault` 显示为危险色，`registering` 显示为提示色（或加载指示），`registered` 显示为警告色，`idle` 与 `offline` 显示为默认中性色。六种状态的标签 MUST 两两可区分。

#### Scenario: 启动过程中的状态可辨识

- **WHEN** 用户对节点执行启动操作并观察其状态标签的变化
- **THEN** 标签依次呈现 `registering`（提示/加载样式）、`registered`（警告样式）至 `online`（成功样式），各阶段视觉样式互不相同

### Requirement: auto_start 配置项

系统 MUST 接受 YAML 配置中的顶层布尔字段 `auto_start`（默认 `false`）。当值为 `true` 时，进程 MUST 在完成全部节点注册与持久化状态恢复后，按配置顺序对每个节点执行启动操作；启动失败的单个节点 MUST 进入 `fault` 状态并记录日志，MUST NOT 阻止其余节点的启动。当值为 `false` 或字段缺省时，MUST 保持现有行为：节点仅被注册、状态为 `idle`，启动是显式操作。

#### Scenario: auto_start 为 true 时进程启动即推进节点

- **WHEN** 配置文件中 `auto_start: true` 且 `nodes:` 列表包含三个节点，进程启动完成
- **THEN** 三个节点均已被执行启动操作；配置了上游注册且上级可达的节点最终进入 `online` 状态

#### Scenario: auto_start 为 true 时单个节点失败不影响其他节点

- **WHEN** 配置文件中 `auto_start: true`，其中节点 A 的信令端口被占用导致启动失败
- **THEN** 节点 A 进入 `fault` 状态并记录警告日志，节点 B、C 的启动不受影响

#### Scenario: auto_start 缺省保持显式启动语义

- **WHEN** 配置文件中未出现 `auto_start` 字段，进程启动完成
- **THEN** 全部节点处于 `idle` 状态，等待显式启动（HTTP 端点或 Web 按钮）
