// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/commands"
	"github.com/leanovate/gopter/gen"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ Versions         = (*sysUnderTest)(nil)
	_ commands.Command = (*putCurrentValidatorCommand)(nil)
	_ commands.Command = (*shiftCurrentValidatorCommand)(nil)
	_ commands.Command = (*updateStakingPeriodCurrentValidatorCommand)(nil)
	_ commands.Command = (*increaseWeightCurrentValidatorCommand)(nil)
	_ commands.Command = (*deleteCurrentValidatorCommand)(nil)
	_ commands.Command = (*putCurrentDelegatorCommand)(nil)
	_ commands.Command = (*shiftCurrentDelegatorCommand)(nil)
	_ commands.Command = (*updateStakingPeriodCurrentDelegatorCommand)(nil)
	_ commands.Command = (*increaseWeightCurrentDelegatorCommand)(nil)
	_ commands.Command = (*deleteCurrentDelegatorCommand)(nil)
	_ commands.Command = (*addTopDiffCommand)(nil)
	_ commands.Command = (*applyAndCommitBottomDiffCommand)(nil)
	_ commands.Command = (*rebuildStateCommand)(nil)

	commandsCtx = snowtest.Context(&testing.T{}, snowtest.PChainID)
	extraWeight = uint64(100)
)

// TestStateAndDiffComparisonToStorageModel verifies that a production-like
// system made of a stack of Diffs built on top of a State conforms to
// our stakersStorageModel. It achieves this by:
//  1. randomly generating a sequence of stakers writes as well as
//     some persistence operations (commit/diff apply),
//  2. applying the sequence to both our stakersStorageModel and the production-like system.
//  3. checking that both stakersStorageModel and the production-like system have
//     the same state after each operation.
//
// The following invariants are required for stakers state to properly work:
//  1. No stakers add/update/delete ops are performed directly on baseState, but on at least a diff
//  2. Any number of stakers ops can be carried out on a single diff
//  3. Diffs work in FIFO fashion: they are added on top of current state and only
//     bottom diff is applied to base state.
//  4. The bottom diff applied to base state is immediately committed.
func TestStateAndDiffComparisonToStorageModel(t *testing.T) {
	properties := gopter.NewProperties(nil)

	// // to reproduce a given scenario do something like this:
	// parameters := gopter.DefaultTestParametersWithSeed(1688718809015601261)
	// properties := gopter.NewProperties(parameters)

	properties.Property("state comparison to storage model", commands.Prop(stakersCommands))
	properties.TestingRun(t)
}

// stakersCommands creates/destroy the system under test and generates
// commands and initial states (stakersStorageModel)
var stakersCommands = &commands.ProtoCommands{
	NewSystemUnderTestFunc: func(initialState commands.State) commands.SystemUnderTest {
		model := initialState.(*stakersStorageModel)

		baseDB := versiondb.New(memdb.New())
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
			genShiftCurrentValidatorCommand,
			genUpdateStakingPeriodCurrentValidatorCommand,
			genIncreaseWeightCurrentValidatorCommand,
			genDeleteCurrentValidatorCommand,

			genPutCurrentDelegatorCommand,
			genShiftCurrentDelegatorCommand,
			genUpdateStakingPeriodCurrentDelegatorCommand,
			genIncreaseWeightCurrentDelegatorCommand,
			genDeleteCurrentDelegatorCommand,

			genAddTopDiffCommand,
			genApplyAndCommitBottomDiffCommand,
			genRebuildStateCommand,
		)
	},
}

// PutCurrentValidator section
type putCurrentValidatorCommand struct {
	sTx *txs.Tx
	err error
}

func (cmd *putCurrentValidatorCommand) Run(sut commands.SystemUnderTest) commands.Result {
	sTx := cmd.sTx
	sys := sut.(*sysUnderTest)

	if err := sys.checkThereIsADiff(); err != nil {
		return sys // state checks later on should spot missing validator
	}

	stakerTx := sTx.Unsigned.(txs.StakerTx)
	startTime := sTx.Unsigned.(txs.ScheduledStaker).StartTime()
	currentVal, err := NewCurrentStaker(sTx.ID(), stakerTx, startTime, uint64(1000))
	if err != nil {
		return sys // state checks later on should spot missing validator
	}

	topChainState := sys.getTopChainState()
	topChainState.PutCurrentValidator(currentVal)
	topChainState.AddTx(sTx, status.Committed)
	return sys
}

