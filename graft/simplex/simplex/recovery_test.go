// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex_test

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	. "github.com/ava-labs/simplex/common"
	. "github.com/ava-labs/simplex/simplex"
	"github.com/ava-labs/simplex/testutil"
	"github.com/stretchr/testify/require"
)

// TestRecoverFromWALProposed tests that the epoch can recover from
// a wal with a single block record written to it(that we have proposed).
func TestRecoverFromWALProposed(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	ctx := context.Background()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	quorum := Quorum(len(nodes))

	conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	protocolMetadata := e.Metadata()
	firstBlock, ok := bb.BuildBlock(ctx, protocolMetadata, emptyBlacklist)
	require.True(t, ok)
	fBytes := firstBlock.Bytes()
	record, err := BlockRecord(firstBlock.BlockHeader(), fBytes)
	require.NoError(t, err)

	// write block record to wal
	require.NoError(t, wal.Append(record))

	records, err := wal.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, record, records[0])

	err = e.Start()
	require.NoError(t, err)

	rounds := uint64(100)
	for i := uint64(0); i < rounds; i++ {
		leader := LeaderForRound(nodes, uint64(i))
		isEpochNode := leader.Equals(e.ID)
		if !isEpochNode {
			md := e.Metadata()
			_, ok := bb.BuildBlock(context.Background(), md, emptyBlacklist)
			require.True(t, ok)
			require.NotEqual(t, 0, rounds)
		}

		block := bb.GetBuiltBlock()
		if rounds == 0 {
			require.Equal(t, firstBlock, block)
		}

		if !isEpochNode {
			// send node a message from the leader
			vote, err := testutil.NewTestVote(block, leader)
			require.NoError(t, err)
			err = e.HandleMessage(&Message{
				BlockMessage: &BlockMessage{
					Vote:  *vote,
					Block: block,
				},
			}, leader)
			require.NoError(t, err)
		}

		// start at one since our node has already voted
		for i := 1; i < quorum; i++ {
			testutil.InjectTestVote(t, e, block, nodes[i])
		}

		for i := 1; i < quorum; i++ {
			testutil.InjectTestFinalizeVote(t, e, block, nodes[i])
		}

		block2 := storage.WaitForBlockCommit(i)

		require.Equal(t, block.BlockHeader().Digest, block2.BlockHeader().Digest)
	}

	require.Equal(t, rounds, e.Storage.NumBlocks())
}

// TestRecoverFromWALNotarized tests that the epoch can recover from a wal
// with a block record written to it, and a notarization record.
func TestRecoverFromNotarization(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	ctx := context.Background()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	quorum := Quorum(len(nodes))
	sigAggregrator := &testutil.TestSignatureAggregator{N: 4}
	conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	protocolMetadata := e.Metadata()
	block, ok := bb.BuildBlock(ctx, protocolMetadata, emptyBlacklist)
	require.True(t, ok)
	bBytes := block.Bytes()
	blockRecord, err := BlockRecord(block.BlockHeader(), bBytes)
	require.NoError(t, err)

	// write block blockRecord to wal
	require.NoError(t, wal.Append(blockRecord))

	// lets add some notarizations
	notarizationRecord, err := testutil.NewNotarizationRecord(conf.Logger, sigAggregrator, block, nodes[0:quorum])
	require.NoError(t, err)

	// when we start this we should kickoff the finalization process by broadcasting a finalization message and then waiting for incoming finalization messages
	require.NoError(t, wal.Append(notarizationRecord))

	records, err := wal.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, blockRecord, records[0])
	require.Equal(t, notarizationRecord, records[1])

	require.Equal(t, uint64(0), e.Metadata().Round)
	err = e.Start()
	require.NoError(t, err)

	// require the round was incremented(notarization increases round)
	require.Equal(t, uint64(1), e.Metadata().Round)
	for i := 1; i < quorum; i++ {
		testutil.InjectTestFinalizeVote(t, e, block, nodes[i])
	}

	firstBlock, _, err := storage.Retrieve(0)
	require.NoError(t, err)
	committedData := firstBlock.Bytes()
	require.Equal(t, bBytes, committedData)
	require.Equal(t, uint64(1), e.Storage.NumBlocks())
}

