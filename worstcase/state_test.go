// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package worstcase

import (
	"crypto/ecdsa"
	"errors"
	"math"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/txpool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/libevm/ethtest"
	"github.com/ava-labs/libevm/params"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/strevm/blocks"
	"github.com/ava-labs/strevm/blocks/blockstest"
	"github.com/ava-labs/strevm/gastime"
	"github.com/ava-labs/strevm/hook"
	"github.com/ava-labs/strevm/hook/hookstest"
	"github.com/ava-labs/strevm/saetest"
)

type SUT struct {
	*State
	genesis    *blocks.Block
	hooks      *hookstest.Stub
	config     *params.ChainConfig
	stateCache state.Database
	db         ethdb.Database
}

const (
	initialGasTarget = 1_000_000
	initialExcess    = 60_303_807 // Maximum excess that results in gas price of 1
)

func newSUT(tb testing.TB, alloc types.GenesisAlloc) SUT {
	tb.Helper()

	db, cache, _ := ethtest.NewEmptyStateDB(tb)
	config := saetest.ChainConfig()

	genesis := blockstest.NewGenesis(
		tb,
		db,
		saetest.NewExecutionResultsDB(),
		config,
		alloc,
		blockstest.WithGasTarget(initialGasTarget),
		blockstest.WithGasExcess(initialExcess),
	)
	hooks := &hookstest.Stub{
		Target: initialGasTarget,
	}
	s, err := NewState(hooks, config, cache, genesis, nil)
	require.NoError(tb, err, "NewState()")

	return SUT{
		State:      s,
		genesis:    genesis,
		hooks:      hooks,
		config:     config,
		stateCache: cache,
		db:         db,
	}
}

const (
	targetToMaxBlockSize = gastime.TargetToRate * maxGasSecondsPerBlock
	initialMaxBlockSize  = initialGasTarget * targetToMaxBlockSize
)

type (
	Op           = hook.Op
	AccountDebit = hook.AccountDebit
)

