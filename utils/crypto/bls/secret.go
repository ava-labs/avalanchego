// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"crypto/rand"
	"errors"

	blst "github.com/supranational/blst/bindings/go"
)

const SecretKeyLen = blst.BLST_SCALAR_BYTES

var (
	errFailedSecretKeyDeserialize = errors.New("couldn't deserialize secret key")

	// More commonly known as G2ProofOfPossession
	ciphersuite = []byte("BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_")
)

type SecretKey = blst.SecretKey

func NewSecretKey() (*SecretKey, error) {
	var ikm [32]byte
	_, err := rand.Read(ikm[:])
	return blst.KeyGen(ikm[:]), err
}

func SecretKeyFromBytes(skBytes []byte) (*SecretKey, error) {
	sk := new(SecretKey).Deserialize(skBytes)
	if sk == nil {
		return nil, errFailedSecretKeyDeserialize
	}
	return sk, nil
}

func SecretKeyToBytes(sk *SecretKey) []byte {
	return sk.Serialize()
}

func PublicFromSecretKey(sk *SecretKey) *PublicKey {
	return new(PublicKey).From(sk)
}

func Sign(sk *SecretKey, msg []byte) *Signature {
	return new(Signature).Sign(sk, msg, ciphersuite)
}
