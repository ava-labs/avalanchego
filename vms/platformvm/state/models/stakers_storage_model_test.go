// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package models

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/commands"
	"github.com/leanovate/gopter/gen"
)

func TestStateAndDiffComparisonToStorageModel(t *testing.T) {
	properties := gopter.NewProperties(nil)
	properties.Property("state comparison to storage model", commands.Prop(stakersCommands))
	properties.TestingRun(t)
}

type sysUnderTest struct {
	blkIDList         []ids.ID
	blkIDToChainState map[ids.ID]state.Chain
}

// stakersCommands creates/destroy the system under test and generates
// commands and initial states (stakersStorageModel)
var stakersCommands = &commands.ProtoCommands{
	NewSystemUnderTestFunc: func(initialState commands.State) commands.SystemUnderTest {
		model := initialState.(*stakersStorageModel)
		baseState, err := buildChainState()
		if err != nil {
			panic(err)
		}

		// fillup baseState with model initial content
		for _, staker := range model.currentValidators {
			baseState.PutCurrentValidator(staker)
		}
		for _, delegators := range model.currentDelegators {
			for _, staker := range delegators {
				baseState.PutCurrentDelegator(staker)
			}
		}
		for _, staker := range model.pendingValidators {
			baseState.PutPendingValidator(staker)
		}
		for _, delegators := range model.currentDelegators {
			for _, staker := range delegators {
				baseState.PutPendingDelegator(staker)
			}
		}

		baseBlkID := baseState.GetLastAccepted()
		sys := &sysUnderTest{
			blkIDList: []ids.ID{baseBlkID},
			blkIDToChainState: map[ids.ID]state.Chain{
				baseBlkID: baseState,
			},
		}
		return sys
	},
	DestroySystemUnderTestFunc: func(sut commands.SystemUnderTest) {
		// retrieve base state and close it
		sys := sut.(*sysUnderTest)
		baseState := sys.blkIDToChainState[sys.blkIDList[0]]
		err := baseState.(state.State).Close()
		if err != nil {
			panic(err)
		}
	},
	InitialStateGen: gen.Const(newStakersStorageModel()), // TODO ABENEGIA: consider adding initial state
	InitialPreConditionFunc: func(state commands.State) bool {
		return true // nothing to do for now
	},
	GenCommandFunc: func(state commands.State) gopter.Gen {
		return gen.OneGenOf(
			genPutCurrentValidatorCommand,
			genDeleteCurrentValidatorCommand,
			// genPutCurrentDelegatorCommand,
			// genDeleteCurrentDelegatorCommand,

			// genApplyBottomDiffCommand,
			// genAddTopDiffCommand,
			// genCommitBottomStateCommand,
		)
	},
}

type putCurrentValidatorCommand state.Staker

func (v *putCurrentValidatorCommand) Run(sut commands.SystemUnderTest) commands.Result {
	staker := (*state.Staker)(v)
	sys := sut.(*sysUnderTest)
	topDiffID := sys.blkIDList[len(sys.blkIDList)-1]
	topDiff := sys.blkIDToChainState[topDiffID]
	topDiff.PutCurrentValidator(staker)
	return sys
}

func (v *putCurrentValidatorCommand) NextState(cmdState commands.State) commands.State {
	staker := (*state.Staker)(v)
	cmdState.(*stakersStorageModel).PutCurrentValidator(staker)
	return cmdState
}

func (*putCurrentValidatorCommand) PreCondition(commands.State) bool {
	// We allow inserting the same validator twice
	return true
}

func (*putCurrentValidatorCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	model := cmdState.(*stakersStorageModel)
	sys := res.(*sysUnderTest)

	if checkSystemAndModelContent(model, *sys) {
		return &gopter.PropResult{Status: gopter.PropTrue}
	}

	return &gopter.PropResult{Status: gopter.PropFalse}
}

func (v *putCurrentValidatorCommand) String() string {
	return fmt.Sprintf("PutCurrentValidator(subnetID: %s, nodeID: %s, txID: %s)", v.SubnetID, v.NodeID, v.TxID)
}

