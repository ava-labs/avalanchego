// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/messages"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

func TestRequestSignature(t *testing.T) {
	testCases := []struct {
		name          string
		bytesAccessor func(t *testing.T) []byte
		expectedValid bool
	}{
		{
			name: "register subnet validator",
			bytesAccessor: func(t *testing.T) []byte {
				require := require.New(t)
				secretKey, err := bls.NewSecretKey()
				require.NoError(err)
				publicKey := bls.PublicFromSecretKey(secretKey)
				pubKeyBytes := *(*[48]byte)(bls.PublicKeyToCompressedBytes(publicKey))
				registerValidatorMessage, err := messages.NewRegisterSubnetValidator(
					ids.GenerateTestID(),
					ids.GenerateTestID(),
					1000,
					pubKeyBytes,
					9999,
				)
				require.NoError(err)
				return registerValidatorMessage.Bytes()
			},
			expectedValid: true,
		},
		{
			name: "set subnet validator weight",
			bytesAccessor: func(t *testing.T) []byte {
				setWeightMessage, err := messages.NewSetSubnetValidatorWeight(ids.GenerateTestID(), 1, 1000)
				require.NoError(t, err)
				return setWeightMessage.Bytes()
			},
			expectedValid: true,
		},
		{
			name: "subnet validator registration",
			bytesAccessor: func(t *testing.T) []byte {
				validationID := ids.GenerateTestID()
				registrationMessage, err := messages.NewSubnetValidatorRegistration(validationID, true)
				require.NoError(t, err)
				return registrationMessage.Bytes()
			},
			expectedValid: true,
		},
		{
			name: "subnet validator weight update",
			bytesAccessor: func(t *testing.T) []byte {
				weightUpdateMessage, err := messages.NewSubnetValidatorWeightUpdate(ids.GenerateTestID(), 2, 2000)
				require.NoError(t, err)
				return weightUpdateMessage.Bytes()
			},
			expectedValid: true,
		},
		{
			name: "invalid message",
			bytesAccessor: func(_ *testing.T) []byte {
				return []byte{1, 2, 3, 4}
			},
			expectedValid: false,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			requestSignature(t, testCase.bytesAccessor(t), testCase.expectedValid)
		})
	}
}

func requestSignature(t *testing.T, msg []byte, valid bool) {
	require := require.New(t)
	snowCtx := snowtest.Context(t, snowtest.PChainID)
	signatureRequestHandler := signatureRequestHandler{
		signer: snowCtx.WarpSigner,
	}

	addressedCall, err := payload.NewAddressedCall(common.Address{}.Bytes(), msg)
	require.NoError(err)
	unsignedMessage, err := warp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, addressedCall.Bytes())
	require.NoError(err)
	require.NoError(unsignedMessage.Initialize())

	reqBytes, err := proto.Marshal(
		&sdk.SignatureRequest{
			Message: unsignedMessage.Bytes(),
		},
	)
	require.NoError(err)

	resp, appErr := signatureRequestHandler.AppRequest(context.Background(), ids.GenerateTestNodeID(), time.Now(), reqBytes)
	if !valid {
		require.NotNil(appErr)
		require.Equal(ErrUnsupportedWarpMessageType, int(appErr.Code))
		return
	}

	require.Nil(appErr)
	sigResponse := sdk.SignatureResponse{}
	require.NoError(proto.Unmarshal(resp, &sigResponse))

	sig, err := bls.SignatureFromBytes(sigResponse.Signature)
	require.NoError(err)
	require.True(bls.Verify(snowCtx.PublicKey, sig, unsignedMessage.Bytes()))
}
