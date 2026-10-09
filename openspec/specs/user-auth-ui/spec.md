# user-auth-ui Specification

## Purpose
为 Web 后台提供简单的用户登录与会话管理。本期不引入完整 RBAC（用户明确排除服务扩展），仅实现"登录 + 修改密码 + 会话 token"基础能力。

## Requirements

### Requirement: 登录 API
- `POST /v1/auth/login` MUST 接收 `{username, password}`，校验后返回 `{token, expires_at}`
- 默认凭据 admin/admin（可通过 YAML `auth.users` 配置）
- 错误凭据 MUST 返回 401

#### Scenario: 正确登录
- **WHEN** POST /v1/auth/login body=`{username:"admin", password:"admin"}`
- **THEN** 200 + `{token:"...", expires_at:"..."}`

#### Scenario: 错误凭据
- **WHEN** POST /v1/auth/login 错误密码
- **THEN** 401 + JSON 错误

### Requirement: 修改密码
- `POST /v1/auth/password` MUST 接收 `{old_password, new_password}`，校验旧密码后更新并返回 204

#### Scenario: 修改密码成功
- **WHEN** POST /v1/auth/password body 含正确旧密码
- **THEN** 204，token 失效需重新登录

#### Scenario: 旧密码错误
- **WHEN** POST /v1/auth/password body 含错误旧密码
- **THEN** 401 + JSON 错误

### Requirement: 会话 token 校验
中间件 MUST 校验 `Authorization: Bearer <token>` 头；缺失或非法 MUST 返回 401。`/v1/auth/login` 不受中间件保护。

#### Scenario: 访问受保护端点
- **WHEN** 请求带 `Authorization: Bearer <valid>`
- **THEN** 通过校验，handler 正常执行

#### Scenario: 缺失 token
- **WHEN** 请求无 Authorization 头
- **THEN** 401 + JSON 错误
