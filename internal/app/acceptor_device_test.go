package app

import (
	"context"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/adapter/manscdp"
	"github.com/your-org/gb28181-simulator/internal/adapter/nodereg"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// deviceHarness 启动一个 device 节点的 Acceptor read loop。
// 与 messageHarness 的区别：ServeDevice 路径，不启动 catalogPoller，
// registry 用于 answerCatalog 查询自身的 channels 和 MediaConfig。
type deviceHarness struct {
	acceptor *Acceptor
	tr       *acceptorTransport
	codec    *manscdp.MANSCDPCodecAdapter
	reg      *nodereg.Registry
	deviceID model.NodeID
}

// newDeviceHarness 构造一个带有真实 codec 和真实 nodereg.Registry 的 device 测试工具。
// profile 已在 registry 中注册，节点级 mediaConfig 存在时通过 SetMediaConfig 设置。
func newDeviceHarness(t *testing.T, deviceID model.NodeID, chs []model.Channel, mediaCfg *model.MediaConfig) *deviceHarness {
	t.Helper()
	tr := newAcceptorTransport()
	codec := manscdp.NewMANSCDPCodec()
	devices := newFakeDevices()
	creds := newFakeCredentials()
	clock := newSyncClock(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC))
	tickers := newTickerScript()
	log := discardLogger()

	acceptor, err := NewAcceptor(context.Background(), clock, &fakeChallenger{},
		&fakeAuthenticator{}, creds, devices, codec, tickers.factory(), log)
	if err != nil {
		t.Fatalf("NewAcceptor: %v", err)
	}

	// 在 nodereg 中注册 device profile，这样 answerCatalog 能查询到 channels 和 MediaConfig。
	reg := nodereg.New()
	profile, err := model.NewNodeProfile(deviceID.String(), "127.0.0.1:15060", "3402000000", "sim")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	if len(chs) > 0 {
		profile, err = profile.WithChannels(chs)
		if err != nil {
			t.Fatalf("WithChannels: %v", err)
		}
	}
	if mediaCfg != nil {
		profile.SetMediaConfig(*mediaCfg)
	}
	if _, err := reg.Register(context.Background(), profile); err != nil {
		t.Fatalf("Register device profile: %v", err)
	}
	acceptor.WithNodeRegistry(reg)

	if err := acceptor.ServeDevice(deviceID, tr, "3402000000"); err != nil {
		t.Fatalf("ServeDevice: %v", err)
	}
	t.Cleanup(func() { _ = acceptor.Close() })

	return &deviceHarness{
		acceptor: acceptor,
		tr:       tr,
		codec:    codec,
		reg:      reg,
		deviceID: deviceID,
	}
}

