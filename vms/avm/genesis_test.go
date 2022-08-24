// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenesisAssetLess(t *testing.T) {
	require := require.New(t)

	var g1, g2 GenesisAsset
	require.False(g1.Less(&g2))
	require.False(g2.Less(&g1))

	g1 = GenesisAsset{
		Alias: "a",
	}
	g2 = GenesisAsset{
		Alias: "aa",
	}
	require.True(g1.Less(&g2))
	require.False(g2.Less(&g1))
}
