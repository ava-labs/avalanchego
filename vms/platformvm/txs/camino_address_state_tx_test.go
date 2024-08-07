// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	as "github.com/ava-labs/avalanchego/vms/platformvm/addrstate"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/test/generate"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestAddressStateTxSyntacticVerify(t *testing.T) {
	ctx := defaultContext()
	addr1 := ids.ShortID{1}
	lockTxID := ids.ID{2}
	owner1 := secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{addr1}}

	baseTx := BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    ctx.NetworkID,
		BlockchainID: ctx.ChainID,
	}}

	type testCase struct {
		tx          *AddressStateTx
		expectedErr error
	}

	tests := map[string]testCase{
		"Nil tx": {
			expectedErr: ErrNilTx,
		},
		"Empty target address": {
			tx: &AddressStateTx{
				BaseTx: baseTx,
			},
			expectedErr: errEmptyAddress,
		},
		"Address state bit is greater than max possible bit": {
			tx: &AddressStateTx{
				BaseTx:   baseTx,
				Address:  addr1,
				StateBit: as.AddressStateBitMax + 1,
			},
			expectedErr: errInvalidAddrStateBit,
		},
		"UpgradeVersion1, bad executor auth": {
			tx: &AddressStateTx{
				UpgradeVersionID: codec.UpgradeVersion1,
				BaseTx:           baseTx,
				Address:          addr1,
				ExecutorAuth:     (*secp256k1fx.Input)(nil),
			},
			expectedErr: errBadExecutorAuth,
		},
		"UpgradeVersion1, empty executor address": {
			tx: &AddressStateTx{
				UpgradeVersionID: codec.UpgradeVersion1,
				BaseTx:           baseTx,
				Address:          addr1,
				ExecutorAuth:     &secp256k1fx.Input{},
			},
			expectedErr: ErrEmptyExecutorAddress,
		},
		"Stakeable base tx input": {
			tx: &AddressStateTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Ins: []*avax.TransferableInput{
						generate.StakeableIn(ctx.AVAXAssetID, 1, 1, []uint32{0}),
					},
				}},
				Address: addr1,
			},
			expectedErr: locked.ErrWrongInType,
		},
		"Stakeable base tx output": {
			tx: &AddressStateTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Outs: []*avax.TransferableOutput{
						generate.StakeableOut(ctx.AVAXAssetID, 1, 1, owner1),
					},
				}},
				Address: addr1,
			},
			expectedErr: locked.ErrWrongOutType,
		},
		"Locked base tx input": {
			tx: &AddressStateTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Ins: []*avax.TransferableInput{
						generate.In(ctx.AVAXAssetID, 1, lockTxID, ids.Empty, []uint32{0}),
					},
				}},
				Address: addr1,
			},
			expectedErr: locked.ErrWrongInType,
		},
		"Locked base tx output": {
			tx: &AddressStateTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Outs: []*avax.TransferableOutput{
						generate.Out(ctx.AVAXAssetID, 1, owner1, lockTxID, ids.Empty),
					},
				}},
				Address: addr1,
			},
			expectedErr: locked.ErrWrongOutType,
		},
		"OK: UpgradeVersion0": {
			tx: &AddressStateTx{
				BaseTx:  baseTx,
				Address: addr1,
			},
		},
		"OK: UpgradeVersion1": {
			tx: &AddressStateTx{
				UpgradeVersionID: codec.UpgradeVersion1,
				BaseTx:           baseTx,
				Address:          addr1,
				Executor:         addr1,
				ExecutorAuth:     &secp256k1fx.Input{},
			},
		},
	}

	// bit range test cases
	for bit := as.AddressStateBit(0); bit <= as.AddressStateBitMax; bit++ {
		tx := &AddressStateTx{
			BaseTx:   baseTx,
			Address:  addr1,
			StateBit: bit,
		}
		if bit.ToAddressState()&as.AddressStateValidBits == 0 {
			testCaseName := fmt.Sprintf("BitValidity/%02d is invalid", bit)
			require.NotContains(t, tests, testCaseName)
			tests[testCaseName] = testCase{
				tx:          tx,
				expectedErr: errInvalidAddrStateBit,
			}
		} else {
			testCaseName := fmt.Sprintf("BitValidity/OK: %02d", bit)
			require.NotContains(t, tests, testCaseName)
			tests[testCaseName] = testCase{
				tx: tx,
			}
		}
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := tt.tx.SyntacticVerify(ctx)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}