// deviceMessageRequest 构造一条发往 device 节点的 MESSAGE 请求。
func deviceMessageRequest(t *testing.T, toDeviceID, body string) model.Message {
	t.Helper()
	msg, err := model.NewRequest("MESSAGE", "sip:"+toDeviceID+"@3402000000", []model.Header{
		model.NewHeader("From", "<sip:platform@3402000000>;tag=p1"),
		model.NewHeader("To", "<sip:"+toDeviceID+"@3402000000>"),
		model.NewHeader("Call-ID", "dev-call-1"),
		model.NewHeader("CSeq", "1 MESSAGE"),
		model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-dev"),
		model.NewHeader("Content-Type", "Application/MANSCDP+XML"),
	}, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return msg
}

// catalogQueryBody 构造一个 GB/T 28181-2016 §A.2.4.1 标准的 catalog query body。
//
// GB/T 28181 在两个方向使用不同的根元素：
//   - 设备→平台：<Notify>（设备通知平台状态变更）
//   - 平台→设备：<Query>（平台主动询问设备目录）
//
// CatalogPoller 主动拉取时发的是 <Query>。DecodeNotify 现在两种根元素都接受
//（见 internal/adapter/manscdp/notify.go），所以 device 节点作为 UAS 收
// CatalogPoller 发的 <Query> 能正确解析。本测试默认走真实链路 <Query> 封装。
func catalogQueryBody(deviceID string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<Query>` + "\n" +
		`  <CmdType>Catalog</CmdType>` + "\n" +
		`  <SN>1</SN>` + "\n" +
		`  <DeviceID>` + deviceID + `</DeviceID>` + "\n" +
		`</Query>`
}

// catalogNotifyBody 构造一个设备→平台方向的 <Notify> 封装的 catalog body。
// 用于验证 device 同时兼容上行方向（真实设备行为）。
func catalogNotifyBody(deviceID string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<Notify>` + "\n" +
		`  <CmdType>Catalog</CmdType>` + "\n" +
		`  <SN>1</SN>` + "\n" +
		`  <DeviceID>` + deviceID + `</DeviceID>` + "\n" +
		`</Notify>`
}

// mustDeviceNodeID 返回一个合法的 device 类型 NodeID（type code 131）。
func mustDeviceNodeID(t *testing.T, id string) model.NodeID {
	t.Helper()
	nid, err := model.ParseNodeID(id)
	if err != nil {
		t.Fatalf("ParseNodeID(%q): %v", id, err)
	}
	if nid.Kind() != model.NodeKindDevice {
		t.Fatalf("id %q kind = %v, want NodeKindDevice", id, nid.Kind())
	}
	return nid
}

// ----------------------------------------------------------------------------
// 测试 3.1：device 节点正确应答 catalog query 并携带 MediaSource 扩展
// ----------------------------------------------------------------------------

// TestAcceptor_DeviceAnswersCatalogQuery 验证 device 节点在收到 catalog query MESSAGE 后：
//   - 返回 200 OK
//   - Content-Type 为 Application/MANSCDP+XML
//   - body 反解后包含 1 个 Item（device 自身的 channel）
//   - 该 Item 携带 MediaSource 扩展，Kind=file，Path=/u.mp4
//
// 验收（tasks.md 3.1）：
//   - device 节点 ServeDevice 后，对 catalog query 返回 200
//   - Item 数 == 1（device 自己的 channel）
//   - Item 包含 MediaSource 扩展块
func TestAcceptor_DeviceAnswersCatalogQuery(t *testing.T) {
	deviceID := mustDeviceNodeID(t, "34020000001310000001")

	// 构造 device profile：1 个 channel，节点级 mediaConfig。
	ch := mustChannel(t, "34020000001310000011", "cam-1", "34020000001310000001", model.ChannelStatusOnline)
	mediaCfg := model.MediaConfig{
		Kind: model.SourceKindFile,
		Path: "/u.mp4",
		Loop: true,
	}

	h := newDeviceHarness(t, deviceID, []model.Channel{ch}, &mediaCfg)

	// 投递 catalog query MESSAGE。
	body := catalogQueryBody(deviceID.String())
	req := deviceMessageRequest(t, deviceID.String(), body)
	resp := h.tr.deliver(t, req)

	// 断言 200 OK。
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}

	// 断言 Content-Type。
	if got := responseHeader(t, resp, "Content-Type"); got != "Application/MANSCDP+XML" {
		t.Errorf("Content-Type = %q, want Application/MANSCDP+XML", got)
	}

	// 反解 body，断言有 1 个 Item。
	cat, media, err := h.codec.DecodeCatalogResponse(resp.Body())
	if err != nil {
		t.Fatalf("DecodeCatalogResponse: %v", err)
	}
	if cat.SumNum() != 1 {
		t.Fatalf("catalog SumNum = %d, want 1", cat.SumNum())
	}

	// 断言 Item 是 device 的 channel。
	if len(cat.Items()) != 1 {
		t.Fatalf("catalog Items = %d, want 1", len(cat.Items()))
	}
	if cat.Items()[0].DeviceID() != "34020000001310000011" {
		t.Errorf("catalog item DeviceID = %q, want 34020000001310000011",
			cat.Items()[0].DeviceID())
	}

	// 断言 MediaSource 扩展存在且匹配配置。
	if len(media) != 1 {
		t.Fatalf("media sources = %d, want 1", len(media))
	}
	if media[0].DeviceID != "34020000001310000011" {
		t.Errorf("media DeviceID = %q, want 34020000001310000011", media[0].DeviceID)
	}
	if media[0].Config.Kind != model.SourceKindFile {
		t.Errorf("media Kind = %q, want file", media[0].Config.Kind)
	}
	if media[0].Config.Path != "/u.mp4" {
		t.Errorf("media Path = %q, want /u.mp4", media[0].Config.Path)
	}
}

// ----------------------------------------------------------------------------
// 测试 3.2：device 节点对非 MESSAGE 方法返回 405，ACK 静默丢弃
// ----------------------------------------------------------------------------

