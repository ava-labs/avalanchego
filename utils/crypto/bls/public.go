// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

type PublicKey []byte

var DummyPublicKey = PublicKey{0}

func PublicKeyFromBytes([]byte) (*PublicKey, error) {
	return &DummyPublicKey, nil
}

func PublicKeyToBytes(*PublicKey) []byte {
	return DummyPublicKey
}
