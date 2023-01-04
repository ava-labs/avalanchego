// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ecdsa

import (
	stdcrypto "crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"

	"github.com/ava-labs/avalanchego/utils/crypto"
)

var _ crypto.Signer = (*Signer)(nil)

// Signer signs messages with an ECDSA signature.
type Signer struct {
	privateKey *ecdsa.PrivateKey
	opts       stdcrypto.SignerOpts
}

// NewSigner returns a new instance of Signer.
func NewSigner(curve elliptic.Curve, opts stdcrypto.SignerOpts) (Signer, error) {
	privateKey, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		return Signer{}, err
	}

	return Signer{
		privateKey: privateKey,
		opts:       opts,
	}, nil
}

func (s Signer) Sign(msg []byte) ([]byte, error) {
	return s.privateKey.Sign(rand.Reader, msg, s.opts)
}
