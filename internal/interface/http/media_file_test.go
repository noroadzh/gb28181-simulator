// Package httpapi_test 涵盖 /v1/nodes/:id/channels/:ch/media-file 端点的
// 行为契约：200 直出、Range/206、channel→node 配置回退、非 file 来源 400、
// 双层缺配置 404、文件缺失 404。
package httpapi_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	httpapi "github.com/your-org/gb28181-simulator/internal/interface/http"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
)

// fakeChannelView 只实现 media-file 端点用到的三条方法，其余返回零值。
type fakeChannelView struct {
	channels map[string]model.Channel
	media    map[string]model.MediaConfig
	hasMedia map[string]bool
}

func newFakeChannelView() *fakeChannelView {
	return &fakeChannelView{
		channels: map[string]model.Channel{},
		media:    map[string]model.MediaConfig{},
		hasMedia: map[string]bool{},
	}
}

func (f *fakeChannelView) key(id model.NodeID, ch string) string {
	return id.String() + "/" + ch
}

func (f *fakeChannelView) seed(t *testing.T, nodeID, channelID string) model.Channel {
	t.Helper()
	ch, err := model.NewChannel(channelID, "cam-"+channelID, "", model.ChannelStatusOnline)
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}
	f.channels[nodeID+"/"+channelID] = ch
	return ch
}

func (f *fakeChannelView) setMedia(nodeID, channelID string, cfg model.MediaConfig) {
	f.media[nodeID+"/"+channelID] = cfg
	f.hasMedia[nodeID+"/"+channelID] = true
}

func (f *fakeChannelView) ListChannels(_ context.Context, _ model.NodeID) []model.Channel {
	return nil
}

func (f *fakeChannelView) Channel(_ context.Context, id model.NodeID, channelID string) (model.Channel, bool) {
	ch, ok := f.channels[id.String()+"/"+channelID]
	return ch, ok
}

func (f *fakeChannelView) GetChannelMedia(_ context.Context, id model.NodeID, channelID string) (model.MediaConfig, bool, error) {
	k := f.key(id, channelID)
	return f.media[k], f.hasMedia[k], nil
}

func (f *fakeChannelView) SetChannelMedia(_ context.Context, _ model.NodeID, _ string, _ model.MediaConfig) error {
	return nil
}

func (f *fakeChannelView) ClearChannelMedia(_ context.Context, _ model.NodeID, _ string) error {
	return nil
}

func (f *fakeChannelView) PTZ(_ context.Context, _ model.NodeID, _, _ string, _, _ int) error {
	return nil
}

func (f *fakeChannelView) Records(_ context.Context, _ model.NodeID, _ string, _, _ time.Time) ([]model.RecordInfoItem, error) {
	return nil, nil
}

func (f *fakeChannelView) Playback(_ context.Context, _ model.NodeID, _, _ string, _ int) error {
	return nil
}

func (f *fakeChannelView) TalkStart(_ context.Context, _ model.NodeID, _ string) (string, string, error) {
	return "", "", fmt.Errorf("unsupported")
}

func (f *fakeChannelView) TalkStop(_ context.Context, _ model.NodeID, _, _ string) error {
	return nil
}

func (f *fakeChannelView) Snapshot(_ context.Context, _ model.NodeID, _ string) ([]byte, string, error) {
	return nil, "", fmt.Errorf("unsupported")
}

// newMediaFileServer 组装一个带 fake NodeView + fake ChannelView 的测试服务器。
func newMediaFileServer(t *testing.T, nodes httpapi.NodeView, chans httpapi.ChannelView) *httptest.Server {
	t.Helper()
	s := httpapi.NewServer(platformconfig.Config{}, logging.NewHub(4), httpapi.Version{Version: "x"}, nodes, nil, chans, nil, nil)
	return httptest.NewServer(s.Echo())
}

// writeTempMP4 生成一个内容已知的临时 mp4 文件，返回其绝对路径与内容。
func writeTempMP4(t *testing.T, content []byte) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "sample.mp4")
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatalf("write temp mp4: %v", err)
	}
	return p
}

// TestMediaFile_200DirectServesFile 场景1：channel 级 file 配置 + 存在的临时
// mp4 → 200、Content-Type video/mp4、body 等于文件内容。
func TestMediaFile_200DirectServesFile(t *testing.T) {
	content := []byte("fake-mp4-payload-0123456789")
	path := writeTempMP4(t, content)

	nodes := newFakeNodeView()
	const nodeID = "34020000011310000001"
	const chID = "34020000011320000001"
	nodes.seed(t, nodeID, "127.0.0.1:5060", model.StatusIdle)

	chans := newFakeChannelView()
	chans.seed(t, nodeID, chID)
	chans.setMedia(nodeID, chID, model.MediaConfig{Kind: model.SourceKindFile, Path: path})

	srv := newMediaFileServer(t, nodes, chans)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/nodes/" + nodeID + "/channels/" + chID + "/media-file")
	if err != nil {
		t.Fatalf("GET media-file: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "video/mp4" {
		t.Fatalf("Content-Type = %q, want video/mp4", ct)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("body = %q, want %q", got, content)
	}
}

// TestMediaFile_RangeRequest206 场景2：带 Range: bytes=0-3 → 206 + Content-Range
// + body 为前 4 字节。
func TestMediaFile_RangeRequest206(t *testing.T) {
	content := []byte("0123456789ABCDEF")
	path := writeTempMP4(t, content)

	nodes := newFakeNodeView()
	const nodeID = "34020000011310000001"
	const chID = "34020000011320000001"
	nodes.seed(t, nodeID, "127.0.0.1:5060", model.StatusIdle)

	chans := newFakeChannelView()
	chans.seed(t, nodeID, chID)
	chans.setMedia(nodeID, chID, model.MediaConfig{Kind: model.SourceKindFile, Path: path})

	srv := newMediaFileServer(t, nodes, chans)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/nodes/"+nodeID+"/channels/"+chID+"/media-file", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Range", "bytes=0-3")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", resp.StatusCode)
	}
	cr := resp.Header.Get("Content-Range")
	if !strings.HasPrefix(cr, "bytes 0-3/") {
		t.Fatalf("Content-Range = %q, want prefix bytes 0-3/", cr)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != string(content[:4]) {
		t.Fatalf("body = %q, want %q", body, content[:4])
	}
}

