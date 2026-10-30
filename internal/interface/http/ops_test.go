package httpapi_test

// device-control-coverage: the operations endpoints.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	httpapi "github.com/your-org/gb28181-simulator/internal/interface/http"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
)

type opsNodeView struct {
	*fakeNodeView
	online   bool
	armed    bool
	armedSet bool
	cfg      model.BasicConfig
	upgraded string
	rebooted bool
	download struct {
		channel string
		start   time.Time
		err     error
	}
}

func (v *opsNodeView) DeviceStatus(context.Context, model.NodeID) (bool, error) {
	return v.online, nil
}

func (v *opsNodeView) Alarms(context.Context, model.NodeID, time.Time, time.Time) ([]model.AlarmSnapshot, error) {
	snap, err := model.NewAlarmSnapshot("a1", "34020000001310000001", "ch-1", 1, 5, "motion", "2026-10-30T10:00:00")
	if err != nil {
		return nil, err
	}
	return []model.AlarmSnapshot{snap}, nil
}

func (v *opsNodeView) SetAlarmMode(_ context.Context, _ model.NodeID, armed bool) error {
	v.armed, v.armedSet = armed, true
	return nil
}

func (v *opsNodeView) AlarmMode(context.Context, model.NodeID) (bool, bool, error) {
	return v.armed, v.armedSet, nil
}

func (v *opsNodeView) Config(context.Context, model.NodeID) (model.BasicConfig, error) {
	return v.cfg, nil
}

func (v *opsNodeView) SetConfig(_ context.Context, _ model.NodeID, cfg model.BasicConfig) error {
	v.cfg = cfg
	return nil
}

func (v *opsNodeView) Upgrade(_ context.Context, _ model.NodeID, version string) error {
	v.upgraded = version
	return nil
}

func (v *opsNodeView) Reboot(context.Context, model.NodeID) error {
	v.rebooted = true
	return nil
}

func (v *opsNodeView) StartDownload(_ context.Context, _ model.NodeID, channelID string, start, _ time.Time) (string, error) {
	v.download.channel = channelID
	v.download.start = start
	if v.download.err != nil {
		return "", v.download.err
	}
	return "call-1", nil
}

func newOpsServer(t *testing.T) (*httptest.Server, model.NodeID, *opsNodeView) {
	t.Helper()
	view := &opsNodeView{fakeNodeView: newFakeNodeView(), online: true}
	node := view.seed(t, "34020000002000000001", "127.0.0.1:15061", model.StatusOnline)
	s := httpapi.NewServer(platformconfig.Config{}, logging.NewHub(4), httpapi.Version{Version: "x"},
		view, nil, nil, nil, nil)
	ts := httptest.NewServer(s.Echo())
	t.Cleanup(ts.Close)
	return ts, node.ID(), view
}

func TestOpsHTTP_Status(t *testing.T) {
	ts, nodeID, _ := newOpsServer(t)
	resp, err := http.Get(ts.URL + "/v1/nodes/" + nodeID.String() + "/status")
	if err != nil {
		t.Fatalf("GET status: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got struct {
		Online bool `json:"online"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if !got.Online {
		t.Error("online = false")
	}
}

func TestOpsHTTP_AlarmModeAndAlarms(t *testing.T) {
	ts, nodeID, view := newOpsServer(t)
	base := ts.URL + "/v1/nodes/" + nodeID.String() + "/channels/ch-1"

	resp, err := http.Post(base+"/alarm-mode", "application/json", strings.NewReader(`{"mode":"armed"}`))
	if err != nil {
		t.Fatalf("POST alarm-mode: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || !view.armed {
		t.Fatalf("status=%d armed=%v", resp.StatusCode, view.armed)
	}

	resp2, err := http.Post(base+"/alarm-mode", "application/json", strings.NewReader(`{"mode":"sometimes"}`))
	if err != nil {
		t.Fatalf("POST bad alarm-mode: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Errorf("bad mode = %d, want 400", resp2.StatusCode)
	}

	resp3, err := http.Get(base + "/alarms")
	if err != nil {
		t.Fatalf("GET alarms: %v", err)
	}
	defer func() { _ = resp3.Body.Close() }()
	body, _ := io.ReadAll(resp3.Body)
	if !strings.Contains(string(body), "motion") {
		t.Errorf("alarms body = %s", body)
	}
}

func TestOpsHTTP_ConfigUpgradeReboot(t *testing.T) {
	ts, nodeID, view := newOpsServer(t)
	base := ts.URL + "/v1/nodes/" + nodeID.String()

	resp, err := http.Get(base + "/config")
	if err != nil {
		t.Fatalf("GET config: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET config = %d", resp.StatusCode)
	}

	put, err := http.NewRequest(http.MethodPut, base+"/config",
		strings.NewReader(`{"heartbeat_interval":45}`))
	if err != nil {
		t.Fatalf("build PUT: %v", err)
	}
	put.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(put)
	if err != nil {
		t.Fatalf("PUT config: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK || view.cfg.HeartbeatInterval != 45 {
		t.Fatalf("PUT config = %d, interval=%d", resp2.StatusCode, view.cfg.HeartbeatInterval)
	}

	resp3, err := http.Post(base+"/upgrade", "application/json", strings.NewReader(`{"version":"2.0.0"}`))
	if err != nil {
		t.Fatalf("POST upgrade: %v", err)
	}
	defer func() { _ = resp3.Body.Close() }()
	if resp3.StatusCode != http.StatusOK || view.upgraded != "2.0.0" {
		t.Fatalf("upgrade = %d version=%q", resp3.StatusCode, view.upgraded)
	}

	resp4, err := http.Post(base+"/reboot", "application/json", nil)
	if err != nil {
		t.Fatalf("POST reboot: %v", err)
	}
	defer func() { _ = resp4.Body.Close() }()
	if resp4.StatusCode != http.StatusNoContent || !view.rebooted {
		t.Fatalf("reboot = %d rebooted=%v", resp4.StatusCode, view.rebooted)
	}
}

func TestOpsHTTP_Download(t *testing.T) {
	ts, nodeID, view := newOpsServer(t)
	body := fmt.Sprintf(`{"start":%q,"end":%q}`,
		"2026-10-30T10:00:00Z", "2026-10-30T10:05:00Z")
	resp, err := http.Post(ts.URL+"/v1/nodes/"+nodeID.String()+"/channels/ch-1/download",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST download: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		out, _ := io.ReadAll(resp.Body)
		t.Fatalf("download = %d (%s)", resp.StatusCode, out)
	}
	if view.download.channel != "ch-1" || view.download.start.IsZero() {
		t.Errorf("download got %+v", view.download)
	}

	bad, err := http.Post(ts.URL+"/v1/nodes/"+nodeID.String()+"/channels/ch-1/download",
		"application/json", strings.NewReader(`{"start":"yesterday","end":"today"}`))
	if err != nil {
		t.Fatalf("POST bad download: %v", err)
	}
	defer func() { _ = bad.Body.Close() }()
	if bad.StatusCode != http.StatusBadRequest {
		t.Errorf("bad window = %d, want 400", bad.StatusCode)
	}
}
