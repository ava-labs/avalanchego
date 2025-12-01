// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutils

import (
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/subnet-evm/accounts/abi/bind"

	sim "github.com/ava-labs/subnet-evm/ethclient/simulated"
)

// NewAuth creates a new transactor with the given private key and chain ID.
func NewAuth(t *testing.T, key *ecdsa.PrivateKey, chainID *big.Int) *bind.TransactOpts {
	t.Helper()
	auth, err := bind.NewKeyedTransactorWithChainID(key, chainID)
	require.NoError(t, err)
	return auth
}

// WaitReceipt commits the simulated backend and waits for the transaction receipt.
func WaitReceipt(t *testing.T, b *sim.Backend, tx *types.Transaction) *types.Receipt {
	t.Helper()
	b.Commit(true)
	receipt, err := b.Client().TransactionReceipt(t.Context(), tx.Hash())
	require.NoError(t, err, "failed to get transaction receipt")
	return receipt
}

// WaitReceiptSuccessful commits the backend, waits for the receipt, and asserts success.
func WaitReceiptSuccessful(t *testing.T, b *sim.Backend, tx *types.Transaction) *types.Receipt {
	t.Helper()
	receipt := WaitReceipt(t, b, tx)
	require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status, "transaction should succeed")
	return receipt
}
