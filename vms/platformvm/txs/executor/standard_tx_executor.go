// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ platform.TxVisitor = (*standardTxExecutor)(nil)

	errEmptyNodeID                      = errors.New("validator nodeID cannot be empty")
	errMissingStartTimePreDurango       = errors.New("staker transactions must have a StartTime pre-Durango")
	errMaxStakeDurationTooLarge         = errors.New("max stake duration must be less than or equal to the global max stake duration")
	errMaxNumActiveValidators           = errors.New("already at the max number of active validators")
	errCouldNotLoadSubnetToL1Conversion = errors.New("could not load subnet conversion")
	errWrongWarpMessageSourceChainID    = errors.New("wrong warp message source chain ID")
	errWrongWarpMessageSourceAddress    = errors.New("wrong warp message source address")
	errWarpMessageExpired               = errors.New("warp message expired")
	errWarpMessageNotYetAllowed         = errors.New("warp message not yet allowed")
	errWarpMessageAlreadyIssued         = errors.New("warp message already issued")
	errCouldNotLoadL1Validator          = errors.New("could not load L1 validator")
	errWarpMessageContainsStaleNonce    = errors.New("warp message contains stale nonce")
	errRemovingLastValidator            = errors.New("attempting to remove the last L1 validator from a converted subnet")
	errStateCorruption                  = errors.New("state corruption")
)

// registerL1ValidatorTxExpiryWindow bounds how far in the future a
// RegisterL1ValidatorTx's warp message may expire.
//
// TODO: Before Etna, ensure that the maximum number of expiries to track is
// limited to a reasonable number by this window.
const (
	second                            = 1
	minute                            = 60 * second
	hour                              = 60 * minute
	day                               = 24 * hour
	registerL1ValidatorTxExpiryWindow = day
)

// StandardTx executes the standard transaction tx.
//
// state is modified to represent the state of the chain after the execution
// of tx.
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
	tx *platform.Tx,
	state *state.Diff,
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

// standardTxExecutor verifies a standard tx and applies its state changes.
// Each execution method first runs [verifyTx], then the state-dependent rules
// of its tx type.
//
// Proposal-only txs are rejected with errWrongTxType before any verification.
type standardTxExecutor struct {
	// inputs, to be filled before visitor methods are called
	backend       *Backend
	state         *state.Diff // state is expected to be modified
	feeCalculator fee.Calculator
	tx            *platform.Tx

	// outputs of visitor execution
	onAccept       func() // may be nil
	inputs         set.Set[ids.ID]
	atomicRequests map[ids.ID]*atomic.Requests // may be nil
}

func (*standardTxExecutor) AdvanceTimeTx(*platform.AdvanceTimeTx) error {
	return errWrongTxType
}

func (*standardTxExecutor) RewardValidatorTx(*platform.RewardValidatorTx) error {
	return errWrongTxType
}

