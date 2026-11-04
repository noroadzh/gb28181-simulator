# GB/T 28181 实现覆盖与架构分析

> 对照文档：`docs/GBT 28181-2016 …（扫描带目录版）.pdf`（218 页，目录完整）  
> 证据来源：`openspec/specs/*`、`docs/roadmap-15-steps.md`、`docs/architecture.md`、`docs/web-ui-guide.md`、`internal/app/acceptor_*.go`、`internal/adapter/manscdp/`  
> 日期：2026-11-04  
> 结论先行：**核心业务章节（注册→目录→实时/回放/下载→控制→订阅→对讲→级联）已可模拟联调；缺口集中在 DeviceControl 子类型、媒体安全面、校时/保活附录、以及文档漂移与部分 god-object。架构方向正确，但 Acceptor/NodeService 过重，级联信令/媒体双路径需刻意维护。**

---

## 1. 对照国标章节的覆盖矩阵

图例：✅ 已实现可联调 · 🟡 部分/模拟级 · ❌ 未做或明确非目标 · ➕ 模拟器增强（超国标）

### 1.1 第 7 / 9 章：控制与流程

| 国标条款 | 能力 | 状态 | 实现要点 / 缺口 |
|----------|------|------|-----------------|
| 7.1 / 9.1 注册注销 | Digest REGISTER、Expires=0 注销 | ✅ | `device_registrar.go`、`acceptor_register.go`；可选免认证 |
| 9.1.2.2 证书双向认证注册 | 数字证书双向认证 | 🟡 | GB35114 SM2 互认证（进程级密钥）；非完整附录 I 证书/CRL 体系 |
| 7.2 / 9.2 实时点播 | INVITE `s=Play` + PS/RTP | ✅ | 设备应答 + 平台拉流 `#40/#41`；附录 D TCP active/passive |
| 7.3 / 9.3 设备控制 | PTZ / 布防 / TeleBoot 等 | 🟡 | 方向/变倍/聚焦 ✅；光圈/预置位调用 501；无 RecordCmd/IFrame/DragZoom/TargetTrack |
| 7.4 / 9.4 报警 | 通知 + 查询 + 布防撤防 | ✅ | Alarm 通知/查询；GuardCmd 布防（规格写 AlarmCmd，线协议用 GuardCmd） |
| 7.5 / 9.5 设备信息查询 | DeviceInfo / DeviceStatus / Catalog / Config | 🟡 | Catalog/DeviceInfo/DeviceStatus/ConfigDownload(BasicParam) ✅；完整 DeviceConfig 矩阵 ❌ |
| 7.6 / 9.6 状态报送 | Keepalive | ✅ | MESSAGE Keepalive；刷新 last_seen 不延长 Expires |
| 7.7 / 9.7 录像检索 | RecordInfo | 🟡 | 设备侧合成目录；平台侧可查真实录像库 `#42` |
| 7.8 / 9.8 历史回放 | INVITE Playback + MANSRTSP | 🟡 | 会话/FLV ✅；入站 PlaybackControl MESSAGE 仅 ACK，真实控制走 INFO |
| 7.9 / 9.9 文件下载 | INVITE `s=Download` | ✅ | `#44`；可写入平台录像库 |
| 7.10 / 9.10 网络校时 | Date / 校时 | ❌ | 未见生产处理路径 |
| 7.11 / 9.11 订阅通知 | catalog / alarm / mobileposition | ✅ | SUBSCRIBE + NOTIFY；未知 Event → 489 |
| 7.12 / 9.12 广播与对讲 | Broadcast + Talk | 🟡 | 对讲 WS↔PCMU ✅；广播 ack + 设备主动音频 INVITE，不进对讲桥（有意） |

### 1.2 附录与传输/安全

| 附录 / 要求 | 状态 | 说明 |
|-------------|------|------|
| A MANSCDP 命令集 | 🟡 | 主路径命令齐全；DeviceControl 子命令与完整配置集有缺口 |
| B MANSRTSP | 🟡 | PLAY/PAUSE 等 INFO 路径有；与 MESSAGE PlaybackControl 语义未完全打通 |
| C PS/RTP 封装 | ✅ | PS 打包 + RTP；基本流封装以源能力为准 |
| D 统一编码 / 附录 L TCP 媒体 | ✅ | 节点 ID 规则；附录 D TCP 媒体已做（路线图曾称附录 D） |
| E 编解码档次一致性 | 🟡 | 透传/封装为主，不做严格一致性测试器 |
| F SDP | ✅ | Play/Playback/Download/Talk |
| G 与其他系统接口 | ❌ | 卡口/接警等外部系统消息格式未做 |
| H 数字摘要认证 | ✅ | MD5 Digest；可选 SM3 |
| I 证书/CRL | ❌ | 非目标（用进程级 SM2 密钥文件替代） |
| J 信令示范 | ➕ | 靠 e2e/sipprobe/抓包验证，非文档复刻 |
| K Subject | 🟡 | 按场景使用，未单独成章验证 |
| M 媒体流保活 | ❌ / 弱 | 未见独立媒体 RTCP/保活规范实现 |
| N 多响应消息 | 🟡 | Catalog 多包等有部分支持，需按对接压力验证 |
| O/P 目录区段 / 域间订阅 | 🟡 | 2022 区段类型有；跨域自动发现 H.3 明确不做 |
| 第 8 章 / GB35114 媒体面 | 🟡 | 信令 SM3+SM2 ✅；SRTP/SM4 媒体加密 ❌；SIP TLS 仅 transport 壳，未做生产级 |

