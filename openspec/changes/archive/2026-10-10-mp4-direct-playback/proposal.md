# Proposal: mp4-direct-playback

## Why

通道详情页目前对所有媒体源统一经 PS→FLV 转封装后用 flv.js 播放（`/v1/flv/{nodeID}/{channelID}`）。MP4 文件源走该链路时持续报错（转封装 metadata 解析/时序问题），且浏览器本可直接原生播放 MP4，无需转封装。需要按源类型在界面层分派播放器：MP4 直播、其余走 flv.js，提升 MP4 场景的稳定性与体验。

## What Changes

- 后端新增媒体文件直出端点 `GET /v1/nodes/{nodeID}/channels/{channelID}/media-file`：
  - 解析通道级媒体配置，未配置时回退节点级；仍无配置返回 404
  - 仅 `kind=file` 可服务，其他 kind 返回 400；文件不存在或非普通文件返回 404
  - 基于 `http.ServeContent` 输出，自动支持 HTTP Range（206 Partial Content），流式读取
  - 文件路径仅来自已保存的媒体配置、不接受请求参数，天然防目录穿越
- 前端通道详情页实现双模式播放分派：
  - 播放前解析媒体配置（通道级 → 节点级回退）
  - `kind=file` 且路径以 `.mp4` 结尾（忽略大小写）时用浏览器原生 video 播放 media-file URL
  - 其余源维持既有 flv.js 链路不变
  - 原生播放失败（如 H.265 编码浏览器不可解）时一次性自动回退 flv.js
  - 播放控制栏按钮文案、"复制播放地址"按钮与"拉流地址"卡片跟随当前实际播放模式展示/复制

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `channel-http-api`: 新增"媒体文件直出"需求——按节点+通道直出 MP4 文件，支持 Range 与配置回退解析
- `channel-web-ui`: 新增"播放模式分派"需求——MP4 文件源原生播放、其余源 flv.js、原生失败一次性回退、控制栏与地址展示跟随实际模式

## Impact

- 后端：`internal/interface/http/`（新增 media_file.go + 单测；server.go 注册路由）——仅新增端点，FLV 既有链路零改动
- 前端：`web/src/api.js`（新增 mediaFileUrl）、`web/src/views/ChannelDetailView.vue`（播放分派逻辑）
- 构建产物：web 重建后 embed 进 Go 二进制
- 路线图阶段：对应 **#14 web-management-ui** 的体验增强延伸

## Non-goals

- 不改动 FLV 转封装网关（`flv-media-gateway`）的任何行为
- 不做视频转码：MP4 内编码若浏览器不可解（如部分 H.265），仅依赖一次性回退 flv.js，不做编码探测/转码
- 不覆盖录像回放页（RecordView）的分派逻辑，本 change 仅针对通道详情页实时播放
- 不改变 GB28181 协议层（SIP/MANSCDP/PS/RTP）任何行为
- 不引入新的前端播放器依赖（不新增 hls.js/mpegts.js 等）
