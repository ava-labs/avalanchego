// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"sync"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/proto/pb/platformvm"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

const (
	ErrFailedToParseWarpAddressedCall = iota + 1
	ErrWarpAddressedCallHasSourceAddress
	ErrFailedToParseWarpAddressedCallPayload
	ErrUnsupportedWarpAddressedCallPayloadType

	ErrFailedToParseJustification
	ErrConversionDoesNotExist
	ErrMismatchedConversionID

	ErrInvalidJustificationType
	ErrFailedToParseSubnetID
	ErrMismatchedValidationID
	ErrValidationDoesNotExist
	ErrValidationExists
	ErrFailedToParseRegisterL1Validator
	ErrValidationCouldBeRegistered

	ErrImpossibleNonce
	ErrWrongNonce
	ErrWrongWeight

	ErrValidatorSetDiffInvalidHeightProgression
	ErrValidatorSetDiffInvalidPreviousTimestamp
	ErrValidatorSetDiffInvalidCurrentTimestamp
	ErrValidatorSetDiffMismatch
)

var _ acp118.Verifier = (*signatureRequestVerifier)(nil)

type signatureRequestVerifier struct {
	vdrsState validators.State
	stateLock sync.Locker
	state     state.State
	log       logging.Logger
}

func (s signatureRequestVerifier) Verify(
	ctx context.Context,
	unsignedMessage *warp.UnsignedMessage,
	justification []byte,
) *common.AppError {
	msg, err := payload.ParseAddressedCall(unsignedMessage.Payload)
	if err != nil {
		return &common.AppError{
			Code:    ErrFailedToParseWarpAddressedCall,
			Message: "failed to parse warp addressed call: " + err.Error(),
		}
	}
	if len(msg.SourceAddress) != 0 {
		return &common.AppError{
			Code:    ErrWarpAddressedCallHasSourceAddress,
			Message: "source address should be empty",
		}
	}

	payloadIntf, err := message.Parse(msg.Payload)
	if err != nil {
		return &common.AppError{
			Code:    ErrFailedToParseWarpAddressedCallPayload,
			Message: "failed to parse warp addressed call payload: " + err.Error(),
		}
	}

	switch payload := payloadIntf.(type) {
	case *message.SubnetToL1Conversion:
		return s.verifySubnetToL1Conversion(payload, justification)
	case *message.L1ValidatorRegistration:
		return s.verifyL1ValidatorRegistration(payload, justification)
	case *message.L1ValidatorWeight:
		return s.verifyL1ValidatorWeight(payload)
	case *message.ValidatorSetState:
		return s.verifyValidatorSetState(ctx, payload)
	case *message.ValidatorSetDiff:
		return s.verifyValidatorSetDiff(ctx, payload)
	default:
		return &common.AppError{
			Code:    ErrUnsupportedWarpAddressedCallPayloadType,
			Message: fmt.Sprintf("unsupported warp addressed call payload type: %T", payloadIntf),
		}
	}
}

func (s signatureRequestVerifier) verifySubnetToL1Conversion(
	msg *message.SubnetToL1Conversion,
	justification []byte,
) *common.AppError {
	subnetID, err := ids.ToID(justification)
	if err != nil {
		return &common.AppError{
			Code:    ErrFailedToParseJustification,
			Message: "failed to parse justification: " + err.Error(),
		}
	}

	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	conversion, err := s.state.GetSubnetToL1Conversion(subnetID)
	if err == database.ErrNotFound {
		return &common.AppError{
			Code:    ErrConversionDoesNotExist,
			Message: fmt.Sprintf("subnet %q has not been converted", subnetID),
		}
	}
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to get subnet conversionID: " + err.Error(),
		}
	}

	if msg.ID != conversion.ConversionID {
		return &common.AppError{
			Code:    ErrMismatchedConversionID,
			Message: fmt.Sprintf("provided conversionID %q != expected conversionID %q", msg.ID, conversion.ConversionID),
		}
	}

	return nil
}

