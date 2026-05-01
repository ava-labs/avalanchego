// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx_test

import (
	"encoding/json"
	"math"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/cmputils"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"

	. "github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
)

// fuzz seeds f with [NewTxs], specifies simple alphabets used to bias the
// fuzzer, and fuzzes the test.
func fuzz(f *testing.F, ff func(t *testing.T, tx *Tx)) {
	fuzzer := &txtest.F{
		F: f,
		Addresses: []common.Address{
			{1},
		},
		AssetIDs: []ids.ID{
			AVAXAssetID,
		},
	}
	for _, tx := range NewTxs {
		fuzzer.Add(tx)
	}
	fuzzer.Fuzz(ff)
}

func FuzzParseRoundTrip(f *testing.F) {
	fuzz(f, func(t *testing.T, want *Tx) {
		bytes, err := want.Bytes()
		require.NoErrorf(t, err, "%T.Bytes()", want)

		got, err := Parse(bytes)
		require.NoError(t, err, "Parse()")
		if diff := cmp.Diff(want, got, CmpOpt()); diff != "" {
			t.Errorf("Parse() diff (-want +got):\n%s", diff)
		}
	})
}

func FuzzJSONCompatibility(f *testing.F) {
	fuzz(f, func(t *testing.T, newTx *Tx) {
		oldTx := ToOldTx(t, newTx)
		want, err := json.Marshal(oldTx)
		require.NoErrorf(t, err, "json.Marshal(%T)", oldTx)

		got, err := json.Marshal(newTx)
		require.NoErrorf(t, err, "json.Marshal(%T)", newTx)
		assert.JSONEq(t, string(want), string(got))
	})
}

func FuzzAsOpCompatibility(f *testing.F) {
	fuzz(f, func(t *testing.T, newTx *Tx) {
		got, err := newTx.AsOp(AVAXAssetID)
		if err != nil {
			t.Skip("invalid tx")
		}

		oldTx := ToOldTx(t, newTx)
		gasUsed, err := oldTx.UnsignedAtomicTx.GasUsed(true)
		require.NoErrorf(t, err, "%T.GasUsed(true)", oldTx.UnsignedAtomicTx)

		gasPrice, err := atomic.EffectiveGasPrice(oldTx.UnsignedAtomicTx, AVAXAssetID, true)
		require.NoErrorf(t, err, "atomic.EffectiveGasPrice(%T, avaxAssetID, true)", oldTx)

		state := newAsOpStateDB()
		if export, ok := oldTx.UnsignedAtomicTx.(*atomic.UnsignedExportTx); ok {
			for _, in := range export.Ins {
				state.initialNonces[in.Address] = in.Nonce
			}
		}

		ctx := &snow.Context{AVAXAssetID: AVAXAssetID}
		require.NoErrorf(t, oldTx.UnsignedAtomicTx.EVMStateTransfer(ctx, state), "%T.EVMStateTransfer()", oldTx.UnsignedAtomicTx)

		want := hook.Op{
			ID:        oldTx.ID(),
			Gas:       gas.Gas(gasUsed),
			GasFeeCap: gasPrice,
			Burn:      state.op.Burn,
			Mint:      state.op.Mint,
		}
		if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("%T.AsOp() diff (-want +got):\n%s", newTx, diff)
		}
	})
}

// asOpStateDB is an in-memory [atomic.StateDB] for [FuzzAsOpCompatibility]. It
// constructs a [hook.Op] from [atomic.UnsignedAtomicTx.EVMStateTransfer].
type asOpStateDB struct {
	initialNonces map[common.Address]uint64
	op            hook.Op
}

func newAsOpStateDB() *asOpStateDB {
	return &asOpStateDB{
		initialNonces: make(map[common.Address]uint64),
		op: hook.Op{
			Burn: make(map[common.Address]hook.AccountDebit),
			Mint: make(map[common.Address]uint256.Int),
		},
	}
}

func (s *asOpStateDB) AddBalance(addr common.Address, amount *uint256.Int) {
	b := s.op.Mint[addr]
	b.Add(&b, amount)
	s.op.Mint[addr] = b
}