// TestRecoverFromWALFinalized tests that the epoch can recover from a wal
// with a block already stored in the storage
func TestRecoverFromWalWithStorage(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	ctx := context.Background()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	quorum := Quorum(len(nodes))
	sigAggregrator := &testutil.TestSignatureAggregator{N: 4}
	conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)

	indexedBlock := testutil.NewTestBlock(ProtocolMetadata{Seq: 0, Round: 0, Epoch: 0}, emptyBlacklist)
	indexedFinalization, _ := testutil.NewFinalizationRecord(t, sigAggregrator, indexedBlock, nodes[0:quorum])
	require.NoError(t, storage.Index(ctx, indexedBlock, indexedFinalization))

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.Equal(t, uint64(1), e.Metadata().Round)

	protocolMetadata := e.Metadata()
	block, ok := bb.BuildBlock(ctx, protocolMetadata, emptyBlacklist)
	require.True(t, ok)
	bBytes := block.Bytes()
	record, err := BlockRecord(block.BlockHeader(), bBytes)
	require.NoError(t, err)

	// write block record to wal
	require.NoError(t, wal.Append(record))

	// lets add some notarizations
	notarizationRecord, err := testutil.NewNotarizationRecord(conf.Logger, sigAggregrator, block, nodes[0:quorum])
	require.NoError(t, err)

	require.NoError(t, wal.Append(notarizationRecord))

	records, err := wal.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, record, records[0])
	require.Equal(t, notarizationRecord, records[1])
	_, vote, err := ParseNotarizationRecord(records[1])
	require.NoError(t, err)
	require.Equal(t, uint64(1), vote.Round)

	err = e.Start()
	require.NoError(t, err)

	// require the round was incremented(notarization increases round)
	require.Equal(t, uint64(2), e.Metadata().Round)

	for i := 1; i < quorum; i++ {
		// type assert block to testBlock
		testutil.InjectTestFinalizeVote(t, e, block, nodes[i])
	}

	secondBlock, _, err := storage.Retrieve(1)
	require.NoError(t, err)
	committedData := secondBlock.Bytes()
	bBytes = block.Bytes()
	require.Equal(t, bBytes, committedData)
	require.Equal(t, uint64(2), e.Storage.NumBlocks())
}

// TestWalCreated tests that the epoch correctly writes to the WAL
func TestWalCreatedProperly(t *testing.T) {
	ctx := context.Background()
	bb := testutil.NewTestBlockBuilder()

	nodes := []NodeID{{1}, {2}, {3}, {4}}
	quorum := Quorum(len(nodes))
	conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	// ensure no records are written to the WAL
	records, err := e.WAL.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 0)

	require.NoError(t, e.Start())

	// ensure a block record is written to the WAL
	wal.AssertWALSize(1)
	records, err = e.WAL.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 1)
	blockFromWal, err := BlockFromRecord(ctx, conf.BlockDeserializer, records[0])
	require.NoError(t, err)
	block := bb.GetBuiltBlock()
	require.Equal(t, blockFromWal.BlockHeader().Digest, block.BlockHeader().Digest)

	// start at one since our node has already voted
	for i := 1; i < quorum; i++ {
		testutil.InjectTestVote(t, e, block, nodes[i])
	}

	records, err = e.WAL.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 2)
	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	expectedNotarizationRecord, err := testutil.NewNotarizationRecord(conf.Logger, sigAggr, block, nodes[0:quorum])
	require.NoError(t, err)
	require.Equal(t, expectedNotarizationRecord, records[1])

	for i := 1; i < quorum; i++ {
		testutil.InjectTestFinalizeVote(t, e, block, nodes[i])
	}

	// we do not append the finalization record to the WAL if it for the next expected sequence
	records, err = e.WAL.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 2)

	blockRetrieved, _, err := storage.Retrieve(0)
	require.NoError(t, err)
	committedData := blockRetrieved.Bytes()
	bBytes := block.Bytes()
	require.Equal(t, bBytes, committedData)
}

// TestWalWritesBlockRecord tests that the epoch correctly writes to the WAL
// a block proposed by a node other than the epoch node
func TestWalWritesBlockRecord(t *testing.T) {
	ctx := context.Background()
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	// nodes[1] is not the leader for the first round
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[1], testutil.NewNoopComm(nodes), bb)

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	// ensure no records are written to the WAL
	records, err := e.WAL.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 0)

	require.NoError(t, e.Start())
	// ensure no records are written to the WAL
	records, err = e.WAL.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 0)

	md := e.Metadata()
	_, ok := bb.BuildBlock(context.Background(), md, emptyBlacklist)
	require.True(t, ok)

	block := bb.GetBuiltBlock()
	// send epoch node this block
	vote, err := testutil.NewTestVote(block, nodes[0])
	require.NoError(t, err)
	err = e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{
			Vote:  *vote,
			Block: block,
		},
	}, nodes[0])
	require.NoError(t, err)

	// ensure a block record is written to the WAL
	wal.AssertWALSize(1)
	records, err = e.WAL.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 1)
	blockFromWal, err := BlockFromRecord(ctx, conf.BlockDeserializer, records[0])
	require.NoError(t, err)
	require.Equal(t, block.BlockHeader().Digest, blockFromWal.BlockHeader().Digest)
}

