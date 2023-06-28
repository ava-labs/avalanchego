// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/commands"
	"github.com/leanovate/gopter/gen"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ Versions         = (*sysUnderTest)(nil)
	_ commands.Command = (*putCurrentValidatorCommand)(nil)
	_ commands.Command = (*deleteCurrentValidatorCommand)(nil)
	_ commands.Command = (*putCurrentDelegatorCommand)(nil)
	_ commands.Command = (*deleteCurrentDelegatorCommand)(nil)
	_ commands.Command = (*addTopDiffCommand)(nil)
	_ commands.Command = (*applyBottomDiffCommand)(nil)
	_ commands.Command = (*commitBottomStateCommand)(nil)
	_ commands.Command = (*rebuildStateCommand)(nil)

	commandsCtx = buildStateCtx()
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
	// parameters := gopter.DefaultTestParametersWithSeed(1680269995295922009)
	// properties := gopter.NewProperties(parameters)

	properties.Property("state comparison to storage model", commands.Prop(stakersCommands))
	properties.TestingRun(t)
}

type sysUnderTest struct {
	diffBlkIDSeed uint64
	baseDB        database.Database
	baseState     State
	sortedDiffIDs []ids.ID
	diffsMap      map[ids.ID]Diff
}

func newSysUnderTest(baseDB database.Database, baseState State) *sysUnderTest {
	sys := &sysUnderTest{
		baseDB:        baseDB,
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

		baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
		baseDB := versiondb.New(baseDBManager.Current().Database)
		baseState, err := buildChainState(baseDB, nil)
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
		if err := baseState.Commit(); err != nil {
			panic(err)
		}

		return newSysUnderTest(baseDB, baseState)
	},
	DestroySystemUnderTestFunc: func(sut commands.SystemUnderTest) {
		// retrieve base state and close it
		sys := sut.(*sysUnderTest)
		err := sys.baseState.Close()
		if err != nil {
			panic(err)
		}
	},
	// a trick to force command regeneration at each sampling.
	// gen.Const would not allow it
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
			genDeleteCurrentValidatorCommand,

			genPutCurrentDelegatorCommand,
			genDeleteCurrentDelegatorCommand,

			genAddTopDiffCommand,
			genApplyBottomDiffCommand,
			genCommitBottomStateCommand,
			genRebuildStateCommand,
		)
	},
}

// PutCurrentValidator section
type putCurrentValidatorCommand txs.Tx

func (v *putCurrentValidatorCommand) Run(sut commands.SystemUnderTest) commands.Result {
	sTx := (*txs.Tx)(v)
	sys := sut.(*sysUnderTest)

	currentVal, err := NewCurrentStaker(sTx.ID(), sTx.Unsigned.(txs.Staker), uint64(1000))
	if err != nil {
		return sys // state checks later on should spot missing validator
	}

	topChainState := sys.getTopChainState()
	topChainState.PutCurrentValidator(currentVal)
	topChainState.AddTx(sTx, status.Committed)
	return sys
}

func (v *putCurrentValidatorCommand) NextState(cmdState commands.State) commands.State {
	sTx := (*txs.Tx)(v)
	currentVal, err := NewCurrentStaker(sTx.ID(), sTx.Unsigned.(txs.Staker), uint64(1000))
	if err != nil {
		return cmdState // state checks later on should spot missing validator
	}

	cmdState.(*stakersStorageModel).PutCurrentValidator(currentVal)
	return cmdState
}

func (*putCurrentValidatorCommand) PreCondition(commands.State) bool {
	// We allow inserting the same validator twice
	return true
}

func (*putCurrentValidatorCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	return checkSystemAndModelContent(cmdState, res)
}

func (v *putCurrentValidatorCommand) String() string {
	stakerTx := v.Unsigned.(txs.StakerTx)
	return fmt.Sprintf("PutCurrentValidator(subnetID: %v, nodeID: %v, txID: %v, priority: %v, unixStartTime: %v, duration: %v)",
		stakerTx.SubnetID(),
		stakerTx.NodeID(),
		v.TxID,
		stakerTx.CurrentPriority(),
		stakerTx.StartTime().Unix(),
		stakerTx.EndTime().Sub(stakerTx.StartTime()),
	)
}

