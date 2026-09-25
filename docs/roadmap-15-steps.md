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
| 9 | `cascade-and-multi-instance` | 级联 + 多节点共存 | ⬜ 待实施 | X-RoutePath、X-PreferredPath |
| 10 | `dynamic-catalog-and-query` | 动态目录与查询 | ⬜ 待实施 | 移动位置、报警事件、历史录像 |
| 11 | `gb28181-2022-extensions` | GB/T 28181-2022 扩展 | ⬜ 待实施 | X-GB-Ver 协商、2022 版增量能力 |
| 12 | `gb35114-security` | GB 35114 安全扩展 | ⬜ 待实施 | SM2 互认证、SM3 Digest |
| 13 | `exception-and-capture` | 异常流 + 抓包 | ⬜ 待实施 | 4xx/5xx 业务编排、pcap 导出 |
| 14 | `web-management-ui` | Web 管理界面 | ⬜ 待实施 | OTel trace 查询、抓包面板 |
| 15 | `scenario-engine` | YAML 场景引擎 | ⬜ 待实施 | 场景脚本驱动 |

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
- 当前已完成 8 个编号（#1/#2/#3/#4/#5/#6/#7/#8），对应 12 个归档 change。
- 待实施 8 个编号（#3/#9/#10/#11/#12/#13/#14/#15）。
