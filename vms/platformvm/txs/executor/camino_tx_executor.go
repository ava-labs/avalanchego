// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	deposits "github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/treasury"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Max number of items allowed in a page
const maxPageSize = 1024

var (
	_ txs.Visitor = (*CaminoStandardTxExecutor)(nil)
	_ txs.Visitor = (*CaminoProposalTxExecutor)(nil)

	errNodeSignatureMissing       = errors.New("last signature is not nodeID's signature")
	errWrongLockMode              = errors.New("this tx can't be used with this caminoGenesis.LockModeBondDeposit")
	errRecoverAdresses            = errors.New("cannot recover addresses from credentials")
	errInvalidRoles               = errors.New("invalid role")
	errValidatorExists            = errors.New("node is already a validator")
	errInvalidSystemTxBody        = errors.New("tx body doesn't match expected one")
	errRemoveValidatorToEarly     = errors.New("attempting to remove validator before its end time")
	errRemoveWrongValidator       = errors.New("attempting to remove wrong validator")
	errDepositOfferNotActiveYet   = errors.New("deposit offer not active yet")
	errDepositOfferInactive       = errors.New("deposit offer inactive")
	errDepositToSmall             = errors.New("deposit amount is less than deposit offer minmum amount")
	errDepositDurationToSmall     = errors.New("deposit duration is less than deposit offer minmum duration")
	errDepositDurationToBig       = errors.New("deposit duration is greater than deposit offer maximum duration")
	errSupplyOverflow             = errors.New("resulting total supply would be more, than allowed maximum")
	errNotConsortiumMember        = errors.New("address isn't consortium member")
	errValidatorNotFound          = errors.New("validator not found")
	errConsortiumMemberHasNode    = errors.New("consortium member already has registered node")
	errConsortiumSignatureMissing = errors.New("wrong consortium's member signature")
	errNodeNotRegistered          = errors.New("no address registered for this node")
	errNotNodeOwner               = errors.New("node is registered for another address")
	errDepositCredentialMissmatch = errors.New("deposit credential isn't matching")
	errDepositNotFound            = errors.New("deposit tx not found")
	errNotSECPOwner               = errors.New("owner is not *secp256k1fx.OutputOwners")
	errWrongCredentialsNumber     = errors.New("unexpected number of credentials")
	errWrongOwnerType             = errors.New("wrong owner type")
	errImportedUTXOMissmatch      = errors.New("imported input doesn't match expected utxo")
	errInputAmountMissmatch       = errors.New("utxo amount doesn't match input amount")
	errInputsUTXOSMismatch        = errors.New("number of inputs is different from number of utxos")
)

type CaminoStandardTxExecutor struct {
	StandardTxExecutor
}

type CaminoProposalTxExecutor struct {
	ProposalTxExecutor
}

/* TLS certificates build by caminogo contain a secp256k1 signature of the
 * x509 public signed with the nodeIDs private key
 * TX which require nodeID verification (tx with nodeID parameter) must contain
 * an additional signature after the signatures used for input verification.
 * This signature must recover to the nodeID itself to verify that the sender
 * has access to this node specific private key.
 */
func (e *CaminoStandardTxExecutor) verifyNodeSignature(nodeID ids.NodeID) error {
	return e.verifyNodeSignatureSig(nodeID, e.Tx.Creds[len(e.Tx.Creds)-1])
}

// Verify that one of the sigs recovers to nodeID
func (e *CaminoStandardTxExecutor) verifyNodeSignatureSig(nodeID ids.NodeID, sigs verify.Verifiable) error {
	if err := e.Backend.Fx.VerifyPermission(
		e.Tx.Unsigned,
		&secp256k1fx.Input{SigIndices: []uint32{0}},
		sigs,
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				ids.ShortID(nodeID),
			},
		},
	); err != nil {
		return fmt.Errorf("%w: %s", errNodeSignatureMissing, err)
	}
	return nil
}