### 1.3 模拟器增强（非国标必选）

| 能力 | 状态 |
|------|------|
| 多节点同进程（device / platform-large / platform-small） | ✅ |
| 级联 MESSAGE 中继 + 媒体 B2BUA | ✅ |
| 故障注入 / 抓包 pcap / YAML 场景引擎 | ✅ |
| Web UI + HTTP 运维 API + 实时日志 | ✅ |
| 媒体源配置（file/HLS/RTSP/合成）+ HTTP-FLV | ✅ |
| 节点/通道/账号 SQLite 运行期 CRUD | ✅ |
| Catalog `<Extension><MediaSource>` 扩展 | ➕ 非标扩展，便于级联媒体源同步 |

---

## 2. 已做 / 未做（精简清单）

### 已扎实覆盖（可对外宣称“能模拟”）

1. 三种身份生命周期与 Digest 注册/注销/心跳  
2. Catalog 查询应答与平台目录聚合、级联通道展开  
3. 实时点播、回放、下载（UDP + 附录 D TCP）  
4. 平台侧拉流、分段录像、上级 RecordInfo/回放应答  
5. 级联信令中继 + 双对话媒体桥  
6. 订阅（目录/报警/移动位置）  
7. 对讲、运维六类命令（状态/广播/报警/配置/升级重启/下载）  
8. 可选 GB35114 信令安全与 X-GB-Ver 2022 门控增量  

### 明确未做或仅占位

| 项 | 建议优先级 |
|----|------------|
| DeviceControl：RecordCmd / IFrameCmd / DragZoom / TargetTrack | P1（真实平台常测） |
| PTZ 光圈、预置位召回编码 | P1 |
| 入站 PlaybackControl 真正驱动会话（现仅 200） | P1 |
| 网络校时（7.10） | P2 |
| 媒体流保活 / RTCP（附录 M） | P2 |
| 编码源快照解码（现 501） | P2（引入解码器成本高） |
| SIP TLS / SRTP / 媒体面国密 | P2–P3（安全联调时） |
| 完整 DeviceConfig、真实升级重启副作用 | P3（模拟器语义可保持“记录即可”） |
| 附录 G 外部系统、H.3 跨主机路径发现 | P3 / 非目标 |
| Web UI「平台拉流/录像库」一等入口 | P1 产品面（路线图 #45 候选） |

---

## 3. 现存问题与解决方案

### P0 / 文档与契约漂移

| 问题 | 证据 | 方案 |
|------|------|------|
| `architecture.md` 仍写「FLV 历史流尚未接入」 | §回放 ~L277；与 #37、`web-ui-guide` 矛盾 | 立刻改文档：标明 `/playback/flv?sn=` 已交付；删除过期段落 |
| `node_service` 注释仍写 outbound DeviceControl 未实现 | 与 `PTZ()` 实际发 MESSAGE 矛盾 | 删/改正注释；加一次文档一致性检查进 `docs-roadmap-sync` |
| 规格 `AlarmCmd` vs 线协议 `GuardCmd` | `device-command-coverage` vs `acceptor_ops` | 规格改成与 XML 字段一致，或双名兼容并写清 |

### P1 / 协议语义不完整（联调会踩）

| 问题 | 影响 | 方案 |
|------|------|------|
| 入站 `PlaybackControl` 只 ACK | 只发 MESSAGE 控制的平台认为“支持了”但媒体不动 | `handlePlaybackControl` 接到 Play/Pause/Teardown/Scale → 调 `PlaybackPort` / 会话状态（与 MANSRTSP INFO 共用） |
| 设备侧 PTZ 只打日志 | 抓包好看、状态不可观测 | 在 profile 维护 pan/tilt/zoom 状态；可选回 DeviceStatus/精确位置 |
| 缺 RecordCmd / IFrame / 预置位 / 光圈 | 部分平台控制面板报错或 501 | 按附录 A 补 Cmd 子类型：不支持的仍明确 ERROR/501，支持的写入状态机 |
| 级联 MESSAGE 中继 ≠ 媒体 | 误以为“级联全通” | UI/帮助文档写清：媒体靠 B2BUA；无 INVITE 中继；验收用例分信令/媒体两套 |
| platform-small splitTransport 事务匹配不足 | 上游主动 INVITE/SUBSCRIBE 边界易丢应答 | 补 Call-ID 事务表 + e2e（已在 architecture 标明未来需求） |

### P2 / 产品与工程债

