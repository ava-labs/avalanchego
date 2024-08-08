// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestUnwrapOutput(t *testing.T) {
	normalOutput := &secp256k1fx.TransferOutput{
		Amt: 123,
		OutputOwners: secp256k1fx.OutputOwners{
			Locktime:  456,
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.ShortEmpty},
		},
	}

	tests := []struct {
		name             string
		output           verify.State
		expectedOutput   *secp256k1fx.TransferOutput
		expectedLocktime uint64
		expectedErr      error
	}{
		{
			name:             "normal output",
			output:           normalOutput,
			expectedOutput:   normalOutput,
			expectedLocktime: 0,
			expectedErr:      nil,
		},
		{
			name: "locked output",
			output: &stakeable.LockOut{
				Locktime:        789,
				TransferableOut: normalOutput,
			},
			expectedOutput:   normalOutput,
			expectedLocktime: 789,
			expectedErr:      nil,
		},
		{
			name: "locked output with no locktime",
			output: &stakeable.LockOut{
				Locktime:        0,
				TransferableOut: normalOutput,
			},
			expectedOutput:   normalOutput,
			expectedLocktime: 0,
			expectedErr:      nil,
		},
		{
			name:             "invalid output",
			output:           nil,
			expectedOutput:   nil,
			expectedLocktime: 0,
			expectedErr:      ErrUnknownOutputType,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			output, locktime, err := unwrapOutput(test.output)
			require.Equal(test.expectedOutput, output)
			require.Equal(test.expectedLocktime, locktime)
			require.ErrorIs(err, test.expectedErr)
		})
	}
}
