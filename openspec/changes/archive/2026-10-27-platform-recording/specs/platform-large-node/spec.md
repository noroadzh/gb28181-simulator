# platform-large-node Delta

## ADDED Requirements

### Requirement: 平台从录像库应答 RecordInfo 查询

平台节点收到 `CmdType = RecordInfo` 的 MANSCDP 查询且其录像库中存在该设备/通道的录像时，MUST 以录像库中的条目应答（SumNum 与 Item 一一对应，时间格式符合 GB/T 28181）。录像库为空时 MUST 返回 `SumNum=0` 的空应答，而不是设备式的固定录像条目。设备节点的既有录像查询行为 MUST 保持不变。

#### Scenario: 有录像时按库应答

- **WHEN** 上级平台向本平台查询某通道的录像，且库中该窗口有分段
- **THEN** 200 OK 的 RecordInfo 应答列出这些分段

#### Scenario: 无录像时返回空表

- **WHEN** 平台上没有该通道的录像
- **THEN** 应答的 SumNum 为 0，Item 为空

### Requirement: 平台从录像库受理回放 INVITE

平台节点收到 `s=Playback` 的 INVITE 且其录像库中存在目标通道与时间窗的录像时，MUST 以 200 OK + SDP（平台接收端口、`a=sendonly`）应答，并把对应窗口的录像以 PS/RTP 发送到请求方声明的端点（UDP，或附录 D 的 TCP）。窗口结束或录像耗尽时 MUST 停止发送。库中没有匹配录像时 MUST 返回 404。

#### Scenario: 回放 INVITE 成功

- **WHEN** 上级平台用 `s=Playback` + 时间窗 INVITE 本平台，库中有匹配录像
- **THEN** 本平台回 200 + SDP，并把 PS/RTP 发到请求方端点

#### Scenario: 无匹配录像

- **WHEN** 回放请求的时间窗与库中任何分段都没有交集
- **THEN** 返回 404，不发送媒体
