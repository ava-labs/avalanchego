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
	"github.com/ava-labs/avalanchego/vms/saevm/saetest/rpctest"

	saerpc "github.com/ava-labs/avalanchego/vms/saevm/sae/rpc"
)

func TestGetChainConfig(t *testing.T) {
	ctx, sut := newSUT(t, 0)
	sut.testRPC(ctx, t, rpctest.Case{
		Method: "eth_getChainConfig",
		Want:   *saetest.ChainConfig(),
	})
}

func withGenesisBaseFee(fee uint64) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.genesis.BaseFee = new(big.Int).SetUint64(fee)
	})
}

func TestBaseFee(t *testing.T) {
	ctx, sut := newSUT(t, 0, withGenesisBaseFee(params.InitialBaseFee))
	sut.testRPC(ctx, t, rpctest.Case{
		Method: "eth_baseFee",
		Want:   hexBig(params.InitialBaseFee),
	})

	b := sut.runConsensusLoop(t)
	sut.testRPC(ctx, t, rpctest.Case{
		Method: "eth_baseFee",
		Want:   (*hexutil.Big)(b.WorstCaseBounds().LatestEndTime.BaseFee().ToBig()),
	})
}

func TestSuggestPriceOptions(t *testing.T) {
	ctx, sut := newSUT(t, 0, withGenesisBaseFee(params.InitialBaseFee))
	// Before any blocks with worst-case bounds, the base fee falls back to the
	// genesis base fee and the tip defaults to the minimum (no txs yet).
	sut.testRPC(ctx, t, rpctest.Case{
		Method: "eth_suggestPriceOptions",
		Want:   saerpc.NewPriceOptions(big.NewInt(params.Wei), big.NewInt(2*params.InitialBaseFee)),
	})

	b := sut.runConsensusLoop(t)

	// This just asserts the round-tripping of the PriceOptions through the RPC.
	// See testing of [saerpc.NewPriceOptions] for behavioral tests.
	tip, err := sut.rawVM.GethRPCBackends().SuggestGasTipCap(t.Context())
	require.NoErrorf(t, err, "SuggestGasTipCap()")
	doubleBaseFee := b.WorstCaseBounds().LatestEndTime.BaseFee().ToBig()
	doubleBaseFee.Lsh(doubleBaseFee, 1)
	sut.testRPC(ctx, t, rpctest.Case{
		Method: "eth_suggestPriceOptions",
		Want:   saerpc.NewPriceOptions(tip, doubleBaseFee),
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

		c.genesis.Alloc[echoReverter] = types.Account{
			Code: saetest.Ops(
				vm.CALLDATASIZE, vm.PUSH0, vm.PUSH0, vm.CALLDATACOPY, // https://www.evm.codes/#37
				vm.CALLDATASIZE, vm.PUSH0, vm.REVERT,
			),
			Balance: new(big.Int),
		}
		c.genesis.Alloc[invalidJumper] = types.Account{
			// Jumping back to PC=0 is invalid because it's not a [vm.JUMPDEST]
			Code:    saetest.Ops(vm.PUSH0, vm.JUMP),
			Balance: new(big.Int),
		}
	}))

	const escrowDepositVal = 42
	recipient := common.Address{'r', 'e', 'c', 'v'}
	_, escrowAddr, _ := sut.deployEscrow(t)
	sut.depositToEscrow(t, escrowAddr, recipient, big.NewInt(escrowDepositVal))

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

	sut.testRPC(ctx, t, []rpctest.Case{
		{
			Method: "eth_callDetailed",
			Args: []any{
				map[string]any{
					"to":   escrowAddr,
					"data": hexutil.Encode(escrow.CallDataForBalance(recipient)),
				},
				latest,
			},
			Want: saerpc.DetailedExecutionResult{
				UsedGas:    23675,
				ReturnData: uint256.NewInt(escrowDepositVal).PaddedBytes(32),
			},
		},
		{
			Method: "eth_callDetailed",
			Args: []any{
				map[string]any{
					"to":   escrowAddr,
					"from": noBalance,
					"data": hexutil.Encode(escrow.CallDataToWithdraw()),
				},
				latest,
			},
			Want: saerpc.DetailedExecutionResult{
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
			Method: "eth_callDetailed",
			Args: []any{
				map[string]any{
					"to":   echoReverter,
					"data": hexutil.Bytes{42},
				},
				latest,
			},
			Want: saerpc.DetailedExecutionResult{
				UsedGas:    21035,
				ErrCode:    revertErrCode,
				Err:        vm.ErrExecutionReverted.Error(),
				ReturnData: hexutil.Bytes{42},
			},
		},
		{
			Method: "eth_callDetailed",
			Args: []any{
				map[string]any{
					"to":   echoReverter,
					"data": hexutil.Bytes(revertAsPanic),
				},
				latest,
			},
			Want: saerpc.DetailedExecutionResult{
				UsedGas:    21241,
				ErrCode:    revertErrCode,
				Err:        fmt.Sprintf("%v: unknown panic code: %#x", vm.ErrExecutionReverted, revertWith),
				ReturnData: hexutil.Bytes(revertAsPanic),
			},
		},
		{
			Method: "eth_callDetailed",
			Args: []any{
				map[string]any{
					"to": invalidJumper,
				},
				latest,
			},
			Want: saerpc.DetailedExecutionResult{
				UsedGas: gasCap,
				Err:     vm.ErrInvalidJump.Error(),
			},
		},
	}...)
}
