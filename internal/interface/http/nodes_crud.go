// Node definition CRUD over HTTP: the wire side of runtime node
// configuration. Create, update and cascade edit accept a JSON definition,
// hand it to NodeService as a StoredNode (which persists first and lives
// second), and answer with the live node plus the definition as stored.
// The delete path is deliberately a hard delete — a node removed here is
// gone from the store and the registry alike; restart will not resurrect it.
package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/your-org/gb28181-simulator/internal/app"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// registrationInput is the JSON shape of one registration row. Durations
// arrive as plain numbers (seconds for the heartbeat pair, milliseconds
// for the transaction timeout) because the API must stay typeable by hand
// — the same rule docs/configuration.md states for the YAML it replaces.
type registrationInput struct {
	Server               string `json:"server"`
	ServerID             string `json:"server_id"`
	Username             string `json:"username"`
	Password             string `json:"password"`
	GBVersion            string `json:"gb_version"`
	Expires              uint32 `json:"expires"`
	TimeoutMs            uint32 `json:"timeout_ms"`
	Transport            string `json:"transport"`
	AllowNoAuth          bool   `json:"allow_no_auth"`
	HeartbeatIntervalSec uint32 `json:"heartbeat_interval_sec"`
	HeartbeatTimeoutSec  uint32 `json:"heartbeat_timeout_sec"`
	HeartbeatMaxFailures uint32 `json:"heartbeat_max_failures"`
	OptionsEnabled       bool   `json:"options_enabled"`
}

// platformInput is the JSON shape of one platform-serving row. A zero
// expires triple means "use the documented defaults", exactly as
// model.NewExpiresPolicy does.
type platformInput struct {
	Realm                 string `json:"realm"`
	MinExpires            uint32 `json:"min_expires"`
	DefaultExpires        uint32 `json:"default_expires"`
	MaxExpires            uint32 `json:"max_expires"`
	AllowNoAuth           bool   `json:"allow_no_auth"`
	CatalogPollIntervalMs int    `json:"catalog_poll_interval_ms"`
}

// nodeCreateRequest is the POST /v1/nodes body. Registration and platform
// are optional: a bare device node needs neither, a node that dials
// upstream needs the first, a node that serves downstreams the second.
type nodeCreateRequest struct {
	ID              string   `json:"id"`
	Kind            string   `json:"kind"`
	Domain          string   `json:"domain"`
	Addr            string   `json:"addr"`
	Vendor          string   `json:"vendor"`
	CascadeParent   string   `json:"cascade_parent"`
	CascadeChildren []string `json:"cascade_children"`
	Registration    *registrationInput `json:"registration"`
	Platform        *platformInput     `json:"platform"`
}

// nodeUpdateRequest is the PUT /v1/nodes/:id body. Every field is a
// pointer (or slice) so a handler can tell "not provided, keep the stored
// value" from "provided empty, clear it". ID is read separately: the body
// may repeat it, but the path wins and a mismatch is rejected.
type nodeUpdateRequest struct {
	ID              *string            `json:"id,omitempty"`
	Kind            *string            `json:"kind,omitempty"`
	Domain          *string            `json:"domain,omitempty"`
	Addr            *string            `json:"addr,omitempty"`
	Vendor          *string            `json:"vendor,omitempty"`
	CascadeParent   *string            `json:"cascade_parent,omitempty"`
	CascadeChildren []string           `json:"cascade_children,omitempty"`
	Registration    *registrationInput `json:"registration,omitempty"`
	Platform        *platformInput     `json:"platform,omitempty"`
}

// cascadeEditRequest is the PUT /v1/nodes/:id/cascade body. parent is one
// node id or ""; children replaces the list wholesale.
type cascadeEditRequest struct {
	Parent   string   `json:"parent"`
	Children []string `json:"children"`
}

// nodeDetailResponse renders a node plus its stored definition. It embeds
// nodeResponse so the summary fields every dashboard already reads keep
// their names; the definition fields hang off the same object, because a
// create/update caller wants one round trip, not two.
type nodeDetailResponse struct {
	nodeResponse
	Domain          string                `json:"domain,omitempty"`
	Vendor          string                `json:"vendor,omitempty"`
	CascadeParent   string                `json:"cascade_parent,omitempty"`
	CascadeChildren []string              `json:"cascade_children"`
	Registration    *registrationInput    `json:"registration,omitempty"`
	Platform        *platformInput        `json:"platform,omitempty"`
	CatalogPollMs   int                   `json:"catalog_poll_interval_ms,omitempty"`
	CreatedAt       string                `json:"created_at,omitempty"`
	UpdatedAt       string                `json:"updated_at,omitempty"`
}

