// Package httpapi wires the Echo HTTP server plus the WebSocket log stream and
// serves the embedded Dashboard (see internal/interface/webui). This package
// registers only the health / version / logs-stream routes; GB28181 routes
// arrive in Change 2+.
package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/your-org/gb28181-simulator/internal/interface/webui"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
)

// Version metadata is injected by the CLI entrypoint (ldflags / defaults).
type Version struct {
	Version string
	Commit  string
	BuiltAt string
}

// Server is the composition root for all HTTP and WebSocket endpoints.
type Server struct {
	cfg   platformconfig.Config
	hub   *logging.Hub
	ver   Version
	nodes NodeView
	echo  *echo.Echo
	http  *http.Server
	lnErr error
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Real UI runs same-origin; tighten in Change 12 if TLS is enabled.
	CheckOrigin: func(_ *http.Request) bool { return true },
}

// NewServer returns a configured but not-yet-started Echo server. nodes may
// be nil, in which case the /v1/nodes endpoints report an empty inventory
// (the process was started without any configured node).
func NewServer(cfg platformconfig.Config, hub *logging.Hub, ver Version, nodes NodeView) *Server {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.Use(middleware.Recover())
	e.Use(middleware.RequestID())

	// Minimal CORS for future Vite dev server. Change 14 may restrict this.
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodOptions},
	}))

	s := &Server{
		cfg:   cfg,
		hub:   hub,
		ver:   ver,
		nodes: nodes,
		echo:  e,
	}
	s.registerRoutes(e)
	return s
}

func (s *Server) registerRoutes(e *echo.Echo) {
	// v1 API routes
	e.GET("/v1/health", s.handleHealth)
	e.GET("/v1/version", s.handleVersion)
	e.GET("/v1/logs/stream", WSHandler(s.hub))

	// Node inventory and per-node control (Change 4). These are registered
	// unconditionally: with no nodes they simply report an empty list.
	e.GET("/v1/nodes", s.handleNodeList)
	e.GET("/v1/nodes/:id", s.handleNodeDetail)
	e.POST("/v1/nodes/:id/start", s.handleNodeStart)
	e.POST("/v1/nodes/:id/stop", s.handleNodeStop)
	e.POST("/v1/nodes/:id/unregister", s.handleNodeUnregister)
	e.GET("/v1/nodes/:id/devices", s.handleNodeDevices)
	e.GET("/v1/nodes/:id/devices/:deviceID", s.handleNodeDevice)

	// Legacy /healthz and /metrics for smoke tests (per §7.3)
	e.GET("/healthz", s.handleHealth)
	e.GET("/metrics", s.handleMetrics)

	// Embedded Dashboard: / → index.html, /assets/... → static, fallback to
	// index.html for SPA hash-less routes.
	e.GET("/*", webui.SpaHandler())
}

func (s *Server) handleHealth(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMetrics(c echo.Context) error {
	// Basic metrics placeholder - can be extended with actual metrics
	return c.JSON(http.StatusOK, map[string]interface{}{
		"uptime":   "ok",
		"requests": 0,
	})
}

func (s *Server) handleVersion(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"version":    s.ver.Version,
		"commit":     s.ver.Commit,
		"go_version": runtimeVersion(),
		"platform":   runtimePlatform(),
	})
}

// Echo returns the underlying Echo instance for testing.
func (s *Server) Echo() *echo.Echo {
	return s.echo
}

// Run starts blocking HTTP serving on the configured address. It returns
// http.ErrServerClosed once Shutdown has been initiated.
func (s *Server) Run(addr string) error {
	s.http = &http.Server{
		Addr:              addr,
		ReadHeaderTimeout: 5 * time.Second,
	}
	// echo.StartServer owns the listener lifecycle.
	return s.echo.StartServer(s.http)
}

// Shutdown drains in-flight requests and closes the listener. It is safe to
// call after Run returns ErrServerClosed.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.echo == nil {
		return errors.New("server: not initialised")
	}
	return s.echo.Shutdown(ctx)
}

// WSHandler upgrades the request to a WebSocket and pumps the LogHub's
// subscriber channel onto it. Slow clients are handled by the hub's
// non-blocking publish; this handler also detects client disconnects.
func WSHandler(hub *logging.Hub) echo.HandlerFunc {
	if hub == nil {
		hub = logging.DefaultHub()
	}
	return func(c echo.Context) error {
		conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
		if err != nil {
			return err
		}
		defer conn.Close()

		sub := hub.Subscribe()
		defer hub.Unsubscribe(sub)

		// Read loop only to detect client close; messages from clients are
		// ignored for now.
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()

		for {
			select {
			case <-done:
				return nil
			case payload, ok := <-sub.Chan():
				if !ok {
					return nil
				}
				if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
					return nil
				}
			}
		}
	}
}
