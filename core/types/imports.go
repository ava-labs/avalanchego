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
	AccessListTx      = ethtypes.AccessListTx
	AccessTuple       = ethtypes.AccessTuple
	BlobTx            = ethtypes.BlobTx
	BlobTxSidecar     = ethtypes.BlobTxSidecar
	Bloom             = ethtypes.Bloom
	DynamicFeeTx      = ethtypes.DynamicFeeTx
	FrontierSigner    = ethtypes.FrontierSigner
	HomesteadSigner   = ethtypes.HomesteadSigner
	LegacyTx          = ethtypes.LegacyTx
	Receipt           = ethtypes.Receipt
	ReceiptForStorage = ethtypes.ReceiptForStorage
	Receipts          = ethtypes.Receipts
	Signer            = ethtypes.Signer
	SlimAccount       = ethtypes.SlimAccount
	StateAccount      = ethtypes.StateAccount
	Transaction       = ethtypes.Transaction
	Transactions      = ethtypes.Transactions
	TxByNonce         = ethtypes.TxByNonce
	TxData            = ethtypes.TxData
)

// The following constants are used directly as their upstream definitions.
const (
	BloomBitLength          = ethtypes.BloomBitLength
	BloomByteLength         = ethtypes.BloomByteLength
	ReceiptStatusFailed     = ethtypes.ReceiptStatusFailed
	ReceiptStatusSuccessful = ethtypes.ReceiptStatusSuccessful

	// Transaction types.
	AccessListTxType = ethtypes.AccessListTxType
	BlobTxType       = ethtypes.BlobTxType
	DynamicFeeTxType = ethtypes.DynamicFeeTxType
	LegacyTxType     = ethtypes.LegacyTxType
)

// The following functions are used directly as their upstream definitions.
var (
	BloomLookup         = ethtypes.BloomLookup
	BytesToBloom        = ethtypes.BytesToBloom
	CreateBloom         = ethtypes.CreateBloom
	FullAccount         = ethtypes.FullAccount
	FullAccountRLP      = ethtypes.FullAccountRLP
	NewContractCreation = ethtypes.NewContractCreation
	NewReceipt          = ethtypes.NewReceipt
	NewTransaction      = ethtypes.NewTransaction
	SlimAccountRLP      = ethtypes.SlimAccountRLP

	// Signers
	LatestSigner           = ethtypes.LatestSigner
	LatestSignerForChainID = ethtypes.LatestSignerForChainID
	MakeSigner             = ethtypes.MakeSigner
	MustSignNewTx          = ethtypes.MustSignNewTx
	NewEIP155Signer        = ethtypes.NewEIP155Signer
	NewEIP2930Signer       = ethtypes.NewEIP2930Signer
	NewLondonSigner        = ethtypes.NewLondonSigner
	Sender                 = ethtypes.Sender
	SignNewTx              = ethtypes.SignNewTx
	SignTx                 = ethtypes.SignTx

	// Transactions
	NewTx        = ethtypes.NewTx
	TxDifference = ethtypes.TxDifference

	// Errors
	ErrGasFeeCapTooLow    = ethtypes.ErrGasFeeCapTooLow
	ErrTxTypeNotSupported = ethtypes.ErrTxTypeNotSupported
)
