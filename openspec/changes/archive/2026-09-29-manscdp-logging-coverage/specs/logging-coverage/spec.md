# Spec Delta

## MODIFIED Requirements

### Requirement: Key-path local logging

<!-- The full text of the MODIFIED requirement is preserved below; only Scenario entries
     below are the delta. The original middleware was modified; its body text remains
     unchanged from the main spec. -->

所有对外可观察的关键执行路径 MUST 在本地产生结构化日志记录, 覆盖以下观测面:

- **SIP 信令面**: 请求/响应收发（含 method、状态码、CSeq、Call-ID、节点标识）, 事务结束, 重传与超时
- **GB/T 28181 业务面**: MANSCDP+ 指令解析结果, 注册状态机流转, 心跳应答, 目录/报警/移动位置上报
- **节点生命周期面**: 节点启动/停止/异常退出, 状态转换, 配置加载结果
- **媒体面**: 媒体源打开/关闭, 转封装管道状态, RTP 发送/失败, 会话建立与拆除
- **级联与路由面**: X-RoutePath/X-PreferredPath 解析与设备 ID 改写, 多级路径发现
- **认证与安全面**: 401 挑战生成/校验结果, SM2/SM3 协商与失败原因, 口令校验结果
- **抓包与异常注入面**: 抓包缓冲写入/溢出/丢弃, 故障注入命中与恢复

记录 MUST 携带足够的上下文属性（至少包含 `node` 节点 ID 或等价标识, 以及该观测面特有的一到两个关键属性）。记录 MUST NOT 包含口令、SM2 私钥、Note 中的完整 nonce 信封等敏感信息（已有脱敏层负责保证, 但调用点 MUST NOT 主动构造含明文敏感值的记录）。

#### Scenario: SIP request lifecycle is traceable

- **WHEN** 任一节点接收或发送一个 SIP 请求/响应
- **THEN** 产生一条包含 method、状态码（响应时）、Call-ID、CSeq、node 的日志记录
- **AND** 该记录可通过 node 字段与 Call-ID 关联, 便于按会话检索

#### Scenario: Error branches always log

- **WHEN** 任何组件处理一个错误（协议错误、认证失败、超时、IO 失败）
- **THEN** 产生一条 error 或 warn 级别记录
- **AND** 记录包含可定位的上下文（至少 err 文本与调用方标识）

#### Scenario: Media hot loop aggregation

- **WHEN** 媒体 RTP 分包/发送的热循环内发生可恢复的逐包错误
- **THEN** 该错误被聚合统计, 不逐包打日志（避免日志洪泛）
- **AND** 在聚合窗口结束时（默认 60 秒）输出一条汇总 warn 记录

#### Scenario: Node lifecycle transitions are recorded

- **WHEN** 节点状态发生任何转换（未启动 → 启动中 → 等待注册 → 已注册 → 运行中 → 暂停 → 异常退出）
- **THEN** 产生一条 debug 级别记录, 包含 node、from 状态、from→to 状态
- **AND** 节点异常退出时追加一条 error 级别记录

#### Scenario: Sensitive values never reach the log

- **WHEN** 任何日志调用点包含 `password`、`secret`、`private_key`、SM2 私钥或注册口令的明文值
- **THEN** 对应记录写出时, 敏感值显示为 `***REDACTED***`

#### Scenario: MANSCDP 业务面成功路径可观测

- **WHEN** `internal/app/acceptor.go` 的任一 MANSCDP 业务 handler（`handleMediaStatus`、`handlePlaybackControl`、`handleDeviceInfo`、`handleRecordInfo`、`handleHomePosition` Query 分支、`handleCruiseTrackList`、`handleSnapShot`）完成解码与业务处理并将响应返回调用方
- **THEN** 该 handler MUST 在成功路径上产生一条 `debug` 级日志记录
- **AND** 记录 MUST 包含结构化字段 `node`（节点 id）与该事件特有的至少一个标识字段（`device_id`、`sn`、`count`、`channel` 等）
- **AND** 该记录 MUST NOT 在 port 缺失或 codec 解码失败时打印（仅记录成功路径，错误路径由 `Scenario: Error branches always log` 覆盖）