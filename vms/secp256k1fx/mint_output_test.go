// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
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
			expectedErr: errNilOutput,
		},
		{
			name: "invalid output owners",
			out: &MintOutput{
				OutputOwners: OutputOwners{
					Threshold: 2,
					Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
				},
			},
			expectedErr: errOutputUnspendable,
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
			require.ErrorIs(t, tt.out.Verify(), tt.expectedErr)
			require.ErrorIs(t, tt.out.VerifyState(), tt.expectedErr)
		})
	}
}
