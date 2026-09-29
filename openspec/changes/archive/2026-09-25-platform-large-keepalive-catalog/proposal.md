## Why

第一部分交付后，platform-large 节点能受理 REGISTER 并维护在线设备表，但那张表是**静止**的：
下级发来的 MANSCDP `Keepalive` 被当作无关消息丢弃，平台也不会因为下级失联而把它从表里移除；
上级平台发来的 MANSCDP 目录查询（`CmdType = Catalog`）则完全没有应答。

结果是：在线表只反映"注册过"，不反映"还活着"——一个拔了网线的下级会永远显示在线，而任何
真实上级平台拉取目录时都拿不到一条记录。这两件事正是 GB/T 28181 里平台作为 UAS 的日常职责，
也是"平台侧仿真"可信度的分水岭。

## What Changes

- **接收心跳**：platform-large 节点在受理循环里解析 `MESSAGE` 的 MANSCDP 体；`CmdType = Keepalive`
  且在册 → 刷新该设备的 `last_seen_at`（不延长授权有效期，有效期只由重注册刷新）；未在册 → 忽略。
- **超时踢线**：每个平台节点随受理 goroutine 起一个清扫周期（`port.Ticker` 注入），把
  `expires_at` 已过且未再刷新的设备移出在线表并记一条日志；平台停止时清扫随之停止。
- **应答目录查询**：`MESSAGE` 且 `CmdType = Catalog` → 回 `200 OK`，body 为 MANSCDP `Catalog`
  响应，`SN` 与查询一致，DeviceList 由该节点当前在线设备表逐条生成（空表时 `SumNum = 0`）。
- **解析能力进适配器**：新增 `model.Notify`（收到的 MANSCDP 通知）与 `model.Catalog`（目录响应）
  两个值对象，以及 `port.MANSCDPCodec`（`DecodeNotify` / `MarshalCatalog`）；XML 与 golden 测试
  留在 `internal/adapter/manscdp`，app 层只看到值与渲染好的字符串。

不在本 change：级联转发（向上注册）、点播/回放信令、通道（Channel）级目录、设备目录上报、
心跳超时后主动通知上级。第一部分已交付的注册受理、鉴权与 HTTP 设备查询保持不变。

## Impact

- 新增/修改 spec：`platform-large-node`（ADDED：心跳接收、超时踢线、目录查询应答）。
- 代码：`internal/domain/model/{notify,catalog}.go`、`internal/domain/port/manscdp.go`、
  `internal/adapter/manscdp/{notify,catalog}.go` + testdata golden、
  `internal/app/acceptor.go`（MESSAGE 分发与清扫 goroutine，新增 `port.TickerFactory` 依赖）。
- 配置：无新增键（清扫周期为内部常量，测试通过注入的 ticker 驱动）。
- HTTP：无新增路由（在线表沿用 `GET /v1/nodes/{id}/devices`，`last_seen_at` 从此会随心跳前进）。
