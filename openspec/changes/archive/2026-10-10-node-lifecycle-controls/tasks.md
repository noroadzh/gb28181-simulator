# Tasks

## 1. 后端配置与自动启动

- [x] 1.1 在 `internal/platform/config/config.go` 的 `Config` 结构体新增顶层字段 `AutoStart bool \`mapstructure:"auto_start"\``（缺省 false，Go 零值即 false，无需改 Defaults）。验证：`go test ./internal/platform/config/` 仍通过，且 `viper.Unmarshal` 可识别 `auto_start: true`。
- [x] 1.2 在 `cmd/gb28181-simulator/main.go` 组合根的节点恢复逻辑之后（`RestorePersistedState` 循环结束），按 `cfg.AutoStart` 决定是否自动启动：若为 true，遍历 `nodeService.List()` 对每个节点调用 `nodeService.Start`，单个失败记 Warn 日志并继续下一个，MUST NOT 阻止后续节点。验证：配置 `auto_start: true` 启动进程后，节点列表 API 中全部节点状态推进到在线或故障，不会停留在 idle。

## 2. 修正无 registration 的 device 节点状态语义

- [x] 2.1 修改 `internal/app/node_service.go` `Start()` 方法中 `NodeKindDevice` 且 `!wants` 的分支：从 `return nil` 改为 `return s.advanceOnline(ctx, id)`。验证：`go test ./internal/app/...` 中设备节点无 registration 启动的用例期望从 `registering` 更新为 `online`，全部测试通过。
- [x] 2.2 补充/更新 `internal/app/node_service_registration_test.go` 或新建用例，覆盖"device 无 registration 启动后状态为 online"。验证：`go test ./internal/app/... -run TestDeviceNoRegistration -v` 输出包含 `online`。

## 3. 后端 nodeResponse 暴露 has_registration

- [x] 3.1 在 `internal/interface/http/nodes.go` 的 `nodeResponse` 新增 `HasRegistration bool \`json:"has_registration"\``，`newNodeResponse` 中由 `n.Registration()` 的 ok 值填充（`model.Node` 接口已含该方法，无需改 domain）。验证：`go test ./internal/interface/http/...` 通过，且新增断言：配 registration 的节点响应 `has_registration: true`，未配的为 `false`。

## 4. 前端节点列表：轮询 + 状态配色 + 操作按钮

- [x] 4.1 修改 `web/src/views/NodesView.vue`：
  - 在 `<script setup>` 引入 `onBeforeUnmount`，使用 `setInterval` 每 4 秒调用 `refresh()`，并在 `onBeforeUnmount` 中 `clearInterval`。验证：`npm run build` 成功，浏览器中停留页面 10 秒以上节点状态不因手动后端变化而保持陈旧。
- [x] 4.2 在 `NodesView.vue` 模板中扩展 `statusType` 函数，区分 `online`/`fault`/`registering`/`registered`/`idle`/`offline` 六种状态标签样式。验证：`npm run build` 成功，视觉呈现六态可区分（参考设计 D6）。
- [x] 4.3 在 `NodesView.vue` 的节点卡片底部新增操作按钮行：依据 `node.status` 与 `node.kind` 显示「启动/重试」「停止」「注销」按钮，并分别调用 `api.startNode(node.id)`、`api.stopNode(node.id)`、`api.unregisterNode(node.id)`。操作期间按钮 loading，成功后 `refresh()` 重新拉取；失败则 `ElMessage.error` 展示后端错误。验证：对 idle 设备节点点击启动后状态刷新为 registering/online；对 fault 节点点击重试可恢复；对 online 节点点击停止后进入 offline。
- [x] 4.4 注销按钮按三条件显隐（参考设计 D5）：`('device' || 'platform-small') && status === 'online' && has_registration === true` 才渲染；`platform-large` 与无上游注册的节点一律不渲染。验证：无 registration 的 device 节点（按任务 2 在线）不出现注销按钮。

## 5. 前端 API 调用

- [x] 5.1 确认 `web/src/api.js` 已提供 `startNode`、`stopNode`、`unregisterNode` 方法（现成可用）。若方法签名与 4.3 一致，无需修改；若存在差异，补齐并验证前端类型检查无警告。验证：`npm run lint` 通过。

## 6. 更新配置示例与文档

- [x] 6.1 在项目根目录 `config.yaml.example` 或 README 中的 YAML 示例增加 `# auto_start: false` 注释说明。验证：示例文件存在且注释完整。
- [x] 6.2 在 `web/src/views/` 中若有其他引用节点状态配色的文件，同步更新（如 ChannelListView 顶部状态标签）。验证：`npm run build` 成功。

## 7. 回归验证

- [x] 7.1 执行 `go test ./...` 确保 Go 层全部通过。
- [x] 7.2 执行 `make web` 确保前端构建产物写入 `internal/interface/webui/embed/dist/` 且无构建错误。
- [x] 7.3 手动冒烟：以默认配置（auto_start: false）启动，验证节点停留在 idle；以 auto_start: true 启动，验证设备无 registration 的节点进入 online；在 Web 界面依次点击启动/停止/重试/注销，验证各状态与按钮显隐符合 spec。
