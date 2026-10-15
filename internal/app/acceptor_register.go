// REGISTER handling for a serving platform node: authenticating the
// downstream device that calls itself, then either remembering it in the
// online table (200 OK with a negotiated lifetime) or forgetting it again
// when it asks for no lifetime at all.
//
// The whole cluster lives here rather than in acceptor.go because it is the
// one part of SIP traffic whose correctness is a security property rather
// than a protocol-plumbing one: GB/T 28181 §L.2 forbids re-challenging a
// response that read cleanly but did not match, so the malformed-vs-bad
// distinction below is a rule, not an optimisation.
package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// handle authenticates a REGISTER and, once it passes, records or forgets the
// device behind it. It is the platform half of the node; the device half —
// the side that dials out and registers upward — lives in device_registrar.go.
func (a *Acceptor) handle(ctx context.Context, p *platform, req model.Message, peer string) (model.Message, error) {
	username := requestUser(req)
	if username == "" {
		// Without a caller there is nobody to look up and nothing worth
		// challenging: refuse rather than hand out a nonce to a stranger.
		a.log.Warn("refusing a REGISTER without a caller", "node_id", p.id.String())
		return buildResponse(403, "Forbidden", req, nil, "")
	}
	if _, ok := req.Header("Authorization"); !ok {
		a.log.Debug("challenging a REGISTER", "node_id", p.id.String(), "username", username)
		return a.challenge(req, p)
	}
	cred, ok := a.creds.Lookup(p.id, username)
	if !ok {
		a.log.Warn("refusing a REGISTER for an unknown account",
			"node_id", p.id.String(), "username", username)
		return buildResponse(403, "Forbidden", req, nil, "")
	}
	if err := a.authenticator.Verify(req, cred); err != nil {
		// A header we cannot read may be a transient botch, so it is
		// worth another challenge; one that reads and does not match
		// is not (GB/T 28181 §L.2 forbids re-challenging).
		if errors.Is(err, port.ErrMalformedCredentials) {
			a.log.Warn("re-challenging an unreadable Authorization",
				"node_id", p.id.String(), "username", username, "error", err.Error())
			return a.challenge(req, p)
		}
		a.log.Warn("refusing a REGISTER with a bad response",
			"node_id", p.id.String(), "username", username, "error", err.Error())
		return buildResponse(403, "Forbidden", req, nil, "")
	}
	return a.grant(ctx, p, req, username, peer, cred)
}

// challenge answers 401 with a fresh WWW-Authenticate for the node's realm.
func (a *Acceptor) challenge(req model.Message, p *platform) (model.Message, error) {
	ch, err := a.challenger.Challenge(p.realm)
	if err != nil {
		return model.Message{}, fmt.Errorf("app: challenge for node %s: %w", p.id, err)
	}
	value := fmt.Sprintf(`Digest realm=%q, nonce=%q, qop="auth", algorithm=%s`,
		ch.Realm(), ch.Nonce(), ch.Algorithm())
	if ch.Opaque() != "" {
		value += fmt.Sprintf(`, opaque=%q`, ch.Opaque())
	}
	// GB 35114 Note integrity: echo the SM3-hashed Note only when the
	// Challenger emitted one, so MD5 peers see the old byte-identical
	// 401.
	if note := ch.Note(); note != "" {
		value += fmt.Sprintf(`, Note=%q`, note)
	}
	return buildResponse(401, "Unauthorized", req, []model.Header{
		model.NewHeader("WWW-Authenticate", value),
	}, "")
}