func (e *standardTxExecutor) AddValidatorTx(tx *platform.AddValidatorTx) error {
	if err := verifyTx(e.backend, e.state.GetTimestamp(), e.tx); err != nil {
		return err
	}

	if err := verifyAddValidatorTx(e.backend, e.state, tx); err != nil {
		return err
	}

	// The empty node ID check must stay scoped to the standard (post-Banff)
	// execution path. Validators with the empty node ID were accepted
	// pre-Banff through proposal blocks, so this check can live neither in
	// [platform.AddValidatorTx] nor in verifyAddValidatorTx (both are also
	// exercised when replaying that history); enforcing it there would reject
	// accepted blocks during bootstrapping.
	if tx.Validator.NodeID == ids.EmptyNodeID {
		return errEmptyNodeID
	}

	if err := e.applySpend(e.tx.Creds); err != nil {
		return err
	}

	if err := e.putStaker(tx); err != nil {
		return err
	}

	txID := e.tx.ID()
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

func (e *standardTxExecutor) AddSubnetValidatorTx(tx *platform.AddSubnetValidatorTx) error {
	if err := verifyTx(e.backend, e.state.GetTimestamp(), e.tx); err != nil {
		return err
	}

	if err := verifyAddSubnetValidatorTx(e.backend, e.state, e.tx, tx); err != nil {
		return err
	}

	if err := e.applySpend(baseTxCreds(e.tx)); err != nil {
		return err
	}

	return e.putStaker(tx)
}

func (e *standardTxExecutor) AddDelegatorTx(tx *platform.AddDelegatorTx) error {
	if err := verifyTx(e.backend, e.state.GetTimestamp(), e.tx); err != nil {
		return err
	}

	if err := verifyAddDelegatorTx(e.backend, e.state, tx); err != nil {
		return err
	}

	if err := e.applySpend(e.tx.Creds); err != nil {
		return err
	}

	return e.putStaker(tx)
}

func (e *standardTxExecutor) CreateChainTx(tx *platform.CreateChainTx) error {
	if err := verifyTx(e.backend, e.state.GetTimestamp(), e.tx); err != nil {
		return err
	}

	// Not bootstrapped yet -- don't need to do full verification.
	if e.backend.Bootstrapped.Get() {
		if err := verifyPoASubnetAuthorization(e.backend.Fx, e.state, e.tx, tx.SubnetID, tx.SubnetAuth); err != nil {
			return err
		}
	}

	if err := e.applySpend(baseTxCreds(e.tx)); err != nil {
		return err
	}

	txID := e.tx.ID()

	// Add the new chain to the database
	e.state.AddChain(e.tx)

	// If this proposal is committed and this node is a member of the subnet
	// that validates the blockchain, create the blockchain
	e.onAccept = func() {
		e.backend.Config.CreateChain(txID, tx)
	}
	return nil
}

func (e *standardTxExecutor) CreateSubnetTx(tx *platform.CreateSubnetTx) error {
	if err := verifyTx(e.backend, e.state.GetTimestamp(), e.tx); err != nil {
		return err
	}

	if err := e.applySpend(e.tx.Creds); err != nil {
		return err
	}

	txID := e.tx.ID()

	// Add the new subnet to the database
	e.state.AddSubnet(txID)
	e.state.SetSubnetOwner(txID, tx.Owner)
	return nil
}

func (e *standardTxExecutor) ImportTx(tx *platform.ImportTx) error {
	if err := verifyTx(e.backend, e.state.GetTimestamp(), e.tx); err != nil {
		return err
	}

	if err := e.verifyImportTx(tx); err != nil {
		return err
	}

	e.inputs = set.NewSet[ids.ID](len(tx.ImportedInputs))
	utxoIDs := make([][]byte, len(tx.ImportedInputs))
	for i, in := range tx.ImportedInputs {
		utxoID := in.UTXOID.InputID()

		e.inputs.Add(utxoID)
		utxoIDs[i] = utxoID[:]
	}

	// The imported inputs are not in e.state, so the local UTXOs are spent
	// directly rather than through [applySpend]. verifyImportTx performed the
	// flow check over both the local and the imported inputs.
	avax.Consume(e.state, tx.Ins)
	avax.Produce(e.state, e.tx.ID(), tx.Outs)

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

// verifyImportTx verifies that the imported UTXOs exist in shared memory and
// that, together with the local inputs of tx, they fund its outputs plus the
// fee. The flow check is performed here rather than through [applySpend]
// because [applySpend] does not account for the imported inputs.
func (e *standardTxExecutor) verifyImportTx(tx *platform.ImportTx) error {
	// Skip verification of the shared memory inputs if the other primary
	// network chains are not guaranteed to be up-to-date.
	if !e.backend.Bootstrapped.Get() || e.backend.Config.PartialSyncPrimaryNetwork {
		return nil
	}

	if err := verify.SameSubnet(context.TODO(), e.backend.Ctx, tx.SourceChain); err != nil {
		return err
	}

	utxoIDs := make([][]byte, len(tx.ImportedInputs))
	for i, in := range tx.ImportedInputs {
		utxoID := in.UTXOID.InputID()
		utxoIDs[i] = utxoID[:]
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
		if _, err := platform.Codec.Unmarshal(utxoBytes, utxo); err != nil {
			return fmt.Errorf("failed to unmarshal UTXO: %w", err)
		}
		utxos[i+len(tx.Ins)] = utxo
	}

	ins, outs, producedAVAX, err := utxo.GetInputOutputs(tx)
	if err != nil {
		return fmt.Errorf("getting utxos %w", err)
	}

	// Verify the flowcheck
	txFee, err := e.feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}

	producedAVAX, err = math.Add(producedAVAX, txFee)
	if err != nil {
		return fmt.Errorf("adding fee: %w", err)
	}

	if err := e.backend.FlowChecker.VerifySpendUTXOs(
		tx,
		utxos,
		ins,
		outs,
		e.tx.Creds,
		map[ids.ID]uint64{
			e.backend.Ctx.AVAXAssetID: producedAVAX,
		},
	); err != nil {
		return fmt.Errorf("%w: %w", errFlowCheckFailed, err)
	}
	return nil
}

func (e *standardTxExecutor) ExportTx(tx *platform.ExportTx) error {
	if err := verifyTx(e.backend, e.state.GetTimestamp(), e.tx); err != nil {
		return err
	}

	if e.backend.Bootstrapped.Get() {
		if err := verify.SameSubnet(context.TODO(), e.backend.Ctx, tx.DestinationChain); err != nil {
			return err
		}
	}

	if err := e.applySpend(e.tx.Creds); err != nil {
		return err
	}

	txID := e.tx.ID()

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

		utxoBytes, err := platform.Codec.Marshal(platform.CodecVersion, utxo)
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

// Verifies a [*platform.RemoveSubnetValidatorTx] and, if it passes, executes
// it on e.state. For verification rules, see [verifyRemoveSubnetValidatorTx].
// This transaction will result in tx.NodeID being removed as a validator of
// tx.Subnet.
// Note: tx.NodeID may be either a current or pending validator.
func (e *standardTxExecutor) RemoveSubnetValidatorTx(tx *platform.RemoveSubnetValidatorTx) error {
	if err := verifyTx(e.backend, e.state.GetTimestamp(), e.tx); err != nil {
		return err
	}

	staker, err := e.state.GetCurrentValidator(tx.Subnet, tx.NodeID)
	if err == database.ErrNotFound {
		staker, err = e.state.GetPendingValidator(tx.Subnet, tx.NodeID)
	}
	if err != nil {
		// It isn't a current or pending validator.
		return fmt.Errorf(
			"%s %w of %s: %w",
			tx.NodeID,
			errNotValidator,
			tx.Subnet,
			err,
		)
	}

	if !staker.Priority.IsPermissionedValidator() {
		return errRemovePermissionlessValidator
	}

	if e.backend.Bootstrapped.Get() {
		if err := verifySubnetAuthorization(e.backend.Fx, e.state, e.tx, tx.Subnet, tx.SubnetAuth); err != nil {
			return err
		}
	}

	if err := e.applySpend(baseTxCreds(e.tx)); err != nil {
		return err
	}

	if staker.Priority.IsCurrentValidator() {
		if err := e.state.DeleteCurrentValidator(staker); err != nil {
			return fmt.Errorf("deleting current validator: %w", err)
		}
	} else {
		e.state.DeletePendingValidator(staker)
	}

	// Invariant: There are no permissioned subnet delegators to remove.

	return nil
}

func (e *standardTxExecutor) TransformSubnetTx(tx *platform.TransformSubnetTx) error {
	if err := verifyTx(e.backend, e.state.GetTimestamp(), e.tx); err != nil {
		return err
	}

	// Note: math.MaxInt32 * time.Second < math.MaxInt64 - so this can never
	// overflow.
	if time.Duration(tx.MaxStakeDuration)*time.Second > e.backend.Config.MaxStakeDuration {
		return errMaxStakeDurationTooLarge
	}

	if e.backend.Bootstrapped.Get() {
		if err := verifyPoASubnetAuthorization(e.backend.Fx, e.state, e.tx, tx.Subnet, tx.SubnetAuth); err != nil {
			return err
		}
	}

	// The tx must additionally fund the reward supply of the subnet asset.
	totalRewardAmount := tx.MaximumSupply - tx.InitialSupply
	if err := applySpend(
		e.backend,
		e.feeCalculator,
		e.state,
		e.tx,
		baseTxCreds(e.tx),
		map[ids.ID]uint64{tx.AssetID: totalRewardAmount},
	); err != nil {
		return err
	}

	// Transform the new subnet in the database
	e.state.AddSubnetTransformation(e.tx)
	e.state.SetCurrentSupply(tx.Subnet, tx.InitialSupply)
	return nil
}

func (e *standardTxExecutor) AddPermissionlessValidatorTx(tx *platform.AddPermissionlessValidatorTx) error {
	if err := verifyTx(e.backend, e.state.GetTimestamp(), e.tx); err != nil {
		return err
	}

	if err := verifyAddPermissionlessValidatorTx(e.backend, e.state, tx); err != nil {
		return err
	}

	if err := e.applySpend(e.tx.Creds); err != nil {
		return err
	}

	if err := e.putStaker(tx); err != nil {
		return err
	}

	txID := e.tx.ID()

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

func (e *standardTxExecutor) AddPermissionlessDelegatorTx(tx *platform.AddPermissionlessDelegatorTx) error {
	if err := verifyTx(e.backend, e.state.GetTimestamp(), e.tx); err != nil {
		return err
	}

	if err := verifyAddPermissionlessDelegatorTx(e.backend, e.state, tx); err != nil {
		return err
	}

	if err := e.applySpend(e.tx.Creds); err != nil {
		return err
	}

	return e.putStaker(tx)
}

// Verifies a [*platform.TransferSubnetOwnershipTx] and, if it passes, executes
// it on e.state. For verification rules, see
// [verifyTransferSubnetOwnershipTx]. This transaction will result in the
// ownership of tx.Subnet being transferred to tx.Owner.
func (e *standardTxExecutor) TransferSubnetOwnershipTx(tx *platform.TransferSubnetOwnershipTx) error {
	if err := verifyTx(e.backend, e.state.GetTimestamp(), e.tx); err != nil {
		return err
	}

	if e.backend.Bootstrapped.Get() {
		if err := verifySubnetAuthorization(e.backend.Fx, e.state, e.tx, tx.Subnet, tx.SubnetAuth); err != nil {
			return err
		}
	}

	if err := e.applySpend(baseTxCreds(e.tx)); err != nil {
		return err
	}

	e.state.SetSubnetOwner(tx.Subnet, tx.Owner)
	return nil
}

func (e *standardTxExecutor) BaseTx(*platform.BaseTx) error {
	if err := verifyTx(e.backend, e.state.GetTimestamp(), e.tx); err != nil {
		return err
	}

	return e.applySpend(e.tx.Creds)
}

func (e *standardTxExecutor) ConvertSubnetToL1Tx(tx *platform.ConvertSubnetToL1Tx) error {
	if err := verifyTx(e.backend, e.state.GetTimestamp(), e.tx); err != nil {
		return err
	}

	// Not bootstrapped yet -- don't need to do full verification.
	if e.backend.Bootstrapped.Get() {
		if err := verifyPoASubnetAuthorization(e.backend.Fx, e.state, e.tx, tx.Subnet, tx.SubnetAuth); err != nil {
			return err
		}
	}

	var (
		currentTimestamp = e.state.GetTimestamp()
		startTime        = uint64(currentTimestamp.Unix())
		currentFees      = e.state.GetAccruedFees()
		// numActiveL1Validators simulates the number of active L1
		// validators as if the validators of this tx had already been
		// added: each validator with a non-zero balance activates exactly
		// one new validator. This is equivalent to the historical check
		// that ran against state between the individual additions.
		numActiveL1Validators    = gas.Gas(e.state.NumActiveL1Validators())
		l1Validators             = make([]state.L1Validator, len(tx.Validators))
		subnetToL1ConversionData = message.SubnetToL1ConversionData{
			SubnetID:       tx.Subnet,
			ManagerChainID: tx.ChainID,
			ManagerAddress: tx.Address,
			Validators:     make([]message.SubnetToL1ConversionValidatorData, len(tx.Validators)),
		}
	)
	for i, vdr := range tx.Validators {
		nodeID, err := ids.ToNodeID(vdr.NodeID)
		if err != nil {
			return err
		}

		remainingBalanceOwner, err := platform.Codec.Marshal(platform.CodecVersion, &vdr.RemainingBalanceOwner)
		if err != nil {
			return err
		}
		deactivationOwner, err := platform.Codec.Marshal(platform.CodecVersion, &vdr.DeactivationOwner)
		if err != nil {
			return err
		}

		l1Validator := state.L1Validator{
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
			if numActiveL1Validators >= e.backend.Config.ValidatorFeeConfig.Capacity {
				return errMaxNumActiveValidators
			}
			numActiveL1Validators++

			l1Validator.EndAccumulatedFee, err = math.Add(vdr.Balance, currentFees)
			if err != nil {
				return err
			}
		}

		l1Validators[i] = l1Validator

		subnetToL1ConversionData.Validators[i] = message.SubnetToL1ConversionValidatorData{
			NodeID:       vdr.NodeID,
			BLSPublicKey: vdr.Signer.PublicKey,
			Weight:       vdr.Weight,
		}
	}

	conversionID, err := message.SubnetToL1ConversionID(subnetToL1ConversionData)
	if err != nil {
		return err
	}

	if err := e.applySpend(baseTxCreds(e.tx)); err != nil {
		return err
	}

	for _, l1Validator := range l1Validators {
		if err := e.state.PutL1Validator(l1Validator); err != nil {
			return err
		}
	}

	// Track the subnet conversion in the database
	e.state.SetSubnetToL1Conversion(
		tx.Subnet,
		state.SubnetToL1Conversion{
			ConversionID: conversionID,
			ChainID:      tx.ChainID,
			Addr:         tx.Address,
		},
	)
	return nil
}

func (e *standardTxExecutor) RegisterL1ValidatorTx(tx *platform.RegisterL1ValidatorTx) error {
	if err := verifyTx(e.backend, e.state.GetTimestamp(), e.tx); err != nil {
		return err
	}

	// Parse the warp message.
	warpMessage, err := warp.ParseMessage(tx.Message)
	if err != nil {
		return err
	}
	addressedCall, err := payload.ParseAddressedCall(warpMessage.Payload)
	if err != nil {
		return err
	}
	msg, err := message.ParseRegisterL1Validator(addressedCall.Payload)
	if err != nil {
		return err
	}
	if err := msg.Verify(); err != nil {
		return err
	}

	// Verify that the warp message was sent from the expected chain and
	// address.
	if err := verifyL1Conversion(e.state, msg.SubnetID, warpMessage.SourceChainID, addressedCall.SourceAddress); err != nil {
		return err
	}

	// Verify that the message contains a valid expiry time.
	currentTimestamp := e.state.GetTimestamp()
	currentTimestampUnix := uint64(currentTimestamp.Unix())
	if msg.Expiry <= currentTimestampUnix {
		return fmt.Errorf("%w at %d and it is currently %d", errWarpMessageExpired, msg.Expiry, currentTimestampUnix)
	}
	if secondsUntilExpiry := msg.Expiry - currentTimestampUnix; secondsUntilExpiry > registerL1ValidatorTxExpiryWindow {
		return fmt.Errorf("%w because time is %d seconds in the future but the limit is %d", errWarpMessageNotYetAllowed, secondsUntilExpiry, registerL1ValidatorTxExpiryWindow)
	}

	// Verify that this warp message isn't being replayed.
	validationID := msg.ValidationID()
	expiry := state.ExpiryEntry{
		Timestamp:    msg.Expiry,
		ValidationID: validationID,
	}
	isDuplicate, err := e.state.HasExpiry(expiry)
	if err != nil {
		return err
	}
	if isDuplicate {
		return fmt.Errorf("%w for validationID %s", errWarpMessageAlreadyIssued, validationID)
	}

	// Verify proof of possession provided by the transaction against the public
	// key provided by the warp message.
	pop := signer.ProofOfPossession{
		PublicKey:         msg.BLSPublicKey,
		ProofOfPossession: tx.ProofOfPossession,
	}

	if err := pop.Verify(); err != nil {
		return err
	}

	// Create the L1 validator.
	nodeID, err := ids.ToNodeID(msg.NodeID)
	if err != nil {
		return err
	}
	remainingBalanceOwner, err := platform.Codec.Marshal(platform.CodecVersion, &msg.RemainingBalanceOwner)
	if err != nil {
		return err
	}
	deactivationOwner, err := platform.Codec.Marshal(platform.CodecVersion, &msg.DisableOwner)
	if err != nil {
		return err
	}
	l1Validator := state.L1Validator{
		ValidationID:          validationID,
		SubnetID:              msg.SubnetID,
		NodeID:                nodeID,
		PublicKey:             bls.PublicKeyToUncompressedBytes(pop.Key()),
		RemainingBalanceOwner: remainingBalanceOwner,
		DeactivationOwner:     deactivationOwner,
		StartTime:             currentTimestampUnix,
		Weight:                msg.Weight,
		MinNonce:              0,
		EndAccumulatedFee:     0, // If Balance is 0, this is will remain 0
	}

	// If the balance is non-zero, this validator should be initially active.
	if tx.Balance != 0 {
		// Verify that there is space for an active validator.
		if gas.Gas(e.state.NumActiveL1Validators()) >= e.backend.Config.ValidatorFeeConfig.Capacity {
			return errMaxNumActiveValidators
		}

		// Mark the validator as active.
		currentFees := e.state.GetAccruedFees()
		l1Validator.EndAccumulatedFee, err = math.Add(tx.Balance, currentFees)
		if err != nil {
			return err
		}
	}

	if err := e.applySpend(e.tx.Creds); err != nil {
		return err
	}

	if err := e.state.PutL1Validator(l1Validator); err != nil {
		return err
	}

	// Prevent this warp message from being replayed
	e.state.PutExpiry(expiry)
	return nil
}

func (e *standardTxExecutor) SetL1ValidatorWeightTx(tx *platform.SetL1ValidatorWeightTx) error {
	if err := verifyTx(e.backend, e.state.GetTimestamp(), e.tx); err != nil {
		return err
	}

	// Parse the warp message.
	warpMessage, err := warp.ParseMessage(tx.Message)
	if err != nil {
		return err
	}
	addressedCall, err := payload.ParseAddressedCall(warpMessage.Payload)
	if err != nil {
		return err
	}
	msg, err := message.ParseL1ValidatorWeight(addressedCall.Payload)
	if err != nil {
		return err
	}
	if err := msg.Verify(); err != nil {
		return err
	}

	// Verify that the message contains a valid nonce for a current validator.
	l1Validator, err := e.state.GetL1Validator(msg.ValidationID)
	if err != nil {
		return fmt.Errorf("%w: %w", errCouldNotLoadL1Validator, err)
	}
	if msg.Nonce < l1Validator.MinNonce {
		return fmt.Errorf("%w %d must be at least %d", errWarpMessageContainsStaleNonce, msg.Nonce, l1Validator.MinNonce)
	}

	// Verify that the warp message was sent from the expected chain and
	// address.
	if err := verifyL1Conversion(e.state, l1Validator.SubnetID, warpMessage.SourceChainID, addressedCall.SourceAddress); err != nil {
		return err
	}

	// Check if we are removing the validator.
	var refundUTXO *avax.UTXO
	if msg.Weight == 0 {
		// Verify that we are not removing the last validator.
		weight, err := e.state.WeightOfL1Validators(l1Validator.SubnetID)
		if err != nil {
			return fmt.Errorf("could not load L1 validator weights: %w", err)
		}
		if weight == l1Validator.Weight {
			return errRemovingLastValidator
		}

		// If the validator is currently active, we need to refund the remaining
		// balance.
		if l1Validator.EndAccumulatedFee != 0 {
			var remainingBalanceOwner message.PChainOwner
			if _, err := platform.Codec.Unmarshal(l1Validator.RemainingBalanceOwner, &remainingBalanceOwner); err != nil {
				return fmt.Errorf("%w: remaining balance owner is malformed", errStateCorruption)
			}

			accruedFees := e.state.GetAccruedFees()
			if l1Validator.EndAccumulatedFee <= accruedFees {
				// This check should be unreachable. However, it prevents AVAX
				// from being minted due to state corruption. This also prevents
				// invalid UTXOs from being created (with 0 value).
				return fmt.Errorf("%w: validator should have already been disabled", errStateCorruption)
			}
			remainingBalance := l1Validator.EndAccumulatedFee - accruedFees

			refundUTXO = &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        e.tx.ID(),
					OutputIndex: uint32(len(tx.Outs)),
				},
				Asset: avax.Asset{
					ID: e.backend.Ctx.AVAXAssetID,
				},
				Out: &secp256k1fx.TransferOutput{
					Amt: remainingBalance,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: remainingBalanceOwner.Threshold,
						Addrs:     remainingBalanceOwner.Addresses,
					},
				},
			}
		}
	}

	if err := e.applySpend(e.tx.Creds); err != nil {
		return err
	}

	if refundUTXO != nil {
		e.state.AddUTXO(refundUTXO)
	}

	// If the weight is being set to 0, it is possible for the nonce increment
	// to overflow. However, the validator is being removed and the nonce
	// doesn't matter. If weight is not 0, msg.Nonce is enforced by
	// msg.Verify() to be less than MaxUInt64 and can therefore be incremented
	// without overflow.
	l1Validator.MinNonce = msg.Nonce + 1
	l1Validator.Weight = msg.Weight
	return e.state.PutL1Validator(l1Validator)
}

