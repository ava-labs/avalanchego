// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"crypto/rand"
	"errors"

	blst "github.com/supranational/blst/bindings/go"
)

type SecretKey = blst.SecretKey

var (
	errInvalidSecretKey = errors.New("invalid secret key")

	// TODO: wtf is this
	dst = []byte("BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_")
)

func NewSecretKey() (*SecretKey, error) {
	var ikm [32]byte
	_, err := rand.Read(ikm[:])
	return blst.KeyGen(ikm[:]), err
}

func SecretKeyFromBytes(skBytes []byte) (*SecretKey, error) {
	sk := new(SecretKey).Deserialize(skBytes)
	if sk == nil {
		return nil, errInvalidSecretKey
	}
	return sk, nil
}

func SecretKeyToBytes(sk *SecretKey) []byte {
	return sk.Serialize()
}

func PublicFromSecretKey(sk *SecretKey) *PublicKey {
	pk := new(PublicKey)
	pk.From(sk)
	return pk
}

func Sign(sk *SecretKey, msg []byte) *Signature {
	sig := new(Signature)
	sig.Sign(sk, msg, dst)
	return sig
}
