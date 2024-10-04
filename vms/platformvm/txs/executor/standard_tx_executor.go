// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	second                                = 1
	minute                                = 60 * second
	hour                                  = 60 * minute
	day                                   = 24 * hour
	RegisterSubnetValidatorTxExpiryWindow = day
)

var (
	_ txs.Visitor = (*StandardTxExecutor)(nil)

	errEmptyNodeID                = errors.New("validator nodeID cannot be empty")
	errMaxStakeDurationTooLarge   = errors.New("max stake duration must be less than or equal to the global max stake duration")
	errMissingStartTimePreDurango = errors.New("staker transactions must have a StartTime pre-Durango")
	errEtnaUpgradeNotActive       = errors.New("attempting to use an Etna-upgrade feature prior to activation")
	errTransformSubnetTxPostEtna  = errors.New("TransformSubnetTx is not permitted post-Etna")
	errMaxNumActiveValidators     = errors.New("already at the max number of active validators")

	errStateCorruption = errors.New("state corruption")
)

type StandardTxExecutor struct {
	// inputs, to be filled before visitor methods are called
	*Backend
	State         state.Diff // state is expected to be modified
	FeeCalculator fee.Calculator
	Tx            *txs.Tx

	// outputs of visitor execution
	OnAccept       func() // may be nil
	Inputs         set.Set[ids.ID]
	AtomicRequests map[ids.ID]*atomic.Requests // may be nil
}

func (*StandardTxExecutor) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return ErrWrongTxType
}

func (*StandardTxExecutor) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return ErrWrongTxType
}

func (e *StandardTxExecutor) CreateChainTx(tx *txs.CreateChainTx) error {
	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	var (
		currentTimestamp = e.State.GetTimestamp()
		isDurangoActive  = e.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp)
	)
	if err := avax.VerifyMemoFieldLength(tx.Memo, isDurangoActive); err != nil {
		return err
	}

	baseTxCreds, err := verifyPoASubnetAuthorization(e.Backend, e.State, e.Tx, tx.SubnetID, tx.SubnetAuth)
	if err != nil {
		return err
	}

	// Verify the flowcheck
	fee, err := e.FeeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}
	if err := e.FlowChecker.VerifySpend(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		baseTxCreds,
		map[ids.ID]uint64{
			e.Ctx.AVAXAssetID: fee,
		},
	); err != nil {
		return err
	}

	txID := e.Tx.ID()

	// Consume the UTXOS
	avax.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	avax.Produce(e.State, txID, tx.Outs)
	// Add the new chain to the database
	e.State.AddChain(e.Tx)

	// If this proposal is committed and this node is a member of the subnet
	// that validates the blockchain, create the blockchain
	e.OnAccept = func() {
		e.Config.CreateChain(txID, tx)
	}
	return nil
}

func (e *StandardTxExecutor) CreateSubnetTx(tx *txs.CreateSubnetTx) error {
	// Make sure this transaction is well formed.
	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	var (
		currentTimestamp = e.State.GetTimestamp()
		isDurangoActive  = e.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp)
	)
	if err := avax.VerifyMemoFieldLength(tx.Memo, isDurangoActive); err != nil {
		return err
	}

	// Verify the flowcheck
	fee, err := e.FeeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}
	if err := e.FlowChecker.VerifySpend(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		e.Tx.Creds,
		map[ids.ID]uint64{
			e.Ctx.AVAXAssetID: fee,
		},
	); err != nil {
		return err
	}

	txID := e.Tx.ID()

	// Consume the UTXOS
	avax.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	avax.Produce(e.State, txID, tx.Outs)
	// Add the new subnet to the database
	e.State.AddSubnet(txID)
	e.State.SetSubnetOwner(txID, tx.Owner)
	return nil
}

