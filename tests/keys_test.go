// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
)

func TestLoadTestKeys(t *testing.T) {
	keys, err := LoadHexTestKeys("test.insecure.secp256k1.keys")
	require.NoError(t, err)
	for i, k := range keys {
		curAddr := encodeShortAddr(k)
		t.Logf("[%d] loaded %v", i, curAddr)
	}
}

func encodeShortAddr(pk *secp256k1.PrivateKey) string {
	return pk.PublicKey().Address().String()
}
