// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestMintOutputVerify(t *testing.T) {
	tests := []struct {
		name        string
		out         *MintOutput
		expectedErr error
	}{
		{
			name:        "nil",
			out:         nil,
			expectedErr: ErrNilOutput,
		},
		{
			name: "invalid output owners",
			out: &MintOutput{
				OutputOwners: OutputOwners{
					Threshold: 2,
					Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
				},
			},
			expectedErr: ErrOutputUnspendable,
		},
		{
			name: "passes verification",
			out: &MintOutput{
				OutputOwners: OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
				},
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