func TestMultipleBlocks(t *testing.T) {
	wallet := saetest.NewUNSAFEWallet(t, 1, types.LatestSigner(saetest.ChainConfig()))
	var (
		eoa          = common.Address{0x01}
		eoaNoBalance = common.Address{0x02}
		eoaViaTx     = wallet.Addresses()[0]
	)
	const startingBalance = math.MaxUint64
	sut := newSUT(t, types.GenesisAlloc{
		eoa: {
			Balance: new(big.Int).SetUint64(startingBalance),
		},
		eoaViaTx: {
			Balance: new(big.Int).SetUint64(startingBalance),
		},
	})

	state := sut.State
	lastHash := sut.genesis.Hash()

	const importedAmount = 10
	type op struct {
		name    string
		op      Op
		wantErr error
	}
	tests := []struct { // note that they are not independent
		hooks                 *hookstest.Stub
		time                  uint64
		wantGasLimit          uint64
		wantBaseFee           *uint256.Int
		ops                   []op
		txsAfterOps           []*types.Transaction
		wantMinSenderBalances []map[common.Address]uint64 // transformed to uint256.Int
	}{
		{
			hooks: &hookstest.Stub{
				Target: 2 * initialGasTarget, // Will double the target _after_ this block.
			},
			wantGasLimit: initialMaxBlockSize,
			wantBaseFee:  uint256.NewInt(1),
			ops: []op{
				{
					name: "include_small_operation",
					op: Op{
						Gas:       gas.Gas(params.TxGas),
						GasFeeCap: *uint256.NewInt(1),
						Burn: map[common.Address]hook.AccountDebit{
							eoa: {},
						},
					},
					wantErr: nil,
				},
				{
					name: "would_exceed_limit",
					op: Op{
						Gas:       gas.Gas(initialMaxBlockSize - params.TxGas + 1),
						GasFeeCap: *uint256.NewInt(1),
					},
					wantErr: core.ErrGasLimitReached,
				},
				{
					name: "fill_block",
					op: Op{
						Gas:       gas.Gas(initialMaxBlockSize - params.TxGas),
						GasFeeCap: *uint256.NewInt(1),
					},
					wantErr: nil,
				},
			},
			wantMinSenderBalances: []map[common.Address]uint64{
				{eoa: startingBalance},
				/* wantErr != nil so not included */
				{ /* empty Burn map*/ },
			},
		},
		{
			hooks: &hookstest.Stub{
				Target: initialGasTarget, // Restore the target _after_ this block.
			},
			wantGasLimit: 2 * initialMaxBlockSize,
			wantBaseFee:  uint256.NewInt(2),
			ops: []op{
				{
					name: "import",
					op: Op{
						Gas:       1,
						GasFeeCap: *uint256.NewInt(2),
						Mint: map[common.Address]uint256.Int{
							eoaNoBalance: *uint256.NewInt(importedAmount),
						},
					},
					wantErr: nil,
				},
				{
					name: "imported_funds_insufficient",
					op: Op{
						Gas:       1,
						GasFeeCap: *uint256.NewInt(2),
						Burn: map[common.Address]AccountDebit{
							eoaNoBalance: {
								Amount:     *uint256.NewInt(importedAmount + 1),
								MinBalance: *uint256.NewInt(importedAmount + 1),
							},
						},
					},
					wantErr: core.ErrInsufficientFunds,
				},
				{
					name: "spend_imported_funds",
					op: Op{
						Gas:       1,
						GasFeeCap: *uint256.NewInt(2),
						Burn: map[common.Address]AccountDebit{
							eoaNoBalance: {
								Amount:     *uint256.NewInt(importedAmount),
								MinBalance: *uint256.NewInt(importedAmount),
							},
						},
					},
					wantErr: nil,
				},
			},
			wantMinSenderBalances: []map[common.Address]uint64{
				{ /* empty Burn map */ },
				/* wantErr != nil */
				{eoaNoBalance: importedAmount},
			},
		},
		{
			wantGasLimit: initialMaxBlockSize,
			wantBaseFee:  uint256.NewInt(2),
			txsAfterOps: []*types.Transaction{
				wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
					To:       &common.Address{},
					Gas:      100_000,
					GasPrice: big.NewInt(2),
				}),
				wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
					To:       &common.Address{},
					Gas:      200_000,
					GasPrice: big.NewInt(2),
					Value:    big.NewInt(123_456),
				}),
				wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
					To:       nil,
					Gas:      100_000,
					GasPrice: big.NewInt(10), // charged in full
				}),
				wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
					To:        &common.Address{},
					Gas:       100_000,
					GasTipCap: big.NewInt(1),
					GasFeeCap: big.NewInt(100),
				}),
				wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
					// Irrelevant parameters that only need to be valid. Needed
					// to induce one more sender balance and test the effect of
					// the penultimate tx.
					To:       &common.Address{},
					Gas:      params.TxGas,
					GasPrice: big.NewInt(2),
				}),
			},
			wantMinSenderBalances: []map[common.Address]uint64{
				// Before each tx:
				{eoaViaTx: startingBalance},
				{eoaViaTx: startingBalance - 2*100_000},
				{eoaViaTx: startingBalance - 2*100_000 - (2*200_000 + 123_456)},
				{eoaViaTx: startingBalance - 2*100_000 - (2*200_000 + 123_456) - 10*100_000},             // non-dynamic fee
				{eoaViaTx: startingBalance - 2*100_000 - (2*200_000 + 123_456) - 10*100_000 - 3*100_000}, // dynamic fee: effective gas price = baseFee + gasTipCap
			},
		},
		{
			// We have currently included slightly over 10s worth of gas. We
			// should increase the time by that same amount to restore the base
			// fee.
			time:         21,
			wantGasLimit: initialMaxBlockSize,
			wantBaseFee:  uint256.NewInt(1),
		},
	}
	for i, block := range tests {
		if block.hooks != nil {
			*sut.hooks = *block.hooks
		}
		header := &types.Header{
			ParentHash: lastHash,
			Number:     big.NewInt(int64(i)),
			Time:       block.time,
		}
		require.NoErrorf(t, state.StartBlock(header), "StartBlock(%d)", i)
		require.Equalf(t, block.wantBaseFee, state.BaseFee(), "base fee after StartBlock(%d)", i)
		require.Equalf(t, block.wantGasLimit, state.GasLimit(), "gas limit after StartBlock(%d)", i)

		for _, op := range block.ops {
			gotErr := state.Apply(op.op)
			require.ErrorIsf(t, gotErr, op.wantErr, "Apply(%s) error", op.name)
		}
		for _, tx := range block.txsAfterOps {
			require.NoErrorf(t, state.ApplyTx(tx), "ApplyTx()")
		}

		got, err := state.FinishBlock()
		require.NoError(t, err, "FinishBlock()")

		want := &blocks.WorstCaseBounds{
			MaxBaseFee: block.wantBaseFee,
		}
		for _, bals := range block.wantMinSenderBalances {
			uBals := make(map[common.Address]*uint256.Int)
			for addr, b := range bals {
				uBals[addr] = uint256.NewInt(b)
			}
			want.MinOpBurnerBalances = append(want.MinOpBurnerBalances, uBals)
		}
		if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("FinishBlock() diff (-want +got): \n%s", diff)
		}

		lastHash = header.Hash()
	}
}

