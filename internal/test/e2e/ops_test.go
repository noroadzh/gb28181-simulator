// End-to-end coverage for device-control-coverage.
//
// Change: device-control-coverage. Two chains: the operations HTTP surface on
// a live node, and a real file download from a device into the platform's
// recording library.

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestOps_StatusConfigArmingUpgrade(t *testing.T) {
	proc, channelID, _ := startLivePlatform(t)
	_ = channelID
	const device = e2eDeviceID
	const platform = livePlatformID

	// Device status.
	status, body := proc.call(t, http.MethodGet, "/v1/nodes/"+device+"/status", nil)
	if status != http.StatusOK {
		t.Fatalf("status endpoint = %d (body %v)", status, body)
	}
	if online, _ := body["online"].(bool); !online {
		t.Errorf("device reports offline: %v", body)
	}

	// Config read and write.
	status, body = proc.call(t, http.MethodGet, "/v1/nodes/"+device+"/config", nil)
	if status != http.StatusOK {
		t.Fatalf("config read = %d (body %v)", status, body)
	}
	status, body = proc.call(t, http.MethodPut, "/v1/nodes/"+device+"/config",
		map[string]any{"heartbeat_interval": 45})
	if status != http.StatusOK {
		t.Fatalf("config write = %d (body %v)", status, body)
	}
	status, body = proc.call(t, http.MethodGet, "/v1/nodes/"+device+"/config", nil)
	if status != http.StatusOK {
		t.Fatalf("config re-read = %d", status)
	}
	if got, _ := body["heartbeat_interval"].(float64); got != 45 {
		t.Errorf("heartbeat interval = %v, want 45", body["heartbeat_interval"])
	}

	// Arming on the platform, which answers for itself.
	status, body = proc.call(t, http.MethodPost,
		"/v1/nodes/"+platform+"/channels/"+channelID+"/alarm-mode", map[string]any{"mode": "armed"})
	if status != http.StatusOK {
		t.Fatalf("alarm-mode = %d (body %v)", status, body)
	}
	status, body = proc.call(t, http.MethodGet,
		"/v1/nodes/"+platform+"/channels/"+channelID+"/alarms", nil)
	if status != http.StatusOK {
		t.Fatalf("alarms = %d (body %v)", status, body)
	}

	// Upgrade and reboot keep the node running.
	status, body = proc.call(t, http.MethodPost, "/v1/nodes/"+device+"/upgrade",
		map[string]any{"version": "2.0.0"})
	if status != http.StatusOK {
		t.Fatalf("upgrade = %d (body %v)", status, body)
	}
	status, _ = proc.call(t, http.MethodPost, "/v1/nodes/"+device+"/reboot", nil)
	if status != http.StatusNoContent {
		t.Fatalf("reboot = %d", status)
	}
	status, body = proc.call(t, http.MethodGet, "/v1/nodes/"+device+"/status", nil)
	if status != http.StatusOK {
		t.Fatalf("status after upgrade = %d", status)
	}
	if online, _ := body["online"].(bool); !online {
		t.Error("upgrade or reboot took the device offline")
	}
}

func TestOps_DownloadWritesIntoTheLibrary(t *testing.T) {
	proc, channelID, _ := startLivePlatform(t)
	const platform = livePlatformID

	start := time.Now().Add(-time.Minute)
	end := time.Now().Add(20 * time.Second)
	status, body := proc.call(t, http.MethodPost,
		fmt.Sprintf("/v1/nodes/%s/channels/%s/download", platform, channelID),
		map[string]any{
			"start": start.Format(time.RFC3339),
			"end":   end.Format(time.RFC3339),
		})
	if status != http.StatusOK {
		t.Fatalf("download = %d (body %v)", status, body)
	}

	// The frames land under a download: channel key in the same library.
	key := "download:" + channelID
	deadline := time.Now().Add(20 * time.Second)
	for {
		resp, err := http.Get(proc.baseURL + "/v1/nodes/" + platform + "/channels/" + key + "/recordings")
		if err == nil {
			var got struct {
				Recordings []struct {
					Size int64 `json:"size"`
				} `json:"recordings"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&got)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				for _, rec := range got.Recordings {
					if rec.Size > 0 {
						return
					}
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the download produced no recording (last status %v)", status)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
