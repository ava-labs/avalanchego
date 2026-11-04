// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saexec

import (
	"math/big"
	"math/rand/v2"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/params"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks/blockstest"
	"github.com/ava-labs/avalanchego/vms/saevm/plantstore"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest/escrow"
)

// TestPlantStores executes the same blocks (a contract deployment, deposits
// that write its storage, transfers to new accounts) on hashdb and on the
// plant's null and ours stores. The ours roots MUST equal hashdb's; the null
// roots only need the state to read the same.
func TestPlantStores(t *testing.T) {
	const numBlocks = 20

	type result struct {
		roots   []common.Hash
		escrow  common.Address
		balance *uint256.Int
		deposit *uint256.Int
	}
	run := func(t *testing.T, scheme string) result {
		ctx, sut := newSUT(t, options.Func[sutConfig](func(c *sutConfig) {
			c.dbScheme = scheme
		}))
		e, chain, wallet := sut.Executor, sut.chain, sut.wallet
		eoa := wallet.Addresses()[0]

		deploy := wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
			Data:     escrow.CreationCode(),
			GasPrice: big.NewInt(1),
			Gas:      1e7,
		})
		contract := crypto.CreateAddress(eoa, deploy.Nonce())

		rng := rand.New(rand.NewPCG(1, 2)) //#nosec G404 -- reproducible test data
		var deposited uint64
		for i := range numBlocks {
			var txs types.Transactions
			if i == 0 {
				txs = append(txs, deploy)
			}
			for range 3 {
				val := rng.Uint64N(100_000)
				deposited += val
				txs = append(txs, wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
					To:       &contract,
					Value:    new(big.Int).SetUint64(val),
					GasPrice: big.NewInt(1),
					Gas:      1e6,
					Data:     escrow.CallDataToDeposit(common.BigToAddress(big.NewInt(int64(rng.IntN(5))))),
				}))
			}
			to := common.BigToAddress(big.NewInt(int64(0x5000 + i))) // a new account
			txs = append(txs, wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
				To:       &to,
				Value:    big.NewInt(1),
				Gas:      params.TxGas,
				GasPrice: big.NewInt(1),
			}))
			b := chain.NewBlock(t, txs, blockstest.WithEthBlockOptions(
				blockstest.ModifyHeader(func(h *types.Header) {
					h.Root = chain.Last().PostExecutionStateRoot() // settles the previous block
				}),
			))
			require.NoErrorf(t, e.Enqueue(ctx, b), "%T.Enqueue()", e)
		}
		final := chain.Last()
		require.NoErrorf(t, final.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", final)

		var res result
		for _, b := range chain.AllBlocks() {
			res.roots = append(res.roots, b.PostExecutionStateRoot())
		}

		sdb, err := e.StateDB(final.PostExecutionStateRoot())
		require.NoError(t, err)
		res.escrow, res.balance = contract, sdb.GetBalance(contract)
		require.Equal(t, uint256.NewInt(deposited), res.balance, "escrow balance")

		evm := vm.NewEVM(vm.BlockContext{BlockNumber: big.NewInt(1), Time: 1, Random: &common.Hash{}}, vm.TxContext{}, sdb, e.ChainConfig(), vm.Config{})
		got, _, err := evm.StaticCall(vm.AccountRef(eoa), contract, escrow.CallDataForBalance(common.Address{}), 1e6)
		require.NoError(t, err, "Escrow.balance()")
		res.deposit = new(uint256.Int).SetBytes(got)
		require.NoError(t, sdb.Error())
		return res
	}

	hash := run(t, rawdb.HashScheme)
	for _, scheme := range []string{plantstore.NullScheme, plantstore.OursScheme} {
		t.Run(scheme, func(t *testing.T) {
			got := run(t, scheme)
			require.Equal(t, hash.roots[0], got.roots[0], "genesis root")
			if scheme == plantstore.OursScheme {
				require.Equal(t, hash.roots, got.roots, "post-execution roots")
			} else {
				require.NotEqual(t, hash.roots[numBlocks], got.roots[numBlocks], "null roots are fake")
			}
			require.Equal(t, hash.balance, got.balance, "escrow balance")
			require.Equal(t, hash.deposit, got.deposit, "Escrow.balance(0x0)")
		})
	}
}
