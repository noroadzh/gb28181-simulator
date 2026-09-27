# Tasks

> 本 change 是路线图 #3 `core-manscdp-and-ps`。任务粒度 ≤ 2 小时；每任务含验收标准；涉及 PS/RTP 字节格式的任务必须包含 golden test。

## 1. MANSCDP 编解码补齐

- [x] 1.1 `internal/domain/model/device_info.go`：新增 `DeviceInfo`、`DeviceInfoQuery`、`DeviceInfoItem` 值对象
- [x] 1.2 `internal/domain/model/record_info.go`：新增 `RecordInfo`、`RecordItem` 值对象
- [x] 1.3 `internal/domain/model/alarm.go`：新增 `AlarmNotify`、`AlarmItem` 值对象
- [x] 1.4 `internal/domain/model/ptz.go`：新增 `PTZControl`、`Telemetry` 值对象
- [x] 1.5 `internal/domain/model/preset.go`：新增 `PresetQuery`、`PresetSet` 值对象
- [x] 1.6 `internal/adapter/manscdp/device_info.go`：新增 DeviceInfo Query/Response 编解码，验收：`go test ./internal/adapter/manscdp/ -run DeviceInfo`
- [x] 1.7 `internal/adapter/manscdp/record_info.go`：新增 RecordInfo Query/Response 编解码，验收：`go test ./internal/adapter/manscdp/ -run RecordInfo`
- [x] 1.8 `internal/adapter/manscdp/alarm.go`：新增 Alarm Notify/Ack 编解码，验收：`go test ./internal/adapter/manscdp/ -run Alarm`
- [x] 1.9 `internal/adapter/manscdp/config.go`：新增 ConfigDownload Command/Ack 编解码，验收：`go test ./internal/adapter/manscdp/ -run Config`
- [x] 1.10 `internal/adapter/manscdp/ptz.go`：新增 PTZControl/Telemetry 编解码，验收：`go test ./internal/adapter/manscdp/ -run PTZ`
- [x] 1.11 `internal/adapter/manscdp/preset.go`：新增 Preset Query/Set 编解码，验收：`go test ./internal/adapter/manscdp/ -run Preset`

## 2. MANSCDP golden test

- [x] 2.1 `testdata/deviceinfo-query.xml`、`deviceinfo-response.xml` 样本文件，验收：`go test ./internal/adapter/manscdp/ -run DeviceInfo -v` 输出与 golden 一致
- [x] 2.2 `testdata/recordinfo-response.xml` 样本文件，验收：`go test ./internal/adapter/manscdp/ -run RecordInfo -v` 输出与 golden 一致
- [x] 2.3 `testdata/alarm-notify.xml`、`ptz-control.xml` 样本文件，验收：`go test ./internal/adapter/manscdp/ -run AlarmPTZ -v` 输出与 golden 一致

## 3. PS 封装/解封装完善

- [x] 3.1 扩展 `internal/adapter/media/ps_packetizer.go`：支持视频（stream ID `0xE0`）与音频（stream ID `0xC0`）两种 PES 封装，验收：`go test ./internal/adapter/media/ -run PS -v` 字节级断言通过
- [x] 3.2 扩展 `internal/adapter/media/ps_depacketizer.go`：支持 `0xC0` 音频流解包、短包拒绝行为，验收：`go test ./internal/adapter/media/ -run PS -v` 短包场景断言通过
- [x] 3.3 新增 golden test：`testdata/ps-roundtrip.bin`，验收：`PS 封包 → 解包 → ES 字节完全一致`

## 4. RTP 分包/收包完善

- [x] 4.1 扩展 `internal/adapter/media/rtpizer.go`：MTU 默认 1400 可配置，marker bit 正确设置，验收：`go test ./internal/adapter/media/ -run RTP -v` marker/sequence/timestamp 断言通过
- [x] 4.2 扩展 `internal/adapter/media/rtp_deizer.go`：去重、乱序重组、丢包报告，验收：`go test ./internal/adapter/media/ -run RTP -v` 乱序/去重/丢包断言通过
- [x] 4.3 新增 golden test：`testdata/rtp-roundtrip.bin`，验收：`PS → RTP 分包 → 收包重组 → PS 字节完全一致`

## 5. 验证与归档

- [x] 5.1 `go build ./...` 与 `go test ./... -count=1` 全绿
- [x] 5.2 `openspec validate core-manscdp-and-ps --strict` 绿色
- [x] 5.3 `openspec archive core-manscdp-and-ps --yes` 归档
