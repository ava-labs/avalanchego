// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	as "github.com/ava-labs/avalanchego/vms/platformvm/addrstate"
	dacProposals "github.com/ava-labs/avalanchego/vms/platformvm/dac"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/treasury"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor/dac"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"golang.org/x/exp/slices"

	deposits "github.com/ava-labs/avalanchego/vms/platformvm/deposit"
)

// Max number of items allowed in a page
const maxPageSize = 1024

var (
	_ txs.Visitor = (*CaminoStandardTxExecutor)(nil)
	_ txs.Visitor = (*CaminoProposalTxExecutor)(nil)

	errNodeSignatureMissing              = errors.New("last signature is not nodeID's signature")
	errWrongLockMode                     = errors.New("this tx can't be used with this caminoGenesis.LockModeBondDeposit")
	errRecoverAddresses                  = errors.New("cannot recover addresses from credentials")
	errAddrStateNotPermitted             = errors.New("don't have permission to set address state bit")
	errValidatorExists                   = errors.New("node is already a validator")
	errInvalidSystemTxBody               = errors.New("tx body doesn't match expected one")
	errRemoveValidatorToEarly            = errors.New("attempting to remove validator before its end time")
	errRemoveWrongValidator              = errors.New("attempting to remove wrong validator")
	errDepositOfferInactive              = errors.New("deposit offer is inactive")
	errDepositTooSmall                   = errors.New("deposit amount is less than deposit offer minimum amount")
	errDepositTooBig                     = errors.New("deposit amount is greater than deposit offer available amount")
	errDepositDurationTooSmall           = errors.New("deposit duration is less than deposit offer minimum duration")
	errDepositDurationTooBig             = errors.New("deposit duration is greater than deposit offer maximum duration")
	errSupplyOverflow                    = errors.New("resulting total supply would be more than allowed maximum")
	errNotConsortiumMember               = errors.New("address isn't consortium member")
	errConsortiumMember                  = errors.New("address is consortium member")
	errValidatorNotFound                 = errors.New("validator not found")
	errConsortiumMemberHasNode           = errors.New("consortium member already has registered node")
	errSignatureMissing                  = errors.New("wrong signature")
	errNodeNotRegistered                 = errors.New("no address registered for this node")
	errNotNodeOwner                      = errors.New("node is registered for another address")
	errNodeAlreadyRegistered             = errors.New("node is already registered")
	errClaimableCredentialMismatch       = errors.New("claimable credential isn't matching")
	errDepositNotFound                   = errors.New("deposit not found")
	errWrongCredentialsNumber            = errors.New("unexpected number of credentials")
	errWrongOwnerType                    = errors.New("wrong owner type")
	errImportedUTXOMismatch              = errors.New("imported input doesn't match expected utxo")
	errInputAmountMismatch               = errors.New("utxo amount doesn't match input amount")
	errInputsUTXOSMismatch               = errors.New("number of inputs is different from number of utxos")
	errWrongClaimedAmount                = errors.New("claiming more than was available to claim")
	errNoUnlock                          = errors.New("no tokens unlocked")
	errAliasCredentialMismatch           = errors.New("alias credential isn't matching")
	errAliasNotFound                     = errors.New("alias not found on state")
	errUnlockedMoreThanAvailable         = errors.New("unlocked more deposited tokens than was available for unlock")
	errMixedDeposits                     = errors.New("tx has expired deposit input and active-deposit/unlocked input")
	errExpiredDepositNotFullyUnlocked    = errors.New("unlocked only part of expired deposit")
	errBurnedDepositUnlock               = errors.New("burned undeposited tokens")
	errAdminCannotBeDeleted              = errors.New("admin cannot be deleted")
	errNotAthensPhase                    = errors.New("not allowed before AthensPhase")
	errNotBerlinPhase                    = errors.New("not allowed before BerlinPhase")
	errBerlinPhase                       = errors.New("not allowed after BerlinPhase")
	errOfferCreatorCredentialMismatch    = errors.New("offer creator credential isn't matching")
	errNotOfferCreator                   = errors.New("address isn't allowed to create deposit offers")
	errDepositCreatorCredentialMismatch  = errors.New("deposit creator credential isn't matching")
	errOfferPermissionCredentialMismatch = errors.New("offer-usage permission credential isn't matching")
	errEmptyDepositCreatorAddress        = errors.New("empty deposit creator address, while offer owner isn't empty")
	errWrongTxUpgradeVersion             = errors.New("wrong tx upgrade version")
	errNestedMsigAlias                   = errors.New("nested msig aliases are not allowed")
	errEmptyAlias                        = errors.New("alias id and alias owners cannot be empty both at the same time")
	errProposalStartToEarly              = errors.New("proposal start time is to early")
	errProposalToFarInFuture             = fmt.Errorf("proposal start time is more than %s ahead of the current chain time", MaxFutureStartTime)
	ErrProposalInactive                  = errors.New("proposal is inactive")
	errProposerCredentialMismatch        = errors.New("proposer credential isn't matching")
	errWrongProposalBondAmount           = errors.New("wrong proposal bond amount")
	errVoterCredentialMismatch           = errors.New("voter credential isn't matching")
	errNotSuccessfulProposal             = errors.New("proposal is not successful")
	errSuccessfulProposal                = errors.New("proposal is successful")
	errNotEarlyFinishedProposal          = errors.New("proposal is not early finished")
	errEarlyFinishedProposal             = errors.New("proposal is early finished")
	errNotExpiredProposal                = errors.New("proposal is not expired")
	errExpiredProposal                   = errors.New("proposal is expired")
	errProposalsAreNotExpiredYet         = errors.New("proposals are not expired yet")
	errEarlyFinishedProposalsMismatch    = errors.New("early proposals mismatch")
	errExpiredProposalsMismatch          = errors.New("expired proposals mismatch")
	errWrongAdminProposal                = errors.New("this type of proposal can't be admin-proposal")
	errNotPermittedToCreateProposal      = errors.New("don't have permission to create proposal of this type")
	errZeroDepositOfferLimits            = errors.New("deposit offer TotalMaxAmount and TotalMaxRewardAmount are zero")

	ErrInvalidProposal = errors.New("proposal is semantically invalid")
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
	if len(e.Tx.Creds) < 2 {
		return errWrongCredentialsNumber
	}

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

	caminoAddValidatorTx, isCaminoTx := e.Tx.Unsigned.(*txs.CaminoAddValidatorTx)

	if !caminoConfig.LockModeBondDeposit && !isCaminoTx {
		return e.StandardTxExecutor.AddValidatorTx(tx)
	}

	if !caminoConfig.LockModeBondDeposit || !isCaminoTx {
		return errWrongLockMode
	}

	// verify camino tx

	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	if len(e.Tx.Creds) < 2 {
		return errWrongCredentialsNumber
	}

	// verify that node owned by consortium member

	consortiumMemberAddress, err := e.State.GetShortIDLink(
		ids.ShortID(tx.NodeID()),
		state.ShortLinkKeyRegisterNode,
	)
	if err != nil {
		return fmt.Errorf("%w: %s", errNodeNotRegistered, err)
	}

	if err := e.Backend.Fx.VerifyMultisigPermission(
		e.Tx.Unsigned,
		caminoAddValidatorTx.NodeOwnerAuth,
		e.Tx.Creds[len(e.Tx.Creds)-1], // consortium member cred
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{consortiumMemberAddress},
		},
		e.State,
	); err != nil {
		return fmt.Errorf("%w: %s", errSignatureMissing, err)
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

	if e.Backend.Bootstrapped.Get() {
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

		if err := e.Fx.VerifyMultisigOwner(rewardOwner, e.State); err != nil {
			return err
		}

		// Verify the flowcheck
		if err := e.Backend.FlowChecker.VerifyLock(
			tx,
			e.State,
			tx.Ins,
			tx.Outs,
			e.Tx.Creds[:len(e.Tx.Creds)-1],
			0,
			e.Backend.Config.AddPrimaryNetworkValidatorFee, // TODO@ use baseFee?
			e.Ctx.AVAXAssetID,
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
	avax.Consume(e.State, tx.Ins)
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

	return e.StandardTxExecutor.AddSubnetValidatorTx(tx) // TODO@ will use avax tx fee
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

	return e.StandardTxExecutor.AddDelegatorTx(tx) // TODO@ will use avax tx fee
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

	return e.StandardTxExecutor.AddPermissionlessValidatorTx(tx) // TODO@ will use avax tx fee
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

	return e.StandardTxExecutor.AddPermissionlessDelegatorTx(tx) // TODO@ will use avax tx fee
}

func (e *CaminoStandardTxExecutor) CreateChainTx(tx *txs.CreateChainTx) error {
	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}

	return e.StandardTxExecutor.CreateChainTx(tx) // TODO@ will use avax tx fee
}

func (e *CaminoStandardTxExecutor) CreateSubnetTx(tx *txs.CreateSubnetTx) error {
	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}

	return e.StandardTxExecutor.CreateSubnetTx(tx) // TODO@ will use avax tx fee
}

