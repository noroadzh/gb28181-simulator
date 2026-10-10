package streaming

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/adapter/media"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// TestDetectCodecMPEG4FeatureBytes 验证 MPEG-4 Part 2 的 VOP(0xB6)/VOS(0xB0)/
// VOL(0x20) start code 特征字节被识别为 CodecMPEG4。修复前这些字节的低 5 位
// 落入 H.264 的 1..23 类型区间（如 0xB6&0x1F=22），全部被误判为 CodecAVC。
func TestDetectCodecMPEG4FeatureBytes(t *testing.T) {
	cases := []struct {
		name string
		b    byte
	}{
		{"vop_0xB6", 0xB6},
		{"vos_0xB0", 0xB0},
		{"vol_0x20", 0x20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// ParsePS 返回的 NALU 为 length-prefixed 格式：4 字节前缀 +
			// 载荷，nalu[4] 即 start code 后首字节。
			nalu := []byte{0x00, 0x00, 0x00, 0x02, tc.b, 0x00}
			if got := DetectCodec([][]byte{nalu}); got != CodecMPEG4 {
				t.Fatalf("DetectCodec(nalu[4]=0x%02X) = %s, want mpeg4", tc.b, got)
			}
		})
	}
}

// TestDetectCodecHEVCIDRDisambiguation 验证 0x20~0x2F 区间与 HEVC IDR/CRA 的
// 消歧：HEVC NAL 首字节在 nuh_layer_id=0 时为 nal_unit_type<<1，IDR_W_RADL(19)=
// 0x26、IDR_N_LP(20)=0x28、CRA(21)=0x2A 均落入 MPEG-4 VOL 区间。HEVC NAL 第二
// 字节低 3 位是 nuh_temporal_id_plus1（恒非 0），据此把真实 HEVC 切片判回
// CodecHEVC，避免"接入仅含 IDR 切片的流时被误判为 MPEG-4 而终止预览"的回归。
func TestDetectCodecHEVCIDRDisambiguation(t *testing.T) {
	cases := []struct {
		name string
		b4   byte // HEVC NAL 首字节（起始码后）
		b5   byte // HEVC NAL 第二字节（nuh_layer_id 低 5 位 <<3 | tid+1）
	}{
		{"idr_w_radl_0x26", 0x26, 0x01},
		{"idr_n_lp_0x28", 0x28, 0x01},
		{"cra_0x2A", 0x2A, 0x01},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nalu := []byte{0x00, 0x00, 0x00, 0x02, tc.b4, tc.b5}
			if got := DetectCodec([][]byte{nalu}); got != CodecHEVC {
				t.Fatalf("DetectCodec(hevc nalu[4]=0x%02X) = %s, want hevc", tc.b4, got)
			}
		})
	}

	// 对照：同区间字节但第二字节低 3 位为 0（不满足 HEVC nuh_temporal_id_plus1
	// 恒非 0 的特征）时，仍按 MPEG-4 VOL 判定。
	nalu := []byte{0x00, 0x00, 0x00, 0x02, 0x22, 0x08}
	if got := DetectCodec([][]byte{nalu}); got != CodecMPEG4 {
		t.Fatalf("DetectCodec(mpeg4 vol 0x22) = %s, want mpeg4", got)
	}
}

// TestDetectCodecAVCAndHEVCUnchanged 验证既有 AVC 与 HEVC 判定不受 MPEG-4
// 分支重排影响：H.264 SPS/IDR 仍返回 CodecAVC；HEVC 真实流首帧以 VPS/SPS/PPS
// （首字节 0x40/0x42/0x44，与 MPEG-4 特征集不相交）开头，批次仍返回 CodecHEVC。
func TestDetectCodecAVCAndHEVCUnchanged(t *testing.T) {
	// lp 构造 length-prefixed NALU（4 字节前缀 + 载荷），与 ParsePS 输出一致。
	lp := func(payload ...byte) []byte {
		nalu := make([]byte, 4+len(payload))
		nalu[3] = byte(len(payload))
		copy(nalu[4:], payload)
		return nalu
	}

	avcCases := []struct {
		name string
		b    byte
	}{
		{"sps_0x67", 0x67},
		{"idr_0x65", 0x65},
	}
	for _, tc := range avcCases {
		t.Run("avc_"+tc.name, func(t *testing.T) {
			nalu := lp(tc.b, 0x64, 0x00, 0x1F)
			if got := DetectCodec([][]byte{nalu}); got != CodecAVC {
				t.Fatalf("DetectCodec(nalu[4]=0x%02X) = %s, want avc", tc.b, got)
			}
		})
	}

	// HEVC: VPS/SPS/PPS 批次（VPS 在前，真实 IRAP access unit 的顺序）。
	hevc := [][]byte{
		lp(0x40, 0x01),             // VPS
		lp(0x42, 0x01, 0x01, 0x01), // SPS
		lp(0x44, 0x01),             // PPS
	}
	if got := DetectCodec(hevc); got != CodecHEVC {
		t.Fatalf("DetectCodec(hevc vps/sps/pps batch) = %s, want hevc", got)
	}
}

