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

GB 35114 A 级要求在 Digest 交换上附加 SM2 公钥签名。本模拟器采用进程级身份：同一进程内的所有节点共享 `security.gb35114` 配置的 SM2 密钥对，节点级凭据不携带密钥对。启用后，REGISTER 请求的 Authorization 必须（MUST）包含 `security-info` 指令，其内容为对 Digest response 的 SM2 签名。当设备侧收到平台的 REGISTER `200 OK` 且该响应携带 `SecurityInfo` 头时，必须（MUST）用同一密钥对的公钥验证签名，验证失败必须（MUST）让本次注册失败。当平台侧收到携带 `security-info` 的 REGISTER 且本进程持有密钥对时，必须（MUST）验证签名，并在 `200 OK` 中附带自身签名。未启用 `security.gb35114` 的进程必须（MUST NOT）发送或要求 `SecurityInfo` 头。

#### Scenario: 持 SM2 密钥的设备在 REGISTER 上发送 SecurityInfo

- **WHEN** 进程启用 `security.gb35114` 并以 SM3 Digest 发送 REGISTER
- **THEN** Authorization 携带 `security-info` 指令，其 payload 是对 Digest response 的有效 SM2 签名

#### Scenario: 平台验证 SM2 签名并以自身签名应答

- **WHEN** 启用了 `security.gb35114` 的平台收到携带有效 SM2 签名的 REGISTER
- **THEN** `200 OK` 包含 `SecurityInfo` 头，内容为平台的 SM2 签名

#### Scenario: 设备验证平台的 SM2 签名

- **WHEN** 启用了 `security.gb35114` 的设备收到携带 `SecurityInfo` 的 `200 OK`
- **THEN** 设备用自身配置的公钥验证该签名覆盖了自己发出的 Digest response
- **AND** 验证失败时注册以失败结束，而不是报告成功

#### Scenario: 无 SM2 密钥的节点使用普通 Digest

- **WHEN** 进程未启用 `security.gb35114`
- **THEN** 其 REGISTER 不携带 `security-info` 指令
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

SM2 密钥与 SM3 哈希默认必须（MUST）关闭。未启用 `security.gb35114` 的进程必须（MUST）与本变更之前行为完全一致：MD5 Digest 认证、无 SM2 头、无 SM3 头。所有未包含 SM2 配置的既有测试 fixture 必须（MUST）无需修改即可通过。

#### Scenario: 开箱节点使用 MD5

- **WHEN** 进程启动时配置中不含 `security.gb35114`
- **THEN** 其 REGISTER 使用 `algorithm=MD5`
- **AND** `200 OK` 使用 `algorithm=MD5`
- **AND** 两个方向均不出现 `SecurityInfo` 头

### Requirement: 进程级 GB 35114 配置

GB 35114 身份必须（MUST）在进程级配置 `security.gb35114` 中声明，而不是逐节点声明：同一进程内的所有节点共享同一对 SM2 密钥。`enabled` 默认必须（MUST）为 false。启用时 `sm2_private_key_file` 与 `sm2_public_key_file` 必须（MUST）同时提供且可解析；任一缺失、不可读或解析失败必须（MUST）让进程启动失败，而不是让每次注册在运行期失败。启用后，服务端挑战必须（MUST）声明 `algorithm=SM3`；`note_integrity` 为 true 时挑战的 Note 必须（MUST）按 SM3 计算。未启用时挑战必须（MUST）继续声明 `algorithm=MD5`。

#### Scenario: 启用后挑战切换为 SM3

- **WHEN** `security.gb35114.enabled` 为 true
- **THEN** 401 挑战的 `WWW-Authenticate` 声明 `algorithm=SM3`
- **AND** `note_integrity` 为 true 时该挑战带 SM3 完整性保护的 Note

#### Scenario: 未启用时保持 MD5

- **WHEN** 配置中不含 `security.gb35114` 或 `enabled` 为 false
- **THEN** 401 挑战的 `WWW-Authenticate` 仍声明 `algorithm=MD5`
- **AND** 请求与响应都不出现 `SecurityInfo` 头

#### Scenario: 缺少密钥文件时启动失败

- **WHEN** `enabled` 为 true 但私钥或公钥文件路径为空、不可读或内容无法解析
- **THEN** 进程在启动阶段报错退出，不会进入服务状态