// TestAcceptor_DeviceReturns405ForNonMessage 验证 device 节点对以下方法返回
// 405 Method Not Allowed，Allow 头为 "MESSAGE"；ACK 静默丢弃无响应。
//
// 验收（tasks.md 3.2）：
//   - device 节点对 OPTIONS 响应 405，Allow 头含 MESSAGE
//   - INVITE / BYE / SUBSCRIBE / INFO 同样返回 405
//   - ACK 无响应（RFC 3261 §13.2.2.4：UAS 不答 ACK）
func TestAcceptor_DeviceReturns405ForNonMessage(t *testing.T) {
	deviceID := mustDeviceNodeID(t, "34020000001310000002")
	h := newDeviceHarness(t, deviceID, nil, nil)

	nonMessageMethods := []struct {
		method string
		body   string
	}{
		{"OPTIONS", ""},
		{"INVITE", "v=0\r\n"},
		{"BYE", ""},
		{"SUBSCRIBE", ""},
		{"INFO", ""},
	}

	for _, tc := range nonMessageMethods {
		tc := tc
		t.Run(tc.method, func(t *testing.T) {
			hdrs := []model.Header{
				model.NewHeader("From", "<sip:platform@3402000000>;tag=p2"),
				model.NewHeader("To", "<sip:"+deviceID.String()+"@3402000000>"),
				model.NewHeader("Call-ID", "405-call-"+tc.method),
				model.NewHeader("CSeq", "1 "+tc.method),
				model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-405"),
			}
			if tc.method == "SUBSCRIBE" {
				hdrs = append(hdrs, model.NewHeader("Event", "catalog"))
			}
			if tc.method == "INVITE" {
				hdrs = append(hdrs, model.NewHeader("Contact", "<sip:platform@127.0.0.1:15060>"))
				hdrs = append(hdrs, model.NewHeader("Content-Type", "application/sdp"))
			}

			req, err := model.NewRequest(tc.method, "sip:"+deviceID.String()+"@3402000000", hdrs, tc.body)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			resp := h.tr.deliver(t, req)

			if resp.StatusCode() != 405 {
				t.Errorf("status = %d, want 405 for %s", resp.StatusCode(), tc.method)
			}
			if got := responseHeader(t, resp, "Allow"); got != "MESSAGE" {
				t.Errorf("Allow = %q, want MESSAGE", got)
			}
		})
	}

	// 单独测试 ACK：投递后等待足够长时间，断言没有任何响应写入。
	t.Run("ACK silent drop", func(t *testing.T) {
		ack, err := model.NewRequest("ACK", "sip:"+deviceID.String()+"@3402000000", []model.Header{
			model.NewHeader("From", "<sip:platform@3402000000>;tag=p2"),
			model.NewHeader("To", "<sip:"+deviceID.String()+"@3402000000>"),
			model.NewHeader("Call-ID", "ack-call-1"),
			model.NewHeader("CSeq", "1 ACK"),
			model.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-ack"),
		}, "")
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		before := h.tr.answers()
		h.tr.inbound <- ack
		// 等 150ms，足够让 read loop 处理完毕。
		deadline := time.Now().Add(150 * time.Millisecond)
		for time.Now().Before(deadline) {
			if h.tr.answers() > before {
				t.Fatalf("device sent an answer for ACK, want silence (RFC 3261 forbids UAS answering ACK)")
			}
			time.Sleep(5 * time.Millisecond)
		}
	})
}

// ----------------------------------------------------------------------------
// 测试 3.3：device 节点无 MediaConfig 时，catalog response 不含 MediaSource
// ----------------------------------------------------------------------------

