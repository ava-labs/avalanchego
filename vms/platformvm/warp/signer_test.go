// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

func TestSigner(t *testing.T) {
	for _, test := range SignerTests {
		sk, err := bls.NewSecretKey()
		require.NoError(t, err)

		networkID := uint32(12345)
		chainID := ids.GenerateTestID()
		s := NewSigner(sk, networkID, chainID)

		test(t, s, sk, networkID, chainID)
	}
}
