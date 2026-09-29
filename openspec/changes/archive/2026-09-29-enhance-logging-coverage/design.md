# Design

## Context

当前 logging 子系统 (`internal/platform/observability/logging`) 已在 Change 1 中实现, 提供:
- `log/slog` 全局 logger (`L()`)、自定义 `MultiHandler` 同时写入文件与 Hub
- 五个级别: `trace|debug|info|warn|error`, 敏感字段脱敏 (默认含 `password`/`secret`/`private_key`/`authorization`)
- 配置 `log.level` 与 `log.file` 通过 viper 注入
- WebSocket 端点 `/v1/logs/stream` 订阅 Hub 实时推送

然而:
1. **覆盖面不足**: 关键业务路径（app/*、adapter/auth/*、adapter/cascade/*、adapter/media/*、capture/*）的 trace/debug 级别调用稀疏, 生产问题定位时大量"看不到过程"的盲区
2. **粒度过粗**: 顶层 `log.level` 单值控制, 无法在生产 info 的前提下, 对单个模块临时开启 debug/trace
3. **无法热更新**: 改日志级别必须修改配置文件并重启进程

## Goals / Non-Goals

**Goals:**
- 在关键执行路径增加 60~80 个 log/slog 调用点, 覆盖 SIP/业务/节点/媒体/级联/认证/异常注入七大观测面
- 引入 `log.modules` 配置节, 支持按模块路径独立设置级别
- 引入 `PATCH /v1/config/log` HTTP 接口, 通过 viper 热重载实现运行时级别调整
- 媒体热循环中可恢复错误聚合而非逐包打日志

**Non-Goals:**
- 不引入新的日志后端（保持 slog）
- 不实现日志轮转（由外部工具负责）
- 不修改日志输出格式（保持 JSON）
- 不修改日志 WebSocket 协议
- 不持久化运行时修改（PATCH 后不写回 config.yaml）

## Decisions

### Decision 1: 使用 `log/slog.Logger.With` + 子 logger 区分模块

**方案**: 在每个关键包内定义包级变量, 例如:
```go
// internal/app/acceptor.go
var log = logging.L().With("component", "app", "subsystem", "sip_acceptor")
```

后续业务调用使用 `log.Debug(...)` / `log.Info(...)` 等。

**理由**:
- 与现有代码风格一致, 不引入新的日志接口
- 通过 `component`/`subsystem` 属性天然支持 Hub 端按节点/子系统过滤（Web UI 已有过滤能力）
- `With` 返回新 logger, 性能损耗忽略不计

**替代方案**:
- 为每个组件单独调用 `logging.L()` 并手动塞属性 → 易遗漏, 不推荐
- 引入 zap/xcobra 等第三方库 → 违反"复用现有技术栈"约束

### Decision 2: `log.modules` 配置节采用"最长前缀匹配"

**方案**: 配置节形如:
```yaml
log:
  level: info
  modules:
    "internal/app": debug
    "internal/app/sip": trace
    "internal/adapter/auth": debug
```

匹配规则: 记录的 `component`/`subsystem` 与模块路径前缀, 命中"最长前缀"。未命中任何模块则回退到全局 `log.level`。

**理由**:
- 与代码 import 路径对齐, 心智负担低
- 前缀匹配允许一组模块共享级别（如 `internal/app` 下所有子系统用 debug, 唯独 `sip` 用 trace）

**替代方案**:
- 精确匹配 → 配置冗长, 不实用
- glob 通配符 → 增加 viper 配置解析复杂度, 收益不匹配

### Decision 3: 模块级级别实现基于 MultiHandler 改造

**方案**: 在 `MultiHandler.Enabled()` 中根据当前记录的 `component` 与 `subsystem` 属性, 比对内置的 `map[modulePath]slog.Level`, 选择最终可见级别。

```go
// internal/platform/observability/logging/handler.go
type MultiHandler struct {
    defaultLevel slog.Level
    moduleLevels map[string]slog.Level  // 前缀 → 级别
    ...
}

func (h *MultiHandler) Enabled(_ context.Context, l slog.Level) bool {
    // 1. 尝试从 ctx 拿 component/subsystem（由子 logger With 注入）
    // 2. 查 moduleLevels，找最长前缀
    // 3. 比对 l
}
```

**理由**:
- 现有 `Enabled` 已存在, 改造成本低
- 子 logger 通过 `With` 注入的属性自动通过 `slog.Record` 携带, 无需额外 API

**风险**:
- 子 logger 的 `With` 属性在 `Enabled()` 中是否可用? → 验证 log/slog 行为: `With(attrs)` 创建的新 logger 在调用 `.Enabled()` 时确实将 attrs 注入到 record。需在 e2e 测试中验证。

### Decision 4: 运行时 PATCH 通过 viper 热重载

**方案**: 添加 `PATCH /v1/config/log` 端点, body 支持:
```json
{"level": "debug"}
// 或
{"modules": {"internal/app": "trace"}}
```

实现流程:
1. Echo handler 解析 body
2. 调用 `cfg.Log.Level.Set("...")` 或 `cfg.Log.Modules.Set("...")` (viper dynamic set)
3. 触发 `logging.UpdateLevels(...)` 重置 MultiHandler 内部 map 与默认 level
5. 现有 hub 订阅者无感知（共享 hub）

**理由**:
- viper 是已有依赖, 无新组件
- MultiHandler 重置级别不需要重启 goroutine

**替代方案**:
- 重建 handler + 重新 Init() → 销毁 hub 引用, 不可接受
- 引入 watch channel → 与 viper OnConfigChange 事件总线重叠, 复杂化

### Decision 5: 媒体热循环使用聚合器

**方案**: 在 `internal/adapter/media/ps_packetizer.go` 与 `rtpizer.go` 中, 引入 `pkg/aggregator` 子模块（新增文件 `internal/adapter/media/error_aggregator.go`）:
- 维护 `map[errorKey]*countedError`, 含首次时间戳
- 60 秒窗口内重复错误仅递增计数, 不打日志
- 窗口结束时输出一条 `warn` 级别聚合记录, 含错误类型、总计数、时间范围

**理由**:
- 媒体每秒数千包, 逐包错误日志会淹没整个 Hub
- 已有项目偏好组合而非框架, 维持纯 Go 自研

**风险**:
- 聚合器需要 graceful shutdown flush, 否则最后一次错误可能丢失

## Risks / Trade-offs

**[Risk 1] 子 logger `With` 属性在 `Enabled()` 中的可见性**
- log/slog 文档未明确保证: 子 logger 的属性对自定义 Enabled() 可见
- **Mitigation**: 在 `logger_test.go` 增加 e2e 测试, 验证 `L().With("component", "x").Debug(...)` 在配置 `modules."x" = debug` 时输出, 在 `info` 时不输出
- 若不可见, 备选方案: 改为全局属性（通过 context.WithValue 或自定义 logger 实现）

**[Risk 2] 性能开销**
- 每个 Enabled() 调用需要遍历 moduleLevels map 与计算前缀匹配, 在 trace 级别全开时每秒可能万次
- **Mitigation**: 使用 sync.RWMutex 保护 moduleLevels, 启用时走读锁; 限制 moduleLevels 上限（默认 ≤ 20 个模块, 极少达到瓶颈）

**[Risk 3] PATCH 接口并发安全**
- 多个并发 PATCH 请求可能竞争 moduleLevels
- **Mitigation**: MultiHandler 内部持有 RWMutex, 写锁保护级别变更

**[Risk 4] 错误聚合器时钟漂移**
- 使用 `time.Now` 检测窗口边界, 与节点时钟绑定; 重启后窗口重置
- **Mitigation**: 在 Shutdown 时强制 flush 一次, 把当前活跃窗口内累计错误输出

**[Trade-off 1] log.modules 优先级 vs slog default level**
- 选择 MultiHandler 内部覆盖, 而非多个 slog.Handler 链式
- 优点: 单 handler 单 hot path, 性能更好; 缺点: 日志代码无法用 slog 原生 `Handler.WithGroup` 实现嵌套 scope
- 接受: 项目实际不依赖 Group 功能

## Migration Plan

1. **第一阶段（不影响行为）**: 仅添加日志调用点, 默认级别不变, 不新增配置项
2. **第二阶段（向后兼容）**: 添加 `log.modules` 配置项与对应解析逻辑; 默认空, 与现状一致
3. **第三阶段（新增能力）**: 添加 `PATCH /v1/config/log` 接口
4. **第四阶段（验证）**: e2e 测试 + golden test 保持; 补全日志调用覆盖率测试

**回滚策略**: 因为不修改现有日志输出格式与默认行为, 回滚仅需 revert 代码即可, 配置文件兼容（`log.modules` 缺省视为空 map）。

## Open Questions

无。spec 已完整描述能力, 设计覆盖所有实现路径, 不存在可推迟的未知。