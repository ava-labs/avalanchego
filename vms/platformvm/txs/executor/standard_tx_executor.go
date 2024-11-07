// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
)

var (
	_ txs.Visitor = (*standardTxExecutor)(nil)

	errEmptyNodeID                = errors.New("validator nodeID cannot be empty")
	errMaxStakeDurationTooLarge   = errors.New("max stake duration must be less than or equal to the global max stake duration")
	errMissingStartTimePreDurango = errors.New("staker transactions must have a StartTime pre-Durango")
	errEtnaUpgradeNotActive       = errors.New("attempting to use an Etna-upgrade feature prior to activation")
	errTransformSubnetTxPostEtna  = errors.New("TransformSubnetTx is not permitted post-Etna")
	errMaxNumActiveValidators     = errors.New("already at the max number of active validators")
)

// StandardTx executes the standard transaction [tx].
//
// [state] is modified to represent the state of the chain after the execution
// of [tx].
//
// Returns:
//   - The IDs of any import UTXOs consumed.
//   - The, potentially nil, atomic requests that should be performed against
//     shared memory when this transaction is accepted.
//   - A, potentially nil, function that should be called when this transaction
//     is accepted.
func StandardTx(
	backend *Backend,
	feeCalculator fee.Calculator,
	tx *txs.Tx,
	state state.Diff,
) (set.Set[ids.ID], map[ids.ID]*atomic.Requests, func(), error) {
	standardExecutor := standardTxExecutor{
		backend:       backend,
		feeCalculator: feeCalculator,
		tx:            tx,
		state:         state,
	}
	if err := tx.Unsigned.Visit(&standardExecutor); err != nil {
		txID := tx.ID()
		return nil, nil, nil, fmt.Errorf("standard tx %s failed execution: %w", txID, err)
	}
	return standardExecutor.inputs, standardExecutor.atomicRequests, standardExecutor.onAccept, nil
}

type standardTxExecutor struct {
	// inputs, to be filled before visitor methods are called
	backend       *Backend
	state         state.Diff // state is expected to be modified
	feeCalculator fee.Calculator
	tx            *txs.Tx

	// outputs of visitor execution
	onAccept       func() // may be nil
	inputs         set.Set[ids.ID]
	atomicRequests map[ids.ID]*atomic.Requests // may be nil
}

func (*standardTxExecutor) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return ErrWrongTxType
}

func (*standardTxExecutor) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return ErrWrongTxType
}

func (e *standardTxExecutor) CreateChainTx(tx *txs.CreateChainTx) error {
	if err := e.tx.SyntacticVerify(e.backend.Ctx); err != nil {
		return err
	}

	var (
		currentTimestamp = e.state.GetTimestamp()
		isDurangoActive  = e.backend.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp)
	)
	if err := avax.VerifyMemoFieldLength(tx.Memo, isDurangoActive); err != nil {
		return err
	}

	baseTxCreds, err := verifyPoASubnetAuthorization(e.backend, e.state, e.tx, tx.SubnetID, tx.SubnetAuth)
	if err != nil {
		return err
	}

	// Verify the flowcheck
	fee, err := e.feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}
	if err := e.backend.FlowChecker.VerifySpend(
		tx,
		e.state,
		tx.Ins,
		tx.Outs,
		baseTxCreds,
		map[ids.ID]uint64{
			e.backend.Ctx.AVAXAssetID: fee,
		},
	); err != nil {
		return err
	}

	txID := e.tx.ID()

	// Consume the UTXOS
	avax.Consume(e.state, tx.Ins)
	// Produce the UTXOS
	avax.Produce(e.state, txID, tx.Outs)
	// Add the new chain to the database
	e.state.AddChain(e.tx)

	// If this proposal is committed and this node is a member of the subnet
	// that validates the blockchain, create the blockchain
	e.onAccept = func() {
		e.backend.Config.CreateChain(txID, tx)
	}
	return nil
}