// TestAcceptor_DeviceCatalogResponseWithoutMediaSource 验证 device 节点的 profile
// 不配置任何 MediaConfig（节点级 nil、channel 级空）时，catalog query 仍然成功，
// 但 MediaSource 扩展块不存在（反解后 media 列表为空）。
//
// 验收（tasks.md 3.3）：
//   - catalog query 返回 200
//   - body 反解后 media 长度为 0
func TestAcceptor_DeviceCatalogResponseWithoutMediaSource(t *testing.T) {
	deviceID := mustDeviceNodeID(t, "34020000001310000003")

	// profile 有 channel 但没有任何 mediaConfig。
	ch := mustChannel(t, "34020000001310000031", "cam-3", "34020000001310000003", model.ChannelStatusOnline)
	h := newDeviceHarness(t, deviceID, []model.Channel{ch}, nil)

	body := catalogQueryBody(deviceID.String())
	req := deviceMessageRequest(t, deviceID.String(), body)
	resp := h.tr.deliver(t, req)

	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}

	cat, media, err := h.codec.DecodeCatalogResponse(resp.Body())
	if err != nil {
		t.Fatalf("DecodeCatalogResponse: %v", err)
	}
	// catalog 仍有 1 个 channel item（无 media source 不影响 channel 列表）。
	if cat.SumNum() != 1 {
		t.Fatalf("catalog SumNum = %d, want 1 (channel still present)", cat.SumNum())
	}
	// 但 media 列表为空（无 MediaSource 扩展）。
	if len(media) != 0 {
		t.Errorf("media sources = %d, want 0 (no MediaConfig configured)", len(media))
	}
}

// ----------------------------------------------------------------------------
// 测试 3.4：device 节点仅节点级 MediaConfig 时，多 channel 共用该配置
// ----------------------------------------------------------------------------

// TestAcceptor_DeviceCatalogResponseWithNodeMediaFallback 验证 device 节点
// profile 仅有节点级 mediaConfig（无 channel 级）且有 2 个 channels 时，
// catalog query 返回的 MediaSource 扩展为 2 条 entry（每条 channel 都命中节点级配置）。
//
// 验收（tasks.md 3.4）：
//   - Item 数 == 2
//   - 反解的 media 包含 2 个 entry，每个 channel id 都命中节点级 MediaConfig
func TestAcceptor_DeviceCatalogResponseWithNodeMediaFallback(t *testing.T) {
	deviceID := mustDeviceNodeID(t, "34020000001310000004")

	// 2 个 channels，无 channel 级 mediaConfig。
	ch1 := mustChannel(t, "34020000001310000041", "cam-4a", "34020000001310000004", model.ChannelStatusOnline)
	ch2 := mustChannel(t, "34020000001310000042", "cam-4b", "34020000001310000004", model.ChannelStatusOffline)
	// 节点级 mediaConfig。
	mediaCfg := model.MediaConfig{
		Kind:  model.SourceKindRTSP,
		Path:  "rtsp://example.com/stream",
		FPS:   25,
		Clock: 90000,
	}
	h := newDeviceHarness(t, deviceID, []model.Channel{ch1, ch2}, &mediaCfg)

	body := catalogQueryBody(deviceID.String())
	req := deviceMessageRequest(t, deviceID.String(), body)
	resp := h.tr.deliver(t, req)

	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}

	cat, media, err := h.codec.DecodeCatalogResponse(resp.Body())
	if err != nil {
		t.Fatalf("DecodeCatalogResponse: %v", err)
	}

	// 2 个 channel items。
	if cat.SumNum() != 2 {
		t.Fatalf("catalog SumNum = %d, want 2", cat.SumNum())
	}
	if len(cat.Items()) != 2 {
		t.Fatalf("catalog Items = %d, want 2", len(cat.Items()))
	}

	// 2 个 media entry，每个 channel 都命中节点级配置。
	if len(media) != 2 {
		t.Fatalf("media sources = %d, want 2 (node-level fallback applied to both channels)", len(media))
	}

	// 收集 media 中出现的 channel id。
	mediaChannelIDs := make(map[string]bool)
	for _, m := range media {
		mediaChannelIDs[m.DeviceID] = true
		if m.Config.Kind != model.SourceKindRTSP {
			t.Errorf("media Kind = %q, want rtsp", m.Config.Kind)
		}
		if m.Config.Path != "rtsp://example.com/stream" {
			t.Errorf("media Path = %q, want rtsp://example.com/stream", m.Config.Path)
		}
	}
	if !mediaChannelIDs["34020000001310000041"] {
		t.Errorf("media missing channel 34020000001310000041")
	}
	if !mediaChannelIDs["34020000001310000042"] {
		t.Errorf("media missing channel 34020000001310000042")
	}
}

// ----------------------------------------------------------------------------
// 补充：device 节点同时兼容 <Query> 和 <Notify> 两种根元素
// 这是 protocol-correctness 测试，覆盖 DecodeNotify 的修复
// ----------------------------------------------------------------------------

