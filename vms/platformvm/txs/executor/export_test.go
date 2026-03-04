// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestNewExportTx(t *testing.T) {
	env := newEnvironment(t, upgradetest.Banff)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	tests := []struct {
		description        string
		destinationChainID ids.ID
		timestamp          time.Time
	}{
		{
			description:        "P->X export",
			destinationChainID: env.ctx.XChainID,
			timestamp:          genesistest.DefaultValidatorStartTime,
		},
		{
			description:        "P->C export",
			destinationChainID: env.ctx.CChainID,
			timestamp:          env.config.UpgradeConfig.ApricotPhase5Time,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			require := require.New(t)

			wallet := newWallet(t, env, walletConfig{})

			tx, err := wallet.IssueExportTx(
				tt.destinationChainID,
				[]*avax.TransferableOutput{{
					Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: genesistest.DefaultInitialBalance - defaultTxFee,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
						},
					},
				}},
			)
			require.NoError(err)

			stateDiff, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(err)

			stateDiff.SetTimestamp(tt.timestamp)

			feeCalculator := state.PickFeeCalculator(env.config, stateDiff)
			_, _, _, err = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				stateDiff,
			)
			require.NoError(err)
		})
	}
}