func (e *standardTxExecutor) CreateSubnetTx(tx *txs.CreateSubnetTx) error {
	// Make sure this transaction is well formed.
	if err := e.tx.SyntacticVerify(e.backend.Ctx); err != nil {
		return err
	}

	var (
		currentTimestamp = e.state.GetTimestamp()
		isDurangoActive  = e.backend.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp)
	)
	if err := avax.VerifyMemoFieldLength(tx.Memo, isDurangoActive); err != nil {
		return err
	}

	// Verify the flowcheck
	fee, err := e.feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}
	if err := e.backend.FlowChecker.VerifySpend(
		tx,
		e.state,
		tx.Ins,
		tx.Outs,
		e.tx.Creds,
		map[ids.ID]uint64{
			e.backend.Ctx.AVAXAssetID: fee,
		},
	); err != nil {
		return err
	}

	txID := e.tx.ID()

	// Consume the UTXOS
	avax.Consume(e.state, tx.Ins)
	// Produce the UTXOS
	avax.Produce(e.state, txID, tx.Outs)
	// Add the new subnet to the database
	e.state.AddSubnet(txID)
	e.state.SetSubnetOwner(txID, tx.Owner)
	return nil
}

func (e *standardTxExecutor) ExportTx(tx *txs.ExportTx) error {
	if err := e.tx.SyntacticVerify(e.backend.Ctx); err != nil {
		return err
	}

	var (
		currentTimestamp = e.state.GetTimestamp()
		isDurangoActive  = e.backend.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp)
	)
	if err := avax.VerifyMemoFieldLength(tx.Memo, isDurangoActive); err != nil {
		return err
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.ExportedOutputs))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.ExportedOutputs)

	if e.backend.Bootstrapped.Get() {
		if err := verify.SameSubnet(context.TODO(), e.backend.Ctx, tx.DestinationChain); err != nil {
			return err
		}
	}

	// Verify the flowcheck
	fee, err := e.feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}
	if err := e.backend.FlowChecker.VerifySpend(
		tx,
		e.state,
		tx.Ins,
		outs,
		e.tx.Creds,
		map[ids.ID]uint64{
			e.backend.Ctx.AVAXAssetID: fee,
		},
	); err != nil {
		return fmt.Errorf("failed verifySpend: %w", err)
	}

	txID := e.tx.ID()

	// Consume the UTXOS
	avax.Consume(e.state, tx.Ins)
	// Produce the UTXOS
	avax.Produce(e.state, txID, tx.Outs)

	// Note: We apply atomic requests even if we are not verifying atomic
	// requests to ensure the shared state will be correct if we later start
	// verifying the requests.
	elems := make([]*atomic.Element, len(tx.ExportedOutputs))
	for i, out := range tx.ExportedOutputs {
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(len(tx.Outs) + i),
			},
			Asset: avax.Asset{ID: out.AssetID()},
			Out:   out.Out,
		}

		utxoBytes, err := txs.Codec.Marshal(txs.CodecVersion, utxo)
		if err != nil {
			return fmt.Errorf("failed to marshal UTXO: %w", err)
		}
		utxoID := utxo.InputID()
		elem := &atomic.Element{
			Key:   utxoID[:],
			Value: utxoBytes,
		}
		if out, ok := utxo.Out.(avax.Addressable); ok {
			elem.Traits = out.Addresses()
		}

		elems[i] = elem
	}
	e.atomicRequests = map[ids.ID]*atomic.Requests{
		tx.DestinationChain: {
			PutRequests: elems,
		},
	}
	return nil
}

func (e *standardTxExecutor) AddValidatorTx(tx *txs.AddValidatorTx) error {
	if tx.Validator.NodeID == ids.EmptyNodeID {
		return errEmptyNodeID
	}

	if _, err := verifyAddValidatorTx(
		e.backend,
		e.feeCalculator,
		e.state,
		e.tx,
		tx,
	); err != nil {
		return err
	}

	if err := e.putStaker(tx); err != nil {
		return err
	}

	txID := e.tx.ID()
	avax.Consume(e.state, tx.Ins)
	avax.Produce(e.state, txID, tx.Outs)

	if e.backend.Config.PartialSyncPrimaryNetwork && tx.Validator.NodeID == e.backend.Ctx.NodeID {
		e.backend.Ctx.Log.Warn("verified transaction that would cause this node to become unhealthy",
			zap.String("reason", "primary network is not being fully synced"),
			zap.Stringer("txID", txID),
			zap.String("txType", "addValidator"),
			zap.Stringer("nodeID", tx.Validator.NodeID),
		)
	}
	return nil
}

