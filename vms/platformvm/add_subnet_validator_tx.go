// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	errDSValidatorSubset = errors.New("all subnets' staking period must be a subset of the primary network")

	_ UnsignedProposalTx = &UnsignedAddSubnetValidatorTx{}
	_ TimedTx            = &UnsignedAddSubnetValidatorTx{}
)

// UnsignedAddSubnetValidatorTx is an unsigned addSubnetValidatorTx
type UnsignedAddSubnetValidatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// The validator
	Validator SubnetValidator `serialize:"true" json:"validator"`
	// Auth that will be allowing this validator into the network
	SubnetAuth verify.Verifiable `serialize:"true" json:"subnetAuthorization"`
}

// StartTime of this validator
func (tx *UnsignedAddSubnetValidatorTx) StartTime() time.Time {
	return tx.Validator.StartTime()
}

// EndTime of this validator
func (tx *UnsignedAddSubnetValidatorTx) EndTime() time.Time {
	return tx.Validator.EndTime()
}

// Weight of this validator
func (tx *UnsignedAddSubnetValidatorTx) Weight() uint64 {
	return tx.Validator.Weight()
}

// Verify return nil iff [tx] is valid
func (tx *UnsignedAddSubnetValidatorTx) Verify(
	ctx *snow.Context,
	c codec.Manager,
	feeAmount uint64,
	feeAssetID ids.ID,
	minStakeDuration time.Duration,
	maxStakeDuration time.Duration,
) error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	}

	duration := tx.Validator.Duration()
	switch {
	case duration < minStakeDuration: // Ensure staking length is not too short
		return errStakeTooShort
	case duration > maxStakeDuration: // Ensure staking length is not too long
		return errStakeTooLong
	}

	if err := tx.BaseTx.Verify(ctx, c); err != nil {
		return err
	}
	if err := verify.All(&tx.Validator, tx.SubnetAuth); err != nil {
		return err
	}

	// cache that this is valid
	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *UnsignedAddSubnetValidatorTx) SemanticVerify(
	vm *VM,
	parentState mutableState,
	stx *Tx,
) (
	versionedState,
	versionedState,
	func() error,
	func() error,
	TxError,
) {
	if err := tx.Verify(
		vm.ctx,
		vm.codec,
		vm.TxFee,
		vm.ctx.AVAXAssetID,
		vm.MinStakeDuration,
		vm.MaxStakeDuration,
	); err != nil {
		return nil, nil, nil, nil, permError{err}
	}

	// Verify the tx is well-formed
	if len(stx.Creds) == 0 {
		return nil, nil, nil, nil, permError{errWrongNumberOfCredentials}
	}

	currentStakers := parentState.CurrentStakerChainState()
	pendingStakers := parentState.PendingStakerChainState()

	if vm.bootstrapped {
		currentTimestamp := parentState.GetTimestamp()
		// Ensure the proposed validator starts after the current timestamp
		if validatorStartTime := tx.StartTime(); !currentTimestamp.Before(validatorStartTime) {
			return nil, nil, nil, nil, permError{
				fmt.Errorf(
					"validator's start time (%s) is at or after current chain timestamp (%s)",
					currentTimestamp,
					validatorStartTime,
				),
			}
		} else if validatorStartTime.After(currentTimestamp.Add(maxFutureStartTime)) {
			return nil, nil, nil, nil, permError{
				fmt.Errorf(
					"validator start time (%s) more than two weeks after current chain timestamp (%s)",
					validatorStartTime,
					currentTimestamp,
				),
			}
		}

		currentValidator, err := currentStakers.GetValidator(tx.Validator.NodeID)
		if err != nil && err != database.ErrNotFound {
			return nil, nil, nil, nil, tempError{
				fmt.Errorf(
					"failed to find whether %s is a validator: %w",
					tx.Validator.NodeID.PrefixedString(constants.NodeIDPrefix),
					err,
				),
			}
		}

		// Ensure that the period this validator validates the specified subnet
		// is a subset of the time they validate the primary network.
		if err == nil {
			vdrTx := currentValidator.AddValidatorTx()
			if !tx.Validator.BoundedBy(vdrTx.StartTime(), vdrTx.EndTime()) {
				return nil, nil, nil, nil, permError{errDSValidatorSubset}
			}

			// Ensure that this transaction isn't a duplicate add validator tx.
			subnets := currentValidator.SubnetValidators()
			if _, validates := subnets[tx.Validator.Subnet]; validates {
				return nil, nil, nil, nil, permError{
					fmt.Errorf(
						"already validating subnet %s",
						tx.Validator.Subnet,
					),
				}
			}
		} else {
			vdrTx, err := pendingStakers.GetStakerByNodeID(tx.Validator.NodeID)
			if err != nil {
				if err == database.ErrNotFound {
					return nil, nil, nil, nil, permError{errDSValidatorSubset}
				}
				return nil, nil, nil, nil, tempError{
					fmt.Errorf(
						"failed to find whether %s is a validator: %w",
						tx.Validator.NodeID.PrefixedString(constants.NodeIDPrefix),
						err,
					),
				}
			}

			if !tx.Validator.BoundedBy(vdrTx.StartTime(), vdrTx.EndTime()) {
				return nil, nil, nil, nil, permError{errDSValidatorSubset}
			}
		}

		// Ensure that this transaction isn't a duplicate add validator tx.
		pendingValidator := pendingStakers.GetValidator(tx.Validator.NodeID)
		subnets := pendingValidator.SubnetValidators()
		if _, validates := subnets[tx.Validator.Subnet]; validates {
			return nil, nil, nil, nil, permError{
				fmt.Errorf(
					"already validating subnet %s",
					tx.Validator.Subnet,
				),
			}
		}

		baseTxCredsLen := len(stx.Creds) - 1
		baseTxCreds := stx.Creds[:baseTxCredsLen]
		subnetCred := stx.Creds[baseTxCredsLen]

		subnetIntf, _, err := parentState.GetTx(tx.Validator.Subnet)
		if err != nil {
			if err == database.ErrNotFound {
				return nil, nil, nil, nil, permError{errDSValidatorSubset}
			}
			return nil, nil, nil, nil, tempError{
				fmt.Errorf(
					"couldn't find subnet %s with %w",
					tx.Validator.Subnet,
					err,
				),
			}
		}

		subnet, ok := subnetIntf.UnsignedTx.(*UnsignedCreateSubnetTx)
		if !ok {
			return nil, nil, nil, nil, tempError{
				fmt.Errorf(
					"%s is not a subnet",
					tx.Validator.Subnet,
				),
			}
		}

		if err := vm.fx.VerifyPermission(tx, tx.SubnetAuth, subnetCred, subnet.Owner); err != nil {
			return nil, nil, nil, nil, permError{err}
		}

		// Verify the flowcheck
		if err := vm.semanticVerifySpend(parentState, tx, tx.Ins, tx.Outs, baseTxCreds, vm.TxFee, vm.ctx.AVAXAssetID); err != nil {
			return nil, nil, nil, nil, err
		}
	}

	// Set up the state if this tx is committed
	newlyPendingStakers := pendingStakers.AddStaker(stx)
	onCommitState := NewVersionedState(parentState, currentStakers, newlyPendingStakers)

	// Consume the UTXOS
	vm.consumeInputs(onCommitState, tx.Ins)
	// Produce the UTXOS
	txID := tx.ID()
	vm.produceOutputs(onCommitState, txID, tx.Outs)

	// Set up the state if this tx is aborted
	onAbortState := NewVersionedState(parentState, currentStakers, pendingStakers)
	// Consume the UTXOS
	vm.consumeInputs(onAbortState, tx.Ins)
	// Produce the UTXOS
	vm.produceOutputs(onAbortState, txID, tx.Outs)

	return onCommitState, onAbortState, nil, nil, nil
}