func (e *StandardTxExecutor) ImportTx(tx *txs.ImportTx) error {
	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	var (
		currentTimestamp = e.State.GetTimestamp()
		isDurangoActive  = e.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp)
	)
	if err := avax.VerifyMemoFieldLength(tx.Memo, isDurangoActive); err != nil {
		return err
	}

	e.Inputs = set.NewSet[ids.ID](len(tx.ImportedInputs))
	utxoIDs := make([][]byte, len(tx.ImportedInputs))
	for i, in := range tx.ImportedInputs {
		utxoID := in.UTXOID.InputID()

		e.Inputs.Add(utxoID)
		utxoIDs[i] = utxoID[:]
	}

	// Skip verification of the shared memory inputs if the other primary
	// network chains are not guaranteed to be up-to-date.
	if e.Bootstrapped.Get() && !e.Config.PartialSyncPrimaryNetwork {
		if err := verify.SameSubnet(context.TODO(), e.Ctx, tx.SourceChain); err != nil {
			return err
		}

		allUTXOBytes, err := e.Ctx.SharedMemory.Get(tx.SourceChain, utxoIDs)
		if err != nil {
			return fmt.Errorf("failed to get shared memory: %w", err)
		}

		utxos := make([]*avax.UTXO, len(tx.Ins)+len(tx.ImportedInputs))
		for index, input := range tx.Ins {
			utxo, err := e.State.GetUTXO(input.InputID())
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
		fee, err := e.FeeCalculator.CalculateFee(tx)
		if err != nil {
			return err
		}
		if err := e.FlowChecker.VerifySpendUTXOs(
			tx,
			utxos,
			ins,
			tx.Outs,
			e.Tx.Creds,
			map[ids.ID]uint64{
				e.Ctx.AVAXAssetID: fee,
			},
		); err != nil {
			return err
		}
	}

	txID := e.Tx.ID()

	// Consume the UTXOS
	avax.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	avax.Produce(e.State, txID, tx.Outs)

	// Note: We apply atomic requests even if we are not verifying atomic
	// requests to ensure the shared state will be correct if we later start
	// verifying the requests.
	e.AtomicRequests = map[ids.ID]*atomic.Requests{
		tx.SourceChain: {
			RemoveRequests: utxoIDs,
		},
	}
	return nil
}

func (e *StandardTxExecutor) ExportTx(tx *txs.ExportTx) error {
	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	var (
		currentTimestamp = e.State.GetTimestamp()
		isDurangoActive  = e.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp)
	)
	if err := avax.VerifyMemoFieldLength(tx.Memo, isDurangoActive); err != nil {
		return err
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.ExportedOutputs))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.ExportedOutputs)

	if e.Bootstrapped.Get() {
		if err := verify.SameSubnet(context.TODO(), e.Ctx, tx.DestinationChain); err != nil {
			return err
		}
	}

	// Verify the flowcheck
	fee, err := e.FeeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}
	if err := e.FlowChecker.VerifySpend(
		tx,
		e.State,
		tx.Ins,
		outs,
		e.Tx.Creds,
		map[ids.ID]uint64{
			e.Ctx.AVAXAssetID: fee,
		},
	); err != nil {
		return fmt.Errorf("failed verifySpend: %w", err)
	}

	txID := e.Tx.ID()

	// Consume the UTXOS
	avax.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	avax.Produce(e.State, txID, tx.Outs)

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
	e.AtomicRequests = map[ids.ID]*atomic.Requests{
		tx.DestinationChain: {
			PutRequests: elems,
		},
	}
	return nil
}

func (e *StandardTxExecutor) AddValidatorTx(tx *txs.AddValidatorTx) error {
	if tx.Validator.NodeID == ids.EmptyNodeID {
		return errEmptyNodeID
	}

	if _, err := verifyAddValidatorTx(
		e.Backend,
		e.FeeCalculator,
		e.State,
		e.Tx,
		tx,
	); err != nil {
		return err
	}

	if err := e.putStaker(tx); err != nil {
		return err
	}

	txID := e.Tx.ID()
	avax.Consume(e.State, tx.Ins)
	avax.Produce(e.State, txID, tx.Outs)

	if e.Config.PartialSyncPrimaryNetwork && tx.Validator.NodeID == e.Ctx.NodeID {
		e.Ctx.Log.Warn("verified transaction that would cause this node to become unhealthy",
			zap.String("reason", "primary network is not being fully synced"),
			zap.Stringer("txID", txID),
			zap.String("txType", "addValidator"),
			zap.Stringer("nodeID", tx.Validator.NodeID),
		)
	}
	return nil
}

