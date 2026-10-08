# 路线图（15 步）

本文件记录 `gb28181-simulator` 的完整 15 步路线图，按原始设计顺序排列。  
状态说明：`⬜ 待实施` / `🔨 进行中` / `✅ 已归档`。

| # | 编号 | 名称 | 状态 | 说明 |
|---|------|------|------|------|
| 1 | `init-project-skeleton` | 项目骨架 + Web 壳 | ✅ 已归档 | 仓库结构、依赖、CI、日志、HTTP 骨架、Web 嵌入 |
| 2 | `core-sip-stack` | 节点无关 SIP/SDP/Digest 库 | ✅ 已归档 | gosip transport、审计日志、sipprobe |
| 3 | `core-manscdp-and-ps` | MANSCDP 编解码 + PS 封装 | ✅ 已归档 | XML 编解码、PS 打包/解包、RTP 分包 |
| 4 | `enterprise-skeleton` | 六边形架构骨架 | ✅ 已归档 | platform/domain/adapter/interface/app |
| 5 | `device-node` | device 节点 | ✅ 已归档 | registration + keepalive |
| 6 | `platform-large-node` | platform-large 节点 | ✅ 已归档 | registration + keepalive-catalog |
| 7 | `platform-small-node` | platform-small 节点 | ✅ 已归档 | cascading-identity + supplementary |
| 8 | `media-sources` | 媒体源 + PS/RTP 管道 | ✅ 已归档 | HLS/RTMP/file/stream → PS → RTP |
| 9 | `cascade-and-multi-instance` | 级联 + 多节点共存 | ✅ 已归档 | X-RoutePath、X-PreferredPath |
| 10 | `dynamic-sim-features` | 动态目录/报警、录像与回放、移动位置 | ✅ 已归档 | 动态目录/报警、录像与回放、移动位置 |
| 11 | `gb28181-2022-extensions` | GB/T 28181-2022 扩展 | ✅ 已归档 | X-GB-Ver 协商、2022 版增量能力 |
| 12 | `gb35114-security` | GB 35114 安全扩展 | ✅ 已归档 | SM2 互认证、SM3 Digest |
| 13 | `exception-and-capture` | 异常流 + 抓包 | ✅ 已归档 | 故障注入、pcap 导出（2026-09-27） |
| 14 | `web-management-ui` | Web 管理界面 | ✅ 已归档 | 节点/抓包/故障/场景页 + 501 占位（2026-09-27） |
| 15 | `scenario-engine` | YAML 场景引擎（2026-09-27） | ✅ 已归档 | YAML-driven 场景脚本引擎 + 报告产出 |
| 16 | `manscdp-logging-coverage` | MANSCDP+ 业务面日志覆盖 | ✅ 已归档 | acceptor 7 处成功路径补 Debug 日志（codec 包按惯例保持无日志）（2026-09-29） |
| 17 | `media-aggregator-integration` | 媒体热循环聚合接入 | ⬜ 待实施 | `ps_packetizer.go` / `rtpizer.go` 调用 `error_aggregator` |
| 18 | `logging-user-docs` | 日志配置用户文档 | ⬜ 待实施 | `docs/logging.md`：env 变量、`log.modules` 语法、PATCH 接口契约 |
| 19 | `media-source-config` | 媒体源端到端配置 | 🔨 进行中 | config `media:` 段 + HTTP API + Web UI 面板；打通 #8 遗留的 app 层 wiring（2026-10-08） |

## 依赖关系

```
1 ──┐
    ├─▶ 2 ──┐
3 ────────┤     ├─▶ 4 ──┐
          ├─▶ 3 ──┤     ├─▶ 5 ──┐
          │       │     │     ├─▶ 6 ──┐
          │       │     │     │     ├─▶ 7 ──┐
          │       │     │     │     │     ├─▶ 8
          │       │     │     │     │     │
          │       │     │     │     │     ├─▶ 9 ──┐
          │       │     │     │     │     │     ├─▶ 10
          │       │     │     │     │     │     │
          │       │     │     │     │     │     ├─▶ 11
          │       │     │     │     │     │     │
          │       │     │     │     │     │     ├─▶ 12
          │       │     │     │     │     │     │
          │       │     │     │     │     │     ├─▶ 13
          │       │     │     │     │     │     │
          │       │     │     │     │     │     ├─▶ 14
          │       │     │     │     │     │     │
          │       │     │     │     │     │     └─▶ 15
```

## 说明

- 原始路线图中 #3 为 `core-manscdp-and-ps`，后被 `enterprise-skeleton` 占位，本文件保留原始编号顺序。
- 部分 change 内部拆分为多个子 change 归档（如 #5、#6、#7、#8）。
- #10 原名 `dynamic-catalog-and-query`，实际由 `dynamic-sim-features` change 实现（动态目录/报警、录像与回放、移动位置、运行时触发 API）。
- 当前已完成 15 个编号（#1–#15），对应 23 个归档 change 目录（`ls openspec/changes/archive/ | wc -l = 23`，2026-09-29 同步）。
- 待实施 4 个编号（#16–#19：manscdp-logging-coverage、media-aggregator-integration、logging-user-docs、media-source-config），均来自 `2026-09-29-enhance-logging-coverage` 的 Deferred 项。
- 路线图同步契约见 capability `docs-roadmap-sync`；每次归档必须在同提交更新本文件。