| 问题 | 方案 |
|------|------|
| `node_service.go` ~1411 行、acceptor 测试 ~1600 行 | 按用例切 `node_*_service`（lifecycle / media / control / ops）；测试按 CmdType 分包 |
| Acceptor 选项爆炸 → nil 时 HTTP 501 | 启动期断言必选 port；或按 NodeKind 组装不同 Acceptor 变体，避免“万能结构体” |
| 录像检索设备侧为合成数据 | 文档标明“合成目录”；可选挂接本地文件时间索引 |
| 快照仅合成源 | 保持 501 + 说明；或可选 FFmpeg/纯 Go 解码插件（CGO 需评估与 `CGO_ENABLED=0` 冲突） |
| 场景引擎未注入时 501 | 启动 wiring 失败要显式日志；Web 提示“引擎未装配” |

### P3 / 安全与附录

| 问题 | 方案 |
|------|------|
| 无校时 / 弱媒体保活 | 需要联调再补：应答 `Date`；媒体侧可选 RTCP RR 或应用层保活定时器 |
| 无 SRTP/TLS 生产路径 | 单独立项；信令 TLS 与媒体 SRTP 分开；保持默认关闭 |

---

## 4. 架构是否符合逻辑？

### 4.1 结论：**方向正确，细节有压力点**

六边形 + 严格单向依赖（`domain` 仅标准库、`app` 只依赖 port）与「模拟器要可替换传输/编解码」的目标匹配。OpenSpec 驱动 + 诚实 501 的失败语义，比静默 200 更适合对接排障。

### 4.2 符合逻辑之处

1. **身份模型清晰**：device / platform-large / platform-small 对应国标前端、上级、下级接入；小平台 = 服务半 + 注册半，符合级联拓扑。  
2. **socket 唯一读者在 Acceptor**：录像查询、中继应答、控制事务放在 acceptor 内，避免多读者抢包——这是正确取舍。  
3. **端口隔离媒体与信令**：`SIPTransport` / `PlaybackPort` / `Live` / `Recording` 分离，HTTP 与 SIP 共享用例层。  
4. **级联拆成 MESSAGE 中继 + 媒体 B2BUA**：实现成本可控；只要文档不把两者混为一谈，逻辑自洽。  
5. **配置真源迁 SQLite**：运行期改节点不改 YAML，符合管理面需求。

### 4.3 架构压力 / 不完全贴合之处

| 点 | 为何别扭 | 建议演进 |
|----|----------|----------|
| 单一 `Acceptor` 服务三种身份 | 选项/分支过多，device 与 platform 路径缠在一起 | 按 Kind 拆 `DeviceUA` / `PlatformUAS`，共享 SIP 工具库 |
| `NodeService` 上帝对象 | 生命周期、媒体、控制、运维全堆一处 | 按边界拆服务，NodeService 变门面 |
| 级联信令与媒体双路径 | 状态不同步时难排查 | 引入 `CascadeSession` 聚合 Call-ID/通道/媒体腿的观测视图 |
| 测试大量 `app → adapter` | 单元纯度下降 | 保留 e2e；纯逻辑测 fake port |
| TLS 在 transport 层“名义支持” | 易误判已安全交付 | 配置/文档标注 experimental；或拿掉直到真实现 |

### 4.4 总体判断

- **作为 GB28181 模拟器架构：合格且可演进。**  
- **作为“国标全文实现”：不是目标，也不应宣称。**  
- 最大风险不是分层错误，而是：**能力表面很全 → 局部语义浅（PlaybackControl、PTZ 状态、DeviceControl 子类型）→ 真实平台联调时表现为“半通”。** 解决路径应是补语义深度与文档诚实度，而不是推翻六边形。

---

## 5. 建议的下一阶段（从 #45 起）

规划状态（2026-11-04）：**#45 / #46 已归档并提交**（`4baaf18`）。

1. **#45 `docs-sync-gb28181-coverage`** — ✅ `openspec/changes/archive/2026-11-04-docs-sync-gb28181-coverage/`  
2. **#46 `interop-hardening`** — ✅ `openspec/changes/archive/2026-11-04-interop-hardening/`  
3. **ux-platform-media**（#47 候选）：Web UI 平台拉流/录像库入口  
4. **cascade-observability**：级联会话统一视图 + platform-small 事务匹配 e2e  
5. **security-media**（按需）：SIP TLS、SRTP/国密媒体面  

---

## 6. 快速参考：CmdType 现状

```
FULL: Keepalive, Catalog, DeviceInfo, RecordInfo, Alarm(+query),
      DeviceStatus, Broadcast, ConfigDownload, DeviceUpgrade,
      DeviceControl(PTZ方向变倍聚焦 / Guard / TeleBoot),
      PresetQuery, MediaStatus, MobilePosition(NOTIFY),
      HomePosition* / CruiseTrackList* / SnapShot*  (*2022 门控)

PARTIAL: PlaybackControl(出站完整 / 入站 ACK), PTZ(无光圈预置位),
         Config(仅 BasicParam), Upgrade/TeleBoot(只记状态)

MISSING: RecordCmd, IFrameCmd, DragZoom, TargetTrack,
         完整 DeviceConfig, 校时, 媒体保活/RTCP, SRTP, 附录 G
```
