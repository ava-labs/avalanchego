// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/entities"
	"github.com/ava-labs/avalanchego/vms/platformvm/platformcodec"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ VerifiableUnsignedProposalTx = VerifiableUnsignedAddValidatorTx{}
	_ TimedTx                      = VerifiableUnsignedAddValidatorTx{}
)

type VerifiableUnsignedAddValidatorTx struct {
	*transactions.UnsignedAddValidatorTx `serialize:"true"`
}

// StartTime of this validator
func (tx VerifiableUnsignedAddValidatorTx) StartTime() time.Time {
	return tx.Validator.StartTime()
}

// EndTime of this validator
func (tx VerifiableUnsignedAddValidatorTx) EndTime() time.Time {
	return tx.Validator.EndTime()
}

// Weight of this validator
func (tx VerifiableUnsignedAddValidatorTx) Weight() uint64 {
	return tx.Validator.Weight()
}

// SemanticVerify this transactions.is valid.
func (tx VerifiableUnsignedAddValidatorTx) SemanticVerify(
	vm *VM,
	parentState MutableState,
	stx *transactions.SignedTx,
) (
	VersionedState,
	VersionedState,
	func() error,
	func() error,
	TxError,
) {
	syntacticCtx := transactions.ProposalTxSyntacticVerificationContext{
		Ctx:               vm.ctx,
		C:                 platformcodec.Codec,
		MinValidatorStake: vm.MinValidatorStake,
		MaxValidatorStake: vm.MaxValidatorStake,
		MinStakeDuration:  vm.MinStakeDuration,
		MaxStakeDuration:  vm.MaxStakeDuration,
		MinDelegationFee:  vm.MinDelegationFee,
	}
	if err := tx.SyntacticVerify(syntacticCtx); err != nil {
		return nil, nil, nil, nil, permError{err}
	}

	currentStakers := parentState.CurrentStakerChainState()
	pendingStakers := parentState.PendingStakerChainState()

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.Stake))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.Stake)

	if vm.bootstrapped {
		currentTimestamp := parentState.GetTimestamp()
		// Ensure the proposed validator starts after the current time
		if startTime := tx.StartTime(); !currentTimestamp.Before(startTime) {
			return nil, nil, nil, nil, permError{
				fmt.Errorf(
					"validator's start time (%s) at or before current timestamp (%s)",
					startTime,
					currentTimestamp,
				),
			}
		} else if startTime.After(currentTimestamp.Add(maxFutureStartTime)) {
			return nil, nil, nil, nil, permError{
				fmt.Errorf(
					"validator start time (%s) more than two weeks after current chain timestamp (%s)",
					startTime,
					currentTimestamp,
				),
			}
		}

		// Ensure this validator isn't currently a validator.
		_, err := currentStakers.GetValidator(tx.Validator.NodeID)
		if err == nil {
			return nil, nil, nil, nil, permError{
				fmt.Errorf(
					"%s is already a primary network validator",
					tx.Validator.NodeID.PrefixedString(constants.NodeIDPrefix),
				),
			}
		}
		if err != database.ErrNotFound {
			return nil, nil, nil, nil, tempError{
				fmt.Errorf(
					"failed to find whether %s is a validator: %w",
					tx.Validator.NodeID.PrefixedString(constants.NodeIDPrefix),
					err,
				),
			}
		}

		// Ensure this validator isn't about to become a validator.
		_, err = pendingStakers.GetValidatorTx(tx.Validator.NodeID)
		if err == nil {
			return nil, nil, nil, nil, permError{
				fmt.Errorf(
					"%s is about to become a primary network validator",
					tx.Validator.NodeID.PrefixedString(constants.NodeIDPrefix),
				),
			}
		}
		if err != database.ErrNotFound {
			return nil, nil, nil, nil, tempError{
				fmt.Errorf(
					"failed to find whether %s is about to become a validator: %w",
					tx.Validator.NodeID.PrefixedString(constants.NodeIDPrefix),
					err,
				),
			}
		}

		// Verify the flowcheck
		if err := vm.semanticVerifySpend(parentState, tx, tx.Ins, outs, stx.Creds, vm.AddStakerTxFee, vm.ctx.AVAXAssetID); err != nil {
			switch err.(type) {
			case permError:
				return nil, nil, nil, nil, permError{
					fmt.Errorf("failed semanticVerifySpend: %w", err),
				}
			default:
				return nil, nil, nil, nil, tempError{
					fmt.Errorf("failed semanticVerifySpend: %w", err),
				}
			}
		}
	}

	// Set up the state if this tx is committed
	newlyPendingStakers := pendingStakers.AddStaker(stx)
	onCommitState := newVersionedState(parentState, currentStakers, newlyPendingStakers)

	// Consume the UTXOS
	consumeInputs(onCommitState, tx.Ins)
	// Produce the UTXOS
	txID := tx.ID()
	produceOutputs(onCommitState, txID, vm.ctx.AVAXAssetID, tx.Outs)

	// Set up the state if this tx is aborted
	onAbortState := newVersionedState(parentState, currentStakers, pendingStakers)
	// Consume the UTXOS
	consumeInputs(onAbortState, tx.Ins)
	// Produce the UTXOS
	produceOutputs(onAbortState, txID, vm.ctx.AVAXAssetID, outs)

	return onCommitState, onAbortState, nil, nil, nil
}