// fakePSMediaSupplier 是 MediaSourceSupplier 的测试桩：SubscribePS 返回一个
// 已预填充并关闭的 PS 帧通道。
type fakePSMediaSupplier struct {
	frames []model.PSFrame
}

func (f *fakePSMediaSupplier) SubscribePS(ctx context.Context, nodeID model.NodeID, channelID string) (<-chan model.PSFrame, error) {
	ch := make(chan model.PSFrame, len(f.frames)+1)
	for _, fr := range f.frames {
		ch <- fr
	}
	close(ch)
	return ch, nil
}

// fakeDeviceRegistry 返回一台带媒体源的设备节点，满足 Subscribe 的前置校验。
type fakeDeviceRegistry struct{}

func (fakeDeviceRegistry) Get(ctx context.Context, id model.NodeID) (model.Node, bool) {
	cfg := model.MediaConfig{Kind: model.SourceKindSynthetic, FPS: 25}
	profile, err := model.NewNodeProfileWithMedia(
		id.String(), "127.0.0.1:5060", "3402000000", "test", &cfg)
	if err != nil {
		return model.Node{}, false
	}
	return model.NewNode(profile), true
}

// TestGatewayMPEG4DegradedClosesChannel 验证 MPEG-4 Part 2 源触发网关降级：
// 用真实 PSPacketizer 打包一个以 VOP start code（00 00 01 B6）开头的 MPEG-4
// ES 帧喂给 pipeline，订阅后 out 通道最多收到 FLV header（也可能为空）即被
// 关闭，且期间不产出任何 AVC sequence header 或视频 tag。
func TestGatewayMPEG4DegradedClosesChannel(t *testing.T) {
	esPayload := append([]byte{0x00, 0x00, 0x01, 0xB6}, bytes.Repeat([]byte{0xAA}, 32)...)
	ps, err := media.NewPSPacketizer(nil).Packetize(model.NewESFrame(esPayload, 90000))
	if err != nil {
		t.Fatalf("Packetize: %v", err)
	}

	g := NewGateway(
		&fakePSMediaSupplier{frames: []model.PSFrame{ps}},
		fakeDeviceRegistry{},
		nil,
	)

	nodeID, err := model.ParseNodeID("34020000011310000001")
	if err != nil {
		t.Fatalf("ParseNodeID: %v", err)
	}
	out, err := g.Subscribe(context.Background(), nodeID, "34020000001310000001")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	chunks := 0
	for {
		select {
		case chunk, ok := <-out:
			if !ok {
				// 通道已关闭：降级终止 pipeline 后由 defer 收尾，符合预期。
				if chunks > 1 {
					t.Fatalf("got %d chunks before close, want <= 1", chunks)
				}
				return
			}
			chunks++
			if chunks > 1 {
				t.Fatalf("got %d chunks, want at most the FLV header before close", chunks)
			}
			// 唯一允许的 chunk 是 FLV header；若是视频 tag 或 AVC sequence
			// header（0x17 0x00 开头的 tag body）则说明 MPEG-4 被按 AVC 输出。
			if !bytes.Equal(chunk, FLVHeader) {
				end := 8
				if len(chunk) < end {
					end = len(chunk)
				}
				t.Fatalf("unexpected chunk (len=%d, head=% x): not the FLV header — possible AVC sequence header / video tag",
					len(chunk), chunk[:end])
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for out channel to close")
		}
	}
}
