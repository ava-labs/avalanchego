// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"testing"

	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/stretchr/testify/assert"
)

func TestLoadTestKeys(t *testing.T) {
	keys, err := LoadHexTestKeys("test.insecure.secp256k1.keys")
	assert.NoError(t, err)
	for i, k := range keys {
		curAddr := encodeShortAddr(k)
		t.Logf("[%d] loaded %v", i, curAddr)
	}
}

func encodeShortAddr(pk *crypto.PrivateKeySECP256K1R) string {
	return pk.PublicKey().Address().String()
}
