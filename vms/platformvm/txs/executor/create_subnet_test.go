// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestCreateSubnetTxAP3FeeChange(t *testing.T) {
	ap3Time := defaultGenesisTime.Add(time.Hour)
	tests := []struct {
		name        string
		time        time.Time
		fee         uint64
		expectedErr error
	}{
		{
			name:        "pre-fork - correctly priced",
			time:        defaultGenesisTime,
			fee:         0,
			expectedErr: nil,
		},
		{
			name:        "post-fork - incorrectly priced",
			time:        ap3Time,
			fee:         100*defaultTxFee - 1*units.NanoAvax,
			expectedErr: utxo.ErrInsufficientUnlockedFunds,
		},
		{
			name:        "post-fork - correctly priced",
			time:        ap3Time,
			fee:         100 * defaultTxFee,
			expectedErr: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			env := newEnvironment(t, false /*=postBanff*/, false /*=postCortina*/, false /*=postDurango*/)
			env.config.ApricotPhase3Time = ap3Time
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()

			ins, outs, _, signers, err := env.utxosHandler.Spend(env.state, preFundedKeys, 0, test.fee, ids.ShortEmpty)
			require.NoError(err)

			// Create the tx
			utx := &txs.CreateSubnetTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins:          ins,
					Outs:         outs,
				}},
				Owner: &secp256k1fx.OutputOwners{},
			}
			tx := &txs.Tx{Unsigned: utx}
			require.NoError(tx.Sign(txs.Codec, signers))

			stateDiff, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(err)

			stateDiff.SetTimestamp(test.time)

			executor := StandardTxExecutor{
				Backend: &env.backend,
				State:   stateDiff,
				Tx:      tx,
			}
			err = tx.Unsigned.Visit(&executor)
			require.ErrorIs(err, test.expectedErr)
		})
	}
}
