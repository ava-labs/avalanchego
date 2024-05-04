package mempool

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	xtxs "github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	ptxs "github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func newXMempool(toEngine chan<- common.Message) (*Mempool[*xtxs.Tx], error) {
	return New[*xtxs.Tx](
		"mempool",
		prometheus.NewRegistry(),
		toEngine,
		"count",
		"Number of transactions in the mempool",
	)
}

func TestAdd(t *testing.T) {
	tx0 := newTx(0, 32)

	tests := []struct {
		name       string
		initialTxs []*xtxs.Tx
		tx         *xtxs.Tx
		err        error
		dropReason error
	}{
		{
			name:       "successfully add tx",
			initialTxs: nil,
			tx:         tx0,
			err:        nil,
			dropReason: nil,
		},
		{
			name:       "attempt adding duplicate tx",
			initialTxs: []*xtxs.Tx{tx0},
			tx:         tx0,
			err:        ErrDuplicateTx,
			dropReason: nil,
		},
		{
			name:       "attempt adding too large tx",
			initialTxs: nil,
			tx:         newTx(0, MaxTxSize+1),
			err:        ErrTxTooLarge,
			dropReason: ErrTxTooLarge,
		},
		{
			name:       "attempt adding tx when full",
			initialTxs: newTxs(maxMempoolSize/MaxTxSize, MaxTxSize),
			tx:         newTx(maxMempoolSize/MaxTxSize, MaxTxSize),
			err:        ErrMempoolFull,
			dropReason: nil,
		},
		{
			name:       "attempt adding conflicting tx",
			initialTxs: []*xtxs.Tx{tx0},
			tx:         newTx(0, 32),
			err:        ErrConflictsWithOtherTx,
			dropReason: ErrConflictsWithOtherTx,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			mempool, err := newXMempool(nil)
			require.NoError(err)

			for _, tx := range test.initialTxs {
				require.NoError(mempool.Add(tx))
			}

			err = mempool.Add(test.tx)
			require.ErrorIs(err, test.err)

			txID := test.tx.ID()

			if err != nil {
				mempool.MarkDropped(txID, err)
			}

			err = mempool.GetDropReason(txID)
			require.ErrorIs(err, test.dropReason)
		})
	}
}

func TestGet(t *testing.T) {
	require := require.New(t)

	mempool, err := newXMempool(nil)
	require.NoError(err)

	tx := newTx(0, 32)
	txID := tx.ID()

	_, exists := mempool.Get(txID)
	require.False(exists)

	require.NoError(mempool.Add(tx))

	returned, exists := mempool.Get(txID)
	require.True(exists)
	require.Equal(tx, returned)

	mempool.Remove(tx)

	_, exists = mempool.Get(txID)
	require.False(exists)
}

func TestPeek(t *testing.T) {
	require := require.New(t)

	mempool, err := newXMempool(nil)
	require.NoError(err)

	_, exists := mempool.Peek()
	require.False(exists)

	tx0 := newTx(0, 32)
	tx1 := newTx(1, 32)

	require.NoError(mempool.Add(tx0))
	require.NoError(mempool.Add(tx1))

	tx, exists := mempool.Peek()
	require.True(exists)
	require.Equal(tx, tx0)

	mempool.Remove(tx0)

	tx, exists = mempool.Peek()
	require.True(exists)
	require.Equal(tx, tx1)

	mempool.Remove(tx0)

	tx, exists = mempool.Peek()
	require.True(exists)
	require.Equal(tx, tx1)

	mempool.Remove(tx1)

	_, exists = mempool.Peek()
	require.False(exists)
}

func TestRemoveConflict(t *testing.T) {
	require := require.New(t)

	mempool, err := newXMempool(nil)
	require.NoError(err)

	tx := newTx(0, 32)
	txConflict := newTx(0, 32)

	require.NoError(mempool.Add(tx))

	returnedTx, exists := mempool.Peek()
	require.True(exists)
	require.Equal(returnedTx, tx)

	mempool.Remove(txConflict)

	_, exists = mempool.Peek()
	require.False(exists)
}

func TestXIterate(t *testing.T) {
	require := require.New(t)

	mempool, err := newXMempool(nil)
	require.NoError(err)

	var (
		iteratedTxs []*xtxs.Tx
		maxLen      = 2
	)
	addTxs := func(tx *xtxs.Tx) bool {
		iteratedTxs = append(iteratedTxs, tx)
		return len(iteratedTxs) < maxLen
	}
	mempool.Iterate(addTxs)
	require.Empty(iteratedTxs)

	tx0 := newTx(0, 32)
	require.NoError(mempool.Add(tx0))

	mempool.Iterate(addTxs)
	require.Equal([]*xtxs.Tx{tx0}, iteratedTxs)

	tx1 := newTx(1, 32)
	require.NoError(mempool.Add(tx1))

	iteratedTxs = nil
	mempool.Iterate(addTxs)
	require.Equal([]*xtxs.Tx{tx0, tx1}, iteratedTxs)

	tx2 := newTx(2, 32)
	require.NoError(mempool.Add(tx2))

	iteratedTxs = nil
	mempool.Iterate(addTxs)
	require.Equal([]*xtxs.Tx{tx0, tx1}, iteratedTxs)

	mempool.Remove(tx0, tx2)

	iteratedTxs = nil
	mempool.Iterate(addTxs)
	require.Equal([]*xtxs.Tx{tx1}, iteratedTxs)
}