func (e *CaminoStandardTxExecutor) AddValidatorTx(tx *txs.AddValidatorTx) error {
	caminoConfig, err := e.State.CaminoConfig()
	if err != nil {
		return err
	}

	if err := locked.VerifyLockMode(tx.Ins, tx.Outs, caminoConfig.LockModeBondDeposit); err != nil {
		return err
	}

	// verify avax tx

	_, isCaminoTx := e.Tx.Unsigned.(*txs.CaminoAddValidatorTx)

	if !caminoConfig.LockModeBondDeposit && !isCaminoTx {
		return e.StandardTxExecutor.AddValidatorTx(tx)
	}

	if !caminoConfig.LockModeBondDeposit || !isCaminoTx {
		return errWrongLockMode
	}

	// verify camino tx

	if err := e.Tx.SyntacticVerify(e.Backend.Ctx); err != nil {
		return err
	}

	// verify that node owned by consortium member

	consortiumMemberAddress, err := e.State.GetShortIDLink(
		ids.ShortID(tx.NodeID()),
		state.ShortLinkKeyRegisterNode,
	)
	if err != nil {
		return fmt.Errorf("%w: %s", errNodeNotRegistered, err)
	}

	// verifying consortium member signatures

	signersAddresses, err := e.Fx.RecoverAddresses(tx, e.Tx.Creds)
	if err != nil {
		return err
	}

	consortiumMemberOwner, err := state.GetOwner(e.State, consortiumMemberAddress)
	if err != nil {
		return err
	}

	if err := verifyAddrsOwner(signersAddresses, consortiumMemberOwner); err != nil {
		return fmt.Errorf("%w: %s", errConsortiumSignatureMissing, err)
	}

	// verify validator

	duration := tx.Validator.Duration()

	switch {
	case tx.Validator.Wght < e.Backend.Config.MinValidatorStake:
		// Ensure validator is staking at least the minimum amount
		return errWeightTooSmall
	case tx.Validator.Wght > e.Backend.Config.MaxValidatorStake:
		// Ensure validator isn't staking too much
		return errWeightTooLarge
	case duration < e.Backend.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return errStakeTooShort
	case duration > e.Backend.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return errStakeTooLong
	}

	if e.Backend.Bootstrapped.GetValue() {
		currentTimestamp := e.State.GetTimestamp()
		// Ensure the proposed validator starts after the current time
		startTime := tx.StartTime()
		if !currentTimestamp.Before(startTime) {
			return fmt.Errorf(
				"%w: %s >= %s",
				errTimestampNotBeforeStartTime,
				currentTimestamp,
				startTime,
			)
		}

		if err := validatorExists(e.State, constants.PrimaryNetworkID, tx.Validator.NodeID); err != nil {
			return err
		}

		rewardOwner, ok := tx.RewardsOwner.(*secp256k1fx.OutputOwners)
		if !ok {
			return errWrongOwnerType
		}

		if err := e.Fx.VerifyMultisigOwner(
			&secp256k1fx.TransferOutput{
				OutputOwners: *rewardOwner,
			}, e.State,
		); err != nil {
			return err
		}

		// Verify the flowcheck
		if err := e.Backend.FlowChecker.VerifyLock(
			tx,
			e.State,
			tx.Ins,
			tx.Outs,
			e.Tx.Creds,
			e.Backend.Config.AddPrimaryNetworkValidatorFee,
			e.Backend.Ctx.AVAXAssetID,
			locked.StateBonded,
		); err != nil {
			return fmt.Errorf("%w: %s", errFlowCheckFailed, err)
		}

		// Make sure the tx doesn't start too far in the future. This is done last
		// to allow the verifier visitor to explicitly check for this error.
		maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
		if startTime.After(maxStartTime) {
			return errFutureStakeTime
		}
	}

	txID := e.Tx.ID()
	newStaker, err := state.NewPendingStaker(txID, tx)
	if err != nil {
		return err
	}
	e.State.PutPendingValidator(newStaker)
	utxo.Consume(e.State, tx.Ins)
	if err := utxo.ProduceLocked(e.State, txID, tx.Outs, locked.StateBonded); err != nil {
		return err
	}

	return nil
}

func (e *CaminoStandardTxExecutor) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}
	caminoConfig, err := e.State.CaminoConfig()
	if err != nil {
		return err
	}

	if caminoConfig.VerifyNodeSignature {
		if err := e.verifyNodeSignature(tx.NodeID()); err != nil {
			return err
		}
		creds := removeCreds(e.Tx, 1)
		defer addCreds(e.Tx, creds)
	}

	return e.StandardTxExecutor.AddSubnetValidatorTx(tx)
}

func (e *CaminoStandardTxExecutor) AddDelegatorTx(tx *txs.AddDelegatorTx) error {
	caminoConfig, err := e.State.CaminoConfig()
	if err != nil {
		return err
	}

	if caminoConfig.LockModeBondDeposit {
		return errWrongTxType
	}

	if err := locked.VerifyLockMode(tx.Ins, tx.Outs, caminoConfig.LockModeBondDeposit); err != nil {
		return err
	}

	return e.StandardTxExecutor.AddDelegatorTx(tx)
}

func (e *CaminoStandardTxExecutor) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	caminoConfig, err := e.State.CaminoConfig()
	if err != nil {
		return err
	}

	if caminoConfig.LockModeBondDeposit {
		return errWrongTxType
	}

	if err := locked.VerifyLockMode(tx.Ins, tx.Outs, caminoConfig.LockModeBondDeposit); err != nil {
		return err
	}

	// Signer (node signature) has to recover to nodeID in case we
	// add a validator to the primary network
	if tx.Subnet == constants.PrimaryNetworkID && tx.Signer.Key() != nil {
		sigs := make([][crypto.SECP256K1RSigLen]byte, 1)
		copy(sigs[0][:], tx.Signer.Signature()[:crypto.SECP256K1RSigLen])

		if err := e.verifyNodeSignatureSig(tx.NodeID(),
			&secp256k1fx.Credential{Sigs: sigs},
		); err != nil {
			return err
		}
	}

	return e.StandardTxExecutor.AddPermissionlessValidatorTx(tx)
}

