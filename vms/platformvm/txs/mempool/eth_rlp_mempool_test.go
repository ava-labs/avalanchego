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
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	ethcommon "github.com/ava-labs/libevm/common"
)

// newEthTx signs a transfer bidding feeCapWei wei per gas.
func newEthTx(t *testing.T, key *secp256k1.PrivateKey, nonce uint64, feeCapWei int64) *txs.Tx {
	t.Helper()
	return newEthTxWithFeeCap(t, key, nonce, big.NewInt(feeCapWei))
}

func newEthTxWithFeeCap(t *testing.T, key *secp256k1.PrivateKey, nonce uint64, feeCap *big.Int) *txs.Tx {
	t.Helper()

	to := ethcommon.Address(ids.GenerateTestShortID())
	chainID := txs.EthRLPChainID(testCtx(t).NetworkID)
	signed := ethtypes.MustSignNewTx(
		key.ToECDSA(),
		ethtypes.LatestSignerForChainID(chainID),
		&ethtypes.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     nonce,
			GasTipCap: big.NewInt(0),
			GasFeeCap: feeCap,
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

// ethTestGasPrice is the price the test mempool reports, high enough that eth
// bids below it are not clamped.
const ethTestGasPrice gas.Price = 1_000_000

func testCtx(t *testing.T) *snow.Context {
	t.Helper()
	return snowtest.Context(t, snowtest.PChainID)
}

// newEthMempool builds a mempool priced at gasPriceNAVAX nAVAX per gas, which
// is the ceiling an eth tx bid is capped to.
func newEthMempool(t *testing.T, gasCapacity gas.Gas, gasPriceNAVAX gas.Price) *Mempool {
	t.Helper()
	m, err := New(
		"",
		gas.Dimensions{gas.Bandwidth: 1, gas.DBRead: 1, gas.DBWrite: 1, gas.Compute: 1},
		gasCapacity,
		testCtx(t),
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)
	m.SetGasPrice(gasPriceNAVAX)
	return m
}

// Eth txs bid via their signed fee cap, so they order against declared-input
// txs rather than sorting at price 0.
func TestMempoolEthOrdering(t *testing.T) {
	require := require.New(t)
	m := newEthMempool(t, 1_000_000, ethTestGasPrice)

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

	cheapTx := newTxWithUTXOs(
		ids.GenerateTestID(),
		[]*avax.TransferableInput{newAVAXInput(ids.GenerateTestID(), 2)},
		1,
	)

	// Size the mempool so the cheap tx fits but adding an eth tx on top of it
	// does not, forcing the eviction decision.
	key0, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	probe := newEthTx(t, key0, 0, 1)
	weights := gas.Dimensions{gas.Bandwidth: 1, gas.DBRead: 1, gas.DBWrite: 1, gas.Compute: 1}
	cheapComplexity, err := fee.TxComplexity(cheapTx.Unsigned)
	require.NoError(err)
	cheapGas, err := cheapComplexity.ToGas(weights)
	require.NoError(err)
	ethComplexity, err := fee.TxComplexity(probe.Unsigned)
	require.NoError(err)
	ethGas, err := ethComplexity.ToGas(weights)
	require.NoError(err)

	m := newEthMempool(t, cheapGas+ethGas-1, ethTestGasPrice)
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
	m := newEthMempool(t, 100_000_000, ethTestGasPrice)

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

// An eth tx pays exactly gas times the current price, so bidding its raw fee
// cap would be free: an absurd cap would evict every paying tx in the mempool
// at no cost, and a cap near 2^256 would even overflow float64 ordering to
// +Inf. The bid is therefore clamped to the current price.
func TestMempoolEthBidIsCappedAtCurrentPrice(t *testing.T) {
	require := require.New(t)

	const price gas.Price = 5
	m := newEthMempool(t, 100_000_000, price)

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	// A tx bidding far above the price is metered at the price, not the bid.
	absurd := newEthTx(t, key, 0, 1_000_000_000_000)
	require.NoError(m.Add(absurd))
	require.Equal(float64(price), m.txs[absurd.ID()].gasPrice)

	// Even a cap that overflows float64 cannot produce an infinite bid.
	huge := newEthTxWithFeeCap(t, key, 1, new(big.Int).Lsh(big.NewInt(1), 255))
	require.NoError(m.Add(huge))
	require.Equal(float64(price), m.txs[huge.ID()].gasPrice)

	// A tx bidding below the price keeps its own lower bid.
	cheap := newEthTx(t, key, 2, 2_000_000_000) // 2 nAVAX per gas, price is 5
	require.NoError(m.Add(cheap))
	require.Equal(2.0, m.txs[cheap.ID()].gasPrice)

	// So the honest higher bidder still wins ordering.
	gotTx, ok := m.Peek()
	require.True(ok)
	require.NotEqual(cheap.ID(), gotTx.ID())
}

// Credentials on an eth tx are unbounded and unpriced, so admission must reject
// them: a padded tx would otherwise be gossiped and packed into a block that no
// proposer can serialize.
func TestMempoolEthRejectsCredentials(t *testing.T) {
	require := require.New(t)
	m := newEthMempool(t, 100_000_000, ethTestGasPrice)

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	honest := newEthTx(t, key, 0, 1e9)

	padded := &txs.Tx{
		Unsigned: &txs.EthRLPTx{RLP: honest.Unsigned.(*txs.EthRLPTx).RLP},
		Creds: []verify.Verifiable{
			&secp256k1fx.Credential{Sigs: make([][secp256k1.SignatureLen]byte, 3000)},
		},
	}
	require.NoError(padded.Initialize(txs.Codec))

	require.ErrorIs(m.Add(padded), ErrEthCredentials)
	require.Zero(m.tree.Len())
}
