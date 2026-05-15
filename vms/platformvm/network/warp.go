// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"bytes"
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

	// Bound expensive historical reconstruction by rejecting heights that
	// are below the node's minimum-retained window. Reads of GetWarpValidatorSets
	// for in-window heights are served by the cachedState wrapper installed
	// in vm.go on cache hit; on miss the underlying validators.Manager
	// performs makeAllValidatorSets (a diff walk from current state) and
	// the cachedState memoises the result for subsequent requests at the
	// same height.
	if appErr := s.verifyMinHeight(ctx, msg.PChainHeight); appErr != nil {
		return appErr
	}

	wset, appErr := s.lookupWarpValidatorSet(ctx, msg.BlockchainID, msg.PChainHeight, "current")
	if appErr != nil {
		return appErr
	}
	if appErr := verifyFullSetHash(canonicalValidatorsToMessage(wset), msg.ValidatorSetHash); appErr != nil {
		return appErr
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
	// Justification format:
	//   8 bytes  — subset format (shard_size)
	//   24 bytes — diff format   (shard_size + prev_height + prev_timestamp)
	var shardSize, prevHeight, prevTimestamp uint64
	isDiff := false
	switch len(justification) {
	case 8:
		shardSize = binary.BigEndian.Uint64(justification[:8])
	case 24:
		shardSize = binary.BigEndian.Uint64(justification[:8])
		prevHeight = binary.BigEndian.Uint64(justification[8:16])
		prevTimestamp = binary.BigEndian.Uint64(justification[16:24])
		isDiff = true
	default:
		return &common.AppError{
			Code:    ErrValidatorSetMetadataInvalidJustification,
			Message: fmt.Sprintf("justification must be 8 or 24 bytes, got %d bytes", len(justification)),
		}
	}
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
		zap.Bool("isDiff", isDiff),
		zap.Int("numShardHashes", len(msg.ShardHashes)),
	)

	if appErr := s.verifyMinHeight(ctx, msg.PChainHeight); appErr != nil {
		return appErr
	}

	if isDiff {
		return s.verifyValidatorSetMetadataDiff(ctx, msg, shardSize, prevHeight, prevTimestamp)
	}
	return s.verifyValidatorSetMetadataSubset(ctx, msg, shardSize)
}

func (s signatureRequestVerifier) verifyValidatorSetMetadataSubset(
	ctx context.Context,
	msg *message.ValidatorSetMetadata,
	shardSize uint64,
) *common.AppError {
	wset, appErr := s.lookupWarpValidatorSet(ctx, msg.BlockchainID, msg.PChainHeight, "current")
	if appErr != nil {
		return appErr
	}
	validators := canonicalValidatorsToMessage(wset)

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

	s.log.Info("validator set metadata (subset) verified",
		zap.Stringer("blockchainID", msg.BlockchainID),
		zap.Uint64("pChainHeight", msg.PChainHeight),
		zap.Int("numShards", len(msg.ShardHashes)),
	)
	return nil
}

