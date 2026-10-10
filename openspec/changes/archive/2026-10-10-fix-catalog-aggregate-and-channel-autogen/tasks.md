# Tasks

## Implementation

- [x] 1. `internal/app/acceptor.go` — `answerCatalog` 聚合下游设备关联节点的 channels（`ParentID = 设备ID`），排序、`SumNum` 累加
- [x] 2. `internal/app/acceptor.go` — 注册失败/成功日志补充 `client_ip` / `transport` / `expires` / 失败原因
- [x] 3. `internal/interface/http/channels.go` — `handleChannelAdd` 当 `channel_id` 为空时自动生成 20 位国标编号（`generateChannelID`），响应新增 `auto_generated` 字段
- [x] 4. `internal/app/node_service.go` — `SetMedia` / `SetChannelMedia` 增加 `loop=true` 且非 `file` 类型的 400 校验
- [x] 5. `web/src/views/LogView.vue` — 新建实时日志页面（WebSocket 订阅、级别筛选、关键字过滤、暂停/继续、自动滚动、1000 条上限）
- [x] 6. `web/src/router/index.js` — 注册 `/logs` 路由
- [x] 7. `web/src/App.vue` — 侧边栏新增「实时日志」菜单项
- [x] 8. `web/src/views/ChannelListView.vue` — 通道 ID 输入框 placeholder 提示「留空自动生成」，保存后回填
- [x] 9. `web/src/views/MediaPanel.vue` — loop 开关在非 file 类型时禁用 + tooltip 提示

## Verification

- [x] 10. 后端单元测试 — `go test ./internal/app/... ./internal/interface/http/...` 通过，覆盖 Catalog 聚合、ID 自动生成、loop 校验
- [x] 11. 前端构建 — `cd web && npm run build` 成功，产物嵌入 Go 二进制
- [x] 12. 后端整体构建 — `go build ./...` 通过
- [x] 13. openspec 严格校验 — `openspec validate "fix-catalog-aggregate-and-channel-autogen" --strict` 无错误

## Deployment

- [x] 14. 交叉编译 Linux/amd64 二进制并 rsync 到 `10.96.1.125:/slow2/gb28181-simulator/binary/`
- [x] 15. 服务器上 `docker build -f Dockerfile.binary` 重建镜像、`docker compose up -d` 重启
- [x] 16. 健康验证 — `docker ps` 容器 healthy、`curl http://10.96.1.125:18080/healthz` 返回 ok

## OpenSpec Archival

- [ ] 17. 归档 change — `openspec archive "fix-catalog-aggregate-and-channel-autogen" --yes`，delta spec 同步到 `openspec/specs/`
- [ ] 18. 更新 `docs/roadmap-15-steps.md` — 追加新行并修正 #18/#19 的状态脱节
