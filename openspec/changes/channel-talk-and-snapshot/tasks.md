# Tasks

## 1. 语音对讲

- [ ] 1.1 `TalkStart`：构造 INVITE（SDP PCMU sendrecv），等待 200 OK 并记录对话；验收：单测断言 INVITE body 的 SDP 与返回的 session id/WSS 地址
- [ ] 1.2 音频桥：把入站 RTP 音频帧经 WSS 推给浏览器；验收：单测断言帧率/编码转发，浏览器侧手工验证
- [ ] 1.3 `TalkStop`：发 BYE 并回收；重复 stop 幂等；对端 BYE/超时回收；验收：三条单测
- [ ] 1.4 错误语义：非 device 501、对端不可达 502、无 200 OK 504；验收：单测覆盖

## 2. 快照抓图

- [x] 2.1 定义 `port.FrameRenderer`（按 `model.MediaConfig` 产出一帧 JPEG）与两个哨兵（`ErrFrameRenderUnsupported`、`ErrSnapshotNoMedia`）；验收：接口与哨兵可被 app 与适配器各自引用，`go build ./...` 通过
- [x] 2.2 适配器实现合成测试图渲染器（复用 `SyntheticSource.renderFrame` → `image/jpeg`）并在 main 注入；`Snapshot` 读取通道媒体配置后调用渲染器；验收：适配器单测断言 JPEG magic 与尺寸，app 单测用假渲染器断言透传
- [x] 2.3 失败语义：无媒体源 503、非 device 501、需要解码的编码 501；HTTP 映射补齐；验收：三条单测
- [ ] 2.4 e2e：对配置本地文件源的 device 抓拍并断言 JPEG；验收：`go test ./internal/test/e2e/...` 通过

## 3. 收尾

- [ ] 3.1 文档：`docs/web-ui-guide.md` 增补对讲/抓拍的使用前提与失败码；验收：人工审阅
- [ ] 3.2 `openspec validate channel-talk-and-snapshot --strict --type change`、`make lint`、`make test` 通过；验收：退出码 0
