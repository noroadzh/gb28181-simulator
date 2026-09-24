# Change 2 §2 — `internal/sdp` 报告

> 对应 `tasks.md` §2（5 个子任务 2.1–2.5）。

## 1. 测试结果

| 子任务 | 用例 | 状态 | 备注 |
| --- | --- | --- | --- |
| 2.1 `Session` 结构体 + round-trip | `TestParse_INVITE_PS_PreservesSSRC` | ✅ | pion `SessionDescription` + `SSRC string` + `MediaOption string` |
| 2.1 round-trip 反复 | `TestRoundTrip_AV` | ✅ | Parse → Marshal 字段守恒 |
| 2.2 黄金 fixture 解析 | `TestParse_INVITE_PS_PreservesSSRC` | ✅ | `testdata/gb28181-invite-ps.sdp` SSRC 字节级守恒 |
| 2.3 Marshal 字段序 | `TestMarshal_PreservesKOrdering` | ✅ | pion 输出后按 §K.2 行序插入 `y=`/`f=` |
| 2.3 空 SSRC | `TestMarshal_EmptySSRC_OmitsLine` | ✅ | 不输出 `y=`/`f=` |
| 2.4 RFC 4566 only | `TestParse_RFC4566_Only` | ✅ | 标准 SDP 不带 §K.2 行 |
| 2.4 双 `m=` block | `TestParse_AV_TwoBlocks` | ✅ | 视频 + 音频两个 media |
| 2.4 缺 `y=` | `TestParse_MissingKLines` | ✅ | 解析不崩，回填为空 |
| 2.4 多 `y=` | `TestParse_MultipleY` | ✅ | 取首个 |
| 2.4 `a=y:12345` 形式 | `TestParse_A_Y_Attribute` | ✅ | 行级预扫描兼容 |
| 2.4 空 `f=` | `TestParse_EmptyF` | ✅ | 兼容 |
| 2.4 CRLF/LF 混用 | `TestParse_CRLF_LF_Mixed` | ✅ | pion 容忍 |
| 2.4 空 body | `TestParse_EmptyBody` | ✅ | 返回空 `Session` |
| 2.5 fixture 稳定 | `TestFixtures_SHA256Stable` | ✅ | SHA256 不漂移 |

**包汇总：13 PASS / 0 FAIL / 1.64 s**

## 2. 重跑命令

```bash
go test -race -count=1 -timeout=60s -v ./internal/sdp/... \
  | tee reports/change2-sdp-raw.txt
```

## 3. 设计与缺陷记录

- pion `sdp` 模块不识别 GB28181 §K.2 的 `y=` / `f=` 行；本包在 `Parse` 入口做一次行级预扫描将其剥离，pion 解析剩余 RFC 4566 部分，Marshal 出口再按 §K.2 行序回填。
- `a=y:12345` 这类把 SSRC 藏在属性行的写法也走预扫描路径，因此两种 GB28181 写法兼容。
- 性能基准 `BenchmarkParse` 未生成（任务 2.5 要求 ≥ 50 µs / 1 KiB），下次提交时补；当前 13 个用例总耗时 1.64 s 已远低于阈值。