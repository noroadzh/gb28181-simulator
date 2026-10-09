# Spec Delta

## Purpose

覆盖 platform-large 节点下级注册账号的 SQLite 持久化、启动 seed、HTTP 管理 API、即时生效语义与密码保密约束，使运维人员可在运行时增删改账号且重启不丢。

## ADDED Requirements

### Requirement: 平台账号落 SQLite 持久化

系统 MUST 将 platform-large 节点的下级注册账号持久化到 SQLite `platform_accounts` 表（`node_id` + `username` 复合主键、`password`、`created_at`）。内存中的 `CredentialStore` 查询 MUST 命中该持久化层，使运行时增删改的账号对 acceptor 的下一次 REGISTER 即时生效。密码 MUST NOT 出现在日志、错误 body 或 HTTP 响应中。

#### Scenario: 首次启动从 YAML seed 入库

- **WHEN** platform-large 节点首次启动且 `platform_accounts` 表为空，YAML 中声明了 2 个 account
- **THEN** 2 条记录被幂等写入 `platform_accounts`；重复启动不产生重复行，也不覆盖已存在的运行时改动
- **AND** acceptor 收到对应 username 的 REGISTER 时能按库里密码完成鉴权

#### Scenario: 运行时新增账号即时生效

- **WHEN** 通过 HTTP API 新增 account `34020000001320000099`，随后下级用该 username 发起 REGISTER
- **THEN** 鉴权按新增的密码通过、回 200 OK 并入在线设备表；无需重启

#### Scenario: 运行时删除账号即时生效

- **WHEN** 通过 HTTP API 删除一个已注册的 account，随后该下级重注册
- **THEN** acceptor 以未知 username 回 403、不下发新挑战；在线设备表中的既有记录本能力不主动踢（踢线归 platform-large-node 的有效期清扫）

#### Scenario: 改密码后旧密码失效

- **WHEN** 通过 HTTP API 修改某 username 的密码，下级以旧密码重注册
- **THEN** response 校验失败、回 403；下级以新密码重注册可通过

#### Scenario: 密码不出现在任何对外输出

- **WHEN** 列表、新增、删除、改密码任一 API 被调用，或 acceptor 鉴权失败
- **THEN** 响应 body 与日志均不含密码明文；错误信息只携带 username 与 node id

### Requirement: 账号管理 HTTP API

系统 MUST 提供以下端点且仅对 platform-large 节点可用（非 platform-large 节点回 404）：

- `GET /v1/platforms/:id/accounts` —— 列出该节点全部账号（username、created_at，不含密码）
- `POST /v1/platforms/:id/accounts` —— 新增（body：username + password）；username 重复回 409
- `DELETE /v1/platforms/:id/accounts/:username` —— 删除；不存在回 404
- `PUT /v1/platforms/:id/accounts/:username/password` —— 改密码（body：password）

#### Scenario: 列表只含元信息

- **WHEN** GET 账号列表
- **THEN** 返回数组，每项含 `username` 与 `created_at`，MUST NOT 包含密码字段

#### Scenario: 重复 username 回 409

- **WHEN** POST 新增一个已存在的 username
- **THEN** 回 409 Conflict，原账号密码不被覆盖

#### Scenario: 删除不存在账号回 404

- **WHEN** DELETE 一个不存在的 username
- **THEN** 回 404 Not Found

#### Scenario: 非 platform 节点回 404

- **WHEN** 对一个 device 或 platform-small 节点调用上述任一端点
- **THEN** 回 404 Not Found