var genPutCurrentValidatorCommand = addPermissionlessValidatorTxGenerator(commandsCtx, nil, nil, &signer.Empty{}).Map(
	func(nonInitTx *txs.Tx) commands.Command {
		sTx, err := txs.NewSigned(nonInitTx.Unsigned, txs.Codec, nil)
		if err != nil {
			panic(fmt.Errorf("failed signing tx, %w", err))
		}

		cmd := (*putCurrentValidatorCommand)(sTx)
		return cmd
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
			break
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
			break
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
	// We allow deleting an un-existing validator
	return true
}

func (*deleteCurrentValidatorCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	return checkSystemAndModelContent(cmdState, res)
}

func (*deleteCurrentValidatorCommand) String() string {
	return "DeleteCurrentValidator"
}

// a trick to force command regeneration at each sampling.
// gen.Const would not allow it
var genDeleteCurrentValidatorCommand = gen.IntRange(1, 2).Map(
	func(int) commands.Command {
		return &deleteCurrentValidatorCommand{}
	},
)

// PutCurrentDelegator section
type putCurrentDelegatorCommand txs.Tx

func (v *putCurrentDelegatorCommand) Run(sut commands.SystemUnderTest) commands.Result {
	candidateDelegator := (*txs.Tx)(v)
	sys := sut.(*sysUnderTest)
	err := addCurrentDelegatorInSystem(sys, candidateDelegator.Unsigned)
	if err != nil {
		panic(err)
	}
	return sys
}

func addCurrentDelegatorInSystem(sys *sysUnderTest, candidateDelegatorTx txs.UnsignedTx) error {
	// 1. check if there is a current validator, already inserted. If not return
	// 2. Update candidateDelegatorTx attributes to make it delegator of selected validator
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
			break
		}
	}
	if !found {
		stakerIt.Release()
		return nil // no current validator to add delegator to
	}
	stakerIt.Release() // release before modifying stakers collection

	// 2. Add a delegator to it
	addPermissionlessDelTx := candidateDelegatorTx.(*txs.AddPermissionlessDelegatorTx)
	addPermissionlessDelTx.Subnet = validator.SubnetID
	addPermissionlessDelTx.Validator.NodeID = validator.NodeID

	signedTx, err := txs.NewSigned(addPermissionlessDelTx, txs.Codec, nil)
	if err != nil {
		return fmt.Errorf("failed signing tx, %w", err)
	}

	delegator, err := NewCurrentStaker(signedTx.ID(), signedTx.Unsigned.(txs.Staker), uint64(1000))
	if err != nil {
		return fmt.Errorf("failed generating staker, %w", err)
	}

	chain.PutCurrentDelegator(delegator)
	chain.AddTx(signedTx, status.Committed)
	return nil
}

func (v *putCurrentDelegatorCommand) NextState(cmdState commands.State) commands.State {
	candidateDelegator := (*txs.Tx)(v)
	model := cmdState.(*stakersStorageModel)
	err := addCurrentDelegatorInModel(model, candidateDelegator.Unsigned)
	if err != nil {
		panic(err)
	}
	return cmdState
}

func addCurrentDelegatorInModel(model *stakersStorageModel, candidateDelegatorTx txs.UnsignedTx) error {
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
			break
		}
	}
	if !found {
		stakerIt.Release()
		return nil // no current validator to add delegator to
	}
	stakerIt.Release() // release before modifying stakers collection

	// 2. Add a delegator to it
	addPermissionlessDelTx := candidateDelegatorTx.(*txs.AddPermissionlessDelegatorTx)
	addPermissionlessDelTx.Subnet = validator.SubnetID
	addPermissionlessDelTx.Validator.NodeID = validator.NodeID

	signedTx, err := txs.NewSigned(addPermissionlessDelTx, txs.Codec, nil)
	if err != nil {
		return fmt.Errorf("failed signing tx, %w", err)
	}

	delegator, err := NewCurrentStaker(signedTx.ID(), signedTx.Unsigned.(txs.Staker), uint64(1000))
	if err != nil {
		return fmt.Errorf("failed generating staker, %w", err)
	}

	model.PutCurrentDelegator(delegator)
	return nil
}

func (*putCurrentDelegatorCommand) PreCondition(commands.State) bool {
	return true
}

func (*putCurrentDelegatorCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	return checkSystemAndModelContent(cmdState, res)
}

func (v *putCurrentDelegatorCommand) String() string {
	stakerTx := v.Unsigned.(txs.StakerTx)
	return fmt.Sprintf("putCurrentDelegator(subnetID: %v, nodeID: %v, txID: %v, priority: %v, unixStartTime: %v, duration: %v)",
		stakerTx.SubnetID(),
		stakerTx.NodeID(),
		v.TxID,
		stakerTx.CurrentPriority(),
		stakerTx.StartTime().Unix(),
		stakerTx.EndTime().Sub(stakerTx.StartTime()))
}

