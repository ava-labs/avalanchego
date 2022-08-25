// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

func TestStandardTxExecutorAddValidatorTx(t *testing.T) {
	env := newEnvironment()
	env.ctx.Lock.Lock()
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()

	chainTime := env.state.GetTimestamp()
	startTime := defaultGenesisTime.Add(1 * time.Second)

	tests := []struct {
		blueberryTime time.Time
		expectedError error
	}{
		{ // Case: Before blueberry
			blueberryTime: chainTime.Add(1),
			expectedError: errIssuedAddStakerTxBeforeBlueberry,
		},
		{ // Case: At blueberry
			blueberryTime: chainTime,
			expectedError: errEmptyNodeID,
		},
		{ // Case: After blueberry
			blueberryTime: chainTime.Add(-1),
			expectedError: errEmptyNodeID,
		},
	}
	for _, test := range tests {
		// Case: Empty validator node ID after blueberry
		env.config.BlueberryTime = test.blueberryTime

		tx, err := env.txBuilder.NewAddValidatorTx( // create the tx
			env.config.MinValidatorStake,
			uint64(startTime.Unix()),
			uint64(defaultValidateEndTime.Unix()),
			ids.EmptyNodeID,
			ids.GenerateTestShortID(),
			reward.PercentDenominator,
			[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
			ids.ShortEmpty, // change addr
		)
		if err != nil {
			t.Fatal(err)
		}

		stateDiff, err := state.NewDiff(lastAcceptedID, env)
		if err != nil {
			t.Fatal(err)
		}

		executor := StandardTxExecutor{
			Backend: &env.backend,
			State:   stateDiff,
			Tx:      tx,
		}
		err = tx.Unsigned.Visit(&executor)
		require.ErrorIs(t, err, test.expectedError)
	}
}
