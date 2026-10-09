# dashboard-stats Specification

## Purpose
为 Dashboard 页面提供节点/通道/流的聚合统计指标。该能力定义 `/v1/stats` 端点的契约与刷新策略。

## Requirements

### Requirement: 聚合统计 API
- `GET /v1/stats` MUST 返回 JSON:
  ```json
  {
    "total_nodes": 5,
    "online_nodes": 4,
    "total_channels": 12,
    "online_channels": 10,
    "offline_channels": 2,
    "active_streams": 3,
    "media_kind_breakdown": {"file": 8, "synthetic": 2, "rtsp": 2}
  }
  ```

#### Scenario: 查询统计
- **WHEN** GET /v1/stats
- **THEN** 200 + JSON

### Requirement: 缓存与刷新
- 统计 MUST 在每次查询时实时聚合（避免数据过期），单次聚合成本 < 10ms
- 响应 MUST 包含 `Cache-Control: no-cache` 头

#### Scenario: 实时刷新
- **WHEN** Dashboard 每 5 秒轮询
- **THEN** 数字随节点/通道/流变化而更新

### Requirement: 媒体源类型分布
`media_kind_breakdown` MUST 按 `MediaConfig.kind` 统计 channel 数（file/rtsp/hls/synthetic），用于 Dashboard 饼图。

#### Scenario: 媒体源分布
- **WHEN** 系统中有 file 类型 8 通道、rtsp 类型 2 通道
- **THEN** `media_kind_breakdown` 为 `{"file":8, "rtsp":2, ...}`