// grant answers 200 OK and records the device — or, when the request asks
// for no lifetime at all, forgets it instead.
func (a *Acceptor) grant(
	ctx context.Context,
	p *platform,
	req model.Message,
	username string,
	peer string,
	cred model.Credentials,
) (model.Message, error) {
	now := a.clock.Now()
	requested, hasExpires := requestedExpires(req)
	if hasExpires && requested == 0 {
		if err := a.devices.Remove(ctx, p.id, username); err != nil {
			a.log.Warn("cannot forget a device that asked to leave",
				"node_id", p.id.String(), "device_id", username, "error", err.Error())
		} else {
			a.log.Info("downstream unregistered", "node_id", p.id.String(), "device_id", username)
		}
		a.notifyCatalogChange(ctx, p)
		hdrs := []model.Header{
			model.NewHeader("Expires", "0"),
			model.NewHeader("Date", httpDate(now)),
		}
		if signer, ok := a.authenticator.(port.SecurityInfoSigner); ok {
			if h, ok := signer.SignSecurityInfo(req, cred); ok {
				hdrs = append(hdrs, h)
			}
		}
		return buildResponse(200, "OK", req, hdrs, "")
	}

	granted := p.policy.Negotiate(requested)
	contact := ""
	if h, ok := req.Header("Contact"); ok {
		contact = h.Value()
	}
	gbVersion := ""
	if h, ok := req.Header("X-GB-Ver"); ok {
		gbVersion = h.Value()
	}
	// The address the request came from is what the platform can answer;
	// the Contact is only what the device claims, so it is stored but not
	// trusted as the address.
	addr := peer
	if addr == "" {
		addr = hostOfContact(contact)
	}
	dev, err := model.NewDownstreamDevice(model.DownstreamDeviceParams{
		DeviceID:  username,
		Addr:      addr,
		Contact:   contact,
		Transport: transportOf(req),
		GBVersion: gbVersion,
		Now:       now,
	})
	if err != nil {
		return model.Message{}, fmt.Errorf("app: record device %s: %w", username, err)
	}
	if err := a.devices.Upsert(ctx, p.id, dev.WithGranted(granted, now)); err != nil {
		return model.Message{}, fmt.Errorf("app: record device %s: %w", username, err)
	}
	a.log.Info("downstream registered", "node_id", p.id.String(),
		"device_id", username, "expires", granted)
	a.notifyCatalogChange(ctx, p)

	hdrs := []model.Header{
		model.NewHeader("Expires", strconv.FormatUint(uint64(granted), 10)),
		model.NewHeader("Date", httpDate(now)),
	}
	if contact != "" {
		hdrs = append(hdrs, model.NewHeader("Contact", contact))
	}
	// X-GB-Ver is echoed only when the peer declared it: a 2016 REGISTER
	// gets today's byte-identical answer, a 2022 one learns the negotiated
	// version (GB/T 28181-2022 Annex I).
	if gbVersion != "" {
		hdrs = append(hdrs, model.NewHeader("X-GB-Ver", gbVersion))
	}
	if signer, ok := a.authenticator.(port.SecurityInfoSigner); ok {
		if h, ok := signer.SignSecurityInfo(req, cred); ok {
			hdrs = append(hdrs, h)
		}
	}
	return buildResponse(200, "OK", req, hdrs, "")
}

// requestedExpires reads the lifetime the request asks for. The second
// result is false when the header is absent, which is not the same thing as
// asking for zero: zero is an explicit goodbye.
func requestedExpires(req model.Message) (uint32, bool) {
	h, ok := req.Header("Expires")
	if !ok {
		return 0, false
	}
	value := strings.TrimSpace(h.Value())
	if value == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(n), true
}

// sipUser extracts the user part of a SIP URI inside a header value such as
// `<sip:34020000011310000001@3402000000>;tag=abc`.
func sipUser(value string) string {
	start := strings.Index(value, "<sip:")
	body := value
	if start >= 0 {
		body = value[start+len("<sip:"):]
	} else if i := strings.Index(value, "sip:"); i >= 0 {
		body = value[i+len("sip:"):]
	} else {
		return ""
	}
	if i := strings.IndexAny(body, "@>"); i > 0 {
		return body[:i]
	}
	return ""
}

// transportOf reads the transport the request arrived over from its Via, so
// the online table can say how to reach the device.
func transportOf(req model.Message) string {
	h, ok := req.Header("Via")
	if !ok {
		return ""
	}
	// Via: SIP/2.0/UDP 127.0.0.1:15060;branch=...
	fields := strings.Fields(h.Value())
	for _, f := range fields {
		parts := strings.Split(f, "/")
		if len(parts) == 3 {
			return strings.ToLower(parts[2])
		}
	}
	return ""
}

// hostOfContact extracts "host:port" from a Contact such as
// `<sip:34020000011310000001@127.0.0.1:15060>`.
func hostOfContact(value string) string {
	i := strings.LastIndex(value, "@")
	if i < 0 {
		return ""
	}
	rest := value[i+1:]
	end := strings.IndexAny(rest, ">;")
	if end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest)
}
