// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	"errors"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

var (
	_ Signer = &BLS{}

	errInvalidProofOfPossession = errors.New("invalid proof of possession")
)

type BLS struct {
	PublicKey [bls.PublicKeyLen]byte `serialize:"true" json:"publicKey"`
	// BLS signature proving ownership of [PublicKey]. The signed message is the
	// [PublicKey].
	ProofOfPossession [bls.SignatureLen]byte `serialize:"true" json:"proofOfPossession"`

	publicKey *bls.PublicKey
}

func (b *BLS) Verify() error {
	publicKey, err := bls.PublicKeyFromBytes(b.PublicKey[:])
	if err != nil {
		return err
	}
	signature, err := bls.SignatureFromBytes(b.ProofOfPossession[:])
	if err != nil {
		return err
	}
	if !bls.Verify(publicKey, signature, b.PublicKey[:]) {
		return errInvalidProofOfPossession
	}

	b.publicKey = publicKey
	return nil
}

func (b *BLS) Key() *bls.PublicKey { return b.publicKey }
