package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
	httpapi "github.com/your-org/gb28181-simulator/internal/interface/http"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
	"github.com/your-org/gb28181-simulator/internal/platform/observability/logging"
)

// inMemoryAccountAdmin is a minimal port.AccountAdmin backed by a map.
type inMemoryAccountAdmin struct {
	accounts map[string][]port.AccountInfo
}

func newInMemoryAccountAdmin() *inMemoryAccountAdmin {
	return &inMemoryAccountAdmin{accounts: make(map[string][]port.AccountInfo)}
}

func (m *inMemoryAccountAdmin) ListAccounts(nodeID model.NodeID) ([]port.AccountInfo, error) {
	return m.accounts[nodeID.String()], nil
}

func (m *inMemoryAccountAdmin) AddAccount(nodeID model.NodeID, username, password string) error {
	for _, a := range m.accounts[nodeID.String()] {
		if a.Username == username {
			return port.ErrAccountExists
		}
	}
	m.accounts[nodeID.String()] = append(m.accounts[nodeID.String()], port.AccountInfo{
		Username:  username,
		CreatedAt: "2026-10-09T00:00:00Z",
	})
	return nil
}

func (m *inMemoryAccountAdmin) RemoveAccount(nodeID model.NodeID, username string) error {
	accs := m.accounts[nodeID.String()]
	for i, a := range accs {
		if a.Username == username {
			m.accounts[nodeID.String()] = append(accs[:i], accs[i+1:]...)
			return nil
		}
	}
	return port.ErrAccountNotFound
}

func (m *inMemoryAccountAdmin) SetAccountPassword(nodeID model.NodeID, username, password string) error {
	for _, a := range m.accounts[nodeID.String()] {
		if a.Username == username {
			return nil
		}
	}
	return port.ErrAccountNotFound
}

// newAccountsTestServer creates a test server with a fakeNodeView (seeded with
// one platform-large node) and the given account admin.
func newAccountsTestServer(t *testing.T, admin port.AccountAdmin) *httptest.Server {
	t.Helper()
	view := newFakeNodeView()
	view.seed(t, "34020000001310000001", "127.0.0.1:5060", model.StatusOnline)
	s := httpapi.NewServer(
		platformconfig.Config{},
		logging.NewHub(4),
		httpapi.Version{Version: "x"},
		view,
		nil, nil, admin, nil,
	)
	return httptest.NewServer(s.Echo())
}

func httpJSON(t *testing.T, srv *httptest.Server, method, path string, body string) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, srv.URL+path, rdr)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// TestAccounts_EmptyPassword_Allowed verifies that an account can be added with
// an empty password field, and that the same API accepts clearing a password
// (set password to empty string).
func TestAccounts_EmptyPassword_Allowed(t *testing.T) {
	admin := newInMemoryAccountAdmin()
	srv := newAccountsTestServer(t, admin)
	defer srv.Close()

	nodeID := "34020000001310000001"
	base := "/v1/platforms/" + nodeID

	// POST with empty password → expect 201 Created (not 400).
	code, body := httpJSON(t, srv, "POST", base+"/accounts",
		`{"username":"34020000001310000001","password":""}`)
	if code != http.StatusCreated {
		t.Errorf("add empty-password account: got status %d, want %d, body=%s", code, http.StatusCreated, body)
	}

	// Verify the account appears in the list (small-case JSON field, task 2).
	code, body = httpJSON(t, srv, "GET", base+"/accounts", "")
	if code != http.StatusOK {
		t.Fatalf("list accounts: got status %d, body=%s", code, body)
	}
	var list struct {
		Accounts []port.AccountInfo `json:"accounts"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode list: %v, body=%s", err, body)
	}
	if len(list.Accounts) != 1 || list.Accounts[0].Username != "34020000001310000001" {
		t.Errorf("list accounts = %+v, want one with username 34020000001310000001", list.Accounts)
	}

	// PUT empty password (clear) → expect 204 No Content (task 3).
	code, _ = httpJSON(t, srv, "PUT", base+"/accounts/34020000001310000001/password", `{"password":""}`)
	if code != http.StatusNoContent {
		t.Errorf("set empty password: got status %d, want %d", code, http.StatusNoContent)
	}
}

// TestAccounts_UsernameStillRequired verifies that username is still required.
func TestAccounts_UsernameStillRequired(t *testing.T) {
	admin := newInMemoryAccountAdmin()
	srv := newAccountsTestServer(t, admin)
	defer srv.Close()

	code, _ := httpJSON(t, srv, "POST", "/v1/platforms/34020000001310000001/accounts", `{"username":"","password":"x"}`)
	if code != http.StatusBadRequest {
		t.Errorf("missing username: got status %d, want %d", code, http.StatusBadRequest)
	}
}
