// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"fmt"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/hashing"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
)

// UnsignedAdvanceTimeTx is a transaction to increase the chain's timestamp.
// When the chain's timestamp is updated (a AdvanceTimeTx is accepted and
// followed by a commit block) the staker set is also updated accordingly.
// It must be that:
//   * proposed timestamp > [current chain time]
//   * proposed timestamp <= [time for next staker to be removed]
type UnsignedAdvanceTimeTx struct {
	vm *VM
	// ID of this tx
	id ids.ID
	// Byte representation of this unsigned tx
	unsignedBytes []byte
	// Byte representation of the signed transaction (ie with credentials)
	bytes []byte

	// Unix time this block proposes increasing the timestamp to
	Time uint64 `serialize:"true"`
}

// initialize [tx]. Sets [tx.vm], [tx.unsignedBytes], [tx.bytes], [tx.id]
func (tx *UnsignedAdvanceTimeTx) initialize(vm *VM, bytes []byte) error {
	if tx.vm != nil { // already been initialized
		return nil
	}
	tx.vm = vm
	tx.bytes = bytes
	tx.id = ids.NewID(hashing.ComputeHash256Array(bytes))
	var err error
	tx.unsignedBytes, err = Codec.Marshal(interface{}(tx))
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedAdvanceTimeTx: %w", err)
	}
	return nil
}

// ID returns the ID of this transaction
func (tx *UnsignedAdvanceTimeTx) ID() ids.ID { return tx.id }

// Timestamp returns the time this block is proposing the chain should be set to
func (tx *UnsignedAdvanceTimeTx) Timestamp() time.Time {
	return time.Unix(int64(tx.Time), 0)
}

// Verify that this transaction is well formed
func (tx *UnsignedAdvanceTimeTx) Verify() TxError {
	switch {
	case tx == nil:
		return tempError{errNilTx}
	case tx.vm.clock.Time().Add(Delta).Before(tx.Timestamp()):
		return tempError{fmt.Errorf("proposed time, %s, is too far in the future relative to local time (%s)",
			tx.Timestamp(), tx.vm.clock.Time())}
	default:
		return nil
	}
}

