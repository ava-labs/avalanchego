// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
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

	ErrValidatorSetMetadataInvalidJustification
	ErrValidatorSetMetadataShardCountMismatch
	ErrValidatorSetMetadataShardHashMismatch
)

var _ acp118.Verifier = (*signatureRequestVerifier)(nil)

type signatureRequestVerifier struct {
	vdrsState validators.State
	stateLock sync.Locker
	state     *state.State
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
	case *message.ValidatorSetMetadata:
		return s.verifyValidatorSetMetadata(ctx, payload, justification)
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
			Message: fmt.Sprintf("invalid block time. provided %d. expected %d", msg.PChainTimestamp, blockTime.Unix()),
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

func (s signatureRequestVerifier) verifyValidatorSetMetadata(
	ctx context.Context,
	msg *message.ValidatorSetMetadata,
	justification []byte,
) *common.AppError {
	if len(justification) != 8 {
		return &common.AppError{
			Code:    ErrValidatorSetMetadataInvalidJustification,
			Message: fmt.Sprintf("justification must be 8 bytes encoding shard size, got %d bytes", len(justification)),
		}
	}
	shardSize := binary.BigEndian.Uint64(justification)
	if shardSize == 0 {
		return &common.AppError{
			Code:    ErrValidatorSetMetadataInvalidJustification,
			Message: "shard size must be greater than zero",
		}
	}

	s.log.Debug("verifying validator set metadata",
		zap.Stringer("blockchainID", msg.BlockchainID),
		zap.Uint64("pChainHeight", msg.PChainHeight),
		zap.Uint64("shardSize", shardSize),
		zap.Int("numShardHashes", len(msg.ShardHashes)),
	)

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
			Message: fmt.Sprintf("invalid block time. provided %d. expected %d", msg.PChainTimestamp, blockTime.Unix()),
		}
	}

	canonicalValidatorSet, err := warp.GetCanonicalValidatorSetFromChainID(ctx, s.vdrsState, msg.PChainHeight, msg.BlockchainID)
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to get canonical validator set: " + err.Error(),
		}
	}

	validators := make([]*message.Validator, len(canonicalValidatorSet.Validators))
	for i, validator := range canonicalValidatorSet.Validators {
		validators[i] = &message.Validator{
			UncompressedPublicKeyBytes: [96]byte(validator.PublicKey.Serialize()),
			Weight:                     validator.Weight,
		}
	}

	numValidators := uint64(len(validators))
	numShards := (numValidators + shardSize - 1) / shardSize
	if numShards == 0 {
		numShards = 1
	}

	if uint64(len(msg.ShardHashes)) != numShards {
		return &common.AppError{
			Code:    ErrValidatorSetMetadataShardCountMismatch,
			Message: fmt.Sprintf("shard count mismatch: message has %d, expected %d (validators=%d, shardSize=%d)", len(msg.ShardHashes), numShards, numValidators, shardSize),
		}
	}

	for i := uint64(0); i < numShards; i++ {
		start := i * shardSize
		end := start + shardSize
		if end > numValidators {
			end = numValidators
		}
		shard := validators[start:end]
		shardBytes, err := message.Codec.Marshal(message.CodecVersion, shard)
		if err != nil {
			return &common.AppError{
				Code:    common.ErrUndefined.Code,
				Message: fmt.Sprintf("failed to marshal shard %d: %s", i, err),
			}
		}
		hash := sha256.Sum256(shardBytes)
		if msg.ShardHashes[i] != ids.ID(hash) {
			return &common.AppError{
				Code:    ErrValidatorSetMetadataShardHashMismatch,
				Message: fmt.Sprintf("shard %d hash mismatch: provided %q, expected %q", i, msg.ShardHashes[i], ids.ID(hash)),
			}
		}
	}

	s.log.Info("validator set metadata verified",
		zap.Stringer("blockchainID", msg.BlockchainID),
		zap.Uint64("pChainHeight", msg.PChainHeight),
		zap.Int("numShards", len(msg.ShardHashes)),
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

	if msg.CurrentHeight <= msg.PreviousHeight {
		return &common.AppError{
			Code:    ErrValidatorSetDiffInvalidHeightProgression,
			Message: fmt.Sprintf("invalid height progression: current %d <= previous %d", msg.CurrentHeight, msg.PreviousHeight),
		}
	}

	if appErr := s.verifyBlockTimestamp(msg.PreviousHeight, msg.PreviousTimestamp, ErrValidatorSetDiffInvalidPreviousTimestamp, "previous"); appErr != nil {
		return appErr
	}
	if appErr := s.verifyBlockTimestamp(msg.CurrentHeight, msg.CurrentTimestamp, ErrValidatorSetDiffInvalidCurrentTimestamp, "current"); appErr != nil {
		return appErr
	}

	subnetID, err := s.vdrsState.GetSubnetID(ctx, msg.BlockchainID)
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to get subnet ID: " + err.Error(),
		}
	}

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
		zap.Int("msgChanges", len(msg.Changes)),
		zap.Uint32("msgNumAdded", msg.NumAdded),
	)

	if len(msg.Changes) != len(dbDiffs) {
		return &common.AppError{
			Code:    ErrValidatorSetDiffMismatch,
			Message: fmt.Sprintf("diff count mismatch: message has %d changes, database has %d", len(msg.Changes), len(dbDiffs)),
		}
	}

	// Additions and removals have PublicKey set in the DB diff, so we can
	// verify them exactly by public key. Weight-only modifications (both
	// PreviousWeight and CurrentWeight > 0) may not have the public key
	// stored, so we verify those in aggregate (count + weight multiset).
	type expectedDiff struct {
		isAddition bool
		weight     uint64
	}
	expectedByKey := make(map[[96]byte]*expectedDiff, len(dbDiffs))
	var expectedNumAdded uint32

	// expectedWeightMods tracks the multiset of CurrentWeights for
	// weight-only modifications (keyed by weight, value is count).
	expectedWeightMods := make(map[uint64]int)
	var expectedWeightModCount int

	for _, diff := range dbDiffs {
		switch {
		case diff.PreviousWeight == 0 && diff.CurrentWeight > 0:
			// Addition
			if len(diff.PublicKey) != 96 {
				return &common.AppError{
					Code:    common.ErrUndefined.Code,
					Message: "addition diff missing public key",
				}
			}
			var key [96]byte
			copy(key[:], diff.PublicKey)
			expectedByKey[key] = &expectedDiff{isAddition: true, weight: diff.CurrentWeight}
			expectedNumAdded++

		case diff.PreviousWeight > 0 && diff.CurrentWeight == 0:
			// Removal
			if len(diff.PublicKey) != 96 {
				return &common.AppError{
					Code:    common.ErrUndefined.Code,
					Message: "removal diff missing public key",
				}
			}
			var key [96]byte
			copy(key[:], diff.PublicKey)
			expectedByKey[key] = &expectedDiff{isAddition: false, weight: 0}

		default:
			// Weight-only modification: public key may not be available,
			// so verify in aggregate rather than per key.
			expectedWeightMods[diff.CurrentWeight]++
			expectedWeightModCount++
		}
	}

	if msg.NumAdded != expectedNumAdded {
		return &common.AppError{
			Code:    ErrValidatorSetDiffMismatch,
			Message: fmt.Sprintf("numAdded mismatch: message has %d, expected %d", msg.NumAdded, expectedNumAdded),
		}
	}

	// Walk message changes. Entries whose public key matches an addition or
	// removal are verified exactly. Remaining entries are collected and
	// compared against the weight-modification multiset.
	var msgWeightModCount int
	msgWeightMods := make(map[uint64]int)

	for _, change := range msg.Changes {
		exp, hasKey := expectedByKey[change.UncompressedPublicKeyBytes]
		if hasKey {
			if exp.isAddition {
				if change.Weight == 0 {
					return &common.AppError{
						Code:    ErrValidatorSetDiffMismatch,
						Message: "message shows removal but database shows addition",
					}
				}
				if change.Weight != exp.weight {
					return &common.AppError{
						Code:    ErrValidatorSetDiffMismatch,
						Message: fmt.Sprintf("addition weight mismatch: message has %d, database has %d", change.Weight, exp.weight),
					}
				}
			} else {
				if change.Weight != 0 {
					return &common.AppError{
						Code:    ErrValidatorSetDiffMismatch,
						Message: "message shows non-zero weight but database shows removal",
					}
				}
			}
			continue
		}

		// No public key match in additions/removals — must be a weight modification.
		if change.Weight == 0 {
			return &common.AppError{
				Code:    ErrValidatorSetDiffMismatch,
				Message: "message contains removal for unknown validator",
			}
		}
		msgWeightMods[change.Weight]++
		msgWeightModCount++
	}

	if msgWeightModCount != expectedWeightModCount {
		return &common.AppError{
			Code:    ErrValidatorSetDiffMismatch,
			Message: fmt.Sprintf("weight modification count mismatch: message has %d, database has %d", msgWeightModCount, expectedWeightModCount),
		}
	}
	for weight, count := range expectedWeightMods {
		if msgWeightMods[weight] != count {
			return &common.AppError{
				Code:    ErrValidatorSetDiffMismatch,
				Message: fmt.Sprintf("weight modification multiset mismatch for weight %d: message has %d, database has %d", weight, msgWeightMods[weight], count),
			}
		}
	}

	s.log.Info("validator set diff verified",
		zap.Stringer("blockchainID", msg.BlockchainID),
		zap.Uint64("previousHeight", msg.PreviousHeight),
		zap.Uint64("currentHeight", msg.CurrentHeight),
		zap.Int("numChanges", len(msg.Changes)),
		zap.Uint32("numAdded", msg.NumAdded),
	)

	return nil
}

// verifyBlockTimestamp verifies that the block at the given height is a Banff
// block and that its timestamp matches the provided value.
func (s signatureRequestVerifier) verifyBlockTimestamp(
	height uint64,
	expectedTimestamp uint64,
	errCode int32,
	label string,
) *common.AppError {
	blockID, err := s.state.GetBlockIDAtHeight(height)
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: fmt.Sprintf("failed to get %s block ID: %s", label, err),
		}
	}
	statelessBlock, err := s.state.GetStatelessBlock(blockID)
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: fmt.Sprintf("failed to get %s block: %s", label, err),
		}
	}
	banffBlock, ok := statelessBlock.(block.BanffBlock)
	if !ok {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: fmt.Sprintf("%s block is not a Banff block", label),
		}
	}
	blockTime := uint64(banffBlock.Timestamp().Unix())
	if expectedTimestamp != blockTime {
		return &common.AppError{
			Code:    errCode,
			Message: fmt.Sprintf("%s timestamp mismatch: provided %d, expected %d", label, expectedTimestamp, blockTime),
		}
	}
	return nil
}
