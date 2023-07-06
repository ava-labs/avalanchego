// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"math"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/prop"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func TestUpdateAndImmediatelyDeleteValidator(t *testing.T) {
	properties := gopter.NewProperties(nil)

	// // to reproduce a given scenario do something like this:
	// parameters := gopter.DefaultTestParametersWithSeed(1688591586962580727)
	// properties := gopter.NewProperties(parameters)

	nodeID := ids.GenerateTestNodeID()

	properties.Property("delete and query committed current validators", prop.ForAll(
		func(nonInitValTx *txs.Tx) string {
			// build state
			baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
			baseDB := versiondb.New(baseDBManager.Current().Database)
			baseState, err := buildChainState(baseDB, nil)
			if err != nil {
				return "failed creating base state"
			}
			sys := newSysUnderTest(baseDB, baseState)

			// create validator and delegator
			sValTx, err := txs.NewSigned(nonInitValTx.Unsigned, txs.Codec, nil)
			if err != nil {
				return fmt.Sprintf("failed signing val tx, %v", err)
			}
			currentVal, err := NewCurrentStaker(sValTx.ID(), sValTx.Unsigned.(txs.StakerTx), uint64(1000))
			if err != nil {
				return fmt.Sprintf("failed creating current validator, %v", err)
			}

			// store and commit validator first
			sys.baseState.PutCurrentValidator(currentVal)
			if err := sys.baseState.Commit(); err != nil {
				return fmt.Sprintf("failed committing current validator, %v", err)
			}

			// on a diff update and delete the validator
			if err := sys.addDiffOnTop(); err != nil {
				return fmt.Sprintf("failed adding diff, %v", err)
			}
			updateDiff := sys.getTopChainState().(*diff)

			updatedVal := *currentVal
			IncreaseStakerWeightInPlace(&updatedVal, currentVal.Weight*2)

			if err := updateDiff.UpdateCurrentValidator(&updatedVal); err != nil {
				return fmt.Sprintf("failed updating validator, %v", err)
			}

			// on another diff delete the validator
			if err := sys.addDiffOnTop(); err != nil {
				return fmt.Sprintf("failed adding diff, %v", err)
			}
			deleteDiff := sys.getTopChainState().(*diff)
			deleteDiff.DeleteCurrentValidator(&updatedVal)

			// apply boths diffs before commit
			if err := updateDiff.Apply(sys.baseState); err != nil {
				return fmt.Sprintf("failed applying diff, %v", err)
			}

			if err := deleteDiff.Apply(sys.baseState); err != nil {
				return fmt.Sprintf("failed applying diff, %v", err)
			}

			if err := sys.baseState.Commit(); err != nil {
				return fmt.Sprintf("failed committing validator changes, %v", err)
			}

			return ""
		},
		stakerTxGenerator(commandsCtx, permissionedValidator, &constants.PrimaryNetworkID, &nodeID, math.MaxUint64),
	))

	properties.TestingRun(t)
}
