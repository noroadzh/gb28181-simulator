# Design

## Context

参见 proposal.md。

## Goals / Non-Goals

**Goals:**

- 消除浏览器控制台 TypeError，flv.js 能正常初始化。
- 修复 FLV 拉流 URL 格式，使前端请求能到达后端 `GET /v1/flv/:nodeID/:channelID` 端点。
- 通道详情页"开始播放"能显示实时视频。
- 录像回放功能不受影响。

**Non-Goals:**

- 不引入新的后端接口或路由。
- 不修改 flv.js 版本或替换播放器库。
- 不引入 Web Worker 替代方案（对 1-2 路并发流性能影响可忽略）。

## Decisions

### 决策 1：关闭 Web Worker 而非升级/降级 flv.js

**选项 A（采用）**：关闭 Web Worker（`enableWorker: false`）。
- 优点：零依赖改动，改动量 1 行，消除 TypeError 立即生效。
- 缺点：主线程承担 FLV 解析，对 1-2 路并发流帧率影响可忽略。

**选项 B**：升级/降级 flv.js 版本。
- 缺点：版本兼容性风险，Vite 5 生态下无明确稳定组合，需测试验证。

**选项 C**：手动配置 Web Worker 的 `workerType` 和 `lazyLoadMaxMs`。
- 缺点：flv.js 1.6.2 API 限制，仍依赖 Blob URL + importScripts，绕过复杂度高。

### 决策 2：flvUrl 使用相对路径而非修复硬编码端口

**选项 A（采用）**：改 `api.flvUrl()` 为相对路径 `/v1/flv/...`。
- 优点：端口自动跟随 Dashboard 当前 origin，彻底消除硬编码风险；与 `api.js` 中其他 API（如 `listChannels`）风格一致。
- 缺点：无。

**选项 B**：将后端 HTTP 端口从 18080 改为 18090。
- 缺点：破坏默认配置一致性，需要用户同时改 YAML + 可能影响其他依赖。

**选项 C**：保留硬编码 `18090`，在 `registerRoutes` 添加兼容路由。
- 缺点：遗留硬编码，且与 `/v1` 前缀 + `.flv` 后缀的不匹配问题仍需解决。

## Risks / Trade-offs

- **风险**：关闭 Worker 后主线程负载略升。**缓解**：对模拟器场景（1-2 路并发），主线程解码 FLV 足够流畅，实测无感知。
- **风险**：前端用 `BASE=''` 相对路径，若 Dashboard 通过子路径（`/dashboard/`）部署则 URL 会失效。**缓解**：当前 Vite embed.FS 以根路径 `/` 嵌入，无子路径问题；未来如需子路径部署可改为 `${location.pathname}/v1/flv/...` 或在构建时注入 `BASE_URL`。
