// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txpool

import (
	"context"
	"math/big"
	"slices"
	"testing"
	"testing/synctest"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/event"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/params"
	"github.com/google/go-cmp/cmp"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/cchaintest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestMain(m *testing.M) {
	evm.RegisterAllLibEVMExtras()
	goleak.VerifyTestMain(m, goleak.IgnoreCurrent())
}

// assertEquals asserts that [Pending.Len], [Pending.Has], [Pending.AwaitTxs],
// and [Pending.Iter] all match the expected transactions.
func (p *Pending) assertEquals(ctx context.Context, tb testing.TB, want ...*tx.Tx) {
	tb.Helper()

	assert.Equal(tb, len(want), p.Len(), "Len")
	for _, w := range want {
		assert.Truef(tb, p.Has(w.ID()), "%T.Has(%s)", p, w.ID())
	}

	if len(want) > 0 {
		require.NoError(tb, p.AwaitTxs(ctx), "%T.AwaitTxs()", p)
	} else {
		ctx, cancel := context.WithCancel(ctx)
		go cancel()
		err := p.AwaitTxs(ctx)
		require.ErrorIs(tb, err, context.Canceled, "%T.AwaitTxs()", p)
	}

	got := slices.Collect(p.Iter())
	if diff := cmp.Diff(want, got, txtest.CmpOpt()); diff != "" {
		tb.Errorf("%T.Iter() diff (-want +got):\n%s", p, diff)
	}
}

// newState returns a [state.StateDB] where the provided keys have the maximum
// balance.
func newState(tb testing.TB, keys ...*secp256k1.PrivateKey) *state.StateDB {
	tb.Helper()

	db := state.NewDatabase(rawdb.NewMemoryDatabase())
	sdb, err := state.New(types.EmptyRootHash, db, nil)
	require.NoError(tb, err, "state.New()")
	for _, sk := range keys {
		maxUint256 := new(uint256.Int).SetAllOne()
		sdb.SetBalance(sk.EthAddress(), maxUint256)
	}
	return sdb
}

// backend is an in-memory [Backend] for tests.
type backend struct {
	state  utils.Atomic[libevm.StateReader]
	events event.FeedOf[core.ChainHeadEvent]
}

// newBackend returns a new [backend] with the provided last executed state.
func newBackend(state libevm.StateReader) *backend {
	b := &backend{}
	b.state.Set(state)
	return b
}

func (b *backend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return b.events.Subscribe(ch)
}

func (b *backend) LastExecutedState() (libevm.StateReader, error) {
	return b.state.Get(), nil
}

// markAsExecuted notifies the pool of the newly executed block and its
// resulting state.
func (b *backend) markAsExecuted(block *types.Block, state libevm.StateReader) {
	// Setting the state before sending the event guarantees that when the pool
	// processes the event, the new state is available.
	b.state.Set(state)
	b.events.Send(core.ChainHeadEvent{Block: block})
}

// SUT is the system under test. It embeds a [Txpool] and provides helper
// methods to manipulate the pool's backend.
type SUT struct {
	*Txpool
	backend *backend
}

// maxSize is the [Txpool] capacity tests run against. Small enough that
// pool-full eviction tests can fill it cheaply, large enough to fit every
// non-pool-full assertion in this file.
const maxSize = 4

// newSUT constructs a [Txpool] backed by a [backend]. The pool is closed via
// [testing.TB.Cleanup].
func newSUT(tb testing.TB, state libevm.StateReader) (context.Context, *SUT) {
	tb.Helper()

	backend := newBackend(state)
	snowCtx := snowtest.Context(tb, snowtest.CChainID)
	log := loggingtest.New(tb, logging.Debug)
	snowCtx.Log = log
	pool, err := New(
		snowCtx,
		saetest.ChainConfig(),
		NewPending(),
		backend,
		maxSize,
	)
	require.NoError(tb, err)
	tb.Cleanup(pool.Close)
	return log.CancelOnError(tb.Context()), &SUT{
		Txpool:  pool,
		backend: backend,
	}
}

// markAsExecuted notifies the pool of the newly executed block and its
// resulting state.
//
// This will evict any transactions contained in block from the pool and updates
// the state that the pool verifies on to state.
//
// This function blocks until the pool has been updated.
func (s *SUT) markAsExecuted(tb testing.TB, block *types.Block, state libevm.StateReader) {
	tb.Helper()

	s.backend.markAsExecuted(block, state)

	// Sending a second event guarantees that this function returns after the
	// pool's goroutine has processed the first event. We pass an empty block to
	// avoid removing more conflicts after this function returns.
	s.backend.markAsExecuted(cchaintest.NewTestBlock(tb), state)
}

// An exportOption configures the default export properties created by
// [newExport].
type exportOption = options.Option[exportProperties]

type exportProperties struct {
	networkID uint32
	amount    uint64
	addr      ids.ShortID
	creds     []tx.Credential
}

func withNetworkID(networkID uint32) exportOption {
	return options.Func[exportProperties](func(p *exportProperties) {
		p.networkID = networkID
	})
}

func withAmount(amount uint64) exportOption {
	return options.Func[exportProperties](func(p *exportProperties) {
		p.amount = amount
	})
}

func withAddress(addr ids.ShortID) exportOption {
	return options.Func[exportProperties](func(p *exportProperties) {
		p.addr = addr
	})
}

func withCredentials(creds []tx.Credential) exportOption {
	return options.Func[exportProperties](func(p *exportProperties) {
		p.creds = creds
	})
}

// newExport returns an export that consumes one input (nonce 0) per key, each
// with the configured amount of AVAX.
func newExport(tb testing.TB, sks []*secp256k1.PrivateKey, opts ...exportOption) *tx.Tx {
	tb.Helper()

	props := options.ApplyTo(&exportProperties{
		networkID: constants.UnitTestID,
		amount:    100,
	}, opts...)

	// [tx.Export.sanityCheck] requires inputs to be sorted by address.
	sorted := slices.Clone(sks)
	slices.SortFunc(sorted, func(a, b *secp256k1.PrivateKey) int {
		return a.EthAddress().Cmp(b.EthAddress())
	})

	ins := make([]tx.Input, len(sorted))
	for i, sk := range sorted {
		ins[i] = tx.Input{
			Address: sk.EthAddress(),
			Amount:  props.amount,
			AssetID: snowtest.AVAXAssetID,
		}
	}
	e := &tx.Export{
		NetworkID:        props.networkID,
		BlockchainID:     snowtest.CChainID,
		DestinationChain: snowtest.XChainID,
		Ins:              ins,
		ExportedOutputs: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: snowtest.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 1,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{props.addr},
				},
			},
		}},
	}

	creds := make([]tx.Credential, len(sorted))
	for i, sk := range sorted {
		creds[i] = &secp256k1fx.Credential{Sigs: []txtest.Signature{
			txtest.Sign(tb, e, sk),
		}}
	}

	props.creds = creds
	props = options.ApplyTo(props, opts...)
	return &tx.Tx{
		Unsigned: e,
		Creds:    props.creds,
	}
}

