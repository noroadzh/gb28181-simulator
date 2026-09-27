package auth

import (
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"

	"github.com/your-org/gb28181-simulator/internal/adapter/sm"
)

// HashFunc returns the hex-encoded (lowercase) digest of its input.
// Default is MD5 per RFC 7616 §3 and GB/T 28181-2016 §L.2.
//
// Callers (notably Change 12 GB35114) replace HashFunc to plug in
// alternative algorithms such as SM3. The signature is intentionally
// `func(string) string` so the cryptographic core stays side-effect-free
// and easy to test.
type HashFunc func(string) string

// MD5Hash is the default hash function. It matches RFC 7616 §3 and
// RFC 2617 §3 exactly.
func MD5Hash(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// SHA1Hash is an optional hash function exposed to allow Change 12
// testing — kept here so the same swap-point works for SHA-1 as well.
func SHA1Hash(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// SM3Hash is the SM3 implementation required by GB 35114. It is wired in
// alongside MD5/SHA-1 so that Callers can pass it as a HashFunc without
// changing any signature.
func SM3Hash(s string) string {
	return sm.HashSM3(s)
}
