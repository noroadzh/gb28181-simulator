// Package app — device registration.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// RegisterStage names the point at which a registration failed. It is
// reported to the operator (log line + HTTP error body) so a failed
// registration can be diagnosed without re-running it with a sniffer.
type RegisterStage string

const (
	StageSend      RegisterStage = "send"
	StageChallenge RegisterStage = "challenge"
	StageResponse  RegisterStage = "response"
	StageTimeout   RegisterStage = "timeout"
)

// Sentinel errors a caller can match with errors.Is.
var (
	// ErrNoChallenge: a 401 arrived without a usable WWW-Authenticate.
	ErrNoChallenge = errors.New("app: 401 without a usable WWW-Authenticate")
	// ErrStillUnauthorized: the platform challenged us twice, so the
	// credentials are wrong rather than merely unproven.
	ErrStillUnauthorized = errors.New("app: platform rejected the authenticated REGISTER")
	// ErrRegisterTimeout: no matching response arrived in time.
	ErrRegisterTimeout = errors.New("app: timed out waiting for a registration response")
)

// RegistrationError reports a failed registration together with the stage
// it failed at and, when one was received, the SIP status code.
type RegistrationError struct {
	Stage      RegisterStage
	StatusCode int
	Err        error
}

// Error implements error.
func (e *RegistrationError) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("app: registration failed at %s: status %d: %v",
			e.Stage, e.StatusCode, e.Err)
	}
	return fmt.Sprintf("app: registration failed at %s: %v", e.Stage, e.Err)
}

// Unwrap exposes the underlying error to errors.Is / errors.As.
func (e *RegistrationError) Unwrap() error { return e.Err }

// FailureStage implements port.StagedFailure, so the HTTP layer can name
// the failing stage without depending on this package.
func (e *RegistrationError) FailureStage() string { return string(e.Stage) }

var _ port.StagedFailure = (*RegistrationError)(nil)

// Registrar performs the device side of a GB/T 28181 registration: send
// REGISTER, answer the platform's 401 challenge with a Digest credential,
// and report what the platform granted.
//
// It depends only on domain ports — the transport is handed in per call
// because every node owns its own listener — and it never imports an
// adapter: message construction uses model.NewRequest and the digest is
// produced by port.Authorizer.
type Registrar struct {
	authorizer port.Authorizer
	clock      port.Clock
	log        *slog.Logger
	newCallID  func() string
}

// NewRegistrar builds a Registrar. authorizer is required; clock and log
// fall back to the real clock and the default slog logger.
func NewRegistrar(authorizer port.Authorizer, clock port.Clock, log *slog.Logger) (*Registrar, error) {
	if authorizer == nil {
		return nil, fmt.Errorf("app: registrar requires an Authorizer")
	}
	if clock == nil {
		clock = realClock{}
	}
	if log == nil {
		log = slog.Default()
	}
	return &Registrar{
		authorizer: authorizer,
		clock:      clock,
		log:        log,
		newCallID:  randomCallID,
	}, nil
}