func (e *CaminoStandardTxExecutor) ExportTx(tx *txs.ExportTx) error {
	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}

	if err := locked.VerifyNoLocks(nil, tx.ExportedOutputs); err != nil {
		return err
	}

	if err := e.StandardTxExecutor.ExportTx(tx); err != nil {
		return err
	}

	if err := e.wrapAtomicElementsForMultisig(tx); err != nil {
		return err
	}

	return nil
}

func (e *CaminoStandardTxExecutor) wrapAtomicElementsForMultisig(tx *txs.ExportTx) error {
	putReqs := e.AtomicRequests[tx.DestinationChain].PutRequests
	if len(putReqs) != len(tx.ExportedOutputs) {
		return errors.New("exported outputs count doesn't match put requests")
	}

	for i, output := range tx.ExportedOutputs {
		owned, ok := output.Out.(secp256k1fx.Owned)
		if !ok {
			return locked.ErrWrongOutType
		}

		aliasIntfs, err := e.Fx.CollectMultisigAliases(owned.Owners(), e.State)
		if err != nil {
			return err
		}
		if len(aliasIntfs) == 0 {
			// no need to wrap current UTXO
			continue
		}

		req := putReqs[i]
		var utxo avax.UTXO
		_, err = txs.Codec.Unmarshal(req.Value, &utxo)
		if err != nil {
			return err
		}

		// wrap utxo with alias
		aliases := make([]verify.State, len(aliasIntfs))
		for i, inf := range aliasIntfs {
			ali, ok := inf.(*multisig.AliasWithNonce)
			if !ok {
				return errors.New("wrong type, expected multisig.AliasWithNonce")
			}
			aliases[i] = ali
		}

		wrappedUtxo := &avax.UTXOWithMSig{
			UTXO:    utxo,
			Aliases: aliases,
		}
		bytes, err := txs.Codec.Marshal(txs.Version, wrappedUtxo)
		if err != nil {
			return err
		}

		putReqs[i] = &atomic.Element{
			Key:    req.Key,
			Value:  bytes,
			Traits: req.Traits,
		}
	}

	return nil
}

