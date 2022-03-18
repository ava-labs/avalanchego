// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crypto

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

var (
	// Interface compliance
	_ Factory    = &FactoryRSAPSS{}
	_ PublicKey  = &PublicKeyRSAPSS{}
	_ PrivateKey = &PrivateKeyRSAPSS{}
)

const rsaPSSSize = 3072

type FactoryRSAPSS struct{}

func (*FactoryRSAPSS) NewPrivateKey() (PrivateKey, error) {
	k, err := rsa.GenerateKey(rand.Reader, rsaPSSSize)
	if err != nil {
		return nil, err
	}
	return &PrivateKeyRSAPSS{sk: k}, nil
}

func (*FactoryRSAPSS) ToPublicKey(b []byte) (PublicKey, error) {
	key, err := x509.ParsePKIXPublicKey(b)
	if err != nil {
		return nil, err
	}
	switch key := key.(type) {
	case *rsa.PublicKey:
		return &PublicKeyRSAPSS{
			pk:    key,
			bytes: b,
		}, nil
	default:
		return nil, errWrongKeyType
	}
}

func (*FactoryRSAPSS) ToPrivateKey(b []byte) (PrivateKey, error) {
	key, err := x509.ParsePKCS1PrivateKey(b)
	if err != nil {
		return nil, err
	}
	return &PrivateKeyRSAPSS{
		sk:    key,
		bytes: b,
	}, nil
}

type PublicKeyRSAPSS struct {
	pk    *rsa.PublicKey
	addr  ids.ShortID
	bytes []byte
}

func (k *PublicKeyRSAPSS) Verify(msg, sig []byte) bool {
	return k.VerifyHash(hashing.ComputeHash256(msg), sig)
}

func (k *PublicKeyRSAPSS) VerifyHash(hash, sig []byte) bool {
	return rsa.VerifyPSS(k.pk, crypto.SHA256, hash, sig, nil) == nil
}

func (k *PublicKeyRSAPSS) Address() ids.ShortID {
	if k.addr == ids.ShortEmpty {
		addr, err := ids.ToShortID(hashing.PubkeyBytesToAddress(k.Bytes()))
		if err != nil {
			panic(err)
		}
		k.addr = addr
	}
	return k.addr
}

func (k *PublicKeyRSAPSS) Bytes() []byte {
	if k.bytes == nil {
		b, err := x509.MarshalPKIXPublicKey(k.pk)
		if err != nil {
			panic(err)
		}
		k.bytes = b
	}
	return k.bytes
}

type PrivateKeyRSAPSS struct {
	sk    *rsa.PrivateKey
	pk    *PublicKeyRSAPSS
	bytes []byte
}

func (k *PrivateKeyRSAPSS) PublicKey() PublicKey {
	if k.pk == nil {
		k.pk = &PublicKeyRSAPSS{pk: &k.sk.PublicKey}
	}
	return k.pk
}

func (k *PrivateKeyRSAPSS) Sign(msg []byte) ([]byte, error) {
	return k.SignHash(hashing.ComputeHash256(msg))
}

func (k *PrivateKeyRSAPSS) SignHash(hash []byte) ([]byte, error) {
	return rsa.SignPSS(rand.Reader, k.sk, crypto.SHA256, hash, nil)
}

func (k *PrivateKeyRSAPSS) Bytes() []byte {
	if k.bytes == nil {
		k.bytes = x509.MarshalPKCS1PrivateKey(k.sk)
	}
	return k.bytes
}