func TestWalWritesFinalization(t *testing.T) {
	ctx := context.Background()
	bb := testutil.NewTestBlockBuilder()
	sigAggregrator := &testutil.TestSignatureAggregator{N: 4}
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	quorum := Quorum(len(nodes))
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())
	firstBlock := bb.GetBuiltBlock()
	// notarize the first block
	for i := 1; i < quorum; i++ {
		testutil.InjectTestVote(t, e, firstBlock, nodes[i])
	}

	wal.AssertWALSize(2)

	records, err := e.WAL.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 2)
	blockFromWal, err := BlockFromRecord(ctx, conf.BlockDeserializer, records[0])
	require.NoError(t, err)

	require.Equal(t, firstBlock.BlockHeader().Digest, blockFromWal.BlockHeader().Digest)
	expectedNotarizationRecord, err := testutil.NewNotarizationRecord(conf.Logger, sigAggregrator, firstBlock, nodes[0:quorum])
	require.NoError(t, err)
	require.Equal(t, expectedNotarizationRecord, records[1])

	// send and notarize a second block
	require.Equal(t, uint64(1), e.Metadata().Round)
	md := e.Metadata()
	md.Prev = firstBlock.BlockHeader().Digest
	_, ok := bb.BuildBlock(context.Background(), md, emptyBlacklist)
	require.True(t, ok)
	secondBlock := bb.GetBuiltBlock()

	// increase the round but don't index storage
	require.Equal(t, uint64(1), e.Metadata().Round)
	require.Equal(t, uint64(0), e.Storage.NumBlocks())

	vote, err := testutil.NewTestVote(secondBlock, nodes[1])
	require.NoError(t, err)
	err = e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{
			Vote:  *vote,
			Block: secondBlock,
		},
	}, nodes[1])
	require.NoError(t, err)

	for i := 1; i < quorum; i++ {
		testutil.InjectTestVote(t, e, secondBlock, nodes[i])
	}

	wal.AssertWALSize(4)

	records, err = e.WAL.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 4)
	blockFromWal, err = BlockFromRecord(ctx, conf.BlockDeserializer, records[2])
	require.NoError(t, err)
	require.Equal(t, secondBlock.BlockHeader().Digest, blockFromWal.BlockHeader().Digest)
	expectedNotarizationRecord, err = testutil.NewNotarizationRecord(conf.Logger, sigAggregrator, secondBlock, nodes[0:quorum])
	require.NoError(t, err)
	require.Equal(t, expectedNotarizationRecord, records[3])

	// finalization for the second block should write to wal
	for i := 1; i < quorum; i++ {
		testutil.InjectTestFinalizeVote(t, e, secondBlock, nodes[i])
	}

	records, err = e.WAL.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 5)
	recordType := binary.BigEndian.Uint16(records[4])
	require.Equal(t, FinalizationRecordType, recordType)
	_, err = FinalizationFromRecord(records[4], e.QCDeserializer)
	_, expectedFinalizationRecord := testutil.NewFinalizationRecord(t, sigAggregrator, secondBlock, nodes[0:quorum])
	require.NoError(t, err)
	require.Equal(t, expectedFinalizationRecord, records[4])

	// ensure the finalization is not indexed
	require.Equal(t, uint64(2), e.Metadata().Round)
	require.Equal(t, uint64(0), e.Storage.NumBlocks())
}

// Appends to the wal -> block, notarization, second block, notarization block 2, finalization for block 2.
func TestRecoverFromMultipleNotarizations(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	ctx := context.Background()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	quorum := Quorum(len(nodes))
	conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)

	// Create first block and write to WAL
	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())

	protocolMetadata := e.Metadata()
	firstBlock, ok := bb.BuildBlock(ctx, protocolMetadata, emptyBlacklist)
	require.True(t, ok)

	fBytes := firstBlock.Bytes()
	record, err := BlockRecord(firstBlock.BlockHeader(), fBytes)
	require.NoError(t, err)

	require.NoError(t, wal.Append(record))

	firstNotarizationRecord, err := testutil.NewNotarizationRecord(conf.Logger, sigAggr, firstBlock, nodes[0:quorum])
	require.NoError(t, err)
	require.NoError(t, wal.Append(firstNotarizationRecord))

	protocolMetadata.Round = 1
	protocolMetadata.Seq = 1
	protocolMetadata.Prev = firstBlock.BlockHeader().Digest
	secondBlock, ok := bb.BuildBlock(ctx, protocolMetadata, emptyBlacklist)
	require.True(t, ok)
	sBytes := secondBlock.Bytes()
	record, err = BlockRecord(secondBlock.BlockHeader(), sBytes)
	require.NoError(t, err)

	require.NoError(t, wal.Append(record))

	// Add notarization for second block
	secondNotarizationRecord, err := testutil.NewNotarizationRecord(conf.Logger, sigAggr, secondBlock, nodes[0:quorum])
	require.NoError(t, err)
	require.NoError(t, wal.Append(secondNotarizationRecord))

	// Create finalization record for second block
	finalization2, finalizationRecord := testutil.NewFinalizationRecord(t, sigAggr, secondBlock, nodes[0:quorum])
	require.NoError(t, wal.Append(finalizationRecord))

	err = e.Start()
	require.NoError(t, err)

	require.Equal(t, uint64(2), e.Metadata().Round)
	require.Equal(t, uint64(0), e.Storage.NumBlocks())

	// now if we send finalization for block 1, we should index both 1 & 2
	finalization1, _ := testutil.NewFinalizationRecord(t, sigAggr, firstBlock, nodes[0:quorum])
	err = e.HandleMessage(&Message{
		Finalization: &finalization1,
	}, nodes[1])
	require.NoError(t, err)

	require.Equal(t, uint64(2), e.Storage.NumBlocks())
	firstBlockRetrieved, finalizationRetrieved1, err := storage.Retrieve(0)
	require.NoError(t, err)
	storageBytes := firstBlockRetrieved.Bytes()
	require.Equal(t, fBytes, storageBytes)

	secondBlockRetrieved, finalizationRetrieved2, err := storage.Retrieve(1)
	require.NoError(t, err)
	storageBytes = secondBlockRetrieved.Bytes()
	require.Equal(t, sBytes, storageBytes)
	require.True(t, finalization1.Finalization.Equals(&finalizationRetrieved1.Finalization.BlockHeader))
	require.Equal(t, finalization1.QC, finalizationRetrieved1.QC)
	require.True(t, finalization2.Finalization.Equals(&finalizationRetrieved2.Finalization.BlockHeader))
	require.Equal(t, finalization2.QC, finalizationRetrieved2.QC)
}

