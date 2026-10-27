package app

// GB 35114 identity wiring: one process-wide SM2 key pair is attached to every
// credential the process verifies (acceptor side) or answers a challenge with
// (registrar side). Off by default.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

var (
	testSM2Priv = bytes.Repeat([]byte{0x01}, 32)
	testSM2Pub  = append([]byte{0x04}, bytes.Repeat([]byte{0x02}, 64)...)
)

// capturingAuthenticator records the credential it was asked to verify.
type capturingAuthenticator struct {
	fakeAuthenticator
	cred model.Credentials
}

func (a *capturingAuthenticator) Verify(_ model.Message, cred model.Credentials) error {
	a.cred = cred
	return a.outcome
}

func registerWithAuthorization(t *testing.T, tr *acceptorTransport) model.Message {
	t.Helper()
	authz := `Digest username="34020000011310000001", realm="3402000000", nonce="n1", uri="sip:3402000000@3402000000", response="ok", algorithm=SM3`
	req := registerRequest(t, "34020000011310000001",
		model.NewHeader("Authorization", authz),
		model.NewHeader("Expires", "3600"))
	return tr.deliver(t, req)
}

// WithGB35114 puts the process identity on the credential handed to the
// authenticator, so the adapter can verify the peer's SecurityInfo.
func TestAcceptor_GB35114IdentityRidesOnCredential(t *testing.T) {
	auth := &capturingAuthenticator{}
	acceptor, tr, creds, _, _ := acceptorFixture(t, auth)
	creds.add(t, mustPlatformNode(t), "34020000011310000001", "secret")
	acceptor.WithGB35114(testSM2Priv, testSM2Pub)

	if resp := registerWithAuthorization(t, tr); resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	if !bytes.Equal(auth.cred.SM2PrivateKey(), testSM2Priv) {
		t.Errorf("private key = %x, want the process identity", auth.cred.SM2PrivateKey())
	}
	if !bytes.Equal(auth.cred.SM2PublicKey(), testSM2Pub) {
		t.Errorf("public key = %x, want the process identity", auth.cred.SM2PublicKey())
	}
}

// Without the option the credential is untouched: MD5-only deployments must
// behave exactly as before.
func TestAcceptor_NoGB35114IdentityLeavesCredentialUntouched(t *testing.T) {
	auth := &capturingAuthenticator{}
	_, tr, creds, _, _ := acceptorFixture(t, auth)
	creds.add(t, mustPlatformNode(t), "34020000011310000001", "secret")

	if resp := registerWithAuthorization(t, tr); resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	if len(auth.cred.SM2PrivateKey()) != 0 || len(auth.cred.SM2PublicKey()) != 0 {
		t.Errorf("credential gained an SM2 key pair: priv=%x pub=%x",
			auth.cred.SM2PrivateKey(), auth.cred.SM2PublicKey())
	}
}

// The identity is copied on the way in, so a caller reusing its buffer cannot
// change what later registrations are verified with.
func TestAcceptor_GB35114IdentityIsCopied(t *testing.T) {
	priv := append([]byte(nil), testSM2Priv...)
	auth := &capturingAuthenticator{}
	acceptor, tr, creds, _, _ := acceptorFixture(t, auth)
	creds.add(t, mustPlatformNode(t), "34020000011310000001", "secret")
	acceptor.WithGB35114(priv, nil)
	priv[0] = 0xFF

	registerWithAuthorization(t, tr)
	if bytes.Equal(auth.cred.SM2PrivateKey(), priv) {
		t.Error("the acceptor kept a reference to the caller's key buffer")
	}
}

// capturingAuthorizer records the credential the registrar answered with.
type capturingAuthorizer struct {
	cred model.Credentials
}

func (a *capturingAuthorizer) Authorize(_ string, cred model.Credentials, _, _ string) (model.Header, error) {
	a.cred = cred
	return model.NewHeader("Authorization", `Digest username="`+cred.Username()+`", response="stub"`), nil
}

var _ port.Authorizer = (*capturingAuthorizer)(nil)

