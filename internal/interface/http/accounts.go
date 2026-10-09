// Package httpapi — 平台账号管理 HTTP API。
//
// 提供平台节点的 SIP 注册账号的增删查改，供 Web 管理页面调用。
// 账号密码通过 sqlite 持久化，运行时改动即时生效。
package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// accountListResponse 是 GET /platforms/:id/accounts 的响应格式。
type accountListResponse struct {
	Accounts []port.AccountInfo `json:"accounts"`
}

// accountAddRequest 是 POST /platforms/:id/accounts 的请求体。
type accountAddRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// accountSetPasswordRequest 是 PUT /platforms/:id/accounts/:username/password 的请求体。
type accountSetPasswordRequest struct {
	Password string `json:"password"`
}

// handleAccountList 返回指定平台节点的账号列表（不含密码）。
// 路径：GET /v1/platforms/:id/accounts
func (s *Server) handleAccountList(c echo.Context) error {
	nodeID, err := model.ParseNodeID(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid node id"})
	}
	if !s.nodeExists(c.Request().Context(), nodeID) {
		return c.JSON(http.StatusNotFound, errorBody{Error: "node not found"})
	}

	accounts, err := s.accounts.ListAccounts(nodeID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorBody{Error: "failed to list accounts"})
	}
	return c.JSON(http.StatusOK, accountListResponse{Accounts: accounts})
}

// handleAccountAdd 为指定平台节点新增一个 SIP 注册账号。
// 路径：POST /v1/platforms/:id/accounts
func (s *Server) handleAccountAdd(c echo.Context) error {
	nodeID, err := model.ParseNodeID(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid node id"})
	}
	if !s.nodeExists(c.Request().Context(), nodeID) {
		return c.JSON(http.StatusNotFound, errorBody{Error: "node not found"})
	}

	var req accountAddRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid request body"})
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Password = strings.TrimSpace(req.Password)
	if req.Username == "" {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "username is required"})
	}
	if len(req.Username) != 20 {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "username must be a 20-digit GB/T 28181 device id"})
	}
	if err := s.accounts.AddAccount(nodeID, req.Username, req.Password); err != nil {
		if errors.Is(err, port.ErrAccountExists) {
			return c.JSON(http.StatusConflict, errorBody{Error: "account already exists"})
		}
		return c.JSON(http.StatusInternalServerError, errorBody{Error: "failed to add account"})
	}
	return c.NoContent(http.StatusCreated)
}

// handleAccountRemove 删除指定平台节点的一个 SIP 注册账号。
// 路径：DELETE /v1/platforms/:id/accounts/:username
func (s *Server) handleAccountRemove(c echo.Context) error {
	nodeID, err := model.ParseNodeID(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid node id"})
	}
	if !s.nodeExists(c.Request().Context(), nodeID) {
		return c.JSON(http.StatusNotFound, errorBody{Error: "node not found"})
	}

	username := strings.TrimSpace(c.Param("username"))
	if username == "" {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "username is required"})
	}
	if err := s.accounts.RemoveAccount(nodeID, username); err != nil {
		if errors.Is(err, port.ErrAccountNotFound) {
			return c.JSON(http.StatusNotFound, errorBody{Error: "account not found"})
		}
		return c.JSON(http.StatusInternalServerError, errorBody{Error: "failed to remove account"})
	}
	return c.NoContent(http.StatusNoContent)
}

// handleAccountSetPassword 修改指定平台节点账号的密码。
// 路径：PUT /v1/platforms/:id/accounts/:username/password
func (s *Server) handleAccountSetPassword(c echo.Context) error {
	nodeID, err := model.ParseNodeID(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid node id"})
	}
	if !s.nodeExists(c.Request().Context(), nodeID) {
		return c.JSON(http.StatusNotFound, errorBody{Error: "node not found"})
	}

	username := strings.TrimSpace(c.Param("username"))
	if username == "" {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "username is required"})
	}

	var req accountSetPasswordRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid request body"})
	}
	req.Password = strings.TrimSpace(req.Password)
	// 允许空密码（方便通过界面清空密码）
	if err := s.accounts.SetAccountPassword(nodeID, username, req.Password); err != nil {
		if errors.Is(err, port.ErrAccountNotFound) {
			return c.JSON(http.StatusNotFound, errorBody{Error: "account not found"})
		}
		return c.JSON(http.StatusInternalServerError, errorBody{Error: "failed to set password"})
	}
	return c.NoContent(http.StatusNoContent)
}