func (e *CaminoStandardTxExecutor) ImportTx(tx *txs.ImportTx) error {
	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}
	if err := locked.VerifyNoLocks(tx.ImportedInputs, nil); err != nil {
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

	return e.StandardTxExecutor.TransformSubnetTx(tx) // TODO@ will use avax tx fee
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
		e.OnCommitState.SetAddressStates(nodeOwnerAddressOnCommit, nodeOwnerAddressStateOnCommit&^as.AddressStateNodeDeferred)

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
		e.OnAbortState.SetAddressStates(nodeOwnerAddressOnAbort, nodeOwnerAddressStateOnAbort&^as.AddressStateNodeDeferred)
	}

	txID := e.Tx.ID()

	avax.Consume(e.OnCommitState, caminoTx.Ins)
	avax.Consume(e.OnAbortState, caminoTx.Ins)
	avax.Produce(e.OnCommitState, txID, caminoTx.Outs)
	avax.Produce(e.OnAbortState, txID, caminoTx.Outs)

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

	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	depositOffer, err := e.State.GetDepositOffer(tx.DepositOfferID)
	if err != nil {
		return fmt.Errorf("can't get deposit offer: %w", err)
	}

	depositAmount := tx.DepositAmount()
	chainTime := e.State.GetTimestamp()
	athensPhase := e.Config.IsAthensPhaseActivated(chainTime)

	switch {
	case !depositOffer.IsActiveAt(uint64(chainTime.Unix())):
		return errDepositOfferInactive
	case tx.DepositDuration < depositOffer.MinDuration:
		return errDepositDurationTooSmall
	case tx.DepositDuration > depositOffer.MaxDuration:
		return errDepositDurationTooBig
	case depositAmount < depositOffer.MinAmount:
		return errDepositTooSmall
	case depositOffer.TotalMaxAmount > 0 && depositAmount > depositOffer.RemainingAmount():
		return errDepositTooBig
	case !athensPhase && depositOffer.TotalMaxRewardAmount > 0:
		return errNotAthensPhase
	}

	deposit := &deposits.Deposit{
		DepositOfferID: tx.DepositOfferID,
		Duration:       tx.DepositDuration,
		Amount:         depositAmount,
		Start:          uint64(chainTime.Unix()),
		RewardOwner:    tx.RewardsOwner,
	}
	potentialReward := deposit.TotalReward(depositOffer)

	if depositOffer.TotalMaxRewardAmount > 0 && potentialReward > depositOffer.RemainingReward() {
		return errDepositTooBig
	}

	baseTxCreds := e.Tx.Creds
	if depositOffer.OwnerAddress != ids.ShortEmpty {
		if !athensPhase {
			return errNotAthensPhase
		}

		if tx.UpgradeVersionID.Version() == 0 {
			return errWrongTxUpgradeVersion
		}

		if tx.DepositCreatorAddress == ids.ShortEmpty {
			return errEmptyDepositCreatorAddress
		}

		if len(e.Tx.Creds) < 3 {
			return errWrongCredentialsNumber
		}

		if err := e.Fx.VerifyMultisigMessage(
			depositOffer.PermissionMsg(tx.DepositCreatorAddress),
			tx.DepositOfferOwnerAuth,
			e.Tx.Creds[len(e.Tx.Creds)-1], // offer usage permission credential created by offer owner
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{depositOffer.OwnerAddress},
			},
			e.State,
		); err != nil {
			return fmt.Errorf("%w: %s", errOfferPermissionCredentialMismatch, err)
		}

		if err := e.Fx.VerifyMultisigPermission(
			tx,
			tx.DepositCreatorAuth,
			e.Tx.Creds[len(e.Tx.Creds)-2], // deposit creator credential
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{tx.DepositCreatorAddress},
			},
			e.State,
		); err != nil {
			return fmt.Errorf("%w: %s", errDepositCreatorCredentialMismatch, err)
		}

		baseTxCreds = e.Tx.Creds[:len(e.Tx.Creds)-2]
	}

	rewardOwner, ok := tx.RewardsOwner.(*secp256k1fx.OutputOwners)
	if !ok {
		return errWrongOwnerType
	}

	if err := e.Fx.VerifyMultisigOwner(rewardOwner, e.State); err != nil {
		return err
	}

	baseFee, err := e.State.GetBaseFee()
	if err != nil {
		return err
	}

	if err := e.FlowChecker.VerifyLock(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		baseTxCreds,
		0,
		baseFee,
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

	newSupply, err := math.Add64(currentSupply, potentialReward)
	if err != nil || newSupply > e.Config.RewardConfig.SupplyCap {
		return errSupplyOverflow
	}

	if depositOffer.TotalMaxAmount > 0 {
		updatedOffer := *depositOffer
		updatedOffer.DepositedAmount += depositAmount
		e.State.SetDepositOffer(&updatedOffer)
	} else if depositOffer.TotalMaxRewardAmount > 0 {
		updatedOffer := *depositOffer
		updatedOffer.RewardedAmount += potentialReward
		e.State.SetDepositOffer(&updatedOffer)
	}

	if newSupply != currentSupply {
		e.State.SetCurrentSupply(constants.PrimaryNetworkID, newSupply)
	}
	e.State.AddDeposit(txID, deposit)

	avax.Consume(e.State, tx.Ins)
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

	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	chainTimestamp := uint64(e.State.GetTimestamp().Unix())
	consumedDepositedAmounts := make(map[ids.ID]uint64)
	producedDepositedAmounts := make(map[ids.ID]uint64)
	hasExpiredDeposits := false
	hasActiveDepositsOrUnlockedIns := false
	consumed := uint64(0)

	for _, input := range tx.Ins {
		lockedIn, ok := input.In.(*locked.In)
		switch {
		case ok && lockedIn.DepositTxID != ids.Empty:
			if _, ok := consumedDepositedAmounts[lockedIn.DepositTxID]; !ok {
				deposit, err := e.State.GetDeposit(lockedIn.DepositTxID)
				if err != nil {
					return err
				}

				isExpired := deposit.IsExpired(chainTimestamp)

				if hasExpiredDeposits && !isExpired || hasActiveDepositsOrUnlockedIns && isExpired {
					return errMixedDeposits
				}

				hasExpiredDeposits = isExpired
				hasActiveDepositsOrUnlockedIns = !isExpired
			}

			consumedDepositedAmounts[lockedIn.DepositTxID], err = math.Add64(consumedDepositedAmounts[lockedIn.DepositTxID], lockedIn.Amount())
			if err != nil {
				return err
			}
		case !ok && hasExpiredDeposits:
			return errMixedDeposits
		case !ok:
			hasActiveDepositsOrUnlockedIns = true
		}

		consumed, err = math.Add64(consumed, input.In.Amount())
		if err != nil {
			return err
		}
	}

	produced := uint64(0)
	for _, output := range tx.Outs {
		if lockedOut, ok := output.Out.(*locked.Out); ok && lockedOut.DepositTxID != ids.Empty {
			producedDepositedAmounts[lockedOut.DepositTxID], err = math.Add64(producedDepositedAmounts[lockedOut.DepositTxID], lockedOut.Amount())
			if err != nil {
				return err
			}
		}
		produced, err = math.Add64(produced, output.Out.Amount())
		if err != nil {
			return err
		}
	}

	if hasExpiredDeposits && consumed != produced {
		return errBurnedDepositUnlock
	}

	amountToBurn := uint64(0)
	if !hasExpiredDeposits {
		baseFee, err := e.State.GetBaseFee()
		if err != nil {
			return err
		}
		amountToBurn = baseFee
	}

	if err := e.FlowChecker.VerifyUnlockDeposit(
		e.State,
		tx,
		tx.Ins,
		tx.Outs,
		e.Tx.Creds,
		amountToBurn,
		e.Ctx.AVAXAssetID,
		!hasExpiredDeposits,
	); err != nil {
		return fmt.Errorf("%w: %s", errFlowCheckFailed, err)
	}

	for depositTxID, consumedDepositedAmount := range consumedDepositedAmounts {
		deposit, err := e.State.GetDeposit(depositTxID)
		if err != nil {
			return err
		}

		unlockedAmount := consumedDepositedAmount - producedDepositedAmounts[depositTxID]
		newTotalUnlockedAmount, err := math.Add64(unlockedAmount, deposit.UnlockedAmount)
		if err != nil {
			return err
		}

		if newTotalUnlockedAmount == deposit.UnlockedAmount {
			return errNoUnlock
		}

		if deposit.IsExpired(chainTimestamp) {
			if newTotalUnlockedAmount != deposit.Amount {
				return errExpiredDepositNotFullyUnlocked
			}

			offer, err := e.State.GetDepositOffer(deposit.DepositOfferID)
			if err != nil {
				return err
			}

			if remainingReward := deposit.TotalReward(offer) - deposit.ClaimedRewardAmount; remainingReward > 0 {
				claimableOwnerID, err := txs.GetOwnerID(deposit.RewardOwner)
				if err != nil {
					return err
				}

				claimable, err := e.State.GetClaimable(claimableOwnerID)
				if err == database.ErrNotFound {
					secpOwner, ok := deposit.RewardOwner.(*secp256k1fx.OutputOwners)
					if !ok {
						return errWrongOwnerType
					}
					claimable = &state.Claimable{
						Owner: secpOwner,
					}
				} else if err != nil {
					return err
				}

				newClaimable := &state.Claimable{
					Owner:           claimable.Owner,
					ValidatorReward: claimable.ValidatorReward,
				}

				newClaimable.ExpiredDepositReward, err = math.Add64(claimable.ExpiredDepositReward, remainingReward)
				if err != nil {
					return err
				}

				e.State.SetClaimable(claimableOwnerID, newClaimable)
			}
			e.State.RemoveDeposit(depositTxID, deposit)
		} else {
			offer, err := e.State.GetDepositOffer(deposit.DepositOfferID)
			if err != nil {
				return err
			}

			if unlockableAmount := deposit.UnlockableAmount(offer, chainTimestamp); unlockableAmount < newTotalUnlockedAmount {
				return errUnlockedMoreThanAvailable
			}

			e.State.ModifyDeposit(depositTxID, &deposits.Deposit{
				DepositOfferID:      deposit.DepositOfferID,
				UnlockedAmount:      newTotalUnlockedAmount,
				ClaimedRewardAmount: deposit.ClaimedRewardAmount,
				Amount:              deposit.Amount,
				Start:               deposit.Start,
				Duration:            deposit.Duration,
				RewardOwner:         deposit.RewardOwner,
			})
		}
	}

	avax.Consume(e.State, tx.Ins)
	avax.Produce(e.State, e.Tx.ID(), tx.Outs)

	return nil
}

