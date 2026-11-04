# 剩余能力路线图（#47+）

> **For agentic workers:** 按阶段开 OpenSpec change → apply → 归档并同步 `docs/roadmap-15-steps.md` / `docs/gb28181-coverage.md` / `docs/web-ui-guide.md`。优先用 subagent-driven-development 或 executing-plans 逐任务落地。

**Goal:** 补齐「操作界面相对后端能力的缺口」，再按价值推进级联可观测与少数国标长尾；不追求国标全文每一附录的完整实现。

**Architecture:** 后端 `#41/#42/#43` 已具备平台拉流、录像库、级联媒体中继；缺口主要在 Web 接线与可观测。协议长尾（校时 / 媒体保活 / TLS·SRTP）另开 change，与 UI 解耦。

**Tech Stack:** Vue 3 + Element Plus（`web/src`）、已有 HTTP API（`internal/interface/http`）、OpenSpec、e2e（`internal/test/e2e`）。

**Spec 锚点:** `openspec/specs/platform-live-pull`、`platform-recording`、`cascade-media-relay`、`channel-http-api`、`flv-media-gateway`；覆盖表 `docs/gb28181-coverage.md`；UI 手册 `docs/web-ui-guide.md`。

## Global Constraints

- 不改动已归档 `#41/#42/#43` 的 SIP/媒体语义；UI 只消费现有端点，缺状态查询时才加最小只读 API。
- 平台 / 设备共用同一 Web；按 `node.kind` 显隐能力，避免两套壳。
- 失败码保持现有约定（404 / 501 / 502 / 504），前端用 `ElMessage` 暴露，禁止静默成功。
- 每阶段归档必须同步路线图与相关用户文档（`docs-roadmap-sync` / `protocol-coverage-docs`）。
- 快照解码器、附录 I CRL、媒体国密 SRTP、DragZoom 几何模拟、附录 G：**本路线图默认不做**（见「明确不做」）。

## Review Focus

1. 平台通道页刷新后是否仍能感知「正在拉流」。
2. 录像列表是否误走设备 `RecordInfo` 路径。
3. `source=live|local` 与默认优先 live 是否与播放按钮一致。
4. 级联中继会话是否可在 UI/API 上区分于普通 live。
5. 协议长尾 change 是否错误扩大到「完整国密 / 证书体系」。

---

## 优先级总览

| 阶段 | 建议编号 | OpenSpec 名（草案） | 价值 | 后端增量 |
|------|----------|---------------------|------|----------|
| A | **#47** | `webui-platform-live-and-recordings` | 高：操作面相对后端最大缺口 | 可选 `GET …/live` |
| B | **#48** | `cascade-observability` | 中：联调排障 | 只读会话列表 API |
| C | **#49** | `network-timing-and-media-keepalive` | 中低：覆盖表 ❌ | 信令 Date + 弱 RTCP/保活 |
| D | **#50** | `sip-tls-transport`（可选拆 `srtp-media`） | 低（演示/特定互通） | 传输与密钥配置 |
| — | — | （不做清单） | — | — |

**推荐执行顺序：** A → B；C/D 仅在有明确互通需求时开单。

> 进度（2026-11-04）：A/B/C 与合壳角色（#50）已交付归档；D（SIP TLS）仍为候选。

---

## 阶段 A — #47 Web UI：平台拉流 + 录像库

**Goal:** 在平台节点通道详情 / 录像页能启动·停止实时拉流、预览 live FLV、查询平台录像库并 FLV 回放。

### 文件地图

| 文件 | 职责 |
|------|------|
| `web/src/api.js` | `startLive` / `stopLive` / `getLive?` / `listRecordings` / `recordingFlvUrl`；`flvUrl(node,ch,{source})` |
| `web/src/views/ChannelDetailView.vue` | `kind===platform*` 时：拉流控件、transport、会话信息；播放走 live 优先 |
| `web/src/views/RecordView.vue` | 平台节点走 `…/recordings` + `…/recordings/:id/flv`；设备节点保持现有 `records`+playback |
| `web/src/views/ChannelListView.vue` / `NodesView.vue` | 平台节点同样可进通道列表（若当前被藏，解开并标注） |
| `web/src/views/HelpView.vue` | 补「平台拉流 / 录像库」步骤 |
| `docs/web-ui-guide.md` | 5.x / 6.x 增平台小节与截图占位说明 |
| `docs/roadmap-15-steps.md` | 登记 #47 |
| （可选）`internal/interface/http/live.go` | `GET …/live` → 当前会话或 404 |
| （可选）对应 httptest + `openspec/specs/channel-http-api` delta |

### 行为决策（锁定）

1. **节点分流：** `node.kind` 以 `device` vs `platform`/`platform-large`/`platform-small`（以仓库实际枚举为准）分支 UI。
2. **拉流：** `POST …/live` body `{transport:"udp"|"tcp"}`；成功展示 `session_id` / `local_port` / `transport`；停止 `POST …/live/stop`（幂等）。
3. **预览：** 拉流成功后 `flvUrl(..., {source:'live'})`；未拉流时平台侧可提示「请先拉流」或尝试默认 URL（后端已 live-优先），但按钮语义要清晰。
4. **录像：** 平台 `GET …/recordings?start&end`（RFC3339）；播放用录像 FLV，**不**发设备侧 `POST /playback`。
5. **刷新恢复（推荐做）：** 增加只读 `GET …/live`；缺省则仅靠组件内状态（刷新后需用户再点拉流或看 FLV 404）。
6. **不做：** 本阶段不做 cascade 会话列表、不做校时/TLS、不修 snapshot 501。