func (e *StandardTxExecutor) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	if err := verifyAddSubnetValidatorTx(
		e.Backend,
		e.FeeCalculator,
		e.State,
		e.Tx,
		tx,
	); err != nil {
		return err
	}

	if err := e.putStaker(tx); err != nil {
		return err
	}

	txID := e.Tx.ID()
	avax.Consume(e.State, tx.Ins)
	avax.Produce(e.State, txID, tx.Outs)
	return nil
}

func (e *StandardTxExecutor) AddDelegatorTx(tx *txs.AddDelegatorTx) error {
	if _, err := verifyAddDelegatorTx(
		e.Backend,
		e.FeeCalculator,
		e.State,
		e.Tx,
		tx,
	); err != nil {
		return err
	}

	if err := e.putStaker(tx); err != nil {
		return err
	}

	txID := e.Tx.ID()
	avax.Consume(e.State, tx.Ins)
	avax.Produce(e.State, txID, tx.Outs)
	return nil
}

// Verifies a [*txs.RemoveSubnetValidatorTx] and, if it passes, executes it on
// [e.State]. For verification rules, see [verifyRemoveSubnetValidatorTx]. This
// transaction will result in [tx.NodeID] being removed as a validator of
// [tx.SubnetID].
// Note: [tx.NodeID] may be either a current or pending validator.
func (e *StandardTxExecutor) RemoveSubnetValidatorTx(tx *txs.RemoveSubnetValidatorTx) error {
	staker, isCurrentValidator, err := verifyRemoveSubnetValidatorTx(
		e.Backend,
		e.FeeCalculator,
		e.State,
		e.Tx,
		tx,
	)
	if err != nil {
		return err
	}

	if isCurrentValidator {
		e.State.DeleteCurrentValidator(staker)
	} else {
		e.State.DeletePendingValidator(staker)
	}

	// Invariant: There are no permissioned subnet delegators to remove.

	txID := e.Tx.ID()
	avax.Consume(e.State, tx.Ins)
	avax.Produce(e.State, txID, tx.Outs)

	return nil
}

func (e *StandardTxExecutor) TransformSubnetTx(tx *txs.TransformSubnetTx) error {
	currentTimestamp := e.State.GetTimestamp()
	if e.Config.UpgradeConfig.IsEtnaActivated(currentTimestamp) {
		return errTransformSubnetTxPostEtna
	}

	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	isDurangoActive := e.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp)
	if err := avax.VerifyMemoFieldLength(tx.Memo, isDurangoActive); err != nil {
		return err
	}

	// Note: math.MaxInt32 * time.Second < math.MaxInt64 - so this can never
	// overflow.
	if time.Duration(tx.MaxStakeDuration)*time.Second > e.Backend.Config.MaxStakeDuration {
		return errMaxStakeDurationTooLarge
	}

	baseTxCreds, err := verifyPoASubnetAuthorization(e.Backend, e.State, e.Tx, tx.Subnet, tx.SubnetAuth)
	if err != nil {
		return err
	}

	// Verify the flowcheck
	fee, err := e.FeeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}
	totalRewardAmount := tx.MaximumSupply - tx.InitialSupply
	if err := e.Backend.FlowChecker.VerifySpend(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		baseTxCreds,
		// Invariant: [tx.AssetID != e.Ctx.AVAXAssetID]. This prevents the first
		//            entry in this map literal from being overwritten by the
		//            second entry.
		map[ids.ID]uint64{
			e.Ctx.AVAXAssetID: fee,
			tx.AssetID:        totalRewardAmount,
		},
	); err != nil {
		return err
	}

	txID := e.Tx.ID()

	// Consume the UTXOS
	avax.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	avax.Produce(e.State, txID, tx.Outs)
	// Transform the new subnet in the database
	e.State.AddSubnetTransformation(e.Tx)
	e.State.SetCurrentSupply(tx.Subnet, tx.InitialSupply)
	return nil
}

