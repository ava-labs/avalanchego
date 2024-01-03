// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenesisAssetCompare(t *testing.T) {
	tests := []struct {
		a        *GenesisAsset
		b        *GenesisAsset
		expected int
	}{
		{
			a:        &GenesisAsset{},
			b:        &GenesisAsset{},
			expected: 0,
		},
		{
			a: &GenesisAsset{
				Alias: "a",
			},
			b: &GenesisAsset{
				Alias: "aa",
			},
			expected: -1,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%s_%s_%d", test.a.Alias, test.b.Alias, test.expected), func(t *testing.T) {
			require := require.New(t)

			require.Equal(test.expected, test.a.Compare(test.b))
			require.Equal(-test.expected, test.b.Compare(test.a))
		})
	}
}