func (s *asOpStateDB) SubBalance(addr common.Address, amount *uint256.Int) {
	d := s.op.Burn[addr]
	d.Amount.Add(&d.Amount, amount)
	d.MinBalance = d.Amount
	s.op.Burn[addr] = d
}

func (*asOpStateDB) GetBalance(common.Address) *uint256.Int {
	return largeUint256()
}

func (*asOpStateDB) AddBalanceMultiCoin(common.Address, common.Hash, *big.Int) {}

func (*asOpStateDB) SubBalanceMultiCoin(common.Address, common.Hash, *big.Int) {}

func (*asOpStateDB) GetBalanceMultiCoin(common.Address, common.Hash) *big.Int {
	return largeBigInt()
}

func (s *asOpStateDB) SetNonce(addr common.Address, nonce uint64) {
	d := s.op.Burn[addr]
	// The op specifies what nonce is being consumed, not the next nonce. So we
	// need to subtract 1.
	d.Nonce = nonce - 1
	s.op.Burn[addr] = d
}

func (s *asOpStateDB) GetNonce(addr common.Address) uint64 {
	return s.initialNonces[addr]
}

func FuzzAtomicRequestsCompatibility(f *testing.F) {
	fuzz(f, func(t *testing.T, newTx *Tx) {
		oldTx := ToOldTx(t, newTx)
		wantChainID, wantRequests, err := oldTx.UnsignedAtomicTx.AtomicOps()
		require.NoErrorf(t, err, "%T.AtomicOps()", oldTx.UnsignedAtomicTx)

		gotChainID, gotRequests, err := newTx.AtomicRequests()
		require.NoErrorf(t, err, "%T.AtomicRequests()", newTx)
		assert.Equal(t, wantChainID, gotChainID, "chainID")
		assert.Equal(t, wantRequests, gotRequests, "requests")
	})
}

func FuzzTransferNonAVAXCompatibility(f *testing.F) {
	fuzz(f, func(t *testing.T, newTx *Tx) {
		op, err := newTx.AsOp(AVAXAssetID)
		if err != nil {
			t.Skip("invalid tx")
		}

		oldSDB := NewStateDB(t)
		newSDB := NewStateDB(t)

		if tx, ok := newTx.Unsigned.(*Export); ok {
			for _, in := range tx.Ins {
				if in.Nonce == math.MaxUint64 {
					t.Skip("nonce overflow")
				}

				for _, sdb := range []*extstate.StateDB{oldSDB, newSDB} {
					sdb.AddBalance(in.Address, largeUint256())
					sdb.SetNonce(in.Address, in.Nonce)
					sdb.AddBalanceMultiCoin(in.Address, common.Hash(in.AssetID), largeBigInt())
				}
			}
		}

		var (
			oldTx = ToOldTx(t, newTx)
			ctx   = &snow.Context{AVAXAssetID: AVAXAssetID}
		)
		require.NoError(t, oldTx.UnsignedAtomicTx.EVMStateTransfer(ctx, oldSDB))
		require.NoError(t, newTx.TransferNonAVAX(AVAXAssetID, newSDB))
		require.NoError(t, op.ApplyTo(newSDB.StateDB))

		// We must manually finalize the trie structures before comparison.
		// Otherwise, comparing the state DBs wouldn't include the changes.
		for _, sdb := range []*extstate.StateDB{oldSDB, newSDB} {
			sdb.Finalise(true)
			sdb.IntermediateRoot(true)
		}

		opts := []cmp.Option{
			cmpopts.IgnoreUnexported(extstate.StateDB{}),
			cmputils.StateDBs(),
		}
		if diff := cmp.Diff(oldSDB, newSDB, opts...); diff != "" {
			t.Errorf("%T.TransferNonAVAX() diff (-want +got):\n%s", newTx, diff)
		}
	})
}

// largeUint256 and largeBigInt return a balance large enough to never underflow
// but small enough to never overflow during test arithmetic.
func largeUint256() *uint256.Int { return new(uint256.Int).Lsh(uint256.NewInt(1), 128) }
func largeBigInt() *big.Int      { return new(big.Int).Lsh(big.NewInt(1), 128) }
