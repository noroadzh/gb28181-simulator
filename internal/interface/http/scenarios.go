package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// handleScenarioRun is the Change 14 placeholder for the YAML scenario
// runner. The engine itself ships with Change 15; until then the endpoint
// answers 501 with an explicit pointer so the web UI can surface the
// limitation instead of failing silently.
func (s *Server) handleScenarioRun(c echo.Context) error {
	return c.JSON(http.StatusNotImplemented, errorBody{
		Error: "scenario execution is provided by scenario-engine (Change #15) and is not implemented yet",
	})
}
