# 任务

## Phase 1：基础框架 + 节点管理 + 抓包面板

- [x] 1.1 初始化 `web/` 前端目录：Vue3 + TypeScript + Element Plus + Vite + Pinia + Vue Router（Hash 模式），实现侧边栏 + 面包屑的主布局壳，产出 `web/dist`（先不含功能页面）。验证：`cd web && npm run build` 退出码 0，`web/dist/index.html` 存在
- [x] 1.2 在 `cmd/gb28181-simulator/main.go` 中通过 `//go:embed web/dist/*` 挂载 `/web/` 静态文件服务；`config.yaml` 新增 `web.enabled` 开关（默认开启）。验证：启动模拟器后 `curl http://127.0.0.1:8080/web/` 返回 index.html
- [x] 1.3 实现节点概览页：卡片网格展示 `GET /v1/nodes` 返回的节点（ID/类型/地址/状态/故障计数），点击进入详情；顶部提供刷新按钮与类型筛选。验证：浏览器打开 `/web/#/nodes` 可见至少 1 个节点卡片且数据与 API 一致
- [x] 1.4 实现抓包面板页：选择节点后轮询 `GET /v1/nodes/:id/capture?limit=50`，表格展示方向/时间戳/端点/大小，点击行展开 hexdump 预览；提供 `GET /v1/nodes/:id/capture.pcap` 下载按钮。验证：设备注册后打开抓包面板可见 REGISTER 与 200 OK 事件，pcap 下载后 `file` 命令识别为 pcap

## Phase 2：故障注入面板

- [x] 2.1 实现故障注入页：表单编辑 canned（方法→状态码映射）、delay（base/jitter）、drop（概率）、blackhole（方法列表）、unsupported-method（501 开关），提交到 `POST /v1/nodes/:id/faults`；展示当前 profile 与计数；`DELETE /v1/nodes/:id/faults` 清除按钮。验证：通过面板安装 canned 403 后触发设备注册可见 fault 计数上升，清除后计数归零
- [x] 2.2 端到端联调：面板安装 blackhole keepalive 后，设备节点在 HeartbeatMaxFailures 内进入 `StatusFault`，面板实时反映状态变化。验证：手动浏览器操作与 `siptest` 中既有 e2e 断言一致

## Phase 3：场景管理预览

- [x] 3.1 实现场景管理页：列表展示场景包（本地 mock 数据），每项提供"执行"按钮与状态标签；执行按钮调用占位端点 `POST /v1/scenarios/run`（返回 501 + 提示"由 #15 scenario-engine 提供"）。验证：页面可见列表，点击执行弹窗提示未实现

## 收尾

- [x] 4.1 全量回归：`CGO_ENABLED=0 go test -race -count=1 ./...` 退出码 0
- [x] 4.2 `openspec validate --changes "web-management-ui" --strict` 退出码 0
- [x] 4.3 归档变更并更新 `docs/roadmap-15-steps.md` 中 #14 状态
