# Tasks

## 1. Purpose 补全

- [x] 1.1 `channel-id-auto-gen`：写入真实 Purpose（自动生成 20 位通道 ID 与 `auto_generated` 标记）；验收：占位符消失且与需求一致
- [x] 1.2 `media-loop-validation`：写入真实 Purpose（loop 仅对本地文件源生效）；验收：同上
- [x] 1.3 `platform-large-node-catalog-aggregate`：写入真实 Purpose（聚合自身与下游通道的目录应答）；验收：同上
- [x] 1.4 `web-realtime-logs`：写入真实 Purpose（WebSocket 实时日志与筛选）；验收：同上
- [x] 1.5 `openspec validate --specs` 不再出现 Purpose 占位符警告；验收：警告数为 0
