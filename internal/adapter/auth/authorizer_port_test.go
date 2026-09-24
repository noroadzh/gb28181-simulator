package auth

import (
	"errors"
	"strings"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

func mustCred(t *testing.T, user, realm, password string) model.Credentials {
	t.Helper()
	cred, err := model.NewCredentials(user, realm, password)
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	return cred
}

func TestAuthorizerAdapter_NilRejected(t *testing.T) {
	t.Parallel()
	if _, err := NewAuthorizerAdapter(nil); err == nil {
		t.Fatal("NewAuthorizerAdapter(nil) must fail")
	}
}

func TestAuthorizerAdapter_SatisfiesPort(t *testing.T) {
	t.Parallel()
	var _ port.Authorizer = (*AuthorizerAdapter)(nil)
}

// The adapter takes the raw WWW-Authenticate value: parsing it is the
// adapter's job, so the app layer never has to know the header grammar.
func TestAuthorizerAdapter_Authorize(t *testing.T) {
	t.Parallel()
	cred := mustCred(t, "34020000001320000001", "3402000000", "secret")
	const uri = "sip:34020000002000000001@3402000000"

	tests := []struct {
		name      string
		challenge string
		wantErr   error
		check     func(t *testing.T, h model.Header)
	}{
		{
			name:      "qop=auth",
			challenge: `Digest realm="3402000000", nonce="nonce-1", qop="auth", algorithm=MD5`,
			check: func(t *testing.T, h model.Header) {
				for _, want := range []string{`qop=auth`, `nc=00000001`, `cnonce="`, `response="`} {
					if !strings.Contains(h.Value(), want) {
						t.Errorf("Authorization missing %q: %v", want, h.Value())
					}
				}
			},
		},
		{
			name:      "no qop",
			challenge: `Digest realm="3402000000", nonce="nonce-1", algorithm=MD5`,
			check: func(t *testing.T, h model.Header) {
				for _, absent := range []string{"qop=", "nc=", "cnonce="} {
					if strings.Contains(h.Value(), absent) {
						t.Errorf("qop-less Authorization must not contain %q: %v", absent, h.Value())
					}
				}
			},
		},
		{
			name:      "unsupported algorithm",
			challenge: `Digest realm="3402000000", nonce="nonce-1", qop="auth", algorithm=SHA-256`,
			wantErr:   ErrUnknownAlgorithm,
		},
		{
			name:      "challenge without a nonce",
			challenge: `Digest realm="3402000000", qop="auth"`,
			wantErr:   ErrMalformedAuthorization,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			adapter, err := NewAuthorizerAdapter(NewAuthorizer(nil))
			if err != nil {
				t.Fatalf("NewAuthorizerAdapter: %v", err)
			}
			h, err := adapter.Authorize(tc.challenge, cred, "REGISTER", uri)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("got %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Authorize: %v", err)
			}
			if h.Name() != "Authorization" {
				t.Errorf("header name: got %q want Authorization", h.Name())
			}
			if !strings.HasPrefix(h.Value(), "Digest ") {
				t.Errorf("header value must start with the Digest scheme: %v", h.Value())
			}
			tc.check(t, h)
		})
	}
}

// Whatever the adapter hands back must be verifiable by the server side,
// i.e. the two halves of the package agree on the digest.
func TestAuthorizerAdapter_OutputPassesVerify(t *testing.T) {
	t.Parallel()
	const password = "gb28181-secret"
	cred := mustCred(t, "34020000001320000001", "3402000000", password)
	challenge := `Digest realm="3402000000", nonce="nonce-1", qop="auth", algorithm=MD5`

	adapter, err := NewAuthorizerAdapter(NewAuthorizer(nil))
	if err != nil {
		t.Fatalf("NewAuthorizerAdapter: %v", err)
	}
	h, err := adapter.Authorize(challenge, cred, "REGISTER", "sip:34020000002000000001@3402000000")
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	verifier, err := NewAuthenticatorAdapter(NewResponder(nil))
	if err != nil {
		t.Fatalf("NewAuthenticatorAdapter: %v", err)
	}
	msg, err := model.NewRequest("REGISTER", "sip:34020000002000000001@3402000000",
		[]model.Header{h}, "")
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := verifier.Verify(msg, cred); err != nil {
		t.Errorf("Verify rejected the adapter's Authorization: %v", err)
	}
}
