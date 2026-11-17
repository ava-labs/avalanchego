package sae

import (
	"fmt"
	"math"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/vms/evm/cmputils"
	"github.com/ava-labs/avalanchego/vms/evm/saetest"
	"github.com/ava-labs/avalanchego/vms/evm/topofblock"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/eth/tracers/logger"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

func TestTopOfBlockAuction(t *testing.T) {
	beneficiary := common.Address{'p', 'a', 'y', ' ', 'm', 'e', ' ', 'S', 'e', 'y', 'm', 'o', 'u', 'r'}

	beneficiaryBalance := common.Address{'b', 'a', 'l'}
	balancePrecompile := withPrecompile(beneficiaryBalance, vm.NewStatefulPrecompile(func(env vm.PrecompileEnvironment, _ []byte) ([]byte, error) {
		return env.ReadOnlyState().GetBalance(beneficiary).PaddedBytes(32), nil
	}))

	base := types.DynamicFeeTx{
		To:        &beneficiaryBalance,
		Gas:       1e6,
		GasFeeCap: big.NewInt(100),
	}
	withBid := func(tb testing.TB, w *saetest.Wallet, account int, bid uint64) *types.Transaction {
		tb.Helper()
		tx := base
		tx.AccessList = types.AccessList{
			topofblock.Bid(*uint256.NewInt(bid)),
		}
		return w.SetNonceAndSign(t, account, &tx)
	}
	withTip := func(tb testing.TB, w *saetest.Wallet, account int, tip uint64) *types.Transaction {
		tb.Helper()
		tx := base
		tx.GasTipCap = new(big.Int).SetUint64(tip)
		return w.SetNonceAndSign(t, account, &tx)
	}
	withNothing := func(tb testing.TB, w *saetest.Wallet, account int) *types.Transaction {
		tb.Helper()
		tx := base
		return w.SetNonceAndSign(t, account, &tx)
	}

	const (
		alice = iota
		bob
		charlie
		dave

		numAccounts
	)

	ignore := cmp.Options{
		cmpopts.IgnoreFields(
			types.Receipt{},
			"Type", "Bloom",
			"BlockNumber", "BlockHash",
			"CumulativeGasUsed", "GasUsed", "GasExecuted", "EffectiveGasPrice",
		),
		cmpopts.IgnoreFields(
			types.Log{},
			"BlockNumber", "BlockHash", "TxHash",
		),
	}
	receipt := func(tx *types.Transaction, logs ...*types.Log) *types.Receipt {
		return &types.Receipt{
			Status: types.ReceiptStatusSuccessful,
			TxHash: tx.Hash(),
			Logs:   logs,
		}
	}
	eoas := saetest.NewUNSAFEKeyChain(t, numAccounts).Addresses()
	settlement := func(account int, bid uint64) *types.Log {
		return topofblock.SettledAuctionLog(eoas[account], beneficiary, *uint256.NewInt(bid))
	}
	enable := []sutOption{withTopOfBlockAuctions(beneficiary)}

	tests := []struct {
		name    string
		opts    []sutOption
		mempool func(testing.TB, *saetest.Wallet) types.Transactions
		// want receives the mempool transactions, in the same order, and
		// returns a set of receipts per block. The test will issue blocks until
		// an empty one is produce, which does not need to be represented in the
		// returned slice.
		want                   func(types.Transactions) []types.Receipts
		wantBeneficiaryBalance []uint64
	}{
		{
			name: "disabled",
			opts: nil,
			mempool: func(tb testing.TB, w *saetest.Wallet) types.Transactions {
				tb.Helper()
				return types.Transactions{
					withBid(t, w, alice, 1),
					withTip(t, w, bob, 1),
				}
			},
			want: func(txs types.Transactions) []types.Receipts {
				return []types.Receipts{{
					receipt(txs[1]),
					receipt(txs[0]), // bids aren't enabled
				}}
			},
			wantBeneficiaryBalance: []uint64{0},
		},
		{
			name: "enabled",
			opts: enable,
			mempool: func(tb testing.TB, w *saetest.Wallet) types.Transactions {
				tb.Helper()
				return types.Transactions{
					withNothing(t, w, charlie),
					withTip(t, w, bob, 1),
					withBid(t, w, alice, 0),
				}
			},
			want: func(txs types.Transactions) []types.Receipts {
				return []types.Receipts{{
					receipt(txs[2], settlement(alice, 0)), // bids first even if zero
					receipt(txs[1]),                       // then tips
					receipt(txs[0]),                       // then the rest
				}}
			},
			wantBeneficiaryBalance: []uint64{0}, // Alice is stingy
		},
		{
			name: "highest_bid_first",
			opts: enable,
			mempool: func(tb testing.TB, w *saetest.Wallet) types.Transactions {
				tb.Helper()
				return types.Transactions{
					withBid(t, w, alice, 0),
					withBid(t, w, bob, 1),
					withBid(t, w, charlie, 42),
					withNothing(t, w, dave),
					withNothing(t, w, dave),
				}
			},
			want: func(txs types.Transactions) []types.Receipts {
				return []types.Receipts{
					{
						receipt(txs[2], settlement(charlie, 42)),
						// Dave is just minding his own business, doing Dave
						// things. What does he care about Bob and Charlie
						// trying to avoid being MEVd by Stephen?
						receipt(txs[3]),
						receipt(txs[4]),
					},
					{receipt(txs[1], settlement(bob, 1))},
					{receipt(txs[0], settlement(alice, 0))},
				}
			},
			wantBeneficiaryBalance: []uint64{42, 43, 43},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := append([]sutOption{balancePrecompile}, tt.opts...)
			ctx, sut := newSUT(t, numAccounts, opts...)
			sdb := sut.stateAt(t, sut.genesis.PostExecutionStateRoot())
			require.Equalf(t, uint256.NewInt(0), sdb.GetBalance(beneficiary), "Auction beneficiary (%v) at genesis", beneficiary)

			txs := tt.mempool(t, sut.wallet)
			sut.sendTxsAndWaitUntilPending(t, txs...)
			var (
				got                   []types.Receipts
				gotBeneficiaryBalance []uint64
			)
			for range len(txs) + 1 {
				b := sut.runConsensusLoop(t)
				require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)

				rs := b.Receipts()
				if len(rs) == 0 {
					break
				}
				got = append(got, rs)

				sdb := sut.stateAt(t, b.PostExecutionStateRoot())
				bal := sdb.GetBalance(beneficiary)
				if !bal.IsUint64() {
					t.Errorf("Beneficiary balance after block %d: %v > 64 bits", b.Height(), bal)
					bal.SetUint64(math.MaxUint64)
				}
				gotBeneficiaryBalance = append(gotBeneficiaryBalance, bal.Uint64())
			}

			want := tt.want(txs)
			for _, rs := range want {
				for ti, r := range rs {
					r.TransactionIndex = uint(ti)
				}
			}
			if diff := cmp.Diff(want, got, cmputils.Receipts(), ignore); diff != "" {
				t.Errorf("%T per block diff (-want +got):\n%s", types.Receipts{}, diff)
			}
			if diff := cmp.Diff(tt.wantBeneficiaryBalance, gotBeneficiaryBalance); diff != "" {
				t.Errorf("Beneficiary balance after each block diff (-want +got):\n%s", diff)
			}

			t.Run("rpc", func(t *testing.T) {
				// RPC implementations execute their own transactions based on
				// state built by [saexec] so we need to ensure that the
				// execution sees the auction settlement.
				wantBal := tt.wantBeneficiaryBalance[0]

				sut.testRPC(ctx, t, withCmpOpts(
					[]rpcTest{{
						method: "debug_traceTransaction",
						args:   []any{want[0][0].TxHash},
						want: logger.ExecutionResult{
							ReturnValue: fmt.Sprintf("%x", uint256.NewInt(wantBal).PaddedBytes(32)),
						},
					}},
					cmpopts.IgnoreFields(logger.ExecutionResult{}, "Gas"),
					cmputils.NilSlicesAreEmpty[[]logger.StructLogRes](),
				)...)
			})
		})
	}
}