// SemanticVerify this transaction is valid.
func (tx *UnsignedAdvanceTimeTx) SemanticVerify(
	db database.Database,
	stx *ProposalTx,
) (
	*versiondb.Database,
	*versiondb.Database,
	func() error,
	func() error,
	TxError,
) {
	if len(stx.Credentials) != 0 {
		return nil, nil, nil, nil, permError{errWrongNumberOfCredentials}
	}
	if err := tx.Verify(); err != nil {
		return nil, nil, nil, nil, err
	}

	if currentTimestamp, err := tx.vm.getTimestamp(db); err != nil {
		return nil, nil, nil, nil, tempError{err}
	} else if tx.Time <= uint64(currentTimestamp.Unix()) {
		return nil, nil, nil, nil, permError{fmt.Errorf("proposed timestamp (%s), not after current timestamp (%s)",
			tx.Timestamp(), currentTimestamp)}
	}

	// Only allow timestamp to move forward as far as the next validator's end time
	if nextValidatorEndTime := tx.vm.nextValidatorChangeTime(db, false); tx.Time > uint64(nextValidatorEndTime.Unix()) {
		return nil, nil, nil, nil, permError{fmt.Errorf("proposed timestamp (%s) later than next validator end time (%s)",
			tx.Timestamp(), nextValidatorEndTime)}
	}

	// Only allow timestamp to move forward as far as the next pending validator's start time
	if nextValidatorStartTime := tx.vm.nextValidatorChangeTime(db, true); tx.Time > uint64(nextValidatorStartTime.Unix()) {
		return nil, nil, nil, nil, permError{fmt.Errorf("proposed timestamp (%s) later than next validator start time (%s)",
			tx.Timestamp(), nextValidatorStartTime)}
	}

	// Calculate what the validator sets will be given new timestamp
	// Move validators from pending to current if their start time is <= new timestamp.
	// Remove validators from current if their end time <= proposed timestamp

	// Specify what the state of the chain will be if this proposal is committed
	onCommitDB := versiondb.New(db)
	if err := tx.vm.putTimestamp(onCommitDB, tx.Timestamp()); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	current, pending, _, _, err := tx.vm.calculateValidators(db, tx.Timestamp(), constants.DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, tempError{err}
	} else if err := tx.vm.putCurrentValidators(onCommitDB, current, constants.DefaultSubnetID); err != nil {
		return nil, nil, nil, nil, tempError{err}
	} else if err := tx.vm.putPendingValidators(onCommitDB, pending, constants.DefaultSubnetID); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	// For each Subnet, calculate what current and pending validator sets should be
	// given new timestamp

	// Key: Subnet ID
	// Value: IDs of validators that will have started validating this Subnet when
	// timestamp is advanced to tx.Timestamp()
	startedValidating := make(map[[32]byte]ids.ShortSet, 0)
	subnets, err := tx.vm.getSubnets(db)
	if err != nil {
		return nil, nil, nil, nil, tempError{err}
	}
	for _, subnet := range subnets {
		subnetID := subnet.ID()
		if current, pending, started, _, err := tx.vm.calculateValidators(db, tx.Timestamp(), subnetID); err != nil {
			return nil, nil, nil, nil, tempError{err}
		} else if err := tx.vm.putCurrentValidators(onCommitDB, current, subnetID); err != nil {
			return nil, nil, nil, nil, tempError{err}
		} else if err := tx.vm.putPendingValidators(onCommitDB, pending, subnetID); err != nil {
			return nil, nil, nil, nil, tempError{err}
		} else {
			startedValidating[subnet.ID().Key()] = started
		}
	}

	// If this block is committed, update the validator sets
	// onAbortDB or onCommitDB should commit (flush to vm.DB) before this is called
	onCommitFunc := func() error {
		// For each Subnet, update the node's validator manager to reflect current Subnet membership
		subnets, err := tx.vm.getSubnets(tx.vm.DB)
		if err != nil {
			return err
		}
		for _, subnet := range subnets {
			if err := tx.vm.updateValidators(subnet.ID()); err != nil {
				return err
			}
		}
		if err := tx.vm.updateValidators(constants.DefaultSubnetID); err != nil {
			return err
		}

		// If this node started validating a Subnet, create the blockchains that the Subnet validates
		chains, err := tx.vm.getChains(tx.vm.DB) // all blockchains
		if err != nil {
			return err
		}
		for subnetID, validatorIDs := range startedValidating {
			if !validatorIDs.Contains(tx.vm.Ctx.NodeID) {
				continue
			}
			for _, chain := range chains {
				unsignedChain := chain.UnsignedDecisionTx.(*UnsignedCreateChainTx)
				if bytes.Equal(subnetID[:], unsignedChain.SubnetID.Bytes()) {
					tx.vm.createChain(chain)
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
func (tx *UnsignedAdvanceTimeTx) InitiallyPrefersCommit() bool {
	return !tx.Timestamp().After(tx.vm.clock.Time())
}

// MarshalJSON marshals [tx] to JSON
func (tx *UnsignedAdvanceTimeTx) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("{\"type:\":\"advanceTimeTx\", \"id\":\"%s\", \"time\":%d}", tx.id, tx.Time)), nil
}

// newAdvanceTimeTx creates a new tx that, if it is accepted and followed by a
// Commit block, will set the chain's timestamp to [timestamp].
func (vm *VM) newAdvanceTimeTx(timestamp time.Time) (*ProposalTx, error) {
	tx := &ProposalTx{UnsignedProposalTx: &UnsignedAdvanceTimeTx{
		Time: uint64(timestamp.Unix()),
	}}

	txBytes, err := vm.codec.Marshal(tx)
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal ProposalTx: %w", err)
	}
	return tx, tx.initialize(vm, txBytes)
}