// TestRecoveryBlocksIndexed tests that the epoch properly skips
// block records that are already indexed in the storage.
func TestRecoveryBlocksIndexed(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	ctx := context.Background()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	quorum := Quorum(len(nodes))
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)

	protocolMetadata := ProtocolMetadata{Seq: 0, Round: 0, Epoch: 0}
	firstBlock, ok := bb.BuildBlock(ctx, protocolMetadata, emptyBlacklist)
	fBytes := firstBlock.Bytes()
	require.True(t, ok)
	record, err := BlockRecord(firstBlock.BlockHeader(), fBytes)
	require.NoError(t, err)
	require.NoError(t, wal.Append(record))

	sigAggr := conf.SignatureAggregatorCreator(conf.Comm.Validators())
	firstNotarizationRecord, err := testutil.NewNotarizationRecord(conf.Logger, sigAggr, firstBlock, nodes[0:quorum])
	require.NoError(t, err)
	require.NoError(t, wal.Append(firstNotarizationRecord))

	_, finalizationBytes := testutil.NewFinalizationRecord(t, sigAggr, firstBlock, nodes[0:quorum])
	require.NoError(t, wal.Append(finalizationBytes))

	protocolMetadata.Round = 1
	protocolMetadata.Seq = 1
	secondBlock, ok := bb.BuildBlock(ctx, protocolMetadata, emptyBlacklist)
	require.True(t, ok)
	sBytes := secondBlock.Bytes()
	record, err = BlockRecord(secondBlock.BlockHeader(), sBytes)
	require.NoError(t, err)
	require.NoError(t, wal.Append(record))

	protocolMetadata.Round = 2
	protocolMetadata.Seq = 2
	thirdBlock, ok := bb.BuildBlock(ctx, protocolMetadata, emptyBlacklist)
	require.True(t, ok)
	tBytes := thirdBlock.Bytes()
	record, err = BlockRecord(thirdBlock.BlockHeader(), tBytes)
	require.NoError(t, err)

	require.NoError(t, wal.Append(record))

	finalization1, _ := testutil.NewFinalizationRecord(t, sigAggr, firstBlock, nodes[0:quorum])
	finalization2, _ := testutil.NewFinalizationRecord(t, sigAggr, secondBlock, nodes[0:quorum])
	fCer3, _ := testutil.NewFinalizationRecord(t, sigAggr, thirdBlock, nodes[0:quorum])

	require.NoError(t, conf.Storage.Index(ctx, firstBlock, finalization1))
	require.NoError(t, conf.Storage.Index(ctx, secondBlock, finalization2))
	require.NoError(t, conf.Storage.Index(ctx, thirdBlock, fCer3))

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.Equal(t, uint64(3), e.Storage.NumBlocks())
	require.NoError(t, e.Start())

	// ensure the round is properly set to 3
	require.Equal(t, uint64(3), e.Metadata().Round)
	require.Equal(t, uint64(3), e.Metadata().Seq)
	require.Equal(t, thirdBlock.BlockHeader().Digest, e.Metadata().Prev)
}

