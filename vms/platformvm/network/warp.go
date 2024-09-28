// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

const (
	ErrFailedToParseRequest = iota + 1
	ErrFailedToParseWarp
	ErrFailedToParseWarpAddressedCall
	ErrWarpAddressedCallHasSourceAddress
	ErrFailedToParseWarpAddressedCallPayload
	ErrUnsupportedWarpAddressedCallPayloadType
	ErrFailedToParseJustification
	ErrMismatchedValidationID
	ErrValidationDoesNotExist
	ErrValidationExists
	ErrValidationCouldBeRegistered
	ErrImpossibleNonce
	ErrWrongNonce
	ErrWrongWeight
	ErrFailedToSignWarp
	errUnimplemented // TODO: Remove
)

var _ p2p.Handler = (*signatureRequestHandler)(nil)

type signatureRequestHandler struct {
	p2p.NoOpHandler

	stateLock sync.Locker
	state     state.Chain

	signer warp.Signer
}

// TODO: This should be allowed only for local networks
func (s signatureRequestHandler) AppRequest(
	_ context.Context,
	_ ids.NodeID,
	_ time.Time,
	requestBytes []byte,
) ([]byte, *common.AppError) {
	// Per ACP-118, the requestBytes are the serialized form of
	// sdk.SignatureRequest.
	var req sdk.SignatureRequest
	if err := proto.Unmarshal(requestBytes, &req); err != nil {
		return nil, &common.AppError{
			Code:    ErrFailedToParseRequest,
			Message: "failed to unmarshal request: " + err.Error(),
		}
	}

	unsignedMessage, err := warp.ParseUnsignedMessage(req.Message)
	if err != nil {
		return nil, &common.AppError{
			Code:    ErrFailedToParseWarp,
			Message: "failed to parse warp message: " + err.Error(),
		}
	}

	msg, err := payload.ParseAddressedCall(unsignedMessage.Payload)
	if err != nil {
		return nil, &common.AppError{
			Code:    ErrFailedToParseWarpAddressedCall,
			Message: "failed to parse warp addressed call: " + err.Error(),
		}
	}
	if len(msg.SourceAddress) != 0 {
		return nil, &common.AppError{
			Code:    ErrWarpAddressedCallHasSourceAddress,
			Message: "source address should be empty",
		}
	}

	payloadIntf, err := message.Parse(msg.Payload)
	if err != nil {
		return nil, &common.AppError{
			Code:    ErrFailedToParseWarpAddressedCallPayload,
			Message: "failed to parse warp addressed call payload: " + err.Error(),
		}
	}

	var payloadErr *common.AppError
	switch payload := payloadIntf.(type) {
	case *message.SubnetConversion:
		payloadErr = s.verifySubnetConversion(payload, req.Justification)
	case *message.SubnetValidatorRegistration:
		payloadErr = s.verifySubnetValidatorRegistration(payload, req.Justification)
	case *message.SubnetValidatorWeight:
		payloadErr = s.verifySubnetValidatorWeight(payload)
	default:
		payloadErr = &common.AppError{
			Code:    ErrUnsupportedWarpAddressedCallPayloadType,
			Message: fmt.Sprintf("unsupported warp addressed call payload type: %T", payloadIntf),
		}
	}
	if payloadErr != nil {
		return nil, payloadErr
	}

	sig, err := s.signer.Sign(unsignedMessage)
	if err != nil {
		return nil, &common.AppError{
			Code:    ErrFailedToSignWarp,
			Message: "failed to sign warp message: " + err.Error(),
		}
	}

	// Per ACP-118, the responseBytes are the serialized form of
	// sdk.SignatureResponse.
	resp := &sdk.SignatureResponse{
		Signature: sig,
	}
	respBytes, err := proto.Marshal(resp)
	if err != nil {
		return nil, &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to marshal response: " + err.Error(),
		}
	}
	return respBytes, nil
}

func (s signatureRequestHandler) verifySubnetConversion(
	msg *message.SubnetConversion,
	justification []byte,
) *common.AppError {
	_ = s
	_ = msg
	_ = justification
	return &common.AppError{
		Code:    errUnimplemented,
		Message: "unimplemented",
	}
}

func (s signatureRequestHandler) verifySubnetValidatorRegistration(
	msg *message.SubnetValidatorRegistration,
	justification []byte,
) *common.AppError {
	var justificationID ids.ID = hashing.ComputeHash256Array(justification)
	if msg.ValidationID != justificationID {
		return &common.AppError{
			Code:    ErrMismatchedValidationID,
			Message: fmt.Sprintf("validationID %q != justificationID %q", msg.ValidationID, justificationID),
		}
	}

	registerSubnetValidator, err := message.ParseRegisterSubnetValidator(justification)
	if err != nil {
		return &common.AppError{
			Code:    ErrFailedToParseJustification,
			Message: "failed to parse justification: " + err.Error(),
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

func (s signatureRequestHandler) verifySubnetValidatorWeight(
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
			Code:    ErrWrongNonce,
			Message: fmt.Sprintf("provided weight %d != expected weight %d", msg.Weight, sov.Weight),
		}
	default:
		return nil // The nonce and weight are correct
	}
}