func (s *Server) handleNodeCreate(c echo.Context) error {
	if s.nodes == nil {
		return c.JSON(http.StatusServiceUnavailable, errorBody{Error: "node service unavailable"})
	}
	var req nodeCreateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid request body"})
	}
	sn, err := storedNodeFromCreate(req)
	if err != nil {
		return c.JSON(http.StatusBadRequest, nodeFieldError{Error: err.Error(), Field: nodeFieldOf(err)})
	}
	node, err := s.nodes.CreateNode(c.Request().Context(), sn)
	if err != nil {
		return s.nodeCRUDError(c, "create", sn.ID, err)
	}
	s.log.Info("node created over http", "node_id", node.ID().String(), "kind", node.Profile().Kind().String())
	return c.JSON(http.StatusCreated, newNodeDetailResponse(node, sn))
}

func (s *Server) handleNodeUpdate(c echo.Context) error {
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	var req nodeUpdateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid request body"})
	}
	if req.ID != nil {
		if body := strings.TrimSpace(*req.ID); body != "" && body != id.String() {
			return c.JSON(http.StatusBadRequest, nodeFieldError{
				Error: "id is immutable; path id wins", Field: "id",
			})
		}
	}
	ctx := c.Request().Context()
	current, ok := s.nodes.GetStoredNode(ctx, id)
	if !ok {
		return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
	}
	if err := applyNodeUpdate(&current, req); err != nil {
		return c.JSON(http.StatusBadRequest, nodeFieldError{Error: err.Error(), Field: nodeFieldOf(err)})
	}
	if err := s.nodes.UpdateNode(ctx, current); err != nil {
		return s.nodeCRUDError(c, "update", id, err)
	}
	node, _ := s.nodes.Get(ctx, id)
	s.log.Info("node updated over http", "node_id", id.String())
	return c.JSON(http.StatusOK, newNodeDetailResponse(node, current))
}

func (s *Server) handleNodeDelete(c echo.Context) error {
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	ctx := c.Request().Context()
	if err := s.nodes.DeleteNode(ctx, id); err != nil {
		return s.nodeCRUDError(c, "delete", id, err)
	}
	s.log.Info("node deleted over http", "node_id", id.String())
	return c.JSON(http.StatusOK, deletedNodeResponse{Status: "deleted", ID: id.String()})
}

func (s *Server) handleNodeCascade(c echo.Context) error {
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	var req cascadeEditRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid request body"})
	}
	ctx := c.Request().Context()
	if err := s.nodes.UpdateCascade(ctx, id, req.Parent, req.Children); err != nil {
		return s.nodeCRUDError(c, "cascade", id, err)
	}
	node, _ := s.nodes.Get(ctx, id)
	sn, _ := s.nodes.GetStoredNode(ctx, id)
	s.log.Info("node cascade edited over http", "node_id", id.String())
	return c.JSON(http.StatusOK, newNodeDetailResponse(node, sn))
}

// nodeFieldError names the offending field alongside the message, so a
// client can highlight the right form input instead of string-matching.
type nodeFieldError struct {
	Error string `json:"error"`
	Field string `json:"field,omitempty"`
}

type deletedNodeResponse struct {
	Status string `json:"status"`
	ID     string `json:"id"`
}

// nodeFieldOf picks the field a validation failure is about. The error
// chain from the model constructors names it in prose; the prefixes here
// cover the shapes the constructors actually produce, and the empty
// fallback ("definition") stays truthful when none match.
func nodeFieldOf(err error) string {
	switch {
	case strings.Contains(err.Error(), "device id"):
		return "id"
	case strings.Contains(err.Error(), "listen addr"):
		return "addr"
	case strings.Contains(err.Error(), "kind"):
		return "kind"
	case strings.Contains(err.Error(), "registration"):
		return "registration"
	case strings.Contains(err.Error(), "platform"):
		return "platform"
	default:
		return "definition"
	}
}

