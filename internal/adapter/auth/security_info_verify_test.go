package auth

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/adapter/sm"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

const verifyDigestResponse = "6629fae49393a05397450978507c4ef1"

func verifyFixture(t *testing.T) (priv, pub []byte, cred model.Credentials, authz model.Header) {
	t.Helper()
	priv, pub, err := sm.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	cred, err = model.NewCredentials("34020000011310000001", "3402000000", "secret")
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	cred = cred.WithSM2KeyPair(priv, pub)
	authz = model.NewHeader("Authorization",
		`Digest username="34020000011310000001", realm="3402000000", nonce="n1", uri="sip:34020000002000000001@3402000000", response="`+
			verifyDigestResponse+`", algorithm=SM3`)
	return priv, pub, cred, authz
}

func answerWithSecurityInfo(t *testing.T, value string) model.Message {
	t.Helper()
	msg, err := model.NewResponse(200, "OK", []model.Header{
		model.NewHeader("Call-ID", "register-1"),
		model.NewHeader("SecurityInfo", value),
	}, "")
	if err != nil {
		t.Fatalf("NewResponse: %v", err)
	}
	return msg
}

// The platform proves it holds the SM2 private key by signing the digest
// response the device sent.
func TestVerifySecurityInfo_AcceptsPlatformSignature(t *testing.T) {
	priv, _, cred, authz := verifyFixture(t)
	sig, err := sm.SignSM2(priv, []byte(verifyDigestResponse))
	if err != nil {
		t.Fatalf("SignSM2: %v", err)
	}
	adapter, _ := NewAuthorizerAdapter(NewAuthorizer(nil))

	if err := adapter.VerifySecurityInfo(answerWithSecurityInfo(t, "SM2,"+hex.EncodeToString(sig)), authz, cred); err != nil {
		t.Fatalf("VerifySecurityInfo: %v", err)
	}
}

// A signature from another key proves nothing about this platform.
func TestVerifySecurityInfo_RejectsForeignSignature(t *testing.T) {
	_, _, cred, authz := verifyFixture(t)
	otherPriv, _, err := sm.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	sig, err := sm.SignSM2(otherPriv, []byte(verifyDigestResponse))
	if err != nil {
		t.Fatalf("SignSM2: %v", err)
	}
	adapter, _ := NewAuthorizerAdapter(NewAuthorizer(nil))

	err = adapter.VerifySecurityInfo(answerWithSecurityInfo(t, "SM2,"+hex.EncodeToString(sig)), authz, cred)
	if !errors.Is(err, port.ErrSecurityInfoMismatch) {
		t.Fatalf("err = %v, want ErrSecurityInfoMismatch", err)
	}
}

// The MD5-only platform answers without the header, so nothing is checked.
func TestVerifySecurityInfo_AbsentHeaderPasses(t *testing.T) {
	_, _, cred, authz := verifyFixture(t)
	resp, err := model.NewResponse(200, "OK", []model.Header{model.NewHeader("Call-ID", "register-1")}, "")
	if err != nil {
		t.Fatalf("NewResponse: %v", err)
	}
	adapter, _ := NewAuthorizerAdapter(NewAuthorizer(nil))

	if err := adapter.VerifySecurityInfo(resp, authz, cred); err != nil {
		t.Fatalf("VerifySecurityInfo without the header: %v", err)
	}
}

// A node with no SM2 identity never asked for mutual authentication, so an
// unsolicited header cannot be verified and is not treated as a failure.
func TestVerifySecurityInfo_NoPublicKeyPasses(t *testing.T) {
	_, _, _, authz := verifyFixture(t)
	cred, err := model.NewCredentials("34020000011310000001", "3402000000", "secret")
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	adapter, _ := NewAuthorizerAdapter(NewAuthorizer(nil))

	if err := adapter.VerifySecurityInfo(answerWithSecurityInfo(t, "SM2,00"), authz, cred); err != nil {
		t.Fatalf("VerifySecurityInfo without a public key: %v", err)
	}
}

func TestVerifySecurityInfo_RejectsMalformedHeaders(t *testing.T) {
	_, _, cred, authz := verifyFixture(t)
	adapter, _ := NewAuthorizerAdapter(NewAuthorizer(nil))

	for name, value := range map[string]string{
		"unknown scheme": "MD5,abcdef",
		"bad hex":        "SM2,zzzz",
	} {
		t.Run(name, func(t *testing.T) {
			err := adapter.VerifySecurityInfo(answerWithSecurityInfo(t, value), authz, cred)
			if !errors.Is(err, port.ErrSecurityInfoMismatch) {
				t.Fatalf("err = %v, want ErrSecurityInfoMismatch", err)
			}
			if !strings.Contains(err.Error(), "security-info") {
				t.Errorf("error %q does not name what failed", err)
			}
		})
	}
}
