// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestSubnetValidatorWeight(t *testing.T) {
	require := require.New(t)

	msg, err := NewSubnetValidatorWeight(
		ids.GenerateTestID(),
		rand.Uint64(), //#nosec G404
		rand.Uint64(), //#nosec G404
	)
	require.NoError(err)

	parsed, err := ParseSubnetValidatorWeight(msg.Bytes())
	require.NoError(err)
	require.Equal(msg, parsed)
}

func TestSubnetValidatorWeight_Verify(t *testing.T) {
	mustCreate := func(msg *SubnetValidatorWeight, err error) *SubnetValidatorWeight {
		require.NoError(t, err)
		return msg
	}
	tests := []struct {
		name     string
		msg      *SubnetValidatorWeight
		expected error
	}{
		{
			name: "Invalid Nonce",
			msg: mustCreate(NewSubnetValidatorWeight(
				ids.GenerateTestID(),
				math.MaxUint64,
				1,
			)),
			expected: ErrNonceReservedForRemoval,
		},
		{
			name: "Valid",
			msg: mustCreate(NewSubnetValidatorWeight(
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
