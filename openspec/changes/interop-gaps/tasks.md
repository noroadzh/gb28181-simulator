# Tasks

## 1. 设备实时视频 INVITE

- [x] 1.1 解析实时视频 offer（`c=IN IP4` + `m=video` 端口）与通道（`To` 的 user）；验收：单测覆盖正常、缺 c=、缺 m=video、无 To user
- [x] 1.2 设备分支受理视频 INVITE：200 OK + sendonly SDP（PS/90000）；无媒体源 404；不被 488；验收：单测覆盖 200/404
- [x] 1.3 起流：`PacketizeOutbound` 把通道源打到 offer 端点，BYE 关闭端口即停；验收：单测断言 UDP 收到 PS 开头的 RTP，BYE 后端口释放
- [x] 1.4 回归：既有对讲（音频）与回放（`s=Playback`）不分派错；验收：既有测试全绿

## 2. 收尾（本项）

- [x] 2.1 `docs/web-ui-guide.md` §5.6 增补「真实平台向设备拉实时流」一行；验收：人工审阅
- [ ] 2.2 `openspec validate interop-gaps --strict --type change`、`make lint`、`make test` 通过；验收：退出码 0

## 后续（同 change，另行排期）

- [ ] 3.1 GB35114 接线：把 `internal/adapter/sm` 注入 acceptor（SM2 互认证、SM3 Note/Digest）
- [ ] 3.2 附录 D：RTP over TCP 媒体传输
- [ ] 3.3 CANCEL / re-INVITE / 487 等异常流
