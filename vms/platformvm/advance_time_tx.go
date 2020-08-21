// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"fmt"
	"time"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/vms/components/avax"
)

var (
	_ UnsignedProposalTx = &UnsignedAdvanceTimeTx{}
)

// UnsignedAdvanceTimeTx is a transaction to increase the chain's timestamp.
// When the chain's timestamp is updated (a AdvanceTimeTx is accepted and
// followed by a commit block) the staker set is also updated accordingly.
// It must be that:
//   * proposed timestamp > [current chain time]
//   * proposed timestamp <= [time for next staker to be removed]
type UnsignedAdvanceTimeTx struct {
	avax.Metadata

	// Unix time this block proposes increasing the timestamp to
	Time uint64 `serialize:"true" json:"time"`
}

// Timestamp returns the time this block is proposing the chain should be set to
func (tx *UnsignedAdvanceTimeTx) Timestamp() time.Time {
	return time.Unix(int64(tx.Time), 0)
}

// SemanticVerify this transaction is valid.
func (tx *UnsignedAdvanceTimeTx) SemanticVerify(
	vm *VM,
	db database.Database,
	stx *Tx,
) (
	*versiondb.Database,
	*versiondb.Database,
	func() error,
	func() error,
	TxError,
) {
	switch {
	case tx == nil:
		return nil, nil, nil, nil, tempError{errNilTx}
	case vm.clock.Time().Add(Delta).Before(tx.Timestamp()):
		return nil, nil, nil, nil, tempError{fmt.Errorf("proposed time, %s, is too far in the future relative to local time (%s)",
			tx.Timestamp(), vm.clock.Time())}
	case len(stx.Creds) != 0:
		return nil, nil, nil, nil, permError{errWrongNumberOfCredentials}
	}

	if currentTimestamp, err := vm.getTimestamp(db); err != nil {
		return nil, nil, nil, nil, tempError{err}
	} else if tx.Time <= uint64(currentTimestamp.Unix()) {
		return nil, nil, nil, nil, permError{fmt.Errorf("proposed timestamp (%s), not after current timestamp (%s)",
			tx.Timestamp(), currentTimestamp)}
	}

	// Only allow timestamp to move forward as far as the next validator's end time
	if nextValidatorEndTime := vm.nextValidatorChangeTime(db, false); tx.Time > uint64(nextValidatorEndTime.Unix()) {
		return nil, nil, nil, nil, permError{fmt.Errorf("proposed timestamp (%s) later than next validator end time (%s)",
			tx.Timestamp(), nextValidatorEndTime)}
	}

	// Only allow timestamp to move forward as far as the next pending validator's start time
	if nextValidatorStartTime := vm.nextValidatorChangeTime(db, true); tx.Time > uint64(nextValidatorStartTime.Unix()) {
		return nil, nil, nil, nil, permError{fmt.Errorf("proposed timestamp (%s) later than next validator start time (%s)",
			tx.Timestamp(), nextValidatorStartTime)}
	}

	// Calculate what the validator sets will be given new timestamp
	// Move validators from pending to current if their start time is <= new timestamp.
	// Remove validators from current if their end time <= proposed timestamp

	// Specify what the state of the chain will be if this proposal is committed
	onCommitDB := versiondb.New(db)
	if err := vm.putTimestamp(onCommitDB, tx.Timestamp()); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	current, pending, _, _, err := vm.calculateValidators(db, tx.Timestamp(), constants.DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, tempError{err}
	} else if err := vm.putCurrentValidators(onCommitDB, current, constants.DefaultSubnetID); err != nil {
		return nil, nil, nil, nil, tempError{err}
	} else if err := vm.putPendingValidators(onCommitDB, pending, constants.DefaultSubnetID); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	// For each Subnet, calculate what current and pending validator sets should be
	// given new timestamp

	// Key: Subnet ID
	// Value: IDs of validators that will have started validating this Subnet when
	// timestamp is advanced to tx.Timestamp()
	startedValidating := make(map[[32]byte]ids.ShortSet, 0)
	subnets, err := vm.getSubnets(db)
	if err != nil {
		return nil, nil, nil, nil, tempError{err}
	}
	for _, subnet := range subnets {
		subnetID := subnet.ID()
		if current, pending, started, _, err := vm.calculateValidators(db, tx.Timestamp(), subnetID); err != nil {
			return nil, nil, nil, nil, tempError{err}
		} else if err := vm.putCurrentValidators(onCommitDB, current, subnetID); err != nil {
			return nil, nil, nil, nil, tempError{err}
		} else if err := vm.putPendingValidators(onCommitDB, pending, subnetID); err != nil {
			return nil, nil, nil, nil, tempError{err}
		} else {
			startedValidating[subnet.ID().Key()] = started
		}
	}

	// If this block is committed, update the validator sets
	// onAbortDB or onCommitDB should commit (flush to vm.DB) before this is called
	onCommitFunc := func() error {
		// For each Subnet, update the node's validator manager to reflect current Subnet membership
		subnets, err := vm.getSubnets(vm.DB)
		if err != nil {
			return err
		}
		for _, subnet := range subnets {
			if err := vm.updateValidators(subnet.ID()); err != nil {
				return err
			}
		}
		if err := vm.updateValidators(constants.DefaultSubnetID); err != nil {
			return err
		}

		// If this node started validating a Subnet, create the blockchains that the Subnet validates
		chains, err := vm.getChains(vm.DB) // all blockchains
		if err != nil {
			return err
		}
		for subnetID, validatorIDs := range startedValidating {
			if !validatorIDs.Contains(vm.Ctx.NodeID) {
				continue
			}
			for _, chain := range chains {
				unsignedChain := chain.UnsignedTx.(*UnsignedCreateChainTx)
				if bytes.Equal(subnetID[:], unsignedChain.SubnetID.Bytes()) {
					vm.createChain(chain)
				}
			}
		}
		return nil
	}

	// State doesn't change if this proposal is aborted
	onAbortDB := versiondb.New(db)
	return onCommitDB, onAbortDB, onCommitFunc, nil, nil
}

// InitiallyPrefersCommit returns true if the proposed time isn't after the
// current wall clock time.
func (tx *UnsignedAdvanceTimeTx) InitiallyPrefersCommit(vm *VM) bool {
	return !tx.Timestamp().After(vm.clock.Time())
}

// newAdvanceTimeTx creates a new tx that, if it is accepted and followed by a
// Commit block, will set the chain's timestamp to [timestamp].
func (vm *VM) newAdvanceTimeTx(timestamp time.Time) (*Tx, error) {
	tx := &Tx{UnsignedTx: &UnsignedAdvanceTimeTx{
		Time: uint64(timestamp.Unix()),
	}}
	return tx, tx.Sign(vm.codec, nil)
}
