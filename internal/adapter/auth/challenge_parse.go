package auth

import (
	"fmt"
	"strings"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// DefaultAlgorithm is what a challenge means when it omits algorithm=
// (RFC 7616 §3.3: MD5 is the default).
const DefaultAlgorithm = "MD5"

// ParseChallenge parses a WWW-Authenticate (or Proxy-Authenticate) header
// value received from a platform and returns it as a model.Challenge.
//
// It is the client-side counterpart of Challenger.Challenge: a device has
// to read whatever a third-party platform sent, whose formatting is not
// under our control — parameter order varies, values may or may not be
// quoted, and the scheme name may be in any case. A challenge without
// realm or nonce is rejected rather than returned half-filled, because a
// computed response over an empty nonce would be silently wrong.
func ParseChallenge(value string) (model.Challenge, error) {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(strings.ToLower(trimmed), "digest") {
		return model.Challenge{}, fmt.Errorf("%w: missing Digest scheme", ErrMalformedAuthorization)
	}
	params, err := parseDigestParams(strings.TrimSpace(trimmed[len("digest"):]))
	if err != nil {
		return model.Challenge{}, fmt.Errorf("%w: %v", ErrMalformedAuthorization, err)
	}
	realm := params["realm"]
	if realm == "" {
		return model.Challenge{}, fmt.Errorf("%w: challenge has no realm", ErrMalformedAuthorization)
	}
	nonce := params["nonce"]
	if nonce == "" {
		return model.Challenge{}, fmt.Errorf("%w: challenge has no nonce", ErrMalformedAuthorization)
	}
	alg := params["algorithm"]
	if alg == "" {
		alg = DefaultAlgorithm
	}
	// A platform may offer several qop values ("auth,auth-int"); GB/T 28181
	// mandates auth, so prefer it when present and keep the raw list
	// otherwise.
	qop := params["qop"]
	if strings.Contains(qop, ",") {
		for _, candidate := range strings.Split(qop, ",") {
			if strings.EqualFold(strings.TrimSpace(candidate), "auth") {
				qop = "auth"
				break
			}
		}
	}
	ch, err := model.NewChallenge(realm, nonce, alg, params["opaque"], qop)
	if err != nil {
		return model.Challenge{}, err
	}
	if note := params["note"]; note != "" {
		ch = ch.WithNote(note)
	}
	return ch, nil
}

// ChallengeFromAuthHeader is a higher-level ParseChallenge that restricts
// the algorithm to the set understood by this stack: MD5, MD5-sess, SM3,
// SM3-sess. It is the preferred entry point when the caller knows the input
// came from a WWW-Authenticate (or 401) header and wants to fail fast on
// unsupported algorithms instead of silently producing a wrong response.
func ChallengeFromAuthHeader(value string) (model.Challenge, error) {
	ch, err := ParseChallenge(value)
	if err != nil {
		return model.Challenge{}, err
	}
	switch strings.ToUpper(ch.Algorithm()) {
	case "", "MD5", "MD5-SESS", "SM3", "SM3-SESS":
		return ch, nil
	default:
		return model.Challenge{}, fmt.Errorf("%w: %s", ErrUnknownAlgorithm, ch.Algorithm())
	}
}

// parseDigestParams reads "key=value" pairs separated by commas, ignoring
// surrounding whitespace. Values may be quoted or bare tokens.
func parseDigestParams(body string) (map[string]string, error) {
	out := make(map[string]string)
	i := 0
	for i < len(body) {
		for i < len(body) && (body[i] == ' ' || body[i] == '\t' || body[i] == ',') {
			i++
		}
		if i >= len(body) {
			break
		}
		j := i
		for j < len(body) && body[j] != '=' && body[j] != ',' {
			j++
		}
		if j >= len(body) || body[j] != '=' {
			return nil, fmt.Errorf("expected '=' after key at offset %d", i)
		}
		key := strings.ToLower(strings.TrimSpace(body[i:j]))
		i = j + 1
		if i < len(body) && body[i] == '"' {
			i++
			start := i
			for i < len(body) && body[i] != '"' {
				if body[i] == '\\' {
					i++
				}
				i++
			}
			if i >= len(body) {
				return nil, fmt.Errorf("unterminated quoted value for %q", key)
			}
			out[key] = body[start:i]
			i++ // closing quote
			continue
		}
		start := i
		for i < len(body) && body[i] != ',' {
			i++
		}
		out[key] = strings.TrimSpace(body[start:i])
	}
	return out, nil
}
