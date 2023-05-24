// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"math"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/commands"
	"github.com/leanovate/gopter/gen"
)

var (
	_ Versions         = (*sysUnderTest)(nil)
	_ commands.Command = (*putCurrentValidatorCommand)(nil)
	_ commands.Command = (*updateCurrentValidatorCommand)(nil)
	_ commands.Command = (*deleteCurrentValidatorCommand)(nil)
	_ commands.Command = (*putCurrentDelegatorCommand)(nil)
	_ commands.Command = (*updateCurrentDelegatorCommand)(nil)
	_ commands.Command = (*deleteCurrentDelegatorCommand)(nil)
	_ commands.Command = (*addTopDiffCommand)(nil)
	_ commands.Command = (*applyBottomDiffCommand)(nil)
	_ commands.Command = (*commitBottomStateCommand)(nil)
)

// TestStateAndDiffComparisonToStorageModel verifies that a production-like
// system made of a stack of Diffs built on top of a State conforms to
// our stakersStorageModel. It achieves this by:
//  1. randomly generating a sequence of stakers writes as well as
//     some persistence operations (commit/diff apply),
//  2. applying the sequence to both our stakersStorageModel and the production-like system.
//  3. checking that both stakersStorageModel and the production-like system have
//     the same state after each operation.

func TestStateAndDiffComparisonToStorageModel(t *testing.T) {
	properties := gopter.NewProperties(nil)

	// to reproduce a given scenario do something like this:
	// parameters := gopter.DefaultTestParametersWithSeed(1680853360138133268)
	// properties := gopter.NewProperties(parameters)

	properties.Property("state comparison to storage model", commands.Prop(stakersCommands))
	properties.TestingRun(t)
}

type sysUnderTest struct {
	diffBlkIDSeed uint64
	baseState     State
	sortedDiffIDs []ids.ID
	diffsMap      map[ids.ID]Diff
}

func newSysUnderTest(baseState State) *sysUnderTest {
	sys := &sysUnderTest{
		baseState:     baseState,
		diffsMap:      map[ids.ID]Diff{},
		sortedDiffIDs: []ids.ID{},
	}
	return sys
}

func (s *sysUnderTest) GetState(blkID ids.ID) (Chain, bool) {
	if state, found := s.diffsMap[blkID]; found {
		return state, found
	}
	return s.baseState, blkID == s.baseState.GetLastAccepted()
}

func (s *sysUnderTest) addDiffOnTop() {
	newTopBlkID := ids.Empty.Prefix(atomic.AddUint64(&s.diffBlkIDSeed, 1))
	var topBlkID ids.ID
	if len(s.sortedDiffIDs) == 0 {
		topBlkID = s.baseState.GetLastAccepted()
	} else {
		topBlkID = s.sortedDiffIDs[len(s.sortedDiffIDs)-1]
	}
	newTopDiff, err := NewDiff(topBlkID, s)
	if err != nil {
		panic(err)
	}
	s.sortedDiffIDs = append(s.sortedDiffIDs, newTopBlkID)
	s.diffsMap[newTopBlkID] = newTopDiff
}

// getTopChainState returns top diff or baseState
func (s *sysUnderTest) getTopChainState() Chain {
	var topChainStateID ids.ID
	if len(s.sortedDiffIDs) != 0 {
		topChainStateID = s.sortedDiffIDs[len(s.sortedDiffIDs)-1]
	} else {
		topChainStateID = s.baseState.GetLastAccepted()
	}

	topChainState, _ := s.GetState(topChainStateID)
	return topChainState
}

// flushBottomDiff applies bottom diff if available
func (s *sysUnderTest) flushBottomDiff() bool {
	if len(s.sortedDiffIDs) == 0 {
		return false
	}
	bottomDiffID := s.sortedDiffIDs[0]
	diffToApply := s.diffsMap[bottomDiffID]

	err := diffToApply.Apply(s.baseState)
	if err != nil {
		panic(err)
	}
	s.baseState.SetLastAccepted(bottomDiffID)

	s.sortedDiffIDs = s.sortedDiffIDs[1:]
	delete(s.diffsMap, bottomDiffID)
	return true
}