// storedNodeFromCreate validates the request into a StoredNode. Field
// errors come back as plain errors carrying the field name in prose; the
// HTTP layer translates them with nodeFieldOf.
func storedNodeFromCreate(req nodeCreateRequest) (model.StoredNode, error) {
	id, err := model.ParseNodeID(strings.TrimSpace(req.ID))
	if err != nil {
		return model.StoredNode{}, err
	}
	// The runtime derives a node's kind from its id's type code, so an
	// omitted kind simply falls back to that; an explicit one must agree
	// or it would be stored and then silently ignored.
	kind := id.Kind()
	if strings.TrimSpace(req.Kind) != "" {
		if kind, err = model.ParseNodeKind(strings.TrimSpace(req.Kind)); err != nil {
			return model.StoredNode{}, err
		}
		if err := model.CheckNodeKindConsistency(id, kind); err != nil {
			return model.StoredNode{}, err
		}
	}
	domain := strings.TrimSpace(req.Domain)
	if domain == "" {
		// A node with no declared domain lives in its own id's domain —
		// the same rule the YAML seeding path applies.
		domain = domainFromID(id.String())
	}
	if strings.TrimSpace(req.Addr) == "" {
		return model.StoredNode{}, errors.New("addr is required: where the node listens for SIP")
	}
	sn := model.StoredNode{
		ID:              id,
		Kind:            kind,
		Domain:          domain,
		Addr:            strings.TrimSpace(req.Addr),
		Vendor:          strings.TrimSpace(req.Vendor),
		CascadeParent:   strings.TrimSpace(req.CascadeParent),
		CascadeChildren: normalizeChildren(req.CascadeChildren),
	}
	if req.Registration != nil {
		reg, err := registrationFromInput(*req.Registration)
		if err != nil {
			return model.StoredNode{}, err
		}
		sn.Registration, sn.HasRegistration = reg, true
	}
	if req.Platform != nil {
		ps, err := platformFromInput(*req.Platform, domain)
		if err != nil {
			return model.StoredNode{}, err
		}
		sn.Platform, sn.HasPlatform = ps, true
	}
	sn.CatalogPollIntervalMs = platformPollOf(req.Platform)
	return sn, nil
}

// applyNodeUpdate overlays the provided fields onto the stored row. Fields
// the request omits keep their stored values, so a caller editing only the
// address cannot accidentally drop the registration.
func applyNodeUpdate(sn *model.StoredNode, req nodeUpdateRequest) error {
	if req.Kind != nil {
		kind, err := model.ParseNodeKind(strings.TrimSpace(*req.Kind))
		if err != nil {
			return err
		}
		if err := model.CheckNodeKindConsistency(sn.ID, kind); err != nil {
			return err
		}
		sn.Kind = kind
	}
	if req.Domain != nil {
		d := strings.TrimSpace(*req.Domain)
		if d != "" {
			sn.Domain = d
		}
	}
	if req.Addr != nil {
		a := strings.TrimSpace(*req.Addr)
		if a == "" {
			return errors.New("addr cannot be cleared: where the node listens for SIP")
		}
		sn.Addr = a
	}
	if req.Vendor != nil {
		sn.Vendor = strings.TrimSpace(*req.Vendor)
	}
	if req.CascadeParent != nil {
		sn.CascadeParent = strings.TrimSpace(*req.CascadeParent)
	}
	if req.CascadeChildren != nil {
		sn.CascadeChildren = normalizeChildren(req.CascadeChildren)
	}
	if req.Registration != nil {
		reg, err := registrationFromInput(*req.Registration)
		if err != nil {
			return err
		}
		sn.Registration, sn.HasRegistration = reg, true
	}
	if req.Platform != nil {
		ps, err := platformFromInput(*req.Platform, sn.Domain)
		if err != nil {
			return err
		}
		sn.Platform, sn.HasPlatform = ps, true
		sn.CatalogPollIntervalMs = req.Platform.CatalogPollIntervalMs
	}
	return nil
}

// registrationFromInput builds the model value; an absent password needs
// the no-auth flag, which NewRegistration enforces.
func registrationFromInput(in registrationInput) (model.Registration, error) {
	reg, err := model.NewRegistration(model.RegistrationParams{
		Server:               strings.TrimSpace(in.Server),
		ServerID:             strings.TrimSpace(in.ServerID),
		Username:             strings.TrimSpace(in.Username),
		Password:             in.Password,
		GBVersion:            strings.TrimSpace(in.GBVersion),
		Expires:              in.Expires,
		Timeout:              time.Duration(in.TimeoutMs) * time.Millisecond,
		Transport:            strings.TrimSpace(in.Transport),
		HeartbeatInterval:    time.Duration(in.HeartbeatIntervalSec) * time.Second,
		HeartbeatTimeout:     time.Duration(in.HeartbeatTimeoutSec) * time.Second,
		HeartbeatMaxFailures: in.HeartbeatMaxFailures,
		OptionsEnabled:       in.OptionsEnabled,
		AllowNoAuth:          in.AllowNoAuth,
	})
	if err != nil {
		return model.Registration{}, err
	}
	return reg, nil
}

// platformFromInput builds the serving value; realm falls back to the
// node's domain, matching DefaultPlatformServing on the YAML path.
func platformFromInput(in platformInput, domain string) (model.PlatformServing, error) {
	realm := strings.TrimSpace(in.Realm)
	if realm == "" {
		realm = domain
	}
	policy, err := model.NewExpiresPolicy(in.MinExpires, in.DefaultExpires, in.MaxExpires)
	if err != nil {
		return model.PlatformServing{}, err
	}
	ps, err := model.NewPlatformServingWithNoAuth(realm, policy, in.AllowNoAuth)
	if err != nil {
		return model.PlatformServing{}, err
	}
	return ps, nil
}