func (e *standardTxExecutor) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	if err := verifyAddSubnetValidatorTx(
		e.backend,
		e.feeCalculator,
		e.state,
		e.tx,
		tx,
	); err != nil {
		return err
	}

	if err := e.putStaker(tx); err != nil {
		return err
	}

	txID := e.tx.ID()
	avax.Consume(e.state, tx.Ins)
	avax.Produce(e.state, txID, tx.Outs)
	return nil
}

func (e *standardTxExecutor) AddDelegatorTx(tx *txs.AddDelegatorTx) error {
	if _, err := verifyAddDelegatorTx(
		e.backend,
		e.feeCalculator,
		e.state,
		e.tx,
		tx,
	); err != nil {
		return err
	}

	if err := e.putStaker(tx); err != nil {
		return err
	}

	txID := e.tx.ID()
	avax.Consume(e.state, tx.Ins)
	avax.Produce(e.state, txID, tx.Outs)
	return nil
}

func (e *standardTxExecutor) ImportTx(tx *txs.ImportTx) error {
	if err := e.tx.SyntacticVerify(e.backend.Ctx); err != nil {
		return err
	}

	var (
		currentTimestamp = e.state.GetTimestamp()
		isDurangoActive  = e.backend.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp)
	)
	if err := avax.VerifyMemoFieldLength(tx.Memo, isDurangoActive); err != nil {
		return err
	}

	e.inputs = set.NewSet[ids.ID](len(tx.ImportedInputs))
	utxoIDs := make([][]byte, len(tx.ImportedInputs))
	for i, in := range tx.ImportedInputs {
		utxoID := in.UTXOID.InputID()

		e.inputs.Add(utxoID)
		utxoIDs[i] = utxoID[:]
	}

	// Skip verification of the shared memory inputs if the other primary
	// network chains are not guaranteed to be up-to-date.
	if e.backend.Bootstrapped.Get() && !e.backend.Config.PartialSyncPrimaryNetwork {
		if err := verify.SameSubnet(context.TODO(), e.backend.Ctx, tx.SourceChain); err != nil {
			return err
		}

		allUTXOBytes, err := e.backend.Ctx.SharedMemory.Get(tx.SourceChain, utxoIDs)
		if err != nil {
			return fmt.Errorf("failed to get shared memory: %w", err)
		}

		utxos := make([]*avax.UTXO, len(tx.Ins)+len(tx.ImportedInputs))
		for index, input := range tx.Ins {
			utxo, err := e.state.GetUTXO(input.InputID())
			if err != nil {
				return fmt.Errorf("failed to get UTXO %s: %w", &input.UTXOID, err)
			}
			utxos[index] = utxo
		}
		for i, utxoBytes := range allUTXOBytes {
			utxo := &avax.UTXO{}
			if _, err := txs.Codec.Unmarshal(utxoBytes, utxo); err != nil {
				return fmt.Errorf("failed to unmarshal UTXO: %w", err)
			}
			utxos[i+len(tx.Ins)] = utxo
		}

		ins := make([]*avax.TransferableInput, len(tx.Ins)+len(tx.ImportedInputs))
		copy(ins, tx.Ins)
		copy(ins[len(tx.Ins):], tx.ImportedInputs)

		// Verify the flowcheck
		fee, err := e.feeCalculator.CalculateFee(tx)
		if err != nil {
			return err
		}
		if err := e.backend.FlowChecker.VerifySpendUTXOs(
			tx,
			utxos,
			ins,
			tx.Outs,
			e.tx.Creds,
			map[ids.ID]uint64{
				e.backend.Ctx.AVAXAssetID: fee,
			},
		); err != nil {
			return err
		}
	}

	txID := e.tx.ID()

	// Consume the UTXOS
	avax.Consume(e.state, tx.Ins)
	// Produce the UTXOS
	avax.Produce(e.state, txID, tx.Outs)

	// Note: We apply atomic requests even if we are not verifying atomic
	// requests to ensure the shared state will be correct if we later start
	// verifying the requests.
	e.atomicRequests = map[ids.ID]*atomic.Requests{
		tx.SourceChain: {
			RemoveRequests: utxoIDs,
		},
	}
	return nil
}

