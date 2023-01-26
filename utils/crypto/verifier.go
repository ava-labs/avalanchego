// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crypto

import (
	"crypto/x509"
	"errors"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

var errFailedBLSVerification = errors.New("failed bls verification")

// MultiVerifier supports the verification of multiple signature types
type MultiVerifier interface {
	// VerifyTLS [sig] against [msg] using a tls key.
	// Returns an error if verification fails.
	VerifyTLS(msg, sig []byte) error
	// VerifyBLS [sig] against [msg] using a bls key.
	// Returns if [sig] is valid for [msg]
	// Returns an error if verification fails.
	VerifyBLS(msg, sig []byte) (bool, error)
}

// BLSVerifier verifies a signature of an ip against a BLS key
type BLSVerifier struct {
	PublicKey *bls.PublicKey
}

func (b BLSVerifier) Verify(msg, sig []byte) (bool, error) {
	blsSig, err := bls.SignatureFromBytes(sig)
	if err != nil {
		return false, err
	}

	if !bls.Verify(b.PublicKey, blsSig, msg) {
		return false, nil
	}

	return true, nil
}

// TLSVerifier verifies a signature of an ip against  a TLS cert.
type TLSVerifier struct {
	Cert *x509.Certificate
}

func (t TLSVerifier) Verify(msg, sig []byte) error {
	return t.Cert.CheckSignature(t.Cert.SignatureAlgorithm, msg, sig)
}
