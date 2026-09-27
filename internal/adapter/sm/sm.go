// Package sm is the single seam between this codebase and the
// github.com/emmansun/gmsm SM2/SM3 implementation.
//
// GB 35114-2022 Grade A security needs two primitives:
//
//   - SM3, the 256-bit hash (GM/T 0004), used as the Digest "algorithm"
//     replacement for MD5 and for Note-field integrity.
//   - SM2, the elliptic-curve signature scheme (GM/T 0003), used for the
//     mutual-authentication signatures carried in the SecurityInfo header.
//
// No other package in this module may import gmsm. Swapping the SM library
// (or dropping it behind a pure-Go fallback) only touches this file.
//
// Key material crosses the adapter boundary as raw bytes so the domain layer
// never imports gmsm types (design decision D3 of the gb35114-security
// change):
//
//   - a private key is the 32-byte big-endian scalar D;
//   - a public key is the 65-byte uncompressed point (0x04 || X || Y);
//   - a signature is the ASN.1 SM2 signature encoding (GB/T 35275).
package sm

import (
	"crypto/ecdsa"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"

	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/sm3"
)

// Key size constants for the SM2 curve (GM/T 0003.5, 256-bit prime field).
const (
	// PrivateKeySize is the byte length of the scalar D.
	PrivateKeySize = 32
	// PublicKeySize is the byte length of the uncompressed point encoding.
	PublicKeySize = 65
)

// ErrBadPrivateKey is returned when the supplied private-key bytes are not a
// valid 32-byte SM2 scalar.
var ErrBadPrivateKey = errors.New("sm: cannot parse SM2 private key")

// ErrBadPublicKey is returned when the supplied public-key bytes are not a
// valid 65-byte uncompressed SM2 point.
var ErrBadPublicKey = errors.New("sm: cannot parse SM2 public key")

// HashSM3 returns the lowercase hex SM3 digest of input.
//
// The name mirrors the existing auth.HashMD5 helper so the Digest adapter can
// switch between the two via its HashFunc seam without any call-shape change.
func HashSM3(input string) string {
	digest := sm3.New()
	digest.Write([]byte(input))
	return fmt.Sprintf("%x", digest.Sum(nil))
}

// HashSM3Bytes returns the raw 32-byte SM3 digest of data. Used where the
// Note-integrity check compares digests rather than their hex rendering.
func HashSM3Bytes(data []byte) []byte {
	digest := sm3.New()
	digest.Write(data)
	return digest.Sum(nil)
}

// SignSM2 signs digest with a raw 32-byte SM2 private-key scalar and returns
// the ASN.1 SM2 signature. The digest must be the 32-byte output of a hash
// (SM3 in practice) — the function does not hash again.
func SignSM2(privateKeyBytes, digest []byte) ([]byte, error) {
	key, err := ParsePrivateKey(privateKeyBytes)
	if err != nil {
		return nil, err
	}
	return sm2.SignASN1(rand.Reader, key, digest, nil)
}

// VerifySM2 verifies an ASN.1 SM2 signature over digest with a raw 65-byte
// uncompressed SM2 public key. It returns false for parse errors as well as
// for wrong signatures: GB 35114 treats both as authentication failure and
// callers do not need to distinguish the two.
func VerifySM2(publicKeyBytes, digest, signature []byte) bool {
	key, err := ParsePublicKey(publicKeyBytes)
	if err != nil {
		return false
	}
	return sm2.VerifyASN1(key, digest, signature)
}

// ParsePrivateKey converts raw 32-byte scalar bytes to a gmsm SM2 private
// key. Exposed for callers that need the concrete key type.
func ParsePrivateKey(privateKeyBytes []byte) (*sm2.PrivateKey, error) {
	if len(privateKeyBytes) != PrivateKeySize {
		return nil, fmt.Errorf("%w: got %d bytes, want %d",
			ErrBadPrivateKey, len(privateKeyBytes), PrivateKeySize)
	}
	key, err := sm2.NewPrivateKey(privateKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrBadPrivateKey, err.Error())
	}
	return key, nil
}

// ParsePublicKey converts raw 65-byte uncompressed-point bytes to an SM2
// public key. gmsm represents SM2 public keys with the standard-library
// ecdsa.PublicKey type, so that is what callers get back.
func ParsePublicKey(publicKeyBytes []byte) (*ecdsa.PublicKey, error) {
	if len(publicKeyBytes) != PublicKeySize {
		return nil, fmt.Errorf("%w: got %d bytes, want %d",
			ErrBadPublicKey, len(publicKeyBytes), PublicKeySize)
	}
	key, err := sm2.NewPublicKey(publicKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrBadPublicKey, err.Error())
	}
	return key, nil
}

// GenerateKeyPair returns a fresh SM2 key pair as (private scalar, public
// uncompressed point). Intended for test fixtures and the config bootstrap
// path; production nodes load their keys from the node configuration.
func GenerateKeyPair() (privateBytes, publicBytes []byte, err error) {
	key, err := sm2.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	privateBytes = make([]byte, PrivateKeySize)
	key.D.FillBytes(privateBytes)
	publicBytes = marshalUncompressedPoint(&key.PublicKey)
	return privateBytes, publicBytes, nil
}

// marshalUncompressedPoint writes the 65-byte uncompressed point encoding
// (0x04 || X || Y) for a curve whose Bytes() is not natively supported by
// the standard library.
func marshalUncompressedPoint(pub *ecdsa.PublicKey) []byte {
	out := make([]byte, PublicKeySize)
	out[0] = 0x04
	pub.X.FillBytes(out[1:33])
	pub.Y.FillBytes(out[33:])
	return out
}

// DigestAndSign signs an arbitrary message: it hashes the message with SM3
// and signs the 32-byte digest. This is the shape GB 35114 uses for the
// SecurityInfo signatures, so callers that hold a raw body (not a digest)
// go through here.
func DigestAndSign(privateKeyBytes []byte, message []byte) ([]byte, error) {
	return SignSM2(privateKeyBytes, HashSM3Bytes(message))
}

// bigIntCover keeps the math/big import honest: FillBytes above is the only
// use, but reviewers may wonder why the import exists. Remove this var and
// the import if the key-size helpers ever change shape.
var _ = big.NewInt
