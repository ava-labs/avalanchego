// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"fmt"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/constants"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
)

// advanceTimeTx is a transaction to increase the chain's timestamp.
// When the chain's timestamp is updated (a AdvanceTimeTx is accepted and
// followed by a commit block) the staker set is also updated accordingly.
// It must be that:
//   * proposed timestamp > [current chain time]
//   * proposed timestamp <= [time for next staker to be removed]
type advanceTimeTx struct {
	// Unix time this block proposes increasing the timestamp to
	Time uint64 `serialize:"true"`

	vm *VM
}

func (tx *advanceTimeTx) initialize(vm *VM) error {
	tx.vm = vm
	return nil
}

// Timestamp returns the time this block is proposing the chain should be set to
func (tx *advanceTimeTx) Timestamp() time.Time { return time.Unix(int64(tx.Time), 0) }

// SyntacticVerify that this transaction is well formed
func (tx *advanceTimeTx) SyntacticVerify() TxError {
	switch {
	case tx == nil:
		return tempError{errNilTx}
	case tx.vm.clock.Time().Add(Delta).Before(tx.Timestamp()):
		return tempError{errTimeTooAdvanced}
	default:
		return nil
	}
}

// SemanticVerify this transaction is valid.
func (tx *advanceTimeTx) SemanticVerify(db database.Database) (*versiondb.Database, *versiondb.Database, func(), func(), TxError) {
	if err := tx.SyntacticVerify(); err != nil {
		return nil, nil, nil, nil, err
	}

	currentTimestamp, err := tx.vm.getTimestamp(db)
	if err != nil {
		return nil, nil, nil, nil, permError{err}
	}
	if tx.Time <= uint64(currentTimestamp.Unix()) {
		return nil, nil, nil, nil, permError{fmt.Errorf("proposed timestamp %s not after current timestamp %s",
			tx.Timestamp(),
			currentTimestamp)}
	}

	// Only allow timestamp to move forward as far as the next validator's end time
	nextValidatorEndTime := tx.vm.nextValidatorChangeTime(db, false)
	if tx.Time > uint64(nextValidatorEndTime.Unix()) {
		return nil, nil, nil, nil, permError{fmt.Errorf("proposed timestamp %v later than next validator end time %s",
			tx.Time,
			nextValidatorEndTime)}
	}

	// Only allow timestamp to move forward as far as the next pending validator's start time
	nextValidatorStartTime := tx.vm.nextValidatorChangeTime(db, true)
	if tx.Time > uint64(nextValidatorStartTime.Unix()) {
		return nil, nil, nil, nil, permError{fmt.Errorf("proposed timestamp %v later than next validator start time %s",
			tx.Time,
			nextValidatorStartTime)}
	}

	// Calculate what the validator sets will be given new timestamp
	// Move validators from pending to current if their start time is <= new timestamp.
	// Remove validators from current if their end time <= proposed timestamp

	// Specify what the state of the chain will be if this proposal is committed
	onCommitDB := versiondb.New(db)
	if err := tx.vm.putTimestamp(onCommitDB, tx.Timestamp()); err != nil {
		return nil, nil, nil, nil, permError{err}
	}

	current, pending, _, _, err := tx.vm.calculateValidators(db, tx.Timestamp(), constants.DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, permError{err}
	}

	if err := tx.vm.putCurrentValidators(onCommitDB, current, constants.DefaultSubnetID); err != nil {
		return nil, nil, nil, nil, permError{err}
	}
	if err := tx.vm.putPendingValidators(onCommitDB, pending, constants.DefaultSubnetID); err != nil {
		return nil, nil, nil, nil, permError{err}
	}

	// For each Subnet, calculate what current and pending validator sets should be
	// given new timestamp

	// Key: Subnet ID
	// Value: IDs of validators that will have started validating this Subnet when
	// timestamp is advanced to tx.Timestamp()
	startedValidating := make(map[[32]byte]ids.ShortSet, 0)
	subnets, err := tx.vm.getSubnets(db)
	if err != nil {
		return nil, nil, nil, nil, permError{err}
	}
	for _, subnet := range subnets {
		current, pending, started, _, err := tx.vm.calculateValidators(db, tx.Timestamp(), subnet.id)
		if err != nil {
			return nil, nil, nil, nil, permError{err}
		}
		if err := tx.vm.putCurrentValidators(onCommitDB, current, subnet.id); err != nil {
			return nil, nil, nil, nil, permError{err}
		}
		if err := tx.vm.putPendingValidators(onCommitDB, pending, subnet.id); err != nil {
			return nil, nil, nil, nil, permError{err}
		}
		startedValidating[subnet.ID().Key()] = started
	}

	// If this block is committed, update the validator sets
	// onAbortDB or onCommitDB should commit (flush to vm.DB) before this is called
	onCommitFunc := func() {
		// For each Subnet, update the node's validator manager to reflect current Subnet membership
		subnets, err := tx.vm.getSubnets(tx.vm.DB)
		if err != nil {
			tx.vm.Ctx.Log.Error("failed to get subnets: %s", err)
			return
		}
		for _, subnet := range subnets {
			if err := tx.vm.updateValidators(subnet.id); err != nil {
				tx.vm.Ctx.Log.Debug("failed to update Subnet %s: %s", subnet.id, err)
			}
		}
		if err := tx.vm.updateValidators(constants.DefaultSubnetID); err != nil {
			tx.vm.Ctx.Log.Fatal("failed to update Default Subnet: %s", err)
		}

		// If this node started validating a Subnet, create the blockchains that the Subnet validates
		chains, err := tx.vm.getChains(tx.vm.DB) // all blockchains
		if err != nil {
			tx.vm.Ctx.Log.Error("couldn't get blockchains: %s", err)
			return
		}
		for subnetID, validatorIDs := range startedValidating {
			if !validatorIDs.Contains(tx.vm.Ctx.NodeID) {
				continue
			}
			for _, chain := range chains {
				if bytes.Equal(subnetID[:], chain.SubnetID.Bytes()) {
					tx.vm.createChain(chain)
				}
			}
		}
	}

	// Specify what the state of the chain will be if this proposal is aborted
	onAbortDB := versiondb.New(db) // state doesn't change

	return onCommitDB, onAbortDB, onCommitFunc, nil, nil
}

// InitiallyPrefersCommit returns true if the proposed time isn't after the
// current wall clock time.
func (tx *advanceTimeTx) InitiallyPrefersCommit() bool {
	return !tx.Timestamp().After(tx.vm.clock.Time())
}

// newAdvanceTimeTx creates a new tx that, if it is accepted and followed by a
// Commit block, will set the chain's timestamp to [timestamp].
func (vm *VM) newAdvanceTimeTx(timestamp time.Time) (*advanceTimeTx, error) {
	tx := &advanceTimeTx{
		Time: uint64(timestamp.Unix()),
	}
	return tx, tx.initialize(vm)
}
