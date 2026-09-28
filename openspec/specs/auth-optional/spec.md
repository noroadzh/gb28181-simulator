# auth-optional 规范

## Purpose

`auth-optional` 为 simulator 的注册认证引入可选的无密码模式，允许在测试、内网或设备直连平台等不需要 Digest 认证的场景下跳过密码验证。

## Requirements

### Requirement: Registration Supports Empty Password

当 device 节点或 platform-small 节点的 `registration` 配置段声明 `allow_no_auth: true` 时，`registration.password` 字段 MUST 可为空字符串，且配置加载 MUST 不报错。

#### Scenario: Device registers without password
- **WHEN** device 节点配置 `registration.allow_no_auth: true` 且 `registration.password` 为空
- **THEN** `NewRegistration` 成功返回 Registration 对象，不报 "has no password" 错误
- **AND** 该节点发送 REGISTER 时在 Authorization 头携带 `username=<node-id>` 和空的 `response=` 字段

#### Scenario: Device registers without password — platform accepts
- **WHEN** device 节点已配置 `registration.allow_no_auth: true` 且向一个 `platform.allow_no_auth: true` 的 platform 注册
- **THEN** platform 侧收到 REGISTER 后，跳过 Digest 密码验证，直接接受注册并授予 Expires 时长

#### Scenario: Device with password — no_auth=false still requires password
- **WHEN** device 节点 `allow_no_auth` 为 false（默认）且 `password` 为空
- **THEN** 配置加载失败，错误信息为 `config: nodes[i].registration: model: registration for <server> has no password`

#### Scenario: Platform with no accounts accepts unregistered devices
- **WHEN** platform 节点配置 `platform.allow_no_auth: true` 且 `platform.accounts` 列表为空
- **THEN** 任何 device 向该 platform 发送 REGISTER 时，platform 不做密码校验，直接接受注册

### Requirement: Platform Authorizer Skips Password When No-Auth Mode

`allow_no_auth` 模式下的平台授权器在验证 Digest 响应时，如果 device 的 Authorization header 中 `response=` 字段为空且节点配置了 `allow_no_auth: true`，则 MUST 跳过密码比较。

#### Scenario: Authorizer skips password for no-auth device
- **WHEN** 收到 REGISTER，Authorization header 中 response 为空，源节点配置 `allow_no_auth: true`
- **THEN** `Authorizer.Check` 返回 `Authorized`，不执行 `password == expected` 比较
- **AND** 日志记录等级为 `debug`，内容包含 "no-auth registration accepted"

#### Scenario: Authorizer still checks password for regular device
- **WHEN** 收到 REGISTER，Authorization header 中 response 非空，源节点未配置 `allow_no_auth`
- **THEN** `Authorizer.Check` 执行正常的 Digest 密码验证，密码错误时返回 `Unauthorized`

### Requirement: Deploy Docs Reflect No-Auth Pattern

部署文档 MUST 提供两种配置模式示例：有密码（生产环境）和无密码（测试/内网），并 MUST 明确说明每种场景的适用条件。

#### Scenario: deploy-linux.md contains no-auth example
- **WHEN** 运维工程师打开 `docs/deploy-linux.md`
- **THEN** 文档中包含 `allow_no_auth: true` 的最小可用配置样例
- **AND** 文档"常见故障"章节包含"密码为空导致注册失败"的排查条目

#### Scenario: deploy-docker-compose.md contains no-auth device example
- **WHEN** 运维工程师打开 `docs/deploy-docker-compose.md`
- **THEN** device 节点的配置示例展示 `allow_no_auth: true` 用法
- **AND** platform 节点示例展示 `platform.allow_no_auth: true` 用法