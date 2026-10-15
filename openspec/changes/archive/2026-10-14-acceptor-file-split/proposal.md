# Proposal: Split acceptor.go into Cluster Files (Flat, Same Package)

## Why

`internal/app/acceptor.go` 已长到 **3043 行 / 100 个顶层函数**，是全仓最大文件（第二名 `node_service.go` 1318 行）。它同时承载 7 个互不相干的关注点：节点生命周期、fault 注入门、过期清扫、REGISTER 认证、MESSAGE 命令分发、catalog/订阅/NOTIFY、媒体管线。后果：

- 每次 feature 落地（如 device-catalog-answer）都要在同一个文件里穿梭数千行，review diff 噪声大、冲突面广；
- fault 门、媒体管线等本可与 SIP 分发解耦的逻辑挤在一个文件，新人定位成本高；
- 4 组互斥锁（`mu`/`inviteMu`/`subMu`/pipeline map）的边界靠注释而非文件边界表达。

仓库里曾按子目录方案（`internal/app/{acceptor,handlers,messages}/`）动工但已搁置——目录为空。搁置原因（本次分析确认）：**全部 13k 行 acceptor 测试是 in-package 的**（`package app`，直接使用 `faultSkip`、`sipReasons`、`platform`、`nullESWriter` 等未导出符号），子目录意味着子包，测试全改。因此改为**同包平铺拆分**：零测试改动。

## What Changes

- **纯文件级重构，零行为变化**：把 `acceptor.go` 的 100 个函数按 7 个关注点切到 **12 个**新文件（均在 `package app`，文件名 `acceptor_*.go` 前缀区分）
- `acceptor.go` 从 3043 行削到 **≤500 行**（实际 153 行），只保留骨架：`Acceptor` struct、`NewAcceptor`、`With*` 链式设置器（迁至 `acceptor_options.go`）、`Serve`/`ServeDevice`/`Stop`/`Close`、`run` 读循环（迁至 `acceptor_run.go`）
- 删除三个空的遗留目录 `internal/app/{acceptor,handlers,messages}/`
- 不改任何函数签名、不改任何未导出符号名、不改任何测试文件

## Capabilities

### New Capabilities

- （无。本 change 是行为零变化的结构重构，不引入也不修改任何能力契约）

### Modified Capabilities

- （无。所有 spec 场景的对外行为保持逐字节一致）

## Non-goals

- 不改成子包（`package acceptor`）——那会迫使 ~13k 行 in-package 测试全部重写
- 不借机改函数签名、重命名符号、调整锁粒度——那是后续独立 change 的事
- 不动 `acceptor_test.go` 等 7 个测试文件
- 不拆 `node_service.go`（1318 行，次大文件）——先验证 acceptor 拆分的做法，再复制经验

## Impact

- **代码**：`internal/app/acceptor.go` 瘦身；新增 **12 个** `internal/app/acceptor_*.go`；删除 3 个空目录
- **测试**：零改动（核心验收标准之一：`git diff --stat` 中 `*_test.go` 变更行数为 0）
- **风险**：低。同包内搬移函数不改变任何符号解析；每步搬运后 `go build && go test ./internal/app/` 必须保持绿
- **回滚**：单个 `git revert` 即可完整回滚（纯移动，无语义 diff）
