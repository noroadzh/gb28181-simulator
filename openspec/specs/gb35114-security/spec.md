# gb35114-security 规范

## Purpose

GB 35114 安全扩展能力。本 capability 覆盖 SM3 Digest 认证算法（SM3 / SM3-sess）、SM2 证书互认证与安全编解码，全部基于纯 Go 实现（无 CGO）。

## Requirements

### Requirement: SM3 Digest 算法

SIP Digest 认证适配器必须（MUST）在既有 `algorithm=MD5` 与 `algorithm=MD5-sess` 之外支持 `algorithm=SM3` 与 `algorithm=SM3-sess`。当对端声明 SM3 时，response 字段必须（MUST）用 SM3 而非 MD5 计算，同时保留相同的 qop/nc/cnonce 管线。`algorithm=` 取值不是这四个可识别名称之一时必须（MUST）返回 `ErrUnknownAlgorithm`。

#### Scenario: SM3 挑战产生 SM3 响应

- **WHEN** 平台下发携带 `algorithm=SM3` 的 `WWW-Authenticate`
- **THEN** 合规客户端应答时在 Authorization 中声明 `algorithm=SM3`，response 按 SM3 计算

#### Scenario: 未知算法被拒绝

- **WHEN** 对端声明一个未知的 `algorithm=` 取值
- **THEN** `Responder.Verify` 返回 `ErrUnknownAlgorithm`

### Requirement: SM2 互认证签名

GB 35114 A 级要求在 Digest 交换上附加 SM2 公钥签名。当设备节点凭据携带 SM2 密钥对时，REGISTER 请求必须（MUST）包含 `SecurityInfo` 头，其内容为对 Digest response 的 SM2 签名。当平台凭据携带 SM2 公钥时，REGISTER 的 `200 OK` 必须（MUST）包含对应的 `SecurityInfo` 头。未配置 SM2 密钥对的节点必须（MUST NOT）发送或要求 `SecurityInfo` 头。

#### Scenario: 持 SM2 密钥的设备在 REGISTER 上发送 SecurityInfo

- **WHEN** 设备节点配置了 SM2 私钥并以 SM3 Digest 发送 REGISTER
- **THEN** REGISTER 携带 `SecurityInfo` 头，其 payload 是对 Digest response 的有效 SM2 签名

#### Scenario: 平台验证 SM2 签名并以自身签名应答

- **WHEN** 平台收到携带有效 SM2 签名的 REGISTER
- **THEN** `200 OK` 包含 `SecurityInfo` 头，内容为平台的 SM2 签名
- **AND** 设备注册器在标记注册成功之前验证平台的 SM2 签名

#### Scenario: 无 SM2 密钥的节点使用普通 Digest

- **WHEN** 设备节点未配置 SM2 凭据
- **THEN** 其 REGISTER 不携带 `SecurityInfo` 头
- **AND** `200 OK` 不携带 `SecurityInfo` 头

### Requirement: SM3 Note 完整性

当对端协商 `algorithm=SM3` 时，`WWW-Authenticate` 的 Note 字段必须（MUST）受 SM3 完整性保护。服务端按 `SM3(nonce + realm + timestamp)` 计算 Note，客户端原样回显。服务端重新哈希并比对；不一致必须（MUST）按畸形 Authorization 处理。对 `algorithm=MD5` 则逐字节沿用既有的 opaque 随机串 Note。

#### Scenario: SM3 Note 完整性校验

- **WHEN** 平台发送携带 `algorithm=SM3` 与 SM3 哈希 Note 的 `WWW-Authenticate`
- **THEN** 客户端回显相同的 Note 值
- **AND** 当 Note 重算出相同 SM3 哈希时服务端接受该响应

#### Scenario: 伪造的 SM3 Note 被拒绝

- **WHEN** 客户端回显被篡改的 Note 值
- **THEN** 服务端返回 `ErrMalformedAuthorization`

#### Scenario: MD5 Note 保持不变

- **WHEN** 平台发送携带 `algorithm=MD5` 的 `WWW-Authenticate`
- **THEN** Note 字段是既有的 opaque 随机串，与 Change 12 之前输出字节一致

### Requirement: 默认关闭配置

SM2 密钥与 SM3 哈希默认必须（MUST）关闭。profile 未声明 SM2 凭据的节点必须（MUST）与本变更之前行为完全一致：MD5 Digest 认证、无 SM2 头、无 SM3 头。所有未包含 SM2 配置的既有测试 fixture 必须（MUST）无需修改即可通过。

#### Scenario: 开箱节点使用 MD5

- **WHEN** 节点启动时配置中不含任何 SM2 凭据
- **THEN** 其 REGISTER 使用 `algorithm=MD5`
- **AND** `200 OK` 使用 `algorithm=MD5`
- **AND** 两个方向均不出现 `SecurityInfo` 头
