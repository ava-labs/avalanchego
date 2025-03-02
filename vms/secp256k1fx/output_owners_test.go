// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestOutputOwnersVerify(t *testing.T) {
	tests := []struct {
		name        string
		out         *OutputOwners
		expectedErr error
	}{
		{
			name:        "nil",
			out:         nil,
			expectedErr: ErrNilOutput,
		},
		{
			name: "threshold > num addrs",
			out: &OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{},
			},
			expectedErr: ErrOutputUnspendable,
		},
		{
			name: "unoptimized",
			out: &OutputOwners{
				Threshold: 0,
				Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
			},
			expectedErr: ErrOutputUnoptimized,
		},
		{
			name: "not sorted",
			out: &OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{{2}, {1}},
			},
			expectedErr: ErrAddrsNotSortedUnique,
		},
		{
			name: "not unique",
			out: &OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{{2}, {2}},
			},
			expectedErr: ErrAddrsNotSortedUnique,
		},
		{
			name: "passes verification",
			out: &OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{{2}},
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			err := tt.out.Verify()
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func TestOutputOwnerEquals(t *testing.T) {
	addr1, addr2 := ids.GenerateTestShortID(), ids.GenerateTestShortID()
	tests := []struct {
		name        string
		out1        *OutputOwners
		out2        *OutputOwners
		shouldEqual bool
	}{
		{
			name:        "both nil",
			out1:        nil,
			out2:        nil,
			shouldEqual: true,
		},
		{
			name: "different locktimes",
			out1: &OutputOwners{
				Locktime: 1,
				Addrs:    []ids.ShortID{addr1, addr2},
			},
			out2: &OutputOwners{
				Locktime: 2,
				Addrs:    []ids.ShortID{addr1, addr2},
			},
			shouldEqual: false,
		},
		{
			name: "different thresholds",
			out1: &OutputOwners{
				Threshold: 1,
				Locktime:  1,
				Addrs:     []ids.ShortID{addr1, addr2},
			},
			out2: &OutputOwners{
				Locktime: 1,
				Addrs:    []ids.ShortID{addr1, addr2},
			},
			shouldEqual: false,
		},
		{
			name: "different addresses",
			out1: &OutputOwners{
				Locktime: 1,
				Addrs:    []ids.ShortID{addr1, ids.GenerateTestShortID()},
			},
			out2: &OutputOwners{
				Locktime: 1,
				Addrs:    []ids.ShortID{addr1, addr2},
			},
			shouldEqual: false,
		},
		{
			name: "equal",
			out1: &OutputOwners{
				Locktime: 1,
				Addrs:    []ids.ShortID{addr1, addr2},
			},
			out2: &OutputOwners{
				Locktime: 1,
				Addrs:    []ids.ShortID{addr1, addr2},
			},
			shouldEqual: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			require.Equal(tt.shouldEqual, tt.out1.Equals(tt.out2))
			require.Equal(tt.shouldEqual, tt.out2.Equals(tt.out1))
			require.True(tt.out1.Equals(tt.out1)) //nolint:gocritic
			require.True(tt.out2.Equals(tt.out2)) //nolint:gocritic
		})
	}
}

func TestMarshalJSONDoesNotRequireCtx(t *testing.T) {
	require := require.New(t)
	out := &OutputOwners{
		Threshold: 1,
		Locktime:  2,
		Addrs: []ids.ShortID{
			{1},
			{0},
		},
	}

	b, err := out.MarshalJSON()
	require.NoError(err)

	require.JSONEq(`{"addresses":["6HgC8KRBEhXYbF4riJyJFLSHt37UNuRt","111111111111111111116DBWJs"],"locktime":2,"threshold":1}`, string(b))
}
