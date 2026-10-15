# GB/T 28181协议模拟器规范

## 技术栈
- 语言: Go，纯 Go / `CGO_ENABLED=0` 
- SIP:  `github.com/ghettovoice/gosip` 
- HTTP:  `github.com/labstack/echo/v4` 
- WebSocket:  `github.com/gorilla/websocket` 
- 配置:  `github.com/spf13/viper`（YAML + 环境变量覆盖） 
- 存储:  `modernc.org/sqlite`（纯 Go，无 CGO） 
- 日志:  `log/slog`（trace / debug / info / warn / error） 
- 追踪:  OpenTelemetry — 默认 stdout 导出器，可选 OTLP gRPC（`tracing.otlp_endpoint`） 
- 依赖装配:  手写 `internal/platform/servicectx`（不使用 wire / fx） 
- 前端:  Vue 3 + Element Plus + Vite，经 `embed.FS` 内嵌 

## 分层架构(严格单向依赖)
- cmd/gb28181-simulator   &rarr;   入口
- cmd/sipprobe        &rarr;       SIP 诊断 CLI（Change 2 §7）
- internal/adapter     &rarr;      SIP / SDP / Digest / Transport / Audit / 节点注册表适配器
- internal/app       &rarr;        用例编排（NodeService，Change 4）
- internal/domain      &rarr;      领域模型 + port 接口
- internal/interface/http  &rarr;  HTTP/WS 服务器（Echo + gorilla/websocket）
- internal/interface/webui     &rarr;    内嵌仪表盘（Vue 3 + Element Plus，embed.FS）
- internal/platform     &rarr;     配置、日志、追踪、时钟、servicectx
- internal/storage     &rarr;      SQLite 引导（暂无业务表）
- internal/sipprobe    &rarr;      sipprobe 核心（可测试）
- configs/         &rarr;          示例配置
- scripts/          &rarr;         冒烟脚本（Change 2 §8.2）
- web/             &rarr;          Vue 3 + Vite 源码
- openspec/      &rarr;            OpenSpec 变更产物

## 常用命令
```sh
make fmt            # gofmt -s -w + goimports
make lint           # go vet ./...
make test           # go test -race -cover ./...
make build          # 构建 bin/gb28181-simulator（先构建 web 产物）
make sipprobe-build # 构建 bin/sipprobe
make service-build  # 为宿主平台构建所有服务二进制
make sip-test       # 快速反馈：仅 SIP / SDP / Digest（约 3-4 秒）
make release-matrix # 5 平台二进制 + sha256，CGO_ENABLED=0
make smoke          # 冒烟基线：单元测试 + 5 平台编译 + 前端构建 + e2e 探针
./scripts/smoke-sip.sh   # 双进程 probe 交互，显式端口控制
```

`bin/gb28181-simulator sipprobe ...` 同样暴露诊断探针，因此单个二进制既能当模拟器也能当探针。

## 测试/部署服务器
- 服务器ip: 10.96.1.125
- 用户名: root
- 项目所在服务器路径: /slow2/gb28181-simulator
- 部署方式: 基于docker容器部署