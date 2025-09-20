// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cubesigner

import (
	"github.com/cubist-labs/cubesigner-go-sdk/client"
	"github.com/cubist-labs/cubesigner-go-sdk/models"
)

// CubeSignerClient defines the interface for CubeSigner client operations
// needed by the keychain implementation
type CubeSignerClient interface {
	// GetKeyInOrg retrieves key information for the given keyID from the CubeSigner organization.
	// It returns the key metadata, including public key and key type.
	GetKeyInOrg(keyID string) (*models.KeyInfo, error)

	// BlobSign signs arbitrary data using the specified keyID.
	// request contains the data to be signed.
	BlobSign(keyID string, request models.BlobSignRequest, receipts ...*client.MfaReceipt) (*client.CubeSignerResponse[models.SignResponse], error)

	// AvaSerializedTxSign signs Avalanche transactions using the specified chainAlias (P/X/C), and materialID (address).
	// request contains the serialized transaction data to be signed.
	AvaSerializedTxSign(chainAlias, materialID string, request models.AvaSerializedTxSignRequest, receipts ...*client.MfaReceipt) (*client.CubeSignerResponse[models.SignResponse], error)
}