func TestTransactionValidation(t *testing.T) {
	// Test parameters from https://eips.ethereum.org/EIPS/eip-3607
	eip3607Key, err := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	require.NoError(t, err, "crypto.HexToECDSA() for eip3607Key")
	eip3607EOA := crypto.PubkeyToAddress(eip3607Key.PublicKey)
	eip3607Alloc := types.Account{
		Balance: big.NewInt(params.Ether),
		Nonce:   0,
		Code:    common.Hex2Bytes("B0B0FACE"),
	}

	defaultKey, err := crypto.GenerateKey()
	require.NoError(t, err, "libevm/crypto.GenerateKey()")
	defaultEOA := crypto.PubkeyToAddress(defaultKey.PublicKey)

	// The referenced clause definitions are documented by Geth in
	// [core.StateTransition.transitionDb].
	tests := []struct {
		name    string
		nonce   uint64
		balance uint64
		tx      types.TxData
		key     *ecdsa.PrivateKey
		wantErr error
	}{
		// Clause 1: the nonce of the message caller is correct
		{
			name: "nonce_too_high",
			tx: &types.LegacyTx{
				Nonce:    1,
				GasPrice: big.NewInt(1),
				Gas:      params.TxGas,
				To:       &common.Address{},
			},
			wantErr: core.ErrNonceTooHigh,
		},
		{
			name:  "nonce_too_low",
			nonce: 1,
			tx: &types.LegacyTx{
				GasPrice: big.NewInt(1),
				Gas:      params.TxGas,
				To:       &common.Address{},
			},
			wantErr: core.ErrNonceTooLow,
		},
		{
			name:  "max_nonce",
			nonce: math.MaxUint64,
			tx: &types.LegacyTx{
				Nonce:    math.MaxUint64,
				GasPrice: big.NewInt(1),
				Gas:      params.TxGas,
				To:       &common.Address{},
			},
			wantErr: core.ErrNonceMax,
		},

		// Clause 2: caller has enough balance to cover transaction fee(gaslimit * gasprice)
		// Clause 6: caller has enough balance to cover asset transfer for **topmost** call
		{
			name: "negative_gas_price",
			tx: &types.LegacyTx{
				GasPrice: big.NewInt(-1),
				Gas:      params.TxGas,
				To:       &common.Address{},
			},
			wantErr: txpool.ErrUnderpriced,
		},
		{
			name: "negative_value",
			tx: &types.LegacyTx{
				GasPrice: big.NewInt(1),
				Gas:      params.TxGas,
				To:       &common.Address{},
				Value:    big.NewInt(-1),
			},
			wantErr: txpool.ErrNegativeValue,
		},
		{
			name: "cost_overflow",
			tx: &types.LegacyTx{
				GasPrice: new(big.Int).Lsh(big.NewInt(1), 256-1),
				Gas:      params.TxGas,
				To:       &common.Address{},
			},
			wantErr: errCostOverflow,
		},
		{
			name: "insufficient_funds",
			tx: &types.LegacyTx{
				Gas:      params.TxGas,
				GasPrice: big.NewInt(1),
				To:       &common.Address{},
				Value:    big.NewInt(0),
			},
			wantErr: core.ErrInsufficientFunds,
		},

		// Clause 3: the amount of gas required is available in the block
		{
			name:    "gas_limit_exceeded",
			balance: initialMaxBlockSize,
			tx: &types.LegacyTx{
				GasPrice: big.NewInt(1),
				Gas:      initialMaxBlockSize + 1,
				To:       &common.Address{},
			},
			wantErr: txpool.ErrGasLimit,
		},

		// Clause 4: the purchased gas is enough to cover intrinsic usage
		{
			name: "not_cover_intrinsic_gas",
			tx: &types.LegacyTx{
				To:  &common.Address{},
				Gas: params.TxGas - 1,
			},
			wantErr: core.ErrIntrinsicGas,
		},

		// Clause 5 (there is no overflow when calculating intrinsic gas) is not
		// tested because it requires constructing such a large transaction that
		// the test would OOM.

		// EIP-1559: onchain gas auction
		{
			name: "gas_fee_cap_very_high",
			tx: &types.DynamicFeeTx{
				GasTipCap: big.NewInt(1),
				GasFeeCap: new(big.Int).Lsh(big.NewInt(1), 256),
				Gas:       params.TxGas,
				To:        &common.Address{},
			},
			wantErr: core.ErrFeeCapVeryHigh,
		},
		{
			name: "gas_tip_cap_very_high",
			tx: &types.DynamicFeeTx{
				GasTipCap: new(big.Int).Lsh(big.NewInt(1), 256),
				GasFeeCap: big.NewInt(1),
				Gas:       params.TxGas,
				To:        &common.Address{},
			},
			wantErr: core.ErrTipVeryHigh,
		},
		{
			name: "gas_tip_above_fee_cap",
			tx: &types.DynamicFeeTx{
				GasTipCap: big.NewInt(2),
				GasFeeCap: big.NewInt(1),
				Gas:       params.TxGas,
				To:        &common.Address{},
			},
			wantErr: core.ErrTipAboveFeeCap,
		},
		{
			name: "gas_fee_cap_too_low",
			tx: &types.DynamicFeeTx{
				GasTipCap: big.NewInt(0),
				GasFeeCap: big.NewInt(0),
				Gas:       params.TxGas,
				To:        &common.Address{},
			},
			wantErr: core.ErrFeeCapTooLow,
		},
		{
			name:    "dynamic_fee_insufficient_for_fee_cap",
			balance: 100_000, // enough for effectiveGasPrice (1) * gas (21000) but not gasFeeCap (100) * gas (21000) = 2_100_000
			tx: &types.DynamicFeeTx{
				GasTipCap: big.NewInt(0),
				GasFeeCap: big.NewInt(100),
				Gas:       params.TxGas,
				To:        &common.Address{},
			},
			wantErr: core.ErrInsufficientFunds,
		},

		// EIP-3607: reject transactions from non-EOAs
		{
			name: "sender_not_eoa",
			tx: &types.LegacyTx{
				GasPrice: big.NewInt(1),
				Gas:      params.TxGas,
				To:       &common.Address{},
			},
			key:     eip3607Key,
			wantErr: core.ErrSenderNoEOA,
		},

		// EIP-3860: limit init code size
		{
			name: "exceed_max_init_code_size",
			tx: &types.LegacyTx{
				Gas:  250_000, // cover intrinsic gas
				To:   nil,     // contract creation
				Data: make([]byte, params.MaxInitCodeSize+1),
			},
			wantErr: core.ErrMaxInitCodeSizeExceeded,
		},

		// Unsupported transaction types
		{
			name: "blob_tx_not_supported",
			tx: &types.BlobTx{
				Gas: params.TxGas,
			},
			wantErr: core.ErrTxTypeNotSupported,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sut := newSUT(t, types.GenesisAlloc{
				defaultEOA: {
					Nonce:   tt.nonce,
					Balance: new(big.Int).SetUint64(tt.balance),
				},
				eip3607EOA: eip3607Alloc,
			})
			state := sut.State

			header := &types.Header{
				ParentHash: sut.genesis.Hash(),
				Number:     big.NewInt(0),
			}
			require.NoErrorf(t, state.StartBlock(header), "StartBlock()")

			key := defaultKey
			if tt.key != nil {
				key = tt.key
			}
			tx := types.MustSignNewTx(key, types.NewCancunSigner(state.config.ChainID), tt.tx)
			gotErr := state.ApplyTx(tx)
			require.ErrorIsf(t, gotErr, tt.wantErr, "ApplyTx() error")
		})
	}
}

