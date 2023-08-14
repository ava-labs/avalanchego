// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var _ BlockTimer = (*noopBlkTimer)(nil)

type noopBlkTimer struct{}

func (*noopBlkTimer) ResetBlockTimer() {}

var preFundedKeys = secp256k1.TestKeys()

// shows that valid tx is not added to mempool if this would exceed its maximum
// size
func TestBlockBuilderMaxMempoolSizeHandling(t *testing.T) {
	require := require.New(t)

	registerer := prometheus.NewRegistry()
	mpool, err := NewMempool(&config.Config{}, &noopBlkTimer{}, "mempool", registerer)
	require.NoError(err)

	decisionTxs, err := createTestDecisionTxs(1)
	require.NoError(err)
	tx := decisionTxs[0]

	// shortcut to simulated almost filled mempool
	mpool.(*mempool).bytesAvailable = len(tx.Bytes()) - 1

	err = mpool.Add(tx, time.Time{})
	require.True(errors.Is(err, errMempoolFull), err, "max mempool size breached")

	// shortcut to simulated almost filled mempool
	mpool.(*mempool).bytesAvailable = len(tx.Bytes())

	err = mpool.Add(tx, time.Time{})
	require.NoError(err, "should have added tx to mempool")
}

func TestDecisionTxsInMempool(t *testing.T) {
	require := require.New(t)

	registerer := prometheus.NewRegistry()
	mpool, err := NewMempool(&config.Config{}, &noopBlkTimer{}, "mempool", registerer)
	require.NoError(err)

	decisionTxs, err := createTestDecisionTxs(2)
	require.NoError(err)

	// txs must not already there before we start
	require.False(mpool.HasTxs())

	for _, tx := range decisionTxs {
		// tx not already there
		require.False(mpool.Has(tx.ID()))

		// we can insert
		require.NoError(mpool.Add(tx, time.Time{}))

		// we can get it
		require.True(mpool.Has(tx.ID()))

		retrieved := mpool.Get(tx.ID())
		require.NotNil(retrieved)
		require.Equal(tx, retrieved)

		// we can peek it
		peeked := mpool.PeekTxs(math.MaxInt)

		// tx will be among those peeked,
		// in NO PARTICULAR ORDER
		found := false
		for _, pk := range peeked {
			if pk.ID() == tx.ID() {
				found = true
				break
			}
		}
		require.True(found)

		// once removed it cannot be there
		mpool.Remove([]*txs.Tx{tx})

		require.False(mpool.Has(tx.ID()))
		require.Equal((*txs.Tx)(nil), mpool.Get(tx.ID()))

		// we can reinsert it again to grow the mempool
		require.NoError(mpool.Add(tx, time.Time{}))
	}
}

func TestProposalTxsInMempool(t *testing.T) {
	require := require.New(t)

	registerer := prometheus.NewRegistry()
	mpool, err := NewMempool(&config.Config{
		ContinuousStakingTime: mockable.MaxTime,
	}, &noopBlkTimer{}, "mempool", registerer)
	require.NoError(err)

	// The proposal txs are ordered by decreasing start time. This means after
	// each insertion, the last inserted transaction should be on the top of the
	// heap.
	proposalTxs, err := createTestProposalTxs(2)
	require.NoError(err)

	// txs should not be already there
	require.False(mpool.HasStakerTx())

	for i, tx := range proposalTxs {
		require.False(mpool.Has(tx.ID()))

		// we can insert
		require.NoError(mpool.Add(tx, time.Time{}))

		// we can get it
		require.True(mpool.HasStakerTx())
		require.True(mpool.Has(tx.ID()))

		retrieved := mpool.Get(tx.ID())
		require.NotNil(retrieved)
		require.Equal(tx, retrieved)

		{
			// we can peek it
			peeked := mpool.PeekStakerTx()
			require.NotNil(peeked)
			require.Equal(tx, peeked)
		}

		{
			// we can peek it
			peeked := mpool.PeekTxs(math.MaxInt)
			require.Len(peeked, i+1)

			// tx will be among those peeked,
			// in NO PARTICULAR ORDER
			found := false
			for _, pk := range peeked {
				if pk.ID() == tx.ID() {
					found = true
					break
				}
			}
			require.True(found)
		}

		// once removed it cannot be there
		mpool.Remove([]*txs.Tx{tx})

		require.False(mpool.Has(tx.ID()))
		require.Equal((*txs.Tx)(nil), mpool.Get(tx.ID()))

		// we can reinsert it again to grow the mempool
		require.NoError(mpool.Add(tx, time.Time{}))
	}
}

func TestContinuousStakingForkInMempool(t *testing.T) {
	require := require.New(t)

	forkTime := time.Now()
	registerer := prometheus.NewRegistry()
	mpool, err := NewMempool(&config.Config{
		ContinuousStakingTime: forkTime,
	}, &noopBlkTimer{}, "mempool", registerer)
	require.NoError(err)

	txs, err := createTestProposalTxs(1)
	require.NoError(err)
	tx := txs[0]

	// insert pre fork
	preForkTime := forkTime.Add(-1 * time.Second)
	require.NoError(mpool.Add(tx, preForkTime))
	require.True(mpool.HasStakerTx())
	retrievedTx := mpool.PeekStakerTx()
	require.Equal(tx, retrievedTx)

	// insert post fork
	postForkTime := forkTime.Add(time.Second)
	err = mpool.Add(tx, postForkTime)
	require.ErrorIs(err, errTxAlreadyInMempool)
	mpool.Remove(txs)
	require.NoError(mpool.Add(tx, postForkTime))
	require.False(mpool.HasStakerTx(), "post fork there should not be staker txs anymore")
	require.True(mpool.HasTxs())
	retrievedTxs := mpool.PeekTxs(math.MaxInt)
	require.True(len(retrievedTxs) == 1)
	require.Equal(retrievedTxs[0], tx)

	// remove post fork
	mpool.Remove(retrievedTxs)
	require.False(mpool.HasTxs())
}

func createTestDecisionTxs(count int) ([]*txs.Tx, error) {
	decisionTxs := make([]*txs.Tx, 0, count)
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
		decisionTxs = append(decisionTxs, tx)
	}
	return decisionTxs, nil
}

// Proposal txs are sorted by decreasing start time
func createTestProposalTxs(count int) ([]*txs.Tx, error) {
	now := time.Now()
	proposalTxs := make([]*txs.Tx, 0, count)
	for i := 0; i < count; i++ {
		utx := &txs.AddValidatorTx{
			BaseTx: txs.BaseTx{},
			Validator: txs.Validator{
				Start: uint64(now.Add(time.Duration(count-i) * time.Second).Unix()),
			},
			StakeOuts:        nil,
			RewardsOwner:     &secp256k1fx.OutputOwners{},
			DelegationShares: 100,
		}

		tx, err := txs.NewSigned(utx, txs.Codec, nil)
		if err != nil {
			return nil, err
		}
		proposalTxs = append(proposalTxs, tx)
	}
	return proposalTxs, nil
}