func (e *CaminoStandardTxExecutor) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	caminoConfig, err := e.State.CaminoConfig()
	if err != nil {
		return err
	}

	if caminoConfig.LockModeBondDeposit {
		return errWrongTxType
	}

	if err := locked.VerifyLockMode(tx.Ins, tx.Outs, caminoConfig.LockModeBondDeposit); err != nil {
		return err
	}

	return e.StandardTxExecutor.AddPermissionlessDelegatorTx(tx)
}

func (e *CaminoStandardTxExecutor) CreateChainTx(tx *txs.CreateChainTx) error {
	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}

	return e.StandardTxExecutor.CreateChainTx(tx)
}

func (e *CaminoStandardTxExecutor) CreateSubnetTx(tx *txs.CreateSubnetTx) error {
	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}

	return e.StandardTxExecutor.CreateSubnetTx(tx)
}

func (e *CaminoStandardTxExecutor) ExportTx(tx *txs.ExportTx) error {
	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}

	if err := locked.VerifyNoLocks(nil, tx.ExportedOutputs); err != nil {
		return err
	}

	return e.StandardTxExecutor.ExportTx(tx)
}

func (e *CaminoStandardTxExecutor) ImportTx(tx *txs.ImportTx) error {
	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}

	return e.StandardTxExecutor.ImportTx(tx)
}

func (e *CaminoStandardTxExecutor) RemoveSubnetValidatorTx(tx *txs.RemoveSubnetValidatorTx) error {
	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}

	return e.StandardTxExecutor.RemoveSubnetValidatorTx(tx)
}

func (e *CaminoStandardTxExecutor) TransformSubnetTx(tx *txs.TransformSubnetTx) error {
	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}

	return e.StandardTxExecutor.TransformSubnetTx(tx)
}

