// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestL1ValidatorWeight(t *testing.T) {
	require := require.New(t)

	msg, err := NewL1ValidatorWeight(
		ids.GenerateTestID(),
		rand.Uint64(), //#nosec G404
		rand.Uint64(), //#nosec G404
	)
	require.NoError(err)

	parsed, err := ParseL1ValidatorWeight(msg.Bytes())
	require.NoError(err)
	require.Equal(msg, parsed)
}

func TestL1ValidatorWeight_Verify(t *testing.T) {
	mustCreate := func(msg *L1ValidatorWeight, err error) *L1ValidatorWeight {
		require.NoError(t, err)
		return msg
	}
	tests := []struct {
		name     string
		msg      *L1ValidatorWeight
		expected error
	}{
		{
			name: "Invalid Nonce",
			msg: mustCreate(NewL1ValidatorWeight(
				ids.GenerateTestID(),
				math.MaxUint64,
				1,
			)),
			expected: ErrNonceReservedForRemoval,
		},
		{
			name: "Valid",
			msg: mustCreate(NewL1ValidatorWeight(
				ids.GenerateTestID(),
				math.MaxUint64,
				0,
			)),
			expected: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.msg.Verify()
			require.ErrorIs(t, err, test.expected)
		})
	}
}