func newKey(tb testing.TB) *secp256k1.PrivateKey {
	tb.Helper()

	sk, err := secp256k1.NewPrivateKey()
	require.NoError(tb, err, "secp256k1.NewPrivateKey()")
	return sk
}

// newEth returns an Ethereum transaction with nonce 0 signed by key.
func newEth(tb testing.TB, key *secp256k1.PrivateKey) *types.Transaction {
	tb.Helper()

	signer := types.MakeSigner(saetest.ChainConfig(), big.NewInt(1), 0)
	tx, err := types.SignNewTx(key.ToECDSA(), signer, &types.LegacyTx{
		Gas: params.TxGas,
		To:  &common.Address{},
	})
	require.NoError(tb, err)
	return tx
}

func TestAdd(t *testing.T) {
	const (
		nonce   = 0
		highFee = 100
		midFee  = 75
		lowFee  = 50
	)
	var (
		alice   = newKey(t)
		bob     = newKey(t)
		charles = newKey(t)

		aliceHigh          = newExport(t, []*secp256k1.PrivateKey{alice}, withAmount(highFee))
		aliceHighConflict  = newExport(t, []*secp256k1.PrivateKey{alice}, withAmount(highFee), withAddress(ids.GenerateTestShortID()))
		aliceAndBobMidHigh = newExport(t, []*secp256k1.PrivateKey{alice, bob}, withAmount(lowFee)) // gas price is between mid and high
		bobMid             = newExport(t, []*secp256k1.PrivateKey{bob}, withAmount(midFee))
		aliceLow           = newExport(t, []*secp256k1.PrivateKey{alice}, withAmount(lowFee))
		charlesLow         = newExport(t, []*secp256k1.PrivateKey{charles}, withAmount(lowFee))

		// Fails [tx.Tx.SanityCheck].
		wrongNetwork = newExport(t, []*secp256k1.PrivateKey{alice}, withNetworkID(constants.UnitTestID+1))
		// Fails [tx.Tx.AsOp]. Calculating the number of signatures for the
		// gas price errors because [avax.TestTransferable] is not a
		// [*secp256k1fx.TransferInput].
		unmarshalable = &tx.Tx{
			Unsigned: &tx.Import{
				NetworkID:    constants.UnitTestID,
				BlockchainID: snowtest.CChainID,
				SourceChain:  snowtest.XChainID,
				ImportedInputs: []*avax.TransferableInput{{
					Asset: avax.Asset{ID: snowtest.AVAXAssetID},
					In:    &avax.TestTransferable{Val: 1},
				}},
				Outs: []tx.Output{{
					Amount:  1,
					AssetID: snowtest.AVAXAssetID,
				}},
			},
		}
		// Fails [tx.Tx.VerifyCredentials].
		missingCreds = newExport(t, []*secp256k1.PrivateKey{alice}, withCredentials(nil))
	)

	allKeys := []*secp256k1.PrivateKey{alice, bob, charles}
	maxSizeTxs := make([]*tx.Tx, maxSize)
	for i := range maxSizeTxs {
		sk := newKey(t)
		allKeys = append(allKeys, sk)
		amount := lowFee + uint64(i) //#nosec G115 -- Won't overflow
		maxSizeTxs[i] = newExport(t, []*secp256k1.PrivateKey{sk}, withAmount(amount))
	}
	slices.Reverse(maxSizeTxs) // Sorted by descending fee.

	tests := []struct {
		name       string
		initNonces map[common.Address]uint64
		init       []*tx.Tx
		toAdd      *tx.Tx
		want       []*tx.Tx
		wantErr    error
	}{
		{
			name:    "sanity_check_failure",
			toAdd:   wrongNetwork,
			wantErr: errSanityCheck,
		},
		{
			name:    "as_op_failure",
			toAdd:   unmarshalable,
			wantErr: errAsOp,
		},
		{
			name:    "verify_credentials_failure",
			toAdd:   missingCreds,
			wantErr: errVerifyCredentials,
		},
		{
			name: "verify_state_failure",
			initNonces: map[common.Address]uint64{
				alice.EthAddress(): nonce + 1,
			},
			toAdd:   aliceHigh,
			wantErr: errVerifyState,
		},
		{
			name:  "empty_pool",
			toAdd: aliceHigh,
			want:  []*tx.Tx{aliceHigh},
		},
		{
			name:  "higher_fee_evicts_conflict",
			init:  []*tx.Tx{aliceLow},
			toAdd: aliceHigh,
			want:  []*tx.Tx{aliceHigh},
		},
		{
			name:    "lower_fee_rejected_by_conflict",
			init:    []*tx.Tx{aliceHigh},
			toAdd:   aliceLow,
			wantErr: errInsufficientFee,
			want:    []*tx.Tx{aliceHigh},
		},
		{
			name:    "equal_fee_rejected_by_conflict",
			init:    []*tx.Tx{aliceHigh},
			toAdd:   aliceHighConflict,
			wantErr: errInsufficientFee,
			want:    []*tx.Tx{aliceHigh},
		},
		{
			name:    "already_known",
			init:    []*tx.Tx{aliceHigh},
			toAdd:   aliceHigh,
			wantErr: ErrAlreadyKnown,
			want:    []*tx.Tx{aliceHigh},
		},
		{
			name:  "ordered_by_gas_price_descending",
			init:  []*tx.Tx{bobMid, charlesLow},
			toAdd: aliceHigh,
			want:  []*tx.Tx{aliceHigh, bobMid, charlesLow},
		},
		{
			name:  "higher_fee_evicts_multiple_conflicts",
			init:  []*tx.Tx{aliceLow, bobMid},
			toAdd: aliceAndBobMidHigh,
			want:  []*tx.Tx{aliceAndBobMidHigh},
		},
		{
			name:    "insufficient_fee_against_one_conflict_keeps_all",
			init:    []*tx.Tx{aliceHigh, bobMid},
			toAdd:   aliceAndBobMidHigh,
			wantErr: errInsufficientFee,
			want:    []*tx.Tx{aliceHigh, bobMid},
		},
		{
			name:    "pool_full_rejects_same_fee_as_cheapest",
			init:    maxSizeTxs,
			toAdd:   aliceLow,
			wantErr: errInsufficientFee,
			want:    maxSizeTxs,
		},
		{
			name:  "pool_full_evicts_cheapest_for_higher_fee",
			init:  maxSizeTxs,
			toAdd: aliceHigh,
			want:  append([]*tx.Tx{aliceHigh}, maxSizeTxs[:maxSize-1]...),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sdb := newState(t, allKeys...)
			for addr, nonce := range tt.initNonces {
				sdb.SetNonce(addr, nonce)
			}

			ctx, sut := newSUT(t, sdb)
			for i, raw := range tt.init {
				require.NoErrorf(t, sut.Add(raw), "%T.Add([%d])", sut, i)
			}

			err := sut.Add(tt.toAdd)
			require.ErrorIsf(t, err, tt.wantErr, "%T.Add(%T)", sut, tt.toAdd)
			sut.assertEquals(ctx, t, tt.want...)
		})
	}
}

