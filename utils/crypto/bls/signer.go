// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

type Signer interface {
	PublicKey() *PublicKey
	Sign(msg []byte) *Signature
	SignProofOfPossession(msg []byte) *Signature
}
