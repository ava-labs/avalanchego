// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"math/big"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	ethtypes "github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"

	ethcommon "github.com/ava-labs/libevm/common"
)

// newEthTx signs a transfer bidding feeCapWei wei per gas.
func newEthTx(t *testing.T, key *secp256k1.PrivateKey, nonce uint64, feeCapWei int64) *txs.Tx {
	t.Helper()

	to := ethcommon.Address(ids.GenerateTestShortID())
	chainID := big.NewInt(txs.EthRLPChainID)
	signed := ethtypes.MustSignNewTx(
		key.ToECDSA(),
		ethtypes.LatestSignerForChainID(chainID),
		&ethtypes.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     nonce,
			GasTipCap: big.NewInt(0),
			GasFeeCap: big.NewInt(feeCapWei),
			Gas:       1_000_000,
			To:        &to,
			Value:     big.NewInt(1e18),
		},
	)
	raw, err := signed.MarshalBinary()
	require.NoError(t, err)

	tx, err := txs.NewSigned(&txs.EthRLPTx{RLP: raw}, txs.Codec, nil)
	require.NoError(t, err)
	return tx
}

func newEthMempool(t *testing.T, gasCapacity gas.Gas) *Mempool {
	t.Helper()
	m, err := New(
		"",
		gas.Dimensions{gas.Bandwidth: 1, gas.DBRead: 1, gas.DBWrite: 1, gas.Compute: 1},
		gasCapacity,
		snowtest.AVAXAssetID,
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)
	return m
}

// Eth txs bid via their signed fee cap, so they order against declared-input
// txs rather than sorting at price 0.
func TestMempoolEthOrdering(t *testing.T) {
	require := require.New(t)
	m := newEthMempool(t, 1_000_000)

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	// Declared-input tx burning 4 of 5 nAVAX over its gas.
	declaredTx := newTxWithUTXOs(
		ids.GenerateTestID(),
		[]*avax.TransferableInput{newAVAXInput(ids.GenerateTestID(), 5)},
		1,
	)
	require.NoError(m.Add(declaredTx))
	declaredPrice := m.txs[declaredTx.ID()].gasPrice
	require.Positive(declaredPrice)

	// Straddle the declared tx's price with eth bids expressed in wei per gas.
	declaredWei := int64(declaredPrice * 1e9)
	require.Positive(declaredWei)

	highEth := newEthTx(t, key, 0, declaredWei*2)
	require.NoError(m.Add(highEth))
	require.Greater(m.txs[highEth.ID()].gasPrice, declaredPrice)
	gotTx, ok := m.Peek()
	require.True(ok)
	require.Equal(highEth.ID(), gotTx.ID())

	// An eth tx bidding below it loses Peek, and nothing is evicted.
	lowKey, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	lowEth := newEthTx(t, lowKey, 0, declaredWei/2)
	require.NoError(m.Add(lowEth))
	require.Less(m.txs[lowEth.ID()].gasPrice, declaredPrice)
	require.Equal(3, m.tree.Len())

	gotTx, ok = m.Peek()
	require.True(ok)
	require.Equal(highEth.ID(), gotTx.ID())
}

// A higher-bidding eth tx evicts a lower-priced declared-input tx under
// capacity pressure, and a lower-bidding one does not.
func TestMempoolEthEviction(t *testing.T) {
	require := require.New(t)

	// Capacity fits exactly one tx of either kind.
	m := newEthMempool(t, 600)

	cheapTx := newTxWithUTXOs(
		ids.GenerateTestID(),
		[]*avax.TransferableInput{newAVAXInput(ids.GenerateTestID(), 2)},
		1,
	)
	require.NoError(m.Add(cheapTx))
	cheapWei := int64(m.txs[cheapTx.ID()].gasPrice * 1e9)
	require.Positive(cheapWei)

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	// Underbidding eth tx cannot make room.
	poorEth := newEthTx(t, key, 0, cheapWei/2)
	require.ErrorIs(m.Add(poorEth), ErrNotEnoughGas)
	_, ok := m.Get(cheapTx.ID())
	require.True(ok)

	// Overbidding eth tx evicts the cheap tx and takes the slot.
	richEth := newEthTx(t, key, 1, cheapWei*2)
	require.NoError(m.Add(richEth))
	_, ok = m.Get(cheapTx.ID())
	require.False(ok)
	_, ok = m.Get(richEth.ID())
	require.True(ok)
}

// The per-sender cap bounds pending eth txs, admits same-nonce replacements
// below the cap (the wallet cancel path), and frees slots on removal.
func TestMempoolEthPendingCap(t *testing.T) {
	require := require.New(t)
	m := newEthMempool(t, 100_000_000)

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	// Two same-nonce txs from one sender coexist: both are executable under
	// the strictly-greater nonce rule.
	first := newEthTx(t, key, 0, 1e9)
	second := newEthTx(t, key, 0, 2e9)
	require.NoError(m.Add(first))
	require.NoError(m.Add(second))
	require.Equal(2, m.tree.Len())

	for i := 2; i < maxEthPendingPerSender; i++ {
		require.NoError(m.Add(newEthTx(t, key, uint64(i), 1e9)))
	}
	require.ErrorIs(m.Add(newEthTx(t, key, 99, 1e9)), ErrTooManyEthPending)

	// A different sender is unaffected.
	otherKey, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	require.NoError(m.Add(newEthTx(t, otherKey, 0, 1e9)))

	// Removing one frees a slot for the capped sender.
	m.Remove(first.ID())
	require.NoError(m.Add(newEthTx(t, key, 100, 1e9)))
}
