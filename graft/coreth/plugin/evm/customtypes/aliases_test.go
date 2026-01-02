// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import "github.com/ava-labs/libevm/core/types"

// TODO(arr4n) These tests were originally part of the `coreth/core/types`
// package so assume the presence of identifiers. Aliases reduce PR noise during
// the refactoring.

type (
	AccessListTx      = types.AccessListTx
	Block             = types.Block
	BlockNonce        = types.BlockNonce
	Bloom             = types.Bloom
	Body              = types.Body
	DynamicFeeTx      = types.DynamicFeeTx
	Header            = types.Header
	LegacyTx          = types.LegacyTx
	Log               = types.Log
	Receipt           = types.Receipt
	ReceiptForStorage = types.ReceiptForStorage
	Receipts          = types.Receipts
	Transaction       = types.Transaction
	Transactions      = types.Transactions
	Withdrawal        = types.Withdrawal
	Withdrawals       = types.Withdrawals
)

var (
	// Function aliases in production code MUST be `func` declarations, not
	// variables; this is only acceptable in tests.
	CopyHeader      = types.CopyHeader
	MustSignNewTx   = types.MustSignNewTx
	NewBlock        = types.NewBlock
	NewLondonSigner = types.NewLondonSigner
	NewTransaction  = types.NewTransaction
)

const ReceiptStatusSuccessful = types.ReceiptStatusSuccessful
