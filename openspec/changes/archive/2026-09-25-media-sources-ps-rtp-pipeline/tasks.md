# Tasks

## 1. domain 模型与端口族

- [x] 1.1 `internal/domain/model/media.go`：定义 `PSFrame` / `RTPPacket` /
      `ESFrame` / `MediaConfig`；无外部依赖，纯值类型
- [x] 1.2 `internal/domain/port/media.go`：定义 `MediaSource` / `PSPacketizer` /
      `PSDepacketizer` / `RTPizer` / `RTPDeizer` 五个接口；`MediaSource` 只产 ES
- [x] 1.3 单测：`domain/model/media.go` 的 `Validate` 覆盖非法 PTS、空 payload；
      `domain/port/` 的 interface 不做 mock，仅做编译检查

## 2. PS 封装/解封装 + golden test

- [x] 2.1 `internal/adapter/media/ps_packetizer.go`：ES frame → PS 包（系统起始码
      `00 00 01 BA` + pack header + PES 头 + payload），支持固定 PTS 注入
- [x] 2.2 `internal/adapter/media/ps_depacketizer.go`：PS 流 → ES frame，
      起始码 + pack header 长度截帧，还原 PTS
- [x] 2.3 golden test：`internal/adapter/media/ps_test.go` 含预录样本，
      封装→解封装字节级一致；`go test ./internal/adapter/media/ -run PS` 通过

## 3. RTP 分包/收包重组 + golden test

- [x] 3.1 `internal/adapter/media/rtpizer.go`：PS 帧 → RTP 包列表，
      按 MTU（默认 1400 bytes）切分，marker bit = 1 标记帧边界，sequence 递增
- [x] 3.2 `internal/adapter/media/rtp_deizer.go`：RTP 包 → PS 流，
      按 (SSRC, payload type) 分组，按 sequence 重组，处理乱序与去重
- [x] 3.3 golden test：`internal/adapter/media/rtp_test.go` 含预录样本，
      分包→收包重组字节级一致；`go test ./internal/adapter/media/ -run RTP` 通过

## 4. 四种媒体源 adapter

- [x] 4.1 `internal/adapter/media/file_source.go`：本地文件 ES 流，
      支持 `.264` / `.h264` / `.h265`；`Open` 返回 `io.ReadCloser`
- [x] 4.2 `internal/adapter/media/synthetic_source.go`：合成图源，
      产出可复现的递进 H.264 ES 样本（默认 25fps），不依赖外部文件，
      是默认测试源
- [x] 4.3 `internal/adapter/media/rtsp_source.go`：RTSP 源，
      用 `bluenviron/gortsplib` 拉取最小流；失败时明确报错
- [x] 4.4 `internal/adapter/media/hls_source.go`：HLS 源，
      用 `bluenviron/m3u8` 解析 m3u8 + 拉取 segment；失败时明确报错
- [x] 4.5 单测：四种源各自有独立测试，`go test ./internal/adapter/media/` 通过

## 5. app 层编排（MediaService）

- [x] 5.1 `internal/app/media_service.go`：新增 `MediaService`，持有
      `domain/port` 依赖，暴露 `Open/Close/Read` 语义；与 `NodeService`
      生命周期解耦，按需挂载
- [x] 5.2 `cmd/gb28181-simulator/main.go`：节点启动时根据媒体配置装配
      `MediaService` 与对应源；媒体源失败只打日志不中断节点
- [x] 5.3 单测：`internal/app/media_service_test.go` 覆盖 Open/Close 生命周期、
      源失败处理、Read 回调；`go test ./internal/app/ -run Media` 通过

## 6. e2e 闭环

- [x] 6.1 出站闭环：合成图源 → ES → PS 封装 → RTP 分包 → 字节级断言
      与 golden fixture 一致
- [x] 6.2 入站闭环：RTP 收包重组 → PS 解封装 → ES 帧落盘 → 字节级断言
      与源文件一致
- [x] 6.3 e2e 用例：`internal/adapter/siptest/media_e2e_test.go`（或合适包），
      两个闭环各一个用例，不依赖外部网络

## 7. 收尾

- [x] 7.1 `go test ./... -race -count=1` 全绿
- [x] 7.2 `README.md`：补媒体源与转封装管道章节（四种源 + 出站/入站流程）
- [x] 7.3 `docs/architecture.md`：补 media 端口族 + adapter 分层说明
- [x] 7.4 `openspec validate "media-sources-ps-rtp-pipeline" --strict` 通过
- [x] 7.5 同步主 spec：新建 `openspec/specs/media-sources/spec.md`（合并 ADDED 需求）
- [x] 7.6 归档到 `openspec/changes/archive/2026-09-25-media-sources-ps-rtp-pipeline/`
- [x] 7.7 提交 commit
