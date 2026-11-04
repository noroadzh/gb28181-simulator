# GB/T 28181 实现覆盖对照

对照文档：`docs/GBT 28181-2016 …（扫描带目录版）.pdf`。  
状态：✅ 已实现可联调 · 🟡 部分/模拟级 · ❌ 未做或明确非目标 · ➕ 模拟器增强。  
维护：协议面 change 归档时须同步更新本表（见 capability `docs-roadmap-sync` / `protocol-coverage-docs`）。

## 第 7 / 9 章：控制与流程

| 国标条款 | 能力 | 状态 | 说明 |
|----------|------|------|------|
| 7.1 / 9.1 注册注销 | Digest REGISTER、Expires=0 | ✅ | device 注册 / platform 受理 |
| 9.1.2.2 证书双向认证 | GB35114 SM2 互认证 | 🟡 | 进程级密钥；非完整附录 I 证书/CRL |
| 7.2 / 9.2 实时点播 | INVITE `s=Play` + PS/RTP | ✅ | 设备应答 + 平台拉流；附录 D TCP |
| 7.3 / 9.3 设备控制 | PTZ / 布防 / TeleBoot 等 | 🟡 | 方向变倍聚焦/光圈/预置位/RecordCmd/IFrame ✅（模拟状态）；DragZoom/TargetTrack 显式 ERROR |
| 7.4 / 9.4 报警 | 通知 + 查询 + 布防撤防 | ✅ | 布防线协议字段为 `GuardCmd`（非 AlarmCmd） |
| 7.5 / 9.5 设备信息查询 | DeviceInfo / Status / Catalog / Config | 🟡 | Config 以 BasicParam 为主 |
| 7.6 / 9.6 状态报送 | Keepalive | ✅ | |
| 7.7 / 9.7 录像检索 | RecordInfo | 🟡 | 设备侧可为合成目录；平台可查录像库 |
| 7.8 / 9.8 历史回放 | INVITE Playback + FLV | ✅ | FLV `#37`；入站 PlaybackControl 驱动会话 `#46`（与 MANSRTSP INFO 共用） |
| 7.9 / 9.9 文件下载 | INVITE `s=Download` | ✅ | |
| 7.10 / 9.10 网络校时 | Date / 校时 | ❌ | |
| 7.11 / 9.11 订阅通知 | catalog / alarm / mobileposition | ✅ | |
| 7.12 / 9.12 广播与对讲 | Broadcast + Talk | 🟡 | 对讲完整；广播 ack + 音频 INVITE（不进对讲 WS 桥） |

## 附录与安全（摘要）

| 附录 / 要求 | 状态 | 说明 |
|-------------|------|------|
| A MANSCDP | 🟡 | 主路径齐全；DragZoom/TargetTrack 仍非完整几何模拟 |
| B MANSRTSP | ✅ | INFO PLAY/PAUSE 与 MESSAGE PlaybackControl 共用 PlaybackPort |
| C PS/RTP | ✅ | |
| 附录 L TCP 媒体 | ✅ | |
| H Digest | ✅ | 可选 SM3 |
| I 证书/CRL | ❌ | |
| M 媒体保活 / RTCP | ❌ / 弱 | |
| G 外部系统接口 | ❌ | |
| 第 8 章 / 媒体国密 SRTP | ❌ | 信令面 GB35114 可选 ✅ |

## 级联限制（必读）

- **MESSAGE 信令中继**：按 `X-RoutePath` / 下一跳转发 MANSCDP MESSAGE 并回传应答。  
- **媒体 B2BUA 中继**：上级对下级通道发实时视频 INVITE 时双对话桥接 PS/RTP。  
- **二者不等价**：信令中继**不会**自动转发 INVITE 或 RTP；跨主机附录 H.3 自动路径发现不在范围内。

## 模拟器增强

故障注入、抓包/pcap、YAML 场景引擎、Web UI / HTTP 运维、多节点同进程、Catalog `<Extension><MediaSource>` 等为模拟器能力，非国标必选章节。
