package httpapi

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
)

// logConfigRequest is the JSON body accepted by PATCH /v1/config/log.
//
// Either field is optional; at least one of `level` or `modules` must be
// supplied. Empty strings in `modules` reset that module to the default level.
//
// Example body:
//
//	{"level":"debug","modules":{"internal/app":"debug","internal/adapter/media":"trace"}}
type logConfigRequest struct {
	Level   string            `json:"level,omitempty"`
	Modules map[string]string `json:"modules,omitempty"`
}

// logConfigResponse echoes the values that were applied so the operator can
// confirm what landed in the running process. Restart will revert to the
// values declared in `file.conf` (PATCH is intentionally non-persistent).
type logConfigResponse struct {
	Level   string            `json:"level"`
	Modules map[string]string `json:"modules"`
}

// handlePatchLogConfig updates the in-memory logging levels at runtime. The
// change is not persisted to disk; restarting the process restores whatever
// `log.level` and `log.modules` were configured at startup.
//
// On invalid input (unknown level name, unparseable module map) the handler
// returns 400 with the parse error.
func (s *Server) handlePatchLogConfig(c echo.Context) error {
	var req logConfigRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("invalid request body: %v", err),
		})
	}

	if req.Level == "" && len(req.Modules) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "request must include `level` and/or `modules`",
		})
	}

	// Resolve to internal types; reject unknown level names here so the
	// handler fails fast before mutating global state.
	level := logging.LevelUnset
	if req.Level != "" {
		l := logging.ParseLevel(req.Level)
		// ParseLevel returns info for unknown input; detect that explicitly.
		if l == logging.LevelInfo && !strings.EqualFold(req.Level, string(logging.LevelInfo)) && req.Level != "" {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": fmt.Sprintf("invalid level %q: must be one of trace|debug|info|warn|error", req.Level),
			})
		}
		level = l
	}

	modules := make(map[string]logging.Level, len(req.Modules))
	for k, v := range req.Modules {
		l := logging.ParseLevel(v)
		if l == logging.LevelInfo && !strings.EqualFold(v, string(logging.LevelInfo)) && v != "" {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": fmt.Sprintf("invalid level for module %q: must be one of trace|debug|info|warn|error", k),
			})
		}
		modules[k] = l
	}

	if err := logging.UpdateLevels(level, modules); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
	}

	// Build the echo: report the resolved levels, not the raw strings, so
	// clients see canonical names (e.g. "Info" vs "INFO").
	effective := logging.CurrentLevels()
	modOut := make(map[string]string, len(effective.Modules))
	for k, v := range effective.Modules {
		modOut[k] = v.String()
	}
	resp := logConfigResponse{
		Level:   effective.Default.String(),
		Modules: modOut,
	}
	s.log.Info("log config updated via API",
		"requested_level", req.Level,
		"requested_modules", req.Modules,
		"effective_level", resp.Level,
	)
	return c.JSON(http.StatusOK, resp)
}