func (e *standardTxExecutor) IncreaseL1ValidatorBalanceTx(tx *platform.IncreaseL1ValidatorBalanceTx) error {
	if err := verifyTx(e.backend, e.state.GetTimestamp(), e.tx); err != nil {
		return err
	}

	l1Validator, err := e.state.GetL1Validator(tx.ValidationID)
	if err != nil {
		return err
	}

	// If the validator is currently inactive, we are activating it.
	if l1Validator.EndAccumulatedFee == 0 {
		if gas.Gas(e.state.NumActiveL1Validators()) >= e.backend.Config.ValidatorFeeConfig.Capacity {
			return errMaxNumActiveValidators
		}

		l1Validator.EndAccumulatedFee = e.state.GetAccruedFees()
	}
	l1Validator.EndAccumulatedFee, err = math.Add(l1Validator.EndAccumulatedFee, tx.Balance)
	if err != nil {
		return err
	}

	if err := e.applySpend(e.tx.Creds); err != nil {
		return err
	}

	return e.state.PutL1Validator(l1Validator)
}

func (e *standardTxExecutor) DisableL1ValidatorTx(tx *platform.DisableL1ValidatorTx) error {
	if err := verifyTx(e.backend, e.state.GetTimestamp(), e.tx); err != nil {
		return err
	}

	l1Validator, err := e.state.GetL1Validator(tx.ValidationID)
	if err != nil {
		return fmt.Errorf("%w: %w", errCouldNotLoadL1Validator, err)
	}

	var disableOwner message.PChainOwner
	if _, err := platform.Codec.Unmarshal(l1Validator.DeactivationOwner, &disableOwner); err != nil {
		return err
	}

	if e.backend.Bootstrapped.Get() {
		if err := verifyAuthorization(
			e.backend.Fx,
			e.tx,
			&secp256k1fx.OutputOwners{
				Threshold: disableOwner.Threshold,
				Addrs:     disableOwner.Addresses,
			},
			tx.DisableAuth,
		); err != nil {
			return err
		}
	}

	// If the validator is already disabled, there is nothing to refund.
	if l1Validator.EndAccumulatedFee == 0 {
		return e.applySpend(baseTxCreds(e.tx))
	}

	var remainingBalanceOwner message.PChainOwner
	if _, err := platform.Codec.Unmarshal(l1Validator.RemainingBalanceOwner, &remainingBalanceOwner); err != nil {
		return err
	}

	accruedFees := e.state.GetAccruedFees()
	if l1Validator.EndAccumulatedFee <= accruedFees {
		// This check should be unreachable. However, including it ensures
		// that AVAX can't get minted out of thin air due to state
		// corruption.
		return fmt.Errorf("%w: validator should have already been disabled", errStateCorruption)
	}
	remainingBalance := l1Validator.EndAccumulatedFee - accruedFees

	if err := e.applySpend(baseTxCreds(e.tx)); err != nil {
		return err
	}

	e.state.AddUTXO(&avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        e.tx.ID(),
			OutputIndex: uint32(len(tx.Outs)),
		},
		Asset: avax.Asset{
			ID: e.backend.Ctx.AVAXAssetID,
		},
		Out: &secp256k1fx.TransferOutput{
			Amt: remainingBalance,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: remainingBalanceOwner.Threshold,
				Addrs:     remainingBalanceOwner.Addresses,
			},
		},
	})

	// Disable the validator
	l1Validator.EndAccumulatedFee = 0
	return e.state.PutL1Validator(l1Validator)
}