func (e *CaminoProposalTxExecutor) RewardValidatorTx(tx *txs.RewardValidatorTx) error {
	caminoConfig, err := e.OnCommitState.CaminoConfig()
	if err != nil {
		return err
	}

	caminoTx, ok := e.Tx.Unsigned.(*txs.CaminoRewardValidatorTx)

	if !caminoConfig.LockModeBondDeposit && !ok {
		return e.ProposalTxExecutor.RewardValidatorTx(tx)
	}

	if !caminoConfig.LockModeBondDeposit || !ok {
		return errWrongLockMode
	}

	switch {
	case tx == nil:
		return txs.ErrNilTx
	case tx.TxID == ids.Empty:
		return errInvalidID
	case len(e.Tx.Creds) != 0:
		return errWrongCredentialsNumber
	}

	ins, outs, err := e.FlowChecker.Unlock(e.OnCommitState, []ids.ID{tx.TxID}, locked.StateBonded)
	if err != nil {
		return err
	}

	expectedTx := &txs.CaminoRewardValidatorTx{
		RewardValidatorTx: *tx,
		Ins:               ins,
		Outs:              outs,
	}

	if !reflect.DeepEqual(caminoTx, expectedTx) {
		return errInvalidSystemTxBody
	}

	var currentStakerToRemove *state.Staker
	var deferredStakerToRemove *state.Staker
	var stakerToRemove *state.Staker
	removeFromCurrent := false

	currentStakerIterator, err := e.OnCommitState.GetCurrentStakerIterator()
	if err != nil {
		return err
	}
	if currentStakerIterator.Next() {
		currentStakerToRemove = currentStakerIterator.Value()
		currentStakerIterator.Release()
	}

	deferredStakerIterator, err := e.OnCommitState.GetDeferredStakerIterator()
	if err != nil {
		return err
	}
	if deferredStakerIterator.Next() {
		deferredStakerToRemove = deferredStakerIterator.Value()
		deferredStakerIterator.Release()
	}

	if currentStakerToRemove == nil && deferredStakerToRemove == nil {
		return fmt.Errorf("failed to get next staker to remove: %w", database.ErrNotFound)
	}

	switch {
	case currentStakerToRemove != nil && currentStakerToRemove.TxID == tx.TxID:
		removeFromCurrent = true
		stakerToRemove = currentStakerToRemove
	case deferredStakerToRemove != nil && deferredStakerToRemove.TxID == tx.TxID:
		stakerToRemove = deferredStakerToRemove
	default:
		return fmt.Errorf(
			"trying to remove validator %s: %w",
			tx.TxID,
			errRemoveWrongValidator,
		)
	}

	// Verify that the chain's timestamp is the validator's end time
	currentChainTime := e.OnCommitState.GetTimestamp()
	if !stakerToRemove.EndTime.Equal(currentChainTime) {
		return fmt.Errorf(
			"removing validator %s at %s, but its endtime is %s: %w",
			tx.TxID,
			currentChainTime,
			stakerToRemove.EndTime,
			errRemoveValidatorToEarly,
		)
	}

	stakerTx, _, err := e.OnCommitState.GetTx(stakerToRemove.TxID)
	if err != nil {
		return fmt.Errorf("failed to get next removed staker tx: %w", err)
	}

	if _, ok := stakerTx.Unsigned.(txs.ValidatorTx); !ok {
		// Invariant: Permissioned stakers are removed by the advancement of
		//            time and the current chain timestamp is == this staker's
		//            EndTime. This means only permissionless stakers should be
		//            left in the staker set.
		return errShouldBePermissionlessStaker
	}

	if removeFromCurrent {
		e.OnCommitState.DeleteCurrentValidator(stakerToRemove)
		e.OnAbortState.DeleteCurrentValidator(stakerToRemove)
	} else {
		e.OnCommitState.DeleteDeferredValidator(stakerToRemove)
		e.OnAbortState.DeleteDeferredValidator(stakerToRemove)
		// Reset deferred bit on node owner address for onCommitState
		nodeOwnerAddressOnCommit, err := e.OnCommitState.GetShortIDLink(
			ids.ShortID(stakerToRemove.NodeID),
			state.ShortLinkKeyRegisterNode,
		)
		if err != nil {
			return err
		}
		nodeOwnerAddressStateOnCommit, err := e.OnCommitState.GetAddressStates(nodeOwnerAddressOnCommit)
		if err != nil {
			return err
		}
		e.OnCommitState.SetAddressStates(nodeOwnerAddressOnCommit, nodeOwnerAddressStateOnCommit&^txs.AddressStateNodeDeferredBit)

		// Reset deferred bit on node owner address for onAbortState
		nodeOwnerAddressOnAbort, err := e.OnAbortState.GetShortIDLink(
			ids.ShortID(stakerToRemove.NodeID),
			state.ShortLinkKeyRegisterNode,
		)
		if err != nil {
			return err
		}
		nodeOwnerAddressStateOnAbort, err := e.OnAbortState.GetAddressStates(nodeOwnerAddressOnAbort)
		if err != nil {
			return err
		}
		e.OnCommitState.SetAddressStates(nodeOwnerAddressOnAbort, nodeOwnerAddressStateOnAbort&^txs.AddressStateNodeDeferredBit)
	}

	txID := e.Tx.ID()

	utxo.Consume(e.OnCommitState, caminoTx.Ins)
	utxo.Consume(e.OnAbortState, caminoTx.Ins)
	utxo.Produce(e.OnCommitState, txID, caminoTx.Outs)
	utxo.Produce(e.OnAbortState, txID, caminoTx.Outs)

	return nil
}

func (e *CaminoStandardTxExecutor) DepositTx(tx *txs.DepositTx) error {
	caminoConfig, err := e.State.CaminoConfig()
	if err != nil {
		return err
	}

	if !caminoConfig.LockModeBondDeposit {
		return errWrongLockMode
	}

	if err := locked.VerifyLockMode(tx.Ins, tx.Outs, caminoConfig.LockModeBondDeposit); err != nil {
		return err
	}

	if err := e.Tx.SyntacticVerify(e.Backend.Ctx); err != nil {
		return err
	}

	depositAmount, err := tx.DepositAmount()
	if err != nil {
		return err
	}

	depositOffer, err := e.State.GetDepositOffer(tx.DepositOfferID)
	if err != nil {
		return err
	}

	currentChainTime := e.State.GetTimestamp()

	switch {
	case depositOffer.Flags&deposits.OfferFlagLocked != 0:
		return errDepositOfferInactive
	case depositOffer.StartTime().After(currentChainTime):
		return errDepositOfferNotActiveYet
	case depositOffer.EndTime().Before(currentChainTime):
		return errDepositOfferInactive
	case tx.DepositDuration < depositOffer.MinDuration:
		return errDepositDurationToSmall
	case tx.DepositDuration > depositOffer.MaxDuration:
		return errDepositDurationToBig
	case depositAmount < depositOffer.MinAmount:
		return errDepositToSmall
	}

	rewardOwner, ok := tx.RewardsOwner.(*secp256k1fx.OutputOwners)
	if !ok {
		return errWrongOwnerType
	}

	if err := e.Fx.VerifyMultisigOwner(
		&secp256k1fx.TransferOutput{
			OutputOwners: *rewardOwner,
		}, e.State,
	); err != nil {
		return err
	}

	if err := e.FlowChecker.VerifyLock(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		e.Tx.Creds,
		e.Config.TxFee,
		e.Ctx.AVAXAssetID,
		locked.StateDeposited,
	); err != nil {
		return fmt.Errorf("%w: %s", errFlowCheckFailed, err)
	}

	txID := e.Tx.ID()

	currentSupply, err := e.State.GetCurrentSupply(constants.PrimaryNetworkID)
	if err != nil {
		return err
	}

	deposit := &deposits.Deposit{
		DepositOfferID: tx.DepositOfferID,
		Duration:       tx.DepositDuration,
		Amount:         depositAmount,
		Start:          uint64(currentChainTime.Unix()),
	}

	potentialReward := deposit.TotalReward(depositOffer)

	newSupply, err := math.Add64(currentSupply, potentialReward)
	if err != nil || newSupply > e.Config.RewardConfig.SupplyCap {
		return errSupplyOverflow
	}

	e.State.SetCurrentSupply(constants.PrimaryNetworkID, newSupply)
	e.State.UpdateDeposit(txID, deposit)

	utxo.Consume(e.State, tx.Ins)
	if err := utxo.ProduceLocked(e.State, txID, tx.Outs, locked.StateDeposited); err != nil {
		return err
	}

	return nil
}

