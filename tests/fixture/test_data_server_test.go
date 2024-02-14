// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
func TestAllocatePreFundedKeys(t *testing.T) {
	keys := make([]*secp256k1.PrivateKey, 5)
	for i := range keys {
		key, err := secp256k1.NewPrivateKey()
		require.NoError(t, err)
		keys[i] = key
	}

	uri, err := ServeTestData(TestData{
		PreFundedKeys: keys,
	})
	require.NoError(t, err)

	testCases := []struct {
		name              string
		count             int
		expectedAddresses []ids.ShortID
		expectedError     error
	}{
		{
			name:  "single key",
			count: 1,
			expectedAddresses: []ids.ShortID{
				keys[4].Address(),
			},
			expectedError: nil,
		},
		{
			name:  "multiple keys",
			count: 4,
			expectedAddresses: []ids.ShortID{
				keys[0].Address(),
				keys[1].Address(),
				keys[2].Address(),
				keys[3].Address(),
			},
			expectedError: nil,
		},
		{
			name:              "insufficient keys available",
			count:             1,
			expectedAddresses: []ids.ShortID{},
			expectedError:     errRequestedKeyCountExceedsAvailable,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require := require.New(t)

			keys, err := AllocatePreFundedKeys(uri, tc.count)
			require.ErrorIs(err, tc.expectedError)

			addresses := make([]ids.ShortID, len(keys))
			for i, key := range keys {
				addresses[i] = key.Address()
			}
			require.Equal(tc.expectedAddresses, addresses)
		})
	}
}