// TestEpochCorrectlyInitializesMetadataFromStorage asserts the next block's metadata is
// derived from the last indexed block.
func TestEpochCorrectlyInitializesMetadataFromStorage(t *testing.T) {
	ctx := context.Background()
	nodes := []NodeID{{1}, {2}, {3}, {4}}

	tests := []struct {
		name          string
		storage       func(t *testing.T) Storage
		expectedRound uint64
		expectedSeq   uint64
		expectedEpoch uint64
	}{
		{
			name: "normal block",
			storage: func(t *testing.T) Storage {
				storage := testutil.NewInMemStorage()
				for _, block := range createBlocks(t, nodes, 2) {
					require.NoError(t, storage.Index(ctx, block.VerifiedBlock, block.Finalization))
				}

				return storage
			},
			expectedRound: 2,
			expectedSeq:   2,
			expectedEpoch: 0,
		},
		{
			name: "sealing block",
			storage: func(t *testing.T) Storage {
				storage := testutil.NewInMemStorage()
				blocks := createBlocks(t, nodes, 8)
				blocks[7].VerifiedBlock.(*testutil.TestBlock).SealingInfo = &SealingBlockInfo{
					ValidatorSet:         NodeIDs(nodes).EqualWeightedNodes(),
					PrevSealingBlockHash: blocks[0].VerifiedBlock.BlockHeader().Digest,
				}
				for _, block := range blocks {
					require.NoError(t, storage.Index(ctx, block.VerifiedBlock, block.Finalization))
				}

				return storage
			},
			expectedRound: 8,
			expectedSeq:   8,
			// The epoch is the sequence of the last indexed sealing block.
			expectedEpoch: 7,
		},
		{
			// Blocks predating Simplex carry no finalization, so the round and epoch
			// come from the number of indexed blocks.
			name: "non simplex blocks",
			storage: func(t *testing.T) Storage {
				storage := testutil.NewInMemStorage()
				for i, block := range createBlocks(t, nodes, 2) {
					block.VerifiedBlock.(*testutil.TestBlock).Metadata = ProtocolMetadata{
						Seq: uint64(i), // set the sequence so we can index without error
					}
					require.NoError(t, storage.Index(ctx, block.VerifiedBlock, Finalization{}))
				}

				return storage
			},
			expectedRound: 1,
			expectedSeq:   2,
			expectedEpoch: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bb := testutil.NewTestBlockBuilder()
			conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)
			conf.Storage = tt.storage(t)

			e, err := NewEpoch(conf)
			require.NoError(t, err)
			t.Cleanup(e.Stop)
			require.NoError(t, e.Start())

			tip, err := RetrieveLastIndexFromStorage(e.Storage)
			require.NoError(t, err)

			md := e.Metadata()
			require.Equal(t, tt.expectedEpoch, md.Epoch)
			require.Equal(t, tt.expectedRound, md.Round)
			require.Equal(t, tt.expectedSeq, md.Seq)
			require.Equal(t, tip.VerifiedBlock.BlockHeader().Digest, md.Prev)
		})
	}
}

func TestRecoveryAsLeader(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	ctx := context.Background()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	finalizedBlocks := createBlocks(t, nodes, 4)
	conf, _, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)

	for _, finalizedBlock := range finalizedBlocks {
		err := storage.Index(ctx, finalizedBlock.VerifiedBlock, finalizedBlock.Finalization)
		require.NoError(t, err)
	}

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.Equal(t, uint64(4), e.Storage.NumBlocks())
	require.NoError(t, e.Start())

	bb.GetBuiltBlock()

	// wait for the block to finish verifying
	time.Sleep(50 * time.Millisecond)

	// ensure the round is properly set
	require.Equal(t, uint64(4), e.Metadata().Round)
	require.Equal(t, uint64(4), e.Metadata().Seq)
}

func TestRecoveryReVerifiesBlocks(t *testing.T) {
	ctx := context.Background()
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	finalizedBlocks := createBlocks(t, nodes, 4)

	deserializer := &testutil.BlockDeserializer{
		DelayedVerification: make(chan struct{}, 1),
	}
	conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)
	for _, finalizedBlock := range finalizedBlocks {
		err := storage.Index(ctx, finalizedBlock.VerifiedBlock, finalizedBlock.Finalization)
		require.NoError(t, err)
	}
	conf.BlockDeserializer = deserializer

	// Create first block and write to WAL
	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	protocolMetadata := e.Metadata()
	firstBlock, ok := bb.BuildBlock(ctx, protocolMetadata, emptyBlacklist)
	require.True(t, ok)
	fBytes := firstBlock.Bytes()
	record, err := BlockRecord(firstBlock.BlockHeader(), fBytes)
	require.NoError(t, err)

	require.NoError(t, wal.Append(record))

	deserializer.DelayedVerification <- struct{}{}
	require.NoError(t, e.Start())
	require.Len(t, deserializer.DelayedVerification, 0)
}

// TestWalRecoveryTriggersEmptyVoteTimeout tests that when recovering from a wal
// we properly broadcast an EmptyVote and create a timeout to rebroadcast it.
// Without this timeout, other nodes may be stuck waiting for an empty notarization that
// can only be completed if they receive our empty vote.
func TestWalRecoveryTriggersEmptyVoteTimeout(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	ctx := context.Background()
	quorum := Quorum(4)
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	initialBlock := createBlocks(t, nodes, 1)[0]
	recordingComm := &recordingComm{Communication: testutil.NewNoopComm(nodes), BroadcastMessages: make(chan *Message, 100), SentMessages: make(chan *Message, 100)}
	conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[0], recordingComm, bb)
	require.NoError(t, storage.Index(ctx, initialBlock.VerifiedBlock, initialBlock.Finalization))

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	protocolMetadata := e.Metadata()
	block, ok := bb.BuildBlock(ctx, protocolMetadata, emptyBlacklist)
	require.True(t, ok)
	bBytes := block.Bytes()
	blockRecord, err := BlockRecord(block.BlockHeader(), bBytes)
	require.NoError(t, err)

	// write block blockRecord to wal
	require.NoError(t, wal.Append(blockRecord))

	// lets add some notarizations
	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	notarizationRecord, err := testutil.NewNotarizationRecord(conf.Logger, sigAggr, block, nodes[0:quorum])
	require.NoError(t, err)

	require.NoError(t, wal.Append(notarizationRecord))

	// In this test say Node ID 3 crashed(it is the block proposer).
	// If our node cannot properly resume from the wal and trigger empty vote timeouts, the other
	// two node will be stuck waiting for an empty notarization.
	emptyVote := createEmptyVote(EmptyVoteMetadata{
		Round: protocolMetadata.Round + 1,
		Epoch: protocolMetadata.Epoch,
	}, e.ID).Vote

	emptyVoteRecord := NewEmptyVoteRecord(emptyVote)

	require.NoError(t, wal.Append(emptyVoteRecord))

	require.NoError(t, e.Start())
	require.Equal(t, uint64(2), e.Metadata().Seq)

	count := 0
	ticker := time.NewTicker(100 * time.Millisecond)
	time := time.Now()
	for count < 3 {
		select {
		case <-ticker.C:
			time = time.Add(DefaultEmptyVoteRebroadcastTimeout)
			e.AdvanceTime(time)
		case msg := <-recordingComm.BroadcastMessages:
			if msg.EmptyVoteMessage != nil {
				require.Equal(t, msg.EmptyVoteMessage.Vote, emptyVote)
				count++
			}
		}
	}
}