func (s signatureRequestVerifier) verifyL1ValidatorRegistration(
	msg *message.L1ValidatorRegistration,
	justificationBytes []byte,
) *common.AppError {
	if msg.Registered {
		return s.verifyL1ValidatorRegistered(msg.ValidationID)
	}

	var justification platformvm.L1ValidatorRegistrationJustification
	if err := proto.Unmarshal(justificationBytes, &justification); err != nil {
		return &common.AppError{
			Code:    ErrFailedToParseJustification,
			Message: "failed to parse justification: " + err.Error(),
		}
	}

	switch preimage := justification.GetPreimage().(type) {
	case *platformvm.L1ValidatorRegistrationJustification_ConvertSubnetToL1TxData:
		return s.verifySubnetValidatorNotCurrentlyRegistered(msg.ValidationID, preimage.ConvertSubnetToL1TxData)
	case *platformvm.L1ValidatorRegistrationJustification_RegisterL1ValidatorMessage:
		return s.verifySubnetValidatorCanNotValidate(msg.ValidationID, preimage.RegisterL1ValidatorMessage)
	default:
		return &common.AppError{
			Code:    ErrInvalidJustificationType,
			Message: fmt.Sprintf("invalid justification type: %T", justification.Preimage),
		}
	}
}

// verifyL1ValidatorRegistered verifies that the validationID is currently a
// validator.
func (s signatureRequestVerifier) verifyL1ValidatorRegistered(
	validationID ids.ID,
) *common.AppError {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	// Verify that the validator exists
	_, err := s.state.GetL1Validator(validationID)
	if err == database.ErrNotFound {
		return &common.AppError{
			Code:    ErrValidationDoesNotExist,
			Message: fmt.Sprintf("validation %q does not exist", validationID),
		}
	}
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to get L1 validator: " + err.Error(),
		}
	}
	return nil
}

// verifySubnetValidatorNotCurrentlyRegistered verifies that the validationID
// could only correspond to a validator from a ConvertSubnetToL1Tx and that it
// is not currently a validator.
func (s signatureRequestVerifier) verifySubnetValidatorNotCurrentlyRegistered(
	validationID ids.ID,
	justification *platformvm.SubnetIDIndex,
) *common.AppError {
	subnetID, err := ids.ToID(justification.GetSubnetId())
	if err != nil {
		return &common.AppError{
			Code:    ErrFailedToParseSubnetID,
			Message: "failed to parse subnetID: " + err.Error(),
		}
	}

	justificationID := subnetID.Append(justification.GetIndex())
	if validationID != justificationID {
		return &common.AppError{
			Code:    ErrMismatchedValidationID,
			Message: fmt.Sprintf("validationID %q != justificationID %q", validationID, justificationID),
		}
	}

	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	// Verify that the provided subnetID has been converted.
	_, err = s.state.GetSubnetToL1Conversion(subnetID)
	if err == database.ErrNotFound {
		return &common.AppError{
			Code:    ErrConversionDoesNotExist,
			Message: fmt.Sprintf("subnet %q has not been converted", subnetID),
		}
	}
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to get subnet conversionID: " + err.Error(),
		}
	}

	// Verify that the validator is not in the current state
	_, err = s.state.GetL1Validator(validationID)
	if err == nil {
		return &common.AppError{
			Code:    ErrValidationExists,
			Message: fmt.Sprintf("validation %q exists", validationID),
		}
	}
	if err != database.ErrNotFound {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to lookup L1 validator: " + err.Error(),
		}
	}

	// Either the validator was removed or it was never registered as part of
	// the subnet conversion.
	return nil
}

