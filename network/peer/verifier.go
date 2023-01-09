// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto/x509"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

func RequiredVerifier(verifier Verifier) bool {
	switch verifier.(type) {
	case X509Verifier:
		return true
	case BLSVerifier:
		return false
	default:
		return false
	}
}

type Signature byte

const (
	TLS Signature = iota
	BLS
)

type Verifier interface {
	Verify(signature, message []byte) (bool, error)
}

var _ Verifier = (*X509Verifier)(nil)

type X509Verifier struct {
	Cert *x509.Certificate
}

func (x X509Verifier) Verify(signature, message []byte) (bool, error) {
	err := x.Cert.CheckSignature(x.Cert.SignatureAlgorithm, message, signature)
	if err != nil {
		return false, err
	}

	return true, nil
}

var _ Verifier = (*BLSVerifier)(nil)

type BLSVerifier struct {
	PublicKey *bls.PublicKey
}

func (b BLSVerifier) Verify(signature, message []byte) (bool, error) {
	sig, err := bls.SignatureFromBytes(signature)
	if err != nil {
		return false, err
	}

	return bls.Verify(b.PublicKey, sig, message), nil
}
