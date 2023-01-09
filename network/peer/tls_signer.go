// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto"
	"crypto/rand"
	"crypto/tls"
	"errors"

	"github.com/ava-labs/avalanchego/utils/hashing"
)

var (
	errInvalidTLSKey = errors.New("invalid TLS key")

	_ IPSigner = (*TLSSigner)(nil)
)

// TLSSigner is signs ips with a TLS key.
type TLSSigner struct {
	privateKey crypto.Signer
}

// NewTLSSigner returns a new instance of IPSigner.
func NewTLSSigner(cert *tls.Certificate) (*TLSSigner, error) {
	privateKey, ok := cert.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, errInvalidTLSKey
	}

	return &TLSSigner{
		privateKey: privateKey,
	}, nil
}

func (s TLSSigner) Sign(ipBytes []byte, sig Signature) (Signature, error) {
	tlsSig, err := s.privateKey.Sign(rand.Reader, hashing.ComputeHash256(ipBytes),
		crypto.SHA256)
	if err != nil {
		return Signature{}, err
	}

	sig.TLSSignature = tlsSig
	return sig, nil
}