// stakersCommands creates/destroy the system under test and generates
// commands and initial states (stakersStorageModel)
var stakersCommands = &commands.ProtoCommands{
	NewSystemUnderTestFunc: func(initialState commands.State) commands.SystemUnderTest {
		model := initialState.(*stakersStorageModel)
		baseState, err := buildChainState(nil)
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
	// Note: using gen.Const(newStakersStorageModel()) would not recreated model
	// among calls. Hence just use a dummy generated with sole purpose of recreating model
	InitialStateGen: gen.IntRange(1, 2).Map(
		func(int) *stakersStorageModel {
			return newStakersStorageModel()
		},
	),

	InitialPreConditionFunc: func(state commands.State) bool {
		return true // nothing to do for now
	},
	GenCommandFunc: func(state commands.State) gopter.Gen {
		return gen.OneGenOf(
			genPutCurrentValidatorCommand,
			genUpdateCurrentValidatorCommand,
			genDeleteCurrentValidatorCommand,

			genPutCurrentDelegatorCommand,
			genUpdateCurrentDelegatorCommand,
			genDeleteCurrentDelegatorCommand,

			genAddTopDiffCommand,
			genApplyBottomDiffCommand,
			genCommitBottomStateCommand,
		)
	},
}

// PutCurrentValidator section
type putCurrentValidatorCommand Staker

func (v *putCurrentValidatorCommand) Run(sut commands.SystemUnderTest) commands.Result {
	staker := (*Staker)(v)
	sys := sut.(*sysUnderTest)
	topChainState := sys.getTopChainState()
	topChainState.PutCurrentValidator(staker)
	return sys
}

func (v *putCurrentValidatorCommand) NextState(cmdState commands.State) commands.State {
	staker := (*Staker)(v)
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
	return fmt.Sprintf("PutCurrentValidator(subnetID: %v, nodeID: %v, txID: %v, priority: %v, unixStartTime: %v, duration: %v)",
		v.SubnetID, v.NodeID, v.TxID, v.Priority, v.StartTime, v.EndTime.Sub(v.StartTime))
}

var genPutCurrentValidatorCommand = stakerGenerator(currentValidator, nil, nil, math.MaxUint64).Map(
	func(staker Staker) commands.Command {
		cmd := (*putCurrentValidatorCommand)(&staker)
		return cmd
	},
)

// UpdateCurrentValidator section
type updateCurrentValidatorCommand struct{}

func (*updateCurrentValidatorCommand) Run(sut commands.SystemUnderTest) commands.Result {
	sys := sut.(*sysUnderTest)
	err := updateCurrentValidatorInSystem(sys)
	if err != nil {
		panic(err)
	}
	return sys
}

func updateCurrentValidatorInSystem(sys *sysUnderTest) error {
	// 1. check if there is a staker, already inserted. If not return
	// 2. Add diff layer on top (to test update across diff layers)
	// 3. query the staker
	// 4. Rotate staker times and update the staker

	chain := sys.getTopChainState()

	// 1. check if there is a staker, already inserted. If not return
	stakerIt, err := chain.GetCurrentStakerIterator()
	if err != nil {
		return err
	}

	var (
		found  = false
		staker *Staker
	)
	for !found && stakerIt.Next() {
		staker = stakerIt.Value()
		if staker.Priority == txs.SubnetPermissionedValidatorCurrentPriority ||
			staker.Priority == txs.SubnetPermissionlessValidatorCurrentPriority ||
			staker.Priority == txs.PrimaryNetworkValidatorCurrentPriority {
			found = true
		}
	}
	if !found {
		return nil // no current validator to update
	}
	stakerIt.Release()

	// 2. Add diff layer on top
	sys.addDiffOnTop()
	chain = sys.getTopChainState()

	// 3. query the staker
	staker, err = chain.GetCurrentValidator(staker.SubnetID, staker.NodeID)
	if err != nil {
		return err
	}

	// 4. Rotate staker times and update the staker
	updatedStaker := *staker
	RotateStakerTimesInPlace(&updatedStaker)
	return chain.UpdateCurrentValidator(&updatedStaker)
}

func (*updateCurrentValidatorCommand) NextState(cmdState commands.State) commands.State {
	model := cmdState.(*stakersStorageModel)

	err := updateCurrentValidatorInModel(model)
	if err != nil {
		panic(err)
	}
	return cmdState
}

func updateCurrentValidatorInModel(model *stakersStorageModel) error {
	stakerIt, err := model.GetCurrentStakerIterator()
	if err != nil {
		return err
	}

	var (
		found  = false
		staker *Staker
	)
	for !found && stakerIt.Next() {
		staker = stakerIt.Value()
		if staker.Priority == txs.SubnetPermissionedValidatorCurrentPriority ||
			staker.Priority == txs.SubnetPermissionlessValidatorCurrentPriority ||
			staker.Priority == txs.PrimaryNetworkValidatorCurrentPriority {
			found = true
		}
	}
	if !found {
		return nil // no current validator to update
	}
	stakerIt.Release()

	updatedStaker := *staker
	RotateStakerTimesInPlace(&updatedStaker)
	return model.UpdateCurrentValidator(&updatedStaker)
}

func (*updateCurrentValidatorCommand) PreCondition(commands.State) bool {
	return true
}

func (*updateCurrentValidatorCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	model := cmdState.(*stakersStorageModel)
	sys := res.(*sysUnderTest)

	if checkSystemAndModelContent(model, sys) {
		return &gopter.PropResult{Status: gopter.PropTrue}
	}

	return &gopter.PropResult{Status: gopter.PropFalse}
}

func (*updateCurrentValidatorCommand) String() string {
	return "AddDiffAndUpdateCurrentValidator"
}

var genUpdateCurrentValidatorCommand = gen.IntRange(1, 2).Map(
	func(int) commands.Command {
		return &updateCurrentValidatorCommand{}
	},
)

// DeleteCurrentValidator section
type deleteCurrentValidatorCommand struct{}

func (*deleteCurrentValidatorCommand) Run(sut commands.SystemUnderTest) commands.Result {
	// delete first validator, if any
	sys := sut.(*sysUnderTest)
	topDiff := sys.getTopChainState()

	stakerIt, err := topDiff.GetCurrentStakerIterator()
	if err != nil {
		panic(err)
	}

	var (
		found     = false
		validator *Staker
	)
	for !found && stakerIt.Next() {
		validator = stakerIt.Value()
		if validator.Priority == txs.SubnetPermissionedValidatorCurrentPriority ||
			validator.Priority == txs.SubnetPermissionlessValidatorCurrentPriority ||
			validator.Priority == txs.PrimaryNetworkValidatorCurrentPriority {
			found = true
		}
	}
	if !found {
		stakerIt.Release()
		return sys // no current validator to delete
	}
	stakerIt.Release() // release before modifying stakers collection

	topDiff.DeleteCurrentValidator(validator)
	return sys // returns sys to allow comparison with state in PostCondition
}

func (*deleteCurrentValidatorCommand) NextState(cmdState commands.State) commands.State {
	model := cmdState.(*stakersStorageModel)
	stakerIt, err := model.GetCurrentStakerIterator()
	if err != nil {
		return err
	}

	var (
		found     = false
		validator *Staker
	)
	for !found && stakerIt.Next() {
		validator = stakerIt.Value()
		if validator.Priority == txs.SubnetPermissionedValidatorCurrentPriority ||
			validator.Priority == txs.SubnetPermissionlessValidatorCurrentPriority ||
			validator.Priority == txs.PrimaryNetworkValidatorCurrentPriority {
			found = true
		}
	}
	if !found {
		stakerIt.Release()
		return cmdState // no current validator to add delegator to
	}
	stakerIt.Release() // release before modifying stakers collection

	model.DeleteCurrentValidator(validator)
	return cmdState
}

func (*deleteCurrentValidatorCommand) PreCondition(commands.State) bool {
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

func (*deleteCurrentValidatorCommand) String() string {
	return "DeleteCurrentValidator"
}

var genDeleteCurrentValidatorCommand = gen.IntRange(1, 2).Map(
	func(int) commands.Command {
		return &deleteCurrentValidatorCommand{}
	},
)

// PutCurrentDelegator section
type putCurrentDelegatorCommand Staker

func (v *putCurrentDelegatorCommand) Run(sut commands.SystemUnderTest) commands.Result {
	candidateDelegator := (*Staker)(v)
	sys := sut.(*sysUnderTest)
	err := addCurrentDelegatorInSystem(sys, candidateDelegator)
	if err != nil {
		panic(err)
	}
	return sys
}

func addCurrentDelegatorInSystem(sys *sysUnderTest, candidateDelegator *Staker) error {
	// 1. check if there is a current validator, already inserted. If not return
	// 2. Update candidateDelegator attributes to make it delegator of selected validator
	// 3. Add delegator to picked validator
	chain := sys.getTopChainState()

	// 1. check if there is a current validator. If not, nothing to do
	stakerIt, err := chain.GetCurrentStakerIterator()
	if err != nil {
		return err
	}

	var (
		found     = false
		validator *Staker
	)
	for !found && stakerIt.Next() {
		validator = stakerIt.Value()
		if validator.Priority == txs.SubnetPermissionedValidatorCurrentPriority ||
			validator.Priority == txs.SubnetPermissionlessValidatorCurrentPriority ||
			validator.Priority == txs.PrimaryNetworkValidatorCurrentPriority {
			found = true
		}
	}
	if !found {
		stakerIt.Release()
		return nil // no current validator to add delegator to
	}
	stakerIt.Release() // release before modifying stakers collection

	// 2. Add a delegator to it
	delegator := candidateDelegator
	delegator.SubnetID = validator.SubnetID
	delegator.NodeID = validator.NodeID

	chain.PutCurrentDelegator(delegator)
	return nil
}

func (v *putCurrentDelegatorCommand) NextState(cmdState commands.State) commands.State {
	candidateDelegator := (*Staker)(v)
	model := cmdState.(*stakersStorageModel)
	err := addCurrentDelegatorInModel(model, candidateDelegator)
	if err != nil {
		panic(err)
	}
	return cmdState
}

func addCurrentDelegatorInModel(model *stakersStorageModel, candidateDelegator *Staker) error {
	// 1. check if there is a current validator, already inserted. If not return
	// 2. Update candidateDelegator attributes to make it delegator of selected validator
	// 3. Add delegator to picked validator

	// 1. check if there is a current validator. If not, nothing to do
	stakerIt, err := model.GetCurrentStakerIterator()
	if err != nil {
		return err
	}

	var (
		found     = false
		validator *Staker
	)
	for !found && stakerIt.Next() {
		validator = stakerIt.Value()
		if validator.Priority == txs.SubnetPermissionedValidatorCurrentPriority ||
			validator.Priority == txs.SubnetPermissionlessValidatorCurrentPriority ||
			validator.Priority == txs.PrimaryNetworkValidatorCurrentPriority {
			found = true
		}
	}
	if !found {
		stakerIt.Release()
		return nil // no current validator to add delegator to
	}
	stakerIt.Release() // release before modifying stakers collection

	// 2. Add a delegator to it
	delegator := candidateDelegator
	delegator.SubnetID = validator.SubnetID
	delegator.NodeID = validator.NodeID

	model.PutCurrentDelegator(delegator)
	return nil
}

func (*putCurrentDelegatorCommand) PreCondition(commands.State) bool {
	return true
}

func (*putCurrentDelegatorCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	model := cmdState.(*stakersStorageModel)
	sys := res.(*sysUnderTest)

	if checkSystemAndModelContent(model, sys) {
		return &gopter.PropResult{Status: gopter.PropTrue}
	}

	return &gopter.PropResult{Status: gopter.PropFalse}
}

func (v *putCurrentDelegatorCommand) String() string {
	return fmt.Sprintf("PutCurrentDelegator(subnetID: %v, nodeID: %v, txID: %v, priority: %v, unixStartTime: %v, duration: %v)",
		v.SubnetID, v.NodeID, v.TxID, v.Priority, v.StartTime, v.EndTime.Sub(v.StartTime))
}

var genPutCurrentDelegatorCommand = stakerGenerator(currentDelegator, nil, nil, 1000).Map(
	func(staker Staker) commands.Command {
		cmd := (*putCurrentDelegatorCommand)(&staker)
		return cmd
	},
)

// UpdateCurrentDelegator section
type updateCurrentDelegatorCommand struct{}

func (*updateCurrentDelegatorCommand) Run(sut commands.SystemUnderTest) commands.Result {
	sys := sut.(*sysUnderTest)
	err := updateCurrentDelegatorInSystem(sys)
	if err != nil {
		panic(err)
	}
	return sys
}

func updateCurrentDelegatorInSystem(sys *sysUnderTest) error {
	// 1. check if there is a staker, already inserted. If not return
	// 2. Add diff layer on top (to test update across diff layers)
	// 3. Rotate staker times and update the staker

	chain := sys.getTopChainState()

	// 1. check if there is a delegator, already inserted. If not return
	stakerIt, err := chain.GetCurrentStakerIterator()
	if err != nil {
		return err
	}

	var (
		found     = false
		delegator *Staker
	)
	for !found && stakerIt.Next() {
		delegator = stakerIt.Value()
		if delegator.Priority == txs.SubnetPermissionlessDelegatorCurrentPriority ||
			delegator.Priority == txs.PrimaryNetworkDelegatorCurrentPriority {
			found = true
		}
	}
	if !found {
		return nil // no current validator to update
	}
	stakerIt.Release()

	// 2. Add diff layer on top
	sys.addDiffOnTop()
	chain = sys.getTopChainState()

	// 3. Rotate delegator times and update the staker
	updatedDelegator := *delegator
	RotateStakerTimesInPlace(&updatedDelegator)
	return chain.UpdateCurrentDelegator(&updatedDelegator)
}

func (*updateCurrentDelegatorCommand) NextState(cmdState commands.State) commands.State {
	model := cmdState.(*stakersStorageModel)

	err := updateCurrentDelegatorInModel(model)
	if err != nil {
		panic(err)
	}
	return cmdState
}

func updateCurrentDelegatorInModel(model *stakersStorageModel) error {
	stakerIt, err := model.GetCurrentStakerIterator()
	if err != nil {
		return err
	}

	var (
		found     = false
		delegator *Staker
	)
	for !found && stakerIt.Next() {
		delegator = stakerIt.Value()
		if delegator.Priority == txs.SubnetPermissionlessDelegatorCurrentPriority ||
			delegator.Priority == txs.PrimaryNetworkDelegatorCurrentPriority {
			found = true
		}
	}
	if !found {
		return nil // no current validator to update
	}
	stakerIt.Release()

	updatedDelegator := *delegator
	RotateStakerTimesInPlace(&updatedDelegator)
	return model.UpdateCurrentDelegator(&updatedDelegator)
}

func (*updateCurrentDelegatorCommand) PreCondition(commands.State) bool {
	return true
}

func (*updateCurrentDelegatorCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	model := cmdState.(*stakersStorageModel)
	sys := res.(*sysUnderTest)

	if checkSystemAndModelContent(model, sys) {
		return &gopter.PropResult{Status: gopter.PropTrue}
	}

	return &gopter.PropResult{Status: gopter.PropFalse}
}

func (*updateCurrentDelegatorCommand) String() string {
	return "AddDiffAndUpdateCurrentDelegator"
}

var genUpdateCurrentDelegatorCommand = gen.IntRange(1, 2).Map(
	func(int) commands.Command {
		return &updateCurrentDelegatorCommand{}
	},
)

// DeleteCurrentDelegator section
type deleteCurrentDelegatorCommand struct{}

func (*deleteCurrentDelegatorCommand) Run(sut commands.SystemUnderTest) commands.Result {
	sys := sut.(*sysUnderTest)
	_, err := deleteCurrentDelegator(sys)
	if err != nil {
		panic(err)
	}
	return sys // returns sys to allow comparison with state in PostCondition
}

func deleteCurrentDelegator(sys *sysUnderTest) (bool, error) {
	// delete first validator, if any
	topDiff := sys.getTopChainState()

	stakerIt, err := topDiff.GetCurrentStakerIterator()
	if err != nil {
		return false, err
	}

	var (
		found     = false
		delegator *Staker
	)
	for !found && stakerIt.Next() {
		delegator = stakerIt.Value()
		if delegator.Priority == txs.SubnetPermissionlessDelegatorCurrentPriority ||
			delegator.Priority == txs.PrimaryNetworkDelegatorCurrentPriority {
			found = true
		}
	}
	if !found {
		stakerIt.Release()
		return false, nil // no current validator to delete
	}
	stakerIt.Release() // release before modifying stakers collection

	topDiff.DeleteCurrentDelegator(delegator)
	return true, nil
}

func (*deleteCurrentDelegatorCommand) NextState(cmdState commands.State) commands.State {
	model := cmdState.(*stakersStorageModel)
	stakerIt, err := model.GetCurrentStakerIterator()
	if err != nil {
		return err
	}

	var (
		found     = false
		delegator *Staker
	)
	for !found && stakerIt.Next() {
		delegator = stakerIt.Value()
		if delegator.Priority == txs.SubnetPermissionlessDelegatorCurrentPriority ||
			delegator.Priority == txs.PrimaryNetworkDelegatorCurrentPriority {
			found = true
		}
	}
	if !found {
		stakerIt.Release()
		return cmdState // no current validator to add delegator to
	}
	stakerIt.Release() // release before modifying stakers collection

	model.DeleteCurrentDelegator(delegator)
	return cmdState
}

func (*deleteCurrentDelegatorCommand) PreCondition(commands.State) bool {
	return true
}

func (*deleteCurrentDelegatorCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	model := cmdState.(*stakersStorageModel)
	sys := res.(*sysUnderTest)

	if checkSystemAndModelContent(model, sys) {
		return &gopter.PropResult{Status: gopter.PropTrue}
	}

	return &gopter.PropResult{Status: gopter.PropFalse}
}

func (*deleteCurrentDelegatorCommand) String() string {
	return "DeleteCurrentDelegator"
}

var genDeleteCurrentDelegatorCommand = gen.IntRange(1, 2).Map(
	func(int) commands.Command {
		return &deleteCurrentDelegatorCommand{}
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

var genAddTopDiffCommand = gen.IntRange(1, 2).Map(
	func(int) commands.Command {
		return &addTopDiffCommand{}
	},
)

// applyBottomDiffCommand section
type applyBottomDiffCommand struct{}

func (*applyBottomDiffCommand) Run(sut commands.SystemUnderTest) commands.Result {
	sys := sut.(*sysUnderTest)
	_ = sys.flushBottomDiff()
	return sys
}

func (*applyBottomDiffCommand) NextState(cmdState commands.State) commands.State {
	return cmdState // model has no diffs
}

func (*applyBottomDiffCommand) PreCondition(commands.State) bool {
	return true
}

func (*applyBottomDiffCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	model := cmdState.(*stakersStorageModel)
	sys := res.(*sysUnderTest)

	if checkSystemAndModelContent(model, sys) {
		return &gopter.PropResult{Status: gopter.PropTrue}
	}

	return &gopter.PropResult{Status: gopter.PropFalse}
}

func (*applyBottomDiffCommand) String() string {
	return "ApplyBottomDiffCommand"
}

var genApplyBottomDiffCommand = gen.IntRange(1, 2).Map(
	func(int) commands.Command {
		return &applyBottomDiffCommand{}
	},
)

// commitBottomStateCommand section
type commitBottomStateCommand struct{}

func (*commitBottomStateCommand) Run(sut commands.SystemUnderTest) commands.Result {
	sys := sut.(*sysUnderTest)
	err := sys.baseState.Commit()
	if err != nil {
		panic(err)
	}
	return sys
}

func (*commitBottomStateCommand) NextState(cmdState commands.State) commands.State {
	return cmdState // model has no diffs
}

func (*commitBottomStateCommand) PreCondition(commands.State) bool {
	return true
}

func (*commitBottomStateCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	model := cmdState.(*stakersStorageModel)
	sys := res.(*sysUnderTest)

	if checkSystemAndModelContent(model, sys) {
		return &gopter.PropResult{Status: gopter.PropTrue}
	}

	return &gopter.PropResult{Status: gopter.PropFalse}
}

func (*commitBottomStateCommand) String() string {
	return "CommitBottomStateCommand"
}

var genCommitBottomStateCommand = gen.IntRange(1, 2).Map(
	func(int) commands.Command {
		return &commitBottomStateCommand{}
	},
)

func checkSystemAndModelContent(model *stakersStorageModel, sys *sysUnderTest) bool {
	// top view content must always match model content
	topDiff := sys.getTopChainState()

	modelIt, err := model.GetCurrentStakerIterator()
	if err != nil {
		return false
	}
	sysIt, err := topDiff.GetCurrentStakerIterator()
	if err != nil {
		return false
	}

	modelStakers := make([]*Staker, 0)
	for modelIt.Next() {
		modelStakers = append(modelStakers, modelIt.Value())
	}
	modelIt.Release()

	sysStakers := make([]*Staker, 0)
	for sysIt.Next() {
		sysStakers = append(sysStakers, sysIt.Value())
	}
	sysIt.Release()

	if len(modelStakers) != len(sysStakers) {
		return false
	}

	for idx, modelStaker := range modelStakers {
		sysStaker := sysStakers[idx]
		if modelStaker == nil || sysStaker == nil || !reflect.DeepEqual(modelStaker, sysStaker) {
			return false
		}
	}

	return true
}