func platformPollOf(in *platformInput) int {
	if in == nil {
		return 0
	}
	return in.CatalogPollIntervalMs
}

// normalizeChildren trims, drops empties and de-duplicates while keeping
// order — a repeated id in the children list is a client typo, not a
// topology.
func normalizeChildren(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, c := range in {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// domainFromID derives the home domain from a 20-digit GB id (digits 1-8).
func domainFromID(id string) string {
	if len(id) >= 8 {
		return id[:8] + "0000"
	}
	return id
}

// newNodeDetailResponse renders the live node together with its stored
// definition. Registration is rendered without the password — the API
// never echoes a secret back; the redaction rules docs/logging.md states
// for logs apply to responses too.
func newNodeDetailResponse(n model.Node, sn model.StoredNode) nodeDetailResponse {
	resp := nodeDetailResponse{
		nodeResponse:    newNodeResponse(n, nil),
		Domain:          sn.Domain,
		Vendor:          sn.Vendor,
		CascadeParent:   sn.CascadeParent,
		CascadeChildren: sn.CascadeChildren,
		CatalogPollMs:   sn.CatalogPollIntervalMs,
	}
	if resp.CascadeChildren == nil {
		resp.CascadeChildren = []string{}
	}
	if sn.HasRegistration {
		r := sn.Registration
		resp.Registration = &registrationInput{
			Server:               r.Server(),
			ServerID:             r.ServerID(),
			Username:             r.Username(),
			GBVersion:            r.GBVersion(),
			Expires:              r.Expires(),
			TimeoutMs:            uint32(r.Timeout() / time.Millisecond),
			Transport:            r.Transport(),
			AllowNoAuth:          r.AllowNoAuth(),
			HeartbeatIntervalSec: uint32(r.HeartbeatInterval() / time.Second),
			HeartbeatTimeoutSec:  uint32(r.HeartbeatTimeout() / time.Second),
			HeartbeatMaxFailures: r.MaxHeartbeatFailures(),
			OptionsEnabled:       r.OptionsEnabled(),
		}
	}
	if sn.HasPlatform {
		p := sn.Platform
		resp.Platform = &platformInput{
			Realm:       p.Realm(),
			MinExpires:  p.Policy().Min(),
			DefaultExpires: p.Policy().Default(),
			MaxExpires:  p.Policy().Max(),
			AllowNoAuth: p.AllowNoAuth(),
		}
	}
	if !sn.CreatedAt.IsZero() {
		resp.CreatedAt = sn.CreatedAt.UTC().Format(time.RFC3339)
	}
	if !sn.UpdatedAt.IsZero() {
		resp.UpdatedAt = sn.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return resp
}

// nodeCRUDError maps a service-layer failure to its HTTP answer. The four
// cases are the ones the API contract names; everything else is a server
// fault and keeps the raw message for the log the user will grep.
func (s *Server) nodeCRUDError(c echo.Context, action string, id model.NodeID, err error) error {
	ctx := c.Request().Context()
	status := func() string {
		if n, ok := s.nodes.Get(ctx, id); ok {
			return n.Status().String()
		}
		return "unknown"
	}
	if errors.Is(err, port.ErrNodeNotFound) {
		s.log.Warn("node crud on unknown node", "action", action, "node_id", id.String())
		return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
	}
	switch {
	case errors.Is(err, app.ErrNodeNotStoppable):
		s.log.Warn("node crud refused: node not quiescent",
			"action", action, "node_id", id.String(), "status", status())
		return c.JSON(http.StatusConflict, nodeError{
			Error: "node must be stopped before this operation",
			Status: status(), Action: action,
		})
	case errors.Is(err, app.ErrCascadeCycle):
		s.log.Warn("node cascade refused: cycle", "action", action, "node_id", id.String())
		return c.JSON(http.StatusConflict, nodeError{
			Error: "cascade change would create a cycle",
			Status: status(), Action: action,
		})
	case errors.Is(err, port.ErrNodeAlreadyExists):
		s.log.Warn("node create refused: already exists", "action", action, "node_id", id.String())
		return c.JSON(http.StatusConflict, nodeError{
			Error: "node already exists: " + id.String(),
			Status: status(), Action: action,
		})
	case errors.Is(err, port.ErrAddrClaimed):
		s.log.Warn("node create refused: address in use", "action", action, "node_id", id.String())
		return c.JSON(http.StatusConflict, nodeError{
			Error: "listen address already claimed by another node: " + err.Error(),
			Status: status(), Action: action,
		})
	}
	s.log.Error("node crud failed", "action", action, "node_id", id.String(), "error", err.Error())
	return c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error()})
}
