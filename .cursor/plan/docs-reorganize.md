# docs 分类编号整理

## 目标结构

```
docs/
  README.md                 # 总目录
  01-guides/                # 操作指南
  02-deploy/                # 部署运维
  03-config/                # 配置参考
  04-architecture/          # 架构与协议
  05-testing/               # 测试验证
  06-planning/              # 规划路线
  90-standards/             # 标准原文（PDF）
  screenshots/              # UI 截图（路径保持，CI/脚本依赖）
  smoke-results.json        # 冒烟产物（CI 路径保持）
```

## 文件映射

| 原路径 | 新路径 |
|--------|--------|
| 操作手册.md | 01-guides/操作手册.md |
| web-ui-guide.md | 01-guides/web-ui-guide.md |
| deploy-docker-compose.md | 02-deploy/deploy-docker-compose.md |
| deploy-linux.md | 02-deploy/deploy-linux.md |
| configuration.md | 03-config/configuration.md |
| logging.md | 03-config/logging.md |
| architecture.md | 04-architecture/architecture.md |
| gb28181-coverage.md | 04-architecture/gb28181-coverage.md |
| smoke-test.md | 05-testing/smoke-test.md |
| roadmap-15-steps.md | 06-planning/roadmap-15-steps.md |
| GBT 28181-2016….pdf | 90-standards/GBT 28181-2016….pdf |

## 同步更新

- docs 内相对链接、docs/README.md
- README.md
- openspec/specs/*（活跃规格）
- scripts/install-linux.sh、docker-compose.dual.yml
- internal 注释中的路径（可选）
- 不改 openspec/changes/archive 历史归档
