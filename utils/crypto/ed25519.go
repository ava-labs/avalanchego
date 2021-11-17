// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crypto

import (
	"errors"

	"golang.org/x/crypto/ed25519"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

var (
	errWrongPublicKeySize  = errors.New("wrong public key size")
	errWrongPrivateKeySize = errors.New("wrong private key size")
)

type FactoryED25519 struct{}

// NewPrivateKey implements the Factory interface
func (*FactoryED25519) NewPrivateKey() (PrivateKey, error) {
	_, k, err := ed25519.GenerateKey(nil)
	return &PrivateKeyED25519{sk: k}, err
}

// ToPublicKey implements the Factory interface
func (*FactoryED25519) ToPublicKey(b []byte) (PublicKey, error) {
	if len(b) != ed25519.PublicKeySize {
		return nil, errWrongPublicKeySize
	}
	return &PublicKeyED25519{pk: b}, nil
}

// ToPrivateKey implements the Factory interface
func (*FactoryED25519) ToPrivateKey(b []byte) (PrivateKey, error) {
	if len(b) != ed25519.PrivateKeySize {
		return nil, errWrongPrivateKeySize
	}
	return &PrivateKeyED25519{sk: b}, nil
}

type PublicKeyED25519 struct {
	pk   ed25519.PublicKey
	addr ids.ShortID
}

// Verify implements the PublicKey interface
func (k *PublicKeyED25519) Verify(msg, sig []byte) bool {
	return ed25519.Verify(k.pk, msg, sig)
}

// VerifyHash implements the PublicKey interface
func (k *PublicKeyED25519) VerifyHash(hash, sig []byte) bool {
	return k.Verify(hash, sig)
}

// Address implements the PublicKey interface
func (k *PublicKeyED25519) Address() ids.ShortID {
	if k.addr == ids.ShortEmpty {
		addr, err := ids.ToShortID(hashing.PubkeyBytesToAddress(k.Bytes()))
		if err != nil {
			panic(err)
		}
		k.addr = addr
	}
	return k.addr
}

// Bytes implements the PublicKey interface
func (k *PublicKeyED25519) Bytes() []byte { return k.pk }

type PrivateKeyED25519 struct {
	sk ed25519.PrivateKey
	pk *PublicKeyED25519
}

// PublicKey implements the PrivateKey interface
func (k *PrivateKeyED25519) PublicKey() PublicKey {
	if k.pk == nil {
		k.pk = &PublicKeyED25519{
			pk: k.sk.Public().(ed25519.PublicKey),
		}
	}
	return k.pk
}

// Sign implements the PrivateKey interface
func (k *PrivateKeyED25519) Sign(msg []byte) ([]byte, error) {
	return ed25519.Sign(k.sk, msg), nil
}

// SignHash implements the PrivateKey interface
func (k PrivateKeyED25519) SignHash(hash []byte) ([]byte, error) {
	return k.Sign(hash)
}

// Bytes implements the PrivateKey interface
func (k PrivateKeyED25519) Bytes() []byte { return k.sk }