func (e *StandardTxExecutor) ConvertSubnetTx(tx *txs.ConvertSubnetTx) error {
	var (
		currentTimestamp = e.State.GetTimestamp()
		upgrades         = e.Backend.Config.UpgradeConfig
	)
	if !upgrades.IsEtnaActivated(currentTimestamp) {
		return errEtnaUpgradeNotActive
	}

	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	if err := avax.VerifyMemoFieldLength(tx.Memo, true /*=isDurangoActive*/); err != nil {
		return err
	}

	baseTxCreds, err := verifyPoASubnetAuthorization(e.Backend, e.State, e.Tx, tx.Subnet, tx.SubnetAuth)
	if err != nil {
		return err
	}

	// Verify the flowcheck
	fee, err := e.FeeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}

	var (
		startTime            = uint64(currentTimestamp.Unix())
		currentFees          = e.State.GetAccruedFees()
		subnetConversionData = message.SubnetConversionData{
			SubnetID:       tx.Subnet,
			ManagerChainID: tx.ChainID,
			ManagerAddress: tx.Address,
			Validators:     make([]message.SubnetConversionValidatorData, len(tx.Validators)),
		}
	)
	for i, vdr := range tx.Validators {
		vdr := vdr

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
			if gas.Gas(e.State.NumActiveSubnetOnlyValidators()) >= e.Backend.Config.ValidatorFeeCapacity {
				return errMaxNumActiveValidators
			}

			sov.EndAccumulatedFee, err = safemath.Add(vdr.Balance, currentFees)
			if err != nil {
				return err
			}

			fee, err = safemath.Add(fee, vdr.Balance)
			if err != nil {
				return err
			}
		}

		if err := e.State.PutSubnetOnlyValidator(sov); err != nil {
			return err
		}

		subnetConversionData.Validators[i] = message.SubnetConversionValidatorData{
			NodeID:       vdr.NodeID,
			BLSPublicKey: vdr.Signer.PublicKey,
			Weight:       vdr.Weight,
		}
	}
	if err := e.Backend.FlowChecker.VerifySpend(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		baseTxCreds,
		map[ids.ID]uint64{
			e.Ctx.AVAXAssetID: fee,
		},
	); err != nil {
		return err
	}

	conversionID, err := message.SubnetConversionID(subnetConversionData)
	if err != nil {
		return err
	}

	txID := e.Tx.ID()

	// Consume the UTXOS
	avax.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	avax.Produce(e.State, txID, tx.Outs)
	// Set the new Subnet manager in the database
	e.State.SetSubnetConversion(tx.Subnet, conversionID, tx.ChainID, tx.Address)
	return nil
}