func (e *CaminoStandardTxExecutor) ClaimTx(tx *txs.ClaimTx) error {
	// Basic checks

	caminoConfig, err := e.State.CaminoConfig()
	if err != nil {
		return fmt.Errorf("couldn't get camino config: %w", err)
	}

	if !caminoConfig.LockModeBondDeposit {
		return errWrongLockMode
	}

	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	// Common vars

	currentTimestamp := uint64(e.State.GetTimestamp().Unix())
	txID := e.Tx.ID()
	claimedAmount := uint64(0)
	claimableCreds := e.Tx.Creds[len(e.Tx.Creds)-len(tx.Claimables):]

	for i, txClaimable := range tx.Claimables {
		switch txClaimable.Type {
		case txs.ClaimTypeActiveDepositReward:
			// Checking deposit signatures

			deposit, err := e.State.GetDeposit(txClaimable.ID)
			if err != nil {
				return fmt.Errorf("%w: %s", errDepositNotFound, err)
			}

			if err := e.Fx.VerifyMultisigPermission(
				tx,
				txClaimable.OwnerAuth,
				claimableCreds[i],
				deposit.RewardOwner,
				e.State,
			); err != nil {
				return fmt.Errorf("%w: %s", errClaimableCredentialMismatch, err)
			}

			// Checking claimed amount

			depositOffer, err := e.State.GetDepositOffer(deposit.DepositOfferID)
			if err != nil {
				return fmt.Errorf("couldn't get deposit offer: %w", err)
			}

			claimableReward := deposit.ClaimableReward(depositOffer, currentTimestamp)
			if claimableReward < txClaimable.Amount {
				return errWrongClaimedAmount
			}

			claimedAmount, err = math.Add64(claimedAmount, txClaimable.Amount)
			if err != nil {
				return fmt.Errorf("couldn't calculate total claimedAmount: %w", err)
			}

			// Updating deposit

			e.State.ModifyDeposit(txClaimable.ID, &deposits.Deposit{
				DepositOfferID:      deposit.DepositOfferID,
				UnlockedAmount:      deposit.UnlockedAmount,
				ClaimedRewardAmount: deposit.ClaimedRewardAmount + txClaimable.Amount,
				Start:               deposit.Start,
				Duration:            deposit.Duration,
				Amount:              deposit.Amount,
				RewardOwner:         deposit.RewardOwner,
			})

		case txs.ClaimTypeExpiredDepositReward, txs.ClaimTypeValidatorReward, txs.ClaimTypeAllTreasury:
			// Checking claimables signatures

			treasuryClaimable, err := e.State.GetClaimable(txClaimable.ID)
			if err == database.ErrNotFound {
				// tx.ClaimedAmount[i] > 0, so we'r trying to claim more, than available
				return fmt.Errorf("no claimable found for the ownerID (%s): %w", txClaimable.ID, errWrongClaimedAmount)
			} else if err != nil {
				return fmt.Errorf("couldn't get claimable: %w", err)
			}

			if err := e.Fx.VerifyMultisigPermission(
				tx,
				txClaimable.OwnerAuth,
				claimableCreds[i],
				treasuryClaimable.Owner,
				e.State,
			); err != nil {
				return fmt.Errorf("%w: %s", errClaimableCredentialMismatch, err)
			}

			// Checking claimed amount

			amountToClaim := txClaimable.Amount
			newClaimableValidatorReward := treasuryClaimable.ValidatorReward
			newClaimableExpiredDepositReward := treasuryClaimable.ExpiredDepositReward

			if txClaimable.Type == txs.ClaimTypeValidatorReward || txClaimable.Type == txs.ClaimTypeAllTreasury {
				if amountToClaim > newClaimableValidatorReward {
					amountToClaim -= newClaimableValidatorReward
					newClaimableValidatorReward = 0
				} else {
					newClaimableValidatorReward -= amountToClaim
					amountToClaim = 0
				}
			}

			if txClaimable.Type == txs.ClaimTypeExpiredDepositReward || txClaimable.Type == txs.ClaimTypeAllTreasury {
				if amountToClaim > newClaimableExpiredDepositReward {
					amountToClaim -= newClaimableExpiredDepositReward
					newClaimableExpiredDepositReward = 0
				} else {
					newClaimableExpiredDepositReward -= amountToClaim
					amountToClaim = 0
				}
			}

			if amountToClaim > 0 {
				return errWrongClaimedAmount
			}

			claimedAmount, err = math.Add64(claimedAmount, txClaimable.Amount)
			if err != nil {
				return fmt.Errorf("couldn't calculate total claimedAmount: %w", err)
			}

			// Updating claimable

			var newClaimable *state.Claimable
			if newClaimableExpiredDepositReward != 0 || newClaimableValidatorReward != 0 {
				newClaimable = &state.Claimable{
					Owner:                treasuryClaimable.Owner,
					ValidatorReward:      newClaimableValidatorReward,
					ExpiredDepositReward: newClaimableExpiredDepositReward,
				}
			}
			e.State.SetClaimable(txClaimable.ID, newClaimable)
		}
	}

	// BaseTx check (fee, reward outs)
	baseFee, err := e.State.GetBaseFee()
	if err != nil {
		return err
	}

	if err := e.FlowChecker.VerifyLock(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		e.Tx.Creds[:len(e.Tx.Creds)-len(tx.Claimables)],
		claimedAmount,
		baseFee,
		e.Ctx.AVAXAssetID,
		locked.StateUnlocked,
	); err != nil {
		return fmt.Errorf("%w: %s", errFlowCheckFailed, err)
	}

	avax.Consume(e.State, tx.Ins)
	avax.Produce(e.State, txID, tx.Outs)

	return nil
}