// TestWalRecoveryMonitorsProgress tests that when recovering from a wal
// we properly create a timeout to rebroadcast empty votes. This timeout will be
// created via a call to monitorProgress() since our most relevant record in the wal is a block record.
func TestWalRecoveryMonitorsProgress(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	ctx := context.Background()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	initialBlock := createBlocks(t, nodes, 1)[0]
	recordingComm := &recordingComm{Communication: testutil.NewNoopComm(nodes), BroadcastMessages: make(chan *Message, 100), SentMessages: make(chan *Message, 100)}
	conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[0], recordingComm, bb)
	require.NoError(t, storage.Index(ctx, initialBlock.VerifiedBlock, initialBlock.Finalization))

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	protocolMetadata := e.Metadata()
	block, ok := bb.BuildBlock(ctx, protocolMetadata, emptyBlacklist)
	require.True(t, ok)
	bBytes := block.Bytes()
	blockRecord, err := BlockRecord(block.BlockHeader(), bBytes)
	require.NoError(t, err)

	// write block blockRecord to wal
	require.NoError(t, wal.Append(blockRecord))

	require.NoError(t, e.Start())
	require.Equal(t, uint64(1), e.Metadata().Round)
	require.Equal(t, uint64(1), e.Metadata().Seq)

	count := 0
	ticker := time.NewTicker(100 * time.Millisecond)
	time := time.Now()
	bb.BlockShouldBeBuilt <- struct{}{}
	for count < 3 {
		select {
		case <-ticker.C:
			time = time.Add(DefaultEmptyVoteRebroadcastTimeout)
			e.AdvanceTime(time)
		case msg := <-recordingComm.BroadcastMessages:
			if msg.EmptyVoteMessage != nil {
				require.Equal(t, uint64(1), msg.EmptyVoteMessage.Vote.Round)
				count++
			}
		}
	}
}

