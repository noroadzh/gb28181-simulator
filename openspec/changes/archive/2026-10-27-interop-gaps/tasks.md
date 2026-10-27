# Tasks

## 1. 设备实时视频 INVITE

- [x] 1.1 解析实时视频 offer（`c=IN IP4` + `m=video` 端口）与通道（`To` 的 user）；验收：单测覆盖正常、缺 c=、缺 m=video、无 To user
- [x] 1.2 设备分支受理视频 INVITE：200 OK + sendonly SDP（PS/90000）；无媒体源 404；不被 488；验收：单测覆盖 200/404
- [x] 1.3 起流：`PacketizeOutbound` 把通道源打到 offer 端点，BYE 关闭端口即停；验收：单测断言 UDP 收到 PS 开头的 RTP，BYE 后端口释放
- [x] 1.4 回归：既有对讲（音频）与回放（`s=Playback`）不分派错；验收：既有测试全绿

## 2. 收尾（本项）

- [x] 2.1 `docs/web-ui-guide.md` §5.6 增补「真实平台向设备拉实时流」一行；验收：人工审阅
- [x] 2.2 `openspec validate interop-gaps --strict --type change`、`make lint`、`make test` 通过；验收：退出码 0

## 后续（同 change，另行排期）

- [x] 3.1 GB35114 接线：把 `internal/adapter/sm` 注入 acceptor（SM2 互认证、SM3 Note/Digest）；采用进程级身份 `security.gb35114`，acceptor 与 registrar 双向注入，registrar 验证平台 SecurityInfo；验收：单测覆盖注入/未注入、签名通过/失败、配置缺文件
- [x] 3.2 附录 D：RTP over TCP 媒体传输；验收：设备侧 video / playback 按 m-line 选择 UDP 或 TCP，active/passive 双向覆盖，TCP 包带 2 字节长度前缀，无法承载回 488；平台侧主动发起的对话仍为 UDP（模拟器作为 offerer 时不声明 TCP）
- [x] 3.3 CANCEL / re-INVITE / 487 等异常流；验收：CANCEL 在事务挂起时回 200 并给 INVITE 回 487，事务已终结时回 481；device 节点应答 BYE/CANCEL 不再 405；re-INVITE 关闭旧媒体并重新协商
