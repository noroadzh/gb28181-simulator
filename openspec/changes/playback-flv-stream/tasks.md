# Tasks

## 1. 回放信令与设备供流

- [x] 1.1 回放 SDP 的构造与解析（`s=Playback`、`u=`、`t=`、`m=video`）；验收：**字节级 golden test**（构造与解析互逆）
- [x] 1.2 设备受理回放 INVITE：200 OK + SDP 或 404（无源/窗口越界）；验收：单测覆盖两条
- [ ] 1.3 设备按时间窗供流：定位到 start、按 end 停止，PS/RTP 发往平台端口；验收：单测断言首包与停止条件，日志记录 seek 退化
- [ ] 1.4 平台接流：回放 INVITE 成功后在该端口收 RTP，按 `node|channel|sn` 建 FLV 会话；验收：单测喂 RTP 断言 FLV 头

## 2. HTTP 与端到端

- [ ] 2.1 `GET /v1/nodes/:id/channels/:ch/playback/flv?sn=`；未知/未开流 404，开流后 200 + `video/x-flv`；验收：httptest 三条
- [ ] 2.2 e2e：真实二进制 → device + 本地文件源 → play（带时间窗）→ 取 FLV，断言 `FLV` 魔数与可解析首帧；验收：`go test ./internal/test/e2e/...` 通过

## 3. 收尾

- [ ] 3.1 `docs/web-ui-guide.md`：回放 FLV 从「未实现」改为可用，写明前提与 404 语义；验收：人工审阅
- [ ] 3.2 `openspec validate playback-flv-stream --strict --type change`、`make lint`、`make test` 通过；验收：退出码 0