func (e *CaminoStandardTxExecutor) UnlockDepositTx(tx *txs.UnlockDepositTx) error {
	caminoConfig, err := e.State.CaminoConfig()
	if err != nil {
		return err
	}

	if !caminoConfig.LockModeBondDeposit {
		return errWrongLockMode
	}

	if err := locked.VerifyLockMode(tx.Ins, tx.Outs, caminoConfig.LockModeBondDeposit); err != nil {
		return err
	}

	if err := e.Tx.SyntacticVerify(e.Backend.Ctx); err != nil {
		return err
	}

	newUnlockedAmounts, err := e.FlowChecker.VerifyUnlockDeposit(
		e.State,
		tx,
		tx.Ins,
		tx.Outs,
		e.Tx.Creds,
		e.Config.TxFee,
		e.Ctx.AVAXAssetID,
	)
	if err != nil {
		return fmt.Errorf("%w: %s", errFlowCheckFailed, err)
	}

	for depositTxID, newUnlockedAmount := range newUnlockedAmounts {
		deposit, err := e.State.GetDeposit(depositTxID)
		if err != nil {
			return err
		}

		newUnlockedAmount, err := math.Add64(newUnlockedAmount, deposit.UnlockedAmount)
		if err != nil {
			return err
		}

		offer, err := e.State.GetDepositOffer(deposit.DepositOfferID)
		if err != nil {
			return err
		}

		var updatedDeposit *deposits.Deposit
		if newUnlockedAmount < deposit.Amount ||
			deposit.ClaimedRewardAmount < deposit.TotalReward(offer) {
			updatedDeposit = &deposits.Deposit{
				DepositOfferID:      deposit.DepositOfferID,
				UnlockedAmount:      newUnlockedAmount,
				ClaimedRewardAmount: deposit.ClaimedRewardAmount,
				Amount:              deposit.Amount,
				Start:               deposit.Start,
			}
		}

		e.State.UpdateDeposit(depositTxID, updatedDeposit)
	}

	utxo.Consume(e.State, tx.Ins)
	utxo.Produce(e.State, e.Tx.ID(), tx.Outs)

	return nil
}

