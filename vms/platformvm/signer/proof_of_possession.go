// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	"encoding/json"
	"errors"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/formatting"
)

var (
	_ Signer = (*ProofOfPossession)(nil)

	ErrInvalidProofOfPossession = errors.New("invalid proof of possession")
)

type ProofOfPossession struct {
	PublicKey [bls.PublicKeyLen]byte `serialize:"true" json:"publicKey"`
	// BLS signature proving ownership of [PublicKey]. The signed message is the
	// [PublicKey].
	ProofOfPossession [bls.SignatureLen]byte `serialize:"true" json:"proofOfPossession"`

	// publicKey is the parsed version of [PublicKey]. It is populated in
	// [Verify].
	publicKey *bls.PublicKey
}

func NewProofOfPossession(sk bls.Signer) (*ProofOfPossession, error) {
	pk := sk.PublicKey()
	pkBytes := bls.PublicKeyToCompressedBytes(pk)
	sig, err := sk.SignProofOfPossession(pkBytes)
	if err != nil {
		return nil, err
	}

	sigBytes := bls.SignatureToBytes(sig)

	pop := &ProofOfPossession{
		publicKey: pk,
	}
	copy(pop.PublicKey[:], pkBytes)
	copy(pop.ProofOfPossession[:], sigBytes)
	return pop, nil
}

func (p *ProofOfPossession) Verify() error {
	publicKey, err := bls.PublicKeyFromCompressedBytes(p.PublicKey[:])
	if err != nil {
		return err
	}
	signature, err := bls.SignatureFromBytes(p.ProofOfPossession[:])
	if err != nil {
		return err
	}
	if !bls.VerifyProofOfPossession(publicKey, signature, p.PublicKey[:]) {
		return ErrInvalidProofOfPossession
	}

	p.publicKey = publicKey
	return nil
}

func (p *ProofOfPossession) Key() *bls.PublicKey {
	return p.publicKey
}

type JsonProofOfPossession struct {
	PublicKey         string `json:"publicKey"`
	ProofOfPossession string `json:"proofOfPossession"`
}

func (p *ProofOfPossession) MarshalJSON() ([]byte, error) {
	pk, err := formatting.Encode(formatting.HexNC, p.PublicKey[:])
	if err != nil {
		return nil, err
	}
	pop, err := formatting.Encode(formatting.HexNC, p.ProofOfPossession[:])
	if err != nil {
		return nil, err
	}
	return json.Marshal(JsonProofOfPossession{
		PublicKey:         pk,
		ProofOfPossession: pop,
	})
}

func (p *ProofOfPossession) UnmarshalJSON(b []byte) error {
	jsonBLS := JsonProofOfPossession{}
	err := json.Unmarshal(b, &jsonBLS)
	if err != nil {
		return err
	}

	pkBytes, err := formatting.Decode(formatting.HexNC, jsonBLS.PublicKey)
	if err != nil {
		return err
	}
	pk, err := bls.PublicKeyFromCompressedBytes(pkBytes)
	if err != nil {
		return err
	}

	popBytes, err := formatting.Decode(formatting.HexNC, jsonBLS.ProofOfPossession)
	if err != nil {
		return err
	}

	copy(p.PublicKey[:], pkBytes)
	copy(p.ProofOfPossession[:], popBytes)
	p.publicKey = pk
	return nil
}
