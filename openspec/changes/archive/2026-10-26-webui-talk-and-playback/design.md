# Design

## Context

后端契约已定：`/talk/audio`（二进制 PCMU 帧，上下行）与 `/playback/flv?sn=`（回放窗口内的 FLV）。前端是 Vue 3 + Element Plus + Vite，经 `embed.FS` 内嵌；仓库没有 JS 测试框架，验收靠 `pnpm build` + 手工路径。

## Decisions

### Decision 1: μ-law 编解码放在前端工具模块

浏览器没有 PCMU 编码器。G.711 μ-law 是十几行的算术，放在 `web/src/utils/ulaw.js`（encode/decode + 往返测试注释），不在后端加转码。

### Decision 2: 8kHz 采集优先，失败回退重采样

`new AudioContext({sampleRate: 8000})` 在 Chrome/Safari 可用；若浏览器拒绝该采样率，则用默认采样率并以固定步长抽取到 8kHz。用 `ScriptProcessorNode`（虽已废弃但零构建成本）而不是 AudioWorklet，避免为这个功能引入打包器配置。

### Decision 3: 回放会话由「点击条目」创建

records 响应保持现状（无 `playback_url`）。点击条目 → `POST /playback {play, sn, start, end}` → 成功后用 `/playback/flv?sn=` 播放；失败直接展示错误。离开页面或停止时发 `teardown`，避免设备侧一直供流。

### Decision 4: 对讲与回放互斥

同一通道页面上，开始对讲前先停止正在播放的回放，反之亦然——两者都占用设备媒体面，模拟器不做混流。

## Risks / Trade-offs

- **R1 ScriptProcessorNode 的延迟** → 10–20ms 缓冲可接受；文档写明延迟不作为优化目标。
- **R2 浏览器自动播放策略** → 下行播放需要用户手势触发；对讲本身由点击开始，满足策略。
- **R3 无前端测试** → 验收以 `pnpm build` + 手工清单为准，清单写进文档。
