# Spec Delta: GB 35114 Security

## ADDED Requirements

### Requirement: SM3 Digest algorithm

The SIP Digest authentication adapter MUST support `algorithm=SM3` and
`algorithm=SM3-sess` in addition to the existing `algorithm=MD5` and
`algorithm=MD5-sess`. When the peer advertises SM3 the response field MUST be
computed with SM3 instead of MD5 while preserving the same qop/nc/cnonce
plumbing. An `algorithm=` value other than the four recognised names MUST
return `ErrUnknownAlgorithm`.

#### Scenario: SM3 challenge produces SM3 response

- **WHEN** the platform issues a `WWW-Authenticate` with `algorithm=SM3`
- **THEN** a compliant client answers with `algorithm=SM3` in its
  `Authorization` header
- **AND** `Responder.Verify` accepts the response when the password matches

#### Scenario: SM3-sess session-key variant

- **WHEN** the platform issues a `WWW-Authenticate` with `algorithm=SM3-sess`
  and a previous MD5-sess-style session key is available
- **THEN** the client answers with `algorithm=SM3-sess`
- **AND** verification uses SM3 over the session key exactly as MD5-sess uses
  MD5

#### Scenario: unknown algorithm is rejected

- **WHEN** the platform issues a `WWW-Authenticate` with `algorithm=SHA-256`
- **THEN** `Responder.Verify` returns `ErrUnknownAlgorithm`

### Requirement: SM2 mutual authentication signatures

GB 35114 Grade A requires an SM2 public-key signature on the Digest
exchange. When a device node's credentials carry an SM2 key pair the REGISTER
request MUST include a `SecurityInfo` header containing an SM2 signature of
the Digest response. When the platform's credentials carry an SM2 public key
the `200 OK` to REGISTER MUST include a matching `SecurityInfo` header. Nodes
without an SM2 key pair MUST NOT send or require `SecurityInfo` headers.

#### Scenario: device with SM2 keys sends SecurityInfo on REGISTER

- **WHEN** a device node is configured with an SM2 private key and sends
  REGISTER with SM3 Digest
- **THEN** the REGISTER carries a `SecurityInfo` header whose payload is a
  valid SM2 signature over the Digest response

#### Scenario: platform verifies SM2 signature and answers with its own

- **WHEN** the platform receives a REGISTER with a valid SM2 signature
- **THEN** the `200 OK` includes a `SecurityInfo` header containing the
  platform's SM2 signature
- **AND** the device registrar verifies the platform's SM2 signature before
  marking registration as successful

#### Scenario: node without SM2 keys uses plain Digest

- **WHEN** a device node has no SM2 credentials configured
- **THEN** its REGISTER carries no `SecurityInfo` header
- **AND** the `200 OK` carries no `SecurityInfo` header

### Requirement: SM3 Note integrity

The `WWW-Authenticate` Note field MUST be integrity-protected with SM3 when
the peer negotiates `algorithm=SM3`. The server computes the Note as
`SM3(nonce + realm + timestamp)` and the client echoes the value. The server
re-hashes and compares; a mismatch MUST be treated as a malformed
Authorization. For `algorithm=MD5` the existing opaque random-string Note is
used byte-for-byte.

#### Scenario: SM3 Note integrity check

- **WHEN** the platform sends a `WWW-Authenticate` with `algorithm=SM3` and a
  SM3-hashed Note
- **THEN** the client echoes the same Note value
- **AND** the server accepts the response when the Note recomputes to the same
  SM3 hash

#### Scenario: forged SM3 Note is rejected

- **WHEN** the client echoes a tampered Note value
- **THEN** the server returns `ErrMalformedAuthorization`

#### Scenario: MD5 Note is unchanged

- **WHEN** the platform sends a `WWW-Authenticate` with `algorithm=MD5`
- **THEN** the Note field is the existing opaque random string, byte-identical
  to the pre-Change-12 output

### Requirement: default-off configuration

SM2 keys and SM3 hashing MUST be disabled by default. A node whose profile
does not declare SM2 credentials MUST behave exactly as it did before this
change: MD5 Digest auth, no SM2 headers, no SM3 headers. All existing test
fixtures that omit SM2 configuration MUST pass without modification.

#### Scenario: out-of-the-box node uses MD5

- **WHEN** a node is started with no SM2 credentials in its configuration
- **THEN** its REGISTER uses `algorithm=MD5`
- **AND** the `200 OK` uses `algorithm=MD5`
- **AND** no `SecurityInfo` header appears in either direction
