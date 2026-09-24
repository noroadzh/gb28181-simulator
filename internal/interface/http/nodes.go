package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// NodeView is the slice of node operations the HTTP layer needs. Depending
// on a narrow interface (rather than on *app.NodeService) keeps this package
// testable and keeps the dependency pointed at the domain.
type NodeView interface {
	List(ctx context.Context) []model.Node
	Get(ctx context.Context, id model.NodeID) (model.Node, bool)
	Start(ctx context.Context, id model.NodeID) error
	Stop(ctx context.Context, id model.NodeID) error
	Unregister(ctx context.Context, id model.NodeID) error
}

// nodeResponse is the JSON shape of a node. Deliberately flat and
// lower-case: it is the contract the future Web UI (Change 14) will bind to.
type nodeResponse struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Addr   string `json:"addr"`
}

// errorBody is the uniform JSON error envelope for the node endpoints.
type errorBody struct {
	Error string `json:"error"`
}

// nodeError adds the node's current status to a conflict response so a
// caller can see what the request collided with. Stage is set when the
// failure happened inside a multi-step operation (e.g. a registration
// transaction) and names the step, so the response says more than "it
// failed".
type nodeError struct {
	Error  string `json:"error"`
	Status string `json:"status"`
	Action string `json:"action"`
	Stage  string `json:"stage,omitempty"`
}

func newNodeResponse(n model.Node) nodeResponse {
	return nodeResponse{
		ID:     n.ID().String(),
		Kind:   n.Profile().Kind().String(),
		Status: n.Status().String(),
		Addr:   n.Profile().Addr(),
	}
}

// handleNodeList returns every registered node; with zero nodes it returns
// an empty JSON array, never null.
func (s *Server) handleNodeList(c echo.Context) error {
	if s.nodes == nil {
		return c.JSON(http.StatusOK, []nodeResponse{})
	}
	nodes := s.nodes.List(c.Request().Context())
	out := make([]nodeResponse, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, newNodeResponse(n))
	}
	return c.JSON(http.StatusOK, out)
}

// handleNodeDetail returns one node, or 404 with a JSON error body.
func (s *Server) handleNodeDetail(c echo.Context) error {
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	n, ok := s.nodes.Get(c.Request().Context(), id)
	if !ok {
		return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
	}
	return c.JSON(http.StatusOK, newNodeResponse(n))
}

// handleNodeStart starts one node. An unknown id is 404; an illegal status
// transition is 409 and reports the status the node is actually in.
func (s *Server) handleNodeStart(c echo.Context) error {
	return s.controlNode(c, "start", NodeView.Start)
}

// handleNodeStop stops one node, with the same status mapping as start.
func (s *Server) handleNodeStop(c echo.Context) error {
	return s.controlNode(c, "stop", NodeView.Stop)
}

// handleNodeUnregister asks the platform to forget one node. It shares the
// start/stop mapping, so a refused unregistration is reported as a bad
// gateway naming the stage, and unregistering a node that is not online is a
// conflict.
func (s *Server) handleNodeUnregister(c echo.Context) error {
	return s.controlNode(c, "unregister", NodeView.Unregister)
}

// controlNode is the shared body of the start/stop endpoints.
func (s *Server) controlNode(
	c echo.Context,
	action string,
	op func(NodeView, context.Context, model.NodeID) error,
) error {
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	ctx := c.Request().Context()
	if _, ok := s.nodes.Get(ctx, id); !ok {
		return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
	}
	if err := op(s.nodes, ctx, id); err != nil {
		// A failure inside a multi-step operation (a registration that was
		// refused or timed out) is the peer's answer, not a bug in the
		// simulator: report it as bad gateway and name the stage.
		var staged port.StagedFailure
		if errors.As(err, &staged) {
			current := "unknown"
			if n, ok := s.nodes.Get(ctx, id); ok {
				current = n.Status().String()
			}
			return c.JSON(http.StatusBadGateway, nodeError{
				Error:  err.Error(),
				Status: current,
				Action: action,
				Stage:  staged.FailureStage(),
			})
		}
		// A node that exists but refuses the transition is a conflict, not a
		// server error (design D8).
		if errors.Is(err, model.ErrIllegalTransition) {
			current := "unknown"
			if n, ok := s.nodes.Get(ctx, id); ok {
				current = n.Status().String()
			}
			return c.JSON(http.StatusConflict, nodeError{
				Error:  err.Error(),
				Status: current,
				Action: action,
			})
		}
		return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
	after, _ := s.nodes.Get(ctx, id)
	return c.JSON(http.StatusOK, newNodeResponse(after))
}

// nodeIDParam parses the {id} path parameter into a validated NodeID. A
// malformed id is rejected before any lookup so the error can name the
// syntax problem.
func (s *Server) nodeIDParam(c echo.Context) (model.NodeID, error) {
	return model.ParseNodeID(c.Param("id"))
}
