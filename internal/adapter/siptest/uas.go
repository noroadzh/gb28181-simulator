// Package siptest provides SIP test doubles: a minimal UAS that behaves
// like a GB/T 28181 platform towards a registering device.
//
// It exists because nothing else in the repository can stand in for a
// platform: sipprobe sends one canned message and never answers, and the
// unit tests use scripted transports that cannot prove a real packet left
// the process. UAS binds a real socket and speaks through the same port
// adapter the production code uses, so a registration against it exercises
// the whole wire path.
//
// It is a test helper, not production code: nothing under cmd/ references
// it.
package siptest

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"github.com/your-org/gb28181-simulator/internal/adapter/auth"
	"github.com/your-org/gb28181-simulator/internal/adapter/siptransport"
	"github.com/your-org/gb28181-simulator/internal/adapter/sm"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// UAS is a one-purpose SIP server: it challenges a REGISTER, checks the
// answer with the same Digest rules a platform uses, and replies 200 OK.
// Every message it receives is recorded so a test can assert on the bytes
// that actually arrived.
type UAS struct {
	inner         *siptransport.Transport
	port          *siptransport.PortAdapter
	realm         string
	password      string
	grantedExpiry uint32
	// silentMessages makes the UAS ignore keepalives instead of answering
	// them, which is how a test drives a node into "unreachable".
	silentMessages bool

	// sm2Mode turns on GB/T 35114 SM3+SM2: SM3 challenges, SM2-verified
	// answers and SM2-signed 200 OKs.
	sm2Mode        bool
	sm2DeviceCred  *model.Credentials
	sm2SigningPriv []byte

	challenger *auth.Challenger
	responder  *auth.Responder

	mu       sync.Mutex
	received []model.Message
}

// UASOption customises a UAS.
type UASOption func(*UAS)

// WithGrantedExpiry makes the UAS answer 200 OK with an Expires of its own
// choosing, so a test can prove the device takes the platform's value over
// the one it asked for.
func WithGrantedExpiry(seconds uint32) UASOption {
	return func(u *UAS) { u.grantedExpiry = seconds }
}

// WithSilentMessages makes the UAS record keepalives but never answer them.
// It stands in for a platform that has stopped responding, so a test can
// watch a node give up after its tolerance runs out.
func WithSilentMessages() UASOption {
	return func(u *UAS) { u.silentMessages = true }
}

// WithSM2 turns on GB/T 35114 SM3+SM2 mode: challenges carry
// algorithm=SM3, answers must carry a valid SM2 signature under device's
// public key, and every 200 OK is signed with the platform key.
func WithSM2(device model.Credentials, platformSigningKey []byte) UASOption {
	return func(u *UAS) {
		u.sm2Mode = true
		u.sm2DeviceCred = &device
		u.sm2SigningPriv = platformSigningKey
		u.challenger = auth.NewChallenger(nil, auth.WithHashName("SM3"))
		u.responder = auth.NewResponder(nil)
	}
}

// NewUAS binds a listener on network ("udp" or "tcp") at addr and answers
// registrations for realm/password. addr may use port 0 to take a free
// port; Addr reports the address actually bound.
func NewUAS(network, addr, realm, password string, opts ...UASOption) (*UAS, error) {
	inner, err := siptransport.New(network + "://" + addr)
	if err != nil {
		return nil, fmt.Errorf("siptest: bind %s://%s: %w", network, addr, err)
	}
	u := &UAS{
		inner:         inner,
		port:          siptransport.NewPortAdapter(inner),
		realm:         realm,
		password:      password,
		grantedExpiry: 3600,
		challenger:    auth.NewChallenger(nil),
		responder:     auth.NewResponder(nil),
	}
	for _, opt := range opts {
		opt(u)
	}
	return u, nil
}

// Addr returns the address the UAS listens on.
func (u *UAS) Addr() string { return u.inner.LocalAddr() }

// Received returns every message the UAS has accepted, in arrival order.
func (u *UAS) Received() []model.Message {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]model.Message, len(u.received))
	copy(out, u.received)
	return out
}

// Keepalives returns the MESSAGEs the UAS has accepted — the heartbeats of
// the devices that registered with it — in arrival order.
func (u *UAS) Keepalives() []model.Message {
	out := make([]model.Message, 0)
	for _, m := range u.Received() {
		if m.Method() == "MESSAGE" {
			out = append(out, m)
		}
	}
	return out
}

// Serve answers registrations and keepalives until ctx is cancelled. It
// returns nil when the context ends, and the receive error otherwise.
func (u *UAS) Serve(ctx context.Context) error {
	for {
		msg, peer, err := u.port.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		u.mu.Lock()
		u.received = append(u.received, msg)
		u.mu.Unlock()

		switch msg.Method() {
		case "REGISTER":
			if err := u.answer(ctx, msg, peer); err != nil {
				return err
			}
		case "MESSAGE":
			if err := u.answerKeepalive(ctx, msg, peer); err != nil {
				return err
			}
		}
	}
}

