# cascade-media-relay Specification

## Purpose
平台作为媒体网关（B2BUA）在上级与下级之间转发实时视频：上级对某个属于下级的通道发起 INVITE 时，平台分别与两侧建立对话，把下级的 PS/RTP 转发给上级，并让两条对话的结束互相同步。

## Requirements

### Requirement: 平台按 INVITE 目标通道中继实时视频

平台节点收到 `m=video` 的实时 INVITE，且 `To` 的通道属于一个在线下级设备时，MUST 以中继方式处理：向该下级发起视频 INVITE（对话 B），并向上级返回携带平台自身接收端点的 SDP（对话 A）。平台 MUST NOT 用「本地媒体源」或「丢弃入站 RTP」的通用应答代替中继。

通道解析 MUST 与本地拉流一致：通道条目的 `parent_id` 指向的在线下级优先；无 `parent_id` 且平台只有一个在线下级时取该下级。若该通道解析不到任何下级，平台 MUST 保持既有的通用对话应答行为（200 + 回声 SDP），MUST NOT 因新增中继而改变这类请求的结果。

#### Scenario: 上级拉取下级通道

- **WHEN** 上级平台对平台节点的某个属于在线下级的通道发送 `m=video` INVITE
- **THEN** 平台向该下级发起视频 INVITE
- **AND** 上级收到 200 + SDP，其 `c=`/`m=` 指向平台的接收端点
- **AND** 上级收到的 RTP 承载的是下级发来的 PS 流

#### Scenario: 通道没有可解析的下级

- **WHEN** 通道无 `parent_id` 且平台没有或多于一个在线下级
- **THEN** 平台按既有行为应答该 INVITE（通用对话），不发起任何下级 INVITE
- **AND** 该行为与加入中继之前逐字节一致

### Requirement: 两条腿的传输独立协商

中继的两条对话 MUST 各自按对端的 offer 选择传输（UDP 或附录 D 的 TCP）；一侧为 UDP、另一侧为 TCP 时 MUST 正常桥接。平台对上级的应答 MUST 反映平台实际使用的接收传输。

#### Scenario: 上级 UDP、下级 TCP

- **WHEN** 上级以 UDP offer，而下级仅支持/选择 TCP
- **THEN** 平台向上级以 UDP 发送，同时以 TCP 从下级接收并转发

#### Scenario: 上级 TCP、下级 UDP

- **WHEN** 上级以 `TCP/RTP/AVP` offer，而下级以 UDP 应答
- **THEN** 平台以 TCP 向上级发送，同时以 UDP 从下级接收

### Requirement: 中继失败以可辨识状态码回答

建立中继时，若下级不可达 MUST 回 502；下级对视频 INVITE 回非 2xx MUST 回 480（或转发下级给出的可映射状态码）；下级无最终应答 MUST 回 504。任何失败路径 MUST NOT 留下半建立的对话或占用的端口。

#### Scenario: 下级不可达

- **WHEN** 目标下级不在线或地址不可用
- **THEN** 上级收到 502，且平台没有残留端口

#### Scenario: 下级拒绝

- **WHEN** 下级对视频 INVITE 回 404/488 等非 2xx
- **THEN** 上级收到 480

#### Scenario: 下级无应答

- **WHEN** 下级在约定时间内没有最终响应
- **THEN** 上级收到 504

### Requirement: 对话成对终止

任一侧结束时，另一侧 MUST 被结束：上级向上级发 BYE 时平台 MUST 向下级发 BYE 并释放两侧资源；下级向平台发 BYE 时平台 MUST 向上级发 BYE。平台 MUST NOT 只释放一侧而让另一侧悬挂。CANCEL 与邀请超时遵循同一规则。

#### Scenario: 上级挂断

- **WHEN** 上级对中继对话发送 BYE
- **THEN** 平台向下级发送 BYE，两侧端口释放

#### Scenario: 下级挂断

- **WHEN** 下级向平台发送 BYE
- **THEN** 平台向上级发送 BYE，两侧端口释放

### Requirement: 中继流可作为预览与录像来源

中继中的 PS 流 MUST 进入平台的实时会话总线，因此同一通道的 `/v1/flv` 预览与录像库（若启用）MUST 能使用该流，而不需要额外拉流。

#### Scenario: 上级拉流同时被平台录制

- **WHEN** 上级经平台拉取下级流且录像库启用
- **THEN** 该流被写入录像库，可按窗口查询与回放
