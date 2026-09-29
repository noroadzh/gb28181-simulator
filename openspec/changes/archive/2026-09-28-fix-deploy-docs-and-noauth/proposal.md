# Proposal

## Why

部署文档（`deploy-linux.md`、`deploy-docker-compose.md`）存在三个问题：①下载地址指向不存在的 GitHub CI 工件地址；②GB/T 28181 部分场景不需要账号密码（设备直连平台、测试环境），但当前代码强制要求密码非空；③安装脚本从不存在的 URL 下载配置。这三个问题导致部署文档无法直接用于生产。

## What Changes

1. **修正下载地址**：所有文档中的 `https://github.com/your-org/gb28181-simulator` 占位符替换为从本地源码构建的说明，因为项目目前没有 GitHub Release。
2. **安装脚本重构**：改为从本地 `configs/config.example.yaml` 复制（而非远程 URL），并加入 `--skip-password` 交互式密码输入或环境变量注入。
3. **支持无密码注册（新增代码能力）**：在 `registration.password` 和 `platform.accounts[].password` 字段引入空密码豁免规则，配合 `allow_no_auth: true` 配置开关；仅影响 simulator 侧注册行为，不改变 GB/T 28181 标准要求。
4. **更新部署文档**：所有配置样例适配无密码模式，错误排查章节加入"密码为空导致注册失败"条目。

## Capabilities

### New Capabilities

- `auth-optional`: 允许 device 节点和 platform 平台在配置中声明无密码认证模式。当节点配置 `allow_no_auth: true` 时，`registration.password` 和 `platform.accounts[].password` 可为空； simulator 侧在 Digest 挑战中跳过密码验证。此 capability 属于工程基础设施，适用于测试/内网/设备直连平台等无需认证的场景。

### Modified Capabilities

- `project-skeleton`: 更新部署文档路径说明，移除 GitHub 下载链接，改为源码构建方式；新增 `allow_no_auth` 无密码模式文档。

## Impact

- **代码变更**：`internal/platform/config/config.go`（密码可空校验逻辑）、`internal/domain/model/registration.go`（空密码处理）、`internal/adapter/auth/authorizer.go`（空密码放行）
- **文档变更**：`docs/deploy-linux.md`、`docs/deploy-docker-compose.md`、`scripts/install.sh`（新增）
- **测试**：新增 unit test 覆盖空密码配置加载与注册行为
- **非破坏性**：现有配置（含密码的节点）行为不变
