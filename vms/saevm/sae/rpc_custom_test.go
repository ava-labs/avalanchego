// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"fmt"
	"math/big"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/libevm/ethapi"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/rpc"
	"github.com/google/go-cmp/cmp"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/cmputils"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest/escrow"

	saerpc "github.com/ava-labs/avalanchego/vms/saevm/sae/rpc"
)

func TestGetChainConfig(t *testing.T) {
	ctx, sut := newSUT(t, 0)
	sut.testRPC(ctx, t, rpcTest{
		method: "eth_getChainConfig",
		want:   *saetest.ChainConfig(),
	})
}

func TestBaseFee(t *testing.T) {
	ctx, sut := newSUT(t, 0)
	sut.testRPC(ctx, t, rpcTest{
		method: "eth_baseFee",
		want:   hexBig(params.InitialBaseFee),
	})

	b := sut.runConsensusLoop(t)
	sut.testRPC(ctx, t, rpcTest{
		method: "eth_baseFee",
		want:   (*hexutil.Big)(b.WorstCaseBounds().LatestEndTime.BaseFee().ToBig()),
	})
}

func TestSuggestPriceOptions(t *testing.T) {
	ctx, sut := newSUT(t, 0)
	// Before any blocks with worst-case bounds, the base fee falls back to the
	// genesis base fee and the tip defaults to the minimum (no txs yet).
	sut.testRPC(ctx, t, rpcTest{
		method: "eth_suggestPriceOptions",
		want:   saerpc.NewPriceOptions(big.NewInt(params.Wei), big.NewInt(2*params.InitialBaseFee)),
	})

	b := sut.runConsensusLoop(t)

	// This just asserts the round-tripping of the PriceOptions through the RPC.
	// See testing of [saerpc.NewPriceOptions] for behavioral tests.
	tip, err := sut.rawVM.GethRPCBackends().SuggestGasTipCap(t.Context())
	require.NoErrorf(t, err, "SuggestGasTipCap()")
	doubleBaseFee := b.WorstCaseBounds().LatestEndTime.BaseFee().ToBig()
	doubleBaseFee.Lsh(doubleBaseFee, 1)
	sut.testRPC(ctx, t, rpcTest{
		method: "eth_suggestPriceOptions",
		want:   saerpc.NewPriceOptions(tip, doubleBaseFee),
	})
}

func TestNewAcceptedTransactions(t *testing.T) {
	ctx, sut := newSUT(t, 1)

	hashes := make(chan common.Hash, 1)
	{
		sub, err := sut.rpcClient.EthSubscribe(ctx, hashes, "newAcceptedTransactions")
		require.NoErrorf(t, err, "EthSubscribe(newAcceptedTransactions)")
		t.Cleanup(sub.Unsubscribe)
	}
	txs := make(chan *ethapi.RPCTransaction, 1)
	{
		sub, err := sut.rpcClient.EthSubscribe(ctx, txs, "newAcceptedTransactions", true)
		require.NoErrorf(t, err, "EthSubscribe(newAcceptedTransactions, true)")
		t.Cleanup(sub.Unsubscribe)
	}

	tx := sut.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
		To:       &zeroAddr,
		Gas:      params.TxGas,
		GasPrice: big.NewInt(1),
	})
	block := sut.runConsensusLoop(t, tx)

	t.Run("hash_only", func(t *testing.T) {
		require.Equalf(t, tx.Hash(), <-hashes, "accepted tx hash")
	})
	t.Run("full_tx", func(t *testing.T) {
		want := ethapi.NewRPCTransaction(tx, block.Hash(), block.NumberU64(), block.BuildTime(), 0, block.Header().BaseFee, saetest.ChainConfig())
		if diff := cmp.Diff(want, <-txs, cmputils.HexutilBigs(), cmputils.NilSlicesAreEmpty[hexutil.Bytes]()); diff != "" {
			t.Errorf("full tx diff (-want +got):\n%s", diff)
		}
	})
}

