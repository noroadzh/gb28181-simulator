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
- [ ] 3.2 `Records` 实现：发查询并等待应答（超时 504、不可达 502）；验收：app 单测覆盖成功/超时/不可达
- [ ] 3.3 PlaybackControl（play/pause/teardown/speed）编码与 `Playback` 实现；验收：golden test + 单测断言下发内容
- [ ] 3.4 回放会话表：重复 play 先拆旧会话、teardown 释放端口与 BYE、空闲超时回收；验收：单测覆盖三条
- [ ] 3.5 FLV 历史流：复用 `internal/adapter/flv` 接回放源；若需拆出，任务行显式标注为后续；验收：e2e 或单测断言 FLV 头可解析

## 4. 收尾

- [ ] 4.1 文档：`docs/architecture.md` 增补通道控制面路径（PTZ/录像/回放）与 501/502/504 语义；验收：人工审阅
- [ ] 4.2 `openspec validate channel-ptz-and-playback --strict --type change`、`make lint`、`make test` 全部通过；验收：退出码 0