func (e *CaminoStandardTxExecutor) RegisterNodeTx(tx *txs.RegisterNodeTx) error {
	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	// verify consortium member state

	consortiumMemberAddressState, err := e.State.GetAddressStates(tx.NodeOwnerAddress)
	if err != nil {
		return err
	}

	if consortiumMemberAddressState.IsNot(as.AddressStateConsortium) {
		return errNotConsortiumMember
	}

	newNodeIDEmpty := tx.NewNodeID == ids.EmptyNodeID
	oldNodeIDEmpty := tx.OldNodeID == ids.EmptyNodeID

	linkedNodeID, err := e.State.GetShortIDLink(tx.NodeOwnerAddress, state.ShortLinkKeyRegisterNode)
	hasLinkedNode := err != database.ErrNotFound
	if hasLinkedNode && err != nil {
		return err
	}

	if oldNodeIDEmpty {
		if hasLinkedNode {
			return errConsortiumMemberHasNode
		}
		// Verify that the node is not already registered
		if _, err := e.State.GetShortIDLink(ids.ShortID(tx.NewNodeID), state.ShortLinkKeyRegisterNode); err == nil {
			return errNodeAlreadyRegistered
		} else if err != database.ErrNotFound {
			return err
		}
	}

	// verify consortium member cred
	if err := e.Backend.Fx.VerifyMultisigPermission(
		e.Tx.Unsigned,
		tx.NodeOwnerAuth,
		e.Tx.Creds[len(e.Tx.Creds)-1], // consortium member cred
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{tx.NodeOwnerAddress},
		},
		e.State,
	); err != nil {
		return fmt.Errorf("%w: %s", errSignatureMissing, err)
	}

	// verify old nodeID ownership

	if !oldNodeIDEmpty && (!hasLinkedNode || tx.OldNodeID != ids.NodeID(linkedNodeID)) {
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
	baseFee, err := e.State.GetBaseFee()
	if err != nil {
		return err
	}

	if err := e.FlowChecker.VerifyLock(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		e.Tx.Creds[:len(e.Tx.Creds)-2], // base tx creds
		0,
		baseFee,
		e.Ctx.AVAXAssetID,
		locked.StateUnlocked,
	); err != nil {
		return err
	}

	// update state

	txID := e.Tx.ID()

	// Consume the UTXOS
	avax.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	avax.Produce(e.State, txID, tx.Outs)

	if !oldNodeIDEmpty {
		e.State.SetShortIDLink(ids.ShortID(tx.OldNodeID), state.ShortLinkKeyRegisterNode, nil)
		e.State.SetShortIDLink(tx.NodeOwnerAddress, state.ShortLinkKeyRegisterNode, nil)
	}

	if !newNodeIDEmpty {
		e.State.SetShortIDLink(ids.ShortID(tx.NewNodeID),
			state.ShortLinkKeyRegisterNode,
			&tx.NodeOwnerAddress,
		)
		link := ids.ShortID(tx.NewNodeID)
		e.State.SetShortIDLink(tx.NodeOwnerAddress,
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

	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	chainTime := e.State.GetTimestamp()

	if e.Bootstrapped.Get() {
		// Getting all treasury utxos exported from c-chain, collecting ones that are old enough

		allUTXOBytes, _, _, err := e.Ctx.SharedMemory.Indexed(
			e.Ctx.CChainID,
			treasury.AddrTraitsBytes,
			ids.ShortEmpty[:], ids.Empty[:], maxPageSize,
		)
		if err != nil {
			return fmt.Errorf("error fetching atomic UTXOs: %w", err)
		}

		chainTimestamp := uint64(chainTime.Unix())

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

		avax.SortTransferableUTXOs(utxos)

		for i, in := range tx.Ins {
			utxo := utxos[i]

			if utxo.InputID() != in.InputID() {
				return errImportedUTXOMismatch
			}

			out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
			if !ok {
				// should never happen
				return locked.ErrWrongOutType
			}

			if out.Amt != in.In.Amount() {
				return fmt.Errorf("utxo.Amt %d, input.Amt %d: %w", out.Amt, in.In.Amount(), errInputAmountMismatch)
			}
		}
	}

	// Getting active validators

	currentStakerIterator, err := e.State.GetCurrentStakerIterator()
	if err != nil {
		return err
	}
	defer currentStakerIterator.Release()

	totalRewardFractions := uint64(0)
	type reward struct {
		owner     *secp256k1fx.OutputOwners
		fractions uint64
	}
	rewardOwners := map[ids.ID]*reward{}
	for currentStakerIterator.Next() {
		staker := currentStakerIterator.Value()
		if staker.SubnetID != constants.PrimaryNetworkID {
			continue
		}

		var txRewardOwner *secp256k1fx.OutputOwners
		if e.Config.IsAthensPhaseActivated(chainTime) {
			addValidatorTx, _, err := e.State.GetTx(staker.TxID)
			if err != nil {
				return err
			}
			unsignedAddValidatorTx, ok := addValidatorTx.Unsigned.(*txs.CaminoAddValidatorTx)
			if !ok {
				return errWrongTxType
			}
			txRewardOwner, ok = unsignedAddValidatorTx.RewardsOwner.(*secp256k1fx.OutputOwners)
			if !ok {
				return errWrongOwnerType
			}
		} else {
			validatorAddr, err := e.State.GetShortIDLink(
				ids.ShortID(staker.NodeID),
				state.ShortLinkKeyRegisterNode,
			)
			if err != nil {
				return err
			}
			txRewardOwner = &secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{validatorAddr},
			}
		}

		ownerID, err := txs.GetOwnerID(txRewardOwner)
		if err != nil {
			return err
		}
		rewardOwner, ok := rewardOwners[ownerID]
		if !ok {
			rewardOwner = &reward{
				owner: txRewardOwner,
			}
			rewardOwners[ownerID] = rewardOwner
		}
		rewardOwner.fractions++
		totalRewardFractions++
	}

	// Set not distributed validator reward

	notDistributedAmount, err := e.State.GetNotDistributedValidatorReward()
	if err != nil {
		return err
	}

	importedAmount := uint64(0)
	for _, in := range tx.Ins {
		importedAmount, err = math.Add64(importedAmount, in.In.Amount())
		if err != nil {
			return err
		}
	}

	amountToDistribute, err := math.Add64(importedAmount, notDistributedAmount)
	if err != nil {
		return err
	}

	addedReward := amountToDistribute / totalRewardFractions
	newNotDistributedAmount := amountToDistribute - addedReward*totalRewardFractions

	if newNotDistributedAmount != notDistributedAmount {
		e.State.SetNotDistributedValidatorReward(newNotDistributedAmount)
	}

	// Set claimables

	if addedReward != 0 {
		for rewardOwnerID, reward := range rewardOwners {
			claimable, err := e.State.GetClaimable(rewardOwnerID)
			if err != nil && err != database.ErrNotFound {
				return err
			}

			newClaimable := &state.Claimable{
				Owner: reward.owner,
			}
			if claimable != nil {
				newClaimable.ValidatorReward = claimable.ValidatorReward
				newClaimable.ExpiredDepositReward = claimable.ExpiredDepositReward
			}

			rewardAddedToOwner, err := math.Mul64(addedReward, reward.fractions)
			if err != nil {
				return err
			}

			newClaimable.ValidatorReward, err = math.Add64(newClaimable.ValidatorReward, rewardAddedToOwner)
			if err != nil {
				return err
			}

			e.State.SetClaimable(rewardOwnerID, newClaimable)
		}
	}

	// Atomic request

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

func (e *CaminoStandardTxExecutor) BaseTx(tx *txs.BaseTx) error {
	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}

	if e.Bootstrapped.Get() {
		baseFee, err := e.State.GetBaseFee()
		if err != nil {
			return err
		}

		if err := e.FlowChecker.VerifyLock(
			tx,
			e.State,
			tx.Ins,
			tx.Outs,
			e.Tx.Creds,
			0,
			baseFee,
			e.Ctx.AVAXAssetID,
			locked.StateUnlocked,
		); err != nil {
			return fmt.Errorf("%w: %s", errFlowCheckFailed, err)
		}
	}

	avax.Consume(e.State, tx.Ins)
	avax.Produce(e.State, e.Tx.ID(), tx.Outs)

	return nil
}

func (e *CaminoStandardTxExecutor) MultisigAliasTx(tx *txs.MultisigAliasTx) error {
	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	isRemoval := tx.MultisigAlias.Owners.IsZero()
	isUpdating := tx.MultisigAlias.ID != ids.ShortEmpty
	isBerlin := e.Config.IsBerlinPhaseActivated(e.State.GetTimestamp())

	if isBerlin {
		// TODO @evlekht if we won't have any empty aliases after Berlin, we can move this to alias syntactic verification
		// verify that alias isn't empty
		// syntactically valid multisig alias ensures that is removal is also updating
		if isRemoval && !isUpdating {
			return errEmptyAlias
		}

		// verify that alias isn't nesting another alias
		isNestedMsig, err := e.Fx.IsNestedMultisig(tx.MultisigAlias.Owners, e.State)
		switch {
		case err != nil:
			return err
		case isNestedMsig:
			return errNestedMsigAlias
		}
	}

	aliasAddrState := as.AddressStateEmpty
	baseCreds := e.Tx.Creds[:len(e.Tx.Creds)]
	var aliasID ids.ShortID
	nonce := uint64(0)
	txID := e.Tx.ID()

	if isUpdating {
		if len(e.Tx.Creds) < 2 {
			return errWrongCredentialsNumber
		}

		oldAlias, err := e.State.GetMultisigAlias(tx.MultisigAlias.ID)
		if err != nil {
			return fmt.Errorf("%w, alias: %s", errAliasNotFound, tx.MultisigAlias.ID)
		}

		baseCreds = e.Tx.Creds[:len(e.Tx.Creds)-1]

		if err := e.Backend.Fx.VerifyMultisigPermission(
			e.Tx.Unsigned,
			tx.Auth,
			e.Tx.Creds[len(e.Tx.Creds)-1],
			oldAlias.Owners,
			e.State,
		); err != nil {
			return fmt.Errorf("%w: %s", errAliasCredentialMismatch, err)
		}

		if isRemoval {
			// verify that alias isn't consortium member or role admin
			aliasAddrState, err = e.State.GetAddressStates(tx.MultisigAlias.ID)
			switch {
			case err != nil:
				return err
			case aliasAddrState.Is(as.AddressStateConsortium):
				return errConsortiumMember
			case aliasAddrState.Is(as.AddressStateRoleAdmin):
				return errAdminCannotBeDeleted
			}
		} else {
			nonce = oldAlias.Nonce + 1
		}

		aliasID = oldAlias.ID
	} else {
		aliasID = multisig.ComputeAliasID(txID)
	}

	// verify the flowcheck
	baseFee, err := e.State.GetBaseFee()
	if err != nil {
		return err
	}

	if err := e.FlowChecker.VerifyLock(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		baseCreds,
		0,
		baseFee,
		e.Ctx.AVAXAssetID,
		locked.StateUnlocked,
	); err != nil {
		return fmt.Errorf("%w: %s", errFlowCheckFailed, err)
	}

	// update state

	var msigAlias *multisig.AliasWithNonce
	if !isBerlin {
		// we need to preserve pre-berlin logic for state consistency
		msigAlias = &multisig.AliasWithNonce{
			Alias: multisig.Alias{
				ID: aliasID,
				Owners: &secp256k1fx.OutputOwners{
					Addrs: []ids.ShortID{},
				},
			},
		}
	}
	if isRemoval && aliasAddrState != as.AddressStateEmpty {
		e.State.SetAddressStates(tx.MultisigAlias.ID, as.AddressStateEmpty)
	} else if !isRemoval {
		msigAlias = &multisig.AliasWithNonce{
			Alias: multisig.Alias{
				ID:     aliasID,
				Memo:   tx.MultisigAlias.Memo,
				Owners: tx.MultisigAlias.Owners,
			},
			Nonce: nonce,
		}
	}

	e.State.SetMultisigAlias(aliasID, msigAlias)

	// Consume the UTXOS
	avax.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	avax.Produce(e.State, txID, tx.Outs)

	return nil
}

func (e *CaminoStandardTxExecutor) AddDepositOfferTx(tx *txs.AddDepositOfferTx) error {
	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	chainTime := e.State.GetTimestamp()

	if !e.Config.IsAthensPhaseActivated(chainTime) {
		return errNotAthensPhase
	}

	if e.Config.IsBerlinPhaseActivated(chainTime) &&
		tx.DepositOffer.TotalMaxAmount == 0 && tx.DepositOffer.TotalMaxRewardAmount == 0 {
		return errZeroDepositOfferLimits
	}

	if len(e.Tx.Creds) < 2 {
		return errWrongCredentialsNumber
	}

	// verify the flowcheck
	baseFee, err := e.State.GetBaseFee()
	if err != nil {
		return err
	}

	if err := e.FlowChecker.VerifyLock(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		e.Tx.Creds[:len(e.Tx.Creds)-1], // base tx credentials
		0,
		baseFee,
		e.Ctx.AVAXAssetID,
		locked.StateUnlocked,
	); err != nil {
		return fmt.Errorf("%w: %s", errFlowCheckFailed, err)
	}

	// check role

	depositOfferCreatorAddressState, err := e.State.GetAddressStates(tx.DepositOfferCreatorAddress)
	if err != nil {
		return err
	}

	if depositOfferCreatorAddressState.IsNot(as.AddressStateOffersCreator) {
		return errNotOfferCreator
	}

	if err := e.Backend.Fx.VerifyMultisigPermission(
		e.Tx.Unsigned,
		tx.DepositOfferCreatorAuth,
		e.Tx.Creds[len(e.Tx.Creds)-1], // offer creator credential
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{tx.DepositOfferCreatorAddress},
		},
		e.State,
	); err != nil {
		return fmt.Errorf("%w: %s", errOfferCreatorCredentialMismatch, err)
	}

	// validate offer

	currentSupply, err := e.State.GetCurrentSupply(constants.PrimaryNetworkID)
	if err != nil {
		return err
	}

	allOffers, err := e.State.GetAllDepositOffers()
	if err != nil {
		return err
	}

	availableSupply := e.Config.RewardConfig.SupplyCap - currentSupply
	chainTimestamp := uint64(chainTime.Unix())

	for _, offer := range allOffers {
		if chainTimestamp <= offer.End && offer.Flags&deposits.OfferFlagLocked == 0 {
			if offer.TotalMaxAmount != 0 {
				availableSupply -= offer.MaxRemainingRewardByTotalMaxAmount()
			} else if offer.TotalMaxRewardAmount != 0 {
				availableSupply -= offer.RemainingReward()
			}
		}
	}

	if tx.DepositOffer.TotalMaxRewardAmount > availableSupply {
		return errSupplyOverflow
	}

	// update state

	txID := e.Tx.ID()

	tx.DepositOffer.ID = txID
	e.State.SetDepositOffer(tx.DepositOffer)

	avax.Consume(e.State, tx.Ins)
	avax.Produce(e.State, txID, tx.Outs)

	return nil
}

