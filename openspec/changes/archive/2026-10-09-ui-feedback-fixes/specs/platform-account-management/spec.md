## ADDED Requirements

### Requirement: Password Field Accepts Empty String in Account Creation

系统 MUST 支持在创建账号时 `password` 字段为空字符串（空密码是 GB/T 28181 某些部署场景的合法配置）。`POST /v1/platforms/:id/accounts` 请求体中 `username` 字段仍 MUST 为非空字符串且长度 MUST 等于 20 位数字国标编码；`password` 字段 MAY 为空字符串，此时系统 MUST 接受并持久化该账号。

#### Scenario: Platform operator creates an account with empty password

WHEN platform operator fills in username field with "34020000001310000001" and leaves password field empty
AND submits the account creation form
THEN system MUST accept the request with HTTP 201
AND persist the account with an empty password
AND subsequent SIP REGISTER requests from device "34020000001310000001" MUST be authenticated using the empty password

#### Scenario: Platform operator creates an account with non-empty password

WHEN platform operator fills in username field with "34020000001310000002" and password field with "securePass123"
AND submits the account creation form
THEN system MUST accept the request with HTTP 201
AND persist the account with password "securePass123"
AND subsequent SIP REGISTER requests from device "34020000001310000002" MUST be authenticated using password "securePass123"

### Requirement: Password Field Accepts Empty String in Password Update

系统 MUST 支持修改账号密码时 `password` 字段为空字符串（用于清空密码或设置为空）。`PUT /v1/platforms/:id/accounts/:username/password` 请求体中 `password` 字段 MAY 为空字符串。

#### Scenario: Platform operator resets account password to empty

WHEN platform operator selects an existing account "34020000001310000001"
AND changes the password to empty string
AND submits the password update form
THEN system MUST accept the request with HTTP 204
AND subsequent SIP REGISTER requests from device "34020000001310000001" MUST be authenticated using the empty password
