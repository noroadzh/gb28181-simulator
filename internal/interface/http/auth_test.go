package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpapi "github.com/noroadzh/gb28181-simulator/internal/interface/http"
	platformconfig "github.com/noroadzh/gb28181-simulator/internal/platform/config"
	"github.com/noroadzh/gb28181-simulator/internal/platform/observability/logging"
)

func newAuthServer(t *testing.T, enabled bool) *httptest.Server {
	t.Helper()
	cfg := platformconfig.Config{
		HTTP: platformconfig.HTTPConfig{
			Auth: platformconfig.HTTPAuthConfig{
				Enabled:  enabled,
				Username: "admin",
				Password: "s3cret",
			},
		},
	}
	s := httpapi.NewServer(cfg, logging.NewHub(4), httpapi.Version{Version: "test"},
		nil, nil, nil, nil, nil)
	ts := httptest.NewServer(s.Echo())
	t.Cleanup(ts.Close)
	return ts
}

func TestAuth_DisabledLeavesAPIOpen(t *testing.T) {
	ts := newAuthServer(t, false)
	resp, err := http.Get(ts.URL + "/v1/nodes")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestAuth_RequiresLogin(t *testing.T) {
	ts := newAuthServer(t, true)
	resp, err := http.Get(ts.URL + "/v1/nodes")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}

	// Health stays public for docker healthchecks.
	hz, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer hz.Body.Close()
	if hz.StatusCode != http.StatusOK {
		t.Fatalf("healthz = %d", hz.StatusCode)
	}
}

func TestAuth_LoginCookieUnlocksAPI(t *testing.T) {
	ts := newAuthServer(t, true)
	client := &http.Client{}

	bad, err := client.Post(ts.URL+"/v1/auth/login", "application/json",
		strings.NewReader(`{"username":"admin","password":"wrong"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Body.Close()
	if bad.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad login = %d", bad.StatusCode)
	}

	login, err := client.Post(ts.URL+"/v1/auth/login", "application/json",
		strings.NewReader(`{"username":"admin","password":"s3cret"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer login.Body.Close()
	if login.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(login.Body)
		t.Fatalf("login = %d (%s)", login.StatusCode, body)
	}
	var got struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(login.Body).Decode(&got)
	if got.Token == "" {
		t.Fatal("empty token")
	}
	cookie := ""
	for _, c := range login.Cookies() {
		if c.Name == "gbsim_sid" {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("missing session cookie")
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/nodes", nil)
	req.AddCookie(&http.Cookie{Name: "gbsim_sid", Value: cookie})
	okResp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer okResp.Body.Close()
	if okResp.StatusCode != http.StatusOK {
		t.Fatalf("authed nodes = %d", okResp.StatusCode)
	}

	bearer, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/version", nil)
	bearer.Header.Set("Authorization", "Bearer "+got.Token)
	br, err := client.Do(bearer)
	if err != nil {
		t.Fatal(err)
	}
	defer br.Body.Close()
	if br.StatusCode != http.StatusOK {
		t.Fatalf("bearer version = %d", br.StatusCode)
	}

	logout, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/auth/logout", nil)
	logout.AddCookie(&http.Cookie{Name: "gbsim_sid", Value: cookie})
	lo, err := client.Do(logout)
	if err != nil {
		t.Fatal(err)
	}
	defer lo.Body.Close()

	again, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/nodes", nil)
	again.AddCookie(&http.Cookie{Name: "gbsim_sid", Value: cookie})
	denied, err := client.Do(again)
	if err != nil {
		t.Fatal(err)
	}
	defer denied.Body.Close()
	if denied.StatusCode != http.StatusUnauthorized {
		t.Fatalf("after logout = %d, want 401", denied.StatusCode)
	}
}