// verifySubnetValidatorCanNotValidate verifies that the validationID is not
// currently and can never become a validator.
func (s signatureRequestVerifier) verifySubnetValidatorCanNotValidate(
	validationID ids.ID,
	justificationBytes []byte,
) *common.AppError {
	justification, err := message.ParseRegisterL1Validator(justificationBytes)
	if err != nil {
		return &common.AppError{
			Code:    ErrFailedToParseRegisterL1Validator,
			Message: "failed to parse RegisterL1Validator justification: " + err.Error(),
		}
	}

	justificationID := justification.ValidationID()
	if validationID != justificationID {
		return &common.AppError{
			Code:    ErrMismatchedValidationID,
			Message: fmt.Sprintf("validationID %q != justificationID %q", validationID, justificationID),
		}
	}

	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	// Verify that the validator does not currently exist
	_, err = s.state.GetL1Validator(validationID)
	if err == nil {
		return &common.AppError{
			Code:    ErrValidationExists,
			Message: fmt.Sprintf("validation %q exists", validationID),
		}
	}
	if err != database.ErrNotFound {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to lookup L1 validator: " + err.Error(),
		}
	}

	currentTimeUnix := uint64(s.state.GetTimestamp().Unix())
	if justification.Expiry <= currentTimeUnix {
		return nil // The expiry time has passed
	}

	// If the validation ID was successfully registered and then removed, it can
	// never be re-used again even if its expiry has not yet passed.
	hasExpiry, err := s.state.HasExpiry(state.ExpiryEntry{
		Timestamp:    justification.Expiry,
		ValidationID: validationID,
	})
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to lookup expiry: " + err.Error(),
		}
	}
	if !hasExpiry {
		return &common.AppError{
			Code:    ErrValidationCouldBeRegistered,
			Message: fmt.Sprintf("validation %q can be registered until %d", validationID, justification.Expiry),
		}
	}

	return nil // The validator has been removed
}

func (s signatureRequestVerifier) verifyL1ValidatorWeight(
	msg *message.L1ValidatorWeight,
) *common.AppError {
	if msg.Nonce == math.MaxUint64 {
		return &common.AppError{
			Code:    ErrImpossibleNonce,
			Message: "impossible nonce",
		}
	}

	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	l1Validator, err := s.state.GetL1Validator(msg.ValidationID)
	switch {
	case err == database.ErrNotFound:
		// If the peer is attempting to verify that the weight of the validator
		// is 0, they should be requesting a [message.L1ValidatorRegistration]
		// with Registered set to false.
		return &common.AppError{
			Code:    ErrValidationDoesNotExist,
			Message: fmt.Sprintf("validation %q does not exist", msg.ValidationID),
		}
	case err != nil:
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to get L1 validator: " + err.Error(),
		}
	case msg.Nonce+1 != l1Validator.MinNonce:
		return &common.AppError{
			Code:    ErrWrongNonce,
			Message: fmt.Sprintf("provided nonce %d != expected nonce (%d - 1)", msg.Nonce, l1Validator.MinNonce),
		}
	case msg.Weight != l1Validator.Weight:
		return &common.AppError{
			Code:    ErrWrongWeight,
			Message: fmt.Sprintf("provided weight %d != expected weight %d", msg.Weight, l1Validator.Weight),
		}
	default:
		return nil // The nonce and weight are correct
	}
}

