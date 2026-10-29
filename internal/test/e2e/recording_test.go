// End-to-end coverage for platform recording.
//
// Change: platform-recording. A pull is recorded as it runs; the library is
// queryable over HTTP and the segment plays back as FLV. The assertion is the
// FLV magic on the playback body, not the 200.

package e2e

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestPlatformRecording_QueryAndPlayback(t *testing.T) {
	proc, channelID, _ := startLivePlatform(t)
	const platformID = livePlatformID

	status, body := proc.call(t, http.MethodPost,
		"/v1/nodes/"+platformID+"/channels/"+channelID+"/live", nil)
	if status != http.StatusOK {
		t.Fatalf("live start = %d, want 200 (body %v)", status, body)
	}
	t.Cleanup(func() {
		_, _ = proc.call(t, http.MethodPost,
			"/v1/nodes/"+platformID+"/channels/"+channelID+"/live/stop", nil)
	})

	// Wait for the pull to produce a segment.
	var recordingID string
	deadline := time.Now().Add(20 * time.Second)
	for recordingID == "" {
		resp, err := http.Get(proc.baseURL + "/v1/nodes/" + platformID +
			"/channels/" + channelID + "/recordings")
		if err != nil {
			t.Fatalf("GET recordings: %v", err)
		}
		var got struct {
			Recordings []struct {
				ID   string `json:"id"`
				Size int64  `json:"size"`
			} `json:"recordings"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&got)
		_ = resp.Body.Close()
		for _, rec := range got.Recordings {
			if rec.Size > 0 {
				recordingID = rec.ID
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no recording appeared after the pull started (body %+v)", got)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Play the segment back as FLV.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	playbackURL := proc.baseURL + "/v1/nodes/" + platformID + "/channels/" + channelID +
		"/recordings/" + recordingID + "/flv"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, playbackURL, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET recording flv: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("recording flv = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "video/x-flv" {
		t.Errorf("content type = %q, want video/x-flv", ct)
	}
	head := make([]byte, 3)
	if _, err := io.ReadFull(resp.Body, head); err != nil {
		t.Fatalf("read FLV header: %v", err)
	}
	if string(head) != "FLV" {
		t.Errorf("body starts with %q, want FLV", head)
	}

	// An unknown recording is 404, not an empty 200.
	missing := proc.baseURL + "/v1/nodes/" + platformID + "/channels/" + channelID +
		"/recordings/" + url.PathEscape("does-not-exist") + "/flv"
	resp2, err := http.Get(missing)
	if err != nil {
		t.Fatalf("GET missing recording: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("unknown recording = %d, want 404", resp2.StatusCode)
	}
}