func (e *standardTxExecutor) AddAutoRenewedValidatorTx(tx *platform.AddAutoRenewedValidatorTx) error {
	if err := verifyTx(e.backend, e.state.GetTimestamp(), e.tx); err != nil {
		return err
	}

	if err := verifyAddAutoRenewedValidatorTx(e.backend, e.state, tx); err != nil {
		return err
	}

	if err := e.applySpend(e.tx.Creds); err != nil {
		return err
	}

	weight := tx.Weight()
	stakeStartTime := e.state.GetTimestamp()

	currentSupply, err := e.state.GetCurrentSupply(constants.PrimaryNetworkID)
	if err != nil {
		return fmt.Errorf("getting current supply: %w", err)
	}

	rewards, err := GetRewardsCalculator(
		e.backend.Config.RewardConfig,
		e.backend.Config.UpgradeConfig,
		e.state,
		constants.PrimaryNetworkID,
	)
	if err != nil {
		return fmt.Errorf("getting rewards calculator: %w", err)
	}

	duration := time.Duration(tx.Period) * time.Second
	potentialReward := rewards.Calculate(
		stakeStartTime,
		duration,
		weight,
		currentSupply,
	)

	newCurrentSupply, err := math.Add(currentSupply, potentialReward)
	if err != nil {
		return fmt.Errorf("adding current supply: %w", err)
	}
	e.state.SetCurrentSupply(constants.PrimaryNetworkID, newCurrentSupply)

	endTime := stakeStartTime.Add(duration)

	staker, err := state.NewCurrentStaker(
		e.tx.ID(),
		tx,
		stakeStartTime,
		endTime,
		weight,
		potentialReward,
	)
	if err != nil {
		return fmt.Errorf("creating staker: %w", err)
	}

	if err := e.state.PutCurrentValidator(staker); err != nil {
		return fmt.Errorf("putting current validator: %w", err)
	}

	stakingInfo := state.StakingInfo{
		AutoCompoundRewardShares: tx.AutoCompoundRewardShares,
		NextPeriod:               tx.Period,
	}
	if err := e.state.SetStakingInfo(staker.SubnetID, staker.NodeID, stakingInfo); err != nil {
		return fmt.Errorf("setting staking info: %w", err)
	}

	if e.backend.Config.PartialSyncPrimaryNetwork &&
		tx.NodeID() == e.backend.Ctx.NodeID {
		e.backend.Ctx.Log.Warn("verified transaction that would cause this node to become unhealthy",
			zap.String("reason", "primary network is not being fully synced"),
			zap.Stringer("txID", e.tx.ID()),
			zap.String("txType", "addAutoRenewedValidatorTx"),
			zap.Stringer("nodeID", tx.NodeID()),
		)
	}

	return nil
}

