// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"fmt"
	"math"
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/proto/pb/platformvm"
	"github.com/ava-labs/avalanchego/snow/engine/common"
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
	ErrFailedToParseRegisterSubnetValidator
	ErrValidationCouldBeRegistered

	ErrImpossibleNonce
	ErrWrongNonce
	ErrWrongWeight
)

var _ acp118.Verifier = (*signatureRequestVerifier)(nil)

type signatureRequestVerifier struct {
	stateLock sync.Locker
	state     state.Chain
}

func (s signatureRequestVerifier) Verify(
	_ context.Context,
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
	case *message.SubnetConversion:
		return s.verifySubnetConversion(payload, justification)
	case *message.SubnetValidatorRegistration:
		return s.verifySubnetValidatorRegistration(payload, justification)
	case *message.SubnetValidatorWeight:
		return s.verifySubnetValidatorWeight(payload)
	default:
		return &common.AppError{
			Code:    ErrUnsupportedWarpAddressedCallPayloadType,
			Message: fmt.Sprintf("unsupported warp addressed call payload type: %T", payloadIntf),
		}
	}
}

func (s signatureRequestVerifier) verifySubnetConversion(
	msg *message.SubnetConversion,
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

	conversion, err := s.state.GetSubnetConversion(subnetID)
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

func (s signatureRequestVerifier) verifySubnetValidatorRegistration(
	msg *message.SubnetValidatorRegistration,
	justificationBytes []byte,
) *common.AppError {
	if msg.Registered {
		return s.verifySubnetValidatorRegistered(msg.ValidationID)
	}

	var justification platformvm.SubnetValidatorRegistrationJustification
	if err := proto.Unmarshal(justificationBytes, &justification); err != nil {
		return &common.AppError{
			Code:    ErrFailedToParseJustification,
			Message: "failed to parse justification: " + err.Error(),
		}
	}

	switch preimage := justification.GetPreimage().(type) {
	case *platformvm.SubnetValidatorRegistrationJustification_ConvertSubnetTxData:
		return s.verifySubnetValidatorNotCurrentlyRegistered(msg.ValidationID, preimage.ConvertSubnetTxData)
	case *platformvm.SubnetValidatorRegistrationJustification_RegisterSubnetValidatorMessage:
		return s.verifySubnetValidatorCanNotValidate(msg.ValidationID, preimage.RegisterSubnetValidatorMessage)
	default:
		return &common.AppError{
			Code:    ErrInvalidJustificationType,
			Message: fmt.Sprintf("invalid justification type: %T", justification.Preimage),
		}
	}
}

// verifySubnetValidatorCanNotValidate verifies that the validationID is
// currently a validator.
func (s signatureRequestVerifier) verifySubnetValidatorRegistered(
	validationID ids.ID,
) *common.AppError {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	// Verify that the validator exists
	_, err := s.state.GetSubnetOnlyValidator(validationID)
	if err == database.ErrNotFound {
		return &common.AppError{
			Code:    ErrValidationDoesNotExist,
			Message: fmt.Sprintf("validation %q does not exist", validationID),
		}
	}
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to get subnet only validator: " + err.Error(),
		}
	}
	return nil
}

// verifySubnetValidatorCanNotValidate verifies that the validationID is not
// currently a validator.
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

	// Verify that the validator does not currently exist
	_, err = s.state.GetSubnetOnlyValidator(validationID)
	if err == nil {
		return &common.AppError{
			Code:    ErrValidationExists,
			Message: fmt.Sprintf("validation %q exists", validationID),
		}
	}
	if err != database.ErrNotFound {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to lookup subnet only validator: " + err.Error(),
		}
	}
	return nil
}

// verifySubnetValidatorCanNotValidate verifies that the validationID does not
// currently and can never become a validator.
func (s signatureRequestVerifier) verifySubnetValidatorCanNotValidate(
	validationID ids.ID,
	justificationBytes []byte,
) *common.AppError {
	justification, err := message.ParseRegisterSubnetValidator(justificationBytes)
	if err != nil {
		return &common.AppError{
			Code:    ErrFailedToParseRegisterSubnetValidator,
			Message: "failed to parse RegisterSubnetValidator justification: " + err.Error(),
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

	// Verify that the validator does not and can never exists
	_, err = s.state.GetSubnetOnlyValidator(validationID)
	if err == nil {
		return &common.AppError{
			Code:    ErrValidationExists,
			Message: fmt.Sprintf("validation %q exists", validationID),
		}
	}
	if err != database.ErrNotFound {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to lookup subnet only validator: " + err.Error(),
		}
	}

	currentTimeUnix := uint64(s.state.GetTimestamp().Unix())
	if justification.Expiry <= currentTimeUnix {
		// The validator is not registered and the expiry time has passed
		return nil
	}

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

func (s signatureRequestVerifier) verifySubnetValidatorWeight(
	msg *message.SubnetValidatorWeight,
) *common.AppError {
	if msg.Nonce == math.MaxUint64 {
		return &common.AppError{
			Code:    ErrImpossibleNonce,
			Message: "impossible nonce",
		}
	}

	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	sov, err := s.state.GetSubnetOnlyValidator(msg.ValidationID)
	switch {
	case err == database.ErrNotFound:
		return &common.AppError{
			Code:    ErrValidationDoesNotExist,
			Message: fmt.Sprintf("validation %q does not exist", msg.ValidationID),
		}
	case err != nil:
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to get subnet only validator: " + err.Error(),
		}
	case msg.Nonce+1 != sov.MinNonce:
		return &common.AppError{
			Code:    ErrWrongNonce,
			Message: fmt.Sprintf("provided nonce %d != expected nonce (%d - 1)", msg.Nonce, sov.MinNonce),
		}
	case msg.Weight != sov.Weight:
		return &common.AppError{
			Code:    ErrWrongWeight,
			Message: fmt.Sprintf("provided weight %d != expected weight %d", msg.Weight, sov.Weight),
		}
	default:
		return nil // The nonce and weight are correct
	}
}
