package pgp

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type ecdsaSigner struct {
	sk *ecdsa.PrivateKey
	pk []byte
}

func newECDSASigner() (*ecdsaSigner, error) {
	sk, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ECDSA key: %w", err)
	}

	pk := sk.PublicKey

	der, err := x509.MarshalPKIXPublicKey(&pk)
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}

	return &ecdsaSigner{sk: sk, pk: der}, nil
}

func (s *ecdsaSigner) Sign(digest []byte) ([]byte, error) {
	sig, err := ecdsa.SignASN1(rand.Reader, s.sk, digest)
	if err != nil {
		return nil, fmt.Errorf("sign message: %w", err)
	}

	return sig, nil
}

func (s *ecdsaSigner) PK() ([]byte, error) {
	return s.pk, nil
}

func TestBinarySigner(t *testing.T) {
	signer, err := newECDSASigner()
	if err != nil {
		t.Fatal("create signer:", err)
	}

	config, err := GenerateConfig(signer, "Demo User <demo@example.com>", time.Unix(1700000000, 0))
	if err != nil {
		t.Fatal("generate config:", err)
	}

	bs := &BinarySigner{Config: config}

	message := []byte("Hello, this is a test binary content.")
	sigBytes, err := bs.Sign(message)
	if err != nil {
		t.Fatal("sign:", err)
	}

	// Write out for GPG verification
	testDir := t.TempDir()
	defer os.RemoveAll(testDir)

	msgPath := filepath.Join(testDir, "test_binary.bin")
	sigPath := filepath.Join(testDir, "test_binary.bin.asc")
	pkPath := filepath.Join(testDir, "pk_binary_test.asc")

	if err := os.WriteFile(msgPath, message, 0644); err != nil {
		t.Fatal("write message:", err)
	}
	if err := os.WriteFile(sigPath, sigBytes, 0644); err != nil {
		t.Fatal("write signature:", err)
	}
	if err := os.WriteFile(pkPath, config.PGPPK, 0644); err != nil {
		t.Fatal("write public key:", err)
	}

	t.Logf("Signature written to %s", sigPath)
	t.Logf("Public key written to %s", pkPath)

	// Verify the signature using GPG
	gpgHome := filepath.Join(testDir, ".gnupg")
	if err := os.MkdirAll(gpgHome, 0700); err != nil {
		t.Fatal("create gnupg home:", err)
	}

	importCmd := exec.Command("gpg", "--homedir", gpgHome, "--no-autostart", "--import", pkPath)
	importOut, err := importCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gpg import failed: %v\n%s", err, importOut)
	}
	t.Logf("GPG import: %s", importOut)

	verifyCmd := exec.Command("gpg", "--homedir", gpgHome, "--no-autostart", "--verify", sigPath, msgPath)
	verifyOut, err := verifyCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gpg verify failed: %v\n%s", err, verifyOut)
	}
	t.Logf("GPG verify: %s", verifyOut)
}
