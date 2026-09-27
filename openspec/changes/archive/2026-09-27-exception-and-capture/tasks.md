# 任务

## 1. 依赖与 Audit 面

- [x] 1.1 在 `go.mod` 中加入 `github.com/google/gopacket`（纯 Go，仅 pcapgo + layers）。验证：`CGO_ENABLED=0 go build ./...` 退出码 0
- [x] 1.2 给 `audit.WireEvent` 新增 `NodeID string`（默认空）以及 `siptransport.WithNodeID(...)` 选项；在 `Send` 与 `Receive` 路径中附带发送。验证：既有传输测试 unchanged；新增测试断言开启 option 后 emitted events 带有 NodeID

## 2. 传输工厂 / Lifecycle 线程

- [x] 2.1 将 `nodereg.TransportFactory` 改为 `func(addr string, nodeID model.NodeID) (port.SIPTransport, error)`；更新 `main.go` 中的 `Lifecycle.Start` / `Lifecycle.Transport` / `bindTransport`，把节点 ID 传下去。验证：`go test ./internal/adapter/nodereg/ ./internal/app/ -count=1` 通过（含更新后的 factory fake）

## 3. 领域模型与端口

- [x] 3.1 新增 `model.FaultProfile`（Canned、Delay{Base,Jitter}、Drop、Blackhole、UnsupportedMethod）及校验（状态码 400–699、drop ∈ [0,1]、非负时长、方法全大写）。验证：`go test ./internal/domain/model/ -count=1 -run Fault` 通过，含非法 profile 拒绝场景
- [x] 3.2 新增 `port.FaultStore`（Install/Clear/Get）与 `port.CaptureStore` + `port.CaptureEvent`（Append/Query/Subscribe/PCAP）。验证：`go build ./...` 退出码 0，且 adapter 测试里的编译期断言可编译

## 4. Capture Adapter

- [x] 4.1 实现 `internal/adapter/capture.Ring`（按节点环形缓冲、可配置容量、默认 2048），支持 Append / Query（limit、返回结果内按最旧优先） / 驱逐。验证：单测覆盖容量、驱逐顺序、按节点隔离、并发下非阻塞 append（`-race`）
- [x] 4.2 实现 `Subscribe` 扇出；慢订阅的事件被丢弃，cancel 释放通道。验证：单测用卡住订阅者证明 capture 继续运行且其余订阅者仍正常接收
- [x] 4.3 实现 `PCAP(nodeID)`，使用 `gopacket` + `pcapgo`：合成 Ethernet/IPv4/UDP 帧，保留原始时间戳，链路类型 `LINKTYPE_ETHERNET`；空缓冲生成合法空 pcap。验证：测试写入两条已知事件的 pcap，并用 `pcapgo` reader 解析校验 payload 字节
- [x] 4.4 在 `main.go` 中把 capture 接为全局 audit emitter（通过 `capture:` 配置段控制容量）。验证：带 capture 配置启动模拟器，执行一次 REGISTER（或等价 e2e 单测）后 `Query(nodeID)` 可看到双向事件

## 5. 故障存储与编排

- [x] 5.1 实现 `internal/app/faults.go`，内存 `FaultStore`（map + sync.RWMutex）及按节点故障计数。验证：`go test ./internal/app/ -count=1 -run FaultStore` 覆盖 install / replace / clear / 并发
- [x] 5.2 在 `acceptor.go` 请求循环加入故障 gate：blackhole → 静默跳过 + 计数；drop → 概率性跳过 + 计数 + info log；delay → 先 sleep 再作答；canned → 生成携带匹配 Via/Call-ID/From/CSeq 的合法 SIP 回复；unsupported-method → 配置为 501，否则静默丢弃。验证：`go test ./internal/app/ -count=1 -run FaultGate` 覆盖各分支，且断言 canned response 携带匹配 transaction headers
- [x] 5.3 把同一 gate 应用到 `keeper.go` 的 keepalive 响应路径（blackhole MESSAGE 导致对端在 HeartbeatMaxFailures 后报 `ErrKeepaliveLost`）。验证：keeper 测试使用 blackhole profile 观察节点进入 `StatusFault`
- [x] 5.4 默认关闭保证：所有未安装故障 profile 的既有 acceptor/keeper/siptest fixture 产生字节级一致的响应。验证：`CGO_ENABLED=0 go test -race -count=1 ./...` 退出码 0

## 6. HTTP API

- [x] 6.1 扩展 `NodeView`：新增 `InstallFault` / `ClearFault` / `FaultDetail` / `CaptureQuery` / `CapturePCAP`；在 `app.NodeService` 上实现并委托给 fault/capture store。验证：`go test ./internal/app/ ./internal/interface/http/ -count=1` 通过（含更新后的 fake）
- [x] 6.2 注册 `POST /v1/nodes/:id/faults`、`DELETE /v1/nodes/:id/faults`（未知节点 404、非法 profile 400、成功 200/204），并断言 install-then-clear 恢复正常行为。验证：`go test ./internal/interface/http/ -count=1 -run Fault` 通过
- [x] 6.3 注册 `GET /v1/nodes/:id/capture`（JSON，`limit` 参数，返回结果内按最旧优先，未知节点 404）与 `GET /v1/nodes/:id/capture.pcap`（二进制 body，`Content-Type: application/vnd.tcpdump.pcap`，未知节点 404）。验证：handler 测试覆盖 limit、空缓冲、未知节点，且 query 不触发驱逐
- [x] 6.4 在 `GET /v1/nodes/:id` 中暴露故障计数，并断言 profile 被清空后计数归零。验证：`go test ./internal/interface/http/ -count=1 -run FaultCounters` 通过

## 7. 回归与集成

- [x] 7.1 运行全量套件。验证：`CGO_ENABLED=0 go test -race -count=1 ./...` 退出码 0
- [x] 7.2 在 `internal/adapter/siptest/` 增加端到端测试：设备向平台发起 REGISTER 并命中 canned 403 故障，观察到 403；随后通过 store 清除故障，第二次 REGISTER 成功；capture 中可看到四次 wire event 且均带有平台节点 ID。验证：`CGO_ENABLED=0 go test -count=1 ./internal/adapter/siptest/ -run Exception` 通过

## 8. 验证与归档

- [x] 8.1 运行 `openspec validate --changes "exception-and-capture" --strict`。验证：validator 退出码 0 且无错误
- [x] 8.2 运行 verify-change 校验产物，确认所有任务已完成。验证：验证输出中无未完成任务
- [ ] 8.3 归档该变更到 `openspec/changes/archive/`，并更新 `docs/roadmap-15-steps.md` 中 #14、#15 状态。验证：`openspec list` 不再显示该变更；roadmap 表格将 #14/#15 标为下一阶段