func TestRequestBuildBlock(t *testing.T) {
	require := require.New(t)

	toEngine := make(chan common.Message, 1)
	mempool, err := newXMempool(toEngine)
	require.NoError(err)

	mempool.RequestBuildBlock(false)
	select {
	case <-toEngine:
		require.FailNow("should not have sent message to engine")
	default:
	}

	tx := newTx(0, 32)
	require.NoError(mempool.Add(tx))

	mempool.RequestBuildBlock(false)
	mempool.RequestBuildBlock(false) // Must not deadlock
	select {
	case <-toEngine:
	default:
		require.FailNow("should have sent message to engine")
	}
	select {
	case <-toEngine:
		require.FailNow("should have only sent one message to engine")
	default:
	}
}

func TestDropped(t *testing.T) {
	require := require.New(t)

	mempool, err := newXMempool(nil)
	require.NoError(err)

	tx := newTx(0, 32)
	txID := tx.ID()
	testErr := errors.New("test")

	mempool.MarkDropped(txID, testErr)

	err = mempool.GetDropReason(txID)
	require.ErrorIs(err, testErr)

	require.NoError(mempool.Add(tx))
	require.NoError(mempool.GetDropReason(txID))

	mempool.MarkDropped(txID, testErr)
	require.NoError(mempool.GetDropReason(txID))
}

func newTxs(num int, size int) []*xtxs.Tx {
	txs := make([]*xtxs.Tx, num)
	for i := range txs {
		txs[i] = newTx(uint32(i), size)
	}
	return txs
}

func newTx(index uint32, size int) *xtxs.Tx {
	tx := &xtxs.Tx{Unsigned: &xtxs.BaseTx{BaseTx: avax.BaseTx{
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        ids.ID{'t', 'x', 'I', 'D'},
				OutputIndex: index,
			},
		}},
	}}}
	tx.SetBytes(utils.RandomBytes(size), utils.RandomBytes(size))
	return tx
}

var preFundedKeys = secp256k1.TestKeys()

func newPMempool(toEngine chan<- common.Message) (*Mempool[*ptxs.Tx], error) {
	return New[*ptxs.Tx](
		"mempool",
		prometheus.NewRegistry(),
		toEngine,
		"txs",
		"Number of decision/staker transactions in the mempool",
	)
}

// shows that valid tx is not added to mempool if this would exceed its maximum
// size
func TestBlockBuilderMaxMempoolSizeHandling(t *testing.T) {
	require := require.New(t)

	mpool, err := newPMempool(nil)
	require.NoError(err)

	decisionTxs, err := createTestDecisionTxs(1)
	require.NoError(err)
	tx := decisionTxs[0]

	// shortcut to simulated almost filled mempool
	mpool.bytesAvailable = len(tx.Bytes()) - 1

	err = mpool.Add(tx)
	require.ErrorIs(err, ErrMempoolFull)

	// tx should not be marked as dropped if the mempool is full
	txID := tx.ID()
	mpool.MarkDropped(txID, err)
	require.NoError(mpool.GetDropReason(txID))

	// shortcut to simulated almost filled mempool
	mpool.bytesAvailable = len(tx.Bytes())

	err = mpool.Add(tx)
	require.NoError(err, "should have added tx to mempool")
}

func TestDecisionTxsInMempool(t *testing.T) {
	require := require.New(t)

	mpool, err := newPMempool(nil)
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

	mpool, err := newPMempool(nil)
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

func createTestDecisionTxs(count int) ([]*ptxs.Tx, error) {
	decisionTxs := make([]*ptxs.Tx, 0, count)
	for i := uint32(0); i < uint32(count); i++ {
		utx := &ptxs.CreateChainTx{
			BaseTx: ptxs.BaseTx{BaseTx: avax.BaseTx{
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

		tx, err := ptxs.NewSigned(utx, ptxs.Codec, nil)
		if err != nil {
			return nil, err
		}
		decisionTxs = append(decisionTxs, tx)
	}
	return decisionTxs, nil
}

// Proposal txs are sorted by decreasing start time
func createTestProposalTxs(count int) ([]*ptxs.Tx, error) {
	now := time.Now()
	proposalTxs := make([]*ptxs.Tx, 0, count)
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

func generateAddValidatorTx(startTime uint64, endTime uint64) (*ptxs.Tx, error) {
	utx := &ptxs.AddValidatorTx{
		BaseTx: ptxs.BaseTx{},
		Validator: ptxs.Validator{
			NodeID: ids.GenerateTestNodeID(),
			Start:  startTime,
			End:    endTime,
		},
		StakeOuts:        nil,
		RewardsOwner:     &secp256k1fx.OutputOwners{},
		DelegationShares: 100,
	}

	return ptxs.NewSigned(utx, ptxs.Codec, nil)
}

func TestPeekTxs(t *testing.T) {
	require := require.New(t)

	toEngine := make(chan common.Message, 100)
	mempool, err := newPMempool(toEngine)
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

	toEngine := make(chan common.Message, 100)
	mempool, err := newPMempool(toEngine)
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

func TestPIterate(t *testing.T) {
	require := require.New(t)

	toEngine := make(chan common.Message, 100)
	mempool, err := newPMempool(toEngine)
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
	mempool.Iterate(func(tx *ptxs.Tx) bool {
		set.Add(tx.ID())
		return true
	})

	require.Equal(expectedSet, set)
}