func (s signatureRequestVerifier) verifyValidatorSetState(
	ctx context.Context,
	msg *message.ValidatorSetState,
) *common.AppError {
	s.log.Debug("verifying validator set state",
		zap.Stringer("blockchainID", msg.BlockchainID),
		zap.Uint64("pChainHeight", msg.PChainHeight),
		zap.Stringer("validatorSetHash", msg.ValidatorSetHash),
	)

	// Check that the P-Chain height exists and is within the window of this node
	minHeight, err := s.vdrsState.GetMinimumHeight(ctx)
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to get minimum height: " + err.Error(),
		}
	}
	if msg.PChainHeight < minHeight {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: fmt.Sprintf("invalid height. provided %d. current minimum %d", msg.PChainHeight, minHeight),
		}
	}

	// Check that the blocktime stamp is correct for the given P-Chain height.
	blockID, err := s.state.GetBlockIDAtHeight(msg.PChainHeight)
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to get block ID at height: " + err.Error(),
		}
	}
	statelessBlock, err := s.state.GetStatelessBlock(blockID)
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to get block: " + err.Error(),
		}
	}
	banffBlock, ok := statelessBlock.(block.BanffBlock)
	if !ok {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "block is not a Banff block",
		}
	}

	blockTime := banffBlock.Timestamp()
	if msg.PChainTimestamp != uint64(blockTime.Unix()) {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: fmt.Sprintf("invalid block time. provided %d. expected %d", msg.PChainTimestamp, blockTime),
		}
	}

	// Get the validator set for the given blockchain ID at the given P-Chain height.
	canonicalValidatorSet, err := warp.GetCanonicalValidatorSetFromChainID(ctx, s.vdrsState, msg.PChainHeight, msg.BlockchainID)
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to get canonical validator set: " + err.Error(),
		}
	}

	// Check that the validator set hash is correct for the given blockchain ID at the given P-Chain height.
	validators := make([]*message.Validator, len(canonicalValidatorSet.Validators))
	for i, validator := range canonicalValidatorSet.Validators {
		validators[i] = &message.Validator{
			UncompressedPublicKeyBytes: [96]byte(validator.PublicKey.Serialize()),
			Weight:                     validator.Weight,
		}
	}
	bytes, err := message.Codec.Marshal(message.CodecVersion, validators)
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to marshal validator set: " + err.Error(),
		}
	}
	hash := sha256.Sum256(bytes)
	if msg.ValidatorSetHash != ids.ID(hash) {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: fmt.Sprintf("invalid validator set hash. provided %q. expected %q", msg.ValidatorSetHash, ids.ID(hash[:])),
		}
	}

	s.log.Info("validator set state verified",
		zap.Stringer("blockchainID", msg.BlockchainID),
		zap.Uint64("pChainHeight", msg.PChainHeight),
		zap.Stringer("validatorSetHash", msg.ValidatorSetHash),
	)

	return nil
}

func (s signatureRequestVerifier) verifyValidatorSetDiff(
	ctx context.Context,
	msg *message.ValidatorSetDiff,
) *common.AppError {
	s.log.Debug("verifying validator set diff",
		zap.Stringer("blockchainID", msg.BlockchainID),
		zap.Uint64("previousHeight", msg.PreviousHeight),
		zap.Uint64("currentHeight", msg.CurrentHeight),
	)

	// Verify height progression
	if msg.CurrentHeight <= msg.PreviousHeight {
		return &common.AppError{
			Code:    ErrValidatorSetDiffInvalidHeightProgression,
			Message: fmt.Sprintf("invalid height progression: current %d <= previous %d", msg.CurrentHeight, msg.PreviousHeight),
		}
	}

	// Check that both heights are within the window of this node
	minHeight, err := s.vdrsState.GetMinimumHeight(ctx)
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to get minimum height: " + err.Error(),
		}
	}
	if msg.PreviousHeight < minHeight {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: fmt.Sprintf("previous height %d below minimum %d", msg.PreviousHeight, minHeight),
		}
	}
	if msg.CurrentHeight < minHeight {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: fmt.Sprintf("current height %d below minimum %d", msg.CurrentHeight, minHeight),
		}
	}

	// Verify previous timestamp
	previousBlockID, err := s.state.GetBlockIDAtHeight(msg.PreviousHeight)
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to get previous block ID: " + err.Error(),
		}
	}
	previousBlock, err := s.state.GetStatelessBlock(previousBlockID)
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to get previous block: " + err.Error(),
		}
	}
	previousBanffBlock, ok := previousBlock.(block.BanffBlock)
	if !ok {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "previous block is not a Banff block",
		}
	}
	if msg.PreviousTimestamp != uint64(previousBanffBlock.Timestamp().Unix()) {
		return &common.AppError{
			Code:    ErrValidatorSetDiffInvalidPreviousTimestamp,
			Message: fmt.Sprintf("previous timestamp mismatch: provided %d, expected %d", msg.PreviousTimestamp, previousBanffBlock.Timestamp().Unix()),
		}
	}

	// Verify current timestamp
	currentBlockID, err := s.state.GetBlockIDAtHeight(msg.CurrentHeight)
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to get current block ID: " + err.Error(),
		}
	}
	currentBlock, err := s.state.GetStatelessBlock(currentBlockID)
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to get current block: " + err.Error(),
		}
	}
	currentBanffBlock, ok := currentBlock.(block.BanffBlock)
	if !ok {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "current block is not a Banff block",
		}
	}
	if msg.CurrentTimestamp != uint64(currentBanffBlock.Timestamp().Unix()) {
		return &common.AppError{
			Code:    ErrValidatorSetDiffInvalidCurrentTimestamp,
			Message: fmt.Sprintf("current timestamp mismatch: provided %d, expected %d", msg.CurrentTimestamp, currentBanffBlock.Timestamp().Unix()),
		}
	}

	// Get subnetID from blockchainID
	subnetID, err := s.vdrsState.GetSubnetID(ctx, msg.BlockchainID)
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to get subnet ID: " + err.Error(),
		}
	}

	// Get aggregated diffs directly from the database
	// This is more efficient than comparing full validator sets: O(d) vs O(n)
	s.log.Debug("fetching aggregated diffs from database",
		zap.Stringer("subnetID", subnetID),
		zap.Uint64("previousHeight", msg.PreviousHeight),
		zap.Uint64("currentHeight", msg.CurrentHeight),
	)

	s.stateLock.Lock()
	dbDiffs, err := s.state.GetAggregatedValidatorDiffs(ctx, subnetID, msg.PreviousHeight, msg.CurrentHeight)
	s.stateLock.Unlock()
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to get aggregated validator diffs: " + err.Error(),
		}
	}

	s.log.Debug("fetched aggregated diffs",
		zap.Int("dbDiffsCount", len(dbDiffs)),
		zap.Int("msgAdded", len(msg.Added)),
		zap.Int("msgRemoved", len(msg.Removed)),
		zap.Int("msgModified", len(msg.Modified)),
	)

	// Verify the message diffs match the database diffs
	if err := s.verifyDiffsMatchDB(msg, dbDiffs); err != nil {
		s.log.Debug("diff verification failed",
			zap.Error(err),
		)
		return &common.AppError{
			Code:    ErrValidatorSetDiffMismatch,
			Message: "diff verification failed: " + err.Error(),
		}
	}

	s.log.Info("validator set diff verified",
		zap.Stringer("blockchainID", msg.BlockchainID),
		zap.Uint64("previousHeight", msg.PreviousHeight),
		zap.Uint64("currentHeight", msg.CurrentHeight),
		zap.Int("added", len(msg.Added)),
		zap.Int("removed", len(msg.Removed)),
		zap.Int("modified", len(msg.Modified)),
	)

	return nil
}

