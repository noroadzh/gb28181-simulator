# Design

## Context

当前模拟器已具备单设备多通道的数据模型（`NodeProfile.Channels`）、全量 MANSCDP acceptor（PTZ/RecordInfo/PlaybackControl/DeviceControl/PresetQuery/HomePosition）和 Web UI 骨架。缺失的是：通道粒度的前端可视化、浏览器可播放的实时流分发、前端播放器、PTZ 控制面板、录像回放、语音对讲和用户认证。

## Goals / Non-Goals

**Goals：**
- 在 Go 进程内内置 HTTP-FLV 网关，复用现有 PS+RTP 管道，无需引入第三方进程
- 前端使用 flv.js + Element Plus 构建专业监控风格界面（深色主题 + 玻璃拟态）
- PTZ/录像/对讲通过 HTTP API 暴露，前端交互以 RESTful 调用
- 通道粒度 MediaConfig 向后兼容单实例模式

**Non-Goals：**
- 不引入 ZLMediaKit、EasyPlayer-Pro 等商业组件
- 不实现报警查询、算法仓、服务扩展（用户已明确排除）
- 不实现服务器侧录像存储（仅支持设备端录像查询和回放）
- 不实现 ONVIF 接入、分组管理、布局预设（已登记本期不做）

## Decisions

### 1. 内置 PS→HTTP-FLV 网关，而非集成 ZLMediaKit
- **Rationale**：保持纯 Go、零外部依赖、镜像自包含，符合模拟器定位；demo 规模并发足够
- **Alternative**：集成 ZLMediaKit 进程外服务
- **Trade-off**：性能不及 ZLMediaKit，但 demo 和测试场景完全可用；后续若需提升可升级

### 2. MediaConfig.ByChannel 空 map 回退单实例
- **Rationale**：不破坏已有 API 和 YAML 配置；现有 `media:` 配置继续生效
- **Alternative**：强制所有 device 节点显式配置 channels
- **Trade-off**：增加一层回退逻辑，但接口和行为完全向后兼容

### 3. 前端播放器选 flv.js（BSD 协议）
- **Rationale**：无商业授权风险，社区成熟，HTTP-FLV 是 EasyGBS 主流协议之一
- **Alternative**：集成 EasyPlayer-Pro（商业付费）或 hls.js
- **Trade-off**：flv.js 不支持 iOS Safari（Safari 支持 HLS）；一期主要针对 PC 端浏览器

### 4. Talk 音频用 PCMU 编码
- **Rationale**：国标设备原生支持 PCMU，零转码成本；浏览器端 AudioContext 可直接解码 PCMU
- **Alternative**：AAC（浏览器原生支持但编码复杂）
- **Trade-off**：PCMU 音质一般，但对讲场景足够

### 5. WebSocket 上行对讲音频（而非 HTTP 长轮询）
- **Rationale**：WebSocket 低延迟、全双工、与现有 gorilla/websocket 技术栈一致
- **Alternative**：HTTP 流式上传（复杂且延迟高）

## Risks / Trade-offs

| 风险 | 缓解 |
|------|------|
| HTTP-FLV 网关并发订阅导致内存增长 | 设置空闲超时（30s），单订阅占内存 < 2MB |
| 浏览器不支持 FLV（Safari） | 一期不覆盖 Safari；后续可加 HLS 降级 |
| 对讲音频抖动 | 加 20ms 音频缓冲 + 抖动缓冲 |
| 多通道设备 Catalog 响应变大 | 限制单设备最多 64 个通道（GB/T 28181 典型上限） |
| 修改 MediaConfig 影响已有 #19 行为 | 通过 `ByChannel == nil` 路径保证绝对向后兼容 |

## Migration Plan

1. **不涉及数据迁移**：MediaConfig.ByChannel 空 map 时完全回退旧行为
2. **渐进式部署**：先升级后端（step 1-4），再部署前端（step 5）
3. **回滚策略**：`ByChannel` 在 config 中不配置，启动行为与 #19 完全相同；若 flv 网关出问题可切换端口或禁用

## Open Questions

无；本期范围与实现路径已确认。
