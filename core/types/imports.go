// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	ethtypes "github.com/ava-labs/libevm/core/types"
)

// The following types are used directly as their upstream definitions.
type (
	AccessList        = ethtypes.AccessList
	AccessListTx      = ethtypes.AccessListTx
	AccessTuple       = ethtypes.AccessTuple
	Account           = ethtypes.Account
	BlobTx            = ethtypes.BlobTx
	BlobTxSidecar     = ethtypes.BlobTxSidecar
	Block             = ethtypes.Block
	BlockNonce        = ethtypes.BlockNonce
	Blocks            = ethtypes.Blocks
	Bloom             = ethtypes.Bloom
	Body              = ethtypes.Body
	DerivableList     = ethtypes.DerivableList
	DynamicFeeTx      = ethtypes.DynamicFeeTx
	GenesisAlloc      = ethtypes.GenesisAlloc
	Header            = ethtypes.Header
	HomesteadSigner   = ethtypes.HomesteadSigner
	LegacyTx          = ethtypes.LegacyTx
	Log               = ethtypes.Log
	Receipt           = ethtypes.Receipt
	ReceiptForStorage = ethtypes.ReceiptForStorage
	Receipts          = ethtypes.Receipts
	Signer            = ethtypes.Signer
	SlimAccount       = ethtypes.SlimAccount
	StateAccount      = ethtypes.StateAccount
	Transaction       = ethtypes.Transaction
	Transactions      = ethtypes.Transactions
	TrieHasher        = ethtypes.TrieHasher
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

// The following variables are used directly as their upstream definitions.
var (
	EmptyRootHash     = ethtypes.EmptyRootHash
	EmptyUncleHash    = ethtypes.EmptyUncleHash
	EmptyCodeHash     = ethtypes.EmptyCodeHash
	EmptyTxsHash      = ethtypes.EmptyTxsHash
	EmptyReceiptsHash = ethtypes.EmptyReceiptsHash
	EmptyVerkleHash   = ethtypes.EmptyVerkleHash
)

// The following functions are used directly as their upstream definitions.
var (
	BloomLookup          = ethtypes.BloomLookup
	BytesToBloom         = ethtypes.BytesToBloom
	CalcUncleHash        = ethtypes.CalcUncleHash
	CopyHeader           = ethtypes.CopyHeader
	CreateBloom          = ethtypes.CreateBloom
	DeriveSha            = ethtypes.DeriveSha
	EncodeNonce          = ethtypes.EncodeNonce
	FullAccount          = ethtypes.FullAccount
	FullAccountRLP       = ethtypes.FullAccountRLP
	NewBlock             = ethtypes.NewBlock
	NewBlockWithHeader   = ethtypes.NewBlockWithHeader
	NewContractCreation  = ethtypes.NewContractCreation
	NewEmptyStateAccount = ethtypes.NewEmptyStateAccount
	NewReceipt           = ethtypes.NewReceipt
	NewTransaction       = ethtypes.NewTransaction
	SlimAccountRLP       = ethtypes.SlimAccountRLP
	TrieRootHash         = ethtypes.TrieRootHash

	// Signers
	LatestSigner           = ethtypes.LatestSigner
	LatestSignerForChainID = ethtypes.LatestSignerForChainID
	MakeSigner             = ethtypes.MakeSigner
	MustSignNewTx          = ethtypes.MustSignNewTx
	NewCancunSigner        = ethtypes.NewCancunSigner
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
