package secp256k1fx

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/require"
)

func TestUpdateManagedAssetVerify(t *testing.T) {
	type test struct {
		op               *UpdateManagedAssetOperation
		description      string
		shouldFailVerify bool
	}

	tests := []test{
		{
			description:      "nil",
			op:               nil,
			shouldFailVerify: true,
		},
		{
			description: "valid, no mint",
			op: &UpdateManagedAssetOperation{
				Input: Input{
					SigIndices: []uint32{0},
				},
				ManagedAssetStatusOutput: ManagedAssetStatusOutput{
					Frozen: false,
					Manager: OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
				Mint:           false,
				TransferOutput: TransferOutput{},
			},
			shouldFailVerify: false,
		},
		{
			description: "valid, mint",
			op: &UpdateManagedAssetOperation{
				Input: Input{
					SigIndices: []uint32{0},
				},
				ManagedAssetStatusOutput: ManagedAssetStatusOutput{
					Frozen: false,
					Manager: OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
				Mint: true,
				TransferOutput: TransferOutput{
					Amt: 1,
					OutputOwners: OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
			},
			shouldFailVerify: false,
		},
		{
			description: "empty input",
			op: &UpdateManagedAssetOperation{
				Input: Input{},
				ManagedAssetStatusOutput: ManagedAssetStatusOutput{
					Frozen: false,
					Manager: OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
				Mint:           false,
				TransferOutput: TransferOutput{},
			},
			shouldFailVerify: false,
		},
		{
			description: "empty ManagedAssetStatusOutput",
			op: &UpdateManagedAssetOperation{
				Input: Input{
					SigIndices: []uint32{0},
				},
				ManagedAssetStatusOutput: ManagedAssetStatusOutput{},
				Mint:                     false,
				TransferOutput:           TransferOutput{},
			},
			shouldFailVerify: true,
		},
		{
			description: "manager threshold == 0",
			op: &UpdateManagedAssetOperation{
				Input: Input{
					SigIndices: []uint32{0},
				},
				ManagedAssetStatusOutput: ManagedAssetStatusOutput{
					Frozen: false,
					Manager: OutputOwners{
						Locktime:  0,
						Threshold: 0,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
				Mint:           false,
				TransferOutput: TransferOutput{},
			},
			shouldFailVerify: true,
		},
		{
			description: "manager no addresses",
			op: &UpdateManagedAssetOperation{
				Input: Input{
					SigIndices: []uint32{0},
				},
				ManagedAssetStatusOutput: ManagedAssetStatusOutput{
					Frozen: false,
					Manager: OutputOwners{
						Locktime:  0,
						Threshold: 0,
						Addrs:     []ids.ShortID{},
					},
				},
				Mint:           false,
				TransferOutput: TransferOutput{},
			},
			shouldFailVerify: true,
		},
		{
			description: "manager no addresses 2",
			op: &UpdateManagedAssetOperation{
				Input: Input{
					SigIndices: []uint32{0},
				},
				ManagedAssetStatusOutput: ManagedAssetStatusOutput{
					Frozen: false,
					Manager: OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{},
					},
				},
				Mint:           false,
				TransferOutput: TransferOutput{},
			},
			shouldFailVerify: true,
		},
		{
			description: "manager threshold > len(addrs)",
			op: &UpdateManagedAssetOperation{
				Input: Input{
					SigIndices: []uint32{0},
				},
				ManagedAssetStatusOutput: ManagedAssetStatusOutput{
					Frozen: false,
					Manager: OutputOwners{
						Locktime:  0,
						Threshold: 2,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
				Mint:           false,
				TransferOutput: TransferOutput{},
			},
			shouldFailVerify: true,
		},
		{
			description: "no mint but transfer amount != 0",
			op: &UpdateManagedAssetOperation{
				Input: Input{
					SigIndices: []uint32{0},
				},
				ManagedAssetStatusOutput: ManagedAssetStatusOutput{
					Frozen: false,
					Manager: OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
				Mint: false,
				TransferOutput: TransferOutput{
					Amt:          1,
					OutputOwners: OutputOwners{},
				},
			},
			shouldFailVerify: true,
		},
		{
			description: "no mint but transfer locktime != 0",
			op: &UpdateManagedAssetOperation{
				Input: Input{
					SigIndices: []uint32{0},
				},
				ManagedAssetStatusOutput: ManagedAssetStatusOutput{
					Frozen: false,
					Manager: OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
				Mint: false,
				TransferOutput: TransferOutput{
					Amt: 0,
					OutputOwners: OutputOwners{
						Locktime: 1,
					},
				},
			},
			shouldFailVerify: true,
		},
		{
			description: "no mint but transfer threshold != 0",
			op: &UpdateManagedAssetOperation{
				Input: Input{
					SigIndices: []uint32{0},
				},
				ManagedAssetStatusOutput: ManagedAssetStatusOutput{
					Frozen: false,
					Manager: OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
				Mint: false,
				TransferOutput: TransferOutput{
					Amt: 0,
					OutputOwners: OutputOwners{
						Threshold: 1,
					},
				},
			},
			shouldFailVerify: true,
		},
		{
			description: "no mint but len(transfer addresses) != 0",
			op: &UpdateManagedAssetOperation{
				Input: Input{
					SigIndices: []uint32{0},
				},
				ManagedAssetStatusOutput: ManagedAssetStatusOutput{
					Frozen: false,
					Manager: OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
				Mint: false,
				TransferOutput: TransferOutput{
					Amt: 0,
					OutputOwners: OutputOwners{
						Addrs: []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
			},
			shouldFailVerify: true,
		},
		{
			description: "mint; transferOut has no amount",
			op: &UpdateManagedAssetOperation{
				Input: Input{
					SigIndices: []uint32{0},
				},
				ManagedAssetStatusOutput: ManagedAssetStatusOutput{
					Frozen: false,
					Manager: OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
				Mint: true,
				TransferOutput: TransferOutput{
					Amt: 0,
					OutputOwners: OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
			},
			shouldFailVerify: true,
		},
		{
			description: "mint; transferOut has no threshold",
			op: &UpdateManagedAssetOperation{
				Input: Input{
					SigIndices: []uint32{0},
				},
				ManagedAssetStatusOutput: ManagedAssetStatusOutput{
					Frozen: false,
					Manager: OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
				Mint: true,
				TransferOutput: TransferOutput{
					Amt: 1,
					OutputOwners: OutputOwners{
						Locktime:  0,
						Threshold: 0,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
			},
			shouldFailVerify: true,
		},
		{
			description: "mint; transferOut threshold > len(addresses)",
			op: &UpdateManagedAssetOperation{
				Input: Input{
					SigIndices: []uint32{0},
				},
				ManagedAssetStatusOutput: ManagedAssetStatusOutput{
					Frozen: false,
					Manager: OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
				Mint: true,
				TransferOutput: TransferOutput{
					Amt: 1,
					OutputOwners: OutputOwners{
						Locktime:  0,
						Threshold: 2,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
			},
			shouldFailVerify: true,
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			err := test.op.Verify()
			if test.shouldFailVerify {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