// Verifies a [*txs.RemoveSubnetValidatorTx] and, if it passes, executes it on
// [e.State]. For verification rules, see [verifyRemoveSubnetValidatorTx]. This
// transaction will result in [tx.NodeID] being removed as a validator of
// [tx.SubnetID].
// Note: [tx.NodeID] may be either a current or pending validator.
func (e *standardTxExecutor) RemoveSubnetValidatorTx(tx *txs.RemoveSubnetValidatorTx) error {
	staker, isCurrentValidator, err := verifyRemoveSubnetValidatorTx(
		e.backend,
		e.feeCalculator,
		e.state,
		e.tx,
		tx,
	)
	if err != nil {
		return err
	}

	if isCurrentValidator {
		e.state.DeleteCurrentValidator(staker)
	} else {
		e.state.DeletePendingValidator(staker)
	}

	// Invariant: There are no permissioned subnet delegators to remove.

	txID := e.tx.ID()
	avax.Consume(e.state, tx.Ins)
	avax.Produce(e.state, txID, tx.Outs)

	return nil
}

func (e *standardTxExecutor) TransformSubnetTx(tx *txs.TransformSubnetTx) error {
	currentTimestamp := e.state.GetTimestamp()
	if e.backend.Config.UpgradeConfig.IsEtnaActivated(currentTimestamp) {
		return errTransformSubnetTxPostEtna
	}

	if err := e.tx.SyntacticVerify(e.backend.Ctx); err != nil {
		return err
	}

	isDurangoActive := e.backend.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp)
	if err := avax.VerifyMemoFieldLength(tx.Memo, isDurangoActive); err != nil {
		return err
	}

	// Note: math.MaxInt32 * time.Second < math.MaxInt64 - so this can never
	// overflow.
	if time.Duration(tx.MaxStakeDuration)*time.Second > e.backend.Config.MaxStakeDuration {
		return errMaxStakeDurationTooLarge
	}

	baseTxCreds, err := verifyPoASubnetAuthorization(e.backend, e.state, e.tx, tx.Subnet, tx.SubnetAuth)
	if err != nil {
		return err
	}

	// Verify the flowcheck
	fee, err := e.feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}
	totalRewardAmount := tx.MaximumSupply - tx.InitialSupply
	if err := e.backend.FlowChecker.VerifySpend(
		tx,
		e.state,
		tx.Ins,
		tx.Outs,
		baseTxCreds,
		// Invariant: [tx.AssetID != e.Ctx.AVAXAssetID]. This prevents the first
		//            entry in this map literal from being overwritten by the
		//            second entry.
		map[ids.ID]uint64{
			e.backend.Ctx.AVAXAssetID: fee,
			tx.AssetID:                totalRewardAmount,
		},
	); err != nil {
		return err
	}

	txID := e.tx.ID()

	// Consume the UTXOS
	avax.Consume(e.state, tx.Ins)
	// Produce the UTXOS
	avax.Produce(e.state, txID, tx.Outs)
	// Transform the new subnet in the database
	e.state.AddSubnetTransformation(e.tx)
	e.state.SetCurrentSupply(tx.Subnet, tx.InitialSupply)
	return nil
}

func (e *standardTxExecutor) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	if err := verifyAddPermissionlessValidatorTx(
		e.backend,
		e.feeCalculator,
		e.state,
		e.tx,
		tx,
	); err != nil {
		return err
	}

	if err := e.putStaker(tx); err != nil {
		return err
	}

	txID := e.tx.ID()
	avax.Consume(e.state, tx.Ins)
	avax.Produce(e.state, txID, tx.Outs)

	if e.backend.Config.PartialSyncPrimaryNetwork &&
		tx.Subnet == constants.PrimaryNetworkID &&
		tx.Validator.NodeID == e.backend.Ctx.NodeID {
		e.backend.Ctx.Log.Warn("verified transaction that would cause this node to become unhealthy",
			zap.String("reason", "primary network is not being fully synced"),
			zap.Stringer("txID", txID),
			zap.String("txType", "addPermissionlessValidator"),
			zap.Stringer("nodeID", tx.Validator.NodeID),
		)
	}

	return nil
}

func (e *standardTxExecutor) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	if err := verifyAddPermissionlessDelegatorTx(
		e.backend,
		e.feeCalculator,
		e.state,
		e.tx,
		tx,
	); err != nil {
		return err
	}

	if err := e.putStaker(tx); err != nil {
		return err
	}

	txID := e.tx.ID()
	avax.Consume(e.state, tx.Ins)
	avax.Produce(e.state, txID, tx.Outs)
	return nil
}

