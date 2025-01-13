// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	ethtypes "github.com/ava-labs/libevm/core/types"
)

// The following types are used directly as their upstream definitions.
// So we list them all here to avoid having many individual files.
type (
	AccessList        = ethtypes.AccessList
	AccessTuple       = ethtypes.AccessTuple
	AccessListTx      = ethtypes.AccessListTx
	Bloom             = ethtypes.Bloom
	Receipt           = ethtypes.Receipt
	Receipts          = ethtypes.Receipts
	ReceiptForStorage = ethtypes.ReceiptForStorage
	LegacyTx          = ethtypes.LegacyTx
	DynamicFeeTx      = ethtypes.DynamicFeeTx
	BlobTx            = ethtypes.BlobTx
	BlobTxSidecar     = ethtypes.BlobTxSidecar
	Signer            = ethtypes.Signer
	HomesteadSigner   = ethtypes.HomesteadSigner
	FrontierSigner    = ethtypes.FrontierSigner

	Transaction  = ethtypes.Transaction
	Transactions = ethtypes.Transactions
	TxByNonce    = ethtypes.TxByNonce
	TxData       = ethtypes.TxData
)

// The following constants are used directly as their upstream definitions.
const (
	BloomBitLength          = ethtypes.BloomBitLength
	BloomByteLength         = ethtypes.BloomByteLength
	ReceiptStatusFailed     = ethtypes.ReceiptStatusFailed
	ReceiptStatusSuccessful = ethtypes.ReceiptStatusSuccessful

	// Transaction types.
	LegacyTxType     = ethtypes.LegacyTxType
	AccessListTxType = ethtypes.AccessListTxType
	DynamicFeeTxType = ethtypes.DynamicFeeTxType
	BlobTxType       = ethtypes.BlobTxType
)

// The following functions are used directly as their upstream definitions.
var (
	BloomLookup         = ethtypes.BloomLookup
	BytesToBloom        = ethtypes.BytesToBloom
	CreateBloom         = ethtypes.CreateBloom
	NewReceipt          = ethtypes.NewReceipt
	NewContractCreation = ethtypes.NewContractCreation
	NewTransaction      = ethtypes.NewTransaction

	// Signers
	NewEIP155Signer        = ethtypes.NewEIP155Signer
	NewEIP2930Signer       = ethtypes.NewEIP2930Signer
	NewLondonSigner        = ethtypes.NewLondonSigner
	NewCancunSigner        = ethtypes.NewCancunSigner
	MakeSigner             = ethtypes.MakeSigner
	LatestSigner           = ethtypes.LatestSigner
	LatestSignerForChainID = ethtypes.LatestSignerForChainID
	SignTx                 = ethtypes.SignTx
	SignNewTx              = ethtypes.SignNewTx
	MustSignNewTx          = ethtypes.MustSignNewTx
	Sender                 = ethtypes.Sender

	// Transactions
	NewTx        = ethtypes.NewTx
	TxDifference = ethtypes.TxDifference

	// Errors
	ErrTxTypeNotSupported = ethtypes.ErrTxTypeNotSupported
	ErrGasFeeCapTooLow    = ethtypes.ErrGasFeeCapTooLow
)
