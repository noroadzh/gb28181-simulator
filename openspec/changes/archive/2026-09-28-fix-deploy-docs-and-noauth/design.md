# Design — fix-deploy-docs-and-noauth

## Context

部署文档（`deploy-linux.md`、`deploy-docker-compose.md`）存在三个可部署性问题：①下载地址指向不存在的 GitHub CI artifact；②GB/T 28181 真实场景中部分设备（特别是老旧 IPC）不支持 Digest 认证，需要无密码模式；③安装脚本从 `raw.githubusercontent.com` 远程下载配置文件。

见 proposal.md §Why。

## Goals / Non-Goals

**Goals**
- 代码层面支持 `allow_no_auth: true` 配置，使 simulator 可模拟不支持认证的设备
- 文档中所有下载链接替换为本地构建说明
- 完整的安装脚本（`scripts/install-linux.sh`）

**Non-Goals**
- 不改变 GB/T 28181 标准协议行为（simulator 仍能验证有密码设备）
- 不引入数据库 schema 变更
- 不改变 Web UI 代码

## Decisions

### D1: `allow_no_auth` 作为节点级布尔开关

在 `NodeRegistrationConfig` 中增加 `AllowNoAuth bool` 字段（mapstructure `allow_no_auth`），在 `NodePlatformConfig` 中同样增加 `AllowNoAuth bool`。默认 false（保持现有行为）。

**替代方案**：
- 全局环境变量 `GBSIM_ALLOW_NO_AUTH=1` — 范围过大，易误用于生产
- 特殊密码值如 `"none"` — 语义不明确

**决策**：节点级布尔更精确、最安全。

### D2: `NewRegistration` 空密码在 `allow_no_auth=true` 时放行

`internal/domain/model/registration.go` 第 83 行：
- 当 `p.Password == "" && p.AllowNoAuth == true` 时，跳过密码必填检查
- 当 `p.Password == "" && p.AllowNoAuth == false` 时，保持现有行为

### D3: Authorizer 对空 response 放行（仅 no-auth 节点）

`internal/adapter/auth/authorizer.go` 中 `Check(ctx, cred)` 方法：
- 若 `response` 为空（对应 `registration.password == ""` 的设备），检查源节点的 `allow_no_auth` 配置
- 若 `allow_no_auth == true`，直接返回 `Authorized`，不执行密码比较
- 日志记录 `debug` 级别：`"no-auth registration accepted for node %s"`

### D4: 配置文件 YAML 示例 — 无密码场景

在 `configs/config.example.yaml` 中增加注释，说明无密码模式的写法：
```yaml
# 无密码注册（测试/内网设备）：
registration:
  server: "127.0.0.1:15061"
  allow_no_auth: true
  password: ""   # allow_no_auth=true 时可省略

# 无密码平台（接受所有设备）：
platform:
  allow_no_auth: true
  accounts: []    # 无需定义任何账号
```

### D5: 部署文档 — 移除 GitHub 下载，改用源码构建

所有文档中删除"从 GitHub Release 下载"章节，替换为：

**deploy-linux.md §2**：
```bash
# 方式一：从源码构建
git clone https://your-actual-repo-url/gb28181-simulator.git
cd gb28181-simulator
make build  # 输出 bin/gb28181-simulator

# 方式二：本地下载二进制（自行从 CI artifact 或 Release 获取）
curl -LO https://your-internal-artifactory/gb28181-simulator
```

**deploy-docker-compose.md §2**：镜像改为本地构建 `docker build -t gb28181-simulator:local .`。

## Risks / Trade-offs

| Risk | Mitigation |
|------|-----------|
| 无密码设备连接真实平台可能不被接受（真实平台要求 Digest） | 文档明确说明 `allow_no_auth` 仅适用于 simulator-to-simulator 测试拓扑 |
| 用户误将 `allow_no_auth` 用于生产环境 | 文档"常见故障"中加入警告，明确此模式不安全 |
| 代码变更破坏现有单元测试 | `allow_no_auth` 默认 false，所有现有测试不受影响 |

## Implementation Order

1. 代码层（config → model → authorizer）
2. 单元测试（空密码配置加载、无密码授权）
3. 文档更新（deploy-linux.md、deploy-docker-compose.md）
4. 安装脚本（`scripts/install-linux.sh`）
5. `config.example.yaml` 增加注释
6. 最终回归验证（`make smoke`）

## Rollback

若变更引入回归，revert commit 即可。`allow_no_auth` 字段在无此字段的旧配置文件中默认为 false，不影响现有部署。
