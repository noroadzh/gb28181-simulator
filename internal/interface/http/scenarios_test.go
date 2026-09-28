package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

type fakeScenarioRunner struct {
	metas    []model.ScenarioMeta
	runErr   error
	lastOk   bool
	lastRun  model.RunReport
}

func (f *fakeScenarioRunner) List() []model.ScenarioMeta { return f.metas }
func (f *fakeScenarioRunner) Run(name string) (model.RunReport, error) {
	return model.RunReport{}, f.runErr
}
func (f *fakeScenarioRunner) LastRun() (model.RunReport, bool) { return f.lastRun, f.lastOk }

func TestScenarioHandlers(t *testing.T) {
	t.Run("list metas", func(t *testing.T) {
		s := &Server{
			scenarios: &fakeScenarioRunner{
				metas: []model.ScenarioMeta{
					{Name: "pkg", Description: "desc"},
				},
			},
		}
		e := echo.New()
		s.registerRoutes(e)

		req := httptest.NewRequest(http.MethodGet, "/v1/scenarios", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("run success", func(t *testing.T) {
		s := &Server{scenarios: &fakeScenarioRunner{}}
		e := echo.New()
		s.registerRoutes(e)

		body := `{"name":"pkg"}`
		req := httptest.NewRequest(http.MethodPost, "/v1/scenarios/run", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("run unknown name returns 404", func(t *testing.T) {
		s := &Server{
			scenarios: &fakeScenarioRunner{runErr: assert.AnError},
		}
		e := echo.New()
		s.registerRoutes(e)

		body := `{"name":"missing"}`
		req := httptest.NewRequest(http.MethodPost, "/v1/scenarios/run", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("last run none returns 404", func(t *testing.T) {
		s := &Server{
			scenarios: &fakeScenarioRunner{lastOk: false},
		}
		e := echo.New()
		s.registerRoutes(e)

		req := httptest.NewRequest(http.MethodGet, "/v1/scenarios/last-run", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}
