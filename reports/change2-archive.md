# Change 2 归档摘要 — core-sip-stack

> 归档日期：2026-09-23
> OpenSpec 路径：`openspec/changes/archive/2026-09-23-core-sip-stack/`
> 主规格：`openspec/specs/core-sip-stack/spec.md`

## 归档动作

| 步骤 | 命令/产物 |
|---|---|
| 1. tasks.md 勾选 | 38/38 已勾（CLI `progress: {total:38, complete:38}`） |
| 2. verify-change | `reports/change2-verify.md`（38/38 task、10/10 req、9/9 scenario、5/5 design） |
| 3. sync-specs | 主规格 `openspec/specs/core-sip-stack/spec.md` 新建（10 个 ADDED requirement 全量合并） |
| 4. validate | `openspec validate --specs`：2 passed, 0 failed |
| 5. archive | `mv openspec/changes/core-sip-stack openspec/changes/archive/2026-09-23-core-sip-stack` |

## 实测基线（2026-09-23）

- **测试**：`CGO_ENABLED=0 go test -race -count=1 -timeout=60s ./...` → 11 包全绿
- **快速子集**：`make sip-test` → 5 包（sip/sdp/auth/sipprobe/audit）全绿
- **golden fixture**：10 份 .sdp/.auth/.pcap.txt 全部 sha256 校验 OK
- **依赖锁定**：`ghettovoice/gosip v0.0.0-20260919124345-798b72cc95a2` + `pion/sdp v1.3.0`（CGO_ENABLED=0 五平台编译）

## 报告清单（全部位于 `reports/`）

- `change2-summary.md` — 索引页 + 总览
- `change2-sdp.md` — §2 SDP 详细
- `change2-auth.md` — §3 Auth 详细
- `change2-sip.md` — §4–§5 SIP + audit 详细
- `change2-siptransport.md` — §6 siptransport 详细
- `change2-sipprobe.md` — §7 sipprobe 详细
- `change2-e2e.md` — §8–§10 端到端 + Makefile/CI/README
- `change2-verify.md` — `/opsx:verify` 三维度报告
- `change2-archive.md` — 本文件

## 路线图位置

| 字段 | 值 |
|---|---|
| 路线图阶段编号 | Change 2 / 15 ✅ 已归档 |
| 前置依赖 | Change 1 (`init-project-skeleton`，已归档) |
| 解锁的下一 change | Change 3 (`core-manscdp-and-ps`)、Change 4 (`node-abstraction`)、Change 11 (`gb28181-2022-extensions`)、Change 12 (`gb35114-security`)、Change 13 (`exception-and-capture`)、Change 14 (`web-management-ui`) |
| 第一阶段终点 | 否 |

## 下一步

按用户要求，下一步进入 Change 3（按用户选择 **新开** `enterprise-skeleton` change），走六边形架构重塑骨架。该 change 与 Change 4-15 并行独立推进，但建议在 Change 4 `node-abstraction` 之前先完成骨架改造，避免 Change 4 直接 embed 老结构。

⚠️ **决策点（待与用户确认后再开 change）**：
- 六边形架构端口命名：`internal/domain/port/` vs `internal/domain/spi/`（Service Provider Interface）？
- 是否引入 `wire` 做编译期 DI，还是手写最小容器？
- `internal/logger` 升级为 OpenTelemetry trace+metric provider，还是保留 `log/slog` ？

待 Change 3 `core-manscdp-and-ps`（MANSCDP+ XML + PS 封装）和 Change 3.5 `enterprise-skeleton`（六边形骨架）的优先级与顺序确认后启动。
