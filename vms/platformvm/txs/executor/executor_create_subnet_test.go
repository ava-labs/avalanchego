// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestCreateSubnetTxAP3FeeChange(t *testing.T) {
	ap3Time := defaultGenesisTime.Add(time.Hour)
	tests := []struct {
		name         string
		time         time.Time
		fee          uint64
		expectsError bool
	}{
		{
			name:         "pre-fork - correctly priced",
			time:         defaultGenesisTime,
			fee:          0,
			expectsError: false,
		},
		{
			name:         "post-fork - incorrectly priced",
			time:         ap3Time,
			fee:          100*defaultTxFee - 1*units.NanoAvax,
			expectsError: true,
		},
		{
			name:         "post-fork - correctly priced",
			time:         ap3Time,
			fee:          100 * defaultTxFee,
			expectsError: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert := assert.New(t)

			h := newTestHelpersCollection()
			h.cfg.ApricotPhase3Time = ap3Time
			defer func() {
				if err := internalStateShutdown(h); err != nil {
					t.Fatal(err)
				}
			}()

			ins, outs, _, signers, err := h.utxosMan.Stake(preFundedKeys, 0, test.fee, ids.ShortEmpty)
			assert.NoError(err)

			// Create the tx
			utx := &txs.CreateSubnetTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    vm.ctx.NetworkID,
					BlockchainID: vm.ctx.ChainID,
					Ins:          ins,
					Outs:         outs,
				}},
				Owner: &secp256k1fx.OutputOwners{},
			}
			tx := &txs.Tx{Unsigned: utx}
			err = tx.Sign(Codec, signers)
			assert.NoError(err)

			state := state.NewVersioned(
				vm.internalState,
				vm.internalState.CurrentStakerChainState(),
				vm.internalState.PendingStakerChainState(),
			)
			state.SetTimestamp(test.time)

			executor := standardTxExecutor{
				vm:    vm,
				state: state,
				tx:    tx,
			}
			err = tx.Unsigned.Visit(&executor)
			assert.Equal(test.expectsError, err != nil)
		})
	}
}
