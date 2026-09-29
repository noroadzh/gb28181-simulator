# Design

## Context

Change 2 (`core-sip-stack`) delivered RFC 2617 / RFC 7616 Digest authentication
with a `HashFunc` replacement point. Change 5 (`device-node-registration`) built
the device registration use-case on top of those ports. The current code paths
are MD5-only by default: `Challenger` returns `algorithm=MD5` in
`WWW-Authenticate`, `Responder.ComputeResponse` uses `crypto/md5`, and
`ErrUnknownAlgorithm` rejects anything else.

GB 35114-2022 defines Grade A security as SM2 elliptic-curve mutual authentication
combined with SM3 integrity hashing. The standard wires this into the existing
Digest challenge/response exchange rather than inventing a parallel protocol.

This change must add that extension without changing the default MD5 path: a
node whose profile does not configure SM2 credentials must see byte-identical
behaviour to the pre-change state.

## Goals / Non-Goals

**Goals:**

- Add `algorithm=SM3` / `algorithm=SM3-sess` to the Digest auth adapter.
- Add optional SM2 public-key signatures to REGISTER and `200 OK` flows.
- Keep plain MD5 behaviour byte-identical when SM2 is not configured.
- Keep all crypto behind `internal/adapter/sm` so the rest of the codebase
  never imports gmsm directly.

**Non-Goals:**

- SM4 symmetric encryption of MANSCDP or media payloads.
- SRTP key exchange via SM2 (media-plane security).
- Smart-card or HSM-backed key storage; keys are loaded from node config.
- GB 35114 B-grade audit-log signing.
- TLS transport (already a separate non-goal in Change 2).

## Decisions

### D1. Extend `HashFunc` seam to register SM3

`Challenger` already carries `HashFunc func(string) string`. Change 12 adds
a second seam: `HashName string` (default `"MD5"`). When `HashName == "SM3"`
the challenger uses `sm.HashSM3`; otherwise the existing `HashMD5` path is
taken. The public API `Challenger.Challenge(realm)` does not change shape;
only the algorithm name and computed hash differ.

`Responder.Verify` mirrors the same seam: it selects the hash function based
on the `algorithm=` field parsed from the incoming `Authorization` header.
If the algorithm is unknown it returns `ErrUnknownAlgorithm` exactly as today.

- **Rationale** — the `HashFunc` replacement point was created in Change 2
  *explicitly* for this change. Using it costs nothing and proves the original
  design foresight.
- **Alternative** — branch on `algorithm` inside `ComputeResponse` and add a
  new method `ComputeSM3Response` → rejected. It would duplicate the qop/nc
  /cnonce plumbing and leave two near-identical implementations to maintain.

### D2. SM2 signatures are a new SIP header, not a new auth scheme

GB 35114 specifies the SM2 signature as a new header (e.g. `SecurityInfo`
containing the VKEK envelope, plus a `P-Auth-Signature`-style header for the
raw SM2 signature). We add these headers on the *same* `Authorization` /
`WWW-Authenticate` round-trip; the client sends an extra header alongside the
Digest response and the server echoes its signature in `200 OK`.

The `Authorizer` port (`Authorize(challenge, cred, method, uri)`) gains an
optional `model.SecurityInfo` return value. When the local credential carries
an SM2 key pair the authorizer includes the signature header; otherwise the
returned `SecurityInfo` is nil and the caller falls back to plain Digest.

- **Rationale** — GB 35114 is an *extension* of Digest, not a replacement.
  Keeping one auth round-trip preserves compatibility with 2016 platforms.
- **Alternative** — a parallel SM2 handshake before Digest → rejected. That
  would require a new port, break existing app-level tests, and force every
  caller to sequence two transactions.

### D3. SM2 key pair lives in `model.Credentials`

`model.Credentials` (domain) gains optional fields `SM2PrivateKey []byte` and
`SM2PublicKey []byte`. A nil private key means "SM2 not configured"; the
adapter never sees gmsm types.

App-layer code (device registrar) never inspects these bytes. The `Authorizer`
implementation in `internal/adapter/auth` reads them, constructs a
`sm.Signatory`/`sm.Verifier`, and signs / verifies as needed.

- **Rationale** — keys are protocol artefacts, but they originate from node
  configuration, so they belong in the domain credential value object. Keeping
  them as raw bytes avoids importing gmsm into `internal/domain`.
- **Alternative** — put gmsm `ecdsa.PrivateKey` in the domain → rejected. It
  would force `internal/domain` to import gmsm, violating the layer rule.

### D4. All gmsm usage lives in `internal/adapter/sm`

A new thin package `internal/adapter/sm` re-exports exactly the symbols the
auth adapter needs:

```go
package sm

import "github.com/emmansun/gmsm/sm2"
import "github.com/emmansun/gmsm/sm3"

func HashSM3(input string) string { /* SM3 hex */ }
func SignSM2(privateKey, digest []byte) ([]byte, error)
func VerifySM2(publicKey, digest, signature []byte) bool
```

No other package imports gmsm. If we ever swap the implementation we only
touch `internal/adapter/sm`.

- **Rationale** — the same isolation pattern is used for `internal/adapter/auth`
  (crypto behind a domain port) and for the gosip transport. Consistent seams
  make future swaps cheap.
- **Alternative** — import gmsm directly in `auth` → rejected. Every package
  that imports `auth` would transitively depend on gmsm, including domain and
  app tests that mock credentials.

### D5. SM3 Note integrity is opt-in on the challenge

The `Challenger` gains `NoteHash bool` (default `false`). When true the
nonce payload in `WWW-Authenticate` carries an additional `opaque`-style note
field whose SM3 hash covers (nonce + realm + timestamp). The client echoes
the same field in `Authorization`; the server re-hashes and compares.

For MD5 peers the existing opaque/nonce flow is unchanged.

- **Rationale** — GB 35114 only requires Note integrity when SM3 is in use.
  Making it opt-in preserves the MD5 round-trip byte-for-byte.
- **Alternative** — always hash the Note → rejected. Would change the wire
  format for every existing MD5 peer.

### D6. SM2 is disabled by default in config and tests

`node.yaml` and `platform.yaml` schema gain optional `sm2_private_key` /
`sm2_public_key` fields (base64-encoded PEM). Every existing fixture that
does not set these fields exercises the MD5 path; no test fixture is modified
to include SM2 unless the test is explicitly about SM2 behaviour.

This keeps `go test -race -count=1 ./...` green for developers who have not
generated SM2 keys.

## Migration Plan

1. Add `github.com/emmansun/gmsm` to `go.mod` and create `internal/adapter/sm`.
2. Extend `Challenger` and `Responder` in `internal/adapter/auth` with the
   SM3 / SM2 seams described above.
3. Extend `model.Credentials` with optional SM2 key-pair bytes.
4. Update `internal/app/registrar.go` to pass credentials through (no new
   logic; the `Authorizer` port already handles the SM2 case internally).
5. Add unit tests at three levels:
   - `internal/adapter/sm` — sign / verify round-trip with a known SM2 key.
   - `internal/adapter/auth` — SM3 challenge, SM3 response, SM2 signature
     header emission and verification; MD5 baseline unchanged.
   - `internal/app` — device registration with SM2 credentials produces the
     expected SM3 challenge and SM2 signature headers; without credentials
     produces byte-identical MD5 headers.
6. Run `CGO_ENABLED=0 go test -race -count=1 ./...` and assert exit 0.
