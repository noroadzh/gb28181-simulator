// Package webui exposes the embedded Vite-built Dashboard.
//
// The actual dist files are produced by `npm --prefix web run build` (see
// Makefile) and live in internal/interface/webui/embed/dist/. They are
// tracked in git so that `go test`, `golangci-lint` and the production
// binary can all typecheck without first running the Node build. The
// //go:embed directive embeds the whole subtree; a `make clean` that drops
// the directory will make the package fail to compile — run `make web`
// to restore it.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/labstack/echo/v4"
)

//go:embed all:embed/dist
var distFS embed.FS

// FS returns the embedded asset filesystem. Callers must not retain references
// to the returned FS across calls to Build (we currently never mutate it).
func FS() fs.FS {
	return distFS
}

// SpaHandler returns an Echo handler serving the embedded Dashboard. The
// root returns /index.html; any other non-API path falls back to index.html
// so client-side hash routing works without server config.
func SpaHandler() echo.HandlerFunc {
	// //go:embed all:embed/dist roots the FS at the "embed" directory; sub to
	// "embed/dist" so request paths resolve directly (e.g. "index.html",
	// "assets/x.js"). Falls back to the root FS when the dist sub-tree is
	// absent (developer bring-up before `make web`).
	sub, subErr := fs.Sub(distFS, "embed/dist")
	if subErr != nil {
		sub = distFS
	}
	indexBytes, _ := fs.ReadFile(sub, "index.html")
	hasIndex := len(indexBytes) > 0

	return func(c echo.Context) error {
		reqPath := strings.TrimPrefix(c.Request().URL.Path, "/")
		if reqPath == "" {
			reqPath = "index.html"
		}
		if data, err := fs.ReadFile(sub, reqPath); err == nil {
			contentType := mimeByExt(path.Ext(reqPath))
			return c.Blob(http.StatusOK, contentType, data)
		}
		// Fallback to index.html for SPA routing.
		if hasIndex {
			return c.Blob(http.StatusOK, "text/html; charset=utf-8", indexBytes)
		}
		// Cold start without a build.
		return c.HTML(http.StatusOK, `<!doctype html><meta charset="utf-8"><title>gb28181-simulator</title><body><h1>Web UI not built</h1><p>Run <code>make web</code> then rebuild.</p></body>`)
	}
}

func mimeByExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "application/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".ico":
		return "image/x-icon"
	case ".woff2":
		return "font/woff2"
	default:
		return "application/octet-stream"
	}
}
