// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"crypto/rand"
	"errors"
	"runtime"

	blst "github.com/supranational/blst/bindings/go"
)

const SecretKeyLen = blst.BLST_SCALAR_BYTES

var (
	errFailedSecretKeyDeserialize = errors.New("couldn't deserialize secret key")

	// The ciphersuite is more commonly known as G2ProofOfPossession.
	// There are two digests to ensure that message space for normal
	// signatures and the proof of possession are distinct.
	ciphersuiteSignature                = []byte("BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_")
	ciphersuiteProofOfPossession        = []byte("BLS_POP_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_")
	_                            Signer = (*LocalSigner)(nil)
)

type SecretKey = blst.SecretKey

type Signer interface {
	PublicKey() *PublicKey
	Sign(msg []byte) *Signature
	SignProofOfPossession(msg []byte) *Signature
}

type LocalSigner struct {
	sk *SecretKey
}

// NewSecretKey generates a new secret key from the local source of
// cryptographically secure randomness.
func NewSigner() (*LocalSigner, error) {
	var ikm [32]byte
	_, err := rand.Read(ikm[:])
	if err != nil {
		return nil, err
	}
	sk := blst.KeyGen(ikm[:])
	ikm = [32]byte{} // zero out the ikm

	return &LocalSigner{sk: sk}, nil
}

// ToBytes returns the big-endian format of the secret key.
func (s *LocalSigner) ToBytes() []byte {
	return s.sk.Serialize()
}

// SecretKeyFromBytes parses the big-endian format of the secret key into a
// secret key.
func SecretKeyFromBytes(skBytes []byte) (*LocalSigner, error) {
	sk := new(SecretKey).Deserialize(skBytes)
	if sk == nil {
		return nil, errFailedSecretKeyDeserialize
	}
	runtime.SetFinalizer(sk, func(sk *SecretKey) {
		sk.Zeroize()
	})
	return &LocalSigner{sk: sk}, nil
}

// PublicKey returns the public key that corresponds to this secret
// key.
func (s *LocalSigner) PublicKey() *PublicKey {
	return new(PublicKey).From(s.sk)
}

// Sign [msg] to authorize this message
func (s *LocalSigner) Sign(msg []byte) *Signature {
	return new(Signature).Sign(s.sk, msg, ciphersuiteSignature)
}

// Sign [msg] to prove the ownership
func (s *LocalSigner) SignProofOfPossession(msg []byte) *Signature {
	return new(Signature).Sign(s.sk, msg, ciphersuiteProofOfPossession)
}