func (s signatureRequestVerifier) verifyValidatorSetMetadataDiff(
	ctx context.Context,
	msg *message.ValidatorSetMetadata,
	shardSize uint64,
	prevHeight uint64,
	prevTimestamp uint64,
) *common.AppError {
	// Look up canonical validator sets at both heights via the cached
	// read-through GetWarpValidatorSets path. prevHeight == 0 means first
	// registration; treat the previous set as empty without doing a lookup.
	var prevVdrs []*message.Validator
	if prevHeight > 0 {
		prevSet, appErr := s.lookupWarpValidatorSet(ctx, msg.BlockchainID, prevHeight, "previous")
		if appErr != nil {
			return appErr
		}
		prevVdrs = canonicalValidatorsToMessage(prevSet)
	}
	currSet, appErr := s.lookupWarpValidatorSet(ctx, msg.BlockchainID, msg.PChainHeight, "current")
	if appErr != nil {
		return appErr
	}
	currVdrs := canonicalValidatorsToMessage(currSet)

	changes := computeValidatorChanges(prevVdrs, currVdrs)

	// Shard the changes into ValidatorSetDiff messages, computing per-shard
	// numAdded using the running set (mirrors relayer shardDiff logic).
	ss := int(shardSize)
	numChanges := len(changes)
	numShards := (numChanges + ss - 1) / ss
	if numShards == 0 {
		numShards = 1
	}

	if len(msg.ShardHashes) != numShards {
		return &common.AppError{
			Code:    ErrValidatorSetMetadataShardCountMismatch,
			Message: fmt.Sprintf("shard count mismatch: message has %d, expected %d (changes=%d, shardSize=%d)", len(msg.ShardHashes), numShards, numChanges, shardSize),
		}
	}

	existingKeys := make(map[[96]byte]struct{}, len(prevVdrs))
	for _, v := range prevVdrs {
		existingKeys[v.UncompressedPublicKeyBytes] = struct{}{}
	}

	for i := 0; i < numShards; i++ {
		start := i * ss
		end := start + ss
		if end > numChanges {
			end = numChanges
		}
		shardChanges := changes[start:end]

		var shardNumAdded uint32
		for _, c := range shardChanges {
			if c.Weight > 0 {
				if _, exists := existingKeys[c.UncompressedPublicKeyBytes]; !exists {
					shardNumAdded++
				}
			}
		}

		// Update existingKeys for the next shard.
		for _, c := range shardChanges {
			if c.Weight == 0 {
				delete(existingKeys, c.UncompressedPublicKeyBytes)
			} else {
				existingKeys[c.UncompressedPublicKeyBytes] = struct{}{}
			}
		}

		diff, err := message.NewValidatorSetDiff(
			msg.BlockchainID,
			prevHeight,
			prevTimestamp,
			msg.PChainHeight,
			msg.PChainTimestamp,
			shardChanges,
			shardNumAdded,
		)
		if err != nil {
			return &common.AppError{
				Code:    common.ErrUndefined.Code,
				Message: fmt.Sprintf("failed to create ValidatorSetDiff for shard %d: %s", i, err),
			}
		}

		hash := sha256.Sum256(diff.Bytes())
		if msg.ShardHashes[i] != ids.ID(hash) {
			return &common.AppError{
				Code:    ErrValidatorSetMetadataShardHashMismatch,
				Message: fmt.Sprintf("shard %d hash mismatch: provided %q, expected %q", i, msg.ShardHashes[i], ids.ID(hash)),
			}
		}
	}

	s.log.Info("validator set metadata (diff) verified",
		zap.Stringer("blockchainID", msg.BlockchainID),
		zap.Uint64("prevHeight", prevHeight),
		zap.Uint64("pChainHeight", msg.PChainHeight),
		zap.Int("numChanges", numChanges),
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

	minHeight, err := s.vdrsState.GetMinimumHeight(ctx)
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to get minimum height: " + err.Error(),
		}
	}
	if msg.PreviousHeight < minHeight {
		return &common.AppError{
			Code:    ErrValidatorSetDiffInvalidHeightProgression,
			Message: fmt.Sprintf("previous height %d is below minimum retained height %d", msg.PreviousHeight, minHeight),
		}
	}

	// Look up canonical validator sets at both heights via the cached
	// read-through GetWarpValidatorSets path; the diff is then computed
	// exactly in memory by sorted merge-walk. Using the full sets at both
	// heights lets us verify each per-key change exactly without a
	// dedicated diff-iterator path.
	prevSet, appErr := s.lookupWarpValidatorSet(ctx, msg.BlockchainID, msg.PreviousHeight, "previous")
	if appErr != nil {
		return appErr
	}
	currSet, appErr := s.lookupWarpValidatorSet(ctx, msg.BlockchainID, msg.CurrentHeight, "current")
	if appErr != nil {
		return appErr
	}

	prevVdrs := canonicalValidatorsToMessage(prevSet)
	currVdrs := canonicalValidatorsToMessage(currSet)
	expectedChanges := computeValidatorChanges(prevVdrs, currVdrs)

	if len(msg.Changes) != len(expectedChanges) {
		return &common.AppError{
			Code:    ErrValidatorSetDiffMismatch,
			Message: fmt.Sprintf("diff count mismatch: message has %d changes, expected %d", len(msg.Changes), len(expectedChanges)),
		}
	}

	// expectedByKey maps every change's public key to its expected new
	// weight (0 for removals). prevSetKeys is used to count how many of
	// the changes are additions (i.e. were not present in prevVdrs).
	expectedByKey := make(map[[96]byte]uint64, len(expectedChanges))
	for _, c := range expectedChanges {
		expectedByKey[c.UncompressedPublicKeyBytes] = c.Weight
	}
	prevSetKeys := make(map[[96]byte]struct{}, len(prevVdrs))
	for _, v := range prevVdrs {
		prevSetKeys[v.UncompressedPublicKeyBytes] = struct{}{}
	}

	var expectedNumAdded uint32
	for _, c := range expectedChanges {
		if c.Weight == 0 {
			continue
		}
		if _, wasPresent := prevSetKeys[c.UncompressedPublicKeyBytes]; !wasPresent {
			expectedNumAdded++
		}
	}
	if msg.NumAdded != expectedNumAdded {
		return &common.AppError{
			Code:    ErrValidatorSetDiffMismatch,
			Message: fmt.Sprintf("numAdded mismatch: message has %d, expected %d", msg.NumAdded, expectedNumAdded),
		}
	}

	for _, change := range msg.Changes {
		expectedWeight, ok := expectedByKey[change.UncompressedPublicKeyBytes]
		if !ok {
			return &common.AppError{
				Code:    ErrValidatorSetDiffMismatch,
				Message: fmt.Sprintf("unexpected change for public key (no diff between heights %d and %d)", msg.PreviousHeight, msg.CurrentHeight),
			}
		}
		if change.Weight != expectedWeight {
			return &common.AppError{
				Code:    ErrValidatorSetDiffMismatch,
				Message: fmt.Sprintf("weight mismatch for change at heights %d -> %d: message has %d, expected %d", msg.PreviousHeight, msg.CurrentHeight, change.Weight, expectedWeight),
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

// ------------------------------------------------------------------
// Helpers shared by all three validator-set verify funcs.
// ------------------------------------------------------------------

// verifyMinHeight rejects requests whose [pChainHeight] is below the node's
// recently-accepted minimum-retained window. Bounds the worst-case work the
// downstream GetWarpValidatorSets call can be forced to do (a diff walk
// from current state back to [pChainHeight]).
func (s signatureRequestVerifier) verifyMinHeight(
	ctx context.Context,
	pChainHeight uint64,
) *common.AppError {
	minHeight, err := s.vdrsState.GetMinimumHeight(ctx)
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to get minimum height: " + err.Error(),
		}
	}
	if pChainHeight < minHeight {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: fmt.Sprintf("invalid height. provided %d. current minimum %d", pChainHeight, minHeight),
		}
	}
	return nil
}