// TestAcceptor_DeviceAnswersCatalogQuery_NotifyEnvelope 验证 device 节点
// 对 <Notify> 封装的 catalog query（上行方向）也正常应答。
// 与 TestAcceptor_DeviceAnswersCatalogQuery 使用相同的 profile 和断言，
// 仅 body 根元素不同。
func TestAcceptor_DeviceAnswersCatalogQuery_NotifyEnvelope(t *testing.T) {
	deviceID := mustDeviceNodeID(t, "34020000001310000006")
	ch := mustChannel(t, "34020000001310000061", "cam-6", "34020000001310000006", model.ChannelStatusOnline)
	mediaCfg := model.MediaConfig{Kind: model.SourceKindFile, Path: "/fallback.mp4", Loop: true}
	h := newDeviceHarness(t, deviceID, []model.Channel{ch}, &mediaCfg)

	// 用 <Notify> 根元素（设备→平台方向）
	body := catalogNotifyBody(deviceID.String())
	req := deviceMessageRequest(t, deviceID.String(), body)
	resp := h.tr.deliver(t, req)

	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200 (Notify envelope)", resp.StatusCode())
	}
	cat, _, err := h.codec.DecodeCatalogResponse(resp.Body())
	if err != nil {
		t.Fatalf("DecodeCatalogResponse: %v", err)
	}
	if cat.SumNum() != 1 {
		t.Fatalf("SumNum = %d, want 1", cat.SumNum())
	}
}

// ----------------------------------------------------------------------------
// 补充：device 节点 realm 空字符串时 ServeDevice 返回错误
// （tasks.md 1.2 验收标准：realm 校验）
// ----------------------------------------------------------------------------

// TestAcceptor_DeviceServeDeviceRejectsEmptyRealm 验证 ServeDevice 在 realm 为
// 空字符串时立即返回错误，不启动任何 goroutine。
func TestAcceptor_DeviceServeDeviceRejectsEmptyRealm(t *testing.T) {
	deviceID := mustDeviceNodeID(t, "34020000001310000005")
	tr := newAcceptorTransport()
	creds := newFakeCredentials()
	devices := newFakeDevices()
	clock := newSyncClock(time.Now())
	codec := manscdp.NewMANSCDPCodec()
	tickers := newTickerScript()

	acceptor, err := NewAcceptor(context.Background(), clock, &fakeChallenger{},
		&fakeAuthenticator{}, creds, devices, codec, tickers.factory(), discardLogger())
	if err != nil {
		t.Fatalf("NewAcceptor: %v", err)
	}

	err = acceptor.ServeDevice(deviceID, tr, "") // realm 为空
	if err == nil {
		t.Error("ServeDevice with empty realm: got nil, want error")
	}
	if acceptor.Serving() != 0 {
		t.Errorf("serving = %d after failed ServeDevice, want 0", acceptor.Serving())
	}
}

// ----------------------------------------------------------------------------
// 补充：device 节点可独立于 platform 节点运行（无 registry 时仍应答）
// ----------------------------------------------------------------------------

// TestAcceptor_DeviceCatalogWithoutRegistry 验证即使没有挂载 NodeRegistry，
// device 节点仍能正确应答 catalog query（走设备表路径，设备表为空时 SumNum=0）。
func TestAcceptor_DeviceCatalogWithoutRegistry(t *testing.T) {
	deviceID := mustDeviceNodeID(t, "34020000001310000006")

	// 构造 harness 但不调用 WithNodeRegistry。
	tr := newAcceptorTransport()
	codec := manscdp.NewMANSCDPCodec()
	devices := newFakeDevices()
	creds := newFakeCredentials()
	clock := newSyncClock(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC))
	tickers := newTickerScript()

	acceptor, err := NewAcceptor(context.Background(), clock, &fakeChallenger{},
		&fakeAuthenticator{}, creds, devices, codec, tickers.factory(), discardLogger())
	if err != nil {
		t.Fatalf("NewAcceptor: %v", err)
	}
	// 注意：没有 WithNodeRegistry()，registry 字段为 nil。

	if err := acceptor.ServeDevice(deviceID, tr, "3402000000"); err != nil {
		t.Fatalf("ServeDevice: %v", err)
	}
	t.Cleanup(func() { _ = acceptor.Close() })

	// 无 registry、无 channels，catalog 仍能应答（设备表为空，SumNum=0）。
	body := catalogQueryBody(deviceID.String())
	req := deviceMessageRequest(t, deviceID.String(), body)
	resp := tr.deliver(t, req)

	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200 without registry", resp.StatusCode())
	}

	cat, _, err := codec.DecodeCatalogResponse(resp.Body())
	if err != nil {
		t.Fatalf("DecodeCatalogResponse: %v", err)
	}
	if cat.SumNum() != 0 {
		t.Errorf("catalog SumNum = %d, want 0 (empty device table, no registry)", cat.SumNum())
	}
}

