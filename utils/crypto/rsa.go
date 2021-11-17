// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crypto

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

var errWrongKeyType = errors.New("wrong key type")

const rsaSize = 3072

type FactoryRSA struct{}

// NewPrivateKey implements the Factory interface
func (*FactoryRSA) NewPrivateKey() (PrivateKey, error) {
	k, err := rsa.GenerateKey(rand.Reader, rsaSize)
	if err != nil {
		return nil, err
	}
	return &PrivateKeyRSA{sk: k}, nil
}

// ToPublicKey implements the Factory interface
func (*FactoryRSA) ToPublicKey(b []byte) (PublicKey, error) {
	key, err := x509.ParsePKIXPublicKey(b)
	if err != nil {
		return nil, err
	}
	switch key := key.(type) {
	case *rsa.PublicKey:
		return &PublicKeyRSA{
			pk:    key,
			bytes: b,
		}, nil
	default:
		return nil, errWrongKeyType
	}
}

// ToPrivateKey implements the Factory interface
func (*FactoryRSA) ToPrivateKey(b []byte) (PrivateKey, error) {
	key, err := x509.ParsePKCS1PrivateKey(b)
	if err != nil {
		return nil, err
	}
	return &PrivateKeyRSA{
		sk:    key,
		bytes: b,
	}, nil
}

type PublicKeyRSA struct {
	pk    *rsa.PublicKey
	addr  ids.ShortID
	bytes []byte
}

// Verify implements the PublicKey interface
func (k *PublicKeyRSA) Verify(msg, sig []byte) bool {
	return k.VerifyHash(hashing.ComputeHash256(msg), sig)
}

// VerifyHash implements the PublicKey interface
func (k *PublicKeyRSA) VerifyHash(hash, sig []byte) bool {
	return rsa.VerifyPKCS1v15(k.pk, crypto.SHA256, hash, sig) == nil
}

// Address implements the PublicKey interface
func (k *PublicKeyRSA) Address() ids.ShortID {
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
func (k *PublicKeyRSA) Bytes() []byte {
	if k.bytes == nil {
		b, err := x509.MarshalPKIXPublicKey(k.pk)
		if err != nil {
			panic(err)
		}
		k.bytes = b
	}
	return k.bytes
}

type PrivateKeyRSA struct {
	sk    *rsa.PrivateKey
	pk    *PublicKeyRSA
	bytes []byte
}

// PublicKey implements the PrivateKey interface
func (k *PrivateKeyRSA) PublicKey() PublicKey {
	if k.pk == nil {
		k.pk = &PublicKeyRSA{pk: &k.sk.PublicKey}
	}
	return k.pk
}

// Sign implements the PrivateKey interface
func (k *PrivateKeyRSA) Sign(msg []byte) ([]byte, error) {
	return k.SignHash(hashing.ComputeHash256(msg))
}

// SignHash implements the PrivateKey interface
func (k *PrivateKeyRSA) SignHash(hash []byte) ([]byte, error) {
	return rsa.SignPKCS1v15(rand.Reader, k.sk, crypto.SHA256, hash)
}

// Bytes implements the PrivateKey interface
func (k *PrivateKeyRSA) Bytes() []byte {
	if k.bytes == nil {
		k.bytes = x509.MarshalPKCS1PrivateKey(k.sk)
	}
	return k.bytes
}
