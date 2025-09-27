// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cubesigner

import (
	"errors"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/cubist-labs/cubesigner-go-sdk/client"
	"github.com/cubist-labs/cubesigner-go-sdk/models"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/keychain"
	"github.com/ava-labs/avalanchego/utils/crypto/keychain/cubesigner/cubesignermock"
)

var errTest = errors.New("test")

const testKeyID = "test-key-id"

func TestNewKeychain(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	// user provides no keys
	mockClient := cubesignermock.NewCubeSignerClient(ctrl)
	_, err := NewKeychain(mockClient, []string{})
	require.ErrorIs(err, ErrNoKeysProvided)

	// client returns error when getting key info
	mockClient = cubesignermock.NewCubeSignerClient(ctrl)
	mockClient.EXPECT().GetKeyInOrg(testKeyID).Return(nil, errTest).Times(1)
	_, err = NewKeychain(mockClient, []string{testKeyID})
	require.ErrorIs(err, errTest)

	// client returns unsupported key type
	mockClient = cubesignermock.NewCubeSignerClient(ctrl)
	keyInfo := &models.KeyInfo{
		KeyType:   "UnsupportedType",
		PublicKey: "0x04" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	mockClient.EXPECT().GetKeyInOrg(testKeyID).Return(keyInfo, nil).Times(1)
	_, err = NewKeychain(mockClient, []string{testKeyID})
	require.ErrorIs(err, ErrUnsupportedKeyType)

	// client returns invalid public key format
	mockClient = cubesignermock.NewCubeSignerClient(ctrl)
	keyInfo = &models.KeyInfo{
		KeyType:   models.SecpAvaAddr,
		PublicKey: "invalid-hex",
	}
	mockClient.EXPECT().GetKeyInOrg(testKeyID).Return(keyInfo, nil).Times(1)
	_, err = NewKeychain(mockClient, []string{testKeyID})
	require.ErrorIs(err, ErrInvalidPublicKey)

	// good path - Avalanche address
	mockClient = cubesignermock.NewCubeSignerClient(ctrl)
	keyInfo = &models.KeyInfo{
		KeyType:   models.SecpAvaAddr,
		PublicKey: "0x04" + "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8",
	}
	mockClient.EXPECT().GetKeyInOrg(testKeyID).Return(keyInfo, nil).Times(1)
	kc, err := NewKeychain(mockClient, []string{testKeyID})
	require.NoError(err)
	require.NotNil(kc)

	// good path - Ethereum address
	mockClient = cubesignermock.NewCubeSignerClient(ctrl)
	keyInfo = &models.KeyInfo{
		KeyType:   models.SecpEthAddr,
		PublicKey: "0x04" + "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8",
	}
	mockClient.EXPECT().GetKeyInOrg(testKeyID).Return(keyInfo, nil).Times(1)
	kc, err = NewKeychain(mockClient, []string{testKeyID})
	require.NoError(err)
	require.NotNil(kc)
}

func TestKeychain_Addresses(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	mockClient := cubesignermock.NewCubeSignerClient(ctrl)

	keyInfo := &models.KeyInfo{
		KeyType:   models.SecpAvaAddr,
		PublicKey: "0x04" + "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8",
	}
	mockClient.EXPECT().GetKeyInOrg(testKeyID).Return(keyInfo, nil).Times(1)

	kc, err := NewKeychain(mockClient, []string{testKeyID})
	require.NoError(err)

	addresses := kc.Addresses()
	require.Len(addresses, 1)
}

func TestKeychain_Get(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	mockClient := cubesignermock.NewCubeSignerClient(ctrl)

	keyInfo := &models.KeyInfo{
		KeyType:   models.SecpAvaAddr,
		PublicKey: "0x04" + "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8",
	}
	mockClient.EXPECT().GetKeyInOrg(testKeyID).Return(keyInfo, nil).Times(1)

	kc, err := NewKeychain(mockClient, []string{testKeyID})
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

func TestKeychain_EthAddresses(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	mockClient := cubesignermock.NewCubeSignerClient(ctrl)

	keyInfo := &models.KeyInfo{
		KeyType:   models.SecpEthAddr,
		PublicKey: "0x04" + "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8",
	}
	mockClient.EXPECT().GetKeyInOrg(testKeyID).Return(keyInfo, nil).Times(1)

	kc, err := NewKeychain(mockClient, []string{testKeyID})
	require.NoError(err)

	ethAddresses := kc.EthAddresses()
	require.Len(ethAddresses, 1)
}

func TestKeychain_GetEth(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	mockClient := cubesignermock.NewCubeSignerClient(ctrl)

	keyInfo := &models.KeyInfo{
		KeyType:   models.SecpEthAddr,
		PublicKey: "0x04" + "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8",
	}
	mockClient.EXPECT().GetKeyInOrg(testKeyID).Return(keyInfo, nil).Times(1)

	kc, err := NewKeychain(mockClient, []string{testKeyID})
	require.NoError(err)

	ethAddresses := kc.EthAddresses()
	require.Len(ethAddresses, 1)

	ethAddr := ethAddresses.List()[0]
	signer, found := kc.GetEth(ethAddr)
	require.True(found)
	require.NotNil(signer)

	// Test non-existent address
	nonExistentAddr := common.HexToAddress("0x0000000000000000000000000000000000000000")
	signer, found = kc.GetEth(nonExistentAddr)
	require.False(found)
	require.Nil(signer)
}

func TestCubesignerSigner_SignHash(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	mockClient := cubesignermock.NewCubeSignerClient(ctrl)

	keyInfo := &models.KeyInfo{
		KeyType:   models.SecpAvaAddr,
		PublicKey: "0x04" + "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8",
	}
	mockClient.EXPECT().GetKeyInOrg(testKeyID).Return(keyInfo, nil).Times(1)

	kc, err := NewKeychain(mockClient, []string{testKeyID})
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
	mockClient.EXPECT().BlobSign(testKeyID, gomock.Any()).Return(response, nil).Times(1)

	signature, err := signer.SignHash(hash)
	require.NoError(err)
	require.NotNil(signature)

	// test client error
	mockClient.EXPECT().BlobSign(testKeyID, gomock.Any()).Return(nil, errTest).Times(1)
	_, err = signer.SignHash(hash)
	require.ErrorIs(err, errTest)

	// test empty response
	emptyResponse := &client.CubeSignerResponse[models.SignResponse]{
		ResponseData: nil,
	}
	mockClient.EXPECT().BlobSign(testKeyID, gomock.Any()).Return(emptyResponse, nil).Times(1)
	_, err = signer.SignHash(hash)
	require.ErrorIs(err, ErrEmptySignatureFromServer)
}

func TestCubesignerSigner_Sign(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	mockClient := cubesignermock.NewCubeSignerClient(ctrl)

	keyInfo := &models.KeyInfo{
		KeyType:   models.SecpAvaAddr,
		PublicKey: "0x04" + "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8",
	}
	mockClient.EXPECT().GetKeyInOrg(testKeyID).Return(keyInfo, nil).Times(1)

	kc, err := NewKeychain(mockClient, []string{testKeyID})
	require.NoError(err)

	addresses := kc.Addresses()
	addr := addresses.List()[0]
	signer, found := kc.Get(addr)
	require.True(found)

	txBytes := []byte("test-transaction-bytes")

	// Test missing chain alias
	_, err = signer.Sign(txBytes)
	require.ErrorIs(err, ErrChainAliasMissing)

	// Test invalid chain alias
	_, err = signer.Sign(txBytes, keychain.WithChainAlias("invalid"))
	require.ErrorIs(err, ErrInvalidChainAlias)

	// Test missing network ID
	_, err = signer.Sign(txBytes, keychain.WithChainAlias("P"))
	require.ErrorIs(err, ErrNetworkIDMissing)

	// Test successful P-chain signing
	response := &client.CubeSignerResponse[models.SignResponse]{
		ResponseData: &models.SignResponse{
			Signature: "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef01",
		},
	}
	mockClient.EXPECT().AvaSerializedTxSign(
		"P",
		gomock.Any(), // materialID (Bech32 address)
		gomock.Any(), // request with hex-encoded tx
	).Return(response, nil).Times(1)

	signature, err := signer.Sign(txBytes,
		keychain.WithChainAlias("P"),
		keychain.WithNetworkID(1))
	require.NoError(err)
	require.NotNil(signature)

	// Test successful X-chain signing
	mockClient.EXPECT().AvaSerializedTxSign(
		"X",
		gomock.Any(), // materialID (Bech32 address)
		gomock.Any(), // request with hex-encoded tx
	).Return(response, nil).Times(1)

	signature, err = signer.Sign(txBytes,
		keychain.WithChainAlias("X"),
		keychain.WithNetworkID(1))
	require.NoError(err)
	require.NotNil(signature)

	// Test successful C-chain signing
	mockClient.EXPECT().AvaSerializedTxSign(
		"C",
		gomock.Any(), // materialID (Eth address)
		gomock.Any(), // request with hex-encoded tx
	).Return(response, nil).Times(1)

	signature, err = signer.Sign(txBytes,
		keychain.WithChainAlias("C"),
		keychain.WithNetworkID(1))
	require.NoError(err)
	require.NotNil(signature)

	// Test client error
	mockClient.EXPECT().AvaSerializedTxSign(
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
	).Return(nil, errTest).Times(1)

	_, err = signer.Sign(txBytes,
		keychain.WithChainAlias("P"),
		keychain.WithNetworkID(1))
	require.ErrorIs(err, errTest)

	// Test empty response
	emptyResponse := &client.CubeSignerResponse[models.SignResponse]{
		ResponseData: nil,
	}
	mockClient.EXPECT().AvaSerializedTxSign(
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
	).Return(emptyResponse, nil).Times(1)

	_, err = signer.Sign(txBytes,
		keychain.WithChainAlias("P"),
		keychain.WithNetworkID(1))
	require.ErrorIs(err, ErrEmptySignatureFromServer)
}
