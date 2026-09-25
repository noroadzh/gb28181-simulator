# Design

## Context

platform-small 的第一部分（双向身份与级联链路）已归档。本 change 在其上补充 4 项主动
能力。依赖关系：

- Change 4 `node-abstraction`：提供 Node/Dialog 等值对象与状态机
- Change 6 `platform-large-node`：提供 Acceptor、目录应答、心跳处理
- Change 7 第一部分：提供 platform-small 双半架构、splitTransport、NodeService 集成
- Change 8 `media-sources`：提供 PS/RTP 双向转封装、出站管道

## Architecture

### 1. splitTransport 升级为事务分拣

当前 splitTransport 按方向（request → serving，response → registering）是临时方案。
INVITE / SUBSCRIBE 的请求和响应都来自同一方向（上游），必须按 Call-ID 匹配。

**设计**：
- `node_split_transport.go` 新增 `TransactionMatcher`，维护 `map[Call-ID]*TransactionHandler`
- `TransactionHandler` 注册处理函数（`OnResponse`、`OnAck`、`OnBye`）
- serving 半与 registering 半各自注册事务处理器
- 分拣规则：
  - 请求 → serving 半
  - 响应 → 按 Call-ID 找到注册的事务处理器；若找不到则丢弃（单向响应无意义）
  - INVITE / SUBSCRIBE 请求同时注册事务处理器，以便接收后续响应/ACK

### 2. Dialog / Session 跟踪（app 层）

**设计**：新建 `internal/app/dialog.go`，不放在 domain 因为 Dialog 是协议会话概念。

```go
// DialogState 跟踪 INVITE 事务
type DialogState struct {
    CallID    string
    LocalURI  string
    RemoteURI string
    CSeq      uint32
    Status    DialogStatus  // calling → proceeding → confirmed → terminated
    Media     *MediaSession
    CancelFn  context.CancelFunc
    Done      chan struct{}
}

type DialogStatus int

const (
    DialogCalling   DialogStatus = iota
    DialogProceeding
    DialogConfirmed
    DialogTerminated
)
```

- `DialogManager`：维护 `sync.Map[Call-ID]*DialogState`，管理 dialog 生命周期
- INVITE 请求：创建 Dialog（status=calling），发送请求，收到 1xx → proceeding，收到 2xx → confirmed 并启动入站媒体管道
- BYE 请求：发送后等待 200 OK，确认后关闭媒体管道并删除 Dialog
- 超时（无人应答 / 传输失败）：进入 terminated，关闭媒体管道

### 3. INVITE 处理

**设计**：`internal/app/acceptor.go` 新增 `handleInvite`：

1. 解析 SDP → `model.Session`
2. 从 SDP 提取媒体信息（IP、端口、payload type、codec）
3. 创建 Dialog（status=proceeding）
4. 启动入站媒体管道：
   - 从 `RTPDeizer` 读取 PS frame
   - 经 `PSDepacketizer` 输出 ES frame
   - 写入 `ESWriteCloser`（可以是文件、内存缓冲或回调）
5. 生成 200 OK + 本端 SDP（携带本端 IP、端口、SSRC）
6. 收到 ACK：Dialog 状态机推进到 confirmed
7. 收到 BYE：停止媒体管道，返回 200 OK，Dialog 进入 terminated
8. 超时（无 ACK / 无 200）：清理资源，Dialog 进入 terminated

**SDP 交互**：
- 入站 SDP：`v=0 / o=- <ssrc> <clock> IN IP4 <addr> / c=IN IP4 <addr> / m=video <port> RTP/AVP <payload> / a=recvonly`
- 出站 SDP：复用入站 SDP 的媒体参数，改为 `a=sendrecv`

### 4. SUBSCRIBE 目录订阅

**设计**：`internal/app/acceptor.go` 新增 `handleSubscribe`：

- 解析 SUBSCRIBE 的 Event 头（`Event: catalog`）与 Expires
- 记录订阅关系：`map[Call-ID]*Subscription`，含 DeviceID、Expires、超时回调
- 返回 200 OK + 可选 Notify（当前设备列表）
- 当下级设备注册/注销时，检查所有活跃订阅，发送 NOTIFY

**NOTIFY 构造**：
- `CmdType = Catalog`
- SN 递增
- DeviceList 来自 platform-small 的在线设备表

### 5. OPTIONS 保活（扩展 Keeper）

**设计**：`internal/app/keeper.go` 新增 `optionsKeepalive`：

- `Keeper` 新增字段 `optionsEnabled bool` 与 `optionsInterval time.Duration`
- 启动时判断：若 `registration:` 存在且配置了 `options: true`，则启动 OPTIONS 探测
- 探测逻辑：
  - 定期发送 `OPTIONS sip:<server_id>@<server> SIP/2.0`
  - 收到 200：链路正常，打 debug 日志
  - 收到 408 / 超时：链路可疑，打 warn；连续 N 次失败触发节点 fault
  - 收到其它错误：打 warn，不立即 fault