// We want to have a generator for put commands for arbitrary int values.
// In this case the command is actually shrinkable, e.g. if the property fails
// by putting a 1000, it might already fail for a 500 as well ...
var genPutCurrentValidatorCommand = stakerGenerator(anyPriority, nil, nil).Map(
	func(staker state.Staker) commands.Command {
		cmd := (*putCurrentValidatorCommand)(&staker)
		return cmd
	},
).WithShrinker(
	func(v interface{}) gopter.Shrink {
		return gen.IntShrinker(v.(putCurrentValidatorCommand)).Map(func(staker state.Staker) *putCurrentValidatorCommand {
			cmd := (*putCurrentValidatorCommand)(&staker)
			return cmd
		})
	},
)

type deleteCurrentValidatorCommand state.Staker

func (v *deleteCurrentValidatorCommand) Run(sut commands.SystemUnderTest) commands.Result {
	staker := (*state.Staker)(v)
	sys := sut.(*sysUnderTest)
	topDiffID := sys.blkIDList[len(sys.blkIDList)-1]
	topDiff := sys.blkIDToChainState[topDiffID]
	topDiff.DeleteCurrentValidator(staker)
	return sys // returns sys to allow comparison with state in PostCondition
}

func (v *deleteCurrentValidatorCommand) NextState(cmdState commands.State) commands.State {
	staker := (*state.Staker)(v)
	cmdState.(*stakersStorageModel).DeleteCurrentValidator(staker)
	return cmdState
}

func (*deleteCurrentValidatorCommand) PreCondition(commands.State) bool {
	// Don't even require staker to be inserted before being deleted
	return true
}

func (*deleteCurrentValidatorCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	model := cmdState.(*stakersStorageModel)
	sys := res.(*sysUnderTest)

	if checkSystemAndModelContent(model, *sys) {
		return &gopter.PropResult{Status: gopter.PropTrue}
	}

	return &gopter.PropResult{Status: gopter.PropFalse}
}

func (v *deleteCurrentValidatorCommand) String() string {
	return fmt.Sprintf("DeleteCurrentValidator(subnetID: %s, nodeID: %s, txID: %s)", v.SubnetID, v.NodeID, v.TxID)
}

// We want to have a generator for put commands for arbitrary int values.
// In this case the command is actually shrinkable, e.g. if the property fails
// by putting a 1000, it might already fail for a 500 as well ...
var genDeleteCurrentValidatorCommand = stakerGenerator(anyPriority, nil, nil).Map(
	func(staker state.Staker) commands.Command {
		cmd := (*deleteCurrentValidatorCommand)(&staker)
		return cmd
	},
).WithShrinker(
	func(v interface{}) gopter.Shrink {
		return gen.IntShrinker(v.(deleteCurrentValidatorCommand)).Map(func(staker state.Staker) *deleteCurrentValidatorCommand {
			cmd := (*deleteCurrentValidatorCommand)(&staker)
			return cmd
		})
	},
)

func checkSystemAndModelContent(model *stakersStorageModel, sys sysUnderTest) bool {
	// top view content must always match model content
	topDiffID := sys.blkIDList[len(sys.blkIDList)-1]
	topDiff := sys.blkIDToChainState[topDiffID]

	modelIt, err := model.GetCurrentStakerIterator()
	if err != nil {
		return false
	}
	sysIt, err := topDiff.GetCurrentStakerIterator()
	if err != nil {
		return false
	}

	for {
		modelNext := modelIt.Next()
		sysNext := sysIt.Next()
		if modelNext != sysNext {
			return false
		}
		if !sysNext {
			break // done with both model and sys iterations
		}

		modelStaker := modelIt.Value()
		sysStaker := sysIt.Value()

		if modelStaker == nil || sysStaker == nil || !reflect.DeepEqual(modelStaker, sysStaker) {
			return false
		}
	}

	modelIt.Release()
	sysIt.Release()
	return true
}
