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
	GetKeyInOrg(keyID string) (*models.KeyInfo, error)
	BlobSign(keyID string, request models.BlobSignRequest, receipts ...*client.MfaReceipt) (*client.CubeSignerResponse[models.SignResponse], error)
	AvaSerializedTxSign(chainAlias, materialID string, request models.AvaSerializedTxSignRequest, receipts ...*client.MfaReceipt) (*client.CubeSignerResponse[models.SignResponse], error)
}