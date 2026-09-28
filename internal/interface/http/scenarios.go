package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// ScenarioRunner is the scenario-engine capability the HTTP layer depends
// on. It aliases the domain port so the handlers stay thin.
type ScenarioRunner = port.ScenarioRunner

// handleScenarioList returns the loadable scenario package metas.
func (s *Server) handleScenarioList(c echo.Context) error {
	if s.scenarios == nil {
		return c.JSON(http.StatusNotImplemented, errorBody{
			Error: "scenario engine is not enabled in this build",
		})
	}
	metas := s.scenarios.List()
	if metas == nil {
		metas = []model.ScenarioMeta{}
	}
	return c.JSON(http.StatusOK, metas)
}

// handleScenarioRun synchronously executes the named scenario and returns
// the run report. An unknown name is 404 with the uniform error envelope.
func (s *Server) handleScenarioRun(c echo.Context) error {
	if s.scenarios == nil {
		return c.JSON(http.StatusNotImplemented, errorBody{
			Error: "scenario engine is not enabled in this build",
		})
	}
	var req scenarioRunRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid JSON body"})
	}
	if req.Name == "" {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "name is required"})
	}
	report, err := s.scenarios.Run(req.Name)
	if err != nil {
		return c.JSON(http.StatusNotFound, errorBody{Error: err.Error()})
	}
	return c.JSON(http.StatusOK, report)
}

// handleScenarioLastRun returns the most recent completed run report, or
// 404 when no scenario has been executed yet.
func (s *Server) handleScenarioLastRun(c echo.Context) error {
	if s.scenarios == nil {
		return c.JSON(http.StatusNotImplemented, errorBody{
			Error: "scenario engine is not enabled in this build",
		})
	}
	report, ok := s.scenarios.LastRun()
	if !ok {
		return c.JSON(http.StatusNotFound, errorBody{Error: "no scenario has been executed yet"})
	}
	return c.JSON(http.StatusOK, report)
}

// scenarioRunRequest is the POST /v1/scenarios/run body.
type scenarioRunRequest struct {
	Name string `json:"name"`
}
