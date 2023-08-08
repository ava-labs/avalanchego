// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fixture

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
)

// Check that funded test keys can be served from an http server to
// ensure at-most-once allocation when tests are executed in parallel.
func TestAllocateFundedKeys(t *testing.T) {
	require := require.New(t)

	factory := secp256k1.Factory{}
	keys := []*secp256k1.PrivateKey{}
	for i := 0; i < 5; i++ {
		key, err := factory.NewPrivateKey()
		require.NoError(err)
		keys = append(keys, key)
	}

	uri, err := ServeTestData(TestData{
		FundedKeys: keys,
	})
	require.NoError(err)

	testCases := []struct {
		name              string
		count             int
		expectedAddresses []ids.ShortID
	}{{
		name:  "single key",
		count: 1,
		expectedAddresses: []ids.ShortID{
			keys[4].Address(),
		},
	}, {
		name:  "multiple keys",
		count: 4,
		expectedAddresses: []ids.ShortID{
			keys[0].Address(),
			keys[1].Address(),
			keys[2].Address(),
			keys[3].Address(),
		},
	}, {
		name:  "insufficient keys available",
		count: 1,
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			keys, err := AllocateFundedKeys(uri, tc.count)
			if tc.expectedAddresses == nil {
				require.ErrorIs(err, errRequestedKeyCountExceedsAvailable)
			} else {
				require.NoError(err)
				addresses := []ids.ShortID{}
				for _, key := range keys {
					addresses = append(addresses, key.Address())
				}
				require.Equal(tc.expectedAddresses, addresses)
			}
		})
	}
}
