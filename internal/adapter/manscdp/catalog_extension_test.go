package manscdp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

func TestMarshalCatalogWithMedia_Golden(t *testing.T) {
	item, err := model.NewCatalogItem(model.CatalogItemParams{
		DeviceID:     "34020000001310000001",
		Name:         "camera-front-door",
		Manufacturer: "gb28181-simulator",
		Model:        "simulator",
		CivilCode:    "340200",
		Parental:     0,
		SafetyWay:    0,
		RegisterWay:  1,
		Secrecy:      0,
		Status:       model.CatalogStatusON,
	})
	if err != nil {
		t.Fatalf("NewCatalogItem: %v", err)
	}
	catalog, err := model.NewCatalog("34020000001310000001", 5, []model.CatalogItem{item})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	media := map[string]model.MediaConfig{
		"34020000001310000001": {
			Kind:  model.SourceKindFile,
			Path:  "/uploads/a.mp4",
			Loop:  true,
			MTU:   1400,
			FPS:   25,
			Clock: 90000,
		},
	}
	got, err := NewMANSCDPCodec().MarshalCatalogWithMedia(catalog, media)
	if err != nil {
		t.Fatalf("MarshalCatalogWithMedia: %v", err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "catalog_with_extension.golden.xml"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got != string(want) {
		t.Errorf("catalog bytes changed.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// 没有媒体源时不发出扩展段：零值配置必须落回MarshalCatalog 的既有字节。
func TestMarshalCatalogWithMedia_ZeroConfigOmitsExtension(t *testing.T) {
	item, err := model.NewCatalogItem(model.CatalogItemParams{DeviceID: "34020000001310000001"})
	if err != nil {
		t.Fatalf("NewCatalogItem: %v", err)
	}
	catalog, err := model.NewCatalog("34020000001310000001", 1, []model.CatalogItem{item})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	got, err := NewMANSCDPCodec().MarshalCatalogWithMedia(catalog, map[string]model.MediaConfig{
		"34020000001310000001": {},
	})
	if err != nil {
		t.Fatalf("MarshalCatalogWithMedia: %v", err)
	}
	if want := "<Extension>"; contains(got, want) {
		t.Errorf("a zero-valued MediaConfig must not produce an Extension block:\n%s", got)
	}
}

// 未在 map 中的 device id 不带扩展段：缺 key 与 nil map 等价。
func TestMarshalCatalogWithMedia_MissingKeyOmitsExtension(t *testing.T) {
	item, err := model.NewCatalogItem(model.CatalogItemParams{DeviceID: "34020000001310000001"})
	if err != nil {
		t.Fatalf("NewCatalogItem: %v", err)
	}
	catalog, err := model.NewCatalog("34020000001310000001", 1, []model.CatalogItem{item})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	got, err := NewMANSCDPCodec().MarshalCatalogWithMedia(catalog, map[string]model.MediaConfig{
		"99999999999999999999": {Kind: model.SourceKindFile, Path: "/x.mp4"},
	})
	if err != nil {
		t.Fatalf("MarshalCatalogWithMedia: %v", err)
	}
	if contains(got, "<Extension>") {
		t.Errorf("an item absent from mediaByDeviceID must not carry an Extension:\n%s", got)
	}
}

func TestDecodeCatalogResponse_Standard(t *testing.T) {
	codec := NewMANSCDPCodec()
	body, err := os.ReadFile(filepath.Join("testdata", "catalog_one.xml"))
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	cat, media, err := codec.DecodeCatalogResponse(string(body))
	if err != nil {
		t.Fatalf("DecodeCatalogResponse: %v", err)
	}
	if cat.SumNum() != 1 || len(cat.Items()) != 1 {
		t.Fatalf("catalog = %d items, want 1", len(cat.Items()))
	}
	if cat.Items()[0].DeviceID() != "34020000011310000001" {
		t.Errorf("device id = %q", cat.Items()[0].DeviceID())
	}
	if cat.Items()[0].Status() != model.CatalogStatusON {
		t.Errorf("status = %q, want ON", cat.Items()[0].Status())
	}
	// 标准响应没有扩展段，media 切片必须为空（而不是一个零值条目）。
	if len(media) != 0 {
		t.Errorf("media = %d entries, want 0 for a standard response", len(media))
	}
}

func TestDecodeCatalogResponse_WithExtension(t *testing.T) {
	codec := NewMANSCDPCodec()
	body, err := os.ReadFile(filepath.Join("testdata", "catalog_with_extension.golden.xml"))
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	cat, media, err := codec.DecodeCatalogResponse(string(body))
	if err != nil {
		t.Fatalf("DecodeCatalogResponse: %v", err)
	}
	if cat.SumNum() != 1 {
		t.Errorf("sum num = %d, want 1", cat.SumNum())
	}
	if len(media) != 1 {
		t.Fatalf("media entries = %d, want 1", len(media))
	}
	got := media[0]
	if got.DeviceID != "34020000001310000001" {
		t.Errorf("device id = %q", got.DeviceID)
	}
	if got.Config.Kind != model.SourceKindFile {
		t.Errorf("kind = %q, want file", got.Config.Kind)
	}
	if got.Config.Path != "/uploads/a.mp4" {
		t.Errorf("path = %q", got.Config.Path)
	}
	if !got.Config.Loop {
		t.Errorf("loop = false, want true")
	}
	if got.Config.MTU != 1400 || got.Config.FPS != 25 || got.Config.Clock != 90000 {
		t.Errorf("timing = %d/%d/%d, want 1400/25/90000", got.Config.MTU, got.Config.FPS, got.Config.Clock)
	}
}

// 编解码往返必须无损：平台端拿到的 MediaConfig 应与 device 端发出的完全一致。
func TestDecodeCatalogResponse_RoundTripWithMedia(t *testing.T) {
	codec := NewMANSCDPCodec()
	item, err := model.NewCatalogItem(model.CatalogItemParams{DeviceID: "34020000001310000001"})
	if err != nil {
		t.Fatalf("NewCatalogItem: %v", err)
	}
	catalog, err := model.NewCatalog("34020000001310000001", 2, []model.CatalogItem{item})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	want := model.MediaConfig{
		Kind:  model.SourceKindRTSP,
		Path:  "rtsp://10.0.0.9/live",
		Loop:  false,
		MTU:   1200,
		FPS:   30,
		Clock: 90000,
	}
	body, err := codec.MarshalCatalogWithMedia(catalog, map[string]model.MediaConfig{
		"34020000001310000001": want,
	})
	if err != nil {
		t.Fatalf("MarshalCatalogWithMedia: %v", err)
	}
	_, media, err := codec.DecodeCatalogResponse(string(body))
	if err != nil {
		t.Fatalf("DecodeCatalogResponse: %v", err)
	}
	if len(media) != 1 {
		t.Fatalf("media entries = %d, want 1", len(media))
	}
	if media[0].Config != want {
		t.Errorf("round trip config = %+v, want %+v", media[0].Config, want)
	}
}

// 扩展段属性缺失时整体解析仍成功，且该通道 media 为空（交给 app 层跳过）。
func TestDecodeCatalogResponse_MalformedExtensionStillSucceeds(t *testing.T) {
	codec := NewMANSCDPCodec()
	body := `<?xml version="1.0" encoding="UTF-8"?>
<Response>
  <CmdType>Catalog</CmdType>
  <SN>9</SN>
  <DeviceID>34020000001310000001</DeviceID>
  <SumNum>1</SumNum>
  <DeviceList Num="1">
    <Item>
      <DeviceID>34020000001310000001</DeviceID>
      <Name>cam</Name>
      <Manufacturer>gb28181-simulator</Manufacturer>
      <Model>simulator</Model>
      <CivilCode>340200</CivilCode>
      <Parental>0</Parental>
      <SafetyWay>0</SafetyWay>
      <RegisterWay>1</RegisterWay>
      <Secrecy>0</Secrecy>
      <Status>ON</Status>
      <Extension>
        <MediaSource kind=""/>
      </Extension>
    </Item>
  </DeviceList>
</Response>`
	cat, media, err := codec.DecodeCatalogResponse(body)
	if err != nil {
		t.Fatalf("DecodeCatalogResponse must tolerate a malformed extension: %v", err)
	}
	if cat.SumNum() != 1 {
		t.Errorf("sum num = %d, want 1 — a bad extension must not drop the item", cat.SumNum())
	}
	if len(media) != 1 {
		t.Fatalf("media entries = %d, want 1 (kind is empty; the app layer skips it)", len(media))
	}
	if media[0].Config.Kind != "" {
		t.Errorf("kind = %q, want empty so the caller skips it", media[0].Config.Kind)
	}
}

func TestDecodeCatalogResponse_RejectsWrongCmdType(t *testing.T) {
	codec := NewMANSCDPCodec()
	body := `<?xml version="1.0" encoding="UTF-8"?>
<Response>
  <CmdType>DeviceInfo</CmdType>
  <SN>1</SN>
  <DeviceID>34020000001310000001</DeviceID>
  <SumNum>0</SumNum>
  <DeviceList Num="0"></DeviceList>
</Response>`
	if _, _, err := codec.DecodeCatalogResponse(body); err == nil {
		t.Fatal("DecodeCatalogResponse must reject a non-Catalog CmdType")
	}
}

func TestDecodeCatalogResponse_RejectsEmptyBody(t *testing.T) {
	if _, _, err := NewMANSCDPCodec().DecodeCatalogResponse(""); err == nil {
		t.Fatal("DecodeCatalogResponse must reject an empty body")
	}
}

// contains 是测试内的字符串查找小工具，避免为一个断言引入 strings 依赖噪音。
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
		_ = i
	}
	return false
}
