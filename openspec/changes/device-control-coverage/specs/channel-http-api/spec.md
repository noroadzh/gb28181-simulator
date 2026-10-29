# channel-http-api Delta

## ADDED Requirements

### Requirement: 运维类 HTTP 端点

系统 MUST 提供设备状态、报警查询、布防撤防、配置查询/下发、升级、重启与文件下载的 HTTP 入口，返回与 SIP 应答一致的结论。路径见下：

- `GET /v1/nodes/:id/status`
- `GET /v1/nodes/:id/channels/:ch/alarms?start=&end=`
- `POST /v1/nodes/:id/channels/:ch/alarm-mode`（`{"mode":"armed"|"disarmed"}`）
- `GET` / `PUT /v1/nodes/:id/config`
- `POST /v1/nodes/:id/upgrade`（`{"version":"..."}`）
- `POST /v1/nodes/:id/reboot`
- `POST /v1/nodes/:id/channels/:ch/download`（`{"start":"...","end":"..."}`）

失败 MUST 可辨识：节点身份不支持 501，目标不在线 502，超时 504，参数非法 400。

#### Scenario: HTTP 查询设备状态

- **WHEN** GET /v1/nodes/:id/status
- **THEN** 返回 200 与该设备的在线状态

#### Scenario: HTTP 布防

- **WHEN** POST alarm-mode 带 `armed`
- **THEN** 返回 200，后续报警查询显示 armed

#### Scenario: HTTP 下发配置

- **WHEN** PUT /v1/nodes/:id/config 带心跳间隔
- **THEN** 返回 200，节点按新间隔心跳

#### Scenario: HTTP 发起下载

- **WHEN** POST download 带时间窗且设备有匹配录像
- **THEN** 返回 200，平台侧收到文件流