// verifyDiffsMatchDB verifies that the message diffs match the aggregated diffs
// read directly from the P-chain consensus database. This is more efficient than
// comparing full validator sets: O(d) instead of O(n) where d << n.
//
// The verification works by comparing the NET WEIGHT CHANGE for each validator:
// - For additions: the message's CurrentWeight must equal the DB's increase amount
// - For removals: the message's PreviousWeight must equal the DB's decrease amount
// - For modifications: the net change (CurrentWeight - PreviousWeight) must match the DB
func (s signatureRequestVerifier) verifyDiffsMatchDB(
	msg *message.ValidatorSetDiff,
	dbDiffs map[ids.NodeID]*state.AggregatedValidatorDiff,
) error {
	// Build expected diffs from message to compare with DB diffs
	// We convert message diffs to net weight changes for comparison
	messageDiffs := make(map[ids.NodeID]struct {
		isIncrease bool   // true if weight increased, false if decreased
		amount     uint64 // magnitude of the change
	})

	// Process additions: weight increased from 0 to CurrentWeight
	for _, added := range msg.Added {
		if added.CurrentWeight == 0 {
			return fmt.Errorf("addition for %s has zero current weight", added.NodeID)
		}
		messageDiffs[added.NodeID] = struct {
			isIncrease bool
			amount     uint64
		}{
			isIncrease: true,
			amount:     added.CurrentWeight,
		}
	}

	// Process removals: weight decreased from PreviousWeight to 0
	for _, removed := range msg.Removed {
		if removed.PreviousWeight == 0 {
			return fmt.Errorf("removal for %s has zero previous weight", removed.NodeID)
		}
		if _, exists := messageDiffs[removed.NodeID]; exists {
			return fmt.Errorf("duplicate entry for %s in message", removed.NodeID)
		}
		messageDiffs[removed.NodeID] = struct {
			isIncrease bool
			amount     uint64
		}{
			isIncrease: false,
			amount:     removed.PreviousWeight,
		}
	}

	// Process modifications: weight changed from PreviousWeight to CurrentWeight
	for _, modified := range msg.Modified {
		if modified.PreviousWeight == modified.CurrentWeight {
			return fmt.Errorf("modification for %s has same previous and current weight", modified.NodeID)
		}
		if _, exists := messageDiffs[modified.NodeID]; exists {
			return fmt.Errorf("duplicate entry for %s in message", modified.NodeID)
		}

		var isIncrease bool
		var amount uint64
		if modified.CurrentWeight > modified.PreviousWeight {
			isIncrease = true
			amount = modified.CurrentWeight - modified.PreviousWeight
		} else {
			isIncrease = false
			amount = modified.PreviousWeight - modified.CurrentWeight
		}
		messageDiffs[modified.NodeID] = struct {
			isIncrease bool
			amount     uint64
		}{
			isIncrease: isIncrease,
			amount:     amount,
		}
	}

	// Verify counts match
	if len(messageDiffs) != len(dbDiffs) {
		return fmt.Errorf("diff count mismatch: message has %d changes, database has %d",
			len(messageDiffs), len(dbDiffs))
	}

	// Verify each message diff matches the corresponding DB diff
	for nodeID, msgDiff := range messageDiffs {
		dbDiff, exists := dbDiffs[nodeID]
		if !exists {
			s.log.Debug("diff comparison failed: no DB diff",
				zap.Stringer("nodeID", nodeID),
				zap.Bool("msgIsIncrease", msgDiff.isIncrease),
				zap.Uint64("msgAmount", msgDiff.amount),
			)
			return fmt.Errorf("message contains change for %s but database has no diff", nodeID)
		}

		// Convert DB diff to comparable format
		// DB stores: CurrentWeight = increase amount (if !Decrease), PreviousWeight = decrease amount (if Decrease)
		var dbIsIncrease bool
		var dbAmount uint64
		if dbDiff.CurrentWeight > 0 && dbDiff.PreviousWeight == 0 {
			// Weight increased
			dbIsIncrease = true
			dbAmount = dbDiff.CurrentWeight
		} else if dbDiff.PreviousWeight > 0 && dbDiff.CurrentWeight == 0 {
			// Weight decreased
			dbIsIncrease = false
			dbAmount = dbDiff.PreviousWeight
		} else {
			s.log.Debug("invalid DB diff format",
				zap.Stringer("nodeID", nodeID),
				zap.Uint64("dbPreviousWeight", dbDiff.PreviousWeight),
				zap.Uint64("dbCurrentWeight", dbDiff.CurrentWeight),
			)
			return fmt.Errorf("invalid DB diff for %s: previousWeight=%d, currentWeight=%d",
				nodeID, dbDiff.PreviousWeight, dbDiff.CurrentWeight)
		}

		s.log.Debug("comparing diff",
			zap.Stringer("nodeID", nodeID),
			zap.Bool("msgIsIncrease", msgDiff.isIncrease),
			zap.Uint64("msgAmount", msgDiff.amount),
			zap.Bool("dbIsIncrease", dbIsIncrease),
			zap.Uint64("dbAmount", dbAmount),
		)

		// Compare direction and magnitude
		if msgDiff.isIncrease != dbIsIncrease {
			return fmt.Errorf("direction mismatch for %s: message says %s, database says %s",
				nodeID,
				directionString(msgDiff.isIncrease),
				directionString(dbIsIncrease))
		}
		if msgDiff.amount != dbAmount {
			return fmt.Errorf("amount mismatch for %s: message has %d, database has %d",
				nodeID, msgDiff.amount, dbAmount)
		}
	}

	s.log.Debug("all diffs verified successfully",
		zap.Int("count", len(messageDiffs)),
	)

	return nil
}

func directionString(isIncrease bool) string {
	if isIncrease {
		return "increase"
	}
	return "decrease"
}
