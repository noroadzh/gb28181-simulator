# Tasks

## 1. 归零（先让失败可见）

- [x] 1.1 `NodeService.PTZ` 改为返回 `port.PTZUnsupported(id, ch, "signalling not implemented")`；HTTP PTZ 处理器用 `errors.As` 映射 501；验收：httptest 断言 501 + 错误体含原因，现有 200 断言同步更新
- [x] 1.2 `NodeService.Records` 改为返回 `port.RecordUnsupported(...)`；Records 处理器映射 501；验收：httptest 断言 501
- [x] 1.3 删除 `SendPTZ` / `ListRecords` / `StartPlayback` / `ControlPlayback` 四个无调用方方法；验收：`go build ./...` 与 `go vet ./...` 通过，无残留引用

## 2. PTZ 下发

- [x] 2.1 MANSCDP DeviceControl（PTZCmd）编码：方向/速度/时长 → 8 字节 PTZCmd；验收：**字节级 golden test**（含八方向 + 预置位调用）
- [x] 2.2 `PTZ` 实现：定位在线 device 节点 → 构造 MESSAGE（Application/MANSCDP+XML）→ 经节点 transport 发送；非 device 501、节点离线 502；验收：app 单测断言报文目的地址、body 与 200 返回
- [x] 2.3 e2e：真实 SIP 抓包断言 PTZ MESSAGE 到达 device 节点且 PTZCmd 内容正确；验收：`go test ./internal/test/e2e/...` 通过

## 3. 录像查询与回放

- [x] 3.1 RecordInfo Query 编码与应答解析；验收：golden test（查询 XML + 应答字段），空应答返回空数组而非错误
- [x] 3.2 `Records` 实现：发查询并等待应答（超时 504、不可达 502）；验收：app 单测覆盖成功/超时/不可达
- [x] 3.3 PlaybackControl（play/pause/teardown/speed）编码与 `Playback` 实现；验收：golden test + 单测断言下发内容
- [x] 3.4 回放会话表：重复 play 先拆旧会话、teardown 释放端口与 BYE、空闲超时回收；验收：单测覆盖三条

## 4. 收尾

- [x] 4.1 文档：`docs/architecture.md` 增补通道控制面路径（PTZ/录像/回放）与 501/502/504 语义；验收：人工审阅
- [x] 4.2 收尾：`openspec validate channel-ptz-and-playback --strict --type change` 与 `make lint` 通过，`go test ./...` 全绿。**例外已记录**：`make test`（-race）在 `siptest.TestE2E_PlatformLargeUpstreamKeepalive` 上偶发数据竞争（`siptransport` 的 Close 与转发 goroutine），在本次改动之前的提交上同样 2/5 复现，属既有问题，与本 change 的代码无关；修复留待独立 change

## 归档补记

- 2026-10-26：任务 4.2 记录的 `siptest` 数据竞争已由提交 `4198e9e` 修复——`siptransport.Close` 原先在转发 goroutine 可能仍在发送时直接关闭 `out`，现在 `out` 只由转发 goroutine 在退出时关闭，`Close` 先等 `done` 再释放 layer。修复后 `make test`（-race）全绿：`MAKE_EXIT=0`，33 个包，无 race 报告。

## 核查记录（2026-10-27）

任务 3.5（FLV 历史流）已由 change `playback-flv-stream`（#37，归档于 2026-10-26-playback-flv-stream）交付：回放 INVITE 按时间窗供流，`GET /v1/nodes/:id/channels/:ch/playback/flv?sn=` 出流，并有专用 e2e。原条目已移除。