func (e *CaminoStandardTxExecutor) AddProposalTx(tx *txs.AddProposalTx) error {
	caminoConfig, err := e.State.CaminoConfig()
	if err != nil {
		return err
	}

	if !caminoConfig.LockModeBondDeposit {
		return errWrongLockMode
	}

	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	chainTime := e.State.GetTimestamp()

	if !e.Config.IsBerlinPhaseActivated(chainTime) {
		return errNotBerlinPhase
	}

	txProposal, err := tx.Proposal()
	if err != nil {
		return err
	}

	adminProposal, isAdminProposal := txProposal.(*dacProposals.AdminProposal)
	adminProposerAddressState := txProposal.AdminProposer()

	// verify proposal and proposer credential

	switch {
	case tx.BondAmount() != e.Config.CaminoConfig.DACProposalBondAmount:
		return errWrongProposalBondAmount
	case txProposal.StartTime().Before(chainTime):
		return errProposalStartToEarly
	case txProposal.StartTime().After(chainTime.Add(MaxFutureStartTime)):
		return errProposalToFarInFuture
	case len(e.Tx.Creds) < 2:
		return errWrongCredentialsNumber
	case isAdminProposal && adminProposerAddressState == as.AddressStateEmpty:
		return errWrongAdminProposal
	}

	if isAdminProposal {
		addrState, err := e.State.GetAddressStates(tx.ProposerAddress)
		if err != nil {
			return err
		}
		if addrState.IsNot(adminProposerAddressState) {
			return errNotPermittedToCreateProposal
		}
	}

	if err := e.Fx.VerifyMultisigPermission(
		tx,
		tx.ProposerAuth,
		e.Tx.Creds[len(e.Tx.Creds)-1], // proposer credential
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{tx.ProposerAddress},
		},
		e.State,
	); err != nil {
		return fmt.Errorf("%w: %s", errProposerCredentialMismatch, err)
	}

	if err := txProposal.VerifyWith(dac.NewProposalVerifier(e.Config, e.State, tx, isAdminProposal)); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidProposal, err)
	}

	// verify the flowcheck

	lockState := locked.StateBonded
	baseFee, err := e.State.GetBaseFee()
	if err != nil {
		return err
	}

	if err := e.FlowChecker.VerifyLock(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		e.Tx.Creds[:len(e.Tx.Creds)-1], // base tx creds
		0,
		baseFee,
		e.Ctx.AVAXAssetID,
		lockState,
	); err != nil {
		return fmt.Errorf("%w: %s", errFlowCheckFailed, err)
	}

	// Get allowed voters and create proposalState

	txID := e.Tx.ID()
	allowedVoters := []ids.ShortID{}
	var proposalState dacProposals.ProposalState

	if isAdminProposal {
		proposalState, err = txProposal.CreateFinishedProposalState(adminProposal.OptionIndex)
		if err != nil {
			// should never happen
			// could error only if proposal can't be admin proposal
			// but it was checked above
			return err
		}
		// its admin proposal, must be finished and executed
		e.State.AddProposalIDToFinish(txID)
	} else {
		// Getting active validators
		// Only validators who were active when the proposal was created can vote
		currentStakerIterator, err := e.State.GetCurrentStakerIterator()
		if err != nil {
			return err
		}
		defer currentStakerIterator.Release()

		for currentStakerIterator.Next() {
			staker := currentStakerIterator.Value()
			if staker.SubnetID != constants.PrimaryNetworkID {
				continue
			}

			consortiumMemberAddress, err := e.State.GetShortIDLink(ids.ShortID(staker.NodeID), state.ShortLinkKeyRegisterNode)
			if err != nil {
				return err
			}

			desiredPos, _ := slices.BinarySearchFunc(allowedVoters, consortiumMemberAddress, func(id, other ids.ShortID) int {
				return bytes.Compare(id[:], other[:])
			})
			allowedVoters = append(allowedVoters, consortiumMemberAddress)
			if desiredPos < len(allowedVoters)-1 {
				copy(allowedVoters[desiredPos+1:], allowedVoters[desiredPos:])
				allowedVoters[desiredPos] = consortiumMemberAddress
			}
		}
		proposalState = txProposal.CreateProposalState(allowedVoters)
	}

	// update state

	e.State.AddProposal(txID, proposalState)
	avax.Consume(e.State, tx.Ins)
	return utxo.ProduceLocked(e.State, txID, tx.Outs, locked.StateBonded)
}

