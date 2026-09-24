package audit

import (
	"bytes"
	"regexp"
)

// authResponseRE matches the response="…" attribute inside a digest
// Authorization / WWW-Authenticate header. It is intentionally simple — the
// value may contain commas and quoted-pairs, but for GB/T 28181 the response
// is always a 32-char hex MD5/SHA-256 string with no escapes, so a plain
// regex suffices.
//
// RFC 2617 / RFC 7616 grammar:
//
//	response       = "response" "=" quoted-string
//	quoted-string  = ( <"> *(qdtext) <"> )
//	qdtext         = LWS / %x21 / %x23-5B / %x5D-7E / UTF8-non-ascii
//
// We replace the entire quoted-string with ***REDACTED***.
var authResponseRE = regexp.MustCompile(`([Rr]esponse=")[^"]*(")`)

// RedactAuthHeader replaces the response="…" field inside any
// Authorization / WWW-Authenticate header with response="***REDACTED***".
//
// It is safe to call on already-redacted or empty buffers; if no match is
// found the input is returned as-is (no allocation in the common "no auth
// header" case).
//
// This function is exported so that Change 14's web UI can sanitise any
// captured pcap bytes before exposing them to operators.
func RedactAuthHeader(buf []byte) []byte {
	if len(buf) == 0 {
		return buf
	}
	if !bytes.ContainsRune(buf, 'r') && !bytes.ContainsRune(buf, 'R') {
		return buf
	}
	out := authResponseRE.ReplaceAll(buf, []byte(`${1}***REDACTED***${2}`))
	return out
}