// InitiallyPrefersCommit returns true if the proposed validators start time is
// after the current wall clock time,
func (tx VerifiableUnsignedAddValidatorTx) InitiallyPrefersCommit(vm *VM) bool {
	return tx.StartTime().After(vm.clock.Time())
}

// NewAddValidatorTx returns a new NewAddValidatorTx
func (vm *VM) newAddValidatorTx(
	stakeAmt, // Amount the delegator stakes
	startTime, // Unix time they start delegating
	endTime uint64, // Unix time they stop delegating
	nodeID ids.ShortID, // ID of the node we are delegating to
	rewardAddress ids.ShortID, // Address to send reward to, if applicable
	shares uint32, // 10,000 times percentage of reward taken from delegators
	keys []*crypto.PrivateKeySECP256K1R, // Keys providing the staked tokens
	changeAddr ids.ShortID, // Address to send change to, if there is any
) (*transactions.SignedTx, error) {
	ins, unlockedOuts, lockedOuts, signers, err := vm.stake(keys, stakeAmt, vm.AddStakerTxFee, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}
	// Create the tx
	utx := VerifiableUnsignedAddValidatorTx{
		UnsignedAddValidatorTx: &transactions.UnsignedAddValidatorTx{
			BaseTx: transactions.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    vm.ctx.NetworkID,
				BlockchainID: vm.ctx.ChainID,
				Ins:          ins,
				Outs:         unlockedOuts,
			}},
			Validator: entities.Validator{
				NodeID: nodeID,
				Start:  startTime,
				End:    endTime,
				Wght:   stakeAmt,
			},
			Stake: lockedOuts,
			RewardsOwner: &secp256k1fx.OutputOwners{
				Locktime:  0,
				Threshold: 1,
				Addrs:     []ids.ShortID{rewardAddress},
			},
			Shares: shares,
		},
	}

	tx := &transactions.SignedTx{UnsignedTx: utx}
	if err := tx.Sign(platformcodec.Codec, signers); err != nil {
		return nil, err
	}
	syntacticCtx := transactions.ProposalTxSyntacticVerificationContext{
		Ctx:               vm.ctx,
		C:                 platformcodec.Codec,
		MinValidatorStake: vm.MinValidatorStake,
		MaxValidatorStake: vm.MaxValidatorStake,
		MinStakeDuration:  vm.MinStakeDuration,
		MaxStakeDuration:  vm.MaxStakeDuration,
		MinDelegationFee:  vm.MinDelegationFee,
	}
	return tx, utx.SyntacticVerify(syntacticCtx)
}
