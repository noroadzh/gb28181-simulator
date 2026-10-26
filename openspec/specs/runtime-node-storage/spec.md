# runtime-node-storage 规范

## Purpose

将节点定义、注册目标、平台账号、级联拓扑从 YAML 配置迁移到 SQLite 持久化，使节点定义可跨进程重启存活并在运行时通过 API 管理。

## Requirements

### Requirement: 节点定义存储

系统 MUST 在 SQLite 数据库中维护 `nodes` 表，主键为 `id`（20 位 GB28181 编码），存储以下字段：`kind`（枚举 device / platform-large / platform-small）、`domain`（归属域）、`addr`（信令监听地址，格式 host:port）、`vendor`（厂商标识）、`cascade_parent`（上级节点 ID，空串表示无）、`cascade_children`（JSON 数组，下级节点 ID 列表）、`created_at`（ISO8601 时间戳）、`updated_at`（ISO8601 时间戳）。`cascade_children` 为空数组时存储为 JSON 文本 `[]`。

#### Scenario: 创建新节点写入 DB

- **WHEN** 操作者通过 HTTP API 创建节点 ID `34020000001310000001`，kind `device`，domain `3402000000`，addr `0.0.0.0:5061`
- **THEN** `INSERT INTO nodes` 成功写入，`SELECT * FROM nodes WHERE id='34020000001310000001'` 返回该行

#### Scenario: 读取节点列表

- **WHEN** DB 中存在 3 条节点记录
- **THEN** `SELECT * FROM nodes` 返回 3 行，每行含全部非 JSON 字段

#### Scenario: 更新节点字段

- **WHEN** 操作者修改节点 ID `34020000001310000001` 的 `addr` 为 `0.0.0.0:5062`
- **THEN** `UPDATE nodes SET addr='0.0.0.0:5062', updated_at=NOW() WHERE id='34020000001310000001'`，`SELECT addr FROM nodes WHERE id='34020000001310000001'` 返回新值

### Requirement: 注册目标存储

系统 MUST 在 SQLite 数据库中维护 `node_registrations` 表，外键关联 `nodes(id)`。字段包括：`node_id`（外键）、`server`（上级平台地址，host:port）、`server_id`（上级平台 ID）、`username`（注册用户名）、`password`（明文，协议必需）、`gb_version`（GB/T 28181 版本，2016 或 2022，默认 2016）、`expires`（请求的注册有效期秒数）、`timeout`（注册请求超时duration）、`transport`（udp / tcp，默认 udp）、`allow_no_auth`（布尔）、`heartbeat_interval`（duration）、`heartbeat_timeout`（duration）、`heartbeat_max_failures`（整数）、`created_at`、`updated_at`。一个 node_id 最多对应一条 registration 记录。

#### Scenario: 节点携带注册目标创建

- **WHEN** 创建节点 ID `34020000001310000001` 时同时提供 `registration` 段
- **THEN** `node_registrations` 表写入对应记录，`SELECT * FROM node_registrations WHERE node_id='34020000001310000001'` 返回该行

#### Scenario: 节点无注册目标

- **WHEN** 节点创建时未提供 `registration` 段
- **THEN** `node_registrations` 表无该节点对应记录

### Requirement: 平台账号存储

系统 MUST 在 SQLite 数据库中维护 `node_platforms` 表，外键关联 `nodes(id)`。字段包括：`node_id`（外键）、`realm`（挑战域，可为空表示使用节点 domain）、`allow_no_auth`（布尔）、`min_expires`、`default_expires`、`max_expires`（均为整数秒），`catalog_poll_interval`（duration）、`created_at`、`updated_at`。每个节点最多对应一条 platform 记录。`accounts` 信息复用已存在的 `platform_accounts` 表（`node_id` + `username` 为唯一键），该表已在 Change 5 实现，本 capability 不重复建设。

#### Scenario: 平台节点携带账号信息