func TestUpdateEvictsConflicts(t *testing.T) {
	var (
		sk     = newKey(t)
		initTx = newExport(t, []*secp256k1.PrivateKey{sk})
	)
	tests := []struct {
		name     string
		block    *types.Block
		wantPool []*tx.Tx
	}{
		{
			name: "no_conflicts_leave_pool_unchanged",
			block: cchaintest.NewTestBlock(
				t,
				cchaintest.WithEthTxs(newEth(t, newKey(t))),
				cchaintest.WithCrossChainTxs(newExport(t, []*secp256k1.PrivateKey{newKey(t)})),
			),
			wantPool: []*tx.Tx{initTx},
		},
		{
			name:  "eth_tx_conflict_evicts_tx",
			block: cchaintest.NewTestBlock(t, cchaintest.WithEthTxs(newEth(t, sk))),
		},
		{
			name:  "avax_tx_conflict_evicts_tx",
			block: cchaintest.NewTestBlock(t, cchaintest.WithCrossChainTxs(newExport(t, []*secp256k1.PrivateKey{sk}))),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sdb := newState(t, sk)
			ctx, sut := newSUT(t, sdb)

			require.NoErrorf(t, sut.Add(initTx), "%T.Add(%T)", sut, initTx)
			sut.assertEquals(ctx, t, initTx)

			sut.markAsExecuted(t, tt.block, sdb)
			sut.assertEquals(ctx, t, tt.wantPool...)
		})
	}
}