// Verifies a [*txs.TransferSubnetOwnershipTx] and, if it passes, executes it on
// [e.State]. For verification rules, see [verifyTransferSubnetOwnershipTx].
// This transaction will result in the ownership of [tx.Subnet] being transferred
// to [tx.Owner].
func (e *standardTxExecutor) TransferSubnetOwnershipTx(tx *txs.TransferSubnetOwnershipTx) error {
	err := verifyTransferSubnetOwnershipTx(
		e.backend,
		e.feeCalculator,
		e.state,
		e.tx,
		tx,
	)
	if err != nil {
		return err
	}

	e.state.SetSubnetOwner(tx.Subnet, tx.Owner)

	txID := e.tx.ID()
	avax.Consume(e.state, tx.Ins)
	avax.Produce(e.state, txID, tx.Outs)
	return nil
}

func (e *standardTxExecutor) BaseTx(tx *txs.BaseTx) error {
	var (
		currentTimestamp = e.state.GetTimestamp()
		upgrades         = e.backend.Config.UpgradeConfig
	)
	if !upgrades.IsDurangoActivated(currentTimestamp) {
		return ErrDurangoUpgradeNotActive
	}

	// Verify the tx is well-formed
	if err := e.tx.SyntacticVerify(e.backend.Ctx); err != nil {
		return err
	}

	if err := avax.VerifyMemoFieldLength(tx.Memo, true /*=isDurangoActive*/); err != nil {
		return err
	}

	// Verify the flowcheck
	fee, err := e.feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}
	if err := e.backend.FlowChecker.VerifySpend(
		tx,
		e.state,
		tx.Ins,
		tx.Outs,
		e.tx.Creds,
		map[ids.ID]uint64{
			e.backend.Ctx.AVAXAssetID: fee,
		},
	); err != nil {
		return err
	}

	txID := e.tx.ID()
	// Consume the UTXOS
	avax.Consume(e.state, tx.Ins)
	// Produce the UTXOS
	avax.Produce(e.state, txID, tx.Outs)
	return nil
}

func (e *standardTxExecutor) ConvertSubnetTx(tx *txs.ConvertSubnetTx) error {
	var (
		currentTimestamp = e.state.GetTimestamp()
		upgrades         = e.backend.Config.UpgradeConfig
	)
	if !upgrades.IsEtnaActivated(currentTimestamp) {
		return errEtnaUpgradeNotActive
	}

	if err := e.tx.SyntacticVerify(e.backend.Ctx); err != nil {
		return err
	}

	if err := avax.VerifyMemoFieldLength(tx.Memo, true /*=isDurangoActive*/); err != nil {
		return err
	}

	baseTxCreds, err := verifyPoASubnetAuthorization(e.backend, e.state, e.tx, tx.Subnet, tx.SubnetAuth)
	if err != nil {
		return err
	}

	// Verify the flowcheck
	fee, err := e.feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}

	var (
		startTime            = uint64(currentTimestamp.Unix())
		currentFees          = e.state.GetAccruedFees()
		subnetConversionData = message.SubnetConversionData{
			SubnetID:       tx.Subnet,
			ManagerChainID: tx.ChainID,
			ManagerAddress: tx.Address,
			Validators:     make([]message.SubnetConversionValidatorData, len(tx.Validators)),
		}
	)
	for i, vdr := range tx.Validators {
		nodeID, err := ids.ToNodeID(vdr.NodeID)
		if err != nil {
			return err
		}

		remainingBalanceOwner, err := txs.Codec.Marshal(txs.CodecVersion, &vdr.RemainingBalanceOwner)
		if err != nil {
			return err
		}
		deactivationOwner, err := txs.Codec.Marshal(txs.CodecVersion, &vdr.DeactivationOwner)
		if err != nil {
			return err
		}

		sov := state.SubnetOnlyValidator{
			ValidationID:          tx.Subnet.Append(uint32(i)),
			SubnetID:              tx.Subnet,
			NodeID:                nodeID,
			PublicKey:             bls.PublicKeyToUncompressedBytes(vdr.Signer.Key()),
			RemainingBalanceOwner: remainingBalanceOwner,
			DeactivationOwner:     deactivationOwner,
			StartTime:             startTime,
			Weight:                vdr.Weight,
			MinNonce:              0,
			EndAccumulatedFee:     0, // If Balance is 0, this is 0
		}
		if vdr.Balance != 0 {
			// We are attempting to add an active validator
			if gas.Gas(e.state.NumActiveSubnetOnlyValidators()) >= e.backend.Config.ValidatorFeeConfig.Capacity {
				return errMaxNumActiveValidators
			}

			sov.EndAccumulatedFee, err = math.Add(vdr.Balance, currentFees)
			if err != nil {
				return err
			}

			fee, err = math.Add(fee, vdr.Balance)
			if err != nil {
				return err
			}
		}

		if err := e.state.PutSubnetOnlyValidator(sov); err != nil {
			return err
		}

		subnetConversionData.Validators[i] = message.SubnetConversionValidatorData{
			NodeID:       vdr.NodeID,
			BLSPublicKey: vdr.Signer.PublicKey,
			Weight:       vdr.Weight,
		}
	}
	if err := e.backend.FlowChecker.VerifySpend(
		tx,
		e.state,
		tx.Ins,
		tx.Outs,
		baseTxCreds,
		map[ids.ID]uint64{
			e.backend.Ctx.AVAXAssetID: fee,
		},
	); err != nil {
		return err
	}

	conversionID, err := message.SubnetConversionID(subnetConversionData)
	if err != nil {
		return err
	}

	txID := e.tx.ID()

	// Consume the UTXOS
	avax.Consume(e.state, tx.Ins)
	// Produce the UTXOS
	avax.Produce(e.state, txID, tx.Outs)
	// Track the subnet conversion in the database
	e.state.SetSubnetConversion(
		tx.Subnet,
		state.SubnetConversion{
			ConversionID: conversionID,
			ChainID:      tx.ChainID,
			Addr:         tx.Address,
		},
	)
	return nil
}

