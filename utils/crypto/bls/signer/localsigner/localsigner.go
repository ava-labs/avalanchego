// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package localsigner

import (
	"crypto/rand"
	"errors"
	"runtime"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"

	blst "github.com/supranational/blst/bindings/go"
)

var (
	ErrFailedSecretKeyDeserialize            = errors.New("couldn't deserialize secret key")
	_                             bls.Signer = (*LocalSigner)(nil)
)

type secretKey = blst.SecretKey

type LocalSigner struct {
	sk *secretKey
	pk *bls.PublicKey
}

// NewSecretKey generates a new secret key from the local source of
// cryptographically secure randomness.
func New() (*LocalSigner, error) {
	var ikm [32]byte
	_, err := rand.Read(ikm[:])
	if err != nil {
		return nil, err
	}
	sk := blst.KeyGen(ikm[:])
	ikm = [32]byte{} // zero out the ikm
	pk := new(bls.PublicKey).From(sk)

	return &LocalSigner{sk: sk, pk: pk}, nil
}

// ToBytes returns the big-endian format of the secret key.
func (s *LocalSigner) ToBytes() []byte {
	return s.sk.Serialize()
}

// FromBytes parses the big-endian format of the secret key into a
// secret key.
func FromBytes(skBytes []byte) (*LocalSigner, error) {
	sk := new(secretKey).Deserialize(skBytes)
	if sk == nil {
		return nil, ErrFailedSecretKeyDeserialize
	}
	runtime.SetFinalizer(sk, func(sk *secretKey) {
		sk.Zeroize()
	})
	pk := new(bls.PublicKey).From(sk)

	return &LocalSigner{sk: sk, pk: pk}, nil
}

// PublicKey returns the public key that corresponds to this secret
// key.
func (s *LocalSigner) PublicKey() *bls.PublicKey {
	return s.pk
}

// Sign [msg] to authorize this message
func (s *LocalSigner) Sign(msg []byte) (*bls.Signature, error) {
	return new(bls.Signature).Sign(s.sk, msg, bls.CiphersuiteSignature.Bytes()), nil
}

// Sign [msg] to prove the ownership
func (s *LocalSigner) SignProofOfPossession(msg []byte) (*bls.Signature, error) {
	return new(bls.Signature).Sign(s.sk, msg, bls.CiphersuiteProofOfPossession.Bytes()), nil
}
