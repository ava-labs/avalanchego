package metadata

import (
	"fmt"
	"testing"

	"github.com/ava-labs/simplex/avalanchego"
	"github.com/ava-labs/simplex/common"

	"github.com/stretchr/testify/require"
)

func TestAuxiliaryInfoBatchEqual(t *testing.T) {
	for _, tt := range []struct {
		name     string
		a        *AuxiliaryInfoBatch
		b        *AuxiliaryInfoBatch
		expected bool
	}{
		{
			name:     "both nil",
			a:        nil,
			b:        nil,
			expected: true,
		},
		{
			name:     "nil vs non-nil",
			a:        nil,
			b:        &AuxiliaryInfoBatch{},
			expected: false,
		},
		{
			name:     "both zero",
			a:        &AuxiliaryInfoBatch{},
			b:        &AuxiliaryInfoBatch{},
			expected: true,
		},
		{
			name: "equal with data",
			a: &AuxiliaryInfoBatch{
				data: []common.AuxiliaryInfo{
					{Version: 1, Data: []byte{1, 2, 3}},
					{Version: 2, Data: []byte{4, 5}},
				},
				PrevAuxInfoSeq: 7,
			},
			b: &AuxiliaryInfoBatch{
				data: []common.AuxiliaryInfo{
					{Version: 1, Data: []byte{1, 2, 3}},
					{Version: 2, Data: []byte{4, 5}},
				},
				PrevAuxInfoSeq: 7,
			},
			expected: true,
		},
		{
			name: "nil data vs empty data",
			a:    &AuxiliaryInfoBatch{},
			b: &AuxiliaryInfoBatch{
				data: []common.AuxiliaryInfo{},
			},
			expected: true,
		},
		{
			name: "different PrevAuxInfoSeq",
			a: &AuxiliaryInfoBatch{
				data:           []common.AuxiliaryInfo{{Version: 1, Data: []byte{1}}},
				PrevAuxInfoSeq: 1,
			},
			b: &AuxiliaryInfoBatch{
				data:           []common.AuxiliaryInfo{{Version: 1, Data: []byte{1}}},
				PrevAuxInfoSeq: 2,
			},
			expected: false,
		},
		{
			name: "different number of entries",
			a: &AuxiliaryInfoBatch{
				data: []common.AuxiliaryInfo{{Version: 1, Data: []byte{1}}},
			},
			b: &AuxiliaryInfoBatch{
				data: []common.AuxiliaryInfo{
					{Version: 1, Data: []byte{1}},
					{Version: 1, Data: []byte{2}},
				},
			},
			expected: false,
		},
		{
			name: "different entry version",
			a: &AuxiliaryInfoBatch{
				data: []common.AuxiliaryInfo{{Version: 1, Data: []byte{1}}},
			},
			b: &AuxiliaryInfoBatch{
				data: []common.AuxiliaryInfo{{Version: 2, Data: []byte{1}}},
			},
			expected: false,
		},
		{
			name: "different entry data",
			a: &AuxiliaryInfoBatch{
				data: []common.AuxiliaryInfo{{Version: 1, Data: []byte{1}}},
			},
			b: &AuxiliaryInfoBatch{
				data: []common.AuxiliaryInfo{{Version: 1, Data: []byte{2}}},
			},
			expected: false,
		},
		{
			name: "same entries in different order",
			a: &AuxiliaryInfoBatch{
				data: []common.AuxiliaryInfo{
					{Version: 1, Data: []byte{1}},
					{Version: 2, Data: []byte{2}},
				},
			},
			b: &AuxiliaryInfoBatch{
				data: []common.AuxiliaryInfo{
					{Version: 2, Data: []byte{2}},
					{Version: 1, Data: []byte{1}},
				},
			},
			expected: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.a.Equal(tt.b))
			require.Equal(t, tt.expected, tt.b.Equal(tt.a))
		})
	}
}

func TestAuxiliaryInfoBatchIsZero(t *testing.T) {
	for _, tt := range []struct {
		name     string
		batch    *AuxiliaryInfoBatch
		expected bool
	}{
		{
			name:     "zero value",
			batch:    &AuxiliaryInfoBatch{},
			expected: true,
		},
		{
			name:     "empty data slice",
			batch:    &AuxiliaryInfoBatch{data: []common.AuxiliaryInfo{}},
			expected: true,
		},
		{
			name:     "non-zero PrevAuxInfoSeq",
			batch:    &AuxiliaryInfoBatch{PrevAuxInfoSeq: 1},
			expected: false,
		},
		{
			name:     "non-empty data",
			batch:    &AuxiliaryInfoBatch{data: []common.AuxiliaryInfo{{Version: 1, Data: []byte{1}}}},
			expected: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.batch.IsZero())
		})
	}
}

