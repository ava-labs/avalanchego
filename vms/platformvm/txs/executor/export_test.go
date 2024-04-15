// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/upgrade"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestNewExportTx(t *testing.T) {
	type test struct {
		description        string
		destinationChainID ids.ID
		sourceKeys         []*secp256k1.PrivateKey
		fork               upgrade.Upgrade
	}

	ctx := snowtest.Context(t, snowtest.PChainID) // the context used in env
	sourceKey := preFundedKeys[0]

	tests := []test{
		{
			description:        "P->X export",
			destinationChainID: ctx.XChainID,
			sourceKeys:         []*secp256k1.PrivateKey{sourceKey},
			fork:               upgrade.ApricotPhase3,
		},
		{
			description:        "P->C export",
			destinationChainID: ctx.CChainID,
			sourceKeys:         []*secp256k1.PrivateKey{sourceKey},
			fork:               upgrade.ApricotPhase5,
		},
	}

	to := ids.GenerateTestShortID()
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			require := require.New(t)
			env := newEnvironment(t, tt.fork)
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()

			tx, err := env.txBuilder.NewExportTx(
				tt.destinationChainID,
				[]*avax.TransferableOutput{{
					Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: defaultBalance - defaultTxFee,
						OutputOwners: secp256k1fx.OutputOwners{
							Locktime:  0,
							Threshold: 1,
							Addrs:     []ids.ShortID{to},
						},
					},
				}},
				tt.sourceKeys,
			)
			require.NoError(err)

			stateDiff, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(err)

			verifier := StandardTxExecutor{
				Backend: &env.backend,
				State:   stateDiff,
				Tx:      tx,
			}
			require.NoError(tx.Unsigned.Visit(&verifier))
		})
	}
}
