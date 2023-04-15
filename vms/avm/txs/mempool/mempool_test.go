// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	keys    = secp256k1.TestKeys()
	chainID = ids.ID{5, 4, 3, 2, 1}
	assetID = ids.ID{1, 2, 3}
)

// shows that valid tx is not added to mempool if this would exceed its maximum
// size
func TestBlockBuilderMaxMempoolSizeHandling(t *testing.T) {
	require := require.New(t)

	registerer := prometheus.NewRegistry()
	mempoolIntf, err := New("mempool", registerer, nil)
	require.NoError(err)

	mempool := mempoolIntf.(*mempool)

	testTxs := createTestTxs(2)
	tx := testTxs[0]

	// shortcut to simulated almost filled mempool
	mempool.bytesAvailable = len(tx.Bytes()) - 1

	err = mempool.Add(tx)
	require.ErrorIs(err, errMempoolFull)

	// shortcut to simulated almost filled mempool
	mempool.bytesAvailable = len(tx.Bytes())

	err = mempool.Add(tx)
	require.NoError(err)
}

func TestTxsInMempool(t *testing.T) {
	require := require.New(t)

	registerer := prometheus.NewRegistry()
	toEngine := make(chan common.Message, 100)
	mempool, err := New("mempool", registerer, toEngine)
	require.NoError(err)

	testTxs := createTestTxs(2)

	mempool.RequestBuildBlock()
	select {
	case <-toEngine:
		t.Fatalf("should not have sent message to engine")
	default:
	}

	for _, tx := range testTxs {
		txID := tx.ID()
		// tx not already there
		require.False(mempool.Has(txID))

		// we can insert
		require.NoError(mempool.Add(tx))

		// we can get it
		require.True(mempool.Has(txID))

		retrieved := mempool.Get(txID)
		require.True(retrieved != nil)
		require.Equal(tx, retrieved)

		// tx exists in mempool
		require.True(mempool.Has(txID))

		// once removed it cannot be there
		mempool.Remove([]*txs.Tx{tx})

		require.False(mempool.Has(txID))
		require.Nil(mempool.Get(txID))

		// we can reinsert it again to grow the mempool
		require.NoError(mempool.Add(tx))
	}

	mempool.RequestBuildBlock()
	select {
	case <-toEngine:
	default:
		t.Fatalf("should have sent message to engine")
	}

	mempool.Remove(testTxs)

	mempool.RequestBuildBlock()
	select {
	case <-toEngine:
		t.Fatalf("should not have sent message to engine")
	default:
	}
}

func createTestTxs(count int) []*txs.Tx {
	testTxs := make([]*txs.Tx, 0, count)
	addr := keys[0].PublicKey().Address()
	for i := uint32(0); i < uint32(count); i++ {
		tx := &txs.Tx{Unsigned: &txs.CreateAssetTx{
			BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    constants.UnitTestID,
				BlockchainID: chainID,
				Ins: []*avax.TransferableInput{{
					UTXOID: avax.UTXOID{
						TxID:        ids.ID{'t', 'x', 'I', 'D'},
						OutputIndex: i,
					},
					Asset: avax.Asset{ID: assetID},
					In: &secp256k1fx.TransferInput{
						Amt: 54321,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{i},
						},
					},
				}},
				Outs: []*avax.TransferableOutput{{
					Asset: avax.Asset{ID: assetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 12345,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addr},
						},
					},
				}},
			}},
			Name:         "NormalName",
			Symbol:       "TICK",
			Denomination: byte(2),
			States: []*txs.InitialState{
				{
					FxIndex: 0,
					Outs: []verify.State{
						&secp256k1fx.TransferOutput{
							Amt: 12345,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{addr},
							},
						},
					},
				},
			},
		}}
		tx.SetBytes(utils.RandomBytes(16), utils.RandomBytes(16))
		testTxs = append(testTxs, tx)
	}
	return testTxs
}
