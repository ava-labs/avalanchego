// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockstest

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/consensus"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/params"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/strevm/saetest"
)

func TestIntegration(t *testing.T) {
	const (
		numAccounts           = 2
		numBlocks             = 3
		txsPerAccountPerBlock = 3
	)

	config := saetest.ChainConfig()
	wallet := saetest.NewUNSAFEWallet(t, numAccounts, types.LatestSigner(config))
	alloc := saetest.MaxAllocFor(wallet.Addresses()...)

	db := rawdb.NewMemoryDatabase()

	// Although the point of SAE is to replace [core.BlockChain], it remains the
	// ground truth for correct block and transaction processing. If we were to
	// test [ChainBuilder] with an SAE executor, which is itself going to be
	// tested with the builder, then we would have a circular argument for
	// correctness.
	bc, err := core.NewBlockChain(
		db, nil,
		&core.Genesis{
			Config: config,
			Alloc:  alloc,
		},
		nil, engine{}, vm.Config{},
		func(*types.Header) bool { return true },
		nil,
	)
	require.NoError(t, err, "core.NewBlockChain()")
	stateProc := core.NewStateProcessor(config, bc, engine{})

	sdb, err := state.New(bc.Genesis().Root(), state.NewDatabase(db), nil)
	require.NoError(t, err, "state.New(%T.Genesis().Root())", bc)

	build := NewChainBuilder(config, NewBlock(t, bc.Genesis(), nil, nil))
	dest := common.Address{'d', 'e', 's', 't'}
	for i := range numBlocks {
		// Genesis is block 0
		blockNum := uint64(i + 1) //nolint:gosec // Known to not overflow

		var txs types.Transactions
		for range txsPerAccountPerBlock {
			for i := range numAccounts {
				tx := wallet.SetNonceAndSign(t, i, &types.LegacyTx{
					To:       &dest,
					Value:    big.NewInt(1),
					Gas:      params.TxGas,
					GasPrice: big.NewInt(1),
				})
				txs = append(txs, tx)
			}
		}
		b := build.NewBlock(t, txs, WithEthBlockOptions(
			ModifyHeader(func(h *types.Header) {
				h.GasLimit = 100e6
			})),
		)

		receipts, _, _, err := stateProc.Process(b.EthBlock(), sdb, *bc.GetVMConfig())
		require.NoError(t, err, "%T.Process(%T.NewBlock().EthBlock()...)", stateProc, build)
		for _, r := range receipts {
			assert.Equal(t, types.ReceiptStatusSuccessful, r.Status, "%T.Status", r)
			assert.Equal(t, blockNum, r.BlockNumber.Uint64(), "%T.BlockNumber", r)
		}
	}

	t.Run("balance_of_recipient", func(t *testing.T) {
		bal := sdb.GetBalance(dest)
		require.True(t, bal.IsUint64(), "%T.GetBalance(...).IsUint64()", sdb)
		require.Equal(t, uint64(numAccounts*numBlocks*txsPerAccountPerBlock), bal.Uint64())
	})
}

// engine is a fake [consensus.Engine], implementing the minimum number of
// methods to avoid a panic.
type engine struct {
	consensus.Engine
}

func (engine) VerifyHeader(consensus.ChainHeaderReader, *types.Header) error {
	return nil
}

func (engine) Author(*types.Header) (common.Address, error) {
	return common.Address{'a', 'u', 't', 'h'}, nil
}

func (engine) Finalize(consensus.ChainHeaderReader, *types.Header, *state.StateDB, []*types.Transaction, []*types.Header, []*types.Withdrawal) {
}

func TestNewGenesis(t *testing.T) {
	config := saetest.ChainConfig()
	signer := types.LatestSigner(config)
	wallet := saetest.NewUNSAFEWallet(t, 10, signer)
	alloc := saetest.MaxAllocFor(wallet.Addresses()...)

	db := rawdb.NewMemoryDatabase()
	xdb := saetest.NewExecutionResultsDB()
	gen := NewGenesis(t, db, xdb, config, alloc)

	assert.True(t, gen.Executed(), "genesis.Executed()")
	assert.NoError(t, gen.WaitUntilSettled(t.Context()), "genesis.WaitUntilSettled()")
	assert.Equal(t, gen.Hash(), gen.LastSettled().Hash(), "genesis.LastSettled().Hash() is self")

	t.Run("alloc", func(t *testing.T) {
		sdb, err := state.New(gen.SettledStateRoot(), state.NewDatabase(db), nil)
		require.NoError(t, err, "state.New(genesis.SettledStateRoot())")
		for i, addr := range wallet.Addresses() {
			want := new(uint256.Int).SetAllOne()
			assert.Truef(t, sdb.GetBalance(addr).Eq(want), "%T.GetBalance(%T.Addresses()[%d]) is max uint256", sdb, wallet, i)
		}
	})
}
