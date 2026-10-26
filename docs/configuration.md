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

## 节点配置回滚

迁移是一次性的单向切换，但切换之后仍然要能退回去。回滚分三种情形，按"损失多少"排序：

### 一、只想回到用 YAML 定义节点（推荐）

这是最常见的情况——DB 里的节点配置有问题，但不需要退回旧版本二进制。**清空三张表即可**，进程下次启动会因为 DB 为空而重新导入 YAML 中的 `nodes:`：

```sh
sqlite3 gb28181.db <<'SQL'
BEGIN;
DELETE FROM node_platforms;
DELETE FROM node_registrations;
DELETE FROM nodes;
COMMIT;
SQL
```

先备份再执行（`cp gb28181.db gb28181.db.bak`）。注意这会丢掉运行期通过 API / WebUI 做过的所有节点改动——如果只想回退某一个节点，用下面的二。

### 二、只想回退某一个节点

不要清库。取该节点当前的 DB 行，把 YAML 里改回旧值，再走 API 写回：

```sh
# 读当前定义（含注册与平台配置，分三块返回）
curl -s http://127.0.0.1:8080/v1/nodes/34020000011310000001 | jq

# 改完用 PUT 覆盖；cascade 段用单独的端点
curl -s -X PUT http://127.0.0.1:8080/v1/nodes/34020000011310000001 \
  -H 'Content-Type: application/json' -d @corrected-node.json | jq
```

`PUT /v1/nodes/{id}` 是一次覆盖写：请求体中省略的字段会被重置为默认值，不是保留旧值。因此务必先 `GET` 再整体改好再整体 `PUT`，不要只发改动的那一个字段。

### 三、退回旧版本二进制

旧版本的节点真源是 YAML，没有这三张表。步骤：

1. 从 DB **导出**节点定义——用 `yaml_to_db` 的逆过程，或直接 `sqlite3 gb28181.db "SELECT ... FROM nodes"` 后手工还原成 YAML；`cascade_children` 是 JSON 数组，还原成 YAML 的列表。
2. 把导出的 YAML 放回配置文件（§节点定义持久化 那张表里的 `nodes` / `node_registrations` / `node_platforms` 三块对应 `nodes:` / `registration:` / `platform:`）。
3. 停掉新版本，用旧版本二进制 + 该 YAML 启动。
4. 保留 DB 文件但**不要让旧版本碰它**——旧版本不认这三张表，但也不会删它们；确认新版本恢复正常后再决定是否清理。

回滚过程中**不需要**撤销数据库迁移本身：这三张表是纯新增，旧二进制忽略它们，不会因此启动失败。这正是把节点配置放进独立三张表（而不是加列到已有表）的原因。
