package auth

import (
	"strings"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/adapter/sm"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// TestGB35114_SM2MutualAuth covers the end-to-end GB 35114 A-grade flow:
// a device holding an SM2 key pair builds an Authorization header whose
// Digest response carries a security-info SM2 signature, and a platform
// holding the peer public key verifies both layers. A platform configured
// with a different public key must refuse.
func TestGB35114_SM2MutualAuth(t *testing.T) {
	priv, pub, err := sm.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	_, pubWrong, err := sm.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair (wrong): %v", err)
	}

	cases := []struct {
		name       string
		serverPub  []byte
		wantAccept bool
	}{
		{"matching_pair", pub, true},
		{"wrong_pubkey", pubWrong, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deviceCred, err := model.NewCredentials("34020000001320000001", "3402000000", "12345678")
			if err != nil {
				t.Fatalf("NewCredentials: %v", err)
			}
			deviceCred = deviceCred.WithSM2KeyPair(priv, pub)

			serverCred, err := model.NewCredentials("34020000001320000001", "3402000000", "12345678")
			if err != nil {
				t.Fatalf("NewCredentials (server): %v", err)
			}
			serverCred = serverCred.WithSM2KeyPair(nil, tc.serverPub)

			ch, err := model.NewChallenge("3402000000", "nonce-gb35114-001", "MD5", "", "auth")
			if err != nil {
				t.Fatalf("NewChallenge: %v", err)
			}

			const (
				method = "REGISTER"
				uri    = "sip:34020000002000000001@3402000000"
			)
			authz, err := BuildAuthorizationWithHash(MD5Hash, deviceCred, ch, method, uri, FormatNC(1), "0a4f113b")
			if err != nil {
				t.Fatalf("BuildAuthorizationWithHash: %v", err)
			}
			if !strings.Contains(authz, `security-info="SM2,`) {
				t.Fatalf("Authorization missing security-info directive:\n%s", authz)
			}

			err = NewResponder(nil).VerifyWithCredentials(&mockReq{method: method, authorization: authz}, serverCred)
			if tc.wantAccept {
				if err != nil {
					t.Fatalf("expected accept, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected refusal, got accept")
			}
		})
	}
}

// TestGB35114_SM3AlgorithmMutualAuth exercises the full A-grade algorithm
// set: algorithm=SM3 Digest plus SM2 security-info signature. The server
// Responder is constructed with the default MD5 hash to prove the hash is
// selected from the Authorization algorithm field, not from server config.
func TestGB35114_SM3AlgorithmMutualAuth(t *testing.T) {
	priv, pub, err := sm.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	deviceCred, err := model.NewCredentials("34020000001320000001", "3402000000", "12345678")
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	deviceCred = deviceCred.WithSM2KeyPair(priv, pub)
	serverCred, err := model.NewCredentials("34020000001320000001", "3402000000", "12345678")
	if err != nil {
		t.Fatalf("NewCredentials (server): %v", err)
	}
	serverCred = serverCred.WithSM2KeyPair(nil, pub)

	ch, err := model.NewChallenge("3402000000", "nonce-gb35114-002", "SM3", "", "auth")
	if err != nil {
		t.Fatalf("NewChallenge: %v", err)
	}

	const (
		method = "REGISTER"
		uri    = "sip:34020000002000000001@3402000000"
	)
	authz, err := BuildAuthorizationWithHash(SM3Hash, deviceCred, ch, method, uri, FormatNC(1), "0a4f113b")
	if err != nil {
		t.Fatalf("BuildAuthorizationWithHash: %v", err)
	}
	if err := NewResponder(nil).VerifyWithCredentials(&mockReq{method: method, authorization: authz}, serverCred); err != nil {
		t.Fatalf("SM3 + SM2 mutual auth failed: %v\nAuthorization:\n%s", err, authz)
	}
}

// TestGB35114_SignSecurityInfo_SM3Triggers verifies the server-side
// response helper (task 5.1): when the client's Authorization advertises
// algorithm=SM3 and the server holds an SM2 private key, the adapter
// produces a SecurityInfo header. A plain MD5 request must not trigger
// the signature.
func TestGB35114_SignSecurityInfo_SM3Triggers(t *testing.T) {
	priv, pub, err := sm.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	cred, err := model.NewCredentials("34020000001320000001", "3402000000", "12345678")
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	cred = cred.WithSM2KeyPair(priv, pub)
	a, err := NewAuthenticatorAdapter(NewResponder(nil))
	if err != nil {
		t.Fatalf("NewAuthenticatorAdapter: %v", err)
	}

	cases := []struct {
		name  string
		alg   string
		wantH bool
	}{
		{"SM3", "SM3", true},
		{"MD5", "MD5", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch, _ := model.NewChallenge("3402000000", "nonce-gb35114-sign", tc.alg, "", "auth")
			const (
				method = "REGISTER"
				uri    = "sip:34020000002000000001@3402000000"
			)
			authz, err := BuildAuthorizationWithHash(MD5Hash, cred, ch, method, uri, FormatNC(1), "0a4f113b")
			if err != nil {
				t.Fatalf("BuildAuthorizationWithHash: %v", err)
			}
			msg, err := model.NewRequest(method, uri, []model.Header{model.NewHeader("Authorization", authz)}, "")
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			h, ok := a.SignSecurityInfo(msg, cred)
			if tc.wantH {
				if !ok {
					t.Fatalf("expected SecurityInfo header for SM3, got none")
				}
				if h.Name() != "SecurityInfo" {
					t.Fatalf("expected header name SecurityInfo, got %s", h.Name())
				}
				if !strings.HasPrefix(h.Value(), "SM2,") {
					t.Fatalf("expected SM2-prefixed value, got %s", h.Value())
				}
				return
			}
			if ok {
				t.Fatalf("expected no SecurityInfo header for MD5, got %s", h.Value())
			}
		})
	}
}

// TestGB35114_PlainDigestUnaffected verifies the backward-compatibility
// contract: credentials without SM2 key material produce no security-info
// directive and VerifyWithCredentials accepts them like plain Verify.
func TestGB35114_PlainDigestUnaffected(t *testing.T) {
	cred, err := model.NewCredentials("34020000001320000001", "3402000000", "12345678")
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	if cred.SM2PrivateKey() != nil || cred.SM2PublicKey() != nil {
		t.Fatal("fresh Credentials must not carry SM2 key material")
	}

	ch, err := model.NewChallenge("3402000000", "nonce-plain", "MD5", "", "auth")
	if err != nil {
		t.Fatalf("NewChallenge: %v", err)
	}
	authz, err := BuildAuthorizationWithHash(MD5Hash, cred, ch, "REGISTER", "sip:34020000002000000001@3402000000", FormatNC(1), "0a4f113b")
	if err != nil {
		t.Fatalf("BuildAuthorizationWithHash: %v", err)
	}
	if strings.Contains(authz, "security-info") {
		t.Fatalf("plain credentials must not emit security-info:\n%s", authz)
	}
	if err := NewResponder(nil).VerifyWithCredentials(&mockReq{method: "REGISTER", authorization: authz}, cred); err != nil {
		t.Fatalf("VerifyWithCredentials (plain): %v", err)
	}
}
