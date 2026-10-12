# Proposal: 让 platform-large 也可被上级拉目录、反向注册与挂通道

## Why

当前 `platform-large`（type 200）节点是纯 UAS——只接受下级注册并应答目录查询——但产品化的对接路径需要它同时具备三件事：

1. 第三方真实平台以 `platform-large` 为入口查询目录时，目录里除下挂设备之外还要包含平台自身声明的"业务通道"。
2. `platform-large` 能主动注册到上游（上级"真平台"或另一台 `platform-large`），把级联链的真实上游纳入，而不是只能"等别人注册到我"。
3. 同一节点同时承担上述两半的语义不能破坏现有 `platform-small`（type 216）双半行为的契约（`platform-small-node` spec 已沉淀）。

不补这三件事，第三方平台→模拟器→设备的目录拉取链会一直停在 `SumNum=0` 的空目录上：这是 #6 / #9 两个路线图阶段未完成时遗留下来的真实场景缺口。

## What Changes

- **能力扩展**：允许 `platform-large` 节点声明可选的 `registration:` 段（与 `platform-small` 一致），启动时按"先 serve / 后注册上级"两半推进；任一半失败回滚另一半。
- **能力扩展**：允许 `platform-large` 节点通过现有 HTTP 端点 `POST /v1/nodes/:id/channels` 与 `DELETE /v1/nodes/:id/channels/:ch` 增删通道；通道增删即时反映到下游 Catalog 响应（通道作为目录条目 `DeviceList` 的一部分被渲染）。
- **行为修正**：把 `channel-http-api` spec 中"非 device 节点回 404"的条款改为"非 platform 节点回 404"——platform-large 现在与 platform-small、device 三类节点一样支持通道管理；非这三类（如未来引入的中间件节点）才回 404。
- **复用现有实现**：`platform-small-node` spec 已沉淀的 UAS/UAC 双半逻辑将以函数复用方式提供给 `platform-large`，新增的 model / service 字段将走最小侵入路径（不引入新节点类型，不分裂 catalog 聚合逻辑）。

无破坏性 API 变更；现有 platform-small 行为契约保持不变。

## Capabilities

### New Capabilities

无新 capability。所有 capability 均在现有条目内做需求级变更。

### Modified Capabilities

- `platform-large-node`：
  - 在"需求：platform-large 节点用在线设备表应答 MANSCDP 目录查询"中追加"通道也是目录条目"：Catalog = 在线设备表 + 节点自身通道（按 device id 升序），空表时空通道亦能渲染 `SumNum=0`。
  - 在"需求：平台服务随节点启停"中追加"可选的反向注册半"：声明 `registration:` 时节点按 serve-then-register 顺序推进；任一半失败则回滚并进入 `fault`。
  - 在"需求：平台服务为声明式、可选且经配置校验"中追加"registration 可选"：复用 `platform-small-node` 的 registration 校验规则（realm / accounts / expires 窗口），错误信息同样不含密码明文。
- `platform-small-node`：保留契约不变，本 change 通过复用其注册半套实现。
- `channel-http-api`：
  - 在"需求：通道增删 HTTP 端点"的 Scenario "非 device 节点回 404" 改为"非 platform 节点回 404"，并新增 Scenario "对 platform-large 节点调用通道增删可成功（201 / 204）"。
  - 增删操作对 platform-large 的可见性即时性需新增 Scenario：新增通道后下一条 Catalog 查询包含该通道；删除同理。
- `node-abstraction`：
  - 在节点身份矩阵中追加 platform-large 的"双半"语义说明（声明 `registration:` 时它既是 UAS 又是 UAC）。

## Impact

### 触动代码

- `internal/app/node_service.go:312-330` —— 启动分发：当前 `platform-large` 分支只走 `serve`，需扩为"有 `registration:` 时走 startPlatformSmall 的同序推进，无独立 th"。
- `internal/app/node_service.go:586-588` —— `unregisterUpstream`：当前对 `platform-large` 直接返回，需扩为按节点实际 `wants` 决定（与 platform-small 同路径）。
- `internal/domain/model/node.go` —— `NodeProfile.Registration()` 已存在；`WithChannelAdded` 已存在且不按 kind 拦，仅需确认 `platform-large` 的 profile 中 channels 字段可达。
- `internal/app/acceptor.go` —— 目录查询应答路径（`acceptor.go:2186` 一带）：当前只取 `devices.List`，需并入节点 `Channels()`，并按 device id 升序。
- `internal/interface/http/channels.go:170-234` —— 端点实现本身不需要改 kind 检查（实现真按 kind 返 404 的实际是 spec 文本与 `acceptor_message_test.go` 的隐含契约），但需要新增 platform-large + device 的端到端测试。
- `configs/` —— 需要新增一份"platform-large 既能挂通道又能向上注册"的样例 yaml 供运维参考。

### 触动 API

- HTTP：`POST /v1/nodes/:id/channels`、`DELETE /v1/nodes/:id/channels/:ch` 在 platform-large 上从"返回 404"改为"返回 201 / 204"。这是 spec-level 行为变更，但 HTTP 字段名约定不变。
- SIP：MANSCDP `Catalog` 响应体字段集不变，仅 `DeviceList` 项数可能增加（含通道）；`X-RoutePath` 等级联头语义不变（`cascade-routing` spec 不受影响）。

### 触动依赖

无新增外部依赖。复用 `port.MANSCDPCodec`、`port.SIPTransport`、`model.DownstreamDevice` 既有抽象。

### 路线图阶段

- 主线：路线图第 6 阶段（platform-large 身份完整实现）—— 在已有的"接受注册/应答目录/在线设备表"基础上补齐"反向注册 + 挂通道"，闭合产品化路径。
- 复用：路线图第 7 阶段（platform-small 身份实现）— 该阶段已沉淀 registration 半套，本 change 通过函数复用避免重复实现。
- 次级：路线图第 9 阶段（cascade-and-multi-instance）— 通过本 change 后 platform-large 可作为级联链中真实的一环被级联起来；该阶段主线未动，本 change 仅做能力扩展。

### Non-goals

显式排除以下 GB28181 能力（不在本 change 范围内）：

- **Alarm 通知上报**（告警订阅与广播）：不属于本 change；按路线图属于第 10 阶段。
- **MobilePosition 移动位置订阅**：不属于本 change；按路线图属于第 10 阶段。
- **RecordInfo 录像查询与回放控制**：不属于本 change；按路线图属于第 10 阶段（`/platform-small-node` 现状仅"解析并转发 MediaStatus"，录像不在此列）。
- **VoiceBroadcast / Talk 语音对讲**（含语音广播）：不属于本 change；按路线图属于第 10 阶段。
- **2022 增量能力**（X-GB-Ver 协商之外的部分）：不属于本 change；按路线图属于第 11 阶段。
- **GB35114 安全**（SM2/SM3 信令保护）：不属于本 change；按路线图属于第 12 阶段。
- **Scenario Engine**（YAML 驱动的场景脚本）：不属于本 change；按路线图属于第 15 阶段。
- **多上级挂载**（一个 platform-large 同时向多个上级注册）：本 change 仅做单上级 single endpoint 的最小闭环；多上级挂载是产品化更深一层诉求，留作后续 change。
- **新节点类型 platform-direct**（早前讨论中作为 B2 路线的设想）：本 change 不新增节点类型，所有能力扩展落在现有 platform-large 上，避免身份图谱分裂。

## 路线图对应

阶段：6 / 7（复用）/ 9（次级）