// ----------------------------------------------------------------------------
// 测试：device 节点与 platform-large 节点共用 Acceptor（spec scenario "device 节点与
// platform-large 共用 Acceptor"）：同一 Acceptor 实例同时 Serve(ServeDevice) 两类
// 节点，两套 transport 互不干扰。
// ----------------------------------------------------------------------------

func TestAcceptor_DeviceAndPlatformShareAcceptor(t *testing.T) {
	deviceID := mustDeviceNodeID(t, "34020000001310000007")
	platformID, err := model.ParseNodeID("34020000002000000007")
	if err != nil {
		t.Fatalf("ParseNodeID(platform): %v", err)
	}
	if platformID.Kind() != model.NodeKindPlatformLarge {
		t.Fatalf("platform id kind = %v, want NodeKindPlatformLarge", platformID.Kind())
	}

	deviceTr := newAcceptorTransport()
	platformTr := newAcceptorTransport()
	codec := manscdp.NewMANSCDPCodec()
	devices := newFakeDevices()
	creds := newFakeCredentials()
	clock := newSyncClock(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC))
	tickers := newTickerScript()

	acceptor, err := NewAcceptor(context.Background(), clock, &fakeChallenger{},
		&fakeAuthenticator{}, creds, devices, codec, tickers.factory(), discardLogger())
	if err != nil {
		t.Fatalf("NewAcceptor: %v", err)
	}

	// 准备 device profile（含 1 channel）使 answerCatalog 能查到内容。
	reg := nodereg.New()
	profile, err := model.NewNodeProfile(deviceID.String(), "127.0.0.1:15060", "3402000000", "sim")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	ch := mustChannel(t, "34020000001310000071", "cam-7", deviceID.String(), model.ChannelStatusOnline)
	profile, err = profile.WithChannels([]model.Channel{ch})
	if err != nil {
		t.Fatalf("WithChannels: %v", err)
	}
	if _, err := reg.Register(context.Background(), profile); err != nil {
		t.Fatalf("Register device profile: %v", err)
	}
	acceptor.WithNodeRegistry(reg)
	t.Cleanup(func() { _ = acceptor.Close() })

	// 共享同一 Acceptor：device 用 ServeDevice，platform-large 用 Serve。
	if err := acceptor.ServeDevice(deviceID, deviceTr, "3402000000"); err != nil {
		t.Fatalf("ServeDevice: %v", err)
	}
	policy, err := model.NewExpiresPolicy(3600, 3600, 3600)
	if err != nil {
		t.Fatalf("NewExpiresPolicy: %v", err)
	}
	if err := acceptor.Serve(platformID, platformTr, "3402000000", policy); err != nil {
		t.Fatalf("Serve platform-large: %v", err)
	}

	if got := acceptor.Serving(); got != 2 {
		t.Fatalf("serving = %d, want 2 (device + platform-large)", got)
	}

	// device 端应答 catalog query。
	body := catalogQueryBody(deviceID.String())
	req := deviceMessageRequest(t, deviceID.String(), body)
	resp := deviceTr.deliver(t, req)
	if resp.StatusCode() != 200 {
		t.Fatalf("device status = %d, want 200", resp.StatusCode())
	}
	if got := responseHeader(t, resp, "Content-Type"); got != "Application/MANSCDP+XML" {
		t.Errorf("device Content-Type = %q, want Application/MANSCDP+XML", got)
	}

	// platform-large 端独立收到自己的 MESSAGE 不会被 device read loop 截走。
	bodyPlatform := catalogQueryBody(platformID.String())
	reqPlatform := deviceMessageRequest(t, platformID.String(), bodyPlatform)
	respPlatform := platformTr.deliver(t, reqPlatform)
	if respPlatform.StatusCode() != 200 {
		t.Fatalf("platform-large status = %d, want 200", respPlatform.StatusCode())
	}
}
