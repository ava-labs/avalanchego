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
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
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
	mpool, err := NewMempool("mempool", registerer, &noopBlkTimer{})
	require.NoError(err)

	decisionTxs, err := createTestDecisionTxs(1)
	require.NoError(err)
	tx := decisionTxs[0]

	// shortcut to simulated almost filled mempool
	mpool.(*mempool).bytesAvailable = len(tx.Bytes()) - 1

	err = mpool.Add(tx)
	require.True(errors.Is(err, errMempoolFull), err, "max mempool size breached")

	// shortcut to simulated almost filled mempool
	mpool.(*mempool).bytesAvailable = len(tx.Bytes())

	err = mpool.Add(tx)
	require.NoError(err, "should have added tx to mempool")
}

func TestDecisionTxsInMempool(t *testing.T) {
	require := require.New(t)

	registerer := prometheus.NewRegistry()
	mpool, err := NewMempool("mempool", registerer, &noopBlkTimer{})
	require.NoError(err)

	decisionTxs, err := createTestDecisionTxs(2)
	require.NoError(err)

	// txs must not already there before we start
	require.False(mpool.HasTxs())

	for _, tx := range decisionTxs {
		// tx not already there
		require.False(mpool.Has(tx.ID()))

		// we can insert
		require.NoError(mpool.Add(tx))

		// we can get it
		require.True(mpool.Has(tx.ID()))

		retrieved := mpool.Get(tx.ID())
		require.True(retrieved != nil)
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
		require.NoError(mpool.Add(tx))
	}
}

func TestProposalTxsInMempool(t *testing.T) {
	require := require.New(t)

	registerer := prometheus.NewRegistry()
	mpool, err := NewMempool("mempool", registerer, &noopBlkTimer{})
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
		require.NoError(mpool.Add(tx))

		// we can get it
		require.True(mpool.HasStakerTx())
		require.True(mpool.Has(tx.ID()))

		retrieved := mpool.Get(tx.ID())
		require.True(retrieved != nil)
		require.Equal(tx, retrieved)

		{
			// we can peek it
			peeked := mpool.PeekStakerTx()
			require.True(peeked != nil)
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
		require.NoError(mpool.Add(tx))
	}
}

func TestPeekTxs(t *testing.T) {
	require := require.New(t)

	registerer := prometheus.NewRegistry()
	mpool, err := NewMempool("mempool", registerer, &noopBlkTimer{})
	require.NoError(err)

	total := 100
	decisionTxs, err := createTestDecisionTxs(total)
	require.NoError(err)
	stakerTxs, err := createTestAddPermissionlessValidatorTxs(total)
	require.NoError(err)

	// txs must not already there before we start
	require.False(mpool.HasTxs())

	oneDecisionTxSize, totalDecisionTxSize := 0, 0
	for _, tx := range decisionTxs {
		require.False(mpool.Has(tx.ID()))
		require.NoError(mpool.Add(tx))

		size := tx.Size()
		if oneDecisionTxSize != 0 {
			// assume all txs have the same size for the purpose of testing
			require.Equal(oneDecisionTxSize, size)
		} else {
			oneDecisionTxSize = size
		}

		totalDecisionTxSize += oneDecisionTxSize
	}

	oneStakerTxSize, totalStakerTxSize := 0, 0
	for _, tx := range stakerTxs {
		require.False(mpool.Has(tx.ID()))
		require.NoError(mpool.Add(tx))

		size := tx.Size()
		if oneStakerTxSize != 0 {
			// assume all txs have the same size for the purpose of testing
			require.Equal(oneStakerTxSize, size)
		} else {
			oneStakerTxSize = size
		}

		totalStakerTxSize += oneStakerTxSize
	}

	tt := []struct {
		desc        string
		maxTxBytes  int
		expectedTxs int
	}{
		{
			desc:        "peek txs with zero max tx bytes should return none",
			maxTxBytes:  0,
			expectedTxs: 0,
		},
		{
			desc:        "peek txs with MaxInt should return all decision + staker txs",
			maxTxBytes:  math.MaxInt,
			expectedTxs: 2 * total,
		},
		{
			desc:        "peek txs with totalDecisionTxSize + totalStakerTxSize should return all",
			maxTxBytes:  totalDecisionTxSize + totalStakerTxSize,
			expectedTxs: 2 * total,
		},
		{
			desc:        "peek txs with totalDecisionTxSize should only return decision txs",
			maxTxBytes:  totalDecisionTxSize,
			expectedTxs: total,
		},
		{
			desc:        "peek txs with totalDecisionTxSize - one decision tx size should return total - 1",
			maxTxBytes:  totalDecisionTxSize - oneDecisionTxSize,
			expectedTxs: total - 1,
		},
		{
			desc:        "peek txs with half of totalDecisionTxSize should return total/2",
			maxTxBytes:  totalDecisionTxSize / 2,
			expectedTxs: total / 2,
		},
		{
			desc:        "peek txs with totalDecisionTxSize + 3 staker tx size should return total + 3",
			maxTxBytes:  totalDecisionTxSize + 3*oneStakerTxSize,
			expectedTxs: total + 3,
		},
		{
			desc:        "peek txs with totalDecisionTxSize + totalStakerTxSize/2 should return total + total/2",
			maxTxBytes:  totalDecisionTxSize + totalStakerTxSize/2,
			expectedTxs: total + total/2,
		},
	}
	for i, tv := range tt {
		require.Lenf(mpool.PeekTxs(tv.maxTxBytes), tv.expectedTxs, "[%d] %s", i, tv.desc)
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
	var clk mockable.Clock
	proposalTxs := make([]*txs.Tx, 0, count)
	for i := 0; i < count; i++ {
		utx := &txs.AddValidatorTx{
			BaseTx: txs.BaseTx{},
			Validator: txs.Validator{
				Start: uint64(clk.Time().Add(time.Duration(count-i) * time.Second).Unix()),
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

func createTestAddPermissionlessValidatorTxs(count int) ([]*txs.Tx, error) {
	var (
		networkID = uint32(1337)
		chainID   = ids.GenerateTestID()
	)

	// A BaseTx that passes syntactic verification.
	validBaseTx := txs.BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		},
	}

	blsSK, err := bls.NewSecretKey()
	if err != nil {
		return nil, err
	}
	blsPOP := signer.NewProofOfPossession(blsSK)

	signers := [][]*secp256k1.PrivateKey{{secp256k1.TestKeys()[0]}}

	var clk mockable.Clock
	tss := make([]*txs.Tx, 0, count)
	for i := uint32(0); i < uint32(count); i++ {
		utx := &txs.AddPermissionlessValidatorTx{
			BaseTx: validBaseTx,
			Validator: txs.Validator{
				NodeID: ids.GenerateTestNodeID(),
				Start:  uint64(clk.Time().Add(time.Duration(uint32(count)-i) * time.Second).Unix()),
			},
			Subnet: ids.GenerateTestID(),
			Signer: blsPOP,
			StakeOuts: []*avax.TransferableOutput{
				{
					Asset: avax.Asset{
						ID: ids.GenerateTestID(),
					},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
				{
					Asset: avax.Asset{
						ID: ids.GenerateTestID(),
					},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ValidatorRewardsOwner: &secp256k1fx.OutputOwners{
				Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
				Threshold: 1,
			},
			DelegatorRewardsOwner: &secp256k1fx.OutputOwners{
				Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
				Threshold: 1,
			},
			DelegationShares: 20_000,
		}

		tx, err := txs.NewSigned(utx, txs.Codec, signers)
		if err != nil {
			return nil, err
		}
		tss = append(tss, tx)
	}
	return tss, nil
}