func (e *StandardTxExecutor) RegisterSubnetValidatorTx(tx *txs.RegisterSubnetValidatorTx) error {
	var (
		currentTimestamp = e.State.GetTimestamp()
		upgrades         = e.Backend.Config.UpgradeConfig
	)
	if !upgrades.IsEtnaActivated(currentTimestamp) {
		return errEtnaUpgradeNotActive
	}

	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	if err := avax.VerifyMemoFieldLength(tx.Memo, true /*=isDurangoActive*/); err != nil {
		return err
	}

	// Verify the flowcheck
	fee, err := e.FeeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}
	fee, err = safemath.Add(fee, tx.Balance)
	if err != nil {
		return err
	}

	if err := e.Backend.FlowChecker.VerifySpend(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		e.Tx.Creds,
		map[ids.ID]uint64{
			e.Ctx.AVAXAssetID: fee,
		},
	); err != nil {
		return err
	}

	warpMessage, err := warp.ParseMessage(tx.Message)
	if err != nil {
		return err
	}
	if warpMessage.NetworkID != e.Ctx.NetworkID {
		return fmt.Errorf("expected networkID %d but got %d", e.Ctx.NetworkID, warpMessage.NetworkID)
	}

	addressedCall, err := payload.ParseAddressedCall(warpMessage.Payload)
	if err != nil {
		return err
	}

	msg, err := message.ParseRegisterSubnetValidator(addressedCall.Payload)
	if err != nil {
		return err
	}
	if err := msg.Verify(); err != nil {
		return err
	}

	_, expectedChainID, expectedAddress, err := e.State.GetSubnetConversion(msg.SubnetID)
	if err != nil {
		return err
	}
	if warpMessage.SourceChainID != expectedChainID {
		return fmt.Errorf("expected chainID %s but got %s", expectedChainID, warpMessage.SourceChainID)
	}
	if !bytes.Equal(addressedCall.SourceAddress, expectedAddress) {
		return fmt.Errorf("expected address %s but got %s", expectedAddress, addressedCall.SourceAddress)
	}

	currentTimestampUnix := uint64(currentTimestamp.Unix())
	if msg.Expiry <= currentTimestampUnix {
		return fmt.Errorf("expected expiry to be after %d but got %d", currentTimestampUnix, msg.Expiry)
	}
	maxAllowedExpiry, err := safemath.Add(currentTimestampUnix, RegisterSubnetValidatorTxExpiryWindow)
	if err != nil {
		// This should never happen, as it would imply that either
		// currentTimestampUnix or RegisterSubnetValidatorTxExpiryWindow is
		// significantly larger than expected.
		return err
	}
	if msg.Expiry > maxAllowedExpiry {
		return fmt.Errorf("expected expiry to be before %d but got %d", maxAllowedExpiry, msg.Expiry)
	}

	pop := signer.ProofOfPossession{
		PublicKey:         msg.BLSPublicKey,
		ProofOfPossession: tx.ProofOfPossession,
	}
	if err := pop.Verify(); err != nil {
		return err
	}

	validationID := msg.ValidationID()
	expiry := state.ExpiryEntry{
		Timestamp:    msg.Expiry,
		ValidationID: validationID,
	}
	isDuplicate, err := e.State.HasExpiry(expiry)
	if err != nil {
		return err
	}
	if isDuplicate {
		return fmt.Errorf("expiry for %s already exists", validationID)
	}

	nodeID, err := ids.ToNodeID(msg.NodeID)
	if err != nil {
		return err
	}

	remainingBalanceOwner, err := txs.Codec.Marshal(txs.CodecVersion, &msg.RemainingBalanceOwner)
	if err != nil {
		return err
	}

	deactivationOwner, err := txs.Codec.Marshal(txs.CodecVersion, &msg.DisableOwner)
	if err != nil {
		return err
	}

	sov := state.SubnetOnlyValidator{
		ValidationID:          validationID,
		SubnetID:              msg.SubnetID,
		NodeID:                nodeID,
		PublicKey:             bls.PublicKeyToUncompressedBytes(pop.Key()),
		RemainingBalanceOwner: remainingBalanceOwner,
		DeactivationOwner:     deactivationOwner,
		StartTime:             currentTimestampUnix,
		Weight:                msg.Weight,
		MinNonce:              0,
		EndAccumulatedFee:     0, // If Balance is 0, this is 0
	}
	if tx.Balance != 0 {
		// We are attempting to add an active validator
		if gas.Gas(e.State.NumActiveSubnetOnlyValidators()) >= e.Backend.Config.ValidatorFeeCapacity {
			return errMaxNumActiveValidators
		}

		currentFees := e.State.GetAccruedFees()
		sov.EndAccumulatedFee, err = safemath.Add(tx.Balance, currentFees)
		if err != nil {
			return err
		}
	}

	if err := e.State.PutSubnetOnlyValidator(sov); err != nil {
		return err
	}

	txID := e.Tx.ID()

	// Consume the UTXOS
	avax.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	avax.Produce(e.State, txID, tx.Outs)
	// Prevent this warp message from being replayed
	e.State.PutExpiry(expiry)
	return nil
}

