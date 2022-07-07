// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/validator"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

var _ BlockTimer = &dummyBlkTimer{}

type dummyBlkTimer struct{}

func (bt *dummyBlkTimer) ResetBlockTimer() {}

var preFundedKeys = crypto.BuildTestKeys()

// shows that valid tx is not added to mempool if this would exceed its maximum
// size
func TestBlockBuilderMaxMempoolSizeHandling(t *testing.T) {
	assert := assert.New(t)

	registerer := prometheus.NewRegistry()
	mpool, err := NewMempool("mempool", registerer, &dummyBlkTimer{})
	assert.NoError(err)

	txes, err := createTestDecisionTxes(1)
	assert.NoError(err)
	tx := txes[0]

	// shortcut to simulated almost filled mempool
	mpool.(*mempool).bytesAvailable = len(tx.Bytes()) - 1

	err = mpool.Add(tx)
	assert.True(errors.Is(err, ErrMempoolFull), err, "max mempool size breached")

	// shortcut to simulated almost filled mempool
	mpool.(*mempool).bytesAvailable = len(tx.Bytes())

	err = mpool.Add(tx)
	assert.NoError(err, "should have added tx to mempool")
}

func TestDecisionTxsInMempool(t *testing.T) {
	assert := assert.New(t)

	registerer := prometheus.NewRegistry()
	mpool, err := NewMempool("mempool", registerer, &dummyBlkTimer{})
	assert.NoError(err)

	txes, err := createTestDecisionTxes(2)
	assert.NoError(err)

	for _, tx := range txes {
		// tx not already there
		assert.False(mpool.HasDecisionTxs())
		assert.False(mpool.Has(tx.ID()))

		// we can insert
		assert.NoError(mpool.Add(tx))

		// we can get it
		assert.True(mpool.HasDecisionTxs())
		assert.True(mpool.Has(tx.ID()))

		retrieved := mpool.Get(tx.ID())
		assert.True(retrieved != nil)
		assert.Equal(tx, retrieved)

		// // we can peek it
		txSize := len(tx.Bytes())
		peeked := mpool.PeekDecisionTxs(txSize)
		assert.True(peeked != nil)
		assert.Equal(tx, peeked[0])

		// once removed it cannot be there
		mpool.Remove([]*txs.Tx{tx})

		assert.False(mpool.HasDecisionTxs())
		assert.False(mpool.Has(tx.ID()))
		assert.Equal((*txs.Tx)(nil), mpool.Get(tx.ID()))
		assert.Equal([]*txs.Tx{}, mpool.PeekDecisionTxs(txSize))

		// we can reinsert it
		assert.NoError(mpool.Add(tx))

		// we can pop it
		popped := mpool.PopDecisionTxs(txSize)
		assert.True(len(popped) == 1)
		assert.Equal(tx, popped[0])

		// once popped it cannot be there
		assert.False(mpool.Has(tx.ID()))
		assert.Equal((*txs.Tx)(nil), mpool.Get(tx.ID()))
		assert.Equal([]*txs.Tx{}, mpool.PeekDecisionTxs(txSize))

		// // we can reinsert it again to grow mempool
		// assert.NoError(mpool.Add(tx))
	}
}

func TestProposalTxsInMempool(t *testing.T) {
	assert := assert.New(t)

	registerer := prometheus.NewRegistry()
	mpool, err := NewMempool("mempool", registerer, &dummyBlkTimer{})
	assert.NoError(err)

	txes, err := createTestProposalTxes(2)
	assert.NoError(err)

	for _, tx := range txes {
		// tx not already there
		assert.False(mpool.HasProposalTx())
		assert.False(mpool.Has(tx.ID()))

		// we can insert
		assert.NoError(mpool.Add(tx))

		// we can get it
		assert.True(mpool.HasProposalTx())
		assert.True(mpool.Has(tx.ID()))

		retrieved := mpool.Get(tx.ID())
		assert.True(retrieved != nil)
		assert.Equal(tx, retrieved)

		// // we can peek it
		peeked := mpool.PeekProposalTx()
		assert.True(peeked != nil)
		assert.Equal(tx, peeked)

		// once removed it cannot be there
		mpool.Remove([]*txs.Tx{tx})

		assert.False(mpool.HasProposalTx())
		assert.False(mpool.Has(tx.ID()))
		assert.Equal((*txs.Tx)(nil), mpool.Get(tx.ID()))
		assert.Equal((*txs.Tx)(nil), mpool.PeekProposalTx())

		// we can reinsert it
		assert.NoError(mpool.Add(tx))

		// we can pop it
		popped := mpool.PopProposalTx()
		assert.Equal(tx, popped)

		// once popped it cannot be there
		assert.False(mpool.Has(tx.ID()))
		assert.Equal((*txs.Tx)(nil), mpool.Get(tx.ID()))
		assert.Equal((*txs.Tx)(nil), mpool.PeekProposalTx())

		// // we can reinsert it again to grow mempool
		// assert.NoError(mpool.Add(tx))
	}
}

func createTestDecisionTxes(count int) ([]*txs.Tx, error) {
	res := make([]*txs.Tx, 0, count)
	for i := uint32(0); i < uint32(count); i++ {
		utx := &txs.CreateChainTx{
			BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    10,
				BlockchainID: ids.Empty.Prefix(uint64(i)),
				Ins: []*avax.TransferableInput{{
					UTXOID: avax.UTXOID{
						TxID:        ids.ID{'t', 'x', 'I', 'D'},
						OutputIndex: i,
					},
					Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 'r', 't'}},
					In: &secp256k1fx.TransferInput{
						Amt:   uint64(5678),
						Input: secp256k1fx.Input{SigIndices: []uint32{i}},
					},
				}},
				Outs: []*avax.TransferableOutput{{
					Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 'r', 't'}},
					Out: &secp256k1fx.TransferOutput{
						Amt: uint64(1234),
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
						},
					},
				}},
			}},
			SubnetID:    ids.GenerateTestID(),
			ChainName:   "chainName",
			VMID:        ids.GenerateTestID(),
			FxIDs:       []ids.ID{ids.GenerateTestID()},
			GenesisData: []byte{'g', 'e', 'n', 'D', 'a', 't', 'a'},
			SubnetAuth:  &secp256k1fx.Input{SigIndices: []uint32{1}},
		}

		tx, err := txs.NewSigned(utx, txs.Codec, nil)
		if err != nil {
			return nil, err
		}
		res = append(res, tx)
	}
	return res, nil
}

func createTestProposalTxes(count int) ([]*txs.Tx, error) {
	var clk mockable.Clock
	res := make([]*txs.Tx, 0, count)
	for i := 0; i < count; i++ {
		utx := &txs.AddValidatorTx{
			BaseTx: txs.BaseTx{},
			Validator: validator.Validator{
				End: uint64(clk.Time().Add(time.Duration(i) * time.Second).Unix()),
			},
			Stake:        nil,
			RewardsOwner: &secp256k1fx.OutputOwners{},
			Shares:       100,
		}

		tx, err := txs.NewSigned(utx, txs.Codec, nil)
		if err != nil {
			return nil, err
		}
		res = append(res, tx)
	}
	return res, nil
}
