// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"crypto/ecdsa"
	"io"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"
)

type Key struct {
	Address    common.Address
	PrivateKey *ecdsa.PrivateKey
}

func NewKey(rand io.Reader) (*Key, error) {
	privateKeyECDSA, err := ecdsa.GenerateKey(crypto.S256(), rand)
	if err != nil {
		return nil, err
	}
	return NewKeyFromECDSA(privateKeyECDSA), nil
}

func NewKeyFromECDSA(privateKeyECDSA *ecdsa.PrivateKey) *Key {
	return &Key{
		Address:    crypto.PubkeyToAddress(privateKeyECDSA.PublicKey),
		PrivateKey: privateKeyECDSA,
	}
}
