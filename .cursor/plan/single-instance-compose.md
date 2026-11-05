# 单实例多角色 Docker 部署

## 目标
默认只部署一套 Web、一个进程内同时跑 platform + device，替代双容器 18080/18081。

## 变更
- `docker-compose.yml` → 单服务 `gbsim`，挂载 `configs/examples/compose-single.yaml`
- `docker-compose.dual.yml` → 可选双容器
- 文档 / HelpView / openspec 同步「合壳一套 Web」

## 端口
| 宿主 | 用途 |
|------|------|
| 18080 | Web UI |
| 15060 | 平台 SIP |
| 15061 | 设备 SIP |

## 迁移
旧环境 `docker compose down` 后换新 compose；数据卷名变为 `gbsim-data`。需要干净节点时 `down -v`。
