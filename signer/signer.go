// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	"crypto"
	"crypto/rand"
	"crypto/tls"
	"errors"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

var errInvalidTLSKey = errors.New("invalid TLS key")

// BLSSigner signs ips with a BLS key.
type BLSSigner struct {
	secretKey *bls.SecretKey
}

// NewBLSSigner returns a new instance of BLSSigner.
func NewBLSSigner(secretKey *bls.SecretKey) BLSSigner {
	return BLSSigner{
		secretKey: secretKey,
	}
}

func (b BLSSigner) Sign(msg []byte) []byte {
	return bls.SignatureToBytes(bls.Sign(b.secretKey, msg))
}

// TLSSigner is signs ips with a TLS key.
type TLSSigner struct {
	privateKey crypto.Signer
}

// NewTLSSigner returns a new instance of TLSSigner.
func NewTLSSigner(cert *tls.Certificate) (TLSSigner, error) {
	privateKey, ok := cert.PrivateKey.(crypto.Signer)
	if !ok {
		return TLSSigner{}, errInvalidTLSKey
	}

	return TLSSigner{
		privateKey: privateKey,
	}, nil
}

func (t TLSSigner) Sign(bytes []byte) ([]byte, error) {
	tlsSig, err := t.privateKey.Sign(rand.Reader,
		hashing.ComputeHash256(bytes), crypto.SHA256)
	if err != nil {
		return nil, err
	}

	return tlsSig, err
}
