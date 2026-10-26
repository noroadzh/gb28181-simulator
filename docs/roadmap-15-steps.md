# 路线图（15 步）

本文件记录 `gb28181-simulator` 的完整 15 步路线图，按原始设计顺序排列。  
状态说明：`⬜ 待实施` / `🔨 进行中` / `✅ 已归档`。

| # | 编号 | 名称 | 状态 | 说明 |
|---|------|------|------|------|
| 1 | `init-project-skeleton` | 项目骨架 + Web 壳 | ✅ 已归档 | 仓库结构、依赖、CI、日志、HTTP 骨架、Web 嵌入（2026-09-23） |
| 2 | `core-sip-stack` | 节点无关 SIP/SDP/Digest 库 | ✅ 已归档 | gosip transport、审计日志、sipprobe（2026-09-23） |
| 3 | `core-manscdp-and-ps` | MANSCDP 编解码 + PS 封装 | ✅ 已归档 | XML 编解码、PS 打包/解包、RTP 分包（2026-09-25） |
| 4 | `enterprise-skeleton` | 六边形架构骨架 | ✅ 已归档 | platform/domain/adapter/interface/app（2026-09-24） |
| 5 | `device-node` | device 节点 | ✅ 已归档 | registration + keepalive（2026-09-24） |
| 6 | `platform-large-node` | platform-large 节点 | ✅ 已归档 | registration + keepalive-catalog（2026-09-24） |
| 7 | `platform-small-node` | platform-small 节点 | ✅ 已归档 | cascading-identity + supplementary（2026-09-25） |
| 8 | `media-sources` | 媒体源 + PS/RTP 管道 | ✅ 已归档 | HLS/RTMP/file/stream → PS → RTP（2026-09-25） |
| 9 | `cascade-and-multi-instance` | 级联 + 多节点共存 | ✅ 已归档 | X-RoutePath、X-PreferredPath（2026-09-25） |
| 10 | `dynamic-sim-features` | 动态目录/报警、录像与回放、移动位置 | ✅ 已归档 | 动态目录/报警、录像与回放、移动位置（2026-09-25） |
| 11 | `gb28181-2022-extensions` | GB/T 28181-2022 扩展 | ✅ 已归档 | X-GB-Ver 协商、2022 版增量能力（2026-09-26） |
| 12 | `gb35114-security` | GB 35114 安全扩展 | ✅ 已归档 | SM2 互认证、SM3 Digest（2026-09-26） |
| 13 | `exception-and-capture` | 异常流 + 抓包 | ✅ 已归档 | 故障注入、pcap 导出（2026-09-27） |
| 14 | `web-management-ui` | Web 管理界面 | ✅ 已归档 | 节点/抓包/故障/场景页 + 501 占位（2026-09-27） |
| 15 | `scenario-engine` | YAML 场景引擎（2026-09-27） | ✅ 已归档 | YAML-driven 场景脚本引擎 + 报告产出 |
| 16 | `manscdp-logging-coverage` | MANSCDP+ 业务面日志覆盖 | ✅ 已归档 | acceptor 7 处成功路径补 Debug 日志（codec 包按惯例保持无日志）（2026-09-29） |
| 17 | `media-aggregator-integration` | 媒体热循环聚合接入 | ✅ 已归档 | `ps_packetizer.go` / `rtpizer.go` 接入 `error_aggregator`（2026-09-29） |
| 18 | `logging-user-docs` | 日志配置用户文档 | ✅ 已归档 | `docs/logging.md`：env 变量、`log.modules` 语法、PATCH 接口契约（2026-09-29） |
| 19 | `media-source-config` | 媒体源端到端配置 | ✅ 已归档 | config `media:` 段 + HTTP API + Web UI 面板；打通 #8 遗留的 app 层 wiring（2026-10-08） |
| 20 | `fix-catalog-aggregate-and-channel-autogen` | 目录聚合 + 通道自动编号 + 循环校验 + 实时日志页 | ✅ 已归档 | Catalog 展开下游通道、通道 ID 自动生成、loop 仅限 file 校验、Web 实时日志页（2026-10-10） |
| 21 | `fix-device-registration-peer-matching` | 设备注册对端匹配修复 | ✅ 已归档 | 设备启动注册直接失败；修正对端地址匹配（2026-10-10） |
| 22 | `mp4-direct-playback` | MP4 源原生播放 | ✅ 已归档 | MP4 媒体源改走浏览器原生播放，其余源仍经 FLV（2026-10-10） |
| 23 | `mpeg4-part2-support` | MPEG-4 Part 2 媒体源 | ✅ 已归档 | 支持 mp4v 轨道；修正 FLV 网关的编码探测误判（2026-10-10） |
| 24 | `node-lifecycle-controls` | 节点生命周期控制 | ✅ 已归档 | 节点卡片启停/重试/注销 + 状态轮询（2026-10-10） |
| 25 | `subscribe-ps-double-close` | SubscribePS 双重关闭修复 | ✅ 已归档 | 并发场景下 reader 与 cleanup 重复 close channel（2026-10-10） |
| 26 | `cascade-media-source-via-catalog` | 级联媒体源经 catalog 同步 | ✅ 已归档 | platform 侧从 catalog 学习下级通道的媒体源（2026-10-12） |
| 27 | `fix-channel-media-fallback` | 通道媒体源回退修复 | ✅ 已归档 | 平台节点 has_media 为假的 HTTP 回归（2026-10-12） |
| 28 | `fix-web-ui-api-contract` | WebUI/API 契约修复 | ✅ 已归档 | 新增通道等处的请求体与后端校验不一致（2026-10-12） |
| 29 | `platform-large-upstream-and-channels` | platform-large 上游与通道 | ✅ 已归档 | 大平台兼具上游注册与通道级能力（2026-10-12） |
| 30 | `device-catalog-answer` | device 应答目录查询 | ✅ 已归档 | device 侧处理 Catalog 查询并返回通道列表（2026-10-13） |
| 31 | `acceptor-file-split` | acceptor 拆文件 | ✅ 已归档 | 3043 行拆为 12 个同包文件，行为零变化（2026-10-14） |
| 32 | `cascade-message-relay` | 级联消息中继 | ✅ 已归档 | 下行按 next hop 投递 + 响应回传，修复只挂头不投递（2026-10-26） |
| 33 | `runtime-node-configuration` | 节点配置运行期化 | ✅ 已归档 | 节点定义落 SQLite，运行期 CRUD 与级联设置（2026-10-26） |
| 34 | `channel-ptz-and-playback` | PTZ 与录像/回放信令 | ✅ 已归档 | PTZ DeviceControl、RecordInfo 查询、PlaybackControl + 回放会话；FLV 历史流延后为 #37（2026-10-26） |
| 35 | `channel-talk-and-snapshot` | 对讲与抓拍 | ✅ 已归档 | 对讲控制面（INVITE/BYE + 空闲/对端挂断回收）与合成源 JPEG 抓拍；音频桥延后为 #38（2026-10-26） |
| 36 | `spec-purpose-backfill` | 主 spec Purpose 补全 | ✅ 已归档 | 为 channel-id-auto-gen / media-loop-validation / platform-large-node-catalog-aggregate / web-realtime-logs 写入真实 Purpose（2026-10-26） |
| 37 | `playback-flv-stream` | 回放 FLV 历史流 | ✅ 已归档 | 回放 SDP（s=Playback/u=/t=）、设备按时间窗供流、`/playback/flv?sn=` 出流；FLV 由本地源按会话窗口产生（2026-10-26） |
| 38 | `talk-audio-bridge` | 对讲音频桥 | ✅ 已归档 | 设备受理音频 INVITE 并用自己的 RTP 端口应答；平台解析 answer；WebSocket ↔ RTP 双向 PCMU（2026-10-26） |
| 39 | `webui-talk-and-playback` | 前端对讲音频与回放播放 | ✅ 已归档 | 对讲：麦克风 → μ-law → /talk/audio；回放：play(sn,start,end) → /playback/flv?sn=；修掉 RecordView 依赖 playback_url 的缺口（2026-10-26） |

