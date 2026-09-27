# Spec Delta

## Purpose

Adjusts the existing `platform-large-node` capability so that registration
acceptance satisfies the GB/T 28181-2022 X-GB-Ver negotiation (Annex I):
the `200 OK` response echoes the version the downstream device declared, and
the recorded downstream device exposes the recorded version for later
version-gated behaviour.

## ADDED Requirements

### Requirement: platform-large echoes X-GB-Ver in 200 OK

When a platform-large node accepts a REGISTER and the incoming request
carries an `X-GB-Ver` header, the `200 OK` response MUST include the same
`X-GB-Ver` header value. When the request does not carry the header, the
response MUST NOT include the header.

#### Scenario: 2022 register receives echoed version

- **WHEN** a device sends REGISTER with `X-GB-Ver: 2022`
- **THEN** the resulting `200 OK` carries `X-GB-Ver: 2022`

#### Scenario: 2016 register receives no version echo

- **WHEN** a device sends REGISTER without `X-GB-Ver`
- **THEN** the resulting `200 OK` does not carry `X-GB-Ver`

### Requirement: platform-large stores downstream GBVersion

The platform-large node MUST persist the downstream's `GBVersion` as part of
the `DownstreamDevice` record created (or updated) by registration. The
recorded value MUST be accessible by the app layer when constructing
version-gated MANSCDP responses.

#### Scenario: GBVersion populated on successful registration

- **WHEN** a device registers with `X-GB-Ver: 2022`
- **THEN** the persisted `DownstreamDevice.GBVersion` equals `"2022"`

#### Scenario: re-registration overwrites the recorded version

- **WHEN** a device first registers with `X-GB-Ver: 2016` and later
  re-registers with `X-GB-Ver: 2022`
- **THEN** the persisted record reflects `GBVersion == "2022"` after the
  second registration

## MODIFIED Requirements

### 需求：platform-large 节点作为 UAS 接受下游 REGISTER

platform-large 节点必须在其自有传输上作为 UAS：收到 REGISTER 后必须先发出质询，
验证下游回传的凭据，然后才以 200 OK 授予注册。授予的响应必须携带协商后的 `Expires`，
回显下游的 `Contact`，并从请求复制事务标识符（`Via`、`From`、带标签的 `To`、
`Call-ID`、`CSeq`），以便下游将应答与其事务匹配。当请求携带 `X-GB-Ver`
头时，200 OK 必须回显该头的原值；请求未携带时，响应不得出现该头。
在线设备表中的下级记录必须包含记录到的 GB 版本，供后续版本门控行为查询。

#### 场景：首次 REGISTER 触发 401 挑战

- **WHEN** 平台收到一条不含 `Authorization` 的 REGISTER
- **THEN** 回 401，带 `WWW-Authenticate: Digest realm="<realm>", nonce="...", qop="auth", algorithm=MD5`
- **AND** 响应回抄请求的 `Via` / `From` / `To`（带 tag）/ `Call-ID` / `CSeq`；不修改在线设备表

#### 场景：凭据正确后回 200 OK 并入表

- **WHEN** 下级以 `Authorization` 重发 REGISTER，且 username 在平台 `accounts` 内、response 校验通过
- **THEN** 回 200 OK，`Expires` 为协商后的有效期，`Contact` 与请求一致，并带 `Date`
- **AND** 该下级被记入在线设备表（deviceID、来源地址、Contact、传输、注册时刻、授予有效期、最后活跃时刻、GB 版本）

#### 场景：2022 REGISTER 的 200 OK 回显版本

- **WHEN** 下级成功注册且请求携带 `X-GB-Ver: 2022`
- **THEN** 200 OK 携带 `X-GB-Ver: 2022`，且在线表记录 `GBVersion == "2022"`

#### 场景：无版本头的 REGISTER 保持 2016 行为

- **WHEN** 下级成功注册且请求未携带 `X-GB-Ver`
- **THEN** 200 OK 不含 `X-GB-Ver` 头，在线表记录的 GB 版本为空