// TestMediaFile_FallbackToNodeMedia 场景3：channel 级无配置但节点级有 → 200。
func TestMediaFile_FallbackToNodeMedia(t *testing.T) {
	content := []byte("node-level-mp4-bytes")
	path := writeTempMP4(t, content)

	nodes := newFakeNodeView()
	const nodeID = "34020000011310000001"
	const chID = "34020000011320000001"
	nodes.seed(t, nodeID, "127.0.0.1:5060", model.StatusIdle)
	if err := nodes.SetMedia(context.Background(), mustNodeID(t, nodeID), model.MediaConfig{Kind: model.SourceKindFile, Path: path}); err != nil {
		t.Fatalf("SetMedia: %v", err)
	}

	chans := newFakeChannelView()
	chans.seed(t, nodeID, chID)

	srv := newMediaFileServer(t, nodes, chans)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/nodes/" + nodeID + "/channels/" + chID + "/media-file")
	if err != nil {
		t.Fatalf("GET media-file: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

// TestMediaFile_NonFileKind400 场景4：kind=rtsp → 400。
func TestMediaFile_NonFileKind400(t *testing.T) {
	nodes := newFakeNodeView()
	const nodeID = "34020000011310000001"
	const chID = "34020000011320000001"
	nodes.seed(t, nodeID, "127.0.0.1:5060", model.StatusIdle)

	chans := newFakeChannelView()
	chans.seed(t, nodeID, chID)
	chans.setMedia(nodeID, chID, model.MediaConfig{Kind: model.SourceKindRTSP, Path: "rtsp://example.com/live"})

	srv := newMediaFileServer(t, nodes, chans)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/nodes/" + nodeID + "/channels/" + chID + "/media-file")
	if err != nil {
		t.Fatalf("GET media-file: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestMediaFile_NoConfigAnywhere404 场景5：channel 与节点均无配置 → 404。
func TestMediaFile_NoConfigAnywhere404(t *testing.T) {
	nodes := newFakeNodeView()
	const nodeID = "34020000011310000001"
	const chID = "34020000011320000001"
	nodes.seed(t, nodeID, "127.0.0.1:5060", model.StatusIdle)

	chans := newFakeChannelView()
	chans.seed(t, nodeID, chID)

	srv := newMediaFileServer(t, nodes, chans)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/nodes/" + nodeID + "/channels/" + chID + "/media-file")
	if err != nil {
		t.Fatalf("GET media-file: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestMediaFile_MissingFile404 场景6：kind=file 但路径不存在 → 404。
func TestMediaFile_MissingFile404(t *testing.T) {
	nodes := newFakeNodeView()
	const nodeID = "34020000011310000001"
	const chID = "34020000011320000001"
	nodes.seed(t, nodeID, "127.0.0.1:5060", model.StatusIdle)

	chans := newFakeChannelView()
	chans.seed(t, nodeID, chID)
	chans.setMedia(nodeID, chID, model.MediaConfig{Kind: model.SourceKindFile, Path: "/nonexistent/definitely-missing.mp4"})

	srv := newMediaFileServer(t, nodes, chans)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/nodes/" + nodeID + "/channels/" + chID + "/media-file")
	if err != nil {
		t.Fatalf("GET media-file: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestMediaFile_LocalFileAliasNormalized 场景7：通道级配置使用 local_file 别名
// → 经 Normalize 后等价 kind=file，200 且 body 等于文件内容。
func TestMediaFile_LocalFileAliasNormalized(t *testing.T) {
	content := []byte("local-file-mp4-payload-0123456789")
	path := writeTempMP4(t, content)

	nodes := newFakeNodeView()
	const nodeID = "34020000011310000001"
	const chID = "34020000011320000001"
	nodes.seed(t, nodeID, "127.0.0.1:5060", model.StatusIdle)

	chans := newFakeChannelView()
	chans.seed(t, nodeID, chID)
	chans.setMedia(nodeID, chID, model.MediaConfig{Kind: "local_file", Path: path})

	srv := newMediaFileServer(t, nodes, chans)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/nodes/" + nodeID + "/channels/" + chID + "/media-file")
	if err != nil {
		t.Fatalf("GET media-file: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "video/mp4" {
		t.Fatalf("Content-Type = %q, want video/mp4", ct)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("body = %q, want %q", got, content)
	}
}
