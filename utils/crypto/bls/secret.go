// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

type SecretKey bool

var falseSecretKey SecretKey = false

func NewSecretKey() (*SecretKey, error) {
	return &falseSecretKey, nil
}

func SecretKeyFromBytes([]byte) (*SecretKey, error) {
	return &falseSecretKey, nil
}

func SecretKeyToBytes(*SecretKey) []byte {
	return []byte{}
}

func PublicFromSecretKey(sk *SecretKey) *PublicKey {
	if sk == nil {
		return nil
	}
	return &DummyPublicKey
}
