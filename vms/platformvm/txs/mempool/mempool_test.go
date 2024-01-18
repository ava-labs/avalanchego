// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var preFundedKeys = secp256k1.TestKeys()

// shows that valid tx is not added to mempool if this would exceed its maximum
// size
func TestBlockBuilderMaxMempoolSizeHandling(t *testing.T) {
	require := require.New(t)

	registerer := prometheus.NewRegistry()
	mpool, err := New("mempool", registerer, nil)
	require.NoError(err)

	decisionTxs, err := createTestDecisionTxs(1)
	require.NoError(err)
	tx := decisionTxs[0]

	// shortcut to simulated almost filled mempool
	mpool.(*mempool).bytesAvailable = len(tx.Bytes()) - 1

	err = mpool.Add(tx)
	require.ErrorIs(err, ErrMempoolFull)

	// tx should not be marked as dropped if the mempool is full
	txID := tx.ID()
	mpool.MarkDropped(txID, err)
	require.NoError(mpool.GetDropReason(txID))

	// shortcut to simulated almost filled mempool
	mpool.(*mempool).bytesAvailable = len(tx.Bytes())

	err = mpool.Add(tx)
	require.NoError(err, "should have added tx to mempool")
}

func TestDecisionTxsInMempool(t *testing.T) {
	require := require.New(t)

	registerer := prometheus.NewRegistry()
	mpool, err := New("mempool", registerer, nil)
	require.NoError(err)

	decisionTxs, err := createTestDecisionTxs(2)
	require.NoError(err)

	for _, tx := range decisionTxs {
		// tx not already there
		_, ok := mpool.Get(tx.ID())
		require.False(ok)

		// we can insert
		require.NoError(mpool.Add(tx))

		// we can get it
		got, ok := mpool.Get(tx.ID())
		require.True(ok)
		require.Equal(tx, got)

		// once removed it cannot be there
		mpool.Remove(tx)

		_, ok = mpool.Get(tx.ID())
		require.False(ok)

		// we can reinsert it again to grow the mempool
		require.NoError(mpool.Add(tx))
	}
}

func TestProposalTxsInMempool(t *testing.T) {
	require := require.New(t)

	registerer := prometheus.NewRegistry()
	mpool, err := New("mempool", registerer, nil)
	require.NoError(err)

	// The proposal txs are ordered by decreasing start time. This means after
	// each insertion, the last inserted transaction should be on the top of the
	// heap.
	proposalTxs, err := createTestProposalTxs(2)
	require.NoError(err)

	for _, tx := range proposalTxs {
		_, ok := mpool.Get(tx.ID())
		require.False(ok)

		// we can insert
		require.NoError(mpool.Add(tx))

		// we can get it
		got, ok := mpool.Get(tx.ID())
		require.Equal(tx, got)
		require.True(ok)

		// once removed it cannot be there
		mpool.Remove(tx)

		_, ok = mpool.Get(tx.ID())
		require.False(ok)

		// we can reinsert it again to grow the mempool
		require.NoError(mpool.Add(tx))
	}
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
		tx, err := generateAddValidatorTx(
			uint64(now.Add(time.Duration(count-i)*time.Second).Unix()), // startTime
			0, // endTime
		)
		if err != nil {
			return nil, err
		}
		proposalTxs = append(proposalTxs, tx)
	}
	return proposalTxs, nil
}

func generateAddValidatorTx(startTime uint64, endTime uint64) (*txs.Tx, error) {
	utx := &txs.AddValidatorTx{
		BaseTx: txs.BaseTx{},
		Validator: txs.Validator{
			NodeID: ids.GenerateTestNodeID(),
			Start:  startTime,
			End:    endTime,
		},
		StakeOuts:        nil,
		RewardsOwner:     &secp256k1fx.OutputOwners{},
		DelegationShares: 100,
	}

	return txs.NewSigned(utx, txs.Codec, nil)
}

func TestPeekTxs(t *testing.T) {
	require := require.New(t)

	registerer := prometheus.NewRegistry()
	toEngine := make(chan common.Message, 100)
	mempool, err := New("mempool", registerer, toEngine)
	require.NoError(err)

	testDecisionTxs, err := createTestDecisionTxs(1)
	require.NoError(err)
	testProposalTxs, err := createTestProposalTxs(1)
	require.NoError(err)

	tx, exists := mempool.Peek()
	require.False(exists)
	require.Nil(tx)

	require.NoError(mempool.Add(testDecisionTxs[0]))
	require.NoError(mempool.Add(testProposalTxs[0]))

	tx, exists = mempool.Peek()
	require.True(exists)
	require.Equal(tx, testDecisionTxs[0])
	require.NotEqual(tx, testProposalTxs[0])

	mempool.Remove(testDecisionTxs[0])

	tx, exists = mempool.Peek()
	require.True(exists)
	require.NotEqual(tx, testDecisionTxs[0])
	require.Equal(tx, testProposalTxs[0])

	mempool.Remove(testProposalTxs[0])

	tx, exists = mempool.Peek()
	require.False(exists)
	require.Nil(tx)
}

func TestRemoveConflicts(t *testing.T) {
	require := require.New(t)

	registerer := prometheus.NewRegistry()
	toEngine := make(chan common.Message, 100)
	mempool, err := New("mempool", registerer, toEngine)
	require.NoError(err)

	txs, err := createTestDecisionTxs(1)
	require.NoError(err)
	conflictTxs, err := createTestDecisionTxs(1)
	require.NoError(err)

	require.NoError(mempool.Add(txs[0]))

	tx, exists := mempool.Peek()
	require.True(exists)
	require.Equal(tx, txs[0])

	mempool.Remove(conflictTxs[0])

	_, exists = mempool.Peek()
	require.False(exists)
}

func TestIterate(t *testing.T) {
	require := require.New(t)

	registerer := prometheus.NewRegistry()
	toEngine := make(chan common.Message, 100)
	mempool, err := New("mempool", registerer, toEngine)
	require.NoError(err)

	testDecisionTxs, err := createTestDecisionTxs(1)
	require.NoError(err)
	decisionTx := testDecisionTxs[0]

	testProposalTxs, err := createTestProposalTxs(1)
	require.NoError(err)
	proposalTx := testProposalTxs[0]

	require.NoError(mempool.Add(decisionTx))
	require.NoError(mempool.Add(proposalTx))

	expectedSet := set.Of(
		decisionTx.ID(),
		proposalTx.ID(),
	)

	set := set.NewSet[ids.ID](2)
	mempool.Iterate(func(tx *txs.Tx) bool {
		set.Add(tx.ID())
		return true
	})

	require.Equal(expectedSet, set)
}