## 补充归档索引

子 change 与无编号跟进的登记处。`docs-roadmap-sync` 要求每个归档目录都能被定位：编号阶段行在上表，其余在此登记。

| 归档目录 | 归属阶段 | 说明 |
|---|---|---|
| `2026-09-24-device-node-registration` | #5 | device 注册事务 |
| `2026-09-24-device-node-keepalive` | #5 | device 心跳与续期 |
| `2026-09-24-node-abstraction` | — | 节点抽象骨架（原路线图第 4 步） |
| `2026-09-24-platform-large-registration` | #6 | 大平台受理下级注册 |
| `2026-09-25-platform-large-keepalive-catalog` | #6 | 心跳与目录聚合 |
| `2026-09-25-platform-small-cascading-identity` | #7 | 小平台级联身份 |
| `2026-09-25-platform-small-supplementary` | #7 | 小平台补充能力 |
| `2026-09-25-media-sources-ps-rtp-pipeline` | #8 | 媒体源 → PS → RTP 管道 |
| `2026-09-27-mp4-and-info-extensions` | #8 | MP4 与 INFO 扩展 |
| `2026-09-28-fix-deploy-docs-and-noauth` | — | 部署文档与免认证修复 |
| `2026-09-28-fix-problems-and-smoke-deploy-docs` | — | 冒烟与部署文档修复 |
| `2026-09-29-docs-roadmap-sync` | — | 路线图同步契约（capability） |
| `2026-09-29-enhance-logging-coverage` | #16 | 日志覆盖增强 |
| `2026-09-30-fix-concurrency-lifecycles` | — | 并发生命周期修复 |
| `2026-10-09-channel-account-management-and-help-doc` | — | 通道/账号管理与帮助文档 |
| `2026-10-09-flv-player-fix` | — | FLV 播放器修复 |
| `2026-10-09-media-source-local-file-alias` | — | 本地文件媒体源别名 |
| `2026-10-09-multi-channel-and-web-player` | — | 多通道与 Web 播放器 |
| `2026-10-09-ui-feedback-fixes` | — | UI 反馈修复 |
| `2026-10-26-roadmap-sync-2026-10` | — | 路线图与归档现实对齐（本次） |

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
- 部分 change 内部拆分为多个子 change 归档；它们与无编号跟进统一登记在「补充归档索引」。
- #10 原名 `dynamic-catalog-and-query`，实际由 `dynamic-sim-features` change 实现（动态目录/报警、录像与回放、移动位置、运行时触发 API）。
- 已完成 39 个编号（#1–#39），对应 55 个归档 change 目录（2026-10-26 同步）。
- 当前无待实施条目：全部登记阶段已归档。下一阶段候选待规划，新增时从 #40 起。
- 路线图同步契约见 capability `docs-roadmap-sync`；每次归档必须在同提交更新本文件。
