# Spec Delta: device-node

## ADDED Requirements

### Requirement: 注册事务的对端匹配使用地址语义等价

系统在判定注册 / 心跳 / 注销事务的响应来源时，MUST 使用地址语义等价而非字符串严格相等：端口数字必须一致；host 部分使用 IP 语义比较（`net.ParseIP` + `Equal`）或主机名与 IP 之间的 DNS 解析后比较。当配置的 `registration.server` 使用主机名（例如 Docker 服务名、域名）而实际 UDP 源地址为 IP 字面量时，MUST 仍能正确关联响应，不丢弃合法响应。

#### Scenario: 配置使用 Docker 服务名时响应可被正确关联

- **WHEN** 节点的 `registration.server` 配置为 `gbsim-platform:5060`（Docker 服务名），而真实 UDP 源地址为 `172.26.0.2:5060`
- **THEN** 注册事务能正确匹配到平台返回的 401 / 200 OK，不因主机名与 IP 字符串不一致而丢弃响应
- **AND** 节点在该事务内应答 401 Digest 挑战并最终推进到 `online`

#### Scenario: 配置使用 IP 字面量时行为不变

- **WHEN** 节点的 `registration.server` 配置为 `127.0.0.1:5060`（IP 字面量），平台直接以 `127.0.0.1:5060` 响应
- **THEN** 按 IP 语义比较得到 true，事务正常推进
- **AND** 端口不一致时（如收到 `127.0.0.1:5061` 的响应）返回 false 并继续等待，不会错误接受

#### Scenario: 端口不一致时仍被拒绝

- **WHEN** `reg.Server()` 为 `gbsim-platform:5060`，而 `Receive()` 返回的 peer 为 `172.26.0.2:5061`（端口不同）
- **THEN** 对端匹配在端口比较阶段即返回 false，该响应被忽略并继续等待

#### Scenario: 主机名解析失败时保守丢弃

- **WHEN** 节点的 `registration.server` 配置为 `platform.example.com:5060`，但 DNS 解析失败
- **THEN** 对端匹配返回 false，该响应被忽略并继续等待
- **AND** `Send` 阶段同样会因为 DNS 解析失败而返回错误，整体行为一致退化，不掩盖底层故障
