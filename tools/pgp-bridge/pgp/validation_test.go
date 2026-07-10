package pgp

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"strings"
	"testing"
	"time"
)

// fixedSigner is a Signer whose public key and signatures use whatever curve it
// is constructed with, letting tests drive the validation paths in EncodePGPPK.
type fixedSigner struct {
	sk *ecdsa.PrivateKey
	pk []byte
}

func newFixedSigner(t *testing.T, curve elliptic.Curve) *fixedSigner {
	t.Helper()
	sk, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&sk.PublicKey)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return &fixedSigner{sk: sk, pk: der}
}

func (s *fixedSigner) Sign(digest []byte) ([]byte, error) { return ecdsa.SignASN1(rand.Reader, s.sk, digest) }
func (s *fixedSigner) PK() ([]byte, error)                { return s.pk, nil }

func TestEncodePGPPK_RejectsNonP256Key(t *testing.T) {
	signer := newFixedSigner(t, elliptic.P384())
	_, err := GenerateConfig(signer, "Demo User <demo@example.com>", time.Unix(1700000000, 0))
	if err == nil {
		t.Fatal("expected error for non-P-256 key, got nil")
	}
	if !strings.Contains(err.Error(), "P-256") {
		t.Fatalf("expected P-256 error, got: %v", err)
	}
}

func TestEncodePGPPK_RejectsOverlongUserID(t *testing.T) {
	signer := newFixedSigner(t, elliptic.P256())
	longUserID := strings.Repeat("a", 256)
	_, err := GenerateConfig(signer, longUserID, time.Unix(1700000000, 0))
	if err == nil {
		t.Fatal("expected error for user ID > 255 bytes, got nil")
	}
	if !strings.Contains(err.Error(), "user ID too long") {
		t.Fatalf("expected user-ID-length error, got: %v", err)
	}
}
