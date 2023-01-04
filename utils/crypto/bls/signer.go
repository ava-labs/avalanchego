// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"github.com/ava-labs/avalanchego/utils/crypto"
)

var _ crypto.Signer = (*Signer)(nil)

// Signer signs messages with a BLS key.
type Signer struct {
	secretKey *SecretKey
}

func NewSigner(secretKey *SecretKey) Signer {
	return Signer{
		secretKey: secretKey,
	}
}

func (s Signer) Sign(msg []byte) (signature []byte, err error) {
	sig := Sign(s.secretKey, msg)
	return SignatureToBytes(sig), nil
}
