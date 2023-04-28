// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"math"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/require"
)

func TestDepositTxSyntacticVerify(t *testing.T) {
	ctx := snow.DefaultContextTest()
	ctx.AVAXAssetID = ids.GenerateTestID()
	owner1 := secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{{1}}}

	tests := map[string]struct {
		tx          *DepositTx
		expectedErr error
	}{
		"Nil tx": {
			expectedErr: ErrNilTx,
		},
		"Bad reward owner": {
			tx: &DepositTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
				}},
				RewardsOwner: (*secp256k1fx.OutputOwners)(nil),
			},
			expectedErr: errInvalidRewardOwner,
		},
		"To big total deposit amount": {
			tx: &DepositTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, math.MaxUint64, owner1, locked.ThisTxID, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, math.MaxUint64, owner1, locked.ThisTxID, ids.Empty),
					},
				}},
				RewardsOwner: &secp256k1fx.OutputOwners{},
			},
			expectedErr: errToBigDeposit,
		},
		"OK": {
			tx: &DepositTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
				}},
				RewardsOwner: &secp256k1fx.OutputOwners{},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, tt.tx.SyntacticVerify(ctx), tt.expectedErr)
		})
	}
}