func (e *CaminoStandardTxExecutor) ClaimRewardTx(tx *txs.ClaimRewardTx) error {
	caminoConfig, err := e.State.CaminoConfig()
	if err != nil {
		return err
	}

	if !caminoConfig.LockModeBondDeposit {
		return errWrongLockMode
	}

	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}

	if err := e.Tx.SyntacticVerify(e.Backend.Ctx); err != nil {
		return err
	}

	if err := e.FlowChecker.VerifyLock(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		e.Tx.Creds[:len(e.Tx.Creds)-1],
		e.Config.TxFee,
		e.Ctx.AVAXAssetID,
		locked.StateDeposited,
	); err != nil {
		return fmt.Errorf("%w: %s", errFlowCheckFailed, err)
	}

	currentTimestamp := uint64(e.State.GetTimestamp().Unix())
	claimRewardTxID := e.Tx.ID()

	for _, depositTxID := range tx.DepositTxs {
		// getting deposit tx

		signedDepositTx, txStatus, err := e.State.GetTx(depositTxID)
		if err != nil {
			return fmt.Errorf("%w: %s", errDepositNotFound, err)
		}
		if txStatus != status.Committed {
			return fmt.Errorf("%w: %s", errDepositNotFound, "tx is not committed")
		}
		depositTx, ok := signedDepositTx.Unsigned.(*txs.DepositTx)
		if !ok {
			return fmt.Errorf("%w: %s", errDepositNotFound, errWrongTxType)
		}

		// checking deposit signatures

		depositRewardsOwner, ok := depositTx.RewardsOwner.(*secp256k1fx.OutputOwners)
		if !ok {
			return errNotSECPOwner
		}

		if err := e.Fx.VerifyPermissionUnordered(tx, e.Tx.Creds[len(e.Tx.Creds)-1], depositRewardsOwner); err != nil {
			return fmt.Errorf("%w: %s", errDepositCredentialMissmatch, err)
		}

		// creating reward output, if there is any

		deposit, err := e.State.GetDeposit(depositTxID)
		if err != nil {
			return err
		}

		depositOffer, err := e.State.GetDepositOffer(deposit.DepositOfferID)
		if err != nil {
			return err
		}

		claimableReward := deposit.ClaimableReward(depositOffer, currentTimestamp)
		if claimableReward > 0 {
			rewardsOwner := tx.RewardsOwner
			secpOwners, ok := rewardsOwner.(*secp256k1fx.OutputOwners)
			if !ok {
				return errNotSECPOwner
			}
			if len(secpOwners.Addrs) == 0 {
				rewardsOwner = depositTx.RewardsOwner
			}

			outIntf, err := e.Fx.CreateOutput(claimableReward, rewardsOwner)
			if err != nil {
				return fmt.Errorf("failed to create output: %w", err)
			}
			out, ok := outIntf.(verify.State)
			if !ok {
				return errInvalidState
			}

			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        claimRewardTxID,
					OutputIndex: uint32(len(tx.Outs)),
				},
				Asset: avax.Asset{ID: e.Ctx.AVAXAssetID},
				Out:   out,
			}

			e.State.AddUTXO(utxo)
			e.State.AddRewardUTXO(depositTxID, utxo)
		}
	}

	utxo.Consume(e.State, tx.Ins)
	utxo.Produce(e.State, e.Tx.ID(), tx.Outs)

	return nil
}

func (e *CaminoStandardTxExecutor) RegisterNodeTx(tx *txs.RegisterNodeTx) error {
	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}

	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	// verify consortium member state

	consortiumMemberAddressState, err := e.State.GetAddressStates(tx.ConsortiumMemberAddress)
	if err != nil {
		return err
	}

	if consortiumMemberAddressState&txs.AddressStateConsortiumBit == 0 {
		return errNotConsortiumMember
	}

	newNodeIDEmpty := tx.NewNodeID == ids.EmptyNodeID
	oldNodeIDEmpty := tx.OldNodeID == ids.EmptyNodeID

	linkedNodeID, err := e.State.GetShortIDLink(tx.ConsortiumMemberAddress, state.ShortLinkKeyRegisterNode)
	haslinkedNode := err != database.ErrNotFound
	if haslinkedNode && err != nil {
		return err
	}

	if oldNodeIDEmpty && haslinkedNode {
		return errConsortiumMemberHasNode
	}

	// verify consortium member cred

	consortiumMemberOwner, err := state.GetOwner(e.State, tx.ConsortiumMemberAddress)
	if err != nil {
		return err
	}

	if err := e.Backend.Fx.VerifyPermission(
		e.Tx.Unsigned,
		tx.ConsortiumMemberAuth,
		e.Tx.Creds[len(e.Tx.Creds)-1], // consortium member cred
		consortiumMemberOwner,
	); err != nil {
		return fmt.Errorf("%w: %s", errConsortiumSignatureMissing, err)
	}

	// verify old nodeID ownership

	if !oldNodeIDEmpty && (!haslinkedNode || tx.OldNodeID != ids.NodeID(linkedNodeID)) {
		return errNotNodeOwner
	}

	// verify that the old node does not exist in any of the pending, current or deferred validator sets

	if !oldNodeIDEmpty {
		if err := validatorExists(e.State, constants.PrimaryNetworkID, tx.OldNodeID); err != nil {
			return err
		}
	}

	// verify new nodeID cred

	if !newNodeIDEmpty {
		if err := e.Backend.Fx.VerifyPermission(
			e.Tx.Unsigned,
			&secp256k1fx.Input{SigIndices: []uint32{0}},
			e.Tx.Creds[len(e.Tx.Creds)-2], // new nodeID cred
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{ids.ShortID(tx.NewNodeID)},
			},
		); err != nil {
			return fmt.Errorf("%w: %s", errNodeSignatureMissing, err)
		}
	}

	// verify the flowcheck

	if err := e.FlowChecker.VerifyLock(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		e.Tx.Creds[:len(e.Tx.Creds)-2], // base tx creds
		e.Config.TxFee,
		e.Ctx.AVAXAssetID,
		locked.StateUnlocked,
	); err != nil {
		return err
	}

	// update state

	txID := e.Tx.ID()

	// Consume the UTXOS
	utxo.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.State, txID, tx.Outs)

	if !oldNodeIDEmpty {
		e.State.SetShortIDLink(ids.ShortID(tx.OldNodeID), state.ShortLinkKeyRegisterNode, nil)
		e.State.SetShortIDLink(tx.ConsortiumMemberAddress, state.ShortLinkKeyRegisterNode, nil)
	}

	if !newNodeIDEmpty {
		e.State.SetShortIDLink(ids.ShortID(tx.NewNodeID),
			state.ShortLinkKeyRegisterNode,
			&tx.ConsortiumMemberAddress,
		)
		link := ids.ShortID(tx.NewNodeID)
		e.State.SetShortIDLink(tx.ConsortiumMemberAddress,
			state.ShortLinkKeyRegisterNode,
			&link,
		)
	}

	return nil
}