var genPutCurrentDelegatorCommand = addPermissionlessDelegatorTxGenerator(commandsCtx, nil, nil, 1000).Map(
	func(nonInitTx *txs.Tx) commands.Command {
		sTx, err := txs.NewSigned(nonInitTx.Unsigned, txs.Codec, nil)
		if err != nil {
			panic(fmt.Errorf("failed signing tx, %w", err))
		}

		cmd := (*putCurrentDelegatorCommand)(sTx)
		return cmd
	},
)

// DeleteCurrentDelegator section
type deleteCurrentDelegatorCommand struct{}

func (*deleteCurrentDelegatorCommand) Run(sut commands.SystemUnderTest) commands.Result {
	// delete first validator, if any
	sys := sut.(*sysUnderTest)
	topDiff := sys.getTopChainState()

	stakerIt, err := topDiff.GetCurrentStakerIterator()
	if err != nil {
		panic(err)
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
			break
		}
	}
	if !found {
		stakerIt.Release()
		return sys // no current validator to delete
	}
	stakerIt.Release() // release before modifying stakers collection

	topDiff.DeleteCurrentDelegator(delegator)
	return sys // returns sys to allow comparison with state in PostCondition
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
			break
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
	return checkSystemAndModelContent(cmdState, res)
}

func (*deleteCurrentDelegatorCommand) String() string {
	return "DeleteCurrentDelegator"
}

// a trick to force command regeneration at each sampling.
// gen.Const would not allow it
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
	return checkSystemAndModelContent(cmdState, res)
}

func (*addTopDiffCommand) String() string {
	return "AddTopDiffCommand"
}

// a trick to force command regeneration at each sampling.
// gen.Const would not allow it
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
	return checkSystemAndModelContent(cmdState, res)
}

func (*applyBottomDiffCommand) String() string {
	return "ApplyBottomDiffCommand"
}

// a trick to force command regeneration at each sampling.
// gen.Const would not allow it
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
	return checkSystemAndModelContent(cmdState, res)
}

func (*commitBottomStateCommand) String() string {
	return "CommitBottomStateCommand"
}

// a trick to force command regeneration at each sampling.
// gen.Const would not allow it
var genCommitBottomStateCommand = gen.IntRange(1, 2).Map(
	func(int) commands.Command {
		return &commitBottomStateCommand{}
	},
)

// rebuildStateCommand section
type rebuildStateCommand struct{}

func (*rebuildStateCommand) Run(sut commands.SystemUnderTest) commands.Result {
	sys := sut.(*sysUnderTest)

	// 1. Persist all outstanding changes
	for sys.flushBottomDiff() {
		err := sys.baseState.Commit()
		if err != nil {
			panic(err)
		}
	}

	if err := sys.baseState.Commit(); err != nil {
		panic(err)
	}

	// 2. Rebuild the state from the db
	baseState, err := buildChainState(sys.baseDB, nil)
	if err != nil {
		panic(err)
	}
	sys.baseState = baseState
	sys.diffsMap = map[ids.ID]Diff{}
	sys.sortedDiffIDs = []ids.ID{}

	return sys
}

func (*rebuildStateCommand) NextState(cmdState commands.State) commands.State {
	return cmdState // model has no diffs
}

func (*rebuildStateCommand) PreCondition(commands.State) bool {
	return true
}

func (*rebuildStateCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	return checkSystemAndModelContent(cmdState, res)
}

func (*rebuildStateCommand) String() string {
	return "RebuildStateCommand"
}

// a trick to force command regeneration at each sampling.
// gen.Const would not allow it
var genRebuildStateCommand = gen.IntRange(1, 2).Map(
	func(int) commands.Command {
		return &rebuildStateCommand{}
	},
)

func checkSystemAndModelContent(cmdState commands.State, res commands.Result) *gopter.PropResult {
	model := cmdState.(*stakersStorageModel)
	sys := res.(*sysUnderTest)

	// top view content must always match model content
	topDiff := sys.getTopChainState()

	modelIt, err := model.GetCurrentStakerIterator()
	if err != nil {
		return &gopter.PropResult{Status: gopter.PropFalse}
	}
	sysIt, err := topDiff.GetCurrentStakerIterator()
	if err != nil {
		return &gopter.PropResult{Status: gopter.PropFalse}
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
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	for idx, modelStaker := range modelStakers {
		sysStaker := sysStakers[idx]
		if modelStaker == nil || sysStaker == nil || !reflect.DeepEqual(modelStaker, sysStaker) {
			return &gopter.PropResult{Status: gopter.PropFalse}
		}
	}

	return &gopter.PropResult{Status: gopter.PropTrue}
}
