// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"crypto"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHashesAvailable(t *testing.T) {
	hashes := []crypto.Hash{
		crypto.SHA1,
		crypto.SHA256,
		crypto.SHA384,
		crypto.SHA512,
	}
	for _, hash := range hashes {
		require.True(t, hash.Available())
	}
}
