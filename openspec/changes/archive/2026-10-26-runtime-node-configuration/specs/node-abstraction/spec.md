# Spec Delta

## MODIFIED Requirements

### Requirement: Node configuration is declarative and backward compatible

系统 MUST 在进程启动时按照以下顺序初始化节点：1）从 SQLite 数据库的 `nodes` 表读取所有节点定义；2）使用读取到的节点定义装配 `NodeService`（对每个节点调用 `NodeService.Create`，在 `auto_start: true` 时调用 `NodeService.Start`）。配置文件中的 `nodes` 段、节点级 `registration` 段、节点级 `platform` 段 MUST NOT 被装配代码消费。节点定义的真源为 DB，YAML 中残留的节点段在启动时记录 `warn` 级别日志后被忽略。

#### Scenario: 声明两个节点后被加载

- **WHEN** 配置含两个 `nodes:` 条目且均合法
- **THEN** 启动时按配置注册两个节点；`GET /v1/nodes` 返回这两个节点

#### Scenario: 缺省配置行为不变

- **WHEN** 配置不含 `nodes:` 段
- **THEN** 进程以零节点启动；HTTP 服务与日志行为与引入本 capability 之前完全一致

#### Scenario: 非法节点配置导致加载失败

- **WHEN** 某条目 `id` 长度不为 20 或 `kind` 不在枚举内
- **THEN** 配置加载返回错误并指出具体条目的序号与字段；进程不启动

#### Scenario: 条目携带注册参数后被加载并生效

- **WHEN** 某条目除 `id` / `kind` / `domain` / `addr` 外还声明了平台地址、鉴权用户名与密码、`expires`、`transport`
- **THEN** 配置加载成功，节点启动后按该注册参数完成注册（见 `device-node` capability）；缺省的 `expires` 取 3600 秒、`transport` 取 `udp`

#### Scenario: 非法注册参数导致加载失败

- **WHEN** 某条目的平台地址缺少端口、`expires` 非正数、`transport` 不在 `udp` / `tcp` 内，或给出平台地址却未提供密码
- **THEN** 配置加载返回错误并指出条目序号与字段名；进程不启动；错误体不含密码明文

#### Scenario: 条目携带 platform 段后被加载并生效

- **WHEN** 某 platform-large 条目声明了 `platform.realm`、若干 `accounts` 与有效期三档
- **THEN** 配置加载成功，节点启动后按该配置受理下级注册（见 `platform-large-node` capability）；缺省的 realm 取该节点的 domain，有效期窗口取 60 / 3600 / 86400 秒

#### Scenario: 非法 platform 参数导致加载失败

- **WHEN** 某条目的 `platform.realm` 为空、`accounts` 含重复 username 或空密码，或有效期三档不满足 `0 < min ≤ default ≤ max`
- **THEN** 配置加载返回错误并指出条目序号与字段名；进程不启动；错误体不含密码明文

#### Scenario: DB 节点装配

- **WHEN** DB `nodes` 表存在 2 条记录
- **THEN** 进程启动后 `NodeService` 中存在 2 个节点，其 ID、kind、addr 等与 DB 行一致

#### Scenario: 配置文件残留 nodes 段被忽略

- **WHEN** 配置文件包含 `nodes:` 段而 DB 为空
- **THEN** 进程启动后 `NodeService` 中无任何节点，启动日志包含 `warn` 级别 `"ignoring deprecated nodes section in config"`

#### Scenario: 运行时通过 API 添加节点

- **WHEN** 进程已启动，操作者调用 `POST /v1/nodes` 创建新节点
- **THEN** `nodes` 表新增一行，DB 优先于配置生效
