# Tasks

## 1. 确认 heartbeat 是否存在同样问题

- [x] 1.1 `grep -n "peer != \|!= reg.Server\|!= node.Registration().Server()" internal/app/device_heartbeat.go`；若有同样的字符串比较，同步改用 `samePeer`；若无，记录结论；验证：无则跳过，文档说明"heartbeat 不受影响"
  - 结论：`device_heartbeat.go` 中没有直接的 peer 比较，但 `keeper.go` 的心跳（364 行）与 OPTIONS（427 行）存在同样的 `peer != s.reg.Server()` 字符串比较，已一并抽取为 `internal/app/peer.go` 的 `samePeer` 并统一替换。

## 2. 实现 samePeer 函数

- [x] 2.1 在 `internal/app/device_registrar.go` 末尾（或靠前位置）添加 `samePeer(peer, server string) bool` 纯函数，按 design §Decision 1 实现（端口比较 → IP 语义比较 → 主机名 DNS 解析交集），包含完整的端口拆分错误回退；通过 `go build ./internal/app/...` 编译验证
  - 实际落点：抽到独立文件 `internal/app/peer.go`（含 `containsIP`/`intersectIPs` 辅助函数），供 registrar 与 keeper 共用。
- [x] 2.2 在 `internal/app/device_registrar.go` 中将第 186 行的 `if peer != reg.Server() { continue }` 替换为 `if !samePeer(peer, reg.Server()) { continue }`；验证 `go build ./...` 编译通过
  - 同步替换 `keeper.go` 364 行与 427 行两处。

## 3. 为 samePeer 编写单元测试

- [x] 3.1 新建 `internal/app/device_registrar_peer_test.go`；表驱动覆盖：`(IP, IP)` 相等 / 不等 / IPv4 vs IPv6 等价；`(IP, 主机名)` 其中一方为 `localhost`/`127.0.0.1`；`(主机名, 主机名)` 同名 / 不同名同 IP / 不同名不同 IP；端口不等直接 false；host 解析失败回退 false；通过 `go test ./internal/app -run TestSamePeer -v` 验证全部用例 PASS

## 4. Registrar 集成测试（修复路径覆盖）

- [x] 4.1 在 `internal/app/` 现有测试文件（如 `device_registrar_test.go`，若存在）中增加或扩展一个测试：用 fake transport 使 `Receive()` 返回 peer = `"127.0.0.1:5060"`，而 `reg.Server()` 配置为 `"localhost:5060"`；验证 `Register()` 仍能正确匹配 401 挑战并发出带 Authorization 的重发，最终得到 200 OK；通过 `go test ./internal/app -run TestRegistrar -v` 验证

## 5. 全量测试与编译

- [x] 5.1 `go vet ./...` → 无 warning；`go build ./...` → 编译成功
- [x] 5.2 `go test ./...` → 全部通过（含新增测试）
- [x] 5.3 交叉编译 linux/amd64：`CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o bin/gb28181-simulator-linux-amd64 ./cmd/gb28181-simulator`；通过 `file bin/gb28181-simulator-linux-amd64` 显示 `ELF 64-bit` 验证

## 6. 部署与冒烟验证

- [x] 6.1 将二进制 scp 到 `root@10.96.1.125:/slow2/gb28181-simulator/binary/`，执行 `docker compose build && docker compose up -d`（gbsim-device + gbsim-platform 重启）；通过 `docker ps --filter name=gbsim` 显示两个容器 `Up (healthy)` 验证
- [x] 6.2 `curl -s -X POST http://127.0.0.1:18081/v1/nodes/34020000001310000001/start` → HTTP 200；`curl -s http://127.0.0.1:18081/v1/nodes/34020000001310000001` → `status=online`（不再 fault）；通过两个 HTTP 响应验证
- [x] 6.3 `curl -s http://127.0.0.1:18080/v1/nodes/34020000002000000001/capture` → 应出现带 `Authorization` 头的 REGISTER 与 200 OK；通过 platform capture 验证完整事务
  - 实测（device 侧 capture）：CSeq:1 REGISTER → 401 + Digest 挑战 → CSeq:2 REGISTER（带 Authorization，response 计算正确）→ 200 OK（Expires: 3600），完整事务成功。
- [x] 6.4 `docker logs gbsim-device --tail 30` → 应有 `registration succeeded` 日志，无 timeout 错误；通过日志验证

## 7. Git 提交与 openspec 归档

- [ ] 7.1 `git add internal/app/device_registrar.go internal/app/device_registrar_peer_test.go openspec/changes/fix-device-registration-peer-matching/` 后提交；`git log -1 --format='%H %s'` 确认新提交
- [ ] 7.2 `openspec archive fix-device-registration-peer-matching --yes`；验证 `ls openspec/changes/archive/2026-10-10-fix-device-registration-peer-matching/` 含 `proposal.md design.md tasks.md specs/`
- [ ] 7.3 `grep -n "对端\|peer\|samePeer\|主机名" openspec/specs/device-node/spec.md` 验证 MODIFIED requirement 已出现在 main spec（由 openspec sync 自动或手动合并）