- **WHEN** 创建 kind 为 `platform-large` 的节点时提供 `platform` 段
- **THEN** `node_platforms` 表写入对应记录，`SELECT * FROM node_platforms WHERE node_id='34020000002000000001'` 返回该行

#### Scenario: 设备节点无 platform 记录

- **WHEN** 创建 kind 为 `device` 的节点
- **THEN** `node_platforms` 表无该节点对应记录

### Requirement: 级联拓扑存储

系统 MUST 将 `cascade_parent`（字符串）和 `cascade_children`（JSON 数组）存储在 `nodes` 表中，并支持级联拓扑的运行时更新。更新 `cascade_children` 时 MUST 将 JSON 数组序列化为文本后写入；读取时 MUST 将文本反序列化为 JSON 数组返回给调用方。

#### Scenario: 更新级联父子关系

- **WHEN** 节点 ID `34020000001310000002` 的 `cascade_parent` 更新为 `34020000002000000001`，`cascade_children` 更新为 `["34020000001310000003","34020000001310000004"]`
- **THEN** `nodes` 表中该行的 `cascade_parent` 为 `34020000002000000001`，`cascade_children` 为 `"[\"34020000001310000003\",\"34020000001310000004\"]"`

#### Scenario: 读取级联拓扑

- **WHEN** 查询节点 ID `34020000001310000002`
- **THEN** 返回的 JSON 包含 `cascade_parent`（字符串或 null）和 `cascade_children`（JSON 数组）

### Requirement: 装配顺序改造

系统 MUST 在进程启动时按照以下顺序初始化：1）读取 YAML 配置文件中的非节点段（http、log、capture、tracing、storage、auto_start、scenario）并初始化日志、存储、HTTP 服务器；2）执行 SQLite schema 初始化（包含本 capability 新增的四张表）；3）从 DB 的 `nodes` 表读取全部节点定义；4）使用读取到的节点定义装配 `NodeService`（对每个节点调用 `NodeService.Create` 并在 `auto_start: true` 时调用 `NodeService.Start`）。若 DB 为空，则不创建任何节点。

#### Scenario: DB 为空时进程启动

- **WHEN** `nodes` 表为空，配置文件 `auto_start: true`
- **THEN** 进程启动成功，HTTP 服务就绪，`GET /v1/nodes` 返回空数组 `[]`

#### Scenario: DB 有节点时 auto_start

- **WHEN** DB 中存在 2 条节点记录，配置文件 `auto_start: true`
- **THEN** 进程启动后自动调用两个节点的启动操作，状态最终为 `online`（若注册成功）或 `fault`（若注册失败）

### Requirement: 废弃 YAML 节点配置

配置文件中的 `nodes` 段、`registration` 段（作为 `nodes` 子段）、`platform` 段（作为 `nodes` 子段）不再被装配代码消费。组合根在启动时若检测到这些段存在 MUST 记录一条 `warn` 级别日志后继续启动，忽略这些段的内容。

#### Scenario: 旧配置文件仍有 nodes 段

- **WHEN** 配置文件包含 `nodes:` 段
- **THEN** 进程启动日志包含 `warn` 级别日志 `"configuration contains deprecated nodes section; ignoring"`，节点装配不读取该段

### Requirement: 迁移脚本

系统 MUST 提供数据库迁移脚本（位于 `internal/storage/migrations/`），每次 schema 变更新增一个带版本号前缀的 SQL 文件（如 `001_runtime_node_storage.sql`），迁移在 `storage.Bootstrap` 执行时按顺序应用。

#### Scenario: 首次启动执行迁移

- **WHEN** 进程首次启动，数据库文件不存在
- **THEN** `storage.Bootstrap` 创建 DB 文件，执行 `001_runtime_node_storage.sql`，然后执行后续迁移

#### Scenario: 重复启动跳过已应用迁移

- **WHEN** 进程再次启动，数据库已存在且 `schema_meta` 表记录已应用迁移 001
- **THEN** 迁移脚本重新执行时不重复写入数据
