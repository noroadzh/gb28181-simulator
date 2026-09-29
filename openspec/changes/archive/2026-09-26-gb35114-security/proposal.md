# Proposal: GB 35114 Security Extension

## Context

Change 2 (`core-sip-stack`) delivered RFC 2617 / RFC 7616 Digest authentication
with a `HashFunc` replacement point in `internal/adapter/auth`. Change 4
(`node-abstraction`) exposed the `Authorizer` port so the app layer never
touches crypto details directly. Device registration (Change 5) uses those
ports today with MD5.

The current `Challenger` and `Responder` deliberately only understand
`algorithm=MD5` and `algorithm=MD5-sess`. `ErrUnknownAlgorithm` is returned
for anything else. This is intentional: GB 35114 A-grade mutual authentication
(SM2/SM3) is deferred to this change.

## What Changes

Implement GB 35114-2022 Grade A security for GB/T 28181 SIP signalling:

1. **SM3 Digest algorithm** — extend the auth adapter so that `algorithm=SM3`
   (and `algorithm=SM3-sess`) is recognised alongside MD5. The `HashFunc`
   field already on `Challenger` is the extension seam; we register the SM3
   implementation there and update error handling so the existing MD5 path is
   byte-identical when SM3 is not configured.

2. **SM2 mutual authentication** — the 35114 spec mandates a two-round
   handshake using SM2 public-key signatures over the Digest exchange. We add
   an optional SM2 key pair to device credentials, emit the SM2 signature as
   a new SIP header on REGISTER, and verify the server's SM2 signature in the
   `200 OK` challenge. Peers without a configured SM2 key pair continue to
   use plain Digest exactly as today.

3. **SM3 integrity for the Note/nonce payload** — GB 35114 requires that the
   `WWW-Authenticate` Note field be integrity-protected with SM3. We extend
   the existing nonce generator to produce an SM3-hashed note when the peer
   advertises SM3 support, and validate the client's echoed note with the same
   hash.

4. **gmsm dependency** — `github.com/emmansun/gmsm` (pure Go, no CGO) is the
   designated SM2/SM3 implementation, already referenced in
   `openspec/config.yaml`. We vendor the module and expose it only behind the
   `internal/adapter/sm` package so the rest of the codebase never imports
   gmsm directly.

## Non-Goals

- SM2/SM3 for media-plane SRTP key exchange (future).
- SM4 symmetric encryption for MANSCDP XML payloads.
- Smart-card credential storage (assumes keys are already loaded into memory
  from the node configuration).
- GB 35114 B-grade (non-repudiation / audit log signing).

## Why Not Alternate Approaches

- **Use `crypto/sm2` / `crypto/sm3` from Go stdlib** — not yet available in
  the Go version this project targets. `gmsm` is the only mature pure-Go
  implementation.
- **Introduce a new SM2 auth flow that bypasses Digest** — would fragment the
  auth port surface. GB 35114 is defined as an extension of the Digest
  exchange; mirroring that design keeps the `Authorizer` / `Challenger`
  interfaces stable.
- **Make SM2 mandatory** — would break every existing fixture and test that
  does not configure a key pair. The spec itself makes SM2 optional at the
  endpoints; a missing key pair falls back to plain SM3 Digest.

## User-Visible Behaviour

- A device node whose profile does not declare SM2 credentials behaves exactly
  as it does today: MD5 Digest auth, no SM2 headers.
- A device node with SM2 credentials advertises `algorithm=SM3` in the
  `WWW-Authenticate` challenge, sends `Authorization` with SM3 response plus
  an SM2 signature header, and verifies the server's SM2 signature in `200 OK`.
- A 2016-registered peer that does not negotiate SM3 never sees SM2 headers.
