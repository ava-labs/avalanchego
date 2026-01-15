// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/ava-labs/avalanchego/utils/perms"
)

// InitNodeStakingKeyPair generates a self-signed TLS key/cert pair to use in
// staking. The key and files will be placed at [keyPath] and [certPath],
// respectively. If there is already a file at [keyPath], returns nil.
func InitNodeStakingKeyPair(keyPath, certPath string) error {
	// If there is already a file at [keyPath], do nothing
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		return nil
	}

	certBytes, keyBytes, err := NewCertAndKeyBytes()
	if err != nil {
		return err
	}

	// Ensure directory where key/cert will live exist
	if err := os.MkdirAll(filepath.Dir(certPath), perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("couldn't create path for cert: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("couldn't create path for key: %w", err)
	}

	// Write cert to disk
	certFile, err := os.Create(certPath)
	if err != nil {
		return fmt.Errorf("couldn't create cert file: %w", err)
	}
	if _, err := certFile.Write(certBytes); err != nil {
		return fmt.Errorf("couldn't write cert file: %w", err)
	}
	if err := certFile.Close(); err != nil {
		return fmt.Errorf("couldn't close cert file: %w", err)
	}
	if err := os.Chmod(certPath, perms.ReadOnly); err != nil { // Make cert read-only
		return fmt.Errorf("couldn't change permissions on cert: %w", err)
	}

	// Write key to disk
	keyOut, err := os.Create(keyPath)
	if err != nil {
		return fmt.Errorf("couldn't create key file: %w", err)
	}
	if _, err := keyOut.Write(keyBytes); err != nil {
		return fmt.Errorf("couldn't write private key: %w", err)
	}
	if err := keyOut.Close(); err != nil {
		return fmt.Errorf("couldn't close key file: %w", err)
	}
	if err := os.Chmod(keyPath, perms.ReadOnly); err != nil { // Make key read-only
		return fmt.Errorf("couldn't change permissions on key: %w", err)
	}
	return nil
}

func LoadTLSCertFromBytes(keyBytes, certBytes []byte) (*tls.Certificate, error) {
	cert, err := tls.X509KeyPair(certBytes, keyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed creating cert: %w", err)
	}

	cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("failed parsing cert: %w", err)
	}
	return &cert, nil
}

func LoadTLSCertFromFiles(keyPath, certPath string) (*tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("failed parsing cert: %w", err)
	}
	return &cert, nil
}

func NewTLSCert() (*tls.Certificate, error) {
	certBytes, keyBytes, err := NewCertAndKeyBytes()
	if err != nil {
		return nil, err
	}
	cert, err := tls.X509KeyPair(certBytes, keyBytes)
	if err != nil {
		return nil, err
	}
	cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
	return &cert, err
}

// Creates a new staking private key / staking certificate pair.
// Returns the PEM byte representations of both.
func NewCertAndKeyBytes() ([]byte, []byte, error) {
	// Create key to sign cert with
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("couldn't generate ecdsa key: %w", err)
	}

	// Create self-signed staking cert
	certTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(0),
		NotBefore:             time.Date(2000, time.January, 0, 0, 0, 0, 0, time.UTC),
		NotAfter:              time.Now().AddDate(100, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	certBytes, err := x509.CreateCertificate(rand.Reader, certTemplate, certTemplate, key.Public(), key)
	if err != nil {
		return nil, nil, fmt.Errorf("couldn't create certificate: %w", err)
	}
	var certBuff bytes.Buffer
	if err := pem.Encode(&certBuff, &pem.Block{Type: "CERTIFICATE", Bytes: certBytes}); err != nil {
		return nil, nil, fmt.Errorf("couldn't write cert file: %w", err)
	}

	privBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("couldn't marshal private key: %w", err)
	}

	var keyBuff bytes.Buffer
	if err := pem.Encode(&keyBuff, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}); err != nil {
		return nil, nil, fmt.Errorf("couldn't write private key: %w", err)
	}
	return certBuff.Bytes(), keyBuff.Bytes(), nil
}
