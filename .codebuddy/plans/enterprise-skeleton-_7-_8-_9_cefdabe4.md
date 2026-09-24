---
name: enterprise-skeleton-§7-§8-§9
overview: 完成 Interface 层迁移(§7)、Storage Adapter 标识(§8)、cmd 入口重写(§9) 三个任务段
todos:
  - id: http-migration
    content: 迁移 internal/api → internal/interface/http/，路由前缀 /api 改为 /v1
    status: completed
  - id: webui-migration
    content: 迁移 internal/webui → internal/interface/webui/，更新 embed 路径
    status: completed
    dependencies:
      - http-migration
  - id: verify-build-7
    content: 验证 go build ./... 通过
    status: completed
    dependencies:
      - http-migration
      - webui-migration
  - id: storage-port
    content: 创建 internal/domain/port/storage.go 定义 Storage 接口
    status: completed
  - id: storage-assertion
    content: 在 internal/storage/storage.go 添加 var _ port.Storage = (*Store)(nil) 编译期断言
    status: completed
    dependencies:
      - storage-port
  - id: main-rewrite
    content: 重写 cmd/gb28181-simulator/main.go 使用 ServiceContext 容器
    status: completed
    dependencies:
      - storage-assertion
  - id: sipprobe-rewrite
    content: 重写 cmd/sipprobe/main.go 使用 ServiceContext 容器
    status: completed
    dependencies:
      - main-rewrite
  - id: verify-cmd-build
    content: 验证 go build ./cmd/... 通过
    status: completed
    dependencies:
      - main-rewrite
      - sipprobe-rewrite
---

## 任务概述

用户要求完成两个任务：

1. §4 OTel trace provider + servicectx 容器 — 重新验证测试（已验证通过）
2. §7/§8/§9 — Interface层迁移 + Storage Adapter标识 + cmd入口重写

## §4 验证结果（已完成）

- `internal/platform/observability/tracing/provider_test.go` — 6 tests PASS
- `internal/platform/servicectx/container_test.go` — 14 tests PASS
- 全部测试通过

## §7 Interface层：HTTP与WebUI迁移

- 7.1 搬迁 `internal/api` → `internal/interface/http/`，路由前缀从 `/api` 改为 `/v1`
- 7.2 搬迁 `internal/webui` → `internal/interface/webui/`
- 7.3 验证 `go build ./...` 通过

## §8 Storage包标识为Adapter

- 8.1 在 `internal/domain/port/storage.go` 定义 `Storage` 接口（CRUD/List/Close）
- 8.2 在 `internal/storage/storage.go` 添加编译期断言 `var _ port.Storage = (*Store)(nil)`

## §9 cmd装配入口重写

- 9.1 重写 `cmd/gb28181-simulator/main.go` 使用 ServiceContext 容器进行依赖注入
- 9.2 重写 `cmd/sipprobe/main.go` 使用 ServiceContext 容器
- 9.3 验证 `go build ./cmd/...` 通过

## 核心功能

- HTTP路由迁移（/api/ *→ /v1/*）
- WebUI静态资源迁移
- Storage适配器编译期接口断言
- cmd入口使用ServiceContext泛型容器进行依赖注入组装

## 技术栈

- Go 1.21+ 
- Clean Architecture (DDD六边形架构)
- ServiceContext 手写泛型依赖注入容器
- OpenTelemetry tracing

## 实现方案

### §7 Interface层迁移

1. **7.1** 创建 `internal/interface/http/` 目录，迁移 `internal/api` 全部文件

- 更新所有 import 路径引用
- 路由前缀从 `/api/health`、`/api/version`、`/api/logs/stream` 改为 `/v1/health`、`/v1/version`、`/v1/logs/stream`
- 保留 `/healthz` 和 `/metrics` 端点（smoke test 要求）

2. **7.2** 创建 `internal/interface/webui/` 目录，迁移 `internal/webui` 全部文件

- 更新 embed FS 路径引用（`internal/webui/dist` → `internal/interface/webui/dist`）

3. **7.3** 验证全量编译通过

### §8 Storage Adapter标识

1. **8.1** 创建 `internal/domain/port/storage.go` 定义接口：

```
type Storage interface {
CRUD(ctx context.Context, entity interface{}) error
List(ctx context.Context, query interface{}) (interface{}, error)
Close() error
}
```

2. **8.2** 在 `internal/storage/storage.go` 末尾添加编译期断言：

```
var _ port.Storage = (*Store)(nil)
```

- 需要将 `*sql.DB` 包装为 `Store` 类型实现 `port.Storage`

### §9 cmd入口重写

1. **9.1** 重写 `cmd/gb28181-simulator/main.go`：

- 使用 `servicectx.NewContainer()` 创建容器
- Provide: ConfigKey, LoggerKey, TracingKey, StorageKey
- Build 后使用 MustGet 注入各组件
- 日志顺序：config → logger → tracing → http-server

2. **9.2** 重写 `cmd/sipprobe/main.go`：

- 同上，使用 ServiceContext 容器
- sipprobe 只需要 Config/Logging/Tracing

3. **9.3** 验证 `go build ./cmd/...` 通过

## 架构设计

- Interface层保持轻量，仅做HTTP处理和静态资源服务
- Storage作为Adapter层，实现domain/port.Storage接口
- cmd入口使用ServiceContext容器实现依赖注入，避免手动 wiring

## 目录结构

```
internal/
├── interface/
│   ├── http/          # [NEW] 迁移自 internal/api
│   │   ├── server.go
│   │   ├── server_test.go
│   │   └── version.go
│   └── webui/         # [NEW] 迁移自 internal/webui
│       ├── dist.go
│       ├── dist_test.go
│       └── embed/
├── domain/port/
│   └── storage.go     # [NEW] Storage接口定义
├── storage/
│   └── storage.go     # [MODIFY] 添加编译期断言
└── ...

cmd/
├── gb28181-simulator/
│   └── main.go        # [MODIFY] 使用ServiceContext
└── sipprobe/
    └── main.go        # [MODIFY] 使用ServiceContext
```

## 关键实现细节

- 路由迁移保持向后兼容：旧路由 `/api/*` 返回 301 重定向到 `/v1/*`
- Storage接口使用 `io.Closer` 模式，Close方法实现接口
- ServiceContext容器保证按Provide顺序Build，反序Close
- 编译期断言确保Adapter实现Port接口，无需运行时检查