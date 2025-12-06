// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutils

import (
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/subnet-evm/accounts/abi/bind"
	"github.com/ava-labs/subnet-evm/eth/ethconfig"
	"github.com/ava-labs/subnet-evm/node"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/params/extras"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"

	sim "github.com/ava-labs/subnet-evm/ethclient/simulated"
)

// NewAuth creates a new transactor with the given private key and chain ID.
func NewAuth(t *testing.T, key *ecdsa.PrivateKey, chainID *big.Int) *bind.TransactOpts {
	t.Helper()
	auth, err := bind.NewKeyedTransactorWithChainID(key, chainID)
	require.NoError(t, err)
	return auth
}

// NewBackendWithPrecompile creates a simulated backend with the given precompile enabled
// at genesis and funds the specified addresses with 1 ETH each. Additional options can be passed
// to configure the backend.
func NewBackendWithPrecompile(
	t *testing.T,
	precompileCfg precompileconfig.Config,
	fundedAddrs []common.Address,
	opts ...func(*node.Config, *ethconfig.Config),
) *sim.Backend {
	t.Helper()
	chainCfg := params.Copy(params.TestChainConfig)
	params.GetExtra(&chainCfg).GenesisPrecompiles = extras.Precompiles{
		precompileCfg.Key(): precompileCfg,
	}

	genesisAlloc := make(types.GenesisAlloc)
	for _, addr := range fundedAddrs {
		genesisAlloc[addr] = types.Account{Balance: big.NewInt(1000000000000000000)}
	}

	opts = append(opts, sim.WithChainConfig(&chainCfg))
	return sim.NewBackend(genesisAlloc, opts...)
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