// lookupWarpValidatorSet resolves [blockchainID] to its subnetID and returns
// the canonical warp validator set for that subnet at [pChainHeight] via
// the cached read-through GetWarpValidatorSets path. The cachedState
// wrapper installed in vm.go memoises the (height -> map[subnetID]WarpSet)
// payload so repeated signature requests at the same height (e.g. by the
// relayer's signature aggregator polling many P-chain validators) are
// served without re-walking validator weight/pubkey diffs.
//
// [label] is used purely for error formatting (e.g. "current" / "previous").
func (s signatureRequestVerifier) lookupWarpValidatorSet(
	ctx context.Context,
	blockchainID ids.ID,
	pChainHeight uint64,
	label string,
) (validators.WarpSet, *common.AppError) {
	subnetID, err := s.vdrsState.GetSubnetID(ctx, blockchainID)
	if err != nil {
		return validators.WarpSet{}, &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: fmt.Sprintf("failed to resolve subnetID for %s blockchainID %q: %s", label, blockchainID, err),
		}
	}
	sets, err := s.vdrsState.GetWarpValidatorSets(ctx, pChainHeight)
	if err != nil {
		return validators.WarpSet{}, &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: fmt.Sprintf("failed to get %s warp validator sets at height %d: %s", label, pChainHeight, err),
		}
	}
	wset, ok := sets[subnetID]
	if !ok {
		return validators.WarpSet{}, &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: fmt.Sprintf("%s warp validator set absent for blockchain %q (subnet %q) at height %d", label, blockchainID, subnetID, pChainHeight),
		}
	}
	return wset, nil
}