func (e *CaminoStandardTxExecutor) AddVoteTx(tx *txs.AddVoteTx) error {
	caminoConfig, err := e.State.CaminoConfig()
	if err != nil {
		return err
	}

	if !caminoConfig.LockModeBondDeposit {
		return errWrongLockMode
	}

	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	chainTime := e.State.GetTimestamp()

	if !e.Config.IsBerlinPhaseActivated(chainTime) {
		return errNotBerlinPhase
	}

	// verify vote with proposal

	proposal, err := e.State.GetProposal(tx.ProposalID)
	if err != nil {
		return err
	}

	if !proposal.IsActiveAt(chainTime) {
		return ErrProposalInactive // should never happen, cause inactive proposals are removed from state
	}

	// verify voter credential and address state (role)

	voterAddressState, err := e.State.GetAddressStates(tx.VoterAddress)
	if err != nil {
		return err
	}

	if voterAddressState.IsNot(as.AddressStateConsortium) {
		return errNotConsortiumMember
	}

	if len(e.Tx.Creds) < 2 {
		return errWrongCredentialsNumber
	}

	if err := e.Backend.Fx.VerifyMultisigPermission(
		e.Tx.Unsigned,
		tx.VoterAuth,
		e.Tx.Creds[len(e.Tx.Creds)-1], // voter credential
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{tx.VoterAddress},
		},
		e.State,
	); err != nil {
		return fmt.Errorf("%w: %s", errVoterCredentialMismatch, err)
	}

	// verify the flowcheck

	baseFee, err := e.State.GetBaseFee()
	if err != nil {
		return err
	}

	if err := e.FlowChecker.VerifyLock(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		e.Tx.Creds[:len(e.Tx.Creds)-1], // base tx creds
		0,
		baseFee,
		e.Ctx.AVAXAssetID,
		locked.StateUnlocked,
	); err != nil {
		return fmt.Errorf("%w: %s", errFlowCheckFailed, err)
	}

	// update state

	vote, err := tx.Vote()
	if err != nil {
		return err
	}

	updatedProposal, err := proposal.AddVote(tx.VoterAddress, vote)
	if err != nil {
		return err
	}
	e.State.ModifyProposal(tx.ProposalID, updatedProposal)

	// If proposal became finishable, it cannot be reverted by future votes, even if they'll be accepted.
	if updatedProposal.CanBeFinished() {
		e.State.AddProposalIDToFinish(tx.ProposalID)
	}

	avax.Consume(e.State, tx.Ins)
	avax.Produce(e.State, e.Tx.ID(), tx.Outs)

	return nil
}