// answerKeepalive answers a device keepalive. A platform that has nothing
// to say simply acknowledges it; WithSilentMessages turns even that off.
func (u *UAS) answerKeepalive(ctx context.Context, req model.Message, peer string) error {
	if u.silentMessages {
		return nil
	}
	resp, err := model.NewResponse(200, "OK", echoHeaders(req), "")
	if err != nil {
		return err
	}
	return u.port.Send(ctx, resp, peer)
}

// answer handles one REGISTER: challenge it, or check the credential it
// carries and accept or refuse.
func (u *UAS) answer(ctx context.Context, req model.Message, peer string) error {
	base := echoHeaders(req)

	if _, ok := req.Header("Authorization"); !ok {
		opts := []auth.ChallengeOption{auth.WithOpaque()}
		if u.sm2Mode {
			opts = append(opts, auth.WithHashName("SM3"))
		}
		challenge, _, err := u.challenger.Challenge(u.realm, opts...)
		if err != nil {
			return err
		}
		resp, err := model.NewResponse(401, "Unauthorized",
			append(base, model.NewHeader("WWW-Authenticate", challenge)), "")
		if err != nil {
			return err
		}
		return u.port.Send(ctx, resp, peer)
	}

	if u.sm2Mode {
		return u.answerSM2(ctx, req, peer, base)
	}

	if err := u.responder.Verify(authorizationRequest{msg: req}, u.password); err != nil {
		resp, rerr := model.NewResponse(403, "Forbidden", base, "")
		if rerr != nil {
			return rerr
		}
		return u.port.Send(ctx, resp, peer)
	}
	hdrs := append(base,
		model.NewHeader("Expires", fmt.Sprintf("%d", u.grantedExpiry)),
		model.NewHeader("Date", "Thu, 24 Sep 2026 12:00:00 GMT"),
	)
	if contact, ok := req.Header("Contact"); ok {
		hdrs = append(hdrs, contact)
	}
	resp, err := model.NewResponse(200, "OK", hdrs, "")
	if err != nil {
		return err
	}
	return u.port.Send(ctx, resp, peer)
}

// answerSM2 handles a REGISTER in GB/T 35114 SM3+SM2 mode: the device's
// SM2 signature is verified first and, on success, the 200 OK is signed
// with the platform's private key.
func (u *UAS) answerSM2(ctx context.Context, req model.Message, peer string, base []model.Header) error {
	authValue, _ := req.Header("Authorization")
	fields, err := auth.ParseAuthorization(authValue.Value())
	if err != nil || !strings.EqualFold(fields.Alg, "SM3") {
		resp, rerr := model.NewResponse(403, "Forbidden", base, "")
		if rerr != nil {
			return rerr
		}
		return u.port.Send(ctx, resp, peer)
	}
	if err := u.responder.VerifyWithCredentials(authorizationRequest{msg: req}, *u.sm2DeviceCred); err != nil {
		resp, rerr := model.NewResponse(403, "Forbidden", base, "")
		if rerr != nil {
			return rerr
		}
		return u.port.Send(ctx, resp, peer)
	}
	sig, err := sm.SignSM2(u.sm2SigningPriv, []byte(fields.Response))
	if err != nil {
		return err
	}
	hdrs := append(base,
		model.NewHeader("Expires", fmt.Sprintf("%d", u.grantedExpiry)),
		model.NewHeader("Date", "Thu, 24 Sep 2026 12:00:00 GMT"),
		model.NewHeader("SecurityInfo", "SM2,"+hex.EncodeToString(sig)),
	)
	if contact, ok := req.Header("Contact"); ok {
		hdrs = append(hdrs, contact)
	}
	resp, err := model.NewResponse(200, "OK", hdrs, "")
	if err != nil {
		return err
	}
	return u.port.Send(ctx, resp, peer)
}

// Close releases the UAS listener.
func (u *UAS) Close() error { return u.inner.Close() }

// echoHeaders copies the transaction headers a response must echo: Via,
// From, To, Call-ID and CSeq.
func echoHeaders(req model.Message) []model.Header {
	out := make([]model.Header, 0, 5)
	for _, name := range []string{"Via", "From", "To", "Call-ID", "CSeq"} {
		if h, ok := req.Header(name); ok {
			out = append(out, h)
		}
	}
	return out
}

func mustHeader(msg model.Message, name string) string {
	if h, ok := msg.Header(name); ok {
		return h.Value()
	}
	return ""
}

// authorizationRequest adapts a domain message to the auth.Request the
// responder needs.
type authorizationRequest struct {
	msg model.Message
}

func (r authorizationRequest) Method() string { return r.msg.Method() }

func (r authorizationRequest) Authorization() string { return mustHeader(r.msg, "Authorization") }
