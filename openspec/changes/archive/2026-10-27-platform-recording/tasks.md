# Tasks

## 1. 录制与存储

- [x] 1.1 录像端口与文件系统存储：`Append` 按分段时长滚动写 `<node>_<channel>_<start>.ps`，启动扫描目录重建索引，`List(node, channel, start, end)` 按窗口交集返回并按开始时间升序。验收：单测覆盖切段、窗口过滤、重启扫描、空库
- [x] 1.2 PS 分段读取：按 `00 00 01 BA` 切帧，`Stream(node, channel, start, end)` 顺序拼接重叠分段，取消时关闭句柄。验收：单测覆盖单段、多段拼接、窗口裁剪、取消后不再产出
- [x] 1.3 录制接线：实时会话建立后订阅 PS 总线写库，会话结束封段；写失败只记 warn 不中断拉流；`recording.enabled=false` 时完全不写。验收：单测断言拉流后库中出现分段、会话结束后结束时间更新、无权限目录下拉流仍成功

## 2. HTTP 查询与回放

- [x] 2.1 `GET /v1/nodes/:id/channels/:ch/recordings?start=&end=`：RFC 3339 参数校验、按窗口返回列表、未启用 501、参数非法 400。验收：httptest 覆盖成功/空表/400/501
- [x] 2.2 `GET …/recordings/:recording/flv`：网关增加 `RecordingSource` 端口并从库取帧流，输出 FLV；录像不存在或不属于该通道 404。验收：httptest 断言 body 以 FLV 头开始、未知 id 404

## 3. SIP 查询与回放

- [x] 3.1 平台 RecordInfo 从库应答：有录像按库返回条目，无录像返回 SumNum=0；设备节点行为不变。验收：单测覆盖平台有库/空库、设备仍返回固定条目
- [x] 3.2 平台回放 INVITE：识别 `s=Playback` 与时间窗，从库取流以 PS/RTP（UDP / 附录 D TCP）发送，窗口结束停流；无匹配录像 404。验收：单测断言 200 + SDP、UDP 收到 PS 开头的 RTP、无录像时 404

## 4. 配置与收尾

- [x] 4.1 配置与装配：`recording.enabled` / `recording.dir` / `recording.segment_seconds`，默认目录随 storage；主程序注入存储与网关；`docs/web-ui-guide.md` §5.6 增补一行。验收：配置测试覆盖默认值与覆盖值；人工审阅文档
- [x] 4.2 e2e：真实二进制平台拉流后 `GET recordings` 出现分段，`GET …/flv` 输出 FLV 头。验收：`go test ./internal/test/e2e/` 通过
- [x] 4.3 `openspec validate platform-recording --strict --type change`、`make lint`、`make test`（`-race`）全部通过。验收：退出码 0
