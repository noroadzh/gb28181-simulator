package webui

import (
	"io/fs"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// reAssetRef matches the first <script src> or <link href> that points into
// the `assets/` subtree. The filenames carry a content hash (e.g.
// index-XXXXXX.js / index-XXXXXX.css) that changes on every rebuild, so the
// test must discover them instead of hardcoding one.
var reAssetRef = regexp.MustCompile(`(?:src|href)="\.?/?(assets/[^"]+)"`)

func firstAssetRef(t *testing.T, index string) string {
	t.Helper()
	m := reAssetRef.FindStringSubmatch(index)
	if m == nil {
		t.Fatalf("index.html has no assets/ reference: %s", index)
	}
	return strings.TrimPrefix(m[1], "./")
}

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

	// Root
	resp, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("get /: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
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

	// Discover the hashed asset filename from the embedded index.html.
	sub, err := fs.Sub(distFS, "embed/dist")
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		t.Fatalf("read embedded index.html: %v", err)
	}
	assetPath := firstAssetRef(t, string(index))
	t.Logf("resolved asset path: %s", assetPath)

	// Asset
	resp2, err := ts.Client().Get(ts.URL + "/" + assetPath)
	if err != nil {
		t.Fatalf("get asset: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != 200 {
		t.Fatalf("asset status = %d", resp2.StatusCode)
	}
	if resp2.ContentLength == 0 {
		t.Fatalf("asset body is empty")
	}

	// SPA fallback
	resp3, err := ts.Client().Get(ts.URL + "/some/spa/route")
	if err != nil {
		t.Fatalf("get spa: %v", err)
	}
	defer func() { _ = resp3.Body.Close() }()
	if resp3.StatusCode != 200 {
		t.Fatalf("spa fallback status = %d", resp3.StatusCode)
	}
	buf2 := make([]byte, 4096)
	n2, _ := resp3.Body.Read(buf2)
	if !strings.Contains(string(buf2[:n2]), "<div id=\"app\">") {
		t.Fatalf("spa fallback body not the shell: %s", string(buf2[:n2]))
	}
}