func (e *StandardTxExecutor) SetSubnetValidatorWeightTx(tx *txs.SetSubnetValidatorWeightTx) error {
	var (
		currentTimestamp = e.State.GetTimestamp()
		upgrades         = e.Backend.Config.UpgradeConfig
	)
	if !upgrades.IsEtnaActivated(currentTimestamp) {
		return errEtnaUpgradeNotActive
	}

	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	if err := avax.VerifyMemoFieldLength(tx.Memo, true /*=isDurangoActive*/); err != nil {
		return err
	}

	// Verify the flowcheck
	fee, err := e.FeeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}

	if err := e.Backend.FlowChecker.VerifySpend(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		e.Tx.Creds,
		map[ids.ID]uint64{
			e.Ctx.AVAXAssetID: fee,
		},
	); err != nil {
		return err
	}

	warpMessage, err := warp.ParseMessage(tx.Message)
	if err != nil {
		return err
	}
	if warpMessage.NetworkID != e.Ctx.NetworkID {
		return fmt.Errorf("expected networkID %d but got %d", e.Ctx.NetworkID, warpMessage.NetworkID)
	}

	addressedCall, err := payload.ParseAddressedCall(warpMessage.Payload)
	if err != nil {
		return err
	}

	msg, err := message.ParseSubnetValidatorWeight(addressedCall.Payload)
	if err != nil {
		return err
	}
	if err := msg.Verify(); err != nil {
		return err
	}

	sov, err := e.State.GetSubnetOnlyValidator(msg.ValidationID)
	if err != nil {
		return err
	}
	if msg.Nonce < sov.MinNonce {
		return fmt.Errorf("expected nonce to be at least %d but got %d", sov.MinNonce, msg.Nonce)
	}

	_, expectedChainID, expectedAddress, err := e.State.GetSubnetConversion(sov.SubnetID)
	if err != nil {
		return err
	}
	if warpMessage.SourceChainID != expectedChainID {
		return fmt.Errorf("expected chainID %s but got %s", expectedChainID, warpMessage.SourceChainID)
	}
	if !bytes.Equal(addressedCall.SourceAddress, expectedAddress) {
		return fmt.Errorf("expected address %s but got %s", expectedAddress, addressedCall.SourceAddress)
	}

	txID := e.Tx.ID()
	if msg.Weight == 0 && sov.EndAccumulatedFee != 0 {
		// If we are removing an active validator, we need to refund the
		// remaining balance.
		var remainingBalanceOwner message.PChainOwner
		if _, err := txs.Codec.Unmarshal(sov.RemainingBalanceOwner, &remainingBalanceOwner); err != nil {
			return err
		}

		accruedFees := e.State.GetAccruedFees()
		if sov.EndAccumulatedFee <= accruedFees {
			// This check should be unreachable. However, including it ensures
			// that AVAX can't get minted out of thin air due to state
			// corruption.
			return fmt.Errorf("%w: validator should have already been disabled", errStateCorruption)
		}
		remainingBalance := sov.EndAccumulatedFee - accruedFees

		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(len(tx.Outs)),
			},
			Asset: avax.Asset{
				ID: e.Ctx.AVAXAssetID,
			},
			Out: &secp256k1fx.TransferOutput{
				Amt: remainingBalance,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: remainingBalanceOwner.Threshold,
					Addrs:     remainingBalanceOwner.Addresses,
				},
			},
		}
		e.State.AddUTXO(utxo)
	}

	// If the weight is being set to 0, the validator is being removed and the
	// nonce doesn't matter.
	sov.MinNonce = msg.Nonce + 1
	sov.Weight = msg.Weight
	if err := e.State.PutSubnetOnlyValidator(sov); err != nil {
		return err
	}

	// Consume the UTXOS
	avax.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	avax.Produce(e.State, txID, tx.Outs)
	return nil
}

func (e *StandardTxExecutor) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	if err := verifyAddPermissionlessValidatorTx(
		e.Backend,
		e.FeeCalculator,
		e.State,
		e.Tx,
		tx,
	); err != nil {
		return err
	}

	if err := e.putStaker(tx); err != nil {
		return err
	}

	txID := e.Tx.ID()
	avax.Consume(e.State, tx.Ins)
	avax.Produce(e.State, txID, tx.Outs)

	if e.Config.PartialSyncPrimaryNetwork &&
		tx.Subnet == constants.PrimaryNetworkID &&
		tx.Validator.NodeID == e.Ctx.NodeID {
		e.Ctx.Log.Warn("verified transaction that would cause this node to become unhealthy",
			zap.String("reason", "primary network is not being fully synced"),
			zap.Stringer("txID", txID),
			zap.String("txType", "addPermissionlessValidator"),
			zap.Stringer("nodeID", tx.Validator.NodeID),
		)
	}

	return nil
}

