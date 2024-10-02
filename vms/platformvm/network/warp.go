// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
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

	ErrMismatchedValidationID
	ErrValidationDoesNotExist
	ErrValidationExists
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

	conversionID, _, _, err := s.state.GetSubnetConversion(subnetID)
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

	if msg.ID != conversionID {
		return &common.AppError{
			Code:    ErrMismatchedConversionID,
			Message: fmt.Sprintf("provided conversionID %q != expected conversionID %q", msg.ID, conversionID),
		}
	}

	return nil
}

func (s signatureRequestVerifier) verifySubnetValidatorRegistration(
	msg *message.SubnetValidatorRegistration,
	justification []byte,
) *common.AppError {
	registerSubnetValidator, err := message.ParseRegisterSubnetValidator(justification)
	if err != nil {
		return &common.AppError{
			Code:    ErrFailedToParseJustification,
			Message: "failed to parse justification: " + err.Error(),
		}
	}

	justificationID := registerSubnetValidator.ValidationID()
	if msg.ValidationID != justificationID {
		return &common.AppError{
			Code:    ErrMismatchedValidationID,
			Message: fmt.Sprintf("validationID %q != justificationID %q", msg.ValidationID, justificationID),
		}
	}

	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	if msg.Registered {
		// Verify that the validator exists
		_, err := s.state.GetSubnetOnlyValidator(msg.ValidationID)
		if err == database.ErrNotFound {
			return &common.AppError{
				Code:    ErrValidationDoesNotExist,
				Message: fmt.Sprintf("validation %q does not exist", msg.ValidationID),
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

	// Verify that the validator does not and can never exists
	_, err = s.state.GetSubnetOnlyValidator(msg.ValidationID)
	if err == nil {
		return &common.AppError{
			Code:    ErrValidationExists,
			Message: fmt.Sprintf("validation %q exists", msg.ValidationID),
		}
	}
	if err != database.ErrNotFound {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to lookup subnet only validator: " + err.Error(),
		}
	}

	currentTimeUnix := uint64(s.state.GetTimestamp().Unix())
	if registerSubnetValidator.Expiry <= currentTimeUnix {
		// The validator is not registered and the expiry time has passed
		return nil
	}

	hasExpiry, err := s.state.HasExpiry(state.ExpiryEntry{
		Timestamp:    registerSubnetValidator.Expiry,
		ValidationID: msg.ValidationID,
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
			Message: fmt.Sprintf("validation %q can be registered until %d", msg.ValidationID, registerSubnetValidator.Expiry),
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
