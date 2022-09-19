// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	"encoding/json"
	"errors"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/formatting"
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

	// publicKey is the parsed version of [PublicKey]. It is populated in
	// [Verify].
	publicKey *bls.PublicKey
}

func NewBLS(sk *bls.SecretKey) *BLS {
	pk := bls.PublicFromSecretKey(sk)
	pkBytes := bls.PublicKeyToBytes(pk)
	sig := bls.Sign(sk, pkBytes)
	sigBytes := bls.SignatureToBytes(sig)

	bls := &BLS{}
	copy(bls.PublicKey[:], pkBytes)
	copy(bls.ProofOfPossession[:], sigBytes)
	return bls
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

func (b *BLS) MarshalJSON() ([]byte, error) {
	pk, err := formatting.Encode(formatting.HexNC, b.PublicKey[:])
	if err != nil {
		return nil, err
	}
	pop, err := formatting.Encode(formatting.HexNC, b.ProofOfPossession[:])
	if err != nil {
		return nil, err
	}
	type jsonBLS struct {
		PublicKey         string `json:"publicKey"`
		ProofOfPossession string `json:"proofOfPossession"`
	}
	return json.Marshal(jsonBLS{
		PublicKey:         pk,
		ProofOfPossession: pop,
	})
}
