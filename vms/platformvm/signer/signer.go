// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var _ Signer = (*Empty)(nil)

type Signer interface {
	verify.Verifiable
	Signature() []byte
	Key() *bls.PublicKey
}

type ProofOfPossession struct {
	signature [crypto.SECP256K1RSigLen]byte
}

func NewProofOfPossession(*bls.SecretKey) *ProofOfPossession {
	return &ProofOfPossession{}
}

func (*ProofOfPossession) Verify() error {
	return nil
}

func (p *ProofOfPossession) Signature() []byte {
	return p.signature[:]
}

func (*ProofOfPossession) Key() *bls.PublicKey {
	return &bls.PublicKey{}
}

type Empty struct{}

func (*Empty) Verify() error {
	return nil
}

func (*Empty) Signature() []byte {
	return nil
}

func (*Empty) Key() *bls.PublicKey {
	return nil
}
