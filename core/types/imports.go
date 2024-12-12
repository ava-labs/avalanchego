// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	gethtypes "github.com/ethereum/go-ethereum/core/types"
)

// The following types are used directly as their upstream definitions.
// So we list them all here to avoid having many individual files.
type (
	AccessList        = gethtypes.AccessList
	AccessTuple       = gethtypes.AccessTuple
	AccessListTx      = gethtypes.AccessListTx
	Bloom             = gethtypes.Bloom
	Receipt           = gethtypes.Receipt
	Receipts          = gethtypes.Receipts
	ReceiptForStorage = gethtypes.ReceiptForStorage
	LegacyTx          = gethtypes.LegacyTx
	DynamicFeeTx      = gethtypes.DynamicFeeTx
	BlobTx            = gethtypes.BlobTx
	BlobTxSidecar     = gethtypes.BlobTxSidecar
	Signer            = gethtypes.Signer
	HomesteadSigner   = gethtypes.HomesteadSigner
	FrontierSigner    = gethtypes.FrontierSigner

	Transaction  = gethtypes.Transaction
	Transactions = gethtypes.Transactions
	TxByNonce    = gethtypes.TxByNonce
	TxData       = gethtypes.TxData
)

// The following constants are used directly as their upstream definitions.
const (
	BloomBitLength          = gethtypes.BloomBitLength
	BloomByteLength         = gethtypes.BloomByteLength
	ReceiptStatusFailed     = gethtypes.ReceiptStatusFailed
	ReceiptStatusSuccessful = gethtypes.ReceiptStatusSuccessful

	// Transaction types.
	LegacyTxType     = gethtypes.LegacyTxType
	AccessListTxType = gethtypes.AccessListTxType
	DynamicFeeTxType = gethtypes.DynamicFeeTxType
	BlobTxType       = gethtypes.BlobTxType
)

// The following functions are used directly as their upstream definitions.
var (
	BloomLookup         = gethtypes.BloomLookup
	BytesToBloom        = gethtypes.BytesToBloom
	CreateBloom         = gethtypes.CreateBloom
	NewReceipt          = gethtypes.NewReceipt
	NewContractCreation = gethtypes.NewContractCreation
	NewTransaction      = gethtypes.NewTransaction

	// Signers
	NewEIP155Signer        = gethtypes.NewEIP155Signer
	NewEIP2930Signer       = gethtypes.NewEIP2930Signer
	NewLondonSigner        = gethtypes.NewLondonSigner
	NewCancunSigner        = gethtypes.NewCancunSigner
	MakeSigner             = gethtypes.MakeSigner
	LatestSigner           = gethtypes.LatestSigner
	LatestSignerForChainID = gethtypes.LatestSignerForChainID
	SignTx                 = gethtypes.SignTx
	SignNewTx              = gethtypes.SignNewTx
	MustSignNewTx          = gethtypes.MustSignNewTx
	Sender                 = gethtypes.Sender

	// Transactions
	NewTx        = gethtypes.NewTx
	TxDifference = gethtypes.TxDifference

	// Errors
	ErrTxTypeNotSupported = gethtypes.ErrTxTypeNotSupported
	ErrGasFeeCapTooLow    = gethtypes.ErrGasFeeCapTooLow
)
