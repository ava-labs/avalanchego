// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"encoding/hex"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/exp/rand"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/platformvm"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
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

func TestSignatureRequestVerifySubnetValidatorRegistrationNotRegistered(t *testing.T) {
	skBytes, err := hex.DecodeString("36a33c536d283dfa599d0a70839c67ded6c954e346c5e8e5b4794e2299907887")
	require.NoError(t, err)

	sk, err := bls.SecretKeyFromBytes(skBytes)
	require.NoError(t, err)

	var (
		subnetID        = ids.ID{3}
		nodeID0         = ids.NodeID{4}
		nodeID1         = ids.NodeID{5}
		nodeID2         = ids.NodeID{6}
		nodeID3         = ids.NodeID{6}
		pk              = bls.PublicFromSecretKey(sk)
		expiry          = genesistest.DefaultValidatorStartTimeUnix + 1
		weight   uint64 = 1
	)

	registerSubnetValidatorToRegister, err := message.NewRegisterSubnetValidator(
		subnetID,
		nodeID0,
		[bls.PublicKeyLen]byte(bls.PublicKeyToCompressedBytes(pk)),
		expiry,
		message.PChainOwner{},
		message.PChainOwner{},
		weight,
	)
	require.NoError(t, err)

	registerSubnetValidatorNotToRegister, err := message.NewRegisterSubnetValidator(
		subnetID,
		nodeID1,
		[bls.PublicKeyLen]byte(bls.PublicKeyToCompressedBytes(pk)),
		expiry,
		message.PChainOwner{},
		message.PChainOwner{},
		weight,
	)
	require.NoError(t, err)

	registerSubnetValidatorExpired, err := message.NewRegisterSubnetValidator(
		subnetID,
		nodeID2,
		[bls.PublicKeyLen]byte(bls.PublicKeyToCompressedBytes(pk)),
		genesistest.DefaultValidatorStartTimeUnix,
		message.PChainOwner{},
		message.PChainOwner{},
		weight,
	)
	require.NoError(t, err)

	registerSubnetValidatorToMarkExpired, err := message.NewRegisterSubnetValidator(
		subnetID,
		nodeID3,
		[bls.PublicKeyLen]byte(bls.PublicKeyToCompressedBytes(pk)),
		expiry,
		message.PChainOwner{},
		message.PChainOwner{},
		weight,
	)
	require.NoError(t, err)

	var (
		registerSubnetValidatorValidationID = registerSubnetValidatorToRegister.ValidationID()
		registerSubnetValidatorSoV          = state.SubnetOnlyValidator{
			ValidationID: registerSubnetValidatorValidationID,
			SubnetID:     registerSubnetValidatorToRegister.SubnetID,
			NodeID:       ids.NodeID(registerSubnetValidatorToRegister.NodeID),
			PublicKey:    bls.PublicKeyToUncompressedBytes(pk),
			Weight:       registerSubnetValidatorToRegister.Weight,
		}
		convertSubnetValidatorValidationID = registerSubnetValidatorToRegister.SubnetID.Append(0)
		convertSubnetValidatorSoV          = state.SubnetOnlyValidator{
			ValidationID: convertSubnetValidatorValidationID,
			SubnetID:     registerSubnetValidatorToRegister.SubnetID,
			NodeID:       ids.GenerateTestNodeID(),
			PublicKey:    bls.PublicKeyToUncompressedBytes(pk),
			Weight:       registerSubnetValidatorToRegister.Weight,
		}
		expiryEntry = state.ExpiryEntry{
			Timestamp:    expiry,
			ValidationID: registerSubnetValidatorToMarkExpired.ValidationID(),
		}

		state = statetest.New(t, statetest.Config{})
		s     = signatureRequestVerifier{
			stateLock: &sync.Mutex{},
			state:     state,
		}
	)

	require.NoError(t, state.PutSubnetOnlyValidator(registerSubnetValidatorSoV))
	require.NoError(t, state.PutSubnetOnlyValidator(convertSubnetValidatorSoV))
	state.PutExpiry(expiryEntry)

	tests := []struct {
		name          string
		validationID  ids.ID
		justification []byte
		expectedErr   *common.AppError
	}{
		{
			name:          "failed to parse justification",
			justification: []byte("invalid"),
			expectedErr: &common.AppError{
				Code:    ErrFailedToParseJustification,
				Message: "failed to parse justification: proto: cannot parse invalid wire-format data",
			},
		},
		{
			name: "failed to parse subnetID",
			justification: must[[]byte](t)(proto.Marshal(
				&platformvm.SubnetValidatorRegistrationJustification{
					Preimage: &platformvm.SubnetValidatorRegistrationJustification_ConvertSubnetTxData{},
				},
			)),
			expectedErr: &common.AppError{
				Code:    ErrFailedToParseSubnetID,
				Message: "failed to parse subnetID: invalid hash length: expected 32 bytes but got 0",
			},
		},
		{
			name:         "mismatched convert subnet validationID",
			validationID: registerSubnetValidatorNotToRegister.ValidationID(),
			justification: must[[]byte](t)(proto.Marshal(
				&platformvm.SubnetValidatorRegistrationJustification{
					Preimage: &platformvm.SubnetValidatorRegistrationJustification_ConvertSubnetTxData{
						ConvertSubnetTxData: &platformvm.SubnetIDIndex{
							SubnetId: subnetID[:],
							Index:    0,
						},
					},
				},
			)),
			expectedErr: &common.AppError{
				Code:    ErrMismatchedValidationID,
				Message: `validationID "2SZuDErFdUGmrQNHuTaFobL6DewfJr4tEKrdcgPNVc7PXYejGD" != justificationID "8XSRE5pasJjRvghBXQyBzDPF91ywXm8AZWZ6jo4522tbVuynN"`,
			},
		},
		{
			name:         "convert subnet validation exists",
			validationID: convertSubnetValidatorValidationID,
			justification: must[[]byte](t)(proto.Marshal(
				&platformvm.SubnetValidatorRegistrationJustification{
					Preimage: &platformvm.SubnetValidatorRegistrationJustification_ConvertSubnetTxData{
						ConvertSubnetTxData: &platformvm.SubnetIDIndex{
							SubnetId: subnetID[:],
							Index:    0,
						},
					},
				},
			)),
			expectedErr: &common.AppError{
				Code:    ErrValidationExists,
				Message: `validation "8XSRE5pasJjRvghBXQyBzDPF91ywXm8AZWZ6jo4522tbVuynN" exists`,
			},
		},
		{
			name:         "valid convert subnet data",
			validationID: subnetID.Append(1),
			justification: must[[]byte](t)(proto.Marshal(
				&platformvm.SubnetValidatorRegistrationJustification{
					Preimage: &platformvm.SubnetValidatorRegistrationJustification_ConvertSubnetTxData{
						ConvertSubnetTxData: &platformvm.SubnetIDIndex{
							SubnetId: subnetID[:],
							Index:    1,
						},
					},
				},
			)),
		},
		{
			name: "failed to parse register subnet validator",
			justification: must[[]byte](t)(proto.Marshal(
				&platformvm.SubnetValidatorRegistrationJustification{
					Preimage: &platformvm.SubnetValidatorRegistrationJustification_RegisterSubnetValidatorMessage{},
				},
			)),
			expectedErr: &common.AppError{
				Code:    ErrFailedToParseRegisterSubnetValidator,
				Message: "failed to parse RegisterSubnetValidator justification: couldn't unpack codec version",
			},
		},
		{
			name:         "mismatched registration validationID",
			validationID: registerSubnetValidatorValidationID,
			justification: must[[]byte](t)(proto.Marshal(
				&platformvm.SubnetValidatorRegistrationJustification{
					Preimage: &platformvm.SubnetValidatorRegistrationJustification_RegisterSubnetValidatorMessage{
						RegisterSubnetValidatorMessage: registerSubnetValidatorNotToRegister.Bytes(),
					},
				},
			)),
			expectedErr: &common.AppError{
				Code:    ErrMismatchedValidationID,
				Message: `validationID "2UB8nmhSCDbhBBzXkpvjZYAu37nC7spNGQAbkVSeWVvbT8RNqS" != justificationID "2SZuDErFdUGmrQNHuTaFobL6DewfJr4tEKrdcgPNVc7PXYejGD"`,
			},
		},
		{
			name:         "registration validation exists",
			validationID: registerSubnetValidatorValidationID,
			justification: must[[]byte](t)(proto.Marshal(
				&platformvm.SubnetValidatorRegistrationJustification{
					Preimage: &platformvm.SubnetValidatorRegistrationJustification_RegisterSubnetValidatorMessage{
						RegisterSubnetValidatorMessage: registerSubnetValidatorToRegister.Bytes(),
					},
				},
			)),
			expectedErr: &common.AppError{
				Code:    ErrValidationExists,
				Message: `validation "2UB8nmhSCDbhBBzXkpvjZYAu37nC7spNGQAbkVSeWVvbT8RNqS" exists`,
			},
		},
		{
			name:         "valid expired registration",
			validationID: registerSubnetValidatorExpired.ValidationID(),
			justification: must[[]byte](t)(proto.Marshal(
				&platformvm.SubnetValidatorRegistrationJustification{
					Preimage: &platformvm.SubnetValidatorRegistrationJustification_RegisterSubnetValidatorMessage{
						RegisterSubnetValidatorMessage: registerSubnetValidatorExpired.Bytes(),
					},
				},
			)),
		},
		{
			name:         "validation could be registered",
			validationID: registerSubnetValidatorNotToRegister.ValidationID(),
			justification: must[[]byte](t)(proto.Marshal(
				&platformvm.SubnetValidatorRegistrationJustification{
					Preimage: &platformvm.SubnetValidatorRegistrationJustification_RegisterSubnetValidatorMessage{
						RegisterSubnetValidatorMessage: registerSubnetValidatorNotToRegister.Bytes(),
					},
				},
			)),
			expectedErr: &common.AppError{
				Code:    ErrValidationCouldBeRegistered,
				Message: `validation "2SZuDErFdUGmrQNHuTaFobL6DewfJr4tEKrdcgPNVc7PXYejGD" can be registered until 1607144401`,
			},
		},
		{
			name:         "validation removed",
			validationID: registerSubnetValidatorToMarkExpired.ValidationID(),
			justification: must[[]byte](t)(proto.Marshal(
				&platformvm.SubnetValidatorRegistrationJustification{
					Preimage: &platformvm.SubnetValidatorRegistrationJustification_RegisterSubnetValidatorMessage{
						RegisterSubnetValidatorMessage: registerSubnetValidatorToMarkExpired.Bytes(),
					},
				},
			)),
		},
		{
			name: "invalid justification type",
			expectedErr: &common.AppError{
				Code:    ErrInvalidJustificationType,
				Message: "invalid justification type: <nil>",
			},
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
							false,
						)).Bytes(),
					)).Bytes(),
				)),
				test.justification,
			)
			if err != nil {
				// Replace use non-breaking spaces (U+00a0) with regular spaces
				// (U+0020). This is needed because the proto library
				// intentionally makes the error message unstable.
				err.Message = strings.ReplaceAll(err.Message, " ", " ")
			}
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