func (e *CaminoStandardTxExecutor) FinishProposalsTx(tx *txs.FinishProposalsTx) error {
	caminoConfig, err := e.State.CaminoConfig()
	if err != nil {
		return err
	}

	if !caminoConfig.LockModeBondDeposit {
		return errWrongLockMode
	}

	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	// basic checks

	chainTime := e.State.GetTimestamp()
	nextToExpireProposalIDs, expirationTime, err := e.State.GetNextToExpireProposalIDsAndTime(nil)
	if err != nil {
		return err
	}
	proposalIDsToFinish, err := e.State.GetProposalIDsToFinish()
	if err != nil {
		return err
	}
	isExpirationTime := expirationTime.Equal(chainTime)

	switch {
	case len(e.Tx.Creds) != 0:
		return errWrongCredentialsNumber
	case !e.Config.IsBerlinPhaseActivated(chainTime):
		return errNotBerlinPhase
	case !isExpirationTime &&
		len(tx.ExpiredSuccessfulProposalIDs)+len(tx.ExpiredFailedProposalIDs) != 0:
		return errProposalsAreNotExpiredYet
	case isExpirationTime &&
		len(tx.ExpiredSuccessfulProposalIDs)+len(tx.ExpiredFailedProposalIDs) != len(nextToExpireProposalIDs):
		return errExpiredProposalsMismatch
	case len(tx.EarlyFinishedSuccessfulProposalIDs)+len(tx.EarlyFinishedFailedProposalIDs) != len(proposalIDsToFinish):
		return errEarlyFinishedProposalsMismatch
	}

	// verify ins and outs

	lockTxIDs, err := dac.GetBondTxIDs(e.State, tx)
	if err != nil {
		return err
	}

	expectedIns, expectedOuts, err := e.FlowChecker.Unlock(
		e.State,
		lockTxIDs,
		locked.StateBonded,
	)
	if err != nil {
		return err
	}

	// TODO @evlekht change rewardValidator tx in the same manner

	if !inputsAreEqual(tx.Ins, expectedIns) {
		return fmt.Errorf("%w: invalid inputs", errInvalidSystemTxBody)
	}

	if !outputsAreEqual(tx.Outs, expectedOuts) {
		return fmt.Errorf("%w: invalid outputs", errInvalidSystemTxBody)
	}

	// getting early finished and expired proposal IDs to check that they match tx proposal IDs

	proposalIDsToFinishSet := set.NewSet[ids.ID](len(proposalIDsToFinish))
	for _, proposalID := range proposalIDsToFinish {
		proposalIDsToFinishSet.Add(proposalID)
	}

	nextToExpireProposalIDsSet := set.NewSet[ids.ID](len(nextToExpireProposalIDs))
	if isExpirationTime {
		for _, proposalID := range nextToExpireProposalIDs {
			nextToExpireProposalIDsSet.Add(proposalID)
		}
	}

	// processing tx proposal IDs

	for _, proposalID := range tx.EarlyFinishedSuccessfulProposalIDs {
		proposal, err := e.State.GetProposal(proposalID)
		if err != nil {
			return err
		}

		if !proposal.IsSuccessful() {
			return errNotSuccessfulProposal
		}

		if !proposalIDsToFinishSet.Contains(proposalID) {
			return errNotEarlyFinishedProposal
		}

		if nextToExpireProposalIDsSet.Contains(proposalID) {
			return errExpiredProposal
		}

		// try to execute proposal
		if err := proposal.ExecuteWith(dac.NewProposalExecutor(e.State)); err != nil {
			return err
		}

		e.State.RemoveProposal(proposalID, proposal)
		e.State.RemoveProposalIDToFinish(proposalID)
		proposalIDsToFinishSet.Remove(proposalID)
	}

	for _, proposalID := range tx.EarlyFinishedFailedProposalIDs {
		proposal, err := e.State.GetProposal(proposalID)
		if err != nil {
			return err
		}

		if proposal.IsSuccessful() {
			return errSuccessfulProposal
		}

		if !proposalIDsToFinishSet.Contains(proposalID) {
			return errNotEarlyFinishedProposal
		}

		if nextToExpireProposalIDsSet.Contains(proposalID) {
			return errExpiredProposal
		}

		e.State.RemoveProposal(proposalID, proposal)
		e.State.RemoveProposalIDToFinish(proposalID)
		proposalIDsToFinishSet.Remove(proposalID)
	}

	for _, proposalID := range tx.ExpiredSuccessfulProposalIDs {
		proposal, err := e.State.GetProposal(proposalID)
		if err != nil {
			return err
		}

		if !proposal.IsSuccessful() {
			return errNotSuccessfulProposal
		}

		if proposalIDsToFinishSet.Contains(proposalID) {
			return errEarlyFinishedProposal
		}

		if !nextToExpireProposalIDsSet.Contains(proposalID) {
			return errNotExpiredProposal
		}

		// try to execute proposal
		if err := proposal.ExecuteWith(dac.NewProposalExecutor(e.State)); err != nil {
			return err
		}

		e.State.RemoveProposal(proposalID, proposal)
		nextToExpireProposalIDsSet.Remove(proposalID)
	}

	for _, proposalID := range tx.ExpiredFailedProposalIDs {
		proposal, err := e.State.GetProposal(proposalID)
		if err != nil {
			return err
		}

		if proposal.IsSuccessful() {
			return errSuccessfulProposal
		}

		if proposalIDsToFinishSet.Contains(proposalID) {
			return errEarlyFinishedProposal
		}

		if !nextToExpireProposalIDsSet.Contains(proposalID) {
			return errNotExpiredProposal
		}

		e.State.RemoveProposal(proposalID, proposal)
		nextToExpireProposalIDsSet.Remove(proposalID)
	}

	avax.Consume(e.State, tx.Ins)
	avax.Produce(e.State, e.Tx.ID(), tx.Outs)

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
	var err error
	if err = e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	chainTime := e.State.GetTimestamp()
	txAddressState := tx.StateBit.ToAddressState()

	// Check for bits that was affected or introduced in AthensPhase
	isAthensPhase := e.Config.IsAthensPhaseActivated(chainTime)
	if !isAthensPhase && txAddressState&as.AddressStateAthensPhaseBits != 0 {
		return fmt.Errorf("%w: can't modify bit (%d) before AthensPhase", errNotAthensPhase, tx.StateBit)
	}

	// Check for bits that was affected or introduced in BerlinPhase
	isBerlinPhase := e.Config.IsBerlinPhaseActivated(chainTime)
	if !isBerlinPhase && txAddressState&as.AddressStateBerlinPhaseBits != 0 {
		return fmt.Errorf("%w: can't modify bit (%d) before BerlinPhase", errNotBerlinPhase, tx.StateBit)
	}
	if isBerlinPhase && tx.StateBit == as.AddressStateBitConsortium { // Berlin phase moved consortium handling to admin proposals
		return fmt.Errorf("%w: can't modify 'Consortium' bit (%d) after BerlinPhase", errBerlinPhase, tx.StateBit)
	}

	creds := e.Tx.Creds
	roles := as.AddressStateEmpty
	isRemovingAdminRole := tx.Remove && tx.StateBit == as.AddressStateBitRoleAdmin

	// Getting executor roles and verifying executor credential
	if tx.UpgradeVersionID.Version() > 0 {
		if !isAthensPhase {
			return errNotAthensPhase
		}
		if err = e.Backend.Fx.VerifyMultisigPermission(
			e.Tx.Unsigned,
			tx.ExecutorAuth,
			e.Tx.Creds[len(e.Tx.Creds)-1], // executor cred
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{tx.Executor},
			},
			e.State,
		); err != nil {
			return fmt.Errorf("%w: missing executor's signature: %s", errSignatureMissing, err)
		}
		creds = e.Tx.Creds[:len(e.Tx.Creds)-1]

		if isRemovingAdminRole && tx.Address == tx.Executor {
			return errAdminCannotBeDeleted
		}

		roles, err = e.State.GetAddressStates(tx.Executor)
		if err != nil {
			return err
		}
	} else {
		if isBerlinPhase {
			return errBerlinPhase
		}
		addresses, err := e.Fx.RecoverAddresses(tx.Bytes(), e.Tx.Creds)
		if err != nil {
			return fmt.Errorf("%w: %s", errRecoverAddresses, err)
		}

		// Accumulate roles over all signers
		for address := range addresses {
			if isRemovingAdminRole && address == tx.Address {
				return errAdminCannotBeDeleted
			}
			states, err := e.State.GetAddressStates(address)
			if err != nil {
				return err
			}
			roles |= states
		}
	}

	// Verify that executor roles are allowed to modify tx.State
	if !isPermittedToModifyAddrStateBit(isBerlinPhase, roles, txAddressState) {
		return fmt.Errorf("%w (addr: %s, bit: %b)", errAddrStateNotPermitted, tx.Address, tx.StateBit)
	}

	// Verify the flowcheck
	baseFee, err := e.State.GetBaseFee()
	if err != nil {
		return err
	}
	if err := e.FlowChecker.VerifySpend(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		creds,
		map[ids.ID]uint64{
			e.Ctx.AVAXAssetID: baseFee,
		},
	); err != nil {
		return err
	}

	// Special case: if addrStateBit is nodeDeferred, perform
	// additional actions that will defer or resume validator
	if tx.StateBit == as.AddressStateBitNodeDeferred {
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

	// Get the current state
	addrState, err := e.State.GetAddressStates(tx.Address)
	if err != nil {
		return err
	}
	// Calculate new state
	newAddrState := addrState
	if tx.Remove && (addrState&txAddressState) != 0 {
		newAddrState ^= txAddressState
	} else if !tx.Remove {
		newAddrState |= txAddressState
	}
	// Set the new state if changed
	if addrState != newAddrState {
		e.State.SetAddressStates(tx.Address, newAddrState)
	}

	// Consume the UTXOS
	avax.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	avax.Produce(e.State, e.Tx.ID(), tx.Outs)

	return nil
}

const (
	addressStateKYCAll   = as.AddressStateKYCVerified | as.AddressStateKYCExpired | as.AddressStateKYBVerified
	addressStateRoleBits = as.AddressStateRoleAdmin | as.AddressStateRoleKYCAdmin |
		as.AddressStateRoleConsortiumSecretary | as.AddressStateRoleOffersAdmin |
		as.AddressStateRoleValidatorAdmin | as.AddressStateFoundationAdmin
)

// [state] must have only one bit set
func isPermittedToModifyAddrStateBit(isBerlinPhase bool, roles, state as.AddressState) bool {
	switch {
	// admin can do anything before BerlinPhase, after that admin can only modify other roles
	case roles.Is(as.AddressStateRoleAdmin) && (!isBerlinPhase || addressStateRoleBits&state != 0):
	// kyc role can change kyc/kyb bits
	case addressStateKYCAll&state != 0 && roles.Is(as.AddressStateRoleKYCAdmin):
	// offers admin can assign offers creator role
	case state == as.AddressStateOffersCreator && roles.Is(as.AddressStateRoleOffersAdmin):
	// validator admin can defer or resume node
	case state == as.AddressStateNodeDeferred && roles.Is(as.AddressStateRoleValidatorAdmin):
	default:
		return false
	}
	return true
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

// Inner ins must implement Equal(any) bool func.
func inputsAreEqual(ins1, ins2 []*avax.TransferableInput) bool {
	return slices.EqualFunc(ins1, ins2, func(in1, in2 *avax.TransferableInput) bool {
		inEq1, ok := in1.In.(interface{ Equal(any) bool })
		return ok &&
			in1.Asset == in2.Asset && in1.TxID == in2.TxID && in1.OutputIndex == in2.OutputIndex &&
			inEq1.Equal(in2.In)
	})
}

// Inner outs must implement Equal(any) bool func.
func outputsAreEqual(outs1, outs2 []*avax.TransferableOutput) bool {
	return slices.EqualFunc(outs1, outs2, func(out1, out2 *avax.TransferableOutput) bool {
		outEq1, ok := out1.Out.(interface{ Equal(any) bool })
		return ok && out1.Asset == out2.Asset && outEq1.Equal(out2.Out)
	})
}
