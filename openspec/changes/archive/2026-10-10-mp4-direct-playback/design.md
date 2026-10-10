# Design

## Context

现状与动机见 proposal.md。关键约束：

- 浏览器原生 video 播放 MP4 必须有支持 HTTP Range（206）的 URL；当前后端仅有 chunked 的 FLV 端点，无文件服务能力，上传文件存放在容器内 `uploads/` 目录，浏览器不可直接访问。
- 通道媒体配置已存在两级：通道级（`ChannelView.GetChannelMedia`）与节点级（`NodeView.GetMedia`），HTTP 层通过窄接口访问，app 层零改动即可完成回退解析。
- 前端 `ChannelDetailView.vue` 的 flv.js 链路（enableWorker:false 规避 worker 崩溃、METADATA_PARSED 后安全 play、AbortError 静默）已经稳定，必须保持不动。

## Goals / Non-Goals

**Goals:**

- 新增一个可被 `<video src>` 直接消费的媒体文件端点（Range/206、流式、Content-Type 正确）
- 前端按媒体配置分派播放模式，原生失败一次性回退 flv.js
- 两种模式共用既有 playing/playerError 状态与错误 UI，销毁逻辑统一清理

**Non-Goals:**

- 不做编码探测/转码；不做录像回放页分派；不改 FLV 网关
- 不引入新前端依赖

## Decisions

1. **用 `os.Open` + `http.ServeContent` 而非自研 Range 解析或 `http.ServeFile`**
   - `ServeContent` 自动处理 Range/If-Range/206/Content-Type（按扩展名推断 video/mp4），`os.Open` 返回的 `*os.File` 自带 `Seek`，满足接口；流式读取不全量载入内存。
   - `http.ServeFile` 会做额外的重定向/目录列表逻辑（path 以 `/` 结尾等），对固定文件场景多余且易引入意外行为。
2. **文件路径仅来自已保存配置，不接受查询参数**
   - 备选方案 `GET /v1/media/file?path=...` 需要额外的目录白名单校验防穿越；改为服务端从 MediaConfig 读取路径后天然免疫路径穿越，且语义（按通道取源文件）更贴合 UI 需求。
3. **配置回退解析放在 HTTP handler 内（ChannelView → NodeView）**
   - app 层已有两级独立查询接口；handler 内先通道级后节点级两次调用即可，不新增 app 层方法，保持窄接口不变。
4. **前端分派封装为独立函数，flv.js 既有函数（startFlv/destroyFlv）零语义改动**
   - 播放入口改为 `startPlay()`：解析配置 → 判定 `.mp4` → 原生模式（设置 `video.src` + 监听 error）或 `startFlv()`。停止/销毁统一走 `stopPlay()`：原生模式清空 src 并移除 error 监听；flv 模式复用 `destroyFlv()`。
5. **原生失败一次性回退 flv.js**
   - video `error` 事件触发时若尚未回退过，切到 flv.js；回退标志防止 flv.js 再次失败时形成循环。场景覆盖 H.265 MP4 在不支持浏览器上的播放。
6. **MediaConfig.Normalize 已将 `local_file` 别名归一为 `file`**，后端判定统一用 `SourceKindFile`；前端判定同样只认 `file` + `.mp4` 后缀（后端保存时已 Validate）。

## Risks / Trade-offs

- [MP4 内编码浏览器不可解（如部分 H.265）] → 原生 video error 事件一次性回退 flv.js；flv.js 也不支持时在播放区展示错误。
- [手填路径（非上传目录）文件被直出] → 路径仅来自用户自行配置的媒体源，与既有 PS 推流读取同一文件，未扩大暴露面；端点不接受任意路径参数。
- [多订阅端并发拉取同一文件] → `ServeContent` 每请求独立 `os.File` 句柄，无共享状态；文件句柄随请求结束关闭。
- [回退逻辑使"错误提示"与"自动重试"耦合] → 错误提示保留展示（用户知情），回退仅在原生模式发生一次。

## Migration Plan

纯增量：新增端点 + 前端分派。回滚 = 移除新路由与前端分派分支即可，无数据迁移。

## Open Questions

（无）
