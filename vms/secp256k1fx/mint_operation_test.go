// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

func TestMintOperationVerify(t *testing.T) {
	var (
		validOutputOwners = OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		}
		validMintInput = Input{
			SigIndices: []uint32{0},
		}
		validMintOutput = MintOutput{
			OutputOwners: validOutputOwners,
		}
		validTransferOutput = TransferOutput{
			Amt:          1,
			OutputOwners: validOutputOwners,
		}
	)

	tests := []struct {
		name        string
		op          *MintOperation
		expectedErr error
	}{
		{
			name:        "nil",
			op:          nil,
			expectedErr: errNilMintOperation,
		},
		{
			name: "invalid mint input",
			op: &MintOperation{
				MintInput: Input{
					SigIndices: []uint32{0, 0},
				},
				MintOutput:     validMintOutput,
				TransferOutput: validTransferOutput,
			},
			expectedErr: ErrInputIndicesNotSortedUnique,
		},
		{
			name: "invalid mint output",
			op: &MintOperation{
				MintInput: validMintInput,
				MintOutput: MintOutput{
					OutputOwners: OutputOwners{
						Threshold: 2,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
				TransferOutput: validTransferOutput,
			},
			expectedErr: ErrOutputUnspendable,
		},
		{
			name: "invalid transfer output",
			op: &MintOperation{
				MintInput:  validMintInput,
				MintOutput: validMintOutput,
				TransferOutput: TransferOutput{
					Amt: 1,
					OutputOwners: OutputOwners{
						Threshold: 0,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
			},
			expectedErr: ErrOutputUnoptimized,
		},
		{
			name: "addresses not unique",
			op: &MintOperation{
				MintInput:  validMintInput,
				MintOutput: validMintOutput,
				TransferOutput: TransferOutput{
					Amt: 1,
					OutputOwners: OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.ShortEmpty, ids.ShortEmpty},
					},
				},
			},
			expectedErr: ErrAddrsNotSortedUnique,
		},
		{
			name: "addresses not sorted",
			op: &MintOperation{
				MintInput:  validMintInput,
				MintOutput: validMintOutput,
				TransferOutput: TransferOutput{
					Amt: 1,
					OutputOwners: OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{{2}, {1}},
					},
				},
			},
			expectedErr: ErrAddrsNotSortedUnique,
		},
		{
			name: "passes verification",
			op: &MintOperation{
				MintInput:      validMintInput,
				MintOutput:     validMintOutput,
				TransferOutput: validTransferOutput,
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.op.Verify()
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestMintOperationOuts(t *testing.T) {
	require := require.New(t)
	op := &MintOperation{
		MintInput: Input{
			SigIndices: []uint32{0},
		},
		MintOutput: MintOutput{
			OutputOwners: OutputOwners{
				Threshold: 1,
				Addrs: []ids.ShortID{
					addr,
				},
			},
		},
		TransferOutput: TransferOutput{
			Amt: 1,
			OutputOwners: OutputOwners{
				Locktime:  0,
				Threshold: 1,
			},
		},
	}

	require.Len(op.Outs(), 2)
}

func TestMintOperationState(t *testing.T) {
	require := require.New(t)
	intf := interface{}(&MintOperation{})
	_, ok := intf.(verify.State)
	require.False(ok)
}