### 任务拆分

- [ ] **A.1** OpenSpec propose `webui-platform-live-and-recordings`（#47）：proposal / design / tasks；若加 `GET …/live` 则带 `channel-http-api` delta。
- [ ] **A.2**（可选）实现 `GET …/live` + httptest；无会话 404。
- [ ] **A.3** `api.js` 封装 live / recordings / flv `source` 查询参数。
- [ ] **A.4** `ChannelDetailView`：平台拉流条 + 错误映射；与现有 device PTZ/对讲并存（平台可隐藏对讲或保留转发能力——**默认平台隐藏对讲/PTZ 面板**，仅保留预览+拉流+进录像，与手册「设备管通道控制」一致；若平台已支持转发 PTZ 可后续加，本 change 不做）。
- [ ] **A.5** `RecordView`：按 kind 双数据源；平台播放不走 playback INVITE。
- [ ] **A.6** 节点/通道入口：确保 platform 节点卡片能进通道（与 device 对称的「通道」按钮若缺失则补）。
- [ ] **A.7** 更新 `web-ui-guide.md`、`HelpView`；`make test` / 前端构建；手工验收清单（见下）。
- [ ] **A.8** 归档 + 路线图 #47 ✅。

### 手工验收清单

1. 拓扑：platform ← device（通道有媒体源）在线。
2. 平台 Web → 通道详情 → 选 UDP 拉流 → 预览出画 → 停止后预览失败或可辨识空流。
3. 拉流一段时间后录像库出现分段；时间窗查询；点播 FLV 可播。
4. 设备节点原 PTZ / 对讲 / RecordInfo 回放回归无回归。
5. 无下级 / 设备拒流 → UI 显示 404/502/504 文案。

---

## 阶段 B — #48 级联可观测性

**Goal:** 运维能看见「谁在中继、上下级 dialog、live key」，避免只能靠抓包。

### 范围（最小）

- 只读 API：例如 `GET /v1/nodes/:id/cascade/sessions`（字段：上级 Call-ID、下级 live key、channel、transport、起止时间）。
- Web：节点详情或独立「级联」页表格 + 链到抓包；不提供强制拆链按钮（拆链仍走 BYE/live/stop，避免双路径）。

### 不做

- 附录 H.3 自动路径发现；改 MESSAGE 中继语义；媒体旁路调试器。

### 任务（摘要）

- [ ] B.1 OpenSpec + 只读端口从 `NodeService`/relay 登记表导出。
- [ ] B.2 httptest / e2e：有中继时列表非空，BYE 后清空。
- [ ] B.3 简单 Vue 表；文档 + 路线图 #48。

---

## 阶段 C — #49 网络校时 + 媒体保活（弱）

**Goal:** 覆盖表 7.10 / 附录 M 从 ❌ 升到 🟡（模拟级）。

### 范围

- 入站/出站 Date 或校时 MESSAGE 的可配置应答（设备跟随平台时间偏移可注入）。
- 媒体面：会话级保活超时已有则文档化；可选发送最小 RTCP RR 或周期性空包——**以「真实平台不因无 RTCP 拆链」为验收**，不做完整 RTCP 栈。

### 不做

- NTP 客户端集群；完整 RFC 3550 实现。

---

## 阶段 D — #50 SIP TLS（SRTP 另议）

**Goal:** 可选监听 `sips`/TLS 传输，配置证书路径；信令面可联调。

### 范围

- 传输层 TLS 包装 + 配置项；文档互操作注意点。
- **SRTP / 国密媒体：** 单独立项评估工作量；默认本阶段不做。

---

## 明确不做（本路线图）

| 项 | 原因 |
|----|------|
| 快照 501（需解码器） | CGO/解码依赖与当前 `CGO_ENABLED=0` 策略冲突；合成源 JPEG 已够用 |
| 附录 I 完整证书/CRL | 远超模拟器定位；现有 GB35114 进程级密钥足够演示 |
| 第 8 章媒体国密 SRTP | 高成本；无明确客户需求不排期 |
| DragZoom / TargetTrack 几何 | 已显式 ERROR；完整模拟收益低 |
| 附录 G 外部系统接口 | 非目标 |
| 广播并入对讲 WS 桥 | 现语义有意区分；改动易破坏 #38 |

---

## OpenSpec 落地约定

每阶段：

1. `openspec-propose`（或等价）建 change，写入路线图编号。
2. apply 实现；`openspec validate … --strict`、`make lint`、`make test`。
3. archive；同提交更新 `docs/roadmap-15-steps.md`；协议相关再更新 `docs/gb28181-coverage.md`；UI 相关更新 `docs/web-ui-guide.md`。

---

## 建议的下一句指令

确认本计划后，直接说：**「按阶段 A 开 OpenSpec 并实现」**（或「先只 propose #47」）。若要把平台 PTZ/对讲也做进 A，或必须先做 B，再说一下优先级即可改表。
