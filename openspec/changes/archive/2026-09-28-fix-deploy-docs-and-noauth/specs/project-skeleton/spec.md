# Spec Delta

## MODIFIED Requirements

### Requirement: 双形态部署文档已发布

> **修改要点**：① 移除"GitHub Release 下载"小节，替换为"从源码本地构建"；② 新增"无密码模式"配置样例与密码管理最佳实践；③ 故障排查章节加入"密码为空导致注册失败"条目；④ 安装脚本改为本地 `configs/config.example.yaml` 复制。

仓库 MUST 提供两种部署形态的 Markdown 文档：`docs/deploy-linux.md`（Linux 单机 systemd unit 部署）与 `docs/deploy-docker-compose.md`（Docker Compose 多节点部署）。两文档 MUST 包含：① 前置依赖（OS 包、Go 版本、Docker/Compose 版本）；② 安装步骤（含本地源码构建、用户与目录、systemd unit 或 Compose 文件），**不依赖外部 GitHub Release**；③ 配置文件位置与最小可用样例（含 `allow_no_auth: true` 的无密码模式示例）；④ 启动/停止/查看日志；⑤ 升级与回滚步骤；⑥ 常见故障排查（含"密码为空导致注册失败"）；⑦ 安装脚本（`scripts/install-linux.sh`）从本地 `configs/config.example.yaml` 复制而非远程下载。

#### 场景：运维在 Linux 上从源码部署单节点
- **WHEN** 运维按 `docs/deploy-linux.md` 在 CentOS 8 / Ubuntu 22.04 / Debian 12 任一 Linux 上执行源码构建与部署
- **THEN** 按文档步骤执行后 `systemctl status gb28181-simulator` 显示 `active (running)`
- **AND** `curl http://localhost:8080/healthz` 返回 `{"status":"ok"}`
- **AND** 文档**不包含**指向 `github.com/your-org/gb28181-simulator/releases` 的下载链接

#### 场景：运维通过 Compose 从源码部署多节点拓扑
- **WHEN** 运维按 `docs/deploy-docker-compose.md` 在装有 Docker Engine 24+ 与 Compose v2 的主机上执行 `docker compose build && docker compose up -d`
- **THEN** 所有服务进入 `healthy` 状态
- **AND** 镜像构建来自本地 Dockerfile（`docker build -t gb28181-simulator:local .`），不依赖 GHCR pull

#### 场景：运维部署无密码 device 节点
- **WHEN** 运维在测试/内网环境部署 device 节点
- **THEN** `docs/deploy-linux.md` 提供 `allow_no_auth: true` 配置样例
- **AND** `docs/deploy-docker-compose.md` 提供 `allow_no_auth: true` 的 device Compose service

#### 场景：运维按文档回滚升级
- **WHEN** 运维执行文档中描述的升级步骤（systemd 二进制替换或 Compose `up --build`）
- **THEN** 出现不兼容升级时能按"回滚步骤"小节回退到上一版本