func TestStateUpdate(t *testing.T) {
	noBalance := newState(t)
	ctx, sut := newSUT(t, noBalance)

	sk := newKey(t)
	tx := newExport(t, []*secp256k1.PrivateKey{sk})
	require.ErrorIsf(t, sut.Add(tx), errVerifyState, "%T.Add()", sut)
	sut.assertEquals(ctx, t)

	hasBalance := newState(t, sk)
	sut.markAsExecuted(t, cchaintest.NewTestBlock(t), hasBalance)
	require.NoErrorf(t, sut.Add(tx), "%T.Add()", sut)
	sut.assertEquals(ctx, t, tx)
}

func TestHasUnknown(t *testing.T) {
	sk := newKey(t)
	_, sut := newSUT(t, newState(t, sk))
	require.Falsef(t, sut.Has(ids.GenerateTestID()), "%T.Has()", sut)

	raw := newExport(t, []*secp256k1.PrivateKey{sk})
	require.NoErrorf(t, sut.Add(raw), "%T.Add(%T)", sut, raw)
	require.Falsef(t, sut.Has(ids.GenerateTestID()), "%T.Has()", sut)
}

func TestAwaitTxs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sk := newKey(t)
		ctx, sut := newSUT(t, newState(t, sk))

		done := make(chan error, 1)
		go func() {
			done <- sut.AwaitTxs(ctx)
		}()

		synctest.Wait()
		require.Emptyf(t, done, "%T.AwaitTxs() should be blocked", sut)

		require.NoErrorf(t, sut.Add(newExport(t, []*secp256k1.PrivateKey{sk})), "%T.Add()", sut)
		require.NoErrorf(t, <-done, "%T.AwaitTxs()", sut)
	})
}

func TestVerifyOp(t *testing.T) {
	const (
		nonce   = 3
		balance = 50
	)
	addr := common.Address{}
	sdb := newState(t)
	sdb.SetNonce(addr, nonce)
	sdb.SetBalance(addr, uint256.NewInt(balance))

	tests := []struct {
		name   string
		nonce  uint64
		amount uint64
		want   error
	}{
		{
			name:   "valid_full_transfer",
			nonce:  nonce,
			amount: balance,
		},
		{
			name:   "valid_partial_transfer",
			nonce:  nonce,
			amount: balance - 1,
		},
		{
			name:   "nonce_too_low",
			nonce:  nonce - 1,
			amount: balance,
			want:   errNonceMismatch,
		},
		{
			name:   "nonce_too_high",
			nonce:  nonce + 1,
			amount: balance,
			want:   errNonceMismatch,
		},
		{
			name:   "insufficient_funds",
			nonce:  nonce,
			amount: balance + 1,
			want:   errInsufficientFunds,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := hook.Op{
				Burn: map[common.Address]hook.AccountDebit{
					addr: {
						Nonce:      tt.nonce,
						Amount:     *uint256.NewInt(tt.amount),
						MinBalance: *uint256.NewInt(tt.amount),
					},
				},
			}
			err := verifyOp(sdb, op)
			require.ErrorIsf(t, err, tt.want, "verifyOp(%T, %T)", sdb, op)
		})
	}
}