// batchBlock returns a StateMachineBlock whose metadata carries the given AuxiliaryInfoBatch.
func batchBlock(batch *AuxiliaryInfoBatch) StateMachineBlock {
	return StateMachineBlock{
		Metadata: StateMachineMetadata{
			AuxiliaryInfoBatch: batch,
		},
	}
}

// blockRetrieverFromMap returns a BlockRetriever backed by the given seq -> block mapping,
// failing the test if a sequence outside the mapping is requested.
func blockRetrieverFromMap(t *testing.T, blocks map[uint64]StateMachineBlock) BlockRetriever {
	return func(seq uint64, _ common.Digest) (StateMachineBlock, *common.Finalization, error) {
		block, ok := blocks[seq]
		require.True(t, ok, "requested unexpected block at seq %d", seq)
		return block, nil, nil
	}
}

func TestGetAuxiliaryHistory(t *testing.T) {
	const (
		defaultVersionID = common.VersionID(42)
		startSeq         = uint64(10)
	)

	for _, tt := range []struct {
		name string
		// batch of the block traversal starts from
		startBatch *AuxiliaryInfoBatch
		// batches of ancestor blocks by seq, reachable via PrevAuxInfoSeq links
		prevBatches map[uint64]*AuxiliaryInfoBatch
		expected    AuxInfoHistory
	}{
		{
			name:       "no batch",
			startBatch: nil,
			expected: AuxInfoHistory{
				LastSeq:         0,
				OldestVersionID: defaultVersionID,
			},
		},
		{
			name:       "batch with no entries",
			startBatch: &AuxiliaryInfoBatch{},
			expected: AuxInfoHistory{
				LastSeq:         0,
				OldestVersionID: defaultVersionID,
			},
		},
		{
			name: "single batch preserves entry order",
			startBatch: &AuxiliaryInfoBatch{
				data: []common.AuxiliaryInfo{
					{Version: 1, Data: []byte("a")},
					{Version: 2, Data: []byte("b")},
				},
			},
			expected: AuxInfoHistory{
				Data:            [][]byte{[]byte("a"), []byte("b")},
				LastSeq:         startSeq,
				OldestVersionID: 1,
			},
		},
		{
			name: "entries with empty data are skipped",
			startBatch: &AuxiliaryInfoBatch{
				data: []common.AuxiliaryInfo{
					{Version: 1, Data: []byte("a")},
					{Version: 2, Data: nil},
					{Version: 3, Data: []byte("c")},
				},
			},
			expected: AuxInfoHistory{
				Data:            [][]byte{[]byte("a"), []byte("c")},
				LastSeq:         startSeq,
				OldestVersionID: 1,
			},
		},
		{
			name: "chain of batches ordered oldest to newest",
			startBatch: &AuxiliaryInfoBatch{
				data:           []common.AuxiliaryInfo{{Version: 3, Data: []byte("d")}},
				PrevAuxInfoSeq: 5,
			},
			prevBatches: map[uint64]*AuxiliaryInfoBatch{
				5: {
					data: []common.AuxiliaryInfo{
						{Version: 2, Data: []byte("b")},
						{Version: 2, Data: []byte("c")},
					},
					PrevAuxInfoSeq: 3,
				},
				3: {
					data: []common.AuxiliaryInfo{{Version: 1, Data: []byte("a")}},
				},
			},
			expected: AuxInfoHistory{
				Data:            [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d")},
				LastSeq:         startSeq,
				OldestVersionID: 1,
			},
		},
		{
			name: "empty starting batch inherits from ancestors",
			startBatch: &AuxiliaryInfoBatch{
				PrevAuxInfoSeq: 4,
			},
			prevBatches: map[uint64]*AuxiliaryInfoBatch{
				4: {
					data: []common.AuxiliaryInfo{{Version: 7, Data: []byte("a")}},
				},
			},
			expected: AuxInfoHistory{
				Data:            [][]byte{[]byte("a")},
				LastSeq:         4,
				OldestVersionID: 7,
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			blocks := make(map[uint64]StateMachineBlock, len(tt.prevBatches))
			for seq, batch := range tt.prevBatches {
				blocks[seq] = batchBlock(batch)
			}

			startBlock := batchBlock(tt.startBatch)
			history, err := GetAuxiliaryHistory(&startBlock, startSeq, blockRetrieverFromMap(t, blocks), defaultVersionID)
			require.NoError(t, err)
			require.Equal(t, tt.expected, history)
		})
	}
}

func TestGetAuxiliaryHistoryRetrievalError(t *testing.T) {
	startBlock := batchBlock(&AuxiliaryInfoBatch{
		data:           []common.AuxiliaryInfo{{Version: 1, Data: []byte("a")}},
		PrevAuxInfoSeq: 5,
	})

	getBlock := func(seq uint64, _ common.Digest) (StateMachineBlock, *common.Finalization, error) {
		return StateMachineBlock{}, nil, fmt.Errorf("no block at seq %d", seq)
	}

	_, err := GetAuxiliaryHistory(&startBlock, 10, getBlock, 0)
	require.ErrorIs(t, err, errAuxInfoBlockRetrieval)
}

type sentAuxInfo struct {
	from avalanchego.NodeID
	info common.AuxiliaryInfo
}

func TestCollectAuxInfo(t *testing.T) {
	node1 := avalanchego.NodeID{1}
	node2 := avalanchego.NodeID{2}
	node3 := avalanchego.NodeID{3}

	// voteCountingAuxInfoApp rejects appends whose data is already in the history.
	history := AuxInfoHistory{
		Data:            [][]byte{[]byte("a")},
		OldestVersionID: 1,
	}

	for _, tt := range []struct {
		name     string
		sends    []sentAuxInfo
		expected []common.AuxiliaryInfo
	}{
		{
			name:     "empty store",
			sends:    nil,
			expected: nil,
		},
		{
			name: "legal entries returned sorted by node id",
			sends: []sentAuxInfo{
				{from: node3, info: common.AuxiliaryInfo{Version: 1, Data: []byte("d")}},
				{from: node1, info: common.AuxiliaryInfo{Version: 1, Data: []byte("b")}},
				{from: node2, info: common.AuxiliaryInfo{Version: 1, Data: []byte("c")}},
			},
			expected: []common.AuxiliaryInfo{
				{Version: 1, Data: []byte("b")},
				{Version: 1, Data: []byte("c")},
				{Version: 1, Data: []byte("d")},
			},
		},
		{
			name: "version mismatch filtered",
			sends: []sentAuxInfo{
				{from: node1, info: common.AuxiliaryInfo{Version: 2, Data: []byte("b")}},
			},
			expected: nil,
		},
		{
			name: "inconsistent versions only keep entries matching the history version",
			sends: []sentAuxInfo{
				{from: node1, info: common.AuxiliaryInfo{Version: 2, Data: []byte("b")}},
				{from: node2, info: common.AuxiliaryInfo{Version: 1, Data: []byte("c")}},
				{from: node3, info: common.AuxiliaryInfo{Version: 3, Data: []byte("d")}},
			},
			expected: []common.AuxiliaryInfo{
				{Version: 1, Data: []byte("c")},
			},
		},
		{
			name: "entries already in history filtered",
			sends: []sentAuxInfo{
				{from: node1, info: common.AuxiliaryInfo{Version: 1, Data: []byte("a")}},
			},
			expected: nil,
		},
		{
			name: "accepted entries extend the history for later entries",
			sends: []sentAuxInfo{
				{from: node1, info: common.AuxiliaryInfo{Version: 1, Data: []byte("b")}},
				{from: node2, info: common.AuxiliaryInfo{Version: 1, Data: []byte("b")}},
				{from: node3, info: common.AuxiliaryInfo{Version: 1, Data: []byte("c")}},
			},
			expected: []common.AuxiliaryInfo{
				{Version: 1, Data: []byte("b")},
				{Version: 1, Data: []byte("c")},
			},
		},
		{
			name: "latest info from a node wins",
			sends: []sentAuxInfo{
				{from: node1, info: common.AuxiliaryInfo{Version: 1, Data: []byte("b")}},
				{from: node1, info: common.AuxiliaryInfo{Version: 1, Data: []byte("c")}},
			},
			expected: []common.AuxiliaryInfo{
				{Version: 1, Data: []byte("c")},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := newAuxInfoStore(&voteCountingAuxInfoApp{}, noopLogger{})
			for _, send := range tt.sends {
				store.HandleAuxiliaryMessage(send.info, send.from)
			}

			require.Equal(t, tt.expected, store.collectAuxInfo(history, nil))
		})
	}
}

func TestCollectAuxInfoKeepsRejectedEntries(t *testing.T) {
	store := newAuxInfoStore(&voteCountingAuxInfoApp{}, noopLogger{})
	info := common.AuxiliaryInfo{Version: 1, Data: []byte("a")}
	store.HandleAuxiliaryMessage(info, avalanchego.NodeID{1})

	// the entry duplicates the history, so it is rejected but kept in the store
	history := AuxInfoHistory{
		Data:            [][]byte{[]byte("a")},
		OldestVersionID: 1,
	}
	require.Empty(t, store.collectAuxInfo(history, nil))

	// with a history that no longer contains the entry, it becomes legal
	require.Equal(t, []common.AuxiliaryInfo{info}, store.collectAuxInfo(AuxInfoHistory{OldestVersionID: 1}, nil))
}
