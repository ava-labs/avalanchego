// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ txs.Visitor = (*CaminoStandardTxExecutor)(nil)
	_ txs.Visitor = (*CaminoProposalTxExecutor)(nil)

	errNodeSignatureMissing = errors.New("last signature is not nodeID's signature")
	errWrongLockMode        = errors.New("this tx can't be used with this caminoGenesis.LockModeBondDeposit")
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
	if state, err := e.State.CaminoGenesisState(); err != nil {
		return err
	} else if state.VerifyNodeSignature {
		if err := e.Backend.Fx.VerifyPermission(
			e.Tx.Unsigned,
			&secp256k1fx.Input{SigIndices: []uint32{0}},
			e.Tx.Creds[len(e.Tx.Creds)-1],
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs: []ids.ShortID{
					ids.ShortID(nodeID),
				},
			},
		); err != nil {
			return fmt.Errorf("%w: %s", errNodeSignatureMissing, err)
		}
	}
	return nil
}

// verifyAddValidatorTxWithBonding carries out the validation for an AddValidatorTx
// using bonding lock-mode instead of avax staking.
// It returns the tx outputs that should be returned if this validator is not
// added to the staking set.
func verifyAddValidatorTxWithBonding(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddValidatorTx,
) (
	[]*avax.TransferableOutput,
	error,
) {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return nil, err
	}

	duration := tx.Validator.Duration()

	switch {
	case tx.Validator.Wght < backend.Config.MinValidatorStake:
		// Ensure validator is staking at least the minimum amount
		return nil, errWeightTooSmall

	case tx.Validator.Wght > backend.Config.MaxValidatorStake:
		// Ensure validator isn't staking too much
		return nil, errWeightTooLarge

	case tx.DelegationShares < backend.Config.MinDelegationFee:
		// Ensure the validator fee is at least the minimum amount
		return nil, errInsufficientDelegationFee

	case duration < backend.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return nil, errStakeTooShort

	case duration > backend.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return nil, errStakeTooLong
	}

	if !backend.Bootstrapped.GetValue() {
		return tx.Outs, nil
	}

	currentTimestamp := chainState.GetTimestamp()
	// Ensure the proposed validator starts after the current time
	startTime := tx.StartTime()
	if !currentTimestamp.Before(startTime) {
		return nil, fmt.Errorf(
			"%w: %s >= %s",
			errTimestampNotBeforeStartTime,
			currentTimestamp,
			startTime,
		)
	}

	_, err := GetValidator(chainState, constants.PrimaryNetworkID, tx.Validator.NodeID)
	if err == nil {
		return nil, fmt.Errorf(
			"attempted to issue duplicate validation for %s",
			tx.Validator.NodeID,
		)
	}
	if err != database.ErrNotFound {
		return nil, fmt.Errorf(
			"failed to find whether %s is a primary network validator: %w",
			tx.Validator.NodeID,
			err,
		)
	}

	// Verify the flowcheck
	if err := backend.FlowChecker.VerifyLock(
		tx,
		chainState,
		tx.Ins,
		tx.Outs,
		sTx.Creds,
		backend.Config.AddPrimaryNetworkValidatorFee,
		backend.Ctx.AVAXAssetID,
		locked.StateBonded,
	); err != nil {
		return nil, fmt.Errorf("%w: %s", errFlowCheckFailed, err)
	}

	// Make sure the tx doesn't start too far in the future. This is done last
	// to allow the verifier visitor to explicitly check for this error.
	maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
	if startTime.After(maxStartTime) {
		return nil, errFutureStakeTime
	}

	return tx.Outs, nil
}

func (e *CaminoStandardTxExecutor) AddValidatorTx(tx *txs.AddValidatorTx) error {
	if err := e.verifyNodeSignature(tx.NodeID()); err != nil {
		return err
	}

	caminoGenesis, err := e.State.CaminoGenesisState()
	if err != nil {
		return err
	}

	if err := locked.VerifyLockMode(tx.Ins, tx.Outs, caminoGenesis.LockModeBondDeposit); err != nil {
		return err
	}

	_, ok := e.Tx.Unsigned.(*txs.CaminoAddValidatorTx)

	if !caminoGenesis.LockModeBondDeposit && !ok {
		if caminoGenesis.VerifyNodeSignature {
			creds := removeCreds(e.Tx, 1)
			err = e.StandardTxExecutor.AddValidatorTx(tx)
			addCreds(e.Tx, creds)
		} else {
			err = e.StandardTxExecutor.AddValidatorTx(tx)
		}
		return err
	}

	if !caminoGenesis.LockModeBondDeposit || !ok {
		return errWrongLockMode
	}

	if tx.Validator.NodeID == ids.EmptyNodeID {
		return errEmptyNodeID
	}

	if _, err := verifyAddValidatorTxWithBonding(
		e.Backend,
		e.State,
		e.Tx,
		tx,
	); err != nil {
		return err
	}

	txID := e.Tx.ID()

	newStaker := state.NewPendingStaker(txID, tx)
	e.State.PutPendingValidator(newStaker)
	utxo.Consume(e.State, tx.Ins)
	if err := utxo.ProduceLocked(e.State, txID, tx.Outs, locked.StateBonded); err != nil {
		return err
	}

	return nil
}

func (e *CaminoStandardTxExecutor) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	if err := e.verifyNodeSignature(tx.NodeID()); err != nil {
		return err
	}

	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}
	caminoGenesis, err := e.State.CaminoGenesisState()
	if err != nil {
		return err
	}

	if caminoGenesis.VerifyNodeSignature {
		creds := removeCreds(e.Tx, 1)
		err = e.StandardTxExecutor.AddSubnetValidatorTx(tx)
		addCreds(e.Tx, creds)
	} else {
		err = e.StandardTxExecutor.AddSubnetValidatorTx(tx)
	}

	return err
}

func (e *CaminoStandardTxExecutor) AddDelegatorTx(tx *txs.AddDelegatorTx) error {
	caminoGenesis, err := e.State.CaminoGenesisState()
	if err != nil {
		return err
	}

	if caminoGenesis.LockModeBondDeposit {
		return errWrongTxType
	}

	if err := locked.VerifyLockMode(tx.Ins, tx.Outs, caminoGenesis.LockModeBondDeposit); err != nil {
		return err
	}

	return e.StandardTxExecutor.AddDelegatorTx(tx)
}

func (e *CaminoStandardTxExecutor) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	caminoGenesis, err := e.State.CaminoGenesisState()
	if err != nil {
		return err
	}

	if caminoGenesis.LockModeBondDeposit {
		return errWrongTxType
	}

	if err := locked.VerifyLockMode(tx.Ins, tx.Outs, caminoGenesis.LockModeBondDeposit); err != nil {
		return err
	}

	return e.StandardTxExecutor.AddPermissionlessValidatorTx(tx)
}

func (e *CaminoStandardTxExecutor) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	caminoGenesis, err := e.State.CaminoGenesisState()
	if err != nil {
		return err
	}

	if caminoGenesis.LockModeBondDeposit {
		return errWrongTxType
	}

	if err := locked.VerifyLockMode(tx.Ins, tx.Outs, caminoGenesis.LockModeBondDeposit); err != nil {
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

func removeCreds(tx *txs.Tx, num int) []verify.Verifiable {
	newCredsLen := len(tx.Creds) - num
	removedCreds := tx.Creds[newCredsLen:len(tx.Creds)]
	tx.Creds = tx.Creds[:newCredsLen]
	return removedCreds
}

func addCreds(tx *txs.Tx, creds []verify.Verifiable) {
	tx.Creds = append(tx.Creds, creds...)
}
