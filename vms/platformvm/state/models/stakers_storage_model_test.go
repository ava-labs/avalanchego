// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package models

import (
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/commands"
	"github.com/leanovate/gopter/gen"
)

var (
	_ state.Versions   = (*sysUnderTest)(nil)
	_ commands.Command = (*putCurrentValidatorCommand)(nil)
	_ commands.Command = (*deleteCurrentValidatorCommand)(nil)
	_ commands.Command = (*addTopDiffCommand)(nil)
)

func TestStateAndDiffComparisonToStorageModel(t *testing.T) {
	properties := gopter.NewProperties(nil)
	properties.Property("state comparison to storage model", commands.Prop(stakersCommands))
	properties.TestingRun(t)
}

type sysUnderTest struct {
	baseState         state.State
	blkIDsByHeight    []ids.ID
	blkIDToChainState map[ids.ID]state.Chain
}

func newSysUnderTest(baseState state.State) *sysUnderTest {
	baseBlkID := baseState.GetLastAccepted()
	sys := &sysUnderTest{
		baseState:         baseState,
		blkIDToChainState: map[ids.ID]state.Chain{},
		blkIDsByHeight:    []ids.ID{baseBlkID},
	}
	return sys
}

func (s *sysUnderTest) GetState(blkID ids.ID) (state.Chain, bool) {
	if state, found := s.blkIDToChainState[blkID]; found {
		return state, found
	}
	return s.baseState, blkID == s.baseState.GetLastAccepted()
}

func (s *sysUnderTest) addDiffOnTop() {
	seed := uint64(len(s.blkIDsByHeight))
	newTopBlkID := ids.Empty.Prefix(atomic.AddUint64(&seed, 1))
	topBlkID := s.blkIDsByHeight[len(s.blkIDsByHeight)-1]
	newTopDiff, err := state.NewDiff(topBlkID, s)
	if err != nil {
		panic(err)
	}
	s.blkIDsByHeight = append(s.blkIDsByHeight, newTopBlkID)
	s.blkIDToChainState[newTopBlkID] = newTopDiff
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

		return newSysUnderTest(baseState)
	},
	DestroySystemUnderTestFunc: func(sut commands.SystemUnderTest) {
		// retrieve base state and close it
		sys := sut.(*sysUnderTest)
		err := sys.baseState.Close()
		if err != nil {
			panic(err)
		}
	},
	// TODO ABENEGIA: using gen.Const(newStakersStorageModel()) would not recreated model
	// among calls. Hence just use a dummy generated with sole purpose of recreating model
	InitialStateGen: gen.IntRange(1, 2).Map(func(int) *stakersStorageModel {
		return newStakersStorageModel()
	}),

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
			genAddTopDiffCommand,
			// genCommitBottomStateCommand,
		)
	},
}

// PutCurrentValidator section
type putCurrentValidatorCommand state.Staker

func (v *putCurrentValidatorCommand) Run(sut commands.SystemUnderTest) commands.Result {
	staker := (*state.Staker)(v)
	sys := sut.(*sysUnderTest)
	topBlkID := sys.blkIDsByHeight[len(sys.blkIDsByHeight)-1]
	topDiff, _ := sys.GetState(topBlkID)
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

	if checkSystemAndModelContent(model, sys) {
		return &gopter.PropResult{Status: gopter.PropTrue}
	}

	return &gopter.PropResult{Status: gopter.PropFalse}
}

func (v *putCurrentValidatorCommand) String() string {
	return fmt.Sprintf("PutCurrentValidator(subnetID: %s, nodeID: %s, txID: %s)", v.SubnetID, v.NodeID, v.TxID)
}

var genPutCurrentValidatorCommand = stakerGenerator(anyPriority, nil, nil).Map(
	func(staker state.Staker) commands.Command {
		cmd := (*putCurrentValidatorCommand)(&staker)
		return cmd
	},
)

// DeleteCurrentValidator section
type deleteCurrentValidatorCommand state.Staker

func (v *deleteCurrentValidatorCommand) Run(sut commands.SystemUnderTest) commands.Result {
	staker := (*state.Staker)(v)
	sys := sut.(*sysUnderTest)
	topBlkID := sys.blkIDsByHeight[len(sys.blkIDsByHeight)-1]
	topDiff, _ := sys.GetState(topBlkID)
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

	if checkSystemAndModelContent(model, sys) {
		return &gopter.PropResult{Status: gopter.PropTrue}
	}

	return &gopter.PropResult{Status: gopter.PropFalse}
}

func (v *deleteCurrentValidatorCommand) String() string {
	return fmt.Sprintf("DeleteCurrentValidator(subnetID: %s, nodeID: %s, txID: %s)", v.SubnetID, v.NodeID, v.TxID)
}

var genDeleteCurrentValidatorCommand = stakerGenerator(anyPriority, nil, nil).Map(
	func(staker state.Staker) commands.Command {
		cmd := (*deleteCurrentValidatorCommand)(&staker)
		return cmd
	},
)

// addTopDiffCommand section
type addTopDiffCommand struct{}

func (*addTopDiffCommand) Run(sut commands.SystemUnderTest) commands.Result {
	sys := sut.(*sysUnderTest)
	sys.addDiffOnTop()
	return sys
}

func (*addTopDiffCommand) NextState(cmdState commands.State) commands.State {
	return cmdState // model has no diffs
}

func (*addTopDiffCommand) PreCondition(commands.State) bool {
	return true
}

func (*addTopDiffCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	model := cmdState.(*stakersStorageModel)
	sys := res.(*sysUnderTest)

	if checkSystemAndModelContent(model, sys) {
		return &gopter.PropResult{Status: gopter.PropTrue}
	}

	return &gopter.PropResult{Status: gopter.PropFalse}
}

func (*addTopDiffCommand) String() string {
	return "AddTopDiffCommand"
}

var genAddTopDiffCommand = stakerGenerator(anyPriority, nil, nil).Map(
	func(state.Staker) commands.Command {
		return &addTopDiffCommand{}
	},
)

func checkSystemAndModelContent(model *stakersStorageModel, sys *sysUnderTest) bool {
	// top view content must always match model content
	topBlkID := sys.blkIDsByHeight[len(sys.blkIDsByHeight)-1]
	topDiff, _ := sys.GetState(topBlkID)

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