func (e *StandardTxExecutor) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	if err := verifyAddPermissionlessDelegatorTx(
		e.Backend,
		e.FeeCalculator,
		e.State,
		e.Tx,
		tx,
	); err != nil {
		return err
	}

	if err := e.putStaker(tx); err != nil {
		return err
	}

	txID := e.Tx.ID()
	avax.Consume(e.State, tx.Ins)
	avax.Produce(e.State, txID, tx.Outs)
	return nil
}

// Verifies a [*txs.TransferSubnetOwnershipTx] and, if it passes, executes it on
// [e.State]. For verification rules, see [verifyTransferSubnetOwnershipTx].
// This transaction will result in the ownership of [tx.Subnet] being transferred
// to [tx.Owner].
func (e *StandardTxExecutor) TransferSubnetOwnershipTx(tx *txs.TransferSubnetOwnershipTx) error {
	err := verifyTransferSubnetOwnershipTx(
		e.Backend,
		e.FeeCalculator,
		e.State,
		e.Tx,
		tx,
	)
	if err != nil {
		return err
	}

	e.State.SetSubnetOwner(tx.Subnet, tx.Owner)

	txID := e.Tx.ID()
	avax.Consume(e.State, tx.Ins)
	avax.Produce(e.State, txID, tx.Outs)
	return nil
}

func (e *StandardTxExecutor) BaseTx(tx *txs.BaseTx) error {
	var (
		currentTimestamp = e.State.GetTimestamp()
		upgrades         = e.Backend.Config.UpgradeConfig
	)
	if !upgrades.IsDurangoActivated(currentTimestamp) {
		return ErrDurangoUpgradeNotActive
	}

	// Verify the tx is well-formed
	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	if err := avax.VerifyMemoFieldLength(tx.Memo, true /*=isDurangoActive*/); err != nil {
		return err
	}

	// Verify the flowcheck
	fee, err := e.FeeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}
	if err := e.FlowChecker.VerifySpend(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		e.Tx.Creds,
		map[ids.ID]uint64{
			e.Ctx.AVAXAssetID: fee,
		},
	); err != nil {
		return err
	}

	txID := e.Tx.ID()
	// Consume the UTXOS
	avax.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	avax.Produce(e.State, txID, tx.Outs)
	return nil
}

// Creates the staker as defined in [stakerTx] and adds it to [e.State].
func (e *StandardTxExecutor) putStaker(stakerTx txs.Staker) error {
	var (
		chainTime = e.State.GetTimestamp()
		txID      = e.Tx.ID()
		staker    *state.Staker
		err       error
	)

	if !e.Config.UpgradeConfig.IsDurangoActivated(chainTime) {
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
			currentSupply, err := e.State.GetCurrentSupply(subnetID)
			if err != nil {
				return err
			}

			rewards, err := GetRewardsCalculator(e.Backend, e.State, subnetID)
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

			e.State.SetCurrentSupply(subnetID, currentSupply+potentialReward)
		}

		staker, err = state.NewCurrentStaker(txID, stakerTx, chainTime, potentialReward)
	}
	if err != nil {
		return err
	}

	switch priority := staker.Priority; {
	case priority.IsCurrentValidator():
		if err := e.State.PutCurrentValidator(staker); err != nil {
			return err
		}
	case priority.IsCurrentDelegator():
		e.State.PutCurrentDelegator(staker)
	case priority.IsPendingValidator():
		if err := e.State.PutPendingValidator(staker); err != nil {
			return err
		}
	case priority.IsPendingDelegator():
		e.State.PutPendingDelegator(staker)
	default:
		return fmt.Errorf("staker %s, unexpected priority %d", staker.TxID, priority)
	}
	return nil
}
