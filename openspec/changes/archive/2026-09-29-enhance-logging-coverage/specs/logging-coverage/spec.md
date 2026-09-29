# Spec Delta

## Purpose

定义 gb28181-simulator 关键执行路径上的本地日志覆盖契约: 哪些观测点必须留下可检索的记录, 以及如何按模块与按环境（生产/测试/集成）选择记录等级。

## ADDED Requirements

### Requirement: Key-path local logging

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

### Requirement: Environment-aware log level configuration

系统 MUST 支持通过配置文件与环境变量配置日志记录等级, 满足:

- **顶层兜底级别**: 全局 `log.level` 字段控制默认级别, 取值为 `trace | debug | info | warn | error`
- **模块级覆盖**: `log.modules.<module-path>` 字段允许针对特定模块路径（含前缀匹配）独立设置日志级别, 优先级高于兜底级别
- **环境约定**:
  - 生产环境: 兜底级别 MUST 为 `info`（仅记录关键业务事件）
  - 测试环境: 兜底级别 MUST 为 `debug`（记录流程细节）
  - 集成环境: 兜底级别 MUST 为 `debug` 或 `trace`（记录 SIP wire bytes 与解码细节）
- **运行时更新**: 通过 HTTP PATCH 接口 `PATCH /v1/config/log` MUST 可动态更新上述配置, 无需重启进程
- **配置校验**: 未知模块路径或非法级别字符串 MUST 拒绝写入并返回 400, 不影响现有 logger 行为

#### Scenario: Global default applied when no module override

- **WHEN** 配置仅设置 `log.level: info` 而无 `log.modules` 节
- **THEN** 所有模块以 info 作为最低可见级别
- **AND** 低于 info 的记录（debug、trace）不出现在文件/WebSocket 输出中

#### Scenario: Module-level override takes precedence

- **WHEN** 配置同时设置 `log.level: info` 与 `log.modules.sip: trace`（或 `log.modules."internal/app/sip": debug`）
- **THEN** SIP 模块及其祖先级记录可以低于全局级别（debug/trace）写出
- **AND** 非匹配模块保持全局 info 级别

#### Scenario: Production profile by default

- **WHEN** 部署时使用默认配置（生产示例配置）
- **THEN** 兜底级别为 info, SIP/媒体模块为 debug（记录流程但不记录 wire bytes）
- **AND** 测试与集成场景通过修改配置或环境变量提升至 trace, 不需要修改代码

#### Scenario: Runtime level update via API

- **WHEN** 调用 `PATCH /v1/config/log`, body 为 `{"level": "debug"}` 或 `{"modules": {"sip": "trace"}}`
- **THEN** 全局/模块级别在 1 秒内生效
- **AND** 现有 hub 订阅者继续接收推送（无中断）
- **AND** 文件输出按新级别过滤

#### Scenario: Invalid level rejected

- **WHEN** 调用 PATCH 接口传入非法级别（如 `"level": "verbose"`）
- **THEN** 返回 400 且 body 中说明非法字段
- **AND** 现有 logger 行为不变

### Requirement: Log level hot reload persistence

运行时通过 PATCH 接口更新的日志级别 MUST 在内存中生效, 不写入磁盘配置文件（防止与外部配置管理工具冲突）。reload 重启后, 级别遵循磁盘配置文件中的值。reload 不强制持久化到 SQLite（不属于本变更范围）。

#### Scenario: Restart restores file-based config

- **WHEN** 进程通过 PATCH 接口将全局级别改为 debug
- **THEN** 磁盘配置文件保持不变
- **AND** 进程重启后级别遵循磁盘配置（如 `file.conf: info`）