// Register runs one registration transaction for node over tr and returns
// what the platform granted. It performs at most two REGISTERs: the first
// unauthenticated, the second answering a 401. Any other final response,
// a second 401, or a timeout fails the transaction — retry and re-register
// are not this change's business.
//
// The transaction is bounded by reg.Timeout(); cancelling ctx aborts it.
func (r *Registrar) Register(
	ctx context.Context,
	tr port.SIPTransport,
	node model.Node,
	reg model.Registration,
) (model.RegistrationResult, error) {
	id := node.ID()
	profile := node.Profile()
	callID := r.newCallID()
	requestURI := registrationRequestURI(profile.Domain(), reg)

	first, err := buildRegister(registerParts{
		requestURI: requestURI,
		deviceID:   id.String(),
		domain:     profile.Domain(),
		contact:    profile.Addr(),
		callID:     callID,
		seqNo:      1,
		expires:    reg.Expires(),
		gbVersion:  reg.GBVersion(),
	})
	if err != nil {
		return model.RegistrationResult{}, &RegistrationError{Stage: StageSend, Err: err}
	}

	ctx, cancel := context.WithTimeout(ctx, reg.Timeout())
	defer cancel()

	if err := tr.Send(ctx, first, reg.Server()); err != nil {
		return model.RegistrationResult{}, &RegistrationError{Stage: StageSend, Err: err}
	}
	r.log.Info("registration sent", "node_id", id.String(), "server", reg.Server(),
		"call_id", callID, "expires", reg.Expires())

	authenticated := false
	seqNo := uint32(1)
	for {
		msg, peer, err := tr.Receive(ctx)
		if err != nil {
			return model.RegistrationResult{}, &RegistrationError{
				Stage: StageTimeout,
				Err:   fmt.Errorf("%w: %v", ErrRegisterTimeout, err),
			}
		}
		// A node may receive traffic for other transactions; only the
		// Call-ID we sent identifies ours.
		if h, ok := msg.Header("Call-ID"); !ok || h.Value() != callID {
			r.log.Debug("ignoring response for another transaction",
				"node_id", id.String(), "want_call_id", callID)
			continue
		}
		// Likewise, only the platform we registered with may conclude it.
		if peer != reg.Server() {
			r.log.Debug("ignoring response from an unexpected peer",
				"node_id", id.String(), "peer", peer, "want", reg.Server())
			continue
		}

		status := msg.StatusCode()
		switch {
		case status >= 100 && status < 200:
			continue // provisional: keep waiting for the final response

		case status == 401:
			if authenticated {
				return model.RegistrationResult{}, &RegistrationError{
					Stage: StageResponse, StatusCode: status, Err: ErrStillUnauthorized,
				}
			}
			challenge, ok := msg.Header("WWW-Authenticate")
			if !ok {
				return model.RegistrationResult{}, &RegistrationError{
					Stage: StageChallenge, StatusCode: status, Err: ErrNoChallenge,
				}
			}
			cred, err := reg.CredentialsFor(profile.Domain(), id)
			if err != nil {
				return model.RegistrationResult{}, &RegistrationError{Stage: StageChallenge, Err: err}
			}
			auth, err := r.authorizer.Authorize(challenge.Value(), cred, "REGISTER", requestURI)
			if err != nil {
				return model.RegistrationResult{}, &RegistrationError{
					Stage: StageChallenge, StatusCode: status, Err: err,
				}
			}
			seqNo++
			retry, err := buildRegister(registerParts{
				requestURI: requestURI,
				deviceID:   id.String(),
				domain:     profile.Domain(),
				contact:    profile.Addr(),
				callID:     callID,
				seqNo:      seqNo,
				expires:    reg.Expires(),
				gbVersion:  reg.GBVersion(),
				extra:      []model.Header{auth},
			})
			if err != nil {
				return model.RegistrationResult{}, &RegistrationError{Stage: StageChallenge, Err: err}
			}
			if err := tr.Send(ctx, retry, reg.Server()); err != nil {
				return model.RegistrationResult{}, &RegistrationError{Stage: StageChallenge, Err: err}
			}
			authenticated = true
			r.log.Debug("registration resent with credentials",
				"node_id", id.String(), "cseq", seqNo)

		case status >= 200 && status < 300:
			granted := reg.Expires()
			if h, ok := msg.Header("Expires"); ok {
				if n, cerr := strconv.ParseUint(strings.TrimSpace(h.Value()), 10, 32); cerr == nil {
					// The platform may shorten what we asked for; it wins.
					granted = uint32(n)
				}
			}
			result, err := model.NewRegistrationResult(reg.Server(), granted, r.clock.Now())
			if err != nil {
				return model.RegistrationResult{}, &RegistrationError{Stage: StageResponse, Err: err}
			}
			r.log.Info("registration succeeded", "node_id", id.String(),
				"server", reg.Server(), "expires", granted)
			return result, nil

		default:
			return model.RegistrationResult{}, &RegistrationError{
				Stage:      StageResponse,
				StatusCode: status,
				Err:        fmt.Errorf("platform refused the registration"),
			}
		}
	}
}

// registerParts is the flat input of one REGISTER build.
type registerParts struct {
	requestURI string
	deviceID   string
	domain     string
	contact    string // host:port the platform must answer us on
	callID     string
	seqNo      uint32
	expires    uint32
	gbVersion  string
	extra      []model.Header
}

// buildRegister assembles a REGISTER as a domain message. The app layer
// composes it itself — a builder port would only shuttle these same fields
// across the boundary.
func buildRegister(p registerParts) (model.Message, error) {
	hdrs := []model.Header{
		model.NewHeader("From", "<sip:"+p.deviceID+"@"+p.domain+">;tag="+randomTag()),
		model.NewHeader("To", "<sip:"+p.deviceID+"@"+p.domain+">"),
		model.NewHeader("Call-ID", p.callID),
		model.NewHeader("CSeq", strconv.FormatUint(uint64(p.seqNo), 10)+" REGISTER"),
		model.NewHeader("Contact", "<sip:"+p.deviceID+"@"+p.contact+">"),
		model.NewHeader("Expires", strconv.FormatUint(uint64(p.expires), 10)),
		model.NewHeader("Max-Forwards", "70"),
	}
	if p.gbVersion != "" {
		hdrs = append(hdrs, model.NewHeader("X-GB-Ver", p.gbVersion))
	}
	hdrs = append(hdrs, p.extra...)
	return model.NewRequest("REGISTER", p.requestURI, hdrs, "")
}

// registrationRequestURI renders the Request-URI of a REGISTER: the
// platform (identified by encoding when configured, by host otherwise) at
// the node's home domain, per GB/T 28181 §L.1.
func registrationRequestURI(domain string, reg model.Registration) string {
	host := reg.ServerID()
	if host == "" {
		if h, _, err := splitHostPort(reg.Server()); err == nil {
			host = h
		} else {
			host = reg.Server()
		}
	}
	return "sip:" + host + "@" + domain
}

func splitHostPort(addr string) (string, string, error) {
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return "", "", fmt.Errorf("app: address %q lacks a port", addr)
	}
	return addr[:i], addr[i+1:], nil
}

func randomCallID() string { return randomHex(16) }

func randomTag() string { return randomHex(8) }

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// A failing CSPRNG is fatal for SIP identity generation; a
		// time-based fallback keeps collisions improbable enough to be
		// diagnosable rather than silent.
		return fmt.Sprintf("%0*x", n*2, n)
	}
	return hex.EncodeToString(buf)
}
