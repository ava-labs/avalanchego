// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"encoding/hex"
	"math"
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
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
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
				must[*message.RegisterL1Validator](t)(message.NewRegisterL1Validator(
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
				Message: "unsupported warp addressed call payload type: *message.RegisterL1Validator",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := signatureRequestVerifier{}
			err := s.Verify(
				t.Context(),
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

func TestSignatureRequestVerifySubnetToL1Conversion(t *testing.T) {
	var (
		subnetID   = ids.GenerateTestID()
		conversion = state.SubnetToL1Conversion{
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

	state.SetSubnetToL1Conversion(subnetID, conversion)

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
				t.Context(),
				must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
					constants.UnitTestID,
					constants.PlatformChainID,
					must[*payload.AddressedCall](t)(payload.NewAddressedCall(
						nil,
						must[*message.SubnetToL1Conversion](t)(message.NewSubnetToL1Conversion(
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

func TestSignatureRequestVerifyL1ValidatorRegistrationRegistered(t *testing.T) {
	sk, err := localsigner.New()
	require.NoError(t, err)

	var (
		l1Validator = state.L1Validator{
			ValidationID: ids.GenerateTestID(),
			SubnetID:     ids.GenerateTestID(),
			NodeID:       ids.GenerateTestNodeID(),
			PublicKey:    bls.PublicKeyToUncompressedBytes(sk.PublicKey()),
			Weight:       1,
		}
		state = statetest.New(t, statetest.Config{})
		s     = signatureRequestVerifier{
			stateLock: &sync.Mutex{},
			state:     state,
		}
	)

	require.NoError(t, state.PutL1Validator(l1Validator))

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
			validationID: l1Validator.ValidationID,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := s.Verify(
				t.Context(),
				must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
					constants.UnitTestID,
					constants.PlatformChainID,
					must[*payload.AddressedCall](t)(payload.NewAddressedCall(
						nil,
						must[*message.L1ValidatorRegistration](t)(message.NewL1ValidatorRegistration(
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

func TestSignatureRequestVerifyL1ValidatorRegistrationNotRegistered(t *testing.T) {
	skBytes, err := hex.DecodeString("36a33c536d283dfa599d0a70839c67ded6c954e346c5e8e5b4794e2299907887")
	require.NoError(t, err)

	sk, err := localsigner.FromBytes(skBytes)
	require.NoError(t, err)

	var (
		convertedSubnetID          = ids.ID{3}
		unconvertedSubnetID        = ids.ID{7}
		nodeID0                    = ids.NodeID{4}
		nodeID1                    = ids.NodeID{5}
		nodeID2                    = ids.NodeID{6}
		nodeID3                    = ids.NodeID{6}
		pk                         = sk.PublicKey()
		expiry                     = genesistest.DefaultValidatorStartTimeUnix + 1
		weight              uint64 = 1
	)

	registerL1ValidatorToRegister, err := message.NewRegisterL1Validator(
		convertedSubnetID,
		nodeID0,
		[bls.PublicKeyLen]byte(bls.PublicKeyToCompressedBytes(pk)),
		expiry,
		message.PChainOwner{},
		message.PChainOwner{},
		weight,
	)
	require.NoError(t, err)

	registerL1ValidatorNotToRegister, err := message.NewRegisterL1Validator(
		convertedSubnetID,
		nodeID1,
		[bls.PublicKeyLen]byte(bls.PublicKeyToCompressedBytes(pk)),
		expiry,
		message.PChainOwner{},
		message.PChainOwner{},
		weight,
	)
	require.NoError(t, err)

	registerL1ValidatorExpired, err := message.NewRegisterL1Validator(
		convertedSubnetID,
		nodeID2,
		[bls.PublicKeyLen]byte(bls.PublicKeyToCompressedBytes(pk)),
		genesistest.DefaultValidatorStartTimeUnix,
		message.PChainOwner{},
		message.PChainOwner{},
		weight,
	)
	require.NoError(t, err)

	registerL1ValidatorToMarkExpired, err := message.NewRegisterL1Validator(
		convertedSubnetID,
		nodeID3,
		[bls.PublicKeyLen]byte(bls.PublicKeyToCompressedBytes(pk)),
		expiry,
		message.PChainOwner{},
		message.PChainOwner{},
		weight,
	)
	require.NoError(t, err)

	var (
		conversion                      = state.SubnetToL1Conversion{}
		registerL1ValidatorValidationID = registerL1ValidatorToRegister.ValidationID()
		registerL1ValidatorValidator    = state.L1Validator{
			ValidationID: registerL1ValidatorValidationID,
			SubnetID:     registerL1ValidatorToRegister.SubnetID,
			NodeID:       ids.NodeID(registerL1ValidatorToRegister.NodeID),
			PublicKey:    bls.PublicKeyToUncompressedBytes(pk),
			Weight:       registerL1ValidatorToRegister.Weight,
		}
		convertSubnetToL1ValidationID = registerL1ValidatorToRegister.SubnetID.Append(0)
		convertSubnetToL1Validator    = state.L1Validator{
			ValidationID: convertSubnetToL1ValidationID,
			SubnetID:     registerL1ValidatorToRegister.SubnetID,
			NodeID:       ids.GenerateTestNodeID(),
			PublicKey:    bls.PublicKeyToUncompressedBytes(pk),
			Weight:       registerL1ValidatorToRegister.Weight,
		}
		expiryEntry = state.ExpiryEntry{
			Timestamp:    expiry,
			ValidationID: registerL1ValidatorToMarkExpired.ValidationID(),
		}

		state = statetest.New(t, statetest.Config{})
		s     = signatureRequestVerifier{
			stateLock: &sync.Mutex{},
			state:     state,
		}
	)

	state.SetSubnetToL1Conversion(convertedSubnetID, conversion)
	require.NoError(t, state.PutL1Validator(registerL1ValidatorValidator))
	require.NoError(t, state.PutL1Validator(convertSubnetToL1Validator))
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
				&platformvm.L1ValidatorRegistrationJustification{
					Preimage: &platformvm.L1ValidatorRegistrationJustification_ConvertSubnetToL1TxData{},
				},
			)),
			expectedErr: &common.AppError{
				Code:    ErrFailedToParseSubnetID,
				Message: "failed to parse subnetID: invalid hash length: expected 32 bytes but got 0",
			},
		},
		{
			name:         "mismatched convert subnet validationID",
			validationID: registerL1ValidatorNotToRegister.ValidationID(),
			justification: must[[]byte](t)(proto.Marshal(
				&platformvm.L1ValidatorRegistrationJustification{
					Preimage: &platformvm.L1ValidatorRegistrationJustification_ConvertSubnetToL1TxData{
						ConvertSubnetToL1TxData: &platformvm.SubnetIDIndex{
							SubnetId: convertedSubnetID[:],
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
			validationID: convertSubnetToL1ValidationID,
			justification: must[[]byte](t)(proto.Marshal(
				&platformvm.L1ValidatorRegistrationJustification{
					Preimage: &platformvm.L1ValidatorRegistrationJustification_ConvertSubnetToL1TxData{
						ConvertSubnetToL1TxData: &platformvm.SubnetIDIndex{
							SubnetId: convertedSubnetID[:],
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
			name:         "conversion does not exist",
			validationID: unconvertedSubnetID.Append(0),
			justification: must[[]byte](t)(proto.Marshal(
				&platformvm.L1ValidatorRegistrationJustification{
					Preimage: &platformvm.L1ValidatorRegistrationJustification_ConvertSubnetToL1TxData{
						ConvertSubnetToL1TxData: &platformvm.SubnetIDIndex{
							SubnetId: unconvertedSubnetID[:],
							Index:    0,
						},
					},
				},
			)),
			expectedErr: &common.AppError{
				Code:    ErrConversionDoesNotExist,
				Message: `subnet "45oj4CqFViNHUtBxJ55TZfqaVAXFwMRMj2XkHVqUYjJYoTaEM" has not been converted`,
			},
		},
		{
			name:         "valid convert subnet data",
			validationID: convertedSubnetID.Append(1),
			justification: must[[]byte](t)(proto.Marshal(
				&platformvm.L1ValidatorRegistrationJustification{
					Preimage: &platformvm.L1ValidatorRegistrationJustification_ConvertSubnetToL1TxData{
						ConvertSubnetToL1TxData: &platformvm.SubnetIDIndex{
							SubnetId: convertedSubnetID[:],
							Index:    1,
						},
					},
				},
			)),
		},
		{
			name: "failed to parse register L1 validator",
			justification: must[[]byte](t)(proto.Marshal(
				&platformvm.L1ValidatorRegistrationJustification{
					Preimage: &platformvm.L1ValidatorRegistrationJustification_RegisterL1ValidatorMessage{},
				},
			)),
			expectedErr: &common.AppError{
				Code:    ErrFailedToParseRegisterL1Validator,
				Message: "failed to parse RegisterL1Validator justification: couldn't unpack codec version",
			},
		},
		{
			name:         "mismatched registration validationID",
			validationID: registerL1ValidatorValidationID,
			justification: must[[]byte](t)(proto.Marshal(
				&platformvm.L1ValidatorRegistrationJustification{
					Preimage: &platformvm.L1ValidatorRegistrationJustification_RegisterL1ValidatorMessage{
						RegisterL1ValidatorMessage: registerL1ValidatorNotToRegister.Bytes(),
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
			validationID: registerL1ValidatorValidationID,
			justification: must[[]byte](t)(proto.Marshal(
				&platformvm.L1ValidatorRegistrationJustification{
					Preimage: &platformvm.L1ValidatorRegistrationJustification_RegisterL1ValidatorMessage{
						RegisterL1ValidatorMessage: registerL1ValidatorToRegister.Bytes(),
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
			validationID: registerL1ValidatorExpired.ValidationID(),
			justification: must[[]byte](t)(proto.Marshal(
				&platformvm.L1ValidatorRegistrationJustification{
					Preimage: &platformvm.L1ValidatorRegistrationJustification_RegisterL1ValidatorMessage{
						RegisterL1ValidatorMessage: registerL1ValidatorExpired.Bytes(),
					},
				},
			)),
		},
		{
			name:         "validation could be registered",
			validationID: registerL1ValidatorNotToRegister.ValidationID(),
			justification: must[[]byte](t)(proto.Marshal(
				&platformvm.L1ValidatorRegistrationJustification{
					Preimage: &platformvm.L1ValidatorRegistrationJustification_RegisterL1ValidatorMessage{
						RegisterL1ValidatorMessage: registerL1ValidatorNotToRegister.Bytes(),
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
			validationID: registerL1ValidatorToMarkExpired.ValidationID(),
			justification: must[[]byte](t)(proto.Marshal(
				&platformvm.L1ValidatorRegistrationJustification{
					Preimage: &platformvm.L1ValidatorRegistrationJustification_RegisterL1ValidatorMessage{
						RegisterL1ValidatorMessage: registerL1ValidatorToMarkExpired.Bytes(),
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
				t.Context(),
				must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
					constants.UnitTestID,
					constants.PlatformChainID,
					must[*payload.AddressedCall](t)(payload.NewAddressedCall(
						nil,
						must[*message.L1ValidatorRegistration](t)(message.NewL1ValidatorRegistration(
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

func TestSignatureRequestVerifyL1ValidatorWeight(t *testing.T) {
	sk, err := localsigner.New()
	require.NoError(t, err)

	const (
		weight = 100
		nonce  = 10
	)
	var (
		l1Validator = state.L1Validator{
			ValidationID: ids.GenerateTestID(),
			SubnetID:     ids.GenerateTestID(),
			NodeID:       ids.GenerateTestNodeID(),
			PublicKey:    bls.PublicKeyToUncompressedBytes(sk.PublicKey()),
			Weight:       weight,
			MinNonce:     nonce + 1,
		}

		state = statetest.New(t, statetest.Config{})
		s     = signatureRequestVerifier{
			stateLock: &sync.Mutex{},
			state:     state,
		}
	)

	require.NoError(t, state.PutL1Validator(l1Validator))

	tests := []struct {
		name         string
		validationID ids.ID
		nonce        uint64
		weight       uint64
		expectedErr  *common.AppError
	}{
		{
			name:  "impossible nonce",
			nonce: math.MaxUint64,
			expectedErr: &common.AppError{
				Code:    ErrImpossibleNonce,
				Message: "impossible nonce",
			},
		},
		{
			name: "validation does not exist",
			expectedErr: &common.AppError{
				Code:    ErrValidationDoesNotExist,
				Message: `validation "11111111111111111111111111111111LpoYY" does not exist`,
			},
		},
		{
			name:         "wrong nonce",
			validationID: l1Validator.ValidationID,
			expectedErr: &common.AppError{
				Code:    ErrWrongNonce,
				Message: "provided nonce 0 != expected nonce (11 - 1)",
			},
		},
		{
			name:         "wrong weight",
			validationID: l1Validator.ValidationID,
			nonce:        nonce,
			expectedErr: &common.AppError{
				Code:    ErrWrongWeight,
				Message: "provided weight 0 != expected weight 100",
			},
		},
		{
			name:         "valid",
			validationID: l1Validator.ValidationID,
			nonce:        nonce,
			weight:       weight,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := s.Verify(
				t.Context(),
				must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
					constants.UnitTestID,
					constants.PlatformChainID,
					must[*payload.AddressedCall](t)(payload.NewAddressedCall(
						nil,
						must[*message.L1ValidatorWeight](t)(message.NewL1ValidatorWeight(
							test.validationID,
							test.nonce,
							test.weight,
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
