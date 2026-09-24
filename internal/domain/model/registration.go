// Package model — Registration.
package model

import (
	"fmt"
	"net"
	"strings"
	"time"
)

// Defaults applied when a registration leaves a field unset. They match
// GB/T 28181 practice: an hour of validity over UDP, and a bounded wait so
// a start call cannot hang on a dead platform.
const (
	DefaultExpires   = 3600
	DefaultTimeout   = 5 * time.Second
	DefaultTransport = "udp"
)

// Registration is an immutable description of how a node registers with an
// upstream platform: where the platform is, which credentials to answer its
// challenge with, and how long to wait. It is optional on a node — a zero
// Registration is never constructed directly; callers use
// NewRegistration, and the absence of one is expressed by nil on the
// profile.
type Registration struct {
	server    string // "host:port" of the upstream platform
	serverID  string // 20-digit platform id; empty means "use the host"
	username  string // defaults to the node id when empty
	password  string
	gbVersion string // optional X-GB-Ver value
	expires   uint32 // seconds requested from the platform
	timeout   time.Duration
	transport string // "udp" / "tcp"
}

// RegistrationParams is the flat input for NewRegistration. Zero values take
// the documented defaults (expires 3600, timeout 5s, transport udp);
// Server and Password have no defaults and are required.
type RegistrationParams struct {
	Server    string
	ServerID  string
	Username  string
	Password  string
	GBVersion string
	Expires   uint32
	Timeout   time.Duration
	Transport string
}

// NewRegistration validates params and returns the immutable Registration.
// It rejects — rather than silently repairing — values that would make a
// registration impossible: a server address without a port, a missing
// password, a non-positive expires or timeout, and an unknown transport.
func NewRegistration(p RegistrationParams) (Registration, error) {
	server := strings.TrimSpace(p.Server)
	if server == "" {
		return Registration{}, fmt.Errorf("model: empty registration server")
	}
	if _, _, err := net.SplitHostPort(server); err != nil {
		return Registration{}, fmt.Errorf("model: registration server %q: %w", server, err)
	}
	if p.Password == "" {
		return Registration{}, fmt.Errorf("model: registration for %s has no password", server)
	}
	if p.ServerID != "" {
		if _, err := ParseNodeID(p.ServerID); err != nil {
			return Registration{}, fmt.Errorf("model: registration server id: %w", err)
		}
	}
	expires := p.Expires
	if expires == 0 {
		expires = DefaultExpires
	}
	timeout := p.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	if timeout < 0 {
		return Registration{}, fmt.Errorf("model: negative registration timeout %v", timeout)
	}
	transport := strings.ToLower(strings.TrimSpace(p.Transport))
	if transport == "" {
		transport = DefaultTransport
	}
	if transport != "udp" && transport != "tcp" {
		return Registration{}, fmt.Errorf("model: unsupported registration transport %q", p.Transport)
	}
	return Registration{
		server:    server,
		serverID:  strings.TrimSpace(p.ServerID),
		username:  strings.TrimSpace(p.Username),
		password:  p.Password,
		gbVersion: strings.TrimSpace(p.GBVersion),
		expires:   expires,
		timeout:   timeout,
		transport: transport,
	}, nil
}

// HasRegistration reports whether r was produced by NewRegistration. A
// zero value is never valid, so this is how callers test for "this node
// does not register".
func (r Registration) HasRegistration() bool { return r.server != "" }

// Server returns the upstream platform address ("host:port").
func (r Registration) Server() string { return r.server }

// ServerID returns the platform id used to build the Request-URI, or ""
// when the configuration left it out.
func (r Registration) ServerID() string { return r.serverID }

// Username returns the configured authentication user, or "" when the node
// id should be used instead.
func (r Registration) Username() string { return r.username }

// GBVersion returns the optional X-GB-Ver value; "" means "send no such
// header".
func (r Registration) GBVersion() string { return r.gbVersion }

// Expires returns the lifetime this node asks the platform for, in seconds.
func (r Registration) Expires() uint32 { return r.expires }

// Timeout returns how long a registration transaction may take.
func (r Registration) Timeout() time.Duration { return r.timeout }

// Transport returns "udp" or "tcp".
func (r Registration) Transport() string { return r.transport }

// CredentialsFor builds the credentials used to answer a challenge issued
// for realm. The realm only becomes known when the platform challenges us,
// so it is a parameter rather than part of the configuration. Username
// falls back to the node id, which is what GB/T 28181 platforms expect.
func (r Registration) CredentialsFor(realm string, id NodeID) (Credentials, error) {
	user := r.username
	if user == "" {
		user = id.String()
	}
	return NewCredentials(user, realm, r.password)
}

// String renders a log-safe one-line summary; the password is never
// included.
func (r Registration) String() string {
	return fmt.Sprintf("Registration<server=%s server_id=%s expires=%d transport=%s timeout=%v>",
		r.server, r.serverID, r.expires, r.transport, r.timeout)
}

// RegistrationResult is what a completed registration leaves behind: where
// the node registered, the lifetime the platform actually granted (it may
// shorten what we asked for), and when it happened.
type RegistrationResult struct {
	server        string
	grantedExpiry uint32
	registeredAt  time.Time
}

// NewRegistrationResult validates and returns a registration outcome.
// grantedExpiry of 0 means the platform did not state one, in which case
// the requested lifetime stands.
func NewRegistrationResult(server string, grantedExpiry uint32, registeredAt time.Time) (RegistrationResult, error) {
	if strings.TrimSpace(server) == "" {
		return RegistrationResult{}, fmt.Errorf("model: empty server in registration result")
	}
	if registeredAt.IsZero() {
		return RegistrationResult{}, fmt.Errorf("model: zero registration time")
	}
	return RegistrationResult{
		server:        strings.TrimSpace(server),
		grantedExpiry: grantedExpiry,
		registeredAt:  registeredAt,
	}, nil
}

// HasResult reports whether r was produced by NewRegistrationResult. A
// zero value carries no server, so it cannot be mistaken for a real
// outcome.
func (r RegistrationResult) HasResult() bool { return r.server != "" }

// Server returns the platform the node registered with.
func (r RegistrationResult) Server() string { return r.server }

// GrantedExpiry returns the lifetime the platform granted, in seconds; 0
// means "not stated".
func (r RegistrationResult) GrantedExpiry() uint32 { return r.grantedExpiry }

// RegisteredAt returns when the registration completed.
func (r RegistrationResult) RegisteredAt() time.Time { return r.registeredAt }

// String renders a log-safe one-line summary.
func (r RegistrationResult) String() string {
	return fmt.Sprintf("RegistrationResult<server=%s granted=%d at=%s>",
		r.server, r.grantedExpiry, r.registeredAt.Format(time.RFC3339))
}
