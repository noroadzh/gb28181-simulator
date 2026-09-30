# logging-user-docs Specification

## Purpose

`docs/logging.md` 是运维与二次开发者配置、检查、排查模拟器日志的单一入口文档。
本 capability 规定该文档必须覆盖的内容边界，以及它必须准确描述的契约：
`log.*` 配置项总表、`log.modules` 前缀最长匹配规则、`PATCH /v1/config/log` 的运行时
热更新语义、component/subsystem 标签对照表、媒体聚合摘要语义与故障排查手册。
它不定义日志系统的代码行为（那属于 `logging-coverage`），只约束文档本身的完整性与准确性。

## Requirements

### Requirement: Log configuration reference

`docs/logging.md` MUST document every field of the `log.*` YAML configuration block (`level`, `file`, `add_source`, `redact_keys`, `modules`) with: YAML key, type, default value, and one-line effect description. The `modules` field MUST be described as `map[string]Level` keyed by `component` prefix.

#### Scenario: every config field is covered
- **WHEN** a reader looks up any `log.*` field present in `internal/platform/config.LogConfig`
- **THEN** `docs/logging.md` contains a table row for that field with its YAML key, type and default

#### Scenario: level enumeration is accurate
- **WHEN** the document lists the supported level values
- **THEN** it lists exactly `trace`, `debug`, `info`, `warn`, `error` (matching `logging.ParseLevel`), and notes that unknown values fall back to `info`

### Requirement: Module prefix matching semantics

The document MUST state that `log.modules` keys match against a record's `component` attribute using longest-prefix matching, and that unmatched records fall back to the global `log.level`.

#### Scenario: longest prefix wins
- **WHEN** `log.modules` contains both `internal/app: warn` and `internal/app/sip_acceptor: trace`
- **THEN** the document explains that records tagged `component=internal/app/sip_acceptor` use `trace`, while other `internal/app` records use `warn`

### Requirement: Runtime PATCH endpoint contract

The document MUST describe the `PATCH /v1/config/log` contract: request schema (`level` optional, `modules` optional, at least one required), response schema (echo of effective values), `400` semantics for empty body / unknown level / malformed JSON, and the memory-only semantics (a process restart reverts to `log.level` and `log.modules` from the config file).

#### Scenario: success and error cases documented
- **WHEN** a reader consults the endpoint section
- **THEN** they find the three documented 400 error cases and the exact request/response schema, consistent with `internal/interface/http/log_config.go`

#### Scenario: restart rollback is explicit
- **WHEN** the document discusses the endpoint
- **THEN** it states explicitly that updates are memory-only and revert on restart

### Requirement: Component and subsystem tag table

The document MUST include a table mapping every logger `subsystem` tag emitted by the codebase (at minimum: `sip_acceptor`, `keepalive_keeper`, `device_registrar`, `node_service`, `fault_store`, `cascade_handler`, capture store, media sources/packetizers) to the owning package, so operators can choose meaningful `log.modules` keys.

#### Scenario: operator can pick a module key
- **WHEN** an operator wants to silence cascade forwarding chatter
- **THEN** the table shows `component=internal/adapter/cascade` (`subsystem=cascade_handler`) as the `log.modules` key to tune

### Requirement: Media aggregation summary semantics

The document MUST explain that `internal/adapter/media` summary records are produced by `ErrorAggregator` with a 60-second window per signature (`ps-mux`, `rtp-send`), one summary line per signature per window, and that these lines can be tuned via `log.modules` with the `internal/adapter/media` prefix.

#### Scenario: flood-free hot loop errors
- **WHEN** the RTP send loop fails repeatedly
- **THEN** the document explains that instead of per-packet log lines, one aggregated summary per 60s window appears, tagged `component=internal/adapter/media`

### Requirement: Troubleshooting playbook

The document MUST include at least one worked troubleshooting example: how to temporarily raise a subsystem to `trace` via `PATCH /v1/config/log`, observe the effect, and revert it — including the equivalent `log.modules` static configuration.

#### Scenario: trace a single subsystem at runtime
- **WHEN** an operator follows the playbook
- **THEN** they can enable `trace` for `sip_acceptor` without a restart and revert to file-backed defaults by restarting or issuing a compensating PATCH