### 6. MediaStatus 上报

**设计**：

- `internal/domain/model/notify.go` 新增 `CmdTypeMediaStatus = "MediaStatus"`
- `internal/adapter/manscdp/codec.go` 扩展 `DecodeNotify`：
  - 对未知命令类型返回可解析的 Notify（不报错），让 use case 层决定是否处理
- `internal/app/acceptor.go` `handleMessage` 新增 case：
  ```go
  case notify.IsMediaStatus():
      return a.handleMediaStatus(ctx, p, req, notify)
  ```
- `handleMediaStatus`：解析设备 ID、媒体参数（分辨率/码率/帧率），更新设备媒体状态；
  若配置了向上转发，则通过 Keeper 或单独通道向上游发送 MediaStatus NOTIFY

### 7. MANSCDP 扩展

**设计**：扩展 `internal/adapter/manscdp/codec.go` 的 `Notify` 结构与编解码：

- 新增 `MediaStatusNotify` 值对象：`DeviceID`、`VideoParam`（分辨率/码率/帧率）、
  `AudioParam`、`RecordStatus`
- `DecodeNotify`：对 `CmdType=MediaStatus` 解码为 `MediaStatusNotify`
- 新增 `MarshalSubscribe` / `MarshalNotify`：渲染 SUBSCRIBE 请求体与 NOTIFY 响应体
- 保持"解析宽松、渲染严格"：未知命令类型不解码但保留原始 XML

### 8. 入站媒体管道组装

**设计**：`internal/app/media_service.go` 新增 `OpenInboundPipeline`：

```go
func (m *MediaService) OpenInboundPipeline(
    addr string,          // 监听地址（来自 SDP）
    ssrc uint32,
    onES func(model.ESFrame),  // ES frame 回调（可以是文件写入、NVR 上报）
) (RTPDeizer, error)
```

- 创建 `RTPDeizer`（绑定 addr 监听 UDP）
- PS → ES → onES 回调的组装由 `ESWriteCloser` 实现
- Dialog 生命周期绑定：confirmed 时启动，terminated 时 Close + 释放端口

### 9. 新增 Domain Port 接口

**设计**：新增 `internal/domain/port/` 文件：

```go
// playback.go
type PlaybackPort interface {
    // StartPlayback 启动回放会话，返回 session ID
    StartPlayback(ctx context.Context, deviceID, startTime, endTime string) (string, error)
    // StopPlayback 停止回放会话
    StopPlayback(ctx context.Context, sessionID string) error
    // QueryRecord 查询录像列表
    QueryRecord(ctx context.Context, deviceID, startTime, endTime string) ([]model.RecordItem, error)
}

// subscribe.go
type SubscribePort interface {
    // Subscribe 向下游设备订阅目录
    Subscribe(ctx context.Context, deviceID string, expires time.Duration) error
    // Unsubscribe 取消订阅
    Unsubscribe(ctx context.Context, deviceID string) error
    // Notify 推送目录变更
    Notify(ctx context.Context, deviceID string, catalog model.Catalog) error
}

// media_status.go
type MediaStatusPort interface {
    // UpdateMediaStatus 更新设备媒体状态
    UpdateMediaStatus(ctx context.Context, deviceID string, status model.MediaStatus) error
    // ForwardMediaStatus 向上游转发媒体状态
    ForwardMediaStatus(ctx context.Context, status model.MediaStatus) error
}
```

每个 port 携带 compile-time 断言：

```go
var _ port.PlaybackPort = (*Adapter)(nil)
```

## Open Questions

- **Q1**：INVITE 媒体流的传输层（UDP / TCP）。当前 RTP 出站走 UDP；入站默认 UDP，
  是否需要支持 TCP？—— 暂按 UDP 实现，TCP 作为后续 Change 11（2022 扩展）的扩展点。
- **Q2**：Dialog 超时阈值。当前设计使用 30s 无响应超时，是否可通过配置覆盖？——
  暂硬编码，后续在 `platform:` 配置段中增加 `dialog_timeout` 字段。
- **Q3**：MediaStatus 向上转发是通过 Keeper 的 MESSAGE 通道还是单独通道？——
  暂用 Keeper 的 MESSAGE 通道，MediaStatus 作为一种特殊的 Notify 发送。
- **Q4**：SUBSCRIBE 的 Event 头仅支持 `catalog`。若收到其它 Event 类型，是否回复
  489 Bad Event？—— 是，这是标准做法。

## Alternatives Considered

- **INVITE 媒体用 TCP 中继**：不选。UDP 更常见于实时流；TCP 支持推到 Change 11。
- **Dialog 放在 domain 层**：不选。Dialog 是协议会话概念，domain 保持纯业务实体。
- **事务分拣用 SIP transaction layer（gosip 内置）**：不选。gosip 的事务层不暴露
  给外部，且当前 splitTransport 已隔离；扩展分拣逻辑更可控。
