# Design: acceptor-file-split

## Context

`internal/app/acceptor.go` = 3043 行 / 100 个顶层函数 / 4 个 import 群。本节记录拆分依据与关键技术决策。

**实际架构骨架**（已逐行确认）：

```
Acceptor struct (99-137)     20 字段：11 个 port + 5 个 With* 能力 + 4 组互斥状态
platform struct (149-199)    每节点运行态，内含 subMu + catalogPoller + pendingNotify
run (567-742, 176 行)        唯一热路径：
                             读循环 → fault 门 → device 门 → 9 路 method switch → 出站路由 → NOTIFY flush
```

### 六个自然切面（按依赖聚类）

| # | 切面 | 原行区间 | 函数数 | 依赖方向 |
|---|------|---------|-------|---------|
| 1 | 生命周期 | 99-137, 241-364, 413-557 | 18 | 叶子，只依赖 `platform` + port |
| 2 | fault 注入 | 744-858 | 6 | 依赖 `buildResponse` |
| 3 | 过期清扫 | 860-911 | 4 | 依赖 `notifyCatalogChange`（在簇 5） |
| 4 | MESSAGE 分发 + 命令 | 913-2305 | 34 | 依赖 1/2 + 簇 5 的 `is2022` |
| 5 | Catalog / 订阅 / NOTIFY | 1339-1718, 2306-2543 | 20 | 被 3 依赖；依赖 4 的 `is2022` |
| 6 | REGISTER / challenge | 2544-2813 | 20 | 依赖 `buildResponse` |
| 7 | 媒体管线 | 2815-3043 | 9 | 依赖 `MediaService` + `net` |

## Goals / Non-Goals

**Goals:**

- `acceptor.go` ≤500 行，只留骨架
- 同包平铺，**零测试改动**
- 每步搬运后测试保持绿

**Non-Goals:**

- 不改子包化（测试成本过高）
- 不改签名/符号名/锁粒度
- 不拆 `node_service.go`

## Decisions

### 决策 1：同包平铺，而非子包

**理由**：`acceptor_test.go`(774) + `acceptor_message_test.go`(1512) + `acceptor_device_test.go`(596) 等共 ~13k 行测试全部 `package app`，直接使用 `faultSkip`、`sipReasons`、`platform`、`nullESWriter`、`cannedReason` 等未导出符号。若拆成 `internal/app/acceptor` 子包（`package acceptor`），这些测试需全部改包名并导出或注入依赖——对一个纯重构 change 而言风险与工作量都不可接受。

**代价**：文件名数量增加（11 个 `acceptor_*.go`）。这是刻意的：用文件名换取零测试改动。

**替代方案（已否决）**：子目录 + 文件名划分。若后续某天确实需要包边界（例如测试需要跨包 mock 未导出符号），可另起 change 处理；届时测试改造是一次性成本。

### 决策 2：拆分单元是「函数整体搬移」，不做任何修改

每个函数连同其 doc comment 整体移到新文件，`func` 签名、receiver、import 一律不动。import 按文件实际用到的包裁剪（Go 会拒绝未使用 import，所以每步 `goimports` 后必过编译）。

**理由**：任何"顺手改一点点"（重命名、补注释、调参数顺序）都会把 `git diff` 从纯 move 变成语义 diff，review 成本陡增，且回滚不再是一个 `git revert`。

**实施纪律**：每个 PR 只做搬移 + import 调整 + 必要的包级 doc 合并。发现的问题记到后续 change，不在本次修。

### 决策 3：`platform` struct 与其 `subMu` 方法必须同行

`platform` (149-180)、`pendingEventNotify` (182-188)、`catalogSub` (190-199)、`addSubscriber`/`deleteSubscriber`/`snapshotSubscribers` (201-235) 是一体的：锁与被保护的数据必须同文件可见，否则加锁边界在阅读时丢失——这正是拆分要解决的问题本身。

同理 `Acceptor` struct (99-137) 与它的 4 组锁注释留在根文件。

### 决策 4：`buildResponse` 留在根文件

`buildResponse` 被 6 处以上调用（簇 4/5/6/7 全部依赖），是唯一的公共叶子。它留在 `acceptor.go`，其余文件直接调用（无需导入，跨文件同包可见）。

### 决策 5：删除空遗留目录

`internal/app/{acceptor,handlers,messages}/` 三个空目录已在 git 中，是被搁置的子包方案残留。删除，避免后人误以为那里应该有代码。

## Risks / Trade-offs

| 风险 | 缓解 |
|------|------|
| 搬移时漏函数 → 编译失败 | 每步 `go build ./...`；最后用 `grep -c '^func'` 核对总数（拆分前 100） |
| 搬移时改动函数体 → 语义漂移 | 纪律：只搬不改；`git diff --stat` 中测试文件变更必须为 0 |
| import 裁剪遗漏 | 每步 `goimports -w`；`go vet ./internal/app/` |
| 拆分后某文件仍过大 | 验收要求每个新文件 ≤450 行；超标则继续细分（如 `device_cmds.go` 可再切 `device_2022_cmds.go`） |

## Migration Plan

分 4 个 PR，每个 PR 一次完整「建文件 → 搬函数 → 编译 → 测试 → 提交」：

- **PR 1.1** 建 `acceptor_*.go` 骨架（空文件 + 包 doc）
- **PR 1.2** 搬移簇 2/3/6/7（fault / sweep / register / media，共 39 函数，无相互依赖，最安全）
- **PR 1.3** 搬移簇 5（catalog / subscribe，20 函数）
- **PR 1.4** 搬移簇 4（messages / device commands，34 函数）+ 削薄根文件至 ≤500 行 + 验收

## Open Questions

- （无。切分粒度已由上表固定）