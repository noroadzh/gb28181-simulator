# Spec Delta

## ADDED Requirements

### Requirement: SIP transport exposes peer and destination addresses

The system SHALL make remote endpoints explicit: sending a message SHALL accept a destination
address, and receiving SHALL report the address the message arrived from. Addresses are
`host:port` strings (optionally prefixed by a transport scheme). A transport that cannot
determine the peer address MUST surface an explicit error rather than returning an empty string.

#### Scenario: Send 使用显式目标地址

- **WHEN** 调用传输层发送接口并传入目标地址 `127.0.0.1:5060`
- **THEN** 报文被发往该地址；不再依赖从报文 URI 反推目标；返回值仅表达传输层错误

#### Scenario: Receive 返回对端地址

- **WHEN** 传输层收到一条来自 `127.0.0.1:5061` 的报文
- **THEN** 接收结果同时给出报文与该对端地址 `127.0.0.1:5061`；UDP 与 TCP 两种传输均如此

#### Scenario: 对端地址可用于直接回包

- **WHEN** 以接收到的对端地址作为目标发回报文
- **THEN** 报文到达原发送方，无需额外配置路由

#### Scenario: 无法判定地址时显式报错

- **WHEN** 传输实现无法确定对端地址
- **THEN** 返回错误（而非返回空地址让调用方静默发往错误目标）

## MODIFIED Requirements

### Requirement: Diagnostic CLI `sipprobe` for golden tests

The system SHALL provide a `sipprobe` subcommand capable of binding a UDP/TCP listener and
sending a single SIP message, then printing the first received response to stdout, used by CI
golden tests and developer smoke checks. In receive-only mode it SHALL additionally be able to
answer inbound SIP requests with a 200 OK response when invoked with `--answer`, so that two
`sipprobe` processes can complete a request/response exchange without any third process.
Answering SHALL be opt-in: without `--answer` the observable behaviour is unchanged.

#### Scenario: sipprobe 接收一条 INVITE 后打印 200 OK

- **WHEN** 执行 `gb28181-simulator sipprobe --bind udp:127.0.0.1:5060 --expect-status 200` 并由外部 client 发送 INVITE
- **THEN** sipprobe 等待 ≤ 5 秒后退出 0，并将 `Response.StatusCode()` 与 `Response.StartLine()` 写入 stdout 一行（TSV 形式）

#### Scenario: sipprobe 超时退出非零

- **WHEN** 5 秒内未收到响应
- **THEN** sipprobe 退出码为 2，stderr 输出 `timeout waiting for status=200`，stdout 为空

#### Scenario: 以 --answer 回应入站请求

- **WHEN** 执行 `gb28181-simulator sipprobe --bind udp://127.0.0.1:15060 --answer` 并收到一条入站 `sip.Request`
- **THEN** sipprobe 向该请求的对端地址回送一条 200 OK（`sip.NewResponseFromRequest`），请求方收到后退出 0

#### Scenario: 两个 sipprobe 互发 INVITE/200 OK

- **WHEN** 进程 A 执行 `sipprobe --bind udp://127.0.0.1:15060 --answer`，
  进程 B 执行 `sipprobe --bind udp://127.0.0.1:15061 --send-to udp://127.0.0.1:15060 --expect-status 200`
- **THEN** B 收到 200 OK 并退出 0；A 回应后退出 0；`scripts/smoke-sip.sh` 的原始断言通过