func (e *CaminoStandardTxExecutor) RewardsImportTx(tx *txs.RewardsImportTx) error {
	caminoConfig, err := e.State.CaminoConfig()
	if err != nil {
		return err
	}

	if !caminoConfig.LockModeBondDeposit {
		return errWrongLockMode
	}

	outs := tx.Outputs()

	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	if e.Bootstrapped.GetValue() {
		// Getting all treasury utxos exported from c-chain, collecting ones that are old enough

		allUTXOBytes, _, _, err := e.Ctx.SharedMemory.Indexed(
			e.Ctx.CChainID,
			treasury.AddrTraitsBytes,
			ids.ShortEmpty[:], ids.Empty[:], maxPageSize,
		)
		if err != nil {
			return fmt.Errorf("error fetching atomic UTXOs: %w", err)
		}

		chainTimestamp := uint64(e.State.GetTimestamp().Unix())

		utxos := []*avax.UTXO{}
		for _, utxoBytes := range allUTXOBytes {
			utxo := &avax.TimedUTXO{}
			if _, err := txs.Codec.Unmarshal(utxoBytes, utxo); err != nil {
				// that means that this could be simple, not-timed utxo
				continue
			}

			if utxo.Timestamp <= chainTimestamp-atomic.SharedMemorySyncBound {
				utxos = append(utxos, &utxo.UTXO)
			}
		}

		// Verifying that utxos match inputs

		if len(tx.Ins) != len(utxos) {
			return fmt.Errorf("there are %d inputs and %d utxos: %w", len(tx.Ins), len(utxos), errInputsUTXOSMismatch)
		}

		for i, in := range tx.Ins {
			utxo := utxos[i]

			if utxo.InputID() != in.InputID() {
				return errImportedUTXOMissmatch
			}

			out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
			if !ok {
				// should never happen
				return locked.ErrWrongOutType
			}

			if out.Amt != in.In.Amount() {
				return fmt.Errorf("utxo.Amt %d, input.Amt %d: %w", out.Amt, in.In.Amount(), errInputAmountMissmatch)
			}
		}
	}

	// Getting active validators

	currentStakerIterator, err := e.State.GetCurrentStakerIterator()
	if err != nil {
		return err
	}
	defer currentStakerIterator.Release()

	validators := set.Set[ids.ShortID]{}
	for currentStakerIterator.Next() {
		staker := currentStakerIterator.Value()
		if staker.SubnetID != constants.PrimaryNetworkID {
			continue
		}

		validatorAddr, err := e.State.GetShortIDLink(
			ids.ShortID(staker.NodeID),
			state.ShortLinkKeyRegisterNode,
		)
		if err != nil {
			return err
		}
		validators.Add(validatorAddr)
	}

	// Set not distributed validator reward

	notDistributedAmount, err := e.State.GetNotDistributedValidatorReward()
	if err != nil {
		return err
	}

	amountToDistribute, err := math.Add64(tx.Outs[0].Out.Amount(), notDistributedAmount)
	if err != nil {
		return err
	}

	addedReward := amountToDistribute / uint64(validators.Len())
	newNotDistributedAmount := amountToDistribute - addedReward*uint64(validators.Len())

	if newNotDistributedAmount != notDistributedAmount {
		e.State.SetNotDistributedValidatorReward(newNotDistributedAmount)
	}

	// Set claimables

	if addedReward != 0 {
		for validatorAddr := range validators {
			owner := &secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{validatorAddr},
			}

			ownerID, err := txs.GetOwnerID(owner)
			if err != nil {
				return err
			}

			claimable, err := e.State.GetClaimable(ownerID)
			if err == database.ErrNotFound {
				claimable = &state.Claimable{
					Owner: owner,
				}
			} else if err != nil {
				return err
			}

			claimable.ValidatorReward, err = math.Add64(claimable.ValidatorReward, addedReward)
			if err != nil {
				return err
			}

			e.State.SetClaimable(ownerID, claimable)
		}
	}

	// Produce UTXOs and atomic request

	utxo.Produce(e.State, e.Tx.ID(), outs)

	utxoIDs := make([][]byte, len(tx.Ins))
	e.Inputs = set.NewSet[ids.ID](len(tx.Ins))
	for i := range tx.Ins {
		utxoID := tx.Ins[i].InputID()
		e.Inputs.Add(utxoID)
		utxoIDs[i] = utxoID[:]
	}

	e.AtomicRequests = map[ids.ID]*atomic.Requests{
		e.Ctx.CChainID: {
			RemoveRequests: utxoIDs,
		},
	}

	return nil
}