func TestWalRecoverySetsRoundCorrectly(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	ctx := context.Background()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	quorum := Quorum(len(nodes))

	tests := []struct {
		name          string
		setupRecords  func(t *testing.T, bb *testutil.TestBlockBuilder, conf EpochConfig) [][]byte
		expectedRound uint64
		expectedEpoch uint64
	}{
		{
			name: "single notarization round 0",
			setupRecords: func(t *testing.T, bb *testutil.TestBlockBuilder, conf EpochConfig) [][]byte {
				block, ok := bb.BuildBlock(ctx, ProtocolMetadata{Round: 0, Epoch: 0}, emptyBlacklist)
				require.True(t, ok)
				bBytes := block.Bytes()
				blockRecord, err := BlockRecord(block.BlockHeader(), bBytes)
				require.NoError(t, err)

				notarizationRecord, err := testutil.NewNotarizationRecord(conf.Logger, conf.SignatureAggregatorCreator(conf.Comm.Validators()), block, nodes[0:quorum])
				require.NoError(t, err)

				return [][]byte{blockRecord, notarizationRecord}
			},
			expectedRound: 1,
			expectedEpoch: 0,
		},
		{
			name: "finalization round 1, empty notarization round 0 (out of order)",
			setupRecords: func(t *testing.T, bb *testutil.TestBlockBuilder, conf EpochConfig) [][]byte {
				// Create block for round 1
				block1, ok := bb.BuildBlock(ctx, ProtocolMetadata{Round: 1, Epoch: 0, Seq: 1}, emptyBlacklist)
				require.True(t, ok)

				bBytes1 := block1.Bytes()
				blockRecord1, err := BlockRecord(block1.BlockHeader(), bBytes1)
				require.NoError(t, err)

				_, finalizationRecord1 := testutil.NewFinalizationRecord(t, conf.SignatureAggregatorCreator(conf.Comm.Validators()), block1, nodes[0:quorum])

				// Create empty notarization for round 0
				emptyNotarization0 := testutil.NewEmptyNotarization(nodes[0:quorum], 0)
				emptyNotarizationRecord0 := NewEmptyNotarizationRecord(emptyNotarization0)

				// Return out of order: finalization for round 1, then empty notarization for round 0
				return [][]byte{blockRecord1, finalizationRecord1, emptyNotarizationRecord0}
			},
			expectedRound: 2, // finalization round 1 + 1
			expectedEpoch: 0,
		},
		{
			name: "notarization rounds 0,1,2, empty notarization round 3 (out of order)",
			setupRecords: func(t *testing.T, bb *testutil.TestBlockBuilder, conf EpochConfig) [][]byte {
				// Create blocks and notarizations for rounds 0, 1, 2
				block0, ok := bb.BuildBlock(ctx, ProtocolMetadata{Round: 0, Epoch: 0, Seq: 0}, emptyBlacklist)
				require.True(t, ok)
				bBytes0 := block0.Bytes()
				blockRecord0, err := BlockRecord(block0.BlockHeader(), bBytes0)
				require.NoError(t, err)
				notarizationRecord0, err := testutil.NewNotarizationRecord(conf.Logger, conf.SignatureAggregatorCreator(conf.Comm.Validators()), block0, nodes[0:quorum])
				require.NoError(t, err)

				block1, ok := bb.BuildBlock(ctx, ProtocolMetadata{Round: 1, Epoch: 0, Seq: 1}, emptyBlacklist)
				require.True(t, ok)
				bBytes1 := block1.Bytes()
				require.NoError(t, err)
				blockRecord1, err := BlockRecord(block1.BlockHeader(), bBytes1)
				require.NoError(t, err)
				notarizationRecord1, err := testutil.NewNotarizationRecord(conf.Logger, conf.SignatureAggregatorCreator(conf.Comm.Validators()), block1, nodes[0:quorum])
				require.NoError(t, err)

				block2, ok := bb.BuildBlock(ctx, ProtocolMetadata{Round: 2, Epoch: 0, Seq: 2}, emptyBlacklist)
				require.True(t, ok)
				bBytes2 := block2.Bytes()
				require.NoError(t, err)
				blockRecord2, err := BlockRecord(block2.BlockHeader(), bBytes2)
				require.NoError(t, err)
				notarizationRecord2, err := testutil.NewNotarizationRecord(conf.Logger, conf.SignatureAggregatorCreator(conf.Comm.Validators()), block2, nodes[0:quorum])
				require.NoError(t, err)

				// Create empty notarization for round 3
				emptyNotarization3 := testutil.NewEmptyNotarization(nodes[0:quorum], 3)
				emptyNotarizationRecord3 := NewEmptyNotarizationRecord(emptyNotarization3)

				// Return out of order: notarization 0, notarization 1, empty notarization 3, notarization 2
				return [][]byte{
					blockRecord0, notarizationRecord0,
					blockRecord1, notarizationRecord1,
					emptyNotarizationRecord3,
					blockRecord2, notarizationRecord2,
				}
			},
			expectedRound: 4, // empty notarization round 3 + 1
			expectedEpoch: 0,
		},
		{
			name: "mixed types in reverse order",
			setupRecords: func(t *testing.T, bb *testutil.TestBlockBuilder, conf EpochConfig) [][]byte {
				// Create finalization for round 3
				block3, ok := bb.BuildBlock(ctx, ProtocolMetadata{Round: 3, Epoch: 1, Seq: 3}, emptyBlacklist)
				require.True(t, ok)
				bBytes3 := block3.Bytes()
				blockRecord3, err := BlockRecord(block3.BlockHeader(), bBytes3)
				require.NoError(t, err)
				_, finalizationRecord3 := testutil.NewFinalizationRecord(t, conf.SignatureAggregatorCreator(conf.Comm.Validators()), block3, nodes[0:quorum])

				// Create empty notarization for round 2
				emptyNotarization2 := testutil.NewEmptyNotarization(nodes[0:quorum], 2)
				emptyNotarization2.Vote.Epoch = 1
				emptyNotarizationRecord2 := NewEmptyNotarizationRecord(emptyNotarization2)

				// Create notarization for round 1
				block1, ok := bb.BuildBlock(ctx, ProtocolMetadata{Round: 1, Epoch: 1, Seq: 1}, emptyBlacklist)
				require.True(t, ok)
				bBytes1 := block1.Bytes()
				blockRecord1, err := BlockRecord(block1.BlockHeader(), bBytes1)
				require.NoError(t, err)
				notarizationRecord1, err := testutil.NewNotarizationRecord(conf.Logger, conf.SignatureAggregatorCreator(conf.Comm.Validators()), block1, nodes[0:quorum])
				require.NoError(t, err)

				// Return in reverse order
				return [][]byte{
					blockRecord3, finalizationRecord3,
					emptyNotarizationRecord2,
					blockRecord1, notarizationRecord1,
				}
			},
			expectedRound: 4, // finalization round 3 + 1
			expectedEpoch: 1,
		},
		{
			name: "large gap with highest in middle",
			setupRecords: func(t *testing.T, bb *testutil.TestBlockBuilder, conf EpochConfig) [][]byte {
				// Create notarization for round 0
				block0, ok := bb.BuildBlock(ctx, ProtocolMetadata{Round: 0, Epoch: 0, Seq: 0}, emptyBlacklist)
				require.True(t, ok)
				bBytes0 := block0.Bytes()
				blockRecord0, err := BlockRecord(block0.BlockHeader(), bBytes0)
				require.NoError(t, err)
				notarizationRecord0, err := testutil.NewNotarizationRecord(conf.Logger, conf.SignatureAggregatorCreator(conf.Comm.Validators()), block0, nodes[0:quorum])
				require.NoError(t, err)

				// Create finalization for round 10 (highest)
				block10, ok := bb.BuildBlock(ctx, ProtocolMetadata{Round: 10, Epoch: 0, Seq: 10}, emptyBlacklist)
				require.True(t, ok)
				bBytes10 := block10.Bytes()
				require.NoError(t, err)
				blockRecord10, err := BlockRecord(block10.BlockHeader(), bBytes10)
				require.NoError(t, err)
				_, finalizationRecord10 := testutil.NewFinalizationRecord(t, conf.SignatureAggregatorCreator(conf.Comm.Validators()), block10, nodes[0:quorum])

				// Create empty notarization for round 5
				emptyNotarization5 := testutil.NewEmptyNotarization(nodes[0:quorum], 5)
				emptyNotarizationRecord5 := NewEmptyNotarizationRecord(emptyNotarization5)

				// Return with highest in middle
				return [][]byte{
					blockRecord0, notarizationRecord0,
					blockRecord10, finalizationRecord10,
					emptyNotarizationRecord5,
				}
			},
			expectedRound: 11, // finalization round 10 + 1
			expectedEpoch: 0,
		},
		{
			name: "all same round with different types",
			setupRecords: func(t *testing.T, bb *testutil.TestBlockBuilder, conf EpochConfig) [][]byte {
				// Create block and all record types for round 2
				block2, ok := bb.BuildBlock(ctx, ProtocolMetadata{Round: 2, Epoch: 0, Seq: 2}, emptyBlacklist)
				require.True(t, ok)
				bBytes2 := block2.Bytes()
				blockRecord2, err := BlockRecord(block2.BlockHeader(), bBytes2)
				require.NoError(t, err)

				notarizationRecord2, err := testutil.NewNotarizationRecord(conf.Logger, conf.SignatureAggregatorCreator(conf.Comm.Validators()), block2, nodes[0:quorum])
				require.NoError(t, err)

				_, finalizationRecord2 := testutil.NewFinalizationRecord(t, conf.SignatureAggregatorCreator(conf.Comm.Validators()), block2, nodes[0:quorum])

				// All records for same round
				return [][]byte{
					blockRecord2,
					notarizationRecord2,
					finalizationRecord2,
				}
			},
			expectedRound: 3, // round 2 + 1
			expectedEpoch: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)

			e, err := NewEpoch(conf)
			require.NoError(t, err)
			t.Cleanup(e.Stop)

			// Setup records for this test case
			records := tt.setupRecords(t, bb, conf)
			for _, record := range records {
				require.NoError(t, wal.Append(record))
			}

			// Verify initial state
			require.Equal(t, uint64(0), e.Metadata().Round)

			// Start epoch (triggers recovery)
			err = e.Start()
			require.NoError(t, err)

			// Verify round and epoch were set correctly from highest record
			require.Equal(t, tt.expectedRound, e.Metadata().Round)
			require.Equal(t, tt.expectedEpoch, e.Metadata().Epoch)
		})
	}
}

