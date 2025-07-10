// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestUnsignedCreateChainTxVerify(t *testing.T) {
	ctx := snowtest.Context(t, snowtest.PChainID)
	testSubnet1ID := ids.GenerateTestID()

	type test struct {
		description string
		subnetID    ids.ID
		genesisData []byte
		vmID        ids.ID
		fxIDs       []ids.ID
		chainName   string
		setup       func(*CreateChainTx) *CreateChainTx
		expectedErr error
	}

	tests := []test{
		{
			description: "tx is nil",
			subnetID:    testSubnet1ID,
			genesisData: nil,
			vmID:        constants.AVMID,
			fxIDs:       nil,
			chainName:   "yeet",
			setup: func(*CreateChainTx) *CreateChainTx {
				return nil
			},
			expectedErr: ErrNilTx,
		},
		{
			description: "vm ID is empty",
			subnetID:    testSubnet1ID,
			genesisData: nil,
			vmID:        constants.AVMID,
			fxIDs:       nil,
			chainName:   "yeet",
			setup: func(tx *CreateChainTx) *CreateChainTx {
				tx.VMID = ids.Empty
				return tx
			},
			expectedErr: errInvalidVMID,
		},
		{
			description: "subnet ID is platform chain's ID",
			subnetID:    testSubnet1ID,
			genesisData: nil,
			vmID:        constants.AVMID,
			fxIDs:       nil,
			chainName:   "yeet",
			setup: func(tx *CreateChainTx) *CreateChainTx {
				tx.SubnetID = ctx.ChainID
				return tx
			},
			expectedErr: ErrCantValidatePrimaryNetwork,
		},
		{
			description: "chain name is too long",
			subnetID:    testSubnet1ID,
			genesisData: nil,
			vmID:        constants.AVMID,
			fxIDs:       nil,
			chainName:   "yeet",
			setup: func(tx *CreateChainTx) *CreateChainTx {
				tx.ChainName = string(make([]byte, MaxNameLen+1))
				return tx
			},
			expectedErr: errNameTooLong,
		},
		{
			description: "chain name has invalid character",
			subnetID:    testSubnet1ID,
			genesisData: nil,
			vmID:        constants.AVMID,
			fxIDs:       nil,
			chainName:   "yeet",
			setup: func(tx *CreateChainTx) *CreateChainTx {
				tx.ChainName = "⌘"
				return tx
			},
			expectedErr: errIllegalNameCharacter,
		},
		{
			description: "genesis data is too long",
			subnetID:    testSubnet1ID,
			genesisData: nil,
			vmID:        constants.AVMID,
			fxIDs:       nil,
			chainName:   "yeet",
			setup: func(tx *CreateChainTx) *CreateChainTx {
				tx.GenesisData = make([]byte, MaxGenesisLen+1)
				return tx
			},
			expectedErr: errGenesisTooLong,
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			require := require.New(t)

			inputs := []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID:        ids.ID{'t', 'x', 'I', 'D'},
					OutputIndex: 2,
				},
				Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 't'}},
				In: &secp256k1fx.TransferInput{
					Amt:   uint64(5678),
					Input: secp256k1fx.Input{SigIndices: []uint32{0}},
				},
			}}
			outputs := []*avax.TransferableOutput{{
				Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 't'}},
				Out: &secp256k1fx.TransferOutput{
					Amt: uint64(1234),
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{preFundedKeys[0].Address()},
					},
				},
			}}
			subnetAuth := &secp256k1fx.Input{
				SigIndices: []uint32{0, 1},
			}

			createChainTx := &CreateChainTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Ins:          inputs,
					Outs:         outputs,
				}},
				SubnetID:    test.subnetID,
				ChainName:   test.chainName,
				VMID:        test.vmID,
				FxIDs:       test.fxIDs,
				GenesisData: test.genesisData,
				SubnetAuth:  subnetAuth,
			}

			signers := [][]*secp256k1.PrivateKey{preFundedKeys}
			stx, err := NewSigned(createChainTx, Codec, signers)
			require.NoError(err)

			createChainTx.SyntacticallyVerified = false
			stx.Unsigned = test.setup(createChainTx)

			err = stx.SyntacticVerify(ctx)
			require.ErrorIs(err, test.expectedErr)
		})
	}
}
