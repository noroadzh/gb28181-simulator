# Proposal

## Why

后端已经把对讲音频（`/talk/audio` WebSocket）与回放 FLV（`/playback/flv?sn=`）做实，但前端两处仍是断的：

- **对讲**：`ChannelDetailView` 的「开始对讲」只调 `talk/start` 建立 SIP 对话，全仓没有 `getUserMedia` / `AudioContext` / `talk/audio`，所以对话建立了却没有声音。
- **回放**：`RecordView.playRecord` 依赖 `rec.playback_url`，而 `recordResponse` 从来没有这个字段——点「录像回放」永远提示「该录像没有回放URL」。

## What Changes

- **对讲音频**：开始对讲时 `getUserMedia`（8kHz 单声道）→ μ-law 编码 → `/talk/audio` 二进制帧；下行帧 μ-law 解码后经 `AudioContext` 播放；停止时关 WebSocket 并调 `talk/stop`。
- **回放播放**：点某条录像时先 `POST /playback {action:"play", sn, start, end}`，成功后用 `/playback/flv?sn=<sn>` 作为 flv.js 的地址；离开页面或点停止时发 `teardown`。
- **不在 records 响应里塞 `playback_url`**：会话是运行时概念，由前端按条目发起。

**BREAKING**：无。两处当前都不可用。

## Capabilities

### Modified Capabilities

- `channel-web-ui`：补充「对讲音频」与「回放播放」两节的可用契约。

对应路线图编号：#39 `webui-talk-and-playback`。

## Impact

- `web/src/views/ChannelDetailView.vue`（对讲按钮 + 音频管线）
- `web/src/views/RecordView.vue`（回放会话 + FLV 地址）
- `web/src/api.js`（`startPlayback` / `stopPlayback` / `playbackFlvUrl`、`talkAudioUrl`）
- 不改后端、不改数据库。

## Non-goals

- 不做回声消除、混音、多方对讲。
- 不支持 PCMU 之外的编码（浏览器侧只做 μ-law）。
- 不重做通道详情页/回放页的视觉设计。
- 不把回放做成「边下边存」或断点续传。