// canonicalValidatorsToMessage converts a canonical-ordered
// [validators.WarpSet] (as returned by GetWarpValidatorSets) into the
// canonical-ordered []*message.Validator slice used by warp validator-set
// messages. Ordering is preserved.
func canonicalValidatorsToMessage(ws validators.WarpSet) []*message.Validator {
	out := make([]*message.Validator, len(ws.Validators))
	for i, v := range ws.Validators {
		out[i] = &message.Validator{
			UncompressedPublicKeyBytes: [96]byte(v.PublicKey.Serialize()),
			Weight:                     v.Weight,
		}
	}
	return out
}

// computeValidatorChanges returns the canonical-ordered diff (additions,
// removals, weight modifications) between two canonical-ordered validator
// slices via a sorted merge-walk -- the same algorithm used by the relayer
// when constructing ValidatorSetDiff messages.
func computeValidatorChanges(prev, curr []*message.Validator) []message.ValidatorChange {
	var changes []message.ValidatorChange
	pi, ci := 0, 0
	for pi < len(prev) || ci < len(curr) {
		var cmp int
		switch {
		case pi >= len(prev):
			cmp = 1
		case ci >= len(curr):
			cmp = -1
		default:
			oldPK := prev[pi].UncompressedPublicKeyBytes
			newPK := curr[ci].UncompressedPublicKeyBytes
			cmp = bytes.Compare(oldPK[:], newPK[:])
		}
		switch {
		case cmp < 0:
			changes = append(changes, message.ValidatorChange{
				UncompressedPublicKeyBytes: prev[pi].UncompressedPublicKeyBytes,
				Weight:                     0,
			})
			pi++
		case cmp > 0:
			changes = append(changes, message.ValidatorChange{
				UncompressedPublicKeyBytes: curr[ci].UncompressedPublicKeyBytes,
				Weight:                     curr[ci].Weight,
			})
			ci++
		default:
			if prev[pi].Weight != curr[ci].Weight {
				changes = append(changes, message.ValidatorChange{
					UncompressedPublicKeyBytes: curr[ci].UncompressedPublicKeyBytes,
					Weight:                     curr[ci].Weight,
				})
			}
			pi++
			ci++
		}
	}
	return changes
}

// verifyFullSetHash recomputes sha256(codec(validators)) over a canonical
// validator slice and compares it to [expected].
func verifyFullSetHash(vdrs []*message.Validator, expected ids.ID) *common.AppError {
	hashBytes, err := message.Codec.Marshal(message.CodecVersion, vdrs)
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to marshal validator set: " + err.Error(),
		}
	}
	hash := sha256.Sum256(hashBytes)
	if expected != ids.ID(hash) {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: fmt.Sprintf("invalid validator set hash. provided %q. expected %q", expected, ids.ID(hash)),
		}
	}
	return nil
}
