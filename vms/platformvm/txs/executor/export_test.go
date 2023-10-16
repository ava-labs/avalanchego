// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"

	ts "github.com/ava-labs/avalanchego/vms/platformvm/testsetup"
)

func TestNewExportTx(t *testing.T) {
	env := newEnvironment(t, ts.BanffFork)
	env.ctx.Lock.Lock()
	defer func() {
		require.NoError(t, shutdownEnvironment(env))
	}()

	type test struct {
		description        string
		destinationChainID ids.ID
		sourceKeys         []*secp256k1.PrivateKey
		timestamp          time.Time
	}

	sourceKey := ts.Keys[0]

	tests := []test{
		{
			description:        "P->X export",
			destinationChainID: ts.XChainID,
			sourceKeys:         []*secp256k1.PrivateKey{sourceKey},
			timestamp:          ts.ValidateStartTime,
		},
		{
			description:        "P->C export",
			destinationChainID: ts.CChainID,
			sourceKeys:         []*secp256k1.PrivateKey{sourceKey},
			timestamp:          env.config.ApricotPhase5Time,
		},
	}

	to := ids.GenerateTestShortID()
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			require := require.New(t)

			// ts.Balance holds the full Avax amount originally hold by any test keys
			// However in our test environment we add a subnet, so we need to account
			// for the reduced avax availability
			amountToExport := ts.Balance - env.config.GetCreateSubnetTxFee(env.clk.Time()) - ts.TxFee

			tx, err := env.txBuilder.NewExportTx(
				amountToExport,
				tt.destinationChainID,
				to,
				tt.sourceKeys,
				ids.ShortEmpty, // Change address
			)
			require.NoError(err)

			fakedState, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(err)

			fakedState.SetTimestamp(tt.timestamp)

			fakedParent := ids.GenerateTestID()
			env.SetState(fakedParent, fakedState)

			verifier := MempoolTxVerifier{
				Backend:       &env.backend,
				ParentID:      fakedParent,
				StateVersions: env,
				Tx:            tx,
			}
			require.NoError(tx.Unsigned.Visit(&verifier))
		})
	}
}