// Creates the staker as defined in [stakerTx] and adds it to [e.State].
func (e *standardTxExecutor) putStaker(stakerTx txs.Staker) error {
	var (
		chainTime = e.state.GetTimestamp()
		txID      = e.tx.ID()
		staker    *state.Staker
		err       error
	)

	if !e.backend.Config.UpgradeConfig.IsDurangoActivated(chainTime) {
		// Pre-Durango, stakers set a future [StartTime] and are added to the
		// pending staker set. They are promoted to the current staker set once
		// the chain time reaches [StartTime].
		scheduledStakerTx, ok := stakerTx.(txs.ScheduledStaker)
		if !ok {
			return fmt.Errorf("%w: %T", errMissingStartTimePreDurango, stakerTx)
		}
		staker, err = state.NewPendingStaker(txID, scheduledStakerTx)
	} else {
		// Only calculate the potentialReward for permissionless stakers.
		// Recall that we only need to check if this is a permissioned
		// validator as there are no permissioned delegators
		var potentialReward uint64
		if !stakerTx.CurrentPriority().IsPermissionedValidator() {
			subnetID := stakerTx.SubnetID()
			currentSupply, err := e.state.GetCurrentSupply(subnetID)
			if err != nil {
				return err
			}

			rewards, err := GetRewardsCalculator(e.backend, e.state, subnetID)
			if err != nil {
				return err
			}

			// Post-Durango, stakers are immediately added to the current staker
			// set. Their [StartTime] is the current chain time.
			stakeDuration := stakerTx.EndTime().Sub(chainTime)
			potentialReward = rewards.Calculate(
				stakeDuration,
				stakerTx.Weight(),
				currentSupply,
			)

			e.state.SetCurrentSupply(subnetID, currentSupply+potentialReward)
		}

		staker, err = state.NewCurrentStaker(txID, stakerTx, chainTime, potentialReward)
	}
	if err != nil {
		return err
	}

	switch priority := staker.Priority; {
	case priority.IsCurrentValidator():
		if err := e.state.PutCurrentValidator(staker); err != nil {
			return err
		}
	case priority.IsCurrentDelegator():
		e.state.PutCurrentDelegator(staker)
	case priority.IsPendingValidator():
		if err := e.state.PutPendingValidator(staker); err != nil {
			return err
		}
	case priority.IsPendingDelegator():
		e.state.PutPendingDelegator(staker)
	default:
		return fmt.Errorf("staker %s, unexpected priority %d", staker.TxID, priority)
	}
	return nil
}