func TestRegistrar_GB35114IdentityRidesOnCredential(t *testing.T) {
	t.Parallel()
	const challenge = `Digest realm="3402000000", nonce="nonce-1", qop="auth", algorithm=SM3`
	tr := &scriptedTransport{in: []incoming{
		{msg: response(t, 401, callIDHeader(testCallID),
			model.NewHeader("WWW-Authenticate", challenge)), peer: testServer},
		{msg: response(t, 200, callIDHeader(testCallID)), peer: testServer},
	}}
	auth := &capturingAuthorizer{}
	reg := testRegistration(t, 3600, 2*time.Second)

	r := newTestRegistrar(t, auth, &fakeClock{now: time.Now()})
	r.newCallID = func() string { return testCallID }
	r.WithGB35114(testSM2Priv, testSM2Pub)

	if _, err := r.Register(context.Background(), tr, registeredNode(t, reg), reg); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !bytes.Equal(auth.cred.SM2PrivateKey(), testSM2Priv) {
		t.Errorf("private key = %x, want the process identity", auth.cred.SM2PrivateKey())
	}
	if !bytes.Equal(auth.cred.SM2PublicKey(), testSM2Pub) {
		t.Errorf("public key = %x, want the process identity", auth.cred.SM2PublicKey())
	}
}

func TestRegistrar_NoGB35114IdentityLeavesCredentialUntouched(t *testing.T) {
	t.Parallel()
	const challenge = `Digest realm="3402000000", nonce="nonce-1", qop="auth", algorithm=MD5`
	tr := &scriptedTransport{in: []incoming{
		{msg: response(t, 401, callIDHeader(testCallID),
			model.NewHeader("WWW-Authenticate", challenge)), peer: testServer},
		{msg: response(t, 200, callIDHeader(testCallID)), peer: testServer},
	}}
	auth := &capturingAuthorizer{}
	reg := testRegistration(t, 3600, 2*time.Second)

	r := newTestRegistrar(t, auth, &fakeClock{now: time.Now()})
	r.newCallID = func() string { return testCallID }

	if _, err := r.Register(context.Background(), tr, registeredNode(t, reg), reg); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if len(auth.cred.SM2PrivateKey()) != 0 || len(auth.cred.SM2PublicKey()) != 0 {
		t.Errorf("credential gained an SM2 key pair: priv=%x pub=%x",
			auth.cred.SM2PrivateKey(), auth.cred.SM2PublicKey())
	}
}

// verifyingAuthorizer adds the client half of mutual authentication: it
// checks the SecurityInfo a platform attaches to the REGISTER answer.
type verifyingAuthorizer struct {
	capturingAuthorizer
	calls int
	err   error
}

func (a *verifyingAuthorizer) VerifySecurityInfo(_ model.Message, _ model.Header, _ model.Credentials) error {
	a.calls++
	return a.err
}

func challengedRegister(t *testing.T) *scriptedTransport {
	t.Helper()
	const challenge = `Digest realm="3402000000", nonce="nonce-1", qop="auth", algorithm=SM3`
	return &scriptedTransport{in: []incoming{
		{msg: response(t, 401, callIDHeader(testCallID),
			model.NewHeader("WWW-Authenticate", challenge)), peer: testServer},
		{msg: response(t, 200, callIDHeader(testCallID),
			model.NewHeader("SecurityInfo", "SM2,00")), peer: testServer},
	}}
}

// With an SM2 identity configured, the platform's signature on the 200 OK is
// checked before the registration is reported as successful.
func TestRegistrar_VerifiesPlatformSecurityInfo(t *testing.T) {
	t.Parallel()
	tr := challengedRegister(t)
	auth := &verifyingAuthorizer{}
	reg := testRegistration(t, 3600, 2*time.Second)

	r := newTestRegistrar(t, auth, &fakeClock{now: time.Now()})
	r.newCallID = func() string { return testCallID }
	r.WithGB35114(testSM2Priv, testSM2Pub)

	if _, err := r.Register(context.Background(), tr, registeredNode(t, reg), reg); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if auth.calls != 1 {
		t.Errorf("VerifySecurityInfo called %d times, want 1", auth.calls)
	}
}

// A platform whose signature does not verify has not proven its identity, so
// the registration fails instead of reporting success.
func TestRegistrar_RejectsInvalidPlatformSecurityInfo(t *testing.T) {
	t.Parallel()
	tr := challengedRegister(t)
	auth := &verifyingAuthorizer{err: fmt.Errorf("%w: bad signature", port.ErrSecurityInfoMismatch)}
	reg := testRegistration(t, 3600, 2*time.Second)

	r := newTestRegistrar(t, auth, &fakeClock{now: time.Now()})
	r.newCallID = func() string { return testCallID }
	r.WithGB35114(testSM2Priv, testSM2Pub)

	_, err := r.Register(context.Background(), tr, registeredNode(t, reg), reg)
	if !errors.Is(err, port.ErrSecurityInfoMismatch) {
		t.Fatalf("err = %v, want ErrSecurityInfoMismatch", err)
	}
	var regErr *RegistrationError
	if !errors.As(err, &regErr) || regErr.Stage != StageResponse {
		t.Errorf("err = %v, want a StageResponse RegistrationError", err)
	}
}
