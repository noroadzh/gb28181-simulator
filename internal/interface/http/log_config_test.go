package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
)

// setupLogConfigTest initialises the logging package with a fresh handler and
// returns a Server whose handlePatchLogConfig can be called directly. Every
// test resets the global state on cleanup so they are fully independent.
//
// The package-level state in internal/platform/observability/logging is a
// process-wide singleton, so callers MUST NOT run these tests in parallel.
// Without that constraint the second test's Init would tear down the first
// test's handler mid-call, and UpdateLevels would return the well-known
// "logger not initialised" error.
func setupLogConfigTest(t *testing.T) (*Server, func()) {
	t.Helper()
	logging.Reset()
	if err := logging.Init(logging.Options{Level: logging.LevelInfo}); err != nil {
		t.Fatalf("logging.Init: %v", err)
	}
	hub := logging.DefaultHub()
	s := &Server{hub: hub, log: logging.L()}
	cleanup := func() {
		logging.Reset()
	}
	return s, cleanup
}

// makePatch builds a PATCH request with the given JSON body.
func makePatch(t *testing.T, body any) *http.Request {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/v1/config/log", bytes.NewReader(b))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	return req
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, want, rec.Body.String())
	}
}

func assertKeyValue(t *testing.T, body []byte, key, want string) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, ok := m[key].(string); !ok || got != want {
		t.Errorf("%s = %q, want %q", key, got, want)
	}
}

// --- 200 OK paths ---

func TestPatchLogConfig_LevelOnly(t *testing.T) {
	s, cleanup := setupLogConfigTest(t)
	defer cleanup()

	rec := httptest.NewRecorder()
	c := echo.New().NewContext(makePatch(t, map[string]any{"level": "debug"}), rec)
	if err := s.handlePatchLogConfig(c); err != nil {
		t.Fatalf("handlePatchLogConfig: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
	assertKeyValue(t, rec.Body.Bytes(), "level", "debug")
}

func TestPatchLogConfig_ModulesOnly(t *testing.T) {
	s, cleanup := setupLogConfigTest(t)
	defer cleanup()

	rec := httptest.NewRecorder()
	c := echo.New().NewContext(makePatch(t, map[string]any{
		"modules": map[string]any{"internal/app": "trace", "internal/adapter/cascade": "warn"},
	}), rec)
	if err := s.handlePatchLogConfig(c); err != nil {
		t.Fatalf("handlePatchLogConfig: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
	var resp logConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp.Level != "info" {
		t.Errorf("default level = %q, want info", resp.Level)
	}
	if resp.Modules["internal/app"] != "trace" {
		t.Errorf("modules[internal/app] = %q, want trace", resp.Modules["internal/app"])
	}
	if resp.Modules["internal/adapter/cascade"] != "warn" {
		t.Errorf("modules[internal/adapter/cascade] = %q, want warn", resp.Modules["internal/adapter/cascade"])
	}
}

func TestPatchLogConfig_LevelAndModules(t *testing.T) {
	s, cleanup := setupLogConfigTest(t)
	defer cleanup()

	rec := httptest.NewRecorder()
	c := echo.New().NewContext(makePatch(t, map[string]any{
		"level":   "error",
		"modules": map[string]any{"internal/app": "debug"},
	}), rec)
	if err := s.handlePatchLogConfig(c); err != nil {
		t.Fatalf("handlePatchLogConfig: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
	var resp logConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp.Level != "error" {
		t.Errorf("level = %q, want error", resp.Level)
	}
	if resp.Modules["internal/app"] != "debug" {
		t.Errorf("modules[internal/app] = %q, want debug", resp.Modules["internal/app"])
	}
}

// --- 400 Bad Request paths ---

func TestPatchLogConfig_EmptyBody(t *testing.T) {
	s, cleanup := setupLogConfigTest(t)
	defer cleanup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/v1/config/log", bytes.NewReader([]byte("{}")))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	c := echo.New().NewContext(req, rec)
	if err := s.handlePatchLogConfig(c); err != nil {
		t.Fatalf("handlePatchLogConfig: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPatchLogConfig_InvalidLevel(t *testing.T) {
	s, cleanup := setupLogConfigTest(t)
	defer cleanup()

	rec := httptest.NewRecorder()
	c := echo.New().NewContext(makePatch(t, map[string]any{"level": "verbose"}), rec)
	if err := s.handlePatchLogConfig(c); err != nil {
		t.Fatalf("handlePatchLogConfig: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPatchLogConfig_InvalidModuleLevel(t *testing.T) {
	s, cleanup := setupLogConfigTest(t)
	defer cleanup()

	rec := httptest.NewRecorder()
	c := echo.New().NewContext(makePatch(t, map[string]any{
		"modules": map[string]any{"internal/app": "notice"},
	}), rec)
	if err := s.handlePatchLogConfig(c); err != nil {
		t.Fatalf("handlePatchLogConfig: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPatchLogConfig_InvalidJSON(t *testing.T) {
	s, cleanup := setupLogConfigTest(t)
	defer cleanup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/v1/config/log", bytes.NewReader([]byte("not json")))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	c := echo.New().NewContext(req, rec)
	if err := s.handlePatchLogConfig(c); err != nil {
		t.Fatalf("handlePatchLogConfig: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

// --- Empty module value falls back to info (documented behaviour) ---

func TestPatchLogConfig_EmptyModuleStringIsInfo(t *testing.T) {
	s, cleanup := setupLogConfigTest(t)
	defer cleanup()

	// Empty string for a module is treated as "use default" (info). The
	// module stays in the override map but at the default severity. This
	// keeps the handler idempotent: an operator can PATCH {"modules": {...}}
	// without first computing which entries to remove.
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(makePatch(t, map[string]any{
		"modules": map[string]any{"internal/app": ""},
	}), rec)
	if err := s.handlePatchLogConfig(c); err != nil {
		t.Fatalf("handlePatchLogConfig: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
	var resp logConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp.Modules["internal/app"] != "info" {
		t.Errorf("modules[internal/app] = %q, want info", resp.Modules["internal/app"])
	}
}

// --- Concurrent safety ---

func TestPatchLogConfig_ConcurrentUpdates(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping race-sensitive test in -short mode")
	}
	s, cleanup := setupLogConfigTest(t)
	defer cleanup()

	var wg sync.WaitGroup
	levels := []string{"debug", "info", "warn", "error", "trace"}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			body := map[string]any{
				"level": levels[idx%len(levels)],
				"modules": map[string]any{
					"internal/app": levels[(idx+1)%len(levels)],
				},
			}
			c := echo.New().NewContext(makePatch(t, body), rec)
			_ = s.handlePatchLogConfig(c)
		}(i)
	}
	wg.Wait()
	// No crash means the handler is safe to call concurrently.
}

// --- Effective level echoed correctly ---

func TestPatchLogConfig_EffectiveLevelCanonical(t *testing.T) {
	s, cleanup := setupLogConfigTest(t)
	defer cleanup()

	// Set level to "INFO" (uppercase) — the response must canonicalise to "info".
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(makePatch(t, map[string]any{"level": "INFO"}), rec)
	if err := s.handlePatchLogConfig(c); err != nil {
		t.Fatalf("handlePatchLogConfig: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
	assertKeyValue(t, rec.Body.Bytes(), "level", "info")
}
