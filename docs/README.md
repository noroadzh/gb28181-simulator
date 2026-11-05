# 文档目录

按用途分类，目录名前缀为阅读顺序序号。

| 序号 | 分类 | 目录 | 说明 |
|------|------|------|------|
| 01 | 操作指南 | [01-guides/](01-guides/) | 联调入口、Web 页面操作 |
| 02 | 部署运维 | [02-deploy/](02-deploy/) | Docker / Linux systemd 装机 |
| 03 | 配置参考 | [03-config/](03-config/) | 节点/YAML 配置、日志 |
| 04 | 架构与协议 | [04-architecture/](04-architecture/) | 分层架构、国标覆盖矩阵 |
| 05 | 测试验证 | [05-testing/](05-testing/) | 冒烟测试说明 |
| 06 | 规划路线 | [06-planning/](06-planning/) | 路线图与阶段状态 |
| 90 | 标准原文 | [90-standards/](90-standards/) | GB/T 28181 PDF |
| — | 截图资源 | [screenshots/](screenshots/) | Web UI 截图（脚本产物） |
| — | 冒烟报告 | [smoke-results.json](smoke-results.json) | `make smoke` 当前结果（CI 固定路径） |

## 01 操作指南

| 文档 | 说明 |
|------|------|
| [操作手册.md](01-guides/操作手册.md) | 联调入口、角色视角、日常操作、测试机启停 |
| [web-ui-guide.md](01-guides/web-ui-guide.md) | 各页面字段级说明与截图索引 |

## 02 部署运维

| 文档 | 说明 |
|------|------|
| [deploy-docker-compose.md](02-deploy/deploy-docker-compose.md) | 单实例多角色 Compose；可选双容器附录 |
| [deploy-linux.md](02-deploy/deploy-linux.md) | systemd 单机安装、升级与回滚 |

## 03 配置参考

| 文档 | 说明 |
|------|------|
| [configuration.md](03-config/configuration.md) | 节点 DB 真源、YAML 进程配置 |
| [logging.md](03-config/logging.md) | `log.*`、模块级级别、热更新与排障 |

## 04 架构与协议

| 文档 | 说明 |
|------|------|
| [architecture.md](04-architecture/architecture.md) | 六边形分层、端口契约、级联中继 |
| [gb28181-coverage.md](04-architecture/gb28181-coverage.md) | GB/T 28181 实现覆盖对照 |

## 05 测试验证

| 文档 | 说明 |
|------|------|
| [smoke-test.md](05-testing/smoke-test.md) | 冒烟矩阵、执行、结果解读、失败排查 |

## 06 规划路线

| 文档 | 说明 |
|------|------|
| [roadmap-15-steps.md](06-planning/roadmap-15-steps.md) | 路线图主表与归档同步契约 |

## 90 标准原文

| 文档 | 说明 |
|------|------|
| [GBT 28181-2016 …（扫描带目录版）.pdf](90-standards/GBT%2028181-2016%20公共安全视频监控联网系统信息传输、交换、控制技术要求（扫描带目录版）.pdf) | 国标扫描件 |
