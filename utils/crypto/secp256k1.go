// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crypto

import (
	"crypto/ecdsa"
	"crypto/rand"
	"math/big"

	"github.com/ava-labs/go-ethereum/crypto"
	"github.com/ava-labs/go-ethereum/crypto/secp256k1"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/hashing"
)

const (
	// SECP256K1SigLen is the number of bytes in a secp2561k signature
	SECP256K1SigLen = 64

	// SECP256K1SKLen is the number of bytes in a secp2561k private key
	SECP256K1SKLen = 32

	// SECP256K1PKLen is the number of bytes in a secp2561k public key
	SECP256K1PKLen = 33
)

// FactorySECP256K1 ...
type FactorySECP256K1 struct{}

// NewPrivateKey implements the Factory interface
func (*FactorySECP256K1) NewPrivateKey() (PrivateKey, error) {
	k, err := ecdsa.GenerateKey(secp256k1.S256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return &PrivateKeySECP256K1{sk: k}, nil
}

// ToPublicKey implements the Factory interface
func (*FactorySECP256K1) ToPublicKey(b []byte) (PublicKey, error) {
	key, err := crypto.DecompressPubkey(b)
	return &PublicKeySECP256K1{
		pk:    key,
		bytes: b,
	}, err
}

// ToPrivateKey implements the Factory interface
func (*FactorySECP256K1) ToPrivateKey(b []byte) (PrivateKey, error) {
	key, err := crypto.ToECDSA(b)
	return &PrivateKeySECP256K1{
		sk:    key,
		bytes: b,
	}, err
}

// PublicKeySECP256K1 ...
type PublicKeySECP256K1 struct {
	pk    *ecdsa.PublicKey
	addr  ids.ShortID
	bytes []byte
}

// Verify implements the PublicKey interface
func (k *PublicKeySECP256K1) Verify(msg, sig []byte) bool {
	return k.VerifyHash(hashing.ComputeHash256(msg), sig)
}

// VerifyHash implements the PublicKey interface
func (k *PublicKeySECP256K1) VerifyHash(hash, sig []byte) bool {
	if verifySECP256K1SignatureFormat(sig) != nil {
		return false
	}
	return crypto.VerifySignature(k.Bytes(), hash, sig)
}

// Address implements the PublicKey interface
func (k *PublicKeySECP256K1) Address() ids.ShortID {
	if k.addr.IsZero() {
		addr, err := ids.ToShortID(hashing.PubkeyBytesToAddress(k.Bytes()))
		if err != nil {
			panic(err)
		}
		k.addr = addr
	}
	return k.addr
}

// Bytes implements the PublicKey interface
func (k *PublicKeySECP256K1) Bytes() []byte {
	if k.bytes == nil {
		k.bytes = crypto.CompressPubkey(k.pk)
	}
	return k.bytes
}

// PrivateKeySECP256K1 ...
type PrivateKeySECP256K1 struct {
	sk    *ecdsa.PrivateKey
	pk    *PublicKeySECP256K1
	bytes []byte
}

// PublicKey implements the PrivateKey interface
func (k *PrivateKeySECP256K1) PublicKey() PublicKey {
	if k.pk == nil {
		k.pk = &PublicKeySECP256K1{pk: (*ecdsa.PublicKey)(&k.sk.PublicKey)}
	}
	return k.pk
}

// Sign implements the PrivateKey interface
func (k *PrivateKeySECP256K1) Sign(msg []byte) ([]byte, error) {
	return k.SignHash(hashing.ComputeHash256(msg))
}

// SignHash implements the PrivateKey interface
func (k *PrivateKeySECP256K1) SignHash(hash []byte) ([]byte, error) {
	sig, err := crypto.Sign(hash, k.sk)
	if err != nil {
		return nil, err
	}
	return sig[:len(sig)-1], err
}

// Bytes implements the PrivateKey interface
func (k *PrivateKeySECP256K1) Bytes() []byte {
	if k.bytes == nil {
		k.bytes = make([]byte, SECP256K1SKLen)
		bytes := k.sk.D.Bytes()
		copy(k.bytes[SECP256K1SKLen-len(bytes):], bytes)
	}
	return k.bytes
}

func verifySECP256K1SignatureFormat(sig []byte) error {
	if len(sig) != SECP256K1SigLen {
		return errInvalidSigLen
	}
	var r, s big.Int
	r.SetBytes(sig[:32])
	s.SetBytes(sig[32:])
	if !crypto.ValidateSignatureValues(0, &r, &s, true) {
		return errMutatedSig
	}
	return nil
}
