# Delta: channel-web-ui

## ADDED Requirements

### Requirement: 播放模式分派

通道详情页实时播放 MUST 在启动播放前解析通道级媒体配置，未配置时回退节点级媒体配置，并据此选择播放模式：

- `kind=file` 且媒体文件路径以 `.mp4` 结尾（不区分大小写）时，MUST 使用浏览器原生 `<video>` 元素播放 media-file URL
- 其他场景（非 file 源、非 .mp4 文件、原生播放失败）MUST 复用既有 flv.js 播放链路（`/v1/flv/{nodeID}/{channelID}`）
- 原生播放失败（如浏览器不支持该编码）MUST 在播放区内联提示错误，并一次性自动回退 flv.js
- 播放控制栏的"开始播放/停止播放"按钮、"复制播放地址"按钮，以及"拉流地址"卡片 MUST 始终展示当前实际播放 URL

#### Scenario: MP4 原生播放

- **WHEN** 通道（或回退到节点）的媒体配置为 `kind=file` 且路径以 `.mp4` 结尾
- **THEN** 页面使用原生 `<video src="/v1/nodes/{nodeID}/channels/{channelID}/media-file">` 播放，不初始化 flv.js

#### Scenario: 非 MP4 走 flv.js

- **WHEN** 通道（或回退到节点）的媒体配置为 `kind=synthetic` 或 `kind=rtsp` 或 `kind=hls`，或 kind=file 但路径非 .mp4
- **THEN** 页面初始化 flv.js player，拉流地址为 `/v1/flv/{nodeID}/{channelID}`

#### Scenario: 原生播放失败回退 flv.js

- **WHEN** 页面尝试原生播放，但 video 元素触发 error（如浏览器不支持编码、解码失败）
- **THEN** 页面立即切换到 flv.js 播放链路，video 元素重新 attach flv.js player，播放区展示错误提示

#### Scenario: 控制栏与地址卡片跟随模式

- **WHEN** 播放模式为原生 video
- **THEN** "复制播放地址"按钮复制 `/v1/nodes/{nodeID}/channels/{channelID}/media-file`，"拉流地址"卡片同步展示该 URL

- **WHEN** 播放模式为 flv.js
- **THEN** "复制播放地址"按钮与"拉流地址"卡片展示 `/v1/flv/{nodeID}/{channelID}`
