# Tasks

## 1. SM2/SM3 Adapter Foundation

- [x] 1.1 Add `github.com/emmansun/gmsm` to `go.mod` (pure Go, no CGO). Verify: `CGO_ENABLED=0 go build ./...` exits 0
- [x] 1.2 Add `internal/adapter/sm` package with `HashSM3`, `SignSM2`, `VerifySM2` wrappers around gmsm. Verify: `go test ./internal/adapter/sm/ -count=1` passes with a known SM2 key round-trip

## 2. SM3 Digest Extension

- [x] 2.1 Extend `Challenger` (`internal/adapter/auth/challenger.go`) with `HashName string` (default `"MD5"`). When `"SM3"` select `sm.HashSM3` for nonce and opaque hashing; existing MD5 path unchanged. Verify: unit test with `HashName: "MD5"` produces identical output to pre-change baseline
- [x] 2.2 Extend `Responder.Verify` (`internal/adapter/auth/responder.go`) to select hash function based on `algorithm=` field from `Authorization`; `algorithm=SM3` and `algorithm=SM3-sess` use `sm.HashSM3`, `algorithm=MD5` / `algorithm=MD5-sess` use existing MD5 path, anything else returns `ErrUnknownAlgorithm`. Verify: SM3 response round-trip test passes; unknown algorithm returns sentinel

## 3. SM2 Mutual Authentication Headers

- [x] 3.1 Extend `model.Credentials` (`internal/domain/model/credentials.go`) with optional `SM2PrivateKey []byte` and `SM2PublicKey []byte` fields (nil = not configured). Verify: existing constructors and tests unchanged
- [x] 3.2 Extend `Authorizer` (`internal/adapter/auth/authorizer.go`) so that when `cred.SM2PrivateKey` is non-nil it computes an SM2 signature over the Digest response and appends a new `SecurityInfo` header (VKEK envelope) to the returned `Authorization` header value. Verify: test fixture with SM2 keys produces an Authorization carrying both Digest response and SM2 signature
- [x] 3.3 Extend `Authenticator` / responder path so that a `200 OK` to a REGISTER that carried `SecurityInfo` may include a server SM2 signature in a new `SecurityInfo` response header when the server credential has a public key. Verify: acceptor REGISTER test with both sides configured for SM2 sees the signature header

## 4. SM3 Note Integrity

- [x] 4.1 Extend `Challenger` so that when `HashName == "SM3"` the `WWW-Authenticate` Note field contains an SM3 hash of (nonce + realm + timestamp). The echoed client Note is re-hashed and compared on the server side. Verify: SM3 challenge round-trip with Note integrity passes; forged Note is rejected
- [x] 4.2 Make SM3 Note opt-in: when `HashName == "MD5"` the Note field is the existing opaque random string, byte-identical to pre-change output. Verify: MD5 challenge golden test unchanged

## 5. Acceptor Wiring (version-gated)

- [x] 5.1 In `handleRegister` (`internal/app/acceptor.go`), after extracting the inbound `Authorization` header, if the peer advertised `algorithm=SM3` and the configured server credential has an SM2 public key, answer with SM2 signature in `SecurityInfo`. Verify: acceptor test with SM3-capable peer receives SM2 signature header
- [x] 5.2 Gate SM3/SM2 behaviour behind a per-peer negotiated security profile: peers without SM3 capability never see SM2 headers. Verify: MD5 peer REGISTER response is byte-identical to pre-change

## 6. Regression & Integration

- [x] 6.1 Run the full suite and confirm every pre-existing auth / registration / acceptor test still passes unchanged. Verify: `CGO_ENABLED=0 go test -race -count=1 ./...` exits 0
- [x] 6.2 Add one end-to-end test: 2022 device with SM2 credentials registers against platform-large carrying `algorithm=SM3`, receives `SecurityInfo` SM2 signature in `200 OK`, and a subsequent MD5-registered device sees no SM2 headers. Verify: test in `internal/adapter/siptest/` passes

## 7. Verify & Archive

- [x] 7.1 Run `openspec validate --changes "gb35114-security" --strict`. Verify: validator exits 0 with no errors
- [x] 7.2 Run verify-change against artifacts and confirm all tasks are complete. Verify: verification output lists no incomplete tasks
- [ ] 7.3 Archive the change to `openspec/changes/archive/` and update `docs/roadmap-15-steps.md` status for #13, #14, #15. Verify: `openspec list` no longer shows the change; roadmap table shows #13/#14/#15 as next
