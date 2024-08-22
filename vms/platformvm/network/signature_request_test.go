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

func TestRequestRegisterSubnetValidator(t *testing.T) {
	secretKey, err := bls.NewSecretKey()
	require.NoError(t, err)
	publicKey := bls.PublicFromSecretKey(secretKey)
	pubKeyBytes := *(*[48]byte)(bls.PublicKeyToCompressedBytes(publicKey))
	registerValidatorMessage, err := messages.NewRegisterSubnetValidator(
		ids.GenerateTestID(),
		ids.GenerateTestID(),
		1000,
		pubKeyBytes,
		9999,
	)
	require.NoError(t, err)
	requestSignature(t, registerValidatorMessage.Bytes(), true)
}

func TestRequestSetSubnetValidatorWeight(t *testing.T) {
	setWeightMessage, err := messages.NewSetSubnetValidatorWeight(ids.GenerateTestID(), 1, 1000)
	require.NoError(t, err)
	requestSignature(t, setWeightMessage.Bytes(), true)
}

func TestRequestSubnetValidatorRegistration(t *testing.T) {
	validationID := ids.GenerateTestID()
	registrationMessage, err := messages.NewSubnetValidatorRegistration(validationID, true)
	require.NoError(t, err)
	requestSignature(t, registrationMessage.Bytes(), true)
}

func TestRequestValidatorWeightUpdate(t *testing.T) {
	weightUpdateMessage, err := messages.NewSubnetValidatorWeightUpdate(ids.GenerateTestID(), 2, 2000)
	require.NoError(t, err)
	requestSignature(t, weightUpdateMessage.Bytes(), true)
}

func TestInvalidRequest(t *testing.T) {
	invalidMessage := []byte{1, 2, 3, 4}
	requestSignature(t, invalidMessage, false)
}

func requestSignature(t *testing.T, msg []byte, valid bool) {
	snowCtx := snowtest.Context(t, snowtest.PChainID)
	signatureRequestHandler := signatureRequestHandler{
		signer: snowCtx.WarpSigner,
	}

	addressedCall, err := payload.NewAddressedCall(common.Address{}.Bytes(), msg)
	require.NoError(t, err)
	unsignedMessage, err := warp.NewUnsignedMessage(snowCtx.NetworkID, snowCtx.ChainID, addressedCall.Bytes())
	require.NoError(t, err)
	require.NoError(t, unsignedMessage.Initialize())

	reqBytes, err := proto.Marshal(
		&sdk.SignatureRequest{
			Message: unsignedMessage.Bytes(),
		},
	)
	require.NoError(t, err)

	resp, appErr := signatureRequestHandler.AppRequest(context.Background(), ids.GenerateTestNodeID(), time.Now(), reqBytes)
	if !valid {
		require.NotNil(t, appErr)
		require.Equal(t, ErrUnsupportedWarpMessageType, int(appErr.Code))
		return
	}

	require.Nil(t, appErr)
	sigResponse := sdk.SignatureResponse{}
	require.NoError(t, proto.Unmarshal(resp, &sigResponse))

	sig, err := bls.SignatureFromBytes(sigResponse.Signature)
	require.NoError(t, err)
	require.True(t, bls.Verify(snowCtx.PublicKey, sig, unsignedMessage.Bytes()))
}