// Test that non-consecutive blocks are sanity checked.
func TestStartBlockNonConsecutiveBlocks(t *testing.T) {
	sut := newSUT(t, nil)
	state := sut.State
	genesisHash := sut.genesis.Hash()

	err := state.StartBlock(&types.Header{
		ParentHash: genesisHash,
	})
	require.NoError(t, err, "StartBlock()")

	err = state.StartBlock(&types.Header{
		ParentHash: genesisHash, // Should be the previously provided header's hash
	})
	require.ErrorIs(t, err, errNonConsecutiveBlocks, "non-consecutive StartBlock()")
}

// Test that filling the queue eventually prevents new blocks from being added.
func TestStartBlockQueueFull(t *testing.T) {
	sut := newSUT(t, nil)
	state := sut.State
	lastHash := sut.genesis.Hash()

	// Fill the queue with the minimum amount of gas to prevent additional
	// blocks.
	for number, gas := range []gas.Gas{initialMaxBlockSize, initialMaxBlockSize, 1} {
		h := &types.Header{
			ParentHash: lastHash,
			Number:     big.NewInt(int64(number)),
		}
		require.NoError(t, state.StartBlock(h), "StartBlock()")

		err := state.Apply(Op{
			Gas:       gas,
			GasFeeCap: *uint256.NewInt(2),
		})
		require.NoError(t, err, "Apply()")

		_, err = state.FinishBlock()
		require.NoError(t, err, "FinishBlock()")

		lastHash = h.Hash()
	}

	err := state.StartBlock(&types.Header{
		ParentHash: lastHash,
		Number:     big.NewInt(3),
	})
	require.ErrorIs(t, err, ErrQueueFull, "StartBlock() with full queue")
}