func TestRecoveryAndBroadcast(t *testing.T) {
	nodes := []NodeID{{1}, {2}, {3}, []byte("recovering")}

	net := testutil.NewControlledNetwork(t, nodes)

	startSeq := uint64(6)
	// initiate a network with 4 nodes. one node is behind by startSeq blocks
	storageData := createBlocks(t, nodes, startSeq)
	testEpochConfig := &testutil.TestNodeConfig{
		InitialStorage:     storageData,
		ReplicationEnabled: true,
	}
	normalNode1 := testutil.NewControlledSimplexNode(t, nodes[0], net, testEpochConfig)
	normalNode2 := testutil.NewControlledSimplexNode(t, nodes[1], net, testEpochConfig)
	normalNode3 := testutil.NewControlledSimplexNode(t, nodes[2], net, testEpochConfig)
	laggingNode := testutil.NewControlledSimplexNode(t, nodes[3], net, &testutil.TestNodeConfig{
		ReplicationEnabled: true,
		InitialStorage:     storageData[:2],
	})

	require.Equal(t, startSeq, normalNode1.Storage.NumBlocks())
	require.Equal(t, startSeq, normalNode2.Storage.NumBlocks())
	require.Equal(t, startSeq, normalNode3.Storage.NumBlocks())
	require.Equal(t, uint64(2), laggingNode.Storage.NumBlocks())

	normalNode1.Start()
	normalNode2.Start()
	normalNode3.Start()

	// start lagging node last since other nodes will not be ready to accept broadcast messages until they have started
	laggingNode.Start()
	defer net.StopInstances()

	laggingNode.Storage.WaitForBlockCommit(startSeq - 1)

}
