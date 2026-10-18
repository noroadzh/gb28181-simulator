# 配置

本页分两部分：**节点定义**（运行期可改的唯一真源，DB）和 **YAML 配置**（进程级设置，重启后依然生效）。

## 节点定义持久化

自 Change `runtime-node-configuration` 起，节点定义不再从 YAML 读取，而以 **SQLite 的 `nodes` / `node_registrations` / `node_platforms` 三张表为唯一真源**。这样做是为了让节点可以在进程运行期被创建、修改、删除，而不必重启并重新加载配置文件。

### 为什么 DB 是唯一真源

节点定义有三类数据，且生命周期各不相同：身份与监听地址（`nodes`）、向上注册的目标与心跳（`node_registrations`）、对下游提供服务时的账号与 lifetime 窗口（`node_platforms`）。把它们统一放进一个 YAML 文档，意味着改一个节点的注册地址要重写整个文件并重启进程；放进三张以 `node_id` 为主键、`ON DELETE CASCADE` 关联的表，意味着改一个字段就是一条 `UPDATE`，删一个节点会连带清掉它的注册与平台配置，不会留下指向已不存在节点的孤儿注册行。

三张表都由 `internal/storage/migrations/001_runtime_node_storage.sql` 定义，并在 `storage.Bootstrap` 中**恰好执行一次**——记录"已执行"的版本行由 `applyMigrations` 写入，而不是写在 SQL 文件里，因此迁移脚本本身不需要知道自己的文件名。

### 表结构

`nodes` —— 节点身份与信令监听地址：

| 列 | 类型 | 说明 |
|----|------|------|
| `id` | TEXT PK | 节点 ID，20 位国标编号 |
| `kind` | TEXT | `device` / `platform-small` / `platform-large` |
| `domain` | TEXT | SIP 域（realm），如 `3402000000` |
| `addr` | TEXT | 信令监听地址 `host:port` |
| `vendor` | TEXT | 厂商，默认空 |
| `cascade_parent` | TEXT | 上级节点 ID，拓扑顶端为空 |
| `cascade_children` | TEXT | 下级节点 ID 的 JSON **数组**（保序），默认 `[]` |
| `created_at` / `updated_at` | TEXT | 由 `datetime('now')` 维护 |

`node_registrations` —— 节点向上级注册，每节点至多一行：

| 列 | 类型 | 说明 |
|----|------|------|
| `node_id` | TEXT PK | 外键 → `nodes(id)`，级联删除 |
| `server` / `server_id` | TEXT | 上级地址与 20 位 ID。**两者都存**：国标把上游地址放在 URI、把 ID 单列携带，现实中只给其中一个的对端很常见 |
| `username` / `password` | TEXT | 认证账号；`password` 绝不写入日志 |
| `gb_version` | TEXT | `2016` / `2022`，默认 `2016` |
| `expires` | INTEGER | 注册有效期（秒），默认 `3600` |
| `timeout_ms` | INTEGER | 注册超时，默认 `30000` |
| `transport` | TEXT | `UDP` / `TCP`，默认 `UDP` |
| `allow_no_auth` | INTEGER | 0/1，默认 `0` |
| `heartbeat_*` | INTEGER | 心跳周期 / 超时 / 最大失败次数 |

`node_platforms` —— 节点对下游提供服务，每平台至多一行：

| 列 | 类型 | 说明 |
|----|------|------|
| `node_id` | TEXT PK | 外键 → `nodes(id)`，级联删除 |
| `realm` | TEXT | 对外 realm；为空表示用节点自己的域 |
| `min_expires` / `default_expires` / `max_expires` | INTEGER | 授予下游的 lifetime 窗口，默认 `60` / `3600` / `86400` |
| `allow_no_auth` | INTEGER | 0/1，默认 `0` |
| `catalog_poll_interval_ms` | INTEGER | 主动目录轮询周期；`0` 表示关闭 |

### 通过 API 管理节点

节点定义通过 HTTP API 读写，这是运行期的正规入口：

```sh
# 列出全部节点定义（来自 DB）
curl -s http://127.0.0.1:8080/v1/nodes | jq

# 查看单个节点定义
curl -s http://127.0.0.1:8080/v1/nodes/34020000011310000001 | jq

# 创建一个设备节点
curl -s -X POST http://127.0.0.1:8080/v1/nodes \
  -H 'Content-Type: application/json' \
  -d '{
        "id":   "34020000011310000002",
        "kind": "device",
        "domain": "3402000000",
        "addr": "127.0.0.1:15070",
        "registration": {
          "server":    "127.0.0.1:15061",
          "server_id": "34020000002000000001",
          "username":  "34020000011310000002",
          "password":  "change-me",
          "expires":   3600
        }
      }' | jq
```

完整接口清单（创建 / 更新 / 删除 / 级联校验）见 [`../README.md`](../README.md) 的 API 章节，或运行 `gb28181-simulator --help`。

### YAML `nodes:` / `registration:` / `platform:` 段的现状

**升级后这三个段仍能被解析，但不再作为真源使用**：

- **DB 为空时**——启动会把 YAML 中的 `nodes:` 一次性导入 DB，方便从旧版本平滑过渡；导入后重启，DB 非空，YAML 被忽略。
- **DB 非空时**——YAML 中的节点定义被完全忽略；此时改 YAML 不会有任何效果。

因此运行期调整节点请走 API 或 WebUI，不要改 YAML 后期待重启生效。密码字段在任何日志级别下都不会被输出（见 [`logging.md`](logging.md) 的 `log.redact_keys`）。

## 从 YAML 迁移到 DB

迁移脚本把一份 YAML 配置转成等价 SQL，供人工核对后入库：

```sh
go run scripts/migrations/yaml_to_db.go <release.yaml> > migration.sql
```

脚本是**只读**的：它不连接任何数据库、不修改文件、不打印密码到 stdout 以外的任何地方。请先备份数据库，人工核对生成的 SQL，再在一个事务里执行：

```sh
sqlite3 gb28181.db < migration.sql
```

首次迁移后建议从 YAML 中移除 `nodes:` 段，并删除其中已不再使用的密码。
