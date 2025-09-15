// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cubesigner

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/keychain/cubesigner/cubesignermock"
	"github.com/cubist-labs/cubesigner-go-sdk/client"
	"github.com/cubist-labs/cubesigner-go-sdk/models"
)

var errTest = errors.New("test")

func TestNewCubesignerKeychain(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	keyID := "test-key-id"

	// user provides no keys
	mockClient := cubesignermock.NewCubeSignerClient(ctrl)
	_, err := NewCubesignerKeychain(mockClient, []string{})
	require.ErrorContains(err, "you need to provide at least one key")

	// client returns error when getting key info
	mockClient = cubesignermock.NewCubeSignerClient(ctrl)
	mockClient.EXPECT().GetKeyInOrg(keyID).Return(nil, errTest).Times(1)
	_, err = NewCubesignerKeychain(mockClient, []string{keyID})
	require.ErrorIs(err, errTest)

	// client returns unsupported key type
	mockClient = cubesignermock.NewCubeSignerClient(ctrl)
	keyInfo := &models.KeyInfo{
		KeyType:   "UnsupportedType",
		PublicKey: "0x04" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	mockClient.EXPECT().GetKeyInOrg(keyID).Return(keyInfo, nil).Times(1)
	_, err = NewCubesignerKeychain(mockClient, []string{keyID})
	require.ErrorContains(err, "keytype UnsupportedType of server key")

	// client returns invalid public key format
	mockClient = cubesignermock.NewCubeSignerClient(ctrl)
	keyInfo = &models.KeyInfo{
		KeyType:   models.SecpAvaAddr,
		PublicKey: "invalid-hex",
	}
	mockClient.EXPECT().GetKeyInOrg(keyID).Return(keyInfo, nil).Times(1)
	_, err = NewCubesignerKeychain(mockClient, []string{keyID})
	require.ErrorContains(err, "failed to decode public key")

	// good path - Avalanche address
	mockClient = cubesignermock.NewCubeSignerClient(ctrl)
	keyInfo = &models.KeyInfo{
		KeyType:   models.SecpAvaAddr,
		PublicKey: "0x04" + "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8",
	}
	mockClient.EXPECT().GetKeyInOrg(keyID).Return(keyInfo, nil).Times(1)
	kc, err := NewCubesignerKeychain(mockClient, []string{keyID})
	require.NoError(err)
	require.NotNil(kc)

	// good path - Ethereum address
	mockClient = cubesignermock.NewCubeSignerClient(ctrl)
	keyInfo = &models.KeyInfo{
		KeyType:   models.SecpEthAddr,
		PublicKey: "0x04" + "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8",
	}
	mockClient.EXPECT().GetKeyInOrg(keyID).Return(keyInfo, nil).Times(1)
	kc, err = NewCubesignerKeychain(mockClient, []string{keyID})
	require.NoError(err)
	require.NotNil(kc)
}

func TestCubesignerKeychain_Addresses(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	keyID := "test-key-id"
	mockClient := cubesignermock.NewCubeSignerClient(ctrl)

	keyInfo := &models.KeyInfo{
		KeyType:   models.SecpAvaAddr,
		PublicKey: "0x04" + "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8",
	}
	mockClient.EXPECT().GetKeyInOrg(keyID).Return(keyInfo, nil).Times(1)

	kc, err := NewCubesignerKeychain(mockClient, []string{keyID})
	require.NoError(err)

	addresses := kc.Addresses()
	require.Len(addresses, 1)
}

func TestCubesignerKeychain_Get(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	keyID := "test-key-id"
	mockClient := cubesignermock.NewCubeSignerClient(ctrl)

	keyInfo := &models.KeyInfo{
		KeyType:   models.SecpAvaAddr,
		PublicKey: "0x04" + "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8",
	}
	mockClient.EXPECT().GetKeyInOrg(keyID).Return(keyInfo, nil).Times(1)

	kc, err := NewCubesignerKeychain(mockClient, []string{keyID})
	require.NoError(err)

	addresses := kc.Addresses()
	require.Len(addresses, 1)

	addr := addresses.List()[0]
	signer, found := kc.Get(addr)
	require.True(found)
	require.NotNil(signer)
	require.Equal(addr, signer.Address())

	// test with non-existent address
	randomAddr := ids.GenerateTestShortID()
	signer, found = kc.Get(randomAddr)
	require.False(found)
	require.Nil(signer)
}

func TestCubesignerKeychain_EthAddresses(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	keyID := "test-key-id"
	mockClient := cubesignermock.NewCubeSignerClient(ctrl)

	keyInfo := &models.KeyInfo{
		KeyType:   models.SecpEthAddr,
		PublicKey: "0x04" + "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8",
	}
	mockClient.EXPECT().GetKeyInOrg(keyID).Return(keyInfo, nil).Times(1)

	kc, err := NewCubesignerKeychain(mockClient, []string{keyID})
	require.NoError(err)

	ethAddresses := kc.EthAddresses()
	require.Len(ethAddresses, 1)
}

func TestCubesignerKeychain_GetEth(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	keyID := "test-key-id"
	mockClient := cubesignermock.NewCubeSignerClient(ctrl)

	keyInfo := &models.KeyInfo{
		KeyType:   models.SecpEthAddr,
		PublicKey: "0x04" + "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8",
	}
	mockClient.EXPECT().GetKeyInOrg(keyID).Return(keyInfo, nil).Times(1)

	kc, err := NewCubesignerKeychain(mockClient, []string{keyID})
	require.NoError(err)

	ethAddresses := kc.EthAddresses()
	require.Len(ethAddresses, 1)

	ethAddr := ethAddresses.List()[0]
	signer, found := kc.GetEth(ethAddr)
	require.True(found)
	require.NotNil(signer)

	// verify this is a C-chain signer
	cubesignerSigner := signer.(*cubesignerSigner)
	require.True(cubesignerSigner.cChainSigner)
}

func TestCubesignerSigner_SignHash(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	keyID := "test-key-id"
	mockClient := cubesignermock.NewCubeSignerClient(ctrl)

	keyInfo := &models.KeyInfo{
		KeyType:   models.SecpAvaAddr,
		PublicKey: "0x04" + "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8",
	}
	mockClient.EXPECT().GetKeyInOrg(keyID).Return(keyInfo, nil).Times(1)

	kc, err := NewCubesignerKeychain(mockClient, []string{keyID})
	require.NoError(err)

	addresses := kc.Addresses()
	addr := addresses.List()[0]
	signer, found := kc.Get(addr)
	require.True(found)

	hash := []byte("test-hash")

	// test successful signing
	response := &client.CubeSignerResponse[models.SignResponse]{
		ResponseData: &models.SignResponse{
			Signature: "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef01",
		},
	}
	mockClient.EXPECT().BlobSign(keyID, gomock.Any()).Return(response, nil).Times(1)

	signature, err := signer.SignHash(hash)
	require.NoError(err)
	require.NotNil(signature)

	// test client error
	mockClient.EXPECT().BlobSign(keyID, gomock.Any()).Return(nil, errTest).Times(1)
	_, err = signer.SignHash(hash)
	require.ErrorIs(err, errTest)

	// test empty response
	emptyResponse := &client.CubeSignerResponse[models.SignResponse]{
		ResponseData: nil,
	}
	mockClient.EXPECT().BlobSign(keyID, gomock.Any()).Return(emptyResponse, nil).Times(1)
	_, err = signer.SignHash(hash)
	require.ErrorContains(err, "empty signature obtained from server")
}