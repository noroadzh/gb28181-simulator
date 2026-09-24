package webui

import (
	"io/fs"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestEmbedContainsIndex(t *testing.T) {
	root := fs.FS(distFS)
	entries, err := fs.ReadDir(root, ".")
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		t.Logf("entry: %s", e.Name())
	}
	sub, err := fs.Sub(distFS, "embed/dist")
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	data, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		t.Fatalf("readfile index.html: %v", err)
	}
	if len(data) < 50 {
		t.Fatalf("index.html too small: %d", len(data))
	}
	if !strings.Contains(string(data), "gb28181-simulator") {
		t.Fatalf("index.html unexpected: %s", string(data))
	}
}

func TestSpaHandlerServesIndex(t *testing.T) {
	e := echo.New()
	e.GET("/*", SpaHandler())
	ts := httptest.NewServer(e)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("get /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("root status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("root content-type = %q", ct)
	}
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	if !strings.Contains(string(buf[:n]), "<div id=\"app\">") {
		t.Fatalf("root body does not look like the SPA shell: %s", string(buf[:n]))
	}

	// Asset
	resp2, err := ts.Client().Get(ts.URL + "/assets/index-6brgIjDG.css")
	if err != nil {
		t.Fatalf("get css: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("css status = %d", resp2.StatusCode)
	}
	if ct := resp2.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Fatalf("css content-type = %q", ct)
	}

	// SPA fallback
	resp3, err := ts.Client().Get(ts.URL + "/some/spa/route")
	if err != nil {
		t.Fatalf("get spa: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != 200 {
		t.Fatalf("spa fallback status = %d", resp3.StatusCode)
	}
	buf2 := make([]byte, 4096)
	n2, _ := resp3.Body.Read(buf2)
	if !strings.Contains(string(buf2[:n2]), "<div id=\"app\">") {
		t.Fatalf("spa fallback body not the shell: %s", string(buf2[:n2]))
	}
}