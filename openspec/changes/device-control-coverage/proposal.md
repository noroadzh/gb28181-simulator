# Proposal

## Why

信令盘点（#40 之后的探索）列出的六个常用命令至今全无实现：`DeviceStatus`、`Broadcast`、报警查询与布防撤防、`ConfigDownload` 流程、`DeviceUpgrade`/`DeviceReboot`、文件下载。真实平台对接时它们不是边角料——上级会在拉流前后查设备状态、下发广播、查询/布防报警、读取设备配置、触发升级或重启，以及下载录像文件。缺一个，对接方就会认为这台模拟设备「不完整」。

路线图编号：#44 `device-control-coverage`。

## What Changes

- **DeviceStatus**：`CmdType=DeviceStatus` 查询与应答（设备在线状态、是否正常工作），设备节点作答、平台节点可向下级转发并回传。
- **Broadcast（语音广播）**：`CmdType=Broadcast` 下发与 200 应答；设备随后按国标向平台发起音频 INVITE，平台以 200 + SDP 应答并把音频接入既有对讲音频桥（`/talk/audio` 同一条 PCMU 通道）。
- **报警查询与布防撤防**：`CmdType=AlarmQuery` 查询历史报警（来自节点的报警快照）、`DeviceControl` 的布防/撤防（`AlarmCmd`）与 `CmdType=Alarm` 订阅通知保持一致。
- **设备配置**：`CmdType=ConfigDownload` 查询设备配置并应答（基础配置项来自节点 profile），平台可下发配置并收到 ack。
- **升级与重启**：`CmdType=DeviceUpgrade` / `CmdType=DeviceReboot` 下发与 200 应答；升级只做状态与应答，不真的替换二进制。
- **文件下载**：`s=Download` 的 INVITE——设备按录像时间窗以 PS/RTP 发送文件流；平台节点可作为请求方发起下载并把流写入录像库目录。

## Capabilities

### New Capabilities

- `device-command-coverage`: 六个常用 MANSCDP 命令的双向行为（查询/应答、下发/ack、以及两条媒体路径：广播音频与文件下载）。

### Modified Capabilities

- `device-node`: 设备节点新增对上述命令的应答。
- `platform-large-node`: 平台节点新增向下级转发/代为应答这些命令。
- `channel-http-api`: 新增设备状态、报警查询、布防撤防、配置、升级/重启、下载的 HTTP 入口。

## Impact

- `internal/domain/model`：六个命令的值对象（Status/Query/Response/Ack 形态）。
- `internal/adapter/manscdp`：编解码与 golden 测试。
- `internal/app`：设备侧与平台侧的处理器；广播音频接入既有对讲桥；下载写入录像库目录。
- `internal/interface/http`：通道级与节点级入口。
- 测试：每命令 golden + app 单测 + 一条广播 e2e。

## Non-goals

- 升级不做真实固件替换（只回 ack 并记录状态）。
- 配置只覆盖基础项（名称、厂商、通道数、心跳间隔），不做完整配置集。
- 下载不实现断点续传与限速。
- 不做 SRTP/TLS 媒体；广播与下载沿用 UDP / 附录 D TCP 的既有传输。
- 不改既有对讲、回放、录像的语义。