func (e *standardTxExecutor) SetAutoRenewedValidatorConfigTx(tx *platform.SetAutoRenewedValidatorConfigTx) error {
	if err := verifyTx(e.backend, e.state.GetTimestamp(), e.tx); err != nil {
		return err
	}

	validator, err := verifySetAutoRenewedValidatorConfigTx(e.backend, e.state, e.tx, tx)
	if err != nil {
		return err
	}

	if err := e.applySpend(baseTxCreds(e.tx)); err != nil {
		return err
	}

	stakingInfo, err := e.state.GetStakingInfo(validator.SubnetID, validator.NodeID)
	if err != nil {
		return fmt.Errorf("getting staking info: %w", err)
	}

	stakingInfo.AutoCompoundRewardShares = tx.AutoCompoundRewardShares
	stakingInfo.NextPeriod = tx.Period

	if err := e.state.SetStakingInfo(validator.SubnetID, validator.NodeID, stakingInfo); err != nil {
		return fmt.Errorf("setting staking info: %w", err)
	}

	return nil
}

func (*standardTxExecutor) RewardAutoRenewedValidatorTx(*platform.RewardAutoRenewedValidatorTx) error {
	return errWrongTxType
}

// Creates the staker as defined in stakerTx and adds it to e.state.
func (e *standardTxExecutor) putStaker(stakerTx platform.BoundedStaker) error {
	var (
		chainTime = e.state.GetTimestamp()
		txID      = e.tx.ID()
		staker    *state.Staker
		err       error
	)

	if !e.backend.Config.UpgradeConfig.IsDurangoActivated(chainTime) {
		// Pre-Durango, stakers set a future StartTime and are added to the
		// pending staker set. They are promoted to the current staker set once
		// the chain time reaches StartTime.
		scheduledStakerTx, ok := stakerTx.(platform.ScheduledStaker)
		if !ok {
			return fmt.Errorf("%w: %T", errMissingStartTimePreDurango, stakerTx)
		}
		staker, err = state.NewPendingStaker(txID, scheduledStakerTx)
	} else {
		// Post-Durango, stakers are immediately added to the current staker
		// set. Their StartTime is the current chain time.
		stakeStartTime := chainTime

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

			rewards, err := GetRewardsCalculator(
				e.backend.Config.RewardConfig,
				e.backend.Config.UpgradeConfig,
				e.state,
				subnetID,
			)
			if err != nil {
				return err
			}

			stakeDuration := stakerTx.EndTime().Sub(stakeStartTime)
			potentialReward = rewards.Calculate(
				stakeStartTime,
				stakeDuration,
				stakerTx.Weight(),
				currentSupply,
			)

			e.state.SetCurrentSupply(subnetID, currentSupply+potentialReward)
		}

		staker, err = state.NewCurrentStaker(
			txID,
			stakerTx,
			stakeStartTime,
			stakerTx.EndTime(),
			stakerTx.Weight(),
			potentialReward,
		)
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
		if err := e.state.PutCurrentDelegator(staker); err != nil {
			return fmt.Errorf("putting current delegator: %w", err)
		}
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

func (e *standardTxExecutor) applySpend(creds []verify.Verifiable) error {
	return applySpend(e.backend, e.feeCalculator, e.state, e.tx, creds, nil)
}

// verifyL1Conversion verifies that the L1 conversion of subnetID references
// the expectedChainID and expectedAddress.
func verifyL1Conversion(
	chainState state.Chain,
	subnetID ids.ID,
	expectedChainID ids.ID,
	expectedAddress []byte,
) error {
	subnetToL1Conversion, err := chainState.GetSubnetToL1Conversion(subnetID)
	if err != nil {
		return fmt.Errorf("%w for %s with: %w", errCouldNotLoadSubnetToL1Conversion, subnetID, err)
	}
	if expectedChainID != subnetToL1Conversion.ChainID {
		return fmt.Errorf("%w expected %s but had %s", errWrongWarpMessageSourceChainID, subnetToL1Conversion.ChainID, expectedChainID)
	}
	if !bytes.Equal(expectedAddress, subnetToL1Conversion.Addr) {
		return fmt.Errorf("%w expected 0x%x but got 0x%x", errWrongWarpMessageSourceAddress, subnetToL1Conversion.Addr, expectedAddress)
	}
	return nil
}