// InitiallyPrefersCommit returns true if the proposed validators start time is
// after the current wall clock time,
func (tx *UnsignedAddSubnetValidatorTx) InitiallyPrefersCommit(vm *VM) bool {
	return tx.StartTime().After(vm.clock.Time())
}

// Create a new transaction
func (vm *VM) newAddSubnetValidatorTx(
	weight, // Sampling weight of the new validator
	startTime, // Unix time they start delegating
	endTime uint64, // Unix time they top delegating
	nodeID ids.ShortID, // ID of the node validating
	subnetID ids.ID, // ID of the subnet the validator will validate
	keys []*crypto.PrivateKeySECP256K1R, // Keys to use for adding the validator
	changeAddr ids.ShortID, // Address to send change to, if there is any
) (*Tx, error) {
	ins, outs, _, signers, err := vm.stake(keys, 0, vm.TxFee, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	subnetAuth, subnetSigners, err := vm.authorize(vm.internalState, subnetID, keys)
	if err != nil {
		return nil, fmt.Errorf("couldn't authorize tx's subnet restrictions: %w", err)
	}
	signers = append(signers, subnetSigners)

	// Create the tx
	utx := &UnsignedAddSubnetValidatorTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.ctx.NetworkID,
			BlockchainID: vm.ctx.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
		Validator: SubnetValidator{
			Validator: Validator{
				NodeID: nodeID,
				Start:  startTime,
				End:    endTime,
				Wght:   weight,
			},
			Subnet: subnetID,
		},
		SubnetAuth: subnetAuth,
	}
	tx := &Tx{UnsignedTx: utx}
	if err := tx.Sign(vm.codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.Verify(
		vm.ctx,
		vm.codec,
		vm.TxFee,
		vm.ctx.AVAXAssetID,
		vm.MinStakeDuration,
		vm.MaxStakeDuration,
	)
}