// Test that changing the target can cause the queue to be treated as full.
func TestStartBlockQueueFullDueToTargetChanges(t *testing.T) {
	sut := newSUT(t, nil)
	state := sut.State

	sut.hooks.Target = 1 // applied after the first block
	h := &types.Header{
		ParentHash: sut.genesis.Hash(),
		Number:     big.NewInt(0),
	}
	require.NoError(t, state.StartBlock(h), "StartBlock()")

	err := state.Apply(Op{
		Gas:       initialMaxBlockSize,
		GasFeeCap: *uint256.NewInt(1),
	})
	require.NoError(t, err, "Apply()")

	_, err = state.FinishBlock()
	require.NoError(t, err, "FinishBlock()")

	err = state.StartBlock(&types.Header{
		ParentHash: h.Hash(),
		Number:     big.NewInt(1),
	})
	require.ErrorIs(t, err, ErrQueueFull, "StartBlock() with full queue")
}

func TestCanExecuteTransactionHook(t *testing.T) {
	config := saetest.ChainConfig()
	signer := types.LatestSigner(config)
	const (
		blocked = iota
		allowed

		numAccounts
	)
	wallet := saetest.NewUNSAFEWallet(t, numAccounts, signer)
	sut := newSUT(t, saetest.MaxAllocFor(wallet.Addresses()...))

	errSenderBlocked := errors.New("sender blocked by allowlist")
	sut.hooks.CanExecuteTransactionFn = func(from common.Address, _ *common.Address, _ libevm.StateReader) error {
		if from == wallet.Addresses()[blocked] {
			return errSenderBlocked
		}
		return nil
	}

	header := &types.Header{
		ParentHash: sut.genesis.Hash(),
		Number:     big.NewInt(1),
	}
	require.NoError(t, sut.StartBlock(header), "StartBlock()")

	tests := []struct {
		name           string
		account        int
		wantErr        error
		wantNonceAfter uint64
	}{
		{
			name:           "blocked_sender_rejected",
			account:        blocked,
			wantErr:        errSenderBlocked,
			wantNonceAfter: 0,
		},
		{
			name:           "allowed_sender_accepted",
			account:        allowed,
			wantErr:        nil,
			wantNonceAfter: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := wallet.SetNonceAndSign(t, tt.account, &types.DynamicFeeTx{
				GasFeeCap: big.NewInt(1),
				Gas:       params.TxGas,
				To:        &common.Address{},
			})
			require.ErrorIsf(t, sut.ApplyTx(tx), tt.wantErr, "ApplyTx() error")
			assert.Equalf(t, tt.wantNonceAfter, sut.State.db.GetNonce(wallet.Addresses()[tt.account]), "sender nonce after ApplyTx()")
		})
	}
}
