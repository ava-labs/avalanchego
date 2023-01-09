// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"github.com/ava-labs/avalanchego/network/peer"
)

var _ peer.Signer = (*Signer)(nil)

// Signer signs messages with a BLS key.
type Signer struct {
	secretKey *SecretKey
}

// NewSigner returns a new instance of Signer.
func NewSigner(secretKey *SecretKey) Signer {
	return Signer{
		secretKey: secretKey,
	}
}

func (s Signer) Sign(msg []byte) (signature []byte, err error) {
	sig := Sign(s.secretKey, msg)
	return SignatureToBytes(sig), nil
}