func TestCallDetailed(t *testing.T) {
	echoReverter := common.Address{'e', 'c', 'h', 'o'}
	invalidJumper := common.Address{'i', 'n', 'v', 'a', 'l', 'i', 'd'}
	const gasCap = 100e6
	ctx, sut := newSUT(t, 1, options.Func[sutConfig](func(c *sutConfig) {
		c.vmConfig.RPCConfig.GasCap = gasCap

		const (
			size   = byte(vm.CALLDATASIZE)
			cp     = byte(vm.CALLDATACOPY)
			zero   = byte(vm.PUSH0)
			revert = byte(vm.REVERT)
			jump   = byte(vm.JUMP)
		)
		c.genesis.Alloc[echoReverter] = types.Account{
			Code: []byte{
				size, zero, zero, cp, // https://www.evm.codes/#37
				size, zero, revert,
			},
			Balance: new(big.Int),
		}
		c.genesis.Alloc[invalidJumper] = types.Account{
			// Jumping back to PC=0 is invalid because it's not a [vm.JUMPDEST]
			Code:    []byte{zero, jump},
			Balance: new(big.Int),
		}
	}))

	deploy := &types.LegacyTx{
		Gas:      1e6,
		GasPrice: big.NewInt(1),
		Data:     escrow.CreationCode(),
	}

	escrowAddr := crypto.CreateAddress(sut.wallet.Addresses()[0], 0)
	recv := common.Address{'r', 'e', 'c', 'v'}
	const depositVal = 42
	deposit := &types.LegacyTx{
		To:       &escrowAddr,
		Gas:      1e6,
		GasPrice: big.NewInt(1),
		Data:     escrow.CallDataToDeposit(recv),
		Value:    big.NewInt(depositVal),
	}

	sign := sut.wallet.SetNonceAndSign
	b := sut.runConsensusLoop(t, sign(t, 0, deploy), sign(t, 0, deposit))
	require.Len(t, b.Transactions(), 2, "tx count")
	require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
	for _, r := range b.Receipts() {
		require.Equalf(t, types.ReceiptStatusSuccessful, r.Status, "%T.Status", r)
	}

	const revertWith = 12345
	revertAsPanic := slices.Concat(
		crypto.Keccak256([]byte("Panic(uint256)"))[:4],
		uint256.NewInt(revertWith).PaddedBytes(32),
	)

	noBalance := common.Address{'b', 'a', 'n', 'k', 'r', 'u', 'p', 't'}
	latest := rpc.LatestBlockNumber.String()

	// revertErrCode is the JSON-RPC error code for execution reverts, matching the
	// value returned by [ethapi.RevertError.ErrorCode].
	const revertErrCode = 3

	sut.testRPC(ctx, t, []rpcTest{
		{
			method: "eth_callDetailed",
			args: []any{
				map[string]any{
					"to":   escrowAddr,
					"data": hexutil.Encode(escrow.CallDataForBalance(recv)),
				},
				latest,
			},
			want: saerpc.DetailedExecutionResult{
				UsedGas:    23675,
				ReturnData: uint256.NewInt(depositVal).PaddedBytes(32),
			},
		},
		{
			method: "eth_callDetailed",
			args: []any{
				map[string]any{
					"to":   escrowAddr,
					"from": noBalance,
					"data": hexutil.Encode(escrow.CallDataToWithdraw()),
				},
				latest,
			},
			want: saerpc.DetailedExecutionResult{
				UsedGas: 23451,
				ErrCode: revertErrCode,
				Err:     vm.ErrExecutionReverted.Error(),
				ReturnData: slices.Concat(
					crypto.Keccak256([]byte("ZeroBalance(address)"))[:4],
					make([]byte, common.HashLength-common.AddressLength),
					noBalance.Bytes(),
				),
			},
		},
		{
			method: "eth_callDetailed",
			args: []any{
				map[string]any{
					"to":   echoReverter,
					"data": hexutil.Bytes{42},
				},
				latest,
			},
			want: saerpc.DetailedExecutionResult{
				UsedGas:    21035,
				ErrCode:    revertErrCode,
				Err:        vm.ErrExecutionReverted.Error(),
				ReturnData: hexutil.Bytes{42},
			},
		},
		{
			method: "eth_callDetailed",
			args: []any{
				map[string]any{
					"to":   echoReverter,
					"data": hexutil.Bytes(revertAsPanic),
				},
				latest,
			},
			want: saerpc.DetailedExecutionResult{
				UsedGas:    21241,
				ErrCode:    revertErrCode,
				Err:        fmt.Sprintf("%v: unknown panic code: %#x", vm.ErrExecutionReverted, revertWith),
				ReturnData: hexutil.Bytes(revertAsPanic),
			},
		},
		{
			method: "eth_callDetailed",
			args: []any{
				map[string]any{
					"to": invalidJumper,
				},
				latest,
			},
			want: saerpc.DetailedExecutionResult{
				UsedGas: gasCap,
				Err:     vm.ErrInvalidJump.Error(),
			},
		},
	}...)
}
