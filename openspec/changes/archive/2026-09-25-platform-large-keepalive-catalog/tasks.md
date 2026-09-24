## 1. 领域：通知与目录值对象

- [x] 1.1 新增 `internal/domain/model/notify.go`：`Notify{CmdType, SN, DeviceID, Status}` + `NewNotify` + 访问器 + `IsKeepalive()` / `IsCatalogQuery()` + `HasNotify()` + 日志安全 `String()`
- [x] 1.2 新增 `internal/domain/model/catalog.go`：`CatalogItem{DeviceID, Name, Manufacturer, Model, Owner, CivilCode, Address, Status, Parental, SafetyWay, RegisterWay, Secrecy}` + `NewCatalogItem`（要求 DeviceID；`CivilCode` 缺省取 device id 前 6 位）
- [x] 1.3 `Catalog{DeviceID, SN, Items}` + `NewCatalog`（要求 DeviceID）+ `SumNum()` + `HasCatalog()`
- [x] 1.4 `internal/domain/model/notify_test.go` / `catalog_test.go`：必填校验、CmdType 判定、CivilCode 推导、SumNum
- [x] 1.5 `gofmt`；`go test ./internal/domain/model/` 通过

## 2. 端口：MANSCDP 编解码

- [x] 2.1 新增 `internal/domain/port/manscdp.go`：`MANSCDPCodec{ DecodeNotify(body string) (model.Notify, error); MarshalCatalog(catalog model.Catalog) (string, error) }`，注释说明方向（收/发）与"不抛 panic"
- [x] 2.2 `internal/domain/port/port_test.go`：确认 app 层可只依赖 `port` 与 `model`（架构约束不变）

## 3. 适配器：解析与渲染

- [x] 3.1 新增 `internal/adapter/manscdp/notify.go`：`DecodeNotify`——解析 `<?xml ...?><Notify>`，取 `CmdType` / `SN` / `DeviceID` / `Status`，CmdType 或 DeviceID 缺失返回 error
- [x] 3.2 新增 `internal/adapter/manscdp/catalog.go`：`MarshalCatalog`——渲染 `<Response><CmdType>Catalog</CmdType><SN/><DeviceID/><SumNum/><DeviceList Num="">` + `<Item>` 列表，空表时 `SumNum=0`
- [x] 3.3 复用既有 `xmlHeader` 与 `xml.MarshalIndent` 范式，输出以换行结尾
- [x] 3.4 `internal/adapter/manscdp/notify_test.go`：合法 keepalive / catalog 查询、缺字段、畸形 XML、非 Notify 根元素
- [x] 3.5 golden 测试：`testdata/catalog_one.xml` / `catalog_empty.xml`，与 `MarshalCatalog` 输出逐字节比对
- [x] 3.6 `var _ port.MANSCDPCodec = (*MANSCDPCodecAdapter)(nil)` 编译期断言

## 4. 用例：Acceptor 的 MESSAGE 分发与清扫

- [x] 4.1 `internal/app/acceptor.go`：`NewAcceptor` 增加 `newTicker port.TickerFactory` 参数（nil → 回落真实 ticker）
- [x] 4.2 增加 `manscdp port.MANSCDPCodec` 依赖（nil → 回落一个"不认识任何命令"的 no-op？还是必填？——定为必填，缺则构造报错）
- [x] 4.3 `run` 循环：非 REGISTER 的 `MESSAGE` 交给 `handleMessage`，其余 method 维持丢弃
- [x] 4.4 `handleMessage`：解码 → `Keepalive` 走 `refresh`；`Catalog` 走 `answerCatalog`；其他/失败 → 记日志返回 nil（不回应）
- [x] 4.5 `refresh`：表内命中 → `WithSeen(now)` 回写 + `200 OK`；未命中 → warn 不回应
- [x] 4.6 `answerCatalog`：以该节点在线表构造 `model.Catalog`（SN 来自查询、按 device id 升序）→ `MarshalCatalog` → `200 OK` + body；SN 缺失 → warn 不回应
- [x] 4.7 清扫 goroutine：session 内起，按 ticker 节拍调用 `sweep`；`now >= ExpiresAt()` 的记录 `Remove` + info 日志（含 node_id、device id、过期时长）
- [x] 4.8 `Stop` / `Close` 同时结束清扫 goroutine（`done` 通道汇合），保持"先停受理再释放端口"
- [x] 4.9 日志：心跳刷新 debug、未注册心跳 warn、踢线 info、畸形报文 debug；均带 `node_id`，不含任何凭据

## 5. 装配与配置

- [x] 5.1 `cmd/gb28181-simulator/main.go`：`NewAcceptor` 传入 `manscdp.NewMANSCDPCodec()` 与 `clock.RealTicker()`
- [x] 5.2 无新增配置键；`configs/config.example.yaml` 与 `README.md` 不需要改（复核一遍确认）
- [x] 5.3 `go build ./...` 通过

## 6. 测试

- [x] 6.1 `internal/app/acceptor_message_test.go`：fake MANSCDP codec + scripted ticker，覆盖 4.4–4.7 各分支
- [x] 6.2 心跳刷新：在册设备 `last_seen_at` 前进、`expires_at` 与 `granted` 不变、条目数不变、回 200
- [x] 6.3 未注册心跳：不回应（无 sent 消息）、不改表
- [x] 6.4 目录应答：SN 一致、条目按序、`SumNum` 正确、空表 `SumNum=0`、两平台互不相见
- [x] 6.5 超时踢线：打点后过期记录被移除并留日志；未过期记录保留；停止后打点不再改表
- [x] 6.6 畸形 body：不 panic、不回应、受理循环继续（随后一条 REGISTER 仍被正常受理）
- [x] 6.7 e2e（`internal/adapter/siptest/`）：device 节点注册到 platform-large 节点 → `Keeper` 心跳 → 平台侧 `last_seen_at` 前进
- [x] 6.8 e2e：裸 transport 发 `CmdType=Catalog` 的 MESSAGE → 收 200 OK 且 body 能被 `DecodeNotify`/XML 解析出条目
- [x] 6.9 e2e：短 expires 注册后不再重注册 → 清扫后设备出表
- [x] 6.10 `go test ./... -race` 全绿

## 7. 收尾

- [x] 7.1 `README.md` 补一句"平台接收心跳并按注册有效期踢线、应答目录查询"
- [x] 7.2 `docs/architecture.md` 的平台受理章节补 MESSAGE 分发与清扫
- [x] 7.3 `openspec validate "platform-large-keepalive-catalog" --strict` 通过
- [x] 7.4 同步主 spec `openspec/specs/platform-large-node/spec.md`（合并 ADDED 需求）
- [x] 7.5 归档到 `openspec/changes/archive/2026-09-24-platform-large-keepalive-catalog/`
- [x] 7.6 提交 commit