func (cmd *putCurrentValidatorCommand) NextState(cmdState commands.State) commands.State {
	sTx := cmd.sTx
	stakerTx := sTx.Unsigned.(txs.StakerTx)
	startTime := sTx.Unsigned.(txs.ScheduledStaker).StartTime()

	currentVal, err := NewCurrentStaker(sTx.ID(), stakerTx, startTime, uint64(1000))
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

func (cmd *putCurrentValidatorCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	if cmd.err != nil {
		cmd.err = nil // reset for next runs
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	if !checkSystemAndModelContent(cmdState, res) {
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	if !checkValidatorSetContent(res) {
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	return &gopter.PropResult{Status: gopter.PropTrue}
}

func (cmd *putCurrentValidatorCommand) String() string {
	stakerTx := cmd.sTx.Unsigned.(txs.StakerTx)
	return fmt.Sprintf("\nputCurrentValidator(subnetID: %v, nodeID: %v, txID: %v, priority: %v, unixStartTime: %v, unixEndTime: %v)",
		stakerTx.SubnetID(),
		stakerTx.NodeID(),
		cmd.sTx.TxID,
		stakerTx.CurrentPriority(),
		stakerTx.(txs.ScheduledStaker).StartTime().Unix(),
		stakerTx.EndTime().Unix(),
	)
}

var genPutCurrentValidatorCommand = addPermissionlessValidatorTxGenerator(commandsCtx, nil, nil, 1000).Map(
	func(nonInitTx *txs.Tx) commands.Command {
		sTx, err := txs.NewSigned(nonInitTx.Unsigned, txs.Codec, nil)
		if err != nil {
			panic(fmt.Errorf("failed signing tx, %w", err))
		}

		cmd := &putCurrentValidatorCommand{
			sTx: sTx,
			err: nil,
		}
		return cmd
	},
)

// ShiftCurrentValidator section
type shiftCurrentValidatorCommand struct {
	err error
}

func (cmd *shiftCurrentValidatorCommand) Run(sut commands.SystemUnderTest) commands.Result {
	sys := sut.(*sysUnderTest)

	if err := sys.checkThereIsADiff(); err != nil {
		return sys // state checks later on should spot missing validator
	}

	err := shiftCurrentValidatorInSystem(sys)
	if err != nil {
		cmd.err = err
	}
	return sys
}

func shiftCurrentValidatorInSystem(sys *sysUnderTest) error {
	// 1. check if there is a staker, already inserted. If not return
	// 2. query the staker
	// 3. shift staker times and update the staker

	chain := sys.getTopChainState()

	// 1. check if there is a staker, already inserted. If not return
	stakerIt, err := chain.GetCurrentStakerIterator()
	if err != nil {
		return err
	}

	var (
		found  bool
		staker *Staker
	)
	for !found && stakerIt.Next() {
		staker = stakerIt.Value()
		if staker.Priority.IsCurrentValidator() {
			found = true
			break
		}
	}
	if !found {
		stakerIt.Release()
		return nil // no current validator to update
	}
	stakerIt.Release()

	chain = sys.getTopChainState()

	// 3. query the staker
	staker, err = chain.GetCurrentValidator(staker.SubnetID, staker.NodeID)
	if err != nil {
		return err
	}

	// 4. shift staker times and update the staker
	updatedStaker := *staker
	ShiftStakerAheadInPlace(&updatedStaker, updatedStaker.EndTime)
	return chain.UpdateCurrentValidator(&updatedStaker)
}

func (cmd *shiftCurrentValidatorCommand) NextState(cmdState commands.State) commands.State {
	model := cmdState.(*stakersStorageModel)

	err := shiftCurrentValidatorInModel(model)
	if err != nil {
		cmd.err = err
	}
	return cmdState
}

func shiftCurrentValidatorInModel(model *stakersStorageModel) error {
	stakerIt, err := model.GetCurrentStakerIterator()
	if err != nil {
		return err
	}

	var (
		found  bool
		staker *Staker
	)
	for !found && stakerIt.Next() {
		staker = stakerIt.Value()
		if staker.Priority.IsCurrentValidator() {
			found = true
			break
		}
	}
	if !found {
		stakerIt.Release()
		return nil // no current validator to update
	}
	stakerIt.Release()

	updatedStaker := *staker
	ShiftStakerAheadInPlace(&updatedStaker, updatedStaker.EndTime)
	return model.UpdateCurrentValidator(&updatedStaker)
}

func (*shiftCurrentValidatorCommand) PreCondition(commands.State) bool {
	return true
}

func (cmd *shiftCurrentValidatorCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	if cmd.err != nil {
		cmd.err = nil // reset for next runs
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	if !checkSystemAndModelContent(cmdState, res) {
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	return &gopter.PropResult{Status: gopter.PropTrue}
}

func (*shiftCurrentValidatorCommand) String() string {
	return "\nshiftCurrentValidatorCommand"
}

var genShiftCurrentValidatorCommand = gen.IntRange(1, 2).Map(
	func(int) commands.Command {
		return &shiftCurrentValidatorCommand{}
	},
)

// updateStakingPeriodCurrentValidator section
type updateStakingPeriodCurrentValidatorCommand struct {
	err error
}

func (cmd *updateStakingPeriodCurrentValidatorCommand) Run(sut commands.SystemUnderTest) commands.Result {
	sys := sut.(*sysUnderTest)

	if err := sys.checkThereIsADiff(); err != nil {
		return sys // state checks later on should spot missing validator
	}

	err := updateStakingPeriodCurrentValidatorInSystem(sys)
	if err != nil {
		cmd.err = err
	}
	return sys
}

func updateStakingPeriodCurrentValidatorInSystem(sys *sysUnderTest) error {
	// 1. check if there is a staker, already inserted. If not return
	// 2. query the staker
	// 3. modify staker period and update the staker

	chain := sys.getTopChainState()

	// 1. check if there is a staker, already inserted. If not return
	stakerIt, err := chain.GetCurrentStakerIterator()
	if err != nil {
		return err
	}

	var (
		found  bool
		staker *Staker
	)
	for !found && stakerIt.Next() {
		staker = stakerIt.Value()
		if staker.Priority.IsCurrentValidator() {
			found = true
			break
		}
	}
	if !found {
		stakerIt.Release()
		return nil // no current validator to update
	}
	stakerIt.Release()

	chain = sys.getTopChainState()

	// 2. query the staker
	staker, err = chain.GetCurrentValidator(staker.SubnetID, staker.NodeID)
	if err != nil {
		return err
	}

	// 3. modify staker period and update the staker
	updatedStaker := *staker
	stakingPeriod := staker.EndTime.Sub(staker.StartTime)
	if stakingPeriod%2 == 0 {
		stakingPeriod -= 30 * time.Minute
	} else {
		stakingPeriod = 3*stakingPeriod + 1
	}
	UpdateStakingPeriodInPlace(&updatedStaker, stakingPeriod)
	return chain.UpdateCurrentValidator(&updatedStaker)
}

func (cmd *updateStakingPeriodCurrentValidatorCommand) NextState(cmdState commands.State) commands.State {
	model := cmdState.(*stakersStorageModel)

	err := updateStakingPeriodCurrentValidatorInModel(model)
	if err != nil {
		cmd.err = err
	}
	return cmdState
}

func updateStakingPeriodCurrentValidatorInModel(model *stakersStorageModel) error {
	stakerIt, err := model.GetCurrentStakerIterator()
	if err != nil {
		return err
	}

	var (
		found  bool
		staker *Staker
	)
	for !found && stakerIt.Next() {
		staker = stakerIt.Value()
		if staker.Priority.IsCurrentValidator() {
			found = true
			break
		}
	}
	if !found {
		stakerIt.Release()
		return nil // no current validator to update
	}
	stakerIt.Release()

	updatedStaker := *staker
	stakingPeriod := staker.EndTime.Sub(staker.StartTime)
	if stakingPeriod%2 == 0 {
		stakingPeriod -= 30 * time.Minute
	} else {
		stakingPeriod = 3*stakingPeriod + 1
	}
	UpdateStakingPeriodInPlace(&updatedStaker, stakingPeriod)
	return model.UpdateCurrentValidator(&updatedStaker)
}

func (*updateStakingPeriodCurrentValidatorCommand) PreCondition(commands.State) bool {
	return true
}

func (cmd *updateStakingPeriodCurrentValidatorCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	if cmd.err != nil {
		cmd.err = nil // reset for next runs
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	if !checkSystemAndModelContent(cmdState, res) {
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	return &gopter.PropResult{Status: gopter.PropTrue}
}

func (*updateStakingPeriodCurrentValidatorCommand) String() string {
	return "\nupdateStakingPeriodCurrentValidatorCommand"
}

var genUpdateStakingPeriodCurrentValidatorCommand = gen.IntRange(1, 2).Map(
	func(int) commands.Command {
		return &updateStakingPeriodCurrentValidatorCommand{}
	},
)

// increaseWeightCurrentValidator section
type increaseWeightCurrentValidatorCommand struct {
	err error
}

func (cmd *increaseWeightCurrentValidatorCommand) Run(sut commands.SystemUnderTest) commands.Result {
	sys := sut.(*sysUnderTest)

	if err := sys.checkThereIsADiff(); err != nil {
		return sys // state checks later on should spot missing validator
	}

	err := increaseWeightCurrentValidatorInSystem(sys)
	if err != nil {
		cmd.err = err
	}
	return sys
}

func increaseWeightCurrentValidatorInSystem(sys *sysUnderTest) error {
	// 1. check if there is a staker, already inserted. If not return
	// 2. query the staker
	// 3. increase staker weight and update the staker

	chain := sys.getTopChainState()

	// 1. check if there is a staker, already inserted. If not return
	stakerIt, err := chain.GetCurrentStakerIterator()
	if err != nil {
		return err
	}

	var (
		found  bool
		staker *Staker
	)
	for !found && stakerIt.Next() {
		staker = stakerIt.Value()
		if staker.Priority.IsCurrentValidator() {
			found = true
			break
		}
	}
	if !found {
		stakerIt.Release()
		return nil // no current validator to update
	}
	stakerIt.Release()

	chain = sys.getTopChainState()

	// 2. query the staker
	staker, err = chain.GetCurrentValidator(staker.SubnetID, staker.NodeID)
	if err != nil {
		return err
	}

	// 3. increase staker weight and update the staker
	updatedStaker := *staker
	IncreaseStakerWeightInPlace(&updatedStaker, updatedStaker.Weight+extraWeight)
	return chain.UpdateCurrentValidator(&updatedStaker)
}

func (cmd *increaseWeightCurrentValidatorCommand) NextState(cmdState commands.State) commands.State {
	model := cmdState.(*stakersStorageModel)

	err := increaseWeightCurrentValidatorInModel(model)
	if err != nil {
		cmd.err = err
	}
	return cmdState
}

func increaseWeightCurrentValidatorInModel(model *stakersStorageModel) error {
	stakerIt, err := model.GetCurrentStakerIterator()
	if err != nil {
		return err
	}

	var (
		found  bool
		staker *Staker
	)
	for !found && stakerIt.Next() {
		staker = stakerIt.Value()
		if staker.Priority.IsCurrentValidator() {
			found = true
			break
		}
	}
	if !found {
		stakerIt.Release()
		return nil // no current validator to update
	}
	stakerIt.Release()

	updatedStaker := *staker
	IncreaseStakerWeightInPlace(&updatedStaker, updatedStaker.Weight+extraWeight)
	return model.UpdateCurrentValidator(&updatedStaker)
}

func (*increaseWeightCurrentValidatorCommand) PreCondition(commands.State) bool {
	return true
}

func (cmd *increaseWeightCurrentValidatorCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	if cmd.err != nil {
		cmd.err = nil // reset for next runs
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	if !checkSystemAndModelContent(cmdState, res) {
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	return &gopter.PropResult{Status: gopter.PropTrue}
}

func (*increaseWeightCurrentValidatorCommand) String() string {
	return "\nincreaseWeightCurrentValidatorCommand"
}

var genIncreaseWeightCurrentValidatorCommand = gen.IntRange(1, 2).Map(
	func(int) commands.Command {
		return &increaseWeightCurrentValidatorCommand{}
	},
)

// DeleteCurrentValidator section
type deleteCurrentValidatorCommand struct {
	err error
}

func (cmd *deleteCurrentValidatorCommand) Run(sut commands.SystemUnderTest) commands.Result {
	// delete first validator without delegators, if any
	sys := sut.(*sysUnderTest)

	if err := sys.checkThereIsADiff(); err != nil {
		return sys // state checks later on should spot missing validator
	}

	topDiff := sys.getTopChainState()

	stakerIt, err := topDiff.GetCurrentStakerIterator()
	if err != nil {
		cmd.err = err
		return sys
	}

	var (
		found     = false
		validator *Staker
	)
	for stakerIt.Next() {
		validator = stakerIt.Value()
		if !validator.Priority.IsCurrentValidator() {
			continue // checks next validator
		}

		// check validator has no delegators
		delIt, err := topDiff.GetCurrentDelegatorIterator(validator.SubnetID, validator.NodeID)
		if err != nil {
			cmd.err = err
			stakerIt.Release()
			return sys
		}

		hadDelegator := delIt.Next()
		delIt.Release()
		if !hadDelegator {
			found = true
			break // found
		} else {
			continue // checks next validator
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

func (cmd *deleteCurrentValidatorCommand) NextState(cmdState commands.State) commands.State {
	// delete first validator without delegators, if any
	model := cmdState.(*stakersStorageModel)
	stakerIt, err := model.GetCurrentStakerIterator()
	if err != nil {
		cmd.err = err
		return cmdState
	}

	var (
		found     = false
		validator *Staker
	)
	for stakerIt.Next() {
		validator = stakerIt.Value()
		if !validator.Priority.IsCurrentValidator() {
			continue // checks next validator
		}

		// check validator has no delegators
		delIt, err := model.GetCurrentDelegatorIterator(validator.SubnetID, validator.NodeID)
		if err != nil {
			cmd.err = err
			stakerIt.Release()
			return cmdState
		}

		hadDelegator := delIt.Next()
		delIt.Release()
		if !hadDelegator {
			found = true
			break // found
		} else {
			continue // checks next validator
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

func (cmd *deleteCurrentValidatorCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	if cmd.err != nil {
		cmd.err = nil // reset for next runs
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	if !checkSystemAndModelContent(cmdState, res) {
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	if !checkValidatorSetContent(res) {
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	return &gopter.PropResult{Status: gopter.PropTrue}
}

func (*deleteCurrentValidatorCommand) String() string {
	return "\ndeleteCurrentValidator"
}

// a trick to force command regeneration at each sampling.
// gen.Const would not allow it
var genDeleteCurrentValidatorCommand = gen.IntRange(1, 2).Map(
	func(int) commands.Command {
		return &deleteCurrentValidatorCommand{}
	},
)

// PutCurrentDelegator section
type putCurrentDelegatorCommand struct {
	sTx *txs.Tx
	err error
}

func (cmd *putCurrentDelegatorCommand) Run(sut commands.SystemUnderTest) commands.Result {
	candidateDelegator := cmd.sTx
	sys := sut.(*sysUnderTest)

	if err := sys.checkThereIsADiff(); err != nil {
		return sys // state checks later on should spot missing validator
	}

	err := addCurrentDelegatorInSystem(sys, candidateDelegator.Unsigned)
	if err != nil {
		cmd.err = err
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
		if validator.Priority.IsCurrentValidator() {
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

	startTime := signedTx.Unsigned.(txs.ScheduledStaker).StartTime()
	delegator, err := NewCurrentStaker(signedTx.ID(), signedTx.Unsigned.(txs.Staker), startTime, uint64(1000))
	if err != nil {
		return fmt.Errorf("failed generating staker, %w", err)
	}

	chain.PutCurrentDelegator(delegator)
	chain.AddTx(signedTx, status.Committed)
	return nil
}

func (cmd *putCurrentDelegatorCommand) NextState(cmdState commands.State) commands.State {
	candidateDelegator := cmd.sTx
	model := cmdState.(*stakersStorageModel)
	err := addCurrentDelegatorInModel(model, candidateDelegator.Unsigned)
	if err != nil {
		cmd.err = err
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
		if validator.Priority.IsCurrentValidator() {
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

	startTime := signedTx.Unsigned.(txs.ScheduledStaker).StartTime()
	delegator, err := NewCurrentStaker(signedTx.ID(), signedTx.Unsigned.(txs.Staker), startTime, uint64(1000))
	if err != nil {
		return fmt.Errorf("failed generating staker, %w", err)
	}

	model.PutCurrentDelegator(delegator)
	return nil
}

func (*putCurrentDelegatorCommand) PreCondition(commands.State) bool {
	return true
}

func (cmd *putCurrentDelegatorCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	if cmd.err != nil {
		cmd.err = nil // reset for next runs
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	if !checkSystemAndModelContent(cmdState, res) {
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	if !checkValidatorSetContent(res) {
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	return &gopter.PropResult{Status: gopter.PropTrue}
}

func (cmd *putCurrentDelegatorCommand) String() string {
	stakerTx := cmd.sTx.Unsigned.(txs.StakerTx)
	return fmt.Sprintf("\nputCurrentDelegator(subnetID: %v, nodeID: %v, txID: %v, priority: %v, unixStartTime: %v, unixEndTime: %v)",
		stakerTx.SubnetID(),
		stakerTx.NodeID(),
		cmd.sTx.TxID,
		stakerTx.CurrentPriority(),
		stakerTx.(txs.ScheduledStaker).StartTime().Unix(),
		stakerTx.EndTime().Unix())
}

var genPutCurrentDelegatorCommand = addPermissionlessDelegatorTxGenerator(commandsCtx, nil, nil, 1000).Map(
	func(nonInitTx *txs.Tx) commands.Command {
		sTx, err := txs.NewSigned(nonInitTx.Unsigned, txs.Codec, nil)
		if err != nil {
			panic(fmt.Errorf("failed signing tx, %w", err))
		}

		cmd := &putCurrentDelegatorCommand{
			sTx: sTx,
		}
		return cmd
	},
)

// UpdateCurrentDelegator section
type updateStakingPeriodCurrentDelegatorCommand struct {
	err error
}

func (cmd *updateStakingPeriodCurrentDelegatorCommand) Run(sut commands.SystemUnderTest) commands.Result {
	sys := sut.(*sysUnderTest)

	if err := sys.checkThereIsADiff(); err != nil {
		return sys // state checks later on should spot missing validator
	}

	err := updateStakingPeriodCurrentDelegatorInSystem(sys)
	if err != nil {
		cmd.err = err
		return sys
	}
	return sys
}

func updateStakingPeriodCurrentDelegatorInSystem(sys *sysUnderTest) error {
	// 1. check if there is a staker, already inserted. If not return
	// 2. update staking period and update the staker

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
		if delegator.Priority.IsCurrentDelegator() {
			found = true
			break
		}
	}
	if !found {
		stakerIt.Release()
		return nil // no current validator to update
	}
	stakerIt.Release()

	chain = sys.getTopChainState()

	// 3. update delegator staking period and update the staker
	updatedDelegator := *delegator
	stakingPeriod := delegator.EndTime.Sub(delegator.StartTime)
	if stakingPeriod%2 == 0 {
		stakingPeriod -= 30 * time.Minute
	} else {
		stakingPeriod = 3*stakingPeriod + 1
	}
	UpdateStakingPeriodInPlace(&updatedDelegator, stakingPeriod)
	return chain.UpdateCurrentDelegator(&updatedDelegator)
}

func (cmd *updateStakingPeriodCurrentDelegatorCommand) NextState(cmdState commands.State) commands.State {
	model := cmdState.(*stakersStorageModel)

	err := updateStakingPeriodCurrentDelegatorInModel(model)
	if err != nil {
		cmd.err = err
	}
	return cmdState
}

func updateStakingPeriodCurrentDelegatorInModel(model *stakersStorageModel) error {
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
		if delegator.Priority.IsCurrentDelegator() {
			found = true
			break
		}
	}
	if !found {
		stakerIt.Release()
		return nil // no current validator to update
	}
	stakerIt.Release()

	updatedDelegator := *delegator
	stakingPeriod := delegator.EndTime.Sub(delegator.StartTime)
	if stakingPeriod%2 == 0 {
		stakingPeriod -= 30 * time.Minute
	} else {
		stakingPeriod = 3*stakingPeriod + 1
	}
	UpdateStakingPeriodInPlace(&updatedDelegator, stakingPeriod)
	return model.UpdateCurrentDelegator(&updatedDelegator)
}

func (*updateStakingPeriodCurrentDelegatorCommand) PreCondition(commands.State) bool {
	return true
}

func (cmd *updateStakingPeriodCurrentDelegatorCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	if cmd.err != nil {
		cmd.err = nil // reset for next runs
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	if !checkSystemAndModelContent(cmdState, res) {
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	return &gopter.PropResult{Status: gopter.PropTrue}
}

func (*updateStakingPeriodCurrentDelegatorCommand) String() string {
	return "\nupdateStakingPeriodCurrentDelegatorCommand"
}

var genUpdateStakingPeriodCurrentDelegatorCommand = gen.IntRange(1, 2).Map(
	func(int) commands.Command {
		return &updateStakingPeriodCurrentDelegatorCommand{}
	},
)

// UpdateCurrentDelegator section
type shiftCurrentDelegatorCommand struct {
	err error
}

func (cmd *shiftCurrentDelegatorCommand) Run(sut commands.SystemUnderTest) commands.Result {
	sys := sut.(*sysUnderTest)

	if err := sys.checkThereIsADiff(); err != nil {
		return sys // state checks later on should spot missing validator
	}

	err := shiftCurrentDelegatorInSystem(sys)
	if err != nil {
		cmd.err = err
	}
	return sys
}

func shiftCurrentDelegatorInSystem(sys *sysUnderTest) error {
	// 1. check if there is a staker, already inserted. If not return
	// 2. Shift staker times and update the staker

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
		if delegator.Priority.IsCurrentDelegator() {
			found = true
			break
		}
	}
	if !found {
		stakerIt.Release()
		return nil // no current validator to update
	}
	stakerIt.Release()

	chain = sys.getTopChainState()

	// 2. Shift delegator times and update the staker
	updatedDelegator := *delegator
	ShiftStakerAheadInPlace(&updatedDelegator, updatedDelegator.EndTime)
	return chain.UpdateCurrentDelegator(&updatedDelegator)
}

func (cmd *shiftCurrentDelegatorCommand) NextState(cmdState commands.State) commands.State {
	model := cmdState.(*stakersStorageModel)

	err := shiftCurrentDelegatorInModel(model)
	if err != nil {
		cmd.err = err
	}
	return cmdState
}

func shiftCurrentDelegatorInModel(model *stakersStorageModel) error {
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
		if delegator.Priority.IsCurrentDelegator() {
			found = true
			break
		}
	}
	if !found {
		stakerIt.Release()
		return nil // no current validator to update
	}
	stakerIt.Release()

	updatedDelegator := *delegator
	ShiftStakerAheadInPlace(&updatedDelegator, updatedDelegator.EndTime)
	return model.UpdateCurrentDelegator(&updatedDelegator)
}

func (*shiftCurrentDelegatorCommand) PreCondition(commands.State) bool {
	return true
}

func (cmd *shiftCurrentDelegatorCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	if cmd.err != nil {
		cmd.err = nil // reset for next runs
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	if !checkSystemAndModelContent(cmdState, res) {
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	return &gopter.PropResult{Status: gopter.PropTrue}
}

func (*shiftCurrentDelegatorCommand) String() string {
	return "\nshiftCurrentDelegator"
}

var genShiftCurrentDelegatorCommand = gen.IntRange(1, 2).Map(
	func(int) commands.Command {
		return &shiftCurrentDelegatorCommand{}
	},
)

// IncreaseWeightCurrentDelegator section
type increaseWeightCurrentDelegatorCommand struct {
	err error
}

func (cmd *increaseWeightCurrentDelegatorCommand) Run(sut commands.SystemUnderTest) commands.Result {
	sys := sut.(*sysUnderTest)

	if err := sys.checkThereIsADiff(); err != nil {
		return sys // state checks later on should spot missing validator
	}

	err := increaseWeightCurrentDelegatorInSystem(sys)
	if err != nil {
		cmd.err = err
	}
	return sys
}

func increaseWeightCurrentDelegatorInSystem(sys *sysUnderTest) error {
	// 1. check if there is a staker, already inserted. If not return
	// 2. increase delegator weight and update the staker

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
		if delegator.Priority.IsCurrentDelegator() {
			found = true
			break
		}
	}
	if !found {
		stakerIt.Release()
		return nil // no current validator to update
	}
	stakerIt.Release()

	chain = sys.getTopChainState()

	// 2. increase delegator weight and update the staker
	updatedDelegator := *delegator
	IncreaseStakerWeightInPlace(&updatedDelegator, updatedDelegator.Weight+extraWeight)
	return chain.UpdateCurrentDelegator(&updatedDelegator)
}

func (cmd *increaseWeightCurrentDelegatorCommand) NextState(cmdState commands.State) commands.State {
	model := cmdState.(*stakersStorageModel)

	err := increaseWeightCurrentDelegatorInModel(model)
	if err != nil {
		cmd.err = err
	}
	return cmdState
}

func increaseWeightCurrentDelegatorInModel(model *stakersStorageModel) error {
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
		if delegator.Priority.IsCurrentDelegator() {
			found = true
			break
		}
	}
	if !found {
		stakerIt.Release()
		return nil // no current validator to update
	}
	stakerIt.Release()

	updatedDelegator := *delegator
	IncreaseStakerWeightInPlace(&updatedDelegator, updatedDelegator.Weight+extraWeight)
	return model.UpdateCurrentDelegator(&updatedDelegator)
}

func (*increaseWeightCurrentDelegatorCommand) PreCondition(commands.State) bool {
	return true
}

func (cmd *increaseWeightCurrentDelegatorCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	if cmd.err != nil {
		cmd.err = nil // reset for next runs
		return &gopter.PropResult{Status: gopter.PropFalse}
	}
	if !checkSystemAndModelContent(cmdState, res) {
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	return &gopter.PropResult{Status: gopter.PropTrue}
}

func (*increaseWeightCurrentDelegatorCommand) String() string {
	return "\nincreaseWeightCurrentDelegator"
}

var genIncreaseWeightCurrentDelegatorCommand = gen.IntRange(1, 2).Map(
	func(int) commands.Command {
		return &increaseWeightCurrentDelegatorCommand{}
	},
)

// DeleteCurrentDelegator section
type deleteCurrentDelegatorCommand struct {
	err error
}

func (cmd *deleteCurrentDelegatorCommand) Run(sut commands.SystemUnderTest) commands.Result {
	// delete first delegator, if any
	sys := sut.(*sysUnderTest)

	if err := sys.checkThereIsADiff(); err != nil {
		return sys // state checks later on should spot missing validator
	}

	_, err := deleteCurrentDelegator(sys)
	if err != nil {
		cmd.err = err
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
		if delegator.Priority.IsCurrentDelegator() {
			found = true
			break
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
		if delegator.Priority.IsCurrentDelegator() {
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

func (cmd *deleteCurrentDelegatorCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	if cmd.err != nil {
		cmd.err = nil // reset for next runs
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	if !checkSystemAndModelContent(cmdState, res) {
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	if !checkValidatorSetContent(res) {
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	return &gopter.PropResult{Status: gopter.PropTrue}
}

func (*deleteCurrentDelegatorCommand) String() string {
	return "\ndeleteCurrentDelegator"
}

// a trick to force command regeneration at each sampling.
// gen.Const would not allow it
var genDeleteCurrentDelegatorCommand = gen.IntRange(1, 2).Map(
	func(int) commands.Command {
		return &deleteCurrentDelegatorCommand{}
	},
)

// addTopDiffCommand section
type addTopDiffCommand struct {
	err error
}

func (cmd *addTopDiffCommand) Run(sut commands.SystemUnderTest) commands.Result {
	sys := sut.(*sysUnderTest)
	err := sys.addDiffOnTop()
	if err != nil {
		cmd.err = err
	}
	return sys
}

func (*addTopDiffCommand) NextState(cmdState commands.State) commands.State {
	return cmdState // model has no diffs
}

func (*addTopDiffCommand) PreCondition(commands.State) bool {
	return true
}

func (cmd *addTopDiffCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	if cmd.err != nil {
		cmd.err = nil // reset for next runs
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	if !checkSystemAndModelContent(cmdState, res) {
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	if !checkValidatorSetContent(res) {
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	return &gopter.PropResult{Status: gopter.PropTrue}
}

func (*addTopDiffCommand) String() string {
	return "\naddTopDiffCommand"
}

// a trick to force command regeneration at each sampling.
// gen.Const would not allow it
var genAddTopDiffCommand = gen.IntRange(1, 2).Map(
	func(int) commands.Command {
		return &addTopDiffCommand{}
	},
)

// applyAndCommitBottomDiffCommand section
type applyAndCommitBottomDiffCommand struct {
	err error
}

func (cmd *applyAndCommitBottomDiffCommand) Run(sut commands.SystemUnderTest) commands.Result {
	sys := sut.(*sysUnderTest)
	if _, err := sys.flushBottomDiff(); err != nil {
		cmd.err = err
		return sys
	}

	if err := sys.baseState.Commit(); err != nil {
		cmd.err = err
		return sys
	}

	return sys
}

func (*applyAndCommitBottomDiffCommand) NextState(cmdState commands.State) commands.State {
	return cmdState // model has no diffs
}

func (*applyAndCommitBottomDiffCommand) PreCondition(commands.State) bool {
	return true
}

func (cmd *applyAndCommitBottomDiffCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	if cmd.err != nil {
		cmd.err = nil // reset for next runs
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	if !checkSystemAndModelContent(cmdState, res) {
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	if !checkValidatorSetContent(res) {
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	return &gopter.PropResult{Status: gopter.PropTrue}
}

func (*applyAndCommitBottomDiffCommand) String() string {
	return "\napplyAndCommitBottomDiffCommand"
}

// a trick to force command regeneration at each sampling.
// gen.Const would not allow it
var genApplyAndCommitBottomDiffCommand = gen.IntRange(1, 2).Map(
	func(int) commands.Command {
		return &applyAndCommitBottomDiffCommand{}
	},
)

// rebuildStateCommand section
type rebuildStateCommand struct {
	err error
}

func (cmd *rebuildStateCommand) Run(sut commands.SystemUnderTest) commands.Result {
	sys := sut.(*sysUnderTest)

	// 1. Persist all outstanding changes
	for {
		diffFound, err := sys.flushBottomDiff()
		if err != nil {
			cmd.err = err
			return sys
		}
		if !diffFound {
			break
		}

		if err := sys.baseState.Commit(); err != nil {
			cmd.err = err
			return sys
		}
	}

	if err := sys.baseState.Commit(); err != nil {
		cmd.err = err
		return sys
	}

	// 2. Rebuild the state from the db
	baseState, err := buildChainState(sys.baseDB, nil)
	if err != nil {
		cmd.err = err
		return sys
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

func (cmd *rebuildStateCommand) PostCondition(cmdState commands.State, res commands.Result) *gopter.PropResult {
	if cmd.err != nil {
		cmd.err = nil // reset for next runs
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	if !checkSystemAndModelContent(cmdState, res) {
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	if !checkValidatorSetContent(res) {
		return &gopter.PropResult{Status: gopter.PropFalse}
	}

	return &gopter.PropResult{Status: gopter.PropTrue}
}

func (*rebuildStateCommand) String() string {
	return "\nrebuildStateCommand"
}

// a trick to force command regeneration at each sampling.
// gen.Const would not allow it
var genRebuildStateCommand = gen.IntRange(1, 2).Map(
	func(int) commands.Command {
		return &rebuildStateCommand{}
	},
)

func checkSystemAndModelContent(cmdState commands.State, res commands.Result) bool {
	model := cmdState.(*stakersStorageModel)
	sys := res.(*sysUnderTest)

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

// checkValidatorSetContent compares ValidatorsSet with P-chain base-state data and
// makes sure they are coherent.
func checkValidatorSetContent(res commands.Result) bool {
	sys := res.(*sysUnderTest)
	valSet := sys.baseState.(*state).cfg.Validators

	sysIt, err := sys.baseState.GetCurrentStakerIterator()
	if err != nil {
		return false
	}

	// valContent subnetID -> nodeID -> aggregate weight (validator's own weight + delegators' weight)
	valContent := make(map[ids.ID]map[ids.NodeID]uint64)
	for sysIt.Next() {
		val := sysIt.Value()
		if val.SubnetID != constants.PrimaryNetworkID {
			continue
		}
		nodes, found := valContent[val.SubnetID]
		if !found {
			nodes = make(map[ids.NodeID]uint64)
			valContent[val.SubnetID] = nodes
		}
		nodes[val.NodeID] += val.Weight
	}
	sysIt.Release()

	for subnetID, nodes := range valContent {
		for nodeID, weight := range nodes {
			val, found := valSet.GetValidator(subnetID, nodeID)
			if !found {
				return false
			}
			if weight != val.Weight {
				return false
			}
		}
	}
	return true
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

func (s *sysUnderTest) addDiffOnTop() error {
	newTopBlkID := ids.Empty.Prefix(atomic.AddUint64(&s.diffBlkIDSeed, 1))
	var topBlkID ids.ID
	if len(s.sortedDiffIDs) == 0 {
		topBlkID = s.baseState.GetLastAccepted()
	} else {
		topBlkID = s.sortedDiffIDs[len(s.sortedDiffIDs)-1]
	}
	newTopDiff, err := NewDiff(topBlkID, s)
	if err != nil {
		return err
	}
	s.sortedDiffIDs = append(s.sortedDiffIDs, newTopBlkID)
	s.diffsMap[newTopBlkID] = newTopDiff
	return nil
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
func (s *sysUnderTest) flushBottomDiff() (bool, error) {
	if len(s.sortedDiffIDs) == 0 {
		return false, nil
	}
	bottomDiffID := s.sortedDiffIDs[0]
	diffToApply := s.diffsMap[bottomDiffID]

	err := diffToApply.Apply(s.baseState)
	if err != nil {
		return true, err
	}
	s.baseState.SetLastAccepted(bottomDiffID)

	s.sortedDiffIDs = s.sortedDiffIDs[1:]
	delete(s.diffsMap, bottomDiffID)
	return true, nil
}

// checkThereIsADiff must be called before any stakers op. It makes
// sure that ops are carried out on at least a diff, as it happens
// in production code.
func (s *sysUnderTest) checkThereIsADiff() error {
	if len(s.sortedDiffIDs) != 0 {
		return nil // there is a diff
	}

	return s.addDiffOnTop()
}
