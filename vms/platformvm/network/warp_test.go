// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/exp/rand"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/statetest"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

func TestSignatureRequestVerify(t *testing.T) {
	tests := []struct {
		name        string
		payload     []byte
		expectedErr *common.AppError
	}{
		{
			name:    "failed to parse warp addressed call",
			payload: nil,
			expectedErr: &common.AppError{
				Code:    ErrFailedToParseWarpAddressedCall,
				Message: "failed to parse warp addressed call: couldn't unpack codec version",
			},
		},
		{
			name: "warp addressed call has source address",
			payload: must[*payload.AddressedCall](t)(payload.NewAddressedCall(
				[]byte{1},
				nil,
			)).Bytes(),
			expectedErr: &common.AppError{
				Code:    ErrWarpAddressedCallHasSourceAddress,
				Message: "source address should be empty",
			},
		},
		{
			name: "failed to parse warp addressed call payload",
			payload: must[*payload.AddressedCall](t)(payload.NewAddressedCall(
				nil,
				nil,
			)).Bytes(),
			expectedErr: &common.AppError{
				Code:    ErrFailedToParseWarpAddressedCallPayload,
				Message: "failed to parse warp addressed call payload: couldn't unpack codec version",
			},
		},
		{
			name: "unsupported warp addressed call payload type",
			payload: must[*payload.AddressedCall](t)(payload.NewAddressedCall(
				nil,
				must[*message.RegisterSubnetValidator](t)(message.NewRegisterSubnetValidator(
					ids.GenerateTestID(),
					ids.GenerateTestNodeID(),
					[bls.PublicKeyLen]byte{},
					rand.Uint64(),
					message.PChainOwner{},
					message.PChainOwner{},
					rand.Uint64(),
				)).Bytes(),
			)).Bytes(),
			expectedErr: &common.AppError{
				Code:    ErrUnsupportedWarpAddressedCallPayloadType,
				Message: "unsupported warp addressed call payload type: *message.RegisterSubnetValidator",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := signatureRequestVerifier{}
			err := s.Verify(
				context.Background(),
				must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
					constants.UnitTestID,
					constants.PlatformChainID,
					test.payload,
				)),
				nil,
			)
			require.Equal(t, test.expectedErr, err)
		})
	}
}

func TestSignatureRequestVerifySubnetConversion(t *testing.T) {
	var (
		subnetID   = ids.GenerateTestID()
		conversion = state.SubnetConversion{
			ConversionID: ids.ID{1, 2, 3, 4, 5, 6, 7, 8},
			ChainID:      ids.GenerateTestID(),
			Addr:         utils.RandomBytes(20),
		}
		state = statetest.New(t, statetest.Config{})
		s     = signatureRequestVerifier{
			stateLock: &sync.Mutex{},
			state:     state,
		}
	)

	state.SetSubnetConversion(subnetID, conversion)

	tests := []struct {
		name         string
		subnetID     []byte
		conversionID ids.ID
		expectedErr  *common.AppError
	}{
		{
			name:     "failed to parse justification",
			subnetID: nil,
			expectedErr: &common.AppError{
				Code:    ErrFailedToParseJustification,
				Message: "failed to parse justification: invalid hash length: expected 32 bytes but got 0",
			},
		},
		{
			name:     "conversion does not exist",
			subnetID: ids.Empty[:],
			expectedErr: &common.AppError{
				Code:    ErrConversionDoesNotExist,
				Message: `subnet "11111111111111111111111111111111LpoYY" has not been converted`,
			},
		},
		{
			name:     "mismatched conversionID",
			subnetID: subnetID[:],
			expectedErr: &common.AppError{
				Code:    ErrMismatchedConversionID,
				Message: `provided conversionID "11111111111111111111111111111111LpoYY" != expected conversionID "SkB92YpWm4Jdy1AQvv4wMsUNbcoYBVZRqKkdz5yByq1bfdik"`,
			},
		},
		{
			name:         "valid",
			subnetID:     subnetID[:],
			conversionID: conversion.ConversionID,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := s.Verify(
				context.Background(),
				must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
					constants.UnitTestID,
					constants.PlatformChainID,
					must[*payload.AddressedCall](t)(payload.NewAddressedCall(
						nil,
						must[*message.SubnetConversion](t)(message.NewSubnetConversion(
							test.conversionID,
						)).Bytes(),
					)).Bytes(),
				)),
				test.subnetID,
			)
			require.Equal(t, test.expectedErr, err)
		})
	}
}

func TestSignatureRequestVerifySubnetValidatorRegistrationRegistered(t *testing.T) {
	sk, err := bls.NewSecretKey()
	require.NoError(t, err)

	var (
		sov = state.SubnetOnlyValidator{
			ValidationID: ids.GenerateTestID(),
			SubnetID:     ids.GenerateTestID(),
			NodeID:       ids.GenerateTestNodeID(),
			PublicKey:    bls.PublicKeyToUncompressedBytes(bls.PublicFromSecretKey(sk)),
			Weight:       1,
		}
		state = statetest.New(t, statetest.Config{})
		s     = signatureRequestVerifier{
			stateLock: &sync.Mutex{},
			state:     state,
		}
	)

	require.NoError(t, state.PutSubnetOnlyValidator(sov))

	tests := []struct {
		name         string
		validationID ids.ID
		expectedErr  *common.AppError
	}{
		{
			name: "validation does not exist",
			expectedErr: &common.AppError{
				Code:    ErrValidationDoesNotExist,
				Message: `validation "11111111111111111111111111111111LpoYY" does not exist`,
			},
		},
		{
			name:         "validation exists",
			validationID: sov.ValidationID,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := s.Verify(
				context.Background(),
				must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
					constants.UnitTestID,
					constants.PlatformChainID,
					must[*payload.AddressedCall](t)(payload.NewAddressedCall(
						nil,
						must[*message.SubnetValidatorRegistration](t)(message.NewSubnetValidatorRegistration(
							test.validationID,
							true,
						)).Bytes(),
					)).Bytes(),
				)),
				nil,
			)
			require.Equal(t, test.expectedErr, err)
		})
	}
}

func must[T any](t require.TestingT) func(T, error) T {
	return func(val T, err error) T {
		require.NoError(t, err)
		return val
	}
}
