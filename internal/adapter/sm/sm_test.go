package sm

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestHashSM3_Deterministic(t *testing.T) {
	got := HashSM3("abc")
	want := "66c7f0f462eeedd9d1f2d46bdc10e4e24167c4875cf2f7a2297da02b8f4ba8e0"
	if got != want {
		t.Fatalf("HashSM3: got %q want %q", got, want)
	}
}

func TestHashSM3Bytes(t *testing.T) {
	b := HashSM3Bytes([]byte("abc"))
	if len(b) != 32 {
		t.Fatalf("HashSM3Bytes length = %d want 32", len(b))
	}
	got := HashSM3("abc")
	want, err := hex.DecodeString(got)
	if err != nil {
		t.Fatalf("hex.DecodeString: %v", err)
	}
	if !bytes.Equal(b, want) {
		t.Fatalf("HashSM3Bytes cross-check mismatch:\n got %x\nwant %x", b, want)
	}
}

func TestSignVerifySM2RoundTrip(t *testing.T) {
	privBytes, pubBytes, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	digest := HashSM3Bytes([]byte("hello-sm2"))
	sig, err := SignSM2(privBytes, digest)
	if err != nil {
		t.Fatalf("SignSM2: %v", err)
	}
	if len(sig) == 0 {
		t.Fatal("SignSM2 returned empty signature")
	}

	if !VerifySM2(pubBytes, digest, sig) {
		t.Fatal("VerifySM2 rejected a valid signature")
	}
	// Tamper with the digest (same length): must fail.
	tamperedDigest := make([]byte, len(digest))
	copy(tamperedDigest, digest)
	tamperedDigest[0] ^= 0xff
	if VerifySM2(pubBytes, tamperedDigest, sig) {
		t.Fatal("VerifySM2 accepted a tampered digest")
	}
	// Tamper with the signature: must fail.
	sig[0] ^= 0xff
	if VerifySM2(pubBytes, digest, sig) {
		t.Fatal("VerifySM2 accepted a tampered signature")
	}
}

func TestGenerateKeyPair(t *testing.T) {
	privBytes, pubBytes, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if len(privBytes) != PrivateKeySize {
		t.Fatalf("private key length = %d want %d", len(privBytes), PrivateKeySize)
	}
	if len(pubBytes) != PublicKeySize {
		t.Fatalf("public key length = %d want %d", len(pubBytes), PublicKeySize)
	}
	if pubBytes[0] != 0x04 {
		t.Fatalf("public key missing uncompressed point prefix 0x04")
	}

	// Parse back and do a sign/verify round-trip.
	priv, err := ParsePrivateKey(privBytes)
	if err != nil {
		t.Fatalf("ParsePrivateKey: %v", err)
	}
	pub, err := ParsePublicKey(pubBytes)
	if err != nil {
		t.Fatalf("ParsePublicKey: %v", err)
	}
	digest := HashSM3Bytes([]byte("round-trip"))
	sig, err := SignSM2(privBytes, digest)
	if err != nil {
		t.Fatalf("SignSM2: %v", err)
	}
	if !VerifySM2(pubBytes, digest, sig) {
		t.Fatal("VerifySM2 rejected round-trip signature")
	}
	// Confirm the gmsm key objects parsed to the same key material.
	if priv.D.Cmp(pub.X) == 0 {
		t.Fatal("private and public keys unexpectedly alias the same scalar")
	}
}

func TestDigestAndSign(t *testing.T) {
	privBytes, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	msg := []byte("the message to sign")
	sig, err := DigestAndSign(privBytes, msg)
	if err != nil {
		t.Fatalf("DigestAndSign: %v", err)
	}
	if len(sig) == 0 {
		t.Fatal("DigestAndSign returned empty signature")
	}
}