func removeCreds(tx *txs.Tx, num int) []verify.Verifiable {
	newCredsLen := len(tx.Creds) - num
	removedCreds := tx.Creds[newCredsLen:len(tx.Creds)]
	tx.Creds = tx.Creds[:newCredsLen]
	return removedCreds
}

func addCreds(tx *txs.Tx, creds []verify.Verifiable) {
	tx.Creds = append(tx.Creds, creds...)
}

func (e *CaminoStandardTxExecutor) AddressStateTx(tx *txs.AddressStateTx) error {
	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}

	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	addresses, err := e.Fx.RecoverAddresses(tx, e.Tx.Creds)
	if err != nil {
		return fmt.Errorf("%w: %s", errRecoverAdresses, err)
	}

	if len(addresses) == 0 {
		return errWrongNumberOfCredentials
	}

	// Accumulate roles over all signers
	roles := uint64(0)
	for address := range addresses {
		states, err := e.State.GetAddressStates(address)
		if err != nil {
			return err
		}
		roles |= states
	}
	statesBit := uint64(1) << uint64(tx.State)

	// Verify that roles are allowed to modify tx.State
	if err := verifyAccess(roles, statesBit); err != nil {
		return err
	}

	// Get the current state
	states, err := e.State.GetAddressStates(tx.Address)
	if err != nil {
		return err
	}
	// Calculate new states
	newStates := states
	if tx.Remove && (states&statesBit) != 0 {
		newStates ^= statesBit
	} else if !tx.Remove {
		newStates |= statesBit
	}

	// Verify the flowcheck
	if err := e.FlowChecker.VerifySpend(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		e.Tx.Creds,
		map[ids.ID]uint64{
			e.Ctx.AVAXAssetID: e.Config.TxFee,
		},
	); err != nil {
		return err
	}

	txID := e.Tx.ID()

	if tx.State == txs.AddressStateNodeDeferred {
		nodeShortID, err := e.State.GetShortIDLink(tx.Address, state.ShortLinkKeyRegisterNode)
		if err != nil {
			return fmt.Errorf("couldn't get consortium member registered nodeID: %w", err)
		}
		nodeID := ids.NodeID(nodeShortID)
		if tx.Remove {
			// transfer staker to from deferred to current stakers set
			stakerToReactivate, err := e.State.GetDeferredValidator(constants.PrimaryNetworkID, nodeID)
			if err != nil {
				return fmt.Errorf("validator with nodeID %s, does not exist in deferred stakers set: %w", nodeID, errValidatorNotFound)
			}
			e.State.DeleteDeferredValidator(stakerToReactivate)
			e.State.PutCurrentValidator(stakerToReactivate)
		} else {
			// transfer staker to from current to deferred stakers set
			stakerToDefer, err := e.State.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
			if err != nil {
				return fmt.Errorf("validator with nodeID %s, does not exist in current stakers set: %w", nodeID, errValidatorNotFound)
			}
			e.State.DeleteCurrentValidator(stakerToDefer)
			e.State.PutDeferredValidator(stakerToDefer)
		}
	}

	// Consume the UTXOS
	utxo.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.State, txID, tx.Outs)
	// Set the new states if changed
	if states != newStates {
		e.State.SetAddressStates(tx.Address, newStates)
	}

	return nil
}

func verifyAccess(roles, statesBit uint64) error {
	switch {
	case (roles & txs.AddressStateRoleAdminBit) != 0:
	case (txs.AddressStateKycBits & statesBit) != 0:
		if (roles & txs.AddressStateRoleKycBit) == 0 {
			return errInvalidRoles
		}
	case (txs.AddressStateRoleBits & statesBit) != 0:
		return errInvalidRoles
	}
	return nil
}

func verifyAddrsOwner(addrs secp256k1fx.RecoverMap, owner *secp256k1fx.OutputOwners) error {
	matchingSigsCount := uint32(0)
	for _, addr := range owner.Addrs {
		if _, exists := addrs[addr]; exists {
			matchingSigsCount++
			if matchingSigsCount == owner.Threshold {
				return nil
			}
		}
	}
	return errors.New("missing signature")
}

func validatorExists(state state.Chain, subnetID ids.ID, nodeID ids.NodeID) error {
	if _, err := GetValidator(state, subnetID, nodeID); err == nil {
		return errValidatorExists
	} else if _, err := state.GetDeferredValidator(subnetID, nodeID); err == nil {
		return errValidatorExists
	} else if err != database.ErrNotFound {
		return fmt.Errorf(
			"failed to find whether %s is a primary network validator: %w",
			nodeID,
			err,
		)
	}
	return nil
}
