// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex_test

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math"
	rand2 "math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/ava-labs/simplex/common"
	. "github.com/ava-labs/simplex/simplex"
	"github.com/ava-labs/simplex/testutil"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

var (
	emptyBlacklist = newEmptyBlacklist()
)

// newEmptyBlacklist returns the blacklist that a leader of a network of four nodes proposes when it has nothing to report.
func newEmptyBlacklist() Blacklist {
	blacklist := NewBlacklist(4, DefaultMaxBlacklistedRounds)
	return blacklist.Next(0, DefaultMaxBlacklistedRounds, nil, nil)
}

// TestFinalizeSameSequence tests that a node only verifies a block after it receives all the empty notarizations the block depends on.
// This means the node can(and should) send a finalize vote for the same sequence if and only if a valid empty notarization has been verified.
//
// The test does this
// Round 0 := finalized block of seq 0
// Round 1 := receive block of seq 1, round 1 advance from notarization & send finalize vote
// Round 2 := receive block of seq 1, round 2 and are unable to verify(because we don't have empty notarization yet)
//
// Once we receive the empty notarization, we can verify the block(seq 1, round 2) and send out a finalize vote
func TestFinalizeSameSequence(t *testing.T) {
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

	require.NoError(t, e.Start())
	require.Equal(t, uint64(1), e.Metadata().Seq)

	// we receive a block and then notarize(this sends out a finalize vote for the block)
	advanceRoundFromNotarization(t, e, bb)
	require.Equal(t, uint64(2), e.Metadata().Seq)
	require.Equal(t, uint64(2), e.Metadata().Round)

	// wait for finalize votes
	for {
		msg := <-recordingComm.BroadcastMessages
		if msg.FinalizeVote != nil {
			require.Equal(t, uint64(1), msg.FinalizeVote.Finalization.Round)
			require.Equal(t, uint64(1), msg.FinalizeVote.Finalization.Seq)
			break
		}
	}

	// clear the recorded messages
	for len(recordingComm.BroadcastMessages) > 0 {
		<-recordingComm.BroadcastMessages
	}

	md := ProtocolMetadata{
		Round: 2,
		Seq:   1, // set next seq to 1 not 2
		Prev:  initialBlock.VerifiedBlock.BlockHeader().Digest,
	}
	vb, ok := bb.BuildBlock(context.Background(), md, NewBlacklist(uint16(len(e.Comm.Validators())), e.MaxBlacklistedRounds))
	require.True(t, ok)

	block := vb.(*testutil.TestBlock)
	var verified atomic.Bool
	block.OnVerify = func() {
		verified.Store(true)
	}

	// send block from leader
	vote, err := testutil.NewTestVote(block, nodes[2])
	require.NoError(t, err)
	err = e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{
			Vote:  *vote,
			Block: block,
		},
	}, nodes[2])
	require.NoError(t, err)

	// give some time for block to be (not)verified
	require.Never(t, func() bool {
		return verified.Load()
	}, 200*time.Millisecond, 50*time.Millisecond)

	// now lets send empty notarization
	emptyNotarization := testutil.NewEmptyNotarization(nodes, 1)
	err = e.HandleMessage(&Message{
		EmptyNotarization: emptyNotarization,
	}, nodes[3])
	require.NoError(t, err)

	// create a notarization and now we should send a finalize vote for seq 1 again
	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	notarization, err := testutil.NewNotarization(e.Logger, sigAggr, block, nodes[1:])
	require.NoError(t, err)
	testutil.InjectTestNotarization(t, e, notarization, nodes[1])

	wal.AssertNotarization(block.Metadata.Round)

	// wait for finalize votes
	for {
		msg := <-recordingComm.BroadcastMessages
		if msg.FinalizeVote != nil {
			require.Equal(t, uint64(2), msg.FinalizeVote.Finalization.Round)
			require.Equal(t, uint64(1), msg.FinalizeVote.Finalization.Seq)
			break
		}
	}

	require.Equal(t, uint64(2), e.Metadata().Seq)
	require.Equal(t, uint64(3), e.Metadata().Round)
}

func advanceWithFinalizeCheck(t *testing.T, e *Epoch, recordingComm *recordingComm, bb *testutil.TestBlockBuilder) *FinalizeVote {
	round := e.Metadata().Round
	seq := e.Metadata().Seq
	advanceRoundFromNotarization(t, e, bb)

	// wait for finalize votes for the round
	for {
		msg := <-recordingComm.BroadcastMessages
		if msg.FinalizeVote != nil {
			require.Equal(t, round, msg.FinalizeVote.Finalization.Round)
			require.Equal(t, seq, msg.FinalizeVote.Finalization.Seq)
			return msg.FinalizeVote
		}
	}
}

func TestFinalizeSameSequenceGap(t *testing.T) {
	nodes := []NodeID{{1}, {2}, {3}, {4}}

	for numEmpty := range uint64(5) {
		for numNotarizations := range uint64(5) {
			leader := LeaderForRound(nodes, 1+numEmpty+numNotarizations)
			if leader.Equals(nodes[0]) {
				continue
			}

			for seqToDoubleFinalize := uint64(1); seqToDoubleFinalize <= numNotarizations; seqToDoubleFinalize++ {
				t.Run(fmt.Sprintf("empty=%d notarizations=%d seq=%d", numEmpty, numNotarizations, seqToDoubleFinalize), func(t *testing.T) {
					t.Parallel()
					testFinalizeSameSequenceGap(t, nodes, numEmpty, numNotarizations, seqToDoubleFinalize)
				})
			}
		}
	}
}

func testFinalizeSameSequenceGap(t *testing.T, nodes []NodeID, numEmptyNotarizations uint64, numNotarizations uint64, seqToDoubleFinalize uint64) {
	bb := testutil.NewTestBlockBuilder()
	ctx := context.Background()
	initialBlock := createBlocks(t, nodes, 1)[0]
	recordingComm := &recordingComm{Communication: testutil.NewNoopComm(nodes), BroadcastMessages: make(chan *Message, 100), SentMessages: make(chan *Message, 100)}
	conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[0], recordingComm, bb)
	require.NoError(t, storage.Index(ctx, initialBlock.VerifiedBlock, initialBlock.Finalization))

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())
	require.Equal(t, uint64(1), e.Metadata().Seq)

	finalizeVoteSeqs := make(map[uint64]*FinalizeVote)
	finalizeVoteSeqs[0] = &FinalizeVote{
		Finalization: ToBeSignedFinalization{
			BlockHeader: initialBlock.VerifiedBlock.BlockHeader(),
		},
	}

	for i := uint64(0); i < numNotarizations; i++ {
		fVote := advanceWithFinalizeCheck(t, e, recordingComm, bb)
		finalizeVoteSeqs[fVote.Finalization.Seq] = fVote
	}

	for range numEmptyNotarizations {
		leader := LeaderForRound(e.Comm.Validators().NodeIDs(), e.Metadata().Round)
		if e.ID.Equals(leader) {
			fVote := advanceWithFinalizeCheck(t, e, recordingComm, bb)
			finalizeVoteSeqs[fVote.Finalization.Seq] = fVote
			numNotarizations++
		}

		advanceRoundFromEmpty(t, e)
	}

	require.Equal(t, 1+numEmptyNotarizations+numNotarizations, e.Metadata().Round)
	require.Equal(t, 1+numNotarizations, e.Metadata().Seq)

	// clear the recorded messages
	for len(recordingComm.BroadcastMessages) > 0 {
		<-recordingComm.BroadcastMessages
	}

	md := ProtocolMetadata{
		Round: 1 + numEmptyNotarizations + numNotarizations,
		Seq:   seqToDoubleFinalize,
		Prev:  finalizeVoteSeqs[seqToDoubleFinalize-1].Finalization.Digest,
	}
	vb, ok := bb.BuildBlock(context.Background(), md, NewBlacklist(uint16(len(e.Comm.Validators())), e.MaxBlacklistedRounds))
	require.True(t, ok)

	block := vb.(*testutil.TestBlock)
	verified := make(chan struct{}, 1)
	block.OnVerify = func() {
		verified <- struct{}{}
	}

	leader := LeaderForRound(e.Comm.Validators().NodeIDs(), 1+numEmptyNotarizations+numNotarizations)
	if e.ID.Equals(leader) {
		return
	}

	// send block from leader
	vote, err := testutil.NewTestVote(block, leader)
	require.NoError(t, err)
	err = e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{
			Vote:  *vote,
			Block: block,
		},
	}, leader)
	require.NoError(t, err)

	// give some time for block to be (not)verified
	require.Never(t, func() bool {
		return len(verified) > 0
	}, 200*time.Millisecond, 50*time.Millisecond)

	// now lets send empty notarizations seqToDoubleFinalize.round - seqToDoubleFinalize-1.round
	startMissingNotarizationRound := finalizeVoteSeqs[seqToDoubleFinalize-1].Finalization.Round + 1
	for i := startMissingNotarizationRound; i < md.Round; i++ {
		emptyNotarization := testutil.NewEmptyNotarization(nodes, i)
		err = e.HandleMessage(&Message{
			EmptyNotarization: emptyNotarization,
		}, nodes[3])
		require.NoError(t, err)
	}

	<-verified

	// drain any finalize votes that were sent
	for len(recordingComm.BroadcastMessages) > 0 {
		<-recordingComm.BroadcastMessages
	}

	// create a notarization and now we should send a finalize vote for seqToDoubleFinalize again
	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	notarization, err := testutil.NewNotarization(e.Logger, sigAggr, block, nodes[1:])
	require.NoError(t, err)
	testutil.InjectTestNotarization(t, e, notarization, nodes[1])

	wal.AssertNotarization(block.Metadata.Round)

	// wait for finalize votes
	for {
		msg := <-recordingComm.BroadcastMessages
		if msg.FinalizeVote != nil {
			require.Equal(t, seqToDoubleFinalize, msg.FinalizeVote.Finalization.Seq)
			break
		}
	}

	require.Equal(t, 2+numEmptyNotarizations+numNotarizations, e.Metadata().Round)
}

func TestBlockNotVerifiedIfParentNotNotarized(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()

	nodes := []NodeID{{1}, {2}, {3}, {4}}

	comm := testutil.NewNoopComm(nodes)
	conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[3], comm, bb)

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())

	blocks := createBlocks(t, nodes, 2)

	var block1Verified atomic.Bool

	var wg sync.WaitGroup
	wg.Add(1)

	block0 := blocks[0].VerifiedBlock.(*testutil.TestBlock)
	block0.OnVerify = func() {
		wg.Done()
	}
	block1 := blocks[1].VerifiedBlock.(*testutil.TestBlock)
	block1.OnVerify = func() {
		block1Verified.Store(true)
	}

	v0, err := testutil.NewTestVote(block0, nodes[0])
	require.NoError(t, err)

	v1, err := testutil.NewTestVote(block1, nodes[1])
	require.NoError(t, err)

	emptyNotarization := testutil.NewEmptyNotarization(nodes, 0)

	err = e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{
			Vote:  *v0,
			Block: block0,
		},
	}, nodes[0])
	require.NoError(t, err)

	wg.Wait()

	err = e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{
			Vote:  *v1,
			Block: block1,
		},
	}, nodes[1])
	require.NoError(t, err)

	err = e.HandleMessage(&Message{
		EmptyNotarization: emptyNotarization,
	}, nodes[1])
	require.NoError(t, err)

	require.Never(t, func() bool {
		return block1Verified.Load()
	}, time.Second, 100*time.Millisecond)
}

func TestEpochHandleNotarizationFutureRound(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	// Create the two blocks ahead of time
	blocks := createBlocks(t, nodes, 2)
	firstBlock := blocks[0].VerifiedBlock.(*testutil.TestBlock)
	secondBlock := blocks[1].VerifiedBlock.(*testutil.TestBlock)

	conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)
	quorum := Quorum(len(nodes))

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())

	// Create a notarization for round 1 which is a future round because we haven't gone through round 0 yet.
	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	notarization, err := testutil.NewNotarization(conf.Logger, sigAggr, secondBlock, nodes)
	require.NoError(t, err)

	// Give the node the notarization message before receiving the first block
	require.NoError(t, e.HandleMessage(&Message{
		Notarization: &notarization,
	}, nodes[1]))

	// Run through round 0
	notarizeAndFinalizeRoundWithMetadata(t, e, bb, &firstBlock.Metadata)

	// Emulate round 1 by sending the block
	vote, err := testutil.NewTestVote(secondBlock, nodes[1])
	require.NoError(t, err)
	err = e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{
			Vote:  *vote,
			Block: secondBlock,
		},
	}, nodes[1])
	require.NoError(t, err)

	// The node should store the notarization of the second block once it gets the block.
	wal.AssertNotarization(1)

	for i := 1; i < quorum; i++ {
		testutil.InjectTestFinalizeVote(t, e, secondBlock, nodes[i])
	}

	blockCommitted := storage.WaitForBlockCommit(1)
	require.Equal(t, secondBlock, blockCommitted)
}

// TestEpochIndexFinalization ensures that we properly index past finalizations when
// there have been empty rounds
func TestEpochIndexFinalization(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	conf, _, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)
	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())
	firstBlock, _ := advanceRoundFromNotarization(t, e, bb)
	advanceRoundFromFinalization(t, e, bb)

	require.Equal(t, uint64(2), e.Metadata().Round)
	require.Equal(t, uint64(2), e.Metadata().Seq)
	require.Equal(t, uint64(0), e.Storage.NumBlocks())

	advanceRoundFromEmpty(t, e)
	require.Equal(t, uint64(3), e.Metadata().Round)
	require.Equal(t, uint64(2), e.Metadata().Seq)
	require.Equal(t, uint64(0), e.Storage.NumBlocks())

	advanceRoundFromFinalization(t, e, bb)
	require.Equal(t, uint64(4), e.Metadata().Round)
	require.Equal(t, uint64(3), e.Metadata().Seq)
	require.Equal(t, uint64(0), e.Storage.NumBlocks())

	// at this point we are waiting on finalization of seq 0.
	// when we receive that finalization, we should commit the rest of the finalizations for seqs
	// 1 & 2

	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	finalization, _ := testutil.NewFinalizationRecord(t, sigAggr, firstBlock, e.Comm.Validators().NodeIDs())
	testutil.InjectTestFinalization(t, e, &finalization, nodes[1])

	storage.WaitForBlockCommit(2)
}

func TestEquivocatedBlock(t *testing.T) {
	// Tests a case where a Byzantine leader equivocates:
	// it sends block A to the node while the honest majority certifies a different
	// block B for the same round. The node must discard the equivocated round, replicate
	// the authentic block B, and — after a restart — serve block B (never block A).
	// The two sub-cases differ only in whether the honest majority notarized or
	// finalized block B.

	for _, tt := range []struct {
		name string
		// injectEquivocation delivers the honest majority's certificate for block B
		// (which the node itself never received) to the running node.
		injectEquivocation func(t *testing.T, e *Epoch, sigAggr SignatureAggregator, blockB *testutil.TestBlock, signers []NodeID, from NodeID)
		// assertEquivocationDiscarded verifies the node discarded the equivocated round
		// rather than acting on a certificate for a block it never saw.
		assertEquivocationDiscarded func(t *testing.T, e *Epoch, wal *testutil.TestWAL, storage *testutil.InMemStorage)
		// buildQR builds the QuorumRound carrying the authentic block B.
		buildQR func(t *testing.T, sigAggr SignatureAggregator, blockB *testutil.TestBlock, signers []NodeID) QuorumRound
		// assertReplicated verifies the node persisted block B after replication.
		assertReplicated func(t *testing.T, wal *testutil.TestWAL, storage *testutil.InMemStorage, blockB *testutil.TestBlock)
		// replicationRequest is the request sent to the restarted node to fetch block B.
		replicationRequest ReplicationRequest
	}{
		{
			name: "Notarized",
			injectEquivocation: func(t *testing.T, e *Epoch, sigAggr SignatureAggregator, blockB *testutil.TestBlock, signers []NodeID, from NodeID) {
				notarizationB, err := testutil.NewNotarization(e.Logger, sigAggr, blockB, signers)
				require.NoError(t, err)
				require.Equal(t, blockB.BlockHeader().Digest, notarizationB.Vote.Digest)
				testutil.InjectTestNotarization(t, e, notarizationB, from)
			},
			assertEquivocationDiscarded: func(t *testing.T, e *Epoch, wal *testutil.TestWAL, storage *testutil.InMemStorage) {
				// The node must NOT advance the round: it never received block B, so it
				// cannot notarize the round it has (which holds block A). The equivocated
				// round is thrown away.
				require.Never(t, func() bool {
					return e.Metadata().Round > 0
				}, time.Second, 100*time.Millisecond)
				// The equivocated notarization is not persisted to the WAL (still just the block record).
				wal.AssertWALSize(1)
			},
			buildQR: func(t *testing.T, sigAggr SignatureAggregator, blockB *testutil.TestBlock, signers []NodeID) QuorumRound {
				notarization, err := testutil.NewNotarization(testutil.MakeLogger(t), sigAggr, blockB, signers)
				require.NoError(t, err)
				return QuorumRound{Block: blockB, Notarization: &notarization}
			},
			assertReplicated: func(t *testing.T, wal *testutil.TestWAL, storage *testutil.InMemStorage, blockB *testutil.TestBlock) {
				// The node writes the notarization of block B to the WAL but does not commit it.
				require.Equal(t, NotarizationRecordType, wal.AssertNotarization(0))
				require.Equal(t, uint64(0), storage.NumBlocks())
			},
			replicationRequest: ReplicationRequest{Rounds: []uint64{0}},
		},
		{
			name: "Finalized",
			injectEquivocation: func(t *testing.T, e *Epoch, sigAggr SignatureAggregator, blockB *testutil.TestBlock, signers []NodeID, from NodeID) {
				finalizationB, _ := testutil.NewFinalizationRecord(t, sigAggr, blockB, signers)
				require.Equal(t, blockB.BlockHeader().Digest, finalizationB.Finalization.Digest)
				testutil.InjectTestFinalization(t, e, &finalizationB, from)
			},
			assertEquivocationDiscarded: func(t *testing.T, e *Epoch, wal *testutil.TestWAL, storage *testutil.InMemStorage) {
				// Block 0 should not commit because the node never received block B.
				storage.EnsureNoBlockCommit(t, 0)
			},
			buildQR: func(t *testing.T, sigAggr SignatureAggregator, blockB *testutil.TestBlock, signers []NodeID) QuorumRound {
				finalizationB, _ := testutil.NewFinalizationRecord(t, sigAggr, blockB, signers)
				return QuorumRound{Block: blockB, Finalization: &finalizationB}
			},
			assertReplicated: func(t *testing.T, wal *testutil.TestWAL, storage *testutil.InMemStorage, blockB *testutil.TestBlock) {
				// The node recovers by committing the block the network actually finalized
				// (block B) at seq 0, not the equivocated block A.
				require.Equal(t, blockB, storage.WaitForBlockCommit(0))
				require.Equal(t, uint64(1), storage.NumBlocks())
			},
			replicationRequest: ReplicationRequest{Seqs: []uint64{0}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			bb := testutil.NewTestBlockBuilder()
			nodes := []NodeID{{1}, {2}, {3}, {4}}

			recordingComm := &recordingComm{
				Communication:     testutil.NewNoopComm(nodes),
				BroadcastMessages: make(chan *Message, 100),
				SentMessages:      make(chan *Message, 100),
			}
			conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[1], recordingComm, bb)
			conf.ReplicationEnabled = true

			leader := LeaderForRound(nodes, 0)
			require.NotEqual(t, conf.ID, leader) // Ensure that the node is not the leader for the first round.

			e, err := NewEpoch(conf)
			require.NoError(t, err)
			require.NoError(t, e.Start())

			// (1) Byzantine leader equivocates and sends block A to the node.
			md := e.Metadata()
			_, ok := bb.BuildBlock(context.Background(), md, emptyBlacklist)
			require.True(t, ok)
			blockA := bb.GetBuiltBlock()

			voteA, err := testutil.NewTestVote(blockA, leader)
			require.NoError(t, err)
			require.NoError(t, e.HandleMessage(&Message{
				BlockMessage: &BlockMessage{
					Vote:  *voteA,
					Block: blockA,
				},
			}, leader))

			// (2) Ensure the node has written the block to the WAL.
			wal.AssertWALSize(1)

			// (3) The honest majority certifies a *different* block B for the same round.
			blockB := testutil.NewTestBlock(blockA.BlockHeader().ProtocolMetadata, emptyBlacklist)
			blockB.Data = []byte("equivocated-block-B")
			blockB.ComputeDigest()
			// Ensure that the two blocks are different.
			require.NotEqual(t, blockA.BlockHeader().Digest, blockB.BlockHeader().Digest)

			validators := e.Comm.Validators()
			quorum := Quorum(len(validators))
			sigAggr := e.SignatureAggregatorCreator(validators)
			signers := validators.NodeIDs()[:quorum]

			// (4) Send the honest majority's certificate for block B to the node.
			tt.injectEquivocation(t, e, sigAggr, blockB, signers, nodes[2])

			// The node must discard the equivocated round: it never received block B.
			tt.assertEquivocationDiscarded(t, e, wal, storage)

			// (5) Wait until we receive a request to replicate the authentic block, block B.
			for msg := range recordingComm.SentMessages {
				if msg.ReplicationRequest != nil {
					break
				}
			}

			// (6) Feed the node the replication response containing the authentic block B.
			replicationResponse := &ReplicationResponse{
				Data: []QuorumRound{tt.buildQR(t, sigAggr, blockB, signers)},
			}
			require.NoError(t, e.HandleMessage(&Message{
				ReplicationResponse: replicationResponse,
			}, nodes[2]))

			// (7) The node persists block B.
			tt.assertReplicated(t, wal, storage, blockB)

			// The rest of the test ensures that when the node restarts, it has in its
			// storage/memory only the correct block B and not the equivocated block A.
			e.Stop()
			e, err = NewEpoch(conf)
			require.NoError(t, err)
			require.NoError(t, e.Start())
			t.Cleanup(e.Stop)

			// Drain the output channel.
			for len(recordingComm.SentMessages) > 0 {
				<-recordingComm.SentMessages
			}

			// (8) Send a replication request to the restarted node and expect to get the
			// correct block B and not the equivocated block A.
			replicationRequest := tt.replicationRequest
			require.NoError(t, e.HandleMessage(&Message{
				ReplicationRequest: &replicationRequest,
			}, nodes[2]))

			// The node should reply with the block the network certified (block B), never
			// the equivocated block A that only it ever saw.
			var resp *VerifiedReplicationResponse
			require.Eventually(t, func() bool {
				for {
					select {
					case msg := <-recordingComm.SentMessages:
						if msg.VerifiedReplicationResponse != nil {
							resp = msg.VerifiedReplicationResponse
							return true
						}
					default:
						return false
					}
				}
			}, 5*time.Second, 50*time.Millisecond, "node did not reply to the replication request")

			require.Len(t, resp.Data, 1)
			gotBlock := resp.Data[0].VerifiedBlock
			require.Equal(t, blockB.BlockHeader().Digest, gotBlock.BlockHeader().Digest)
			require.NotEqual(t, blockA.BlockHeader().Digest, gotBlock.BlockHeader().Digest)
		})
	}
}

func TestEpochConsecutiveProposalsDoNotGetVerified(t *testing.T) {
	for _, test := range []struct {
		name                      string
		err                       error
		expectedVerificationCount int
		capacity                  int
	}{
		{
			name:                      "valid block",
			expectedVerificationCount: 1,
			capacity:                  10,
		},
		{
			name:                      "invalid block",
			err:                       fmt.Errorf("invalid block"),
			expectedVerificationCount: 10,
			capacity:                  1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			bb := testutil.NewTestBlockBuilder()
			nodes := []NodeID{{1}, {2}, {3}, {4}}

			conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[1], testutil.NewNoopComm(nodes), bb)

			e, err := NewEpoch(conf)
			require.NoError(t, err)
			t.Cleanup(e.Stop)

			semaphore := make(chan struct{}, test.capacity) // poor man's semaphore to limit the number of concurrent verifications

			e.Logger.(*testutil.TestLogger).Intercept(func(entry zapcore.Entry) error {
				if strings.Contains(entry.Message, "Block verification ended") {
					<-semaphore // release a resource in the semaphore when a block verification ends
				}
				return nil
			})

			require.NoError(t, e.Start())

			leader := nodes[0]

			md := e.Metadata()
			vb, ok := bb.BuildBlock(context.Background(), md, emptyBlacklist)
			require.True(t, ok)
			require.Equal(t, md.Round, md.Seq)

			var timesVerified atomic.Uint32

			var scheduledWG sync.WaitGroup
			scheduledWG.Add(test.expectedVerificationCount)

			block := vb.(*testutil.TestBlock)
			block.OnVerify = func() {
				defer scheduledWG.Done()
				timesVerified.Add(1)
			}
			block.VerificationError = test.err

			vote, err := testutil.NewTestVote(block, leader)
			require.NoError(t, err)

			for i := 0; i < 10; i++ {
				semaphore <- struct{}{} // acquire a resource in the semaphore before scheduling a block verification
				err := e.HandleMessage(&Message{
					BlockMessage: &BlockMessage{
						Vote:  *vote,
						Block: block,
					},
				}, leader)
				require.NoError(t, err)
			}
			scheduledWG.Wait()

			require.Equal(t, uint32(test.expectedVerificationCount), timesVerified.Load())
		})
	}
}

// TestEpochLeaderEquivocationDoesNotFloodBlockVerification tests that
// a leader flooding distinct valid blocks for the same round must get at most
// one verified and must not overwhelm the bounded verification queue.
func TestEpochLeaderEquivocationDoesNotFloodBlockVerification(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}

	conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[1], testutil.NewNoopComm(nodes), bb)

	// Count the scheduler's "queue full" warnings. Installed before NewEpoch (and
	// thus before the scheduler goroutine is started) so there is no race on the logger.
	var queueFullWarnings atomic.Uint32
	logger := conf.Logger.(*testutil.TestLogger)
	logger.Intercept(func(entry zapcore.Entry) error {
		if strings.Contains(entry.Message, "Too many blocks being verified") {
			queueFullWarnings.Add(1)
		}
		return nil
	})

	e, err := NewEpoch(conf)
	require.NoError(t, err)

	leader := LeaderForRound(nodes, 0)
	require.NotEqual(t, conf.ID, leader) // the local node must not be the leader of the round

	// Hold every verification until the flood is fully submitted, so exactly one
	// proposal stays in-flight (pendingRounds only blocks an in-flight verification).
	gate := make(chan struct{})
	var releaseGate sync.Once
	openGate := func() { releaseGate.Do(func() { close(gate) }) }
	t.Cleanup(e.Stop)
	t.Cleanup(openGate)

	require.NoError(t, e.Start())

	md := e.Metadata()
	require.Equal(t, uint64(0), md.Round)

	var timesVerified atomic.Uint32

	// More distinct, valid proposals than the 500-slot queue can hold.
	const floodSize = DefaultProcessingBlocks + 50
	digests := make(map[Digest]struct{}, floodSize)
	for i := 0; i < floodSize; i++ {
		// Distinct payload -> distinct digest, but identical, valid metadata.
		block := testutil.NewTestBlock(md, emptyBlacklist)
		block.Data = []byte(fmt.Sprintf("equivocated-proposal-%d", i))
		block.ComputeDigest()
		block.VerificationDelay = gate
		block.OnVerify = func() { timesVerified.Add(1) }

		d := block.BlockHeader().Digest
		_, dup := digests[d]
		require.False(t, dup, "the leader's equivocated blocks must be distinct")
		digests[d] = struct{}{}

		vote, err := testutil.NewTestVote(block, leader)
		require.NoError(t, err)

		require.NoError(t, e.HandleMessage(&Message{
			BlockMessage: &BlockMessage{Vote: *vote, Block: block},
		}, leader))
	}

	// Scheduling is synchronous within HandleMessage, so any queue-full warnings
	// have already been emitted by the time the flood loop returns.
	require.Zero(t, queueFullWarnings.Load(),
		"leader equivocation flood must not overrun the shared verification queue")

	// Let the scheduled verification(s) run to completion.
	openGate()

	// The leader's proposal is verified at least once...
	require.Eventually(t, func() bool { return timesVerified.Load() >= 1 },
		time.Second, 10*time.Millisecond, "the leader's proposal should be verified")
	// ...but no more than one proposal per (leader, round) is verified.
	require.Never(t, func() bool { return timesVerified.Load() > 1 },
		500*time.Millisecond, 10*time.Millisecond,
		"a leader must not get more than one proposal verified for the same round")
}

// TestReplicationRerequestsRoundWhenVerificationQueueIsFull asserts that a replicated notarized block whose
// verification cannot be scheduled is re-requested rather than dropped.
func TestReplicationRerequestsRoundWhenVerificationQueueIsFull(t *testing.T) {
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	comm := &recordingComm{Communication: testutil.NewNoopComm(nodes), SentMessages: make(chan *Message, 100)}
	conf, _, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[2], comm, testutil.NewTestBlockBuilder())
	conf.ReplicationEnabled = true
	// Filling the queue logs a WARN, which fails CI.
	conf.Logger.(*testutil.TestLogger).Silence()

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	// Hold the verification of the notarized block so every redelivery queues another task behind it.
	blocks := createBlocks(t, nodes, 1)
	block := blocks[0].VerifiedBlock.(*testutil.TestBlock)
	blockVerification := make(chan struct{})
	allowVerification := sync.OnceFunc(func() { close(blockVerification) })
	t.Cleanup(allowVerification)
	block.VerificationDelay = blockVerification
	notarization, err := testutil.NewNotarization(conf.Logger, &testutil.TestSignatureAggregator{N: len(nodes)}, block, nodes)
	require.NoError(t, err)
	notarizedRound := &Message{ReplicationResponse: &ReplicationResponse{
		Data: []QuorumRound{{Block: block, Notarization: &notarization}},
	}}

	// Redeliver until the queue is full and the node re-requests the round instead of scheduling it.
	for i := 0; len(comm.SentMessages) == 0; i++ {
		require.Less(t, i, DefaultProcessingBlocks+2)
		require.NoError(t, e.HandleMessage(notarizedRound, nodes[0]))
	}
	request := (<-comm.SentMessages).ReplicationRequest
	require.NotNil(t, request)
	require.Contains(t, request.Rounds, uint64(0))

	// Once the queue drains the node advances past the notarized round and still commits it.
	allowVerification()
	require.Eventually(t, func() bool { return e.Metadata().Round == 1 }, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, e.HandleMessage(replicateSeq(blocks[0]), nodes[0]))
	storage.WaitForBlockCommit(0)
}

// TestEpochIncreasesRoundAfterFinalization ensures that the epochs round is incremented
// if we receive a finalization for the current round(even if it is not the next seq to commit)
func TestEpochIncreasesRoundAfterFinalization(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}, {5}, {6}}

	conf, _, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[2], testutil.NewNoopComm(nodes), bb)
	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())

	block, _ := advanceRoundFromNotarization(t, e, bb)
	advanceRoundFromFinalization(t, e, bb)
	require.Equal(t, uint64(2), e.Metadata().Round)
	require.Equal(t, uint64(0), storage.NumBlocks())

	// create the finalized block
	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	finalization, _ := testutil.NewFinalizationRecord(t, sigAggr, block, nodes)
	testutil.InjectTestFinalization(t, e, &finalization, nodes[1])

	storage.WaitForBlockCommit(1)
	require.Equal(t, uint64(2), e.Metadata().Round)
	require.Equal(t, uint64(2), storage.NumBlocks())

	// we are the leader, ensure we can continue & propose a block
	notarizeAndFinalizeRound(t, e, bb)
}

func TestEpochNotarizeTwiceThenFinalize(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()

	nodes := []NodeID{{1}, {2}, {3}, {4}}

	recordedMessages := make(chan *Message, 100)
	comm := &recordingComm{Communication: testutil.NewNoopComm(nodes), BroadcastMessages: recordedMessages}

	conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[0], comm, bb)

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())

	// Round 0
	block0 := bb.GetBuiltBlock()

	testutil.InjectTestVote(t, e, block0, nodes[1])
	testutil.InjectTestVote(t, e, block0, nodes[2])
	wal.AssertNotarization(0)

	// Round 1
	emptyNote := testutil.NewEmptyNotarization(nodes, 1)
	err = e.HandleMessage(&Message{
		EmptyNotarization: emptyNote,
	}, nodes[1])
	require.NoError(t, err)
	emptyRecord := wal.AssertNotarization(1)
	require.Equal(t, EmptyNotarizationRecordType, emptyRecord)

	// Round 2
	md := e.Metadata()
	_, ok := bb.BuildBlock(context.Background(), md, emptyBlacklist)
	require.True(t, ok)
	block1 := bb.GetBuiltBlock()

	vote, err := testutil.NewTestVote(block1, nodes[2])
	require.NoError(t, err)
	err = e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{
			Vote:  *vote,
			Block: block1,
		},
	}, nodes[1])
	require.NoError(t, err)

	testutil.InjectTestVote(t, e, block1, nodes[3])
	wal.AssertNotarization(2)

	// Round 3
	md = e.Metadata()
	_, ok = bb.BuildBlock(context.Background(), md, emptyBlacklist)
	require.True(t, ok)
	block2 := bb.GetBuiltBlock()

	vote, err = testutil.NewTestVote(block2, nodes[3])
	require.NoError(t, err)
	err = e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{
			Vote:  *vote,
			Block: block2,
		},
	}, nodes[3])
	require.NoError(t, err)

	testutil.InjectTestVote(t, e, block2, nodes[2])
	wal.AssertNotarization(3)
	require.Equal(t, uint64(0), storage.NumBlocks())

	// drain the recorded messages
	for len(recordedMessages) > 0 {
		<-recordedMessages
	}

	blocks := make(map[uint64]*testutil.TestBlock)
	blocks[0] = block0
	blocks[2] = block1

	var wg sync.WaitGroup
	wg.Add(1)

	finish := make(chan struct{})
	// Once the node sends a finalizeVote message, send it finalizeVote messages as a response
	go func() {
		defer wg.Done()
		for {
			select {
			case <-finish:
				return
			case msg := <-recordedMessages:
				if msg.FinalizeVote != nil {
					round := msg.FinalizeVote.Finalization.Round
					if block, ok := blocks[round]; ok {
						testutil.InjectTestFinalizeVote(t, e, block, nodes[1])
						testutil.InjectTestFinalizeVote(t, e, block, nodes[2])
					}
				}
			}
		}
	}()

	testutil.InjectTestFinalizeVote(t, e, block2, nodes[1])
	testutil.InjectTestFinalizeVote(t, e, block2, nodes[2])

	storage.WaitForBlockCommit(0)
	storage.WaitForBlockCommit(1)
	storage.WaitForBlockCommit(2)

	close(finish)
	wg.Wait()
}

func TestEpochFinalizeThenNotarize(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	quorum := Quorum(len(nodes))

	conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)
	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())

	t.Run("commit without notarization, only with finalization", func(t *testing.T) {
		for round := 0; round < 100; round++ {
			advanceRoundFromFinalization(t, e, bb)
			storage.WaitForBlockCommit(uint64(round))
		}
	})

	t.Run("notarization after commit without notarizations", func(t *testing.T) {
		// leader is the proposer of the new block for the given round
		leader := LeaderForRound(nodes, uint64(100))
		// only create blocks if we are not the node running the epoch
		if !leader.Equals(e.ID) {
			md := e.Metadata()
			_, ok := bb.BuildBlock(context.Background(), md, emptyBlacklist)
			require.True(t, ok)
		}

		block := bb.GetBuiltBlock()

		vote, err := testutil.NewTestVote(block, nodes[0])
		require.NoError(t, err)
		err = e.HandleMessage(&Message{
			BlockMessage: &BlockMessage{
				Vote:  *vote,
				Block: block,
			},
		}, nodes[1])
		require.NoError(t, err)

		for i := 1; i < quorum; i++ {
			testutil.InjectTestVote(t, e, block, nodes[i])
		}

		wal.AssertNotarization(100)
	})

}

func TestEpochSimpleFlow(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())

	rounds := uint64(100)
	for round := uint64(0); round < rounds; round++ {
		notarizeAndFinalizeRound(t, e, bb)
	}
}

func TestEpochResizesBlacklistOnEpochChange(t *testing.T) {
	nodes := []NodeID{{1}, {2}}
	bb := testutil.NewTestBlockBuilder()

	// The epoch number is the sequence of the last indexed sealing block, so both nodes
	// below start in epoch 1 while their last indexed block belongs to epoch 0.
	epoch0Block := testutil.NewTestBlock(ProtocolMetadata{Epoch: 0, Round: 0, Seq: 0}, NewBlacklist(1, DefaultMaxBlacklistedRounds))
	sealingBlock := testutil.NewTestBlock(ProtocolMetadata{Epoch: 0, Round: 1, Seq: 1, Prev: epoch0Block.Digest}, NewBlacklist(1, DefaultMaxBlacklistedRounds))
	sealingBlock.SealingInfo = &SealingBlockInfo{
		ValidatorSet:         NodeIDs(nodes).EqualWeightedNodes(),
		PrevSealingBlockHash: epoch0Block.Digest,
	}
	sealingBlacklist := sealingBlock.Blacklist()
	require.Equal(t, uint16(1), sealingBlacklist.NodeCount(),
		"blacklist must contain exactly one node")

	sigAggregator := &testutil.TestSignatureAggregator{N: len(nodes)}
	epoch0Finalization, _ := testutil.NewFinalizationRecord(t, sigAggregator, epoch0Block, nodes)
	sealingFinalization, _ := testutil.NewFinalizationRecord(t, sigAggregator, sealingBlock, nodes)

	conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)
	require.NoError(t, conf.Storage.Index(context.Background(), epoch0Block, epoch0Finalization))
	require.NoError(t, conf.Storage.Index(context.Background(), sealingBlock, sealingFinalization))

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	require.NoError(t, e.Start())
	require.Equal(t, uint64(1), e.Metadata().Epoch)

	// The node (leader of round 2) builds the next block on top of the sealing block. Its
	// blacklist must be sized for the new validator set (2), not inherited from the
	// parent (1), otherwise its blacklist is malformed and the block cannot be notarized.
	bb.BlockShouldBeBuilt <- struct{}{}
	block := bb.GetBuiltBlock()
	blockBlacklist := block.Blacklist()
	require.Equal(t, uint16(2), blockBlacklist.NodeCount(),
		"blacklist must be resized to the new epoch's validator count")
	e.Stop()

	// Next, create the other node (follower) and ensure it can verify the block.
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[1], testutil.NewNoopComm(nodes), bb)
	require.NoError(t, conf.Storage.Index(context.Background(), epoch0Block, epoch0Finalization))
	require.NoError(t, conf.Storage.Index(context.Background(), sealingBlock, sealingFinalization))

	e, err = NewEpoch(conf)
	require.NoError(t, err)
	require.NoError(t, e.Start())
	t.Cleanup(e.Stop)
	require.Equal(t, uint64(1), e.Metadata().Epoch)

	vote, err := testutil.NewTestVote(block, nodes[0])
	require.NoError(t, err)

	err = e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{
			Vote:  *vote,
			Block: block,
		},
	}, nodes[0])
	require.NoError(t, err)
	wal.AssertNotarization(2)
}

// TestEpochBlacklistCapBypassedByPendingSuspects shows that the blacklist can end up with more
// than f blacklisted nodes. The cap of f is only enforced when a node is first added to the
// suspected list; incrementing nodes that are already pending never re-checks it.
// With 4 nodes (f=1), two leaders accuse nodes 0 and 1 in the same orbit, so both cross the
// f+1 threshold and both are blacklisted. Node 0 (the node under test) then refuses to
// propose in its own round.
func TestEpochBlacklistCapBypassedByPendingSuspects(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	f := (len(nodes) - 1) / 3

	conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	// Rounds 0 and 1: ordinary blocks proposed by nodes 0 and 1.
	notarizeAndFinalizeRound(t, e, bb)
	block, _ := notarizeAndFinalizeRound(t, e, bb)

	// Pretend nodes 0 and 1 timed out in round 2, so that the leaders accuse them.
	accuseNodes0And1 := map[uint16]uint64{0: 2, 1: 2}

	// Round 2: node 2 proposes a block accusing nodes 0 and 1.
	// Nothing is blacklisted yet, so both are added as pending with a single accusation each.
	prevBlacklist := block.Blacklist()
	blacklist := prevBlacklist.Next(e.Metadata().Round, DefaultMaxBlacklistedRounds, accuseNodes0And1, nil)
	bb.BuildBlock(context.Background(), e.Metadata(), blacklist)
	block, _ = notarizeAndFinalizeRound(t, e, bb)
	require.Equal(t, nodes[2], LeaderForRound(nodes, block.BlockHeader().Round))

	actual := block.Blacklist()
	require.False(t, actual.IsNodeSuspected(0))
	require.False(t, actual.IsNodeSuspected(1))
	require.True(t, blacklist.Equals(&actual))

	// Round 3: node 3 accuses nodes 0 and 1 again, still within the same orbit.
	// Each pending entry is incremented to f+1 without re-checking the cap.
	prevBlacklist = block.Blacklist()
	blacklist = prevBlacklist.Next(e.Metadata().Round, DefaultMaxBlacklistedRounds, accuseNodes0And1, nil)
	bb.BuildBlock(context.Background(), e.Metadata(), blacklist)
	block, _ = notarizeAndFinalizeRound(t, e, bb)
	require.Equal(t, nodes[3], LeaderForRound(nodes, block.BlockHeader().Round))

	actual = block.Blacklist()
	blacklisted := 0
	for i := range nodes {
		if actual.IsNodeSuspected(uint16(i)) {
			blacklisted++
		}
	}

	require.LessOrEqual(t, blacklisted, f,
		"at most f=%d nodes may be blacklisted, but %d are: %s", f, blacklisted, actual.String())
}

func TestEpochStartedTwice(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()

	nodes := []NodeID{{1}, {2}, {3}, {4}}
	conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())
	require.ErrorIs(t, e.Start(), ErrAlreadyStarted)
}

func advanceRoundFromEmpty(t *testing.T, e *Epoch) {
	leader := LeaderForRound(e.Comm.Validators().NodeIDs(), e.Metadata().Round)
	require.False(t, e.ID.Equals(leader), "epoch cannot be the leader for the empty round")

	emptyNote := testutil.NewEmptyNotarization(e.Comm.Validators().NodeIDs(), e.Metadata().Round)
	err := e.HandleMessage(&Message{
		EmptyNotarization: emptyNote,
	}, leader)

	require.NoError(t, err)

	emptyRecord := e.WAL.(*testutil.TestWAL).AssertNotarization(emptyNote.Vote.Round)
	require.Equal(t, EmptyNotarizationRecordType, emptyRecord)
}

func advanceRoundFromNotarization(t *testing.T, e *Epoch, bb *testutil.TestBlockBuilder) (VerifiedBlock, *Notarization) {
	return advanceRound(t, e, bb, true, false, nil)
}

func advanceRoundFromFinalization(t *testing.T, e *Epoch, bb *testutil.TestBlockBuilder) VerifiedBlock {
	block, _ := advanceRound(t, e, bb, false, true, nil)
	return block
}

func notarizeAndFinalizeRound(t *testing.T, e *Epoch, bb *testutil.TestBlockBuilder) (VerifiedBlock, *Notarization) {
	return advanceRound(t, e, bb, true, true, nil)
}

func notarizeAndFinalizeRoundWithMetadata(t *testing.T, e *Epoch, bb *testutil.TestBlockBuilder, md *ProtocolMetadata) (VerifiedBlock, *Notarization) {
	return advanceRound(t, e, bb, true, true, md)
}

func FuzzEpochInterleavingMessages(f *testing.F) {
	f.Fuzz(func(t *testing.T, seed int64) {
		testEpochInterleavingMessages(t, seed)
	})
}

func TestEpochInterleavingMessages(t *testing.T) {
	buff := make([]byte, 8)

	for i := 0; i < 100; i++ {
		_, err := rand.Read(buff)
		require.NoError(t, err)
		seed := int64(binary.BigEndian.Uint64(buff))
		testEpochInterleavingMessages(t, seed)
	}
}

func testEpochInterleavingMessages(t *testing.T, seed int64) {
	rounds := 10
	bb := testutil.NewTestBlockBuilder().WithBuiltBuffer(uint64(rounds))
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	conf, _, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	var protocolMetadata ProtocolMetadata

	callbacks := createCallbacks(t, rounds, protocolMetadata, nodes, e, bb)

	require.NoError(t, e.Start())

	r := rand2.New(rand2.NewSource(seed))
	for i, index := range r.Perm(len(callbacks)) {
		t.Log("Called callback", i, "out of", len(callbacks))
		callbacks[index]()
	}

	for i := 0; i < rounds; i++ {
		t.Log("Waiting for commit of round", i)
		storage.WaitForBlockCommit(uint64(i))
	}
}

func createCallbacks(t *testing.T, rounds int, protocolMetadata ProtocolMetadata, nodes []NodeID, e *Epoch, bb *testutil.TestBlockBuilder) []func() {
	blocks := make([]VerifiedBlock, 0, rounds)

	callbacks := make([]func(), 0, rounds*4+len(blocks))

	for i := 0; i < rounds; i++ {
		block := testutil.NewTestBlock(protocolMetadata, emptyBlacklist)
		blocks = append(blocks, block)

		protocolMetadata.Seq++
		protocolMetadata.Round++
		protocolMetadata.Prev = block.BlockHeader().Digest

		leader := LeaderForRound(nodes, uint64(i))

		if !leader.Equals(e.ID) {
			vote, err := testutil.NewTestVote(block, leader)
			require.NoError(t, err)

			callbacks = append(callbacks, func() {
				t.Log("Injecting block", block.BlockHeader().Round)
				require.NoError(t, e.HandleMessage(&Message{
					BlockMessage: &BlockMessage{
						Block: block,
						Vote:  *vote,
					},
				}, leader))
			})
		} else {
			bb.SetBuiltBlock(block)
		}

		for j := 1; j <= 2; j++ {
			node := nodes[j]
			vote, err := testutil.NewTestVote(block, node)
			require.NoError(t, err)
			msg := Message{
				VoteMessage: vote,
			}

			callbacks = append(callbacks, func() {
				t.Log("Injecting vote for round",
					msg.VoteMessage.Vote.Round, msg.VoteMessage.Vote.Digest, msg.VoteMessage.Signature.Signer)
				err := e.HandleMessage(&msg, node)
				require.NoError(t, err)
			})
		}

		for j := 1; j <= 2; j++ {
			node := nodes[j]
			vote := testutil.NewTestFinalizeVote(t, block, node)
			msg := Message{
				FinalizeVote: vote,
			}
			callbacks = append(callbacks, func() {
				t.Log("Injecting finalized vote for round", msg.FinalizeVote.Finalization.Round, msg.FinalizeVote.Finalization.Digest)
				err := e.HandleMessage(&msg, node)
				require.NoError(t, err)
			})
		}
	}
	return callbacks
}

func TestEpochBlockSentTwice(t *testing.T) {
	var tooFarMsg, alreadyReceivedMsg bool

	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[1], testutil.NewNoopComm(nodes), bb)

	l := conf.Logger.(*testutil.TestLogger)

	l.Intercept(func(entry zapcore.Entry) error {
		if entry.Message == "Got block of a future round" {
			tooFarMsg = true
		}

		if entry.Message == "Already received a proposal from this node for the round" {
			alreadyReceivedMsg = true
		}

		return nil
	})

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())

	md := e.Metadata()
	md.Round = 2

	b, ok := bb.BuildBlock(context.Background(), md, emptyBlacklist)
	require.True(t, ok)

	block := b.(Block)

	vote, err := testutil.NewTestVote(block, nodes[2])
	require.NoError(t, err)
	err = e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{
			Vote:  *vote,
			Block: block,
		},
	}, nodes[2])
	require.NoError(t, err)

	wal.AssertWALSize(0)
	require.True(t, tooFarMsg)

	err = e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{
			Vote:  *vote,
			Block: block,
		},
	}, nodes[2])
	require.NoError(t, err)

	wal.AssertWALSize(0)
	require.True(t, alreadyReceivedMsg)

}

func TestEpochQCSignedByNonExistentNodes(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(6)

	defer wg.Wait()

	unknownNotarizationChan := make(chan struct{})
	unknownEmptyNotarizationChan := make(chan struct{})
	unknownFinalizationChan := make(chan struct{})
	doubleNotarizationChan := make(chan struct{})
	doubleEmptyNotarizationChan := make(chan struct{})
	doubleFinalizationChan := make(chan struct{})

	callbacks := map[string]func(){
		"Notarization quorum certificate contains an unknown signer": func() {
			wg.Done()
			close(unknownNotarizationChan)
		},
		"Empty notarization quorum certificate contains an unknown signer": func() {
			wg.Done()
			close(unknownEmptyNotarizationChan)
		},
		"Finalization quorum certificate contains an unknown signer": func() {
			wg.Done()
			close(unknownFinalizationChan)
		},
		"Notarization quorum certificate is signed by the same node": func() {
			wg.Done()
			close(doubleNotarizationChan)
		},
		"Empty notarization quorum certificate is signed by the same node": func() {
			wg.Done()
			close(doubleEmptyNotarizationChan)
		},
		"Finalization quorum certificate is signed by the same node": func() {
			wg.Done()
			close(doubleFinalizationChan)
		},
	}

	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)
	l := conf.Logger.(*testutil.TestLogger)
	l.Intercept(func(entry zapcore.Entry) error {
		for key, f := range callbacks {
			if strings.Contains(entry.Message, key) {
				f()
			}
		}
		return nil
	})

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())

	block := bb.GetBuiltBlock()

	wal.AssertWALSize(1)

	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())

	t.Run("notarization with unknown signer isn't taken into account", func(t *testing.T) {
		notarization, err := testutil.NewNotarization(conf.Logger, sigAggr, block, []NodeID{{2}, {3}, {5}})
		require.NoError(t, err)

		err = e.HandleMessage(&Message{
			Notarization: &notarization,
		}, nodes[1])
		require.NoError(t, err)

		time.Sleep(time.Second)

		wal.AssertWALSize(1)
	})

	t.Run("notarization with double signer isn't taken into account", func(t *testing.T) {
		notarization, err := testutil.NewNotarization(conf.Logger, sigAggr, block, []NodeID{{2}, {3}})
		require.NoError(t, err)

		tqc := notarization.QC.(testutil.TestQC)
		tqc = append(tqc, Signature{Signer: nodes[2], Value: []byte{0}})
		notarization.QC = tqc

		err = e.HandleMessage(&Message{
			Notarization: &notarization,
		}, nodes[1])
		require.NoError(t, err)

		wal.AssertWALSize(1)
	})

	t.Run("empty notarization with unknown signer isn't taken into account", func(t *testing.T) {
		var qc testutil.TestQC
		for i, n := range []NodeID{{2}, {3}, {5}} {
			qc = append(qc, Signature{Signer: n, Value: []byte{byte(i)}})
		}

		err = e.HandleMessage(&Message{
			EmptyNotarization: &EmptyNotarization{
				Vote: ToBeSignedEmptyVote{EmptyVoteMetadata: EmptyVoteMetadata{
					Round: 0,
					Epoch: 0,
				}},
				QC: qc,
			},
		}, nodes[1])
		require.NoError(t, err)

		wal.AssertWALSize(1)
	})

	t.Run("empty notarization with double signer isn't taken into account", func(t *testing.T) {
		var qc testutil.TestQC
		for i, n := range []NodeID{{2}, {3}, {2}} {
			qc = append(qc, Signature{Signer: n, Value: []byte{byte(i)}})
		}

		err = e.HandleMessage(&Message{
			EmptyNotarization: &EmptyNotarization{
				Vote: ToBeSignedEmptyVote{EmptyVoteMetadata: EmptyVoteMetadata{
					Round: 0,
					Epoch: 0,
				}},
				QC: qc,
			},
		}, nodes[1])
		require.NoError(t, err)

		wal.AssertWALSize(1)
	})

	t.Run("finalization with unknown signer isn't taken into account", func(t *testing.T) {
		finalization, _ := testutil.NewFinalizationRecord(t, sigAggr, block, []NodeID{{2}, {3}, {5}})

		err = e.HandleMessage(&Message{
			Finalization: &finalization,
		}, nodes[1])
		require.NoError(t, err)

		storage.EnsureNoBlockCommit(t, 0)
	})

	t.Run("finalization with double signer isn't taken into account", func(t *testing.T) {
		finalization, _ := testutil.NewFinalizationRecord(t, sigAggr, block, []NodeID{{2}, {3}, {3}})

		err = e.HandleMessage(&Message{
			Finalization: &finalization,
		}, nodes[1])
		require.NoError(t, err)

		storage.EnsureNoBlockCommit(t, 0)
	})
}

func TestEpochBlockSentFromNonLeader(t *testing.T) {
	nonLeaderMessage := false

	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[1], testutil.NewNoopComm(nodes), bb)
	l := conf.Logger.(*testutil.TestLogger)
	l.Intercept(func(entry zapcore.Entry) error {
		if entry.Message == "Got block from a block proposer that is not the leader of the round" {
			nonLeaderMessage = true
		}
		return nil
	})

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())

	md := e.Metadata()
	b, ok := bb.BuildBlock(context.Background(), md, emptyBlacklist)
	require.True(t, ok)

	block := b.(Block)

	notLeader := nodes[3]
	vote, err := testutil.NewTestVote(block, notLeader)
	require.NoError(t, err)
	err = e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{
			Vote:  *vote,
			Block: block,
		},
	}, notLeader)
	require.NoError(t, err)
	require.True(t, nonLeaderMessage)
	records, err := wal.WriteAheadLog.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 0)
}

func TestEpochBlockTooHighRound(t *testing.T) {
	var rejectedBlock bool

	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[1], testutil.NewNoopComm(nodes), bb)

	l := conf.Logger.(*testutil.TestLogger)
	l.Intercept(func(entry zapcore.Entry) error {
		if entry.Message == "Received a block message for a too high round" {
			rejectedBlock = true
		}
		return nil
	})

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())

	t.Run("block from higher round is rejected", func(t *testing.T) {
		defer func() {
			rejectedBlock = false
		}()

		md := e.Metadata()
		md.Round = math.MaxUint64 - 3

		b, ok := bb.BuildBlock(context.Background(), md, emptyBlacklist)
		require.True(t, ok)

		block := b.(Block)

		vote, err := testutil.NewTestVote(block, nodes[0])
		require.NoError(t, err)
		err = e.HandleMessage(&Message{
			BlockMessage: &BlockMessage{
				Vote:  *vote,
				Block: block,
			},
		}, nodes[0])
		require.NoError(t, err)
		require.True(t, rejectedBlock)

		wal.AssertWALSize(0)
	})

	t.Run("block is accepted", func(t *testing.T) {
		defer func() {
			rejectedBlock = false
		}()

		md := e.Metadata()
		b, ok := bb.BuildBlock(context.Background(), md, emptyBlacklist)
		require.True(t, ok)

		block := b.(Block)

		vote, err := testutil.NewTestVote(block, nodes[0])
		require.NoError(t, err)
		err = e.HandleMessage(&Message{
			BlockMessage: &BlockMessage{
				Vote:  *vote,
				Block: block,
			},
		}, nodes[0])
		require.NoError(t, err)
		require.False(t, rejectedBlock)

		wal.AssertWALSize(1)
	})
}

// TestEpochSendsBlockDigestRequest ensures that we send a block digest request
// when we receive a notarization for a block we don't have
func TestEpochSendsBlockDigestRequest(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	ctx := context.Background()
	recordedMessages := make(chan *Message, 100)
	comm := &recordingComm{Communication: testutil.NewNoopComm(nodes), SentMessages: recordedMessages}
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[3], comm, bb)
	conf.ReplicationEnabled = true

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())

	round0Empty := testutil.NewEmptyNotarization(nodes, 0)
	err = e.HandleMessage(&Message{
		EmptyNotarization: round0Empty,
	}, nodes[0])
	require.NoError(t, err)

	emptyNote := wal.AssertNotarization(0)
	require.NoError(t, err)
	require.Equal(t, EmptyNotarizationRecordType, emptyNote)

	_, built := bb.BuildBlock(ctx, ProtocolMetadata{}, emptyBlacklist)
	require.True(t, built)
	block := bb.GetBuiltBlock()

	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	notarization, err := testutil.NewNotarization(conf.Logger, sigAggr, block, nodes)
	require.NoError(t, err)

	err = e.HandleMessage(&Message{
		Notarization: &notarization,
	}, nodes[1])
	require.NoError(t, err)

	// ensure we send the block digest request
	for msg := range recordedMessages {
		if msg.BlockDigestRequest != nil {
			require.Equal(t, uint64(0), msg.BlockDigestRequest.Seq)
			require.Equal(t, block.BlockHeader().Digest, msg.BlockDigestRequest.Digest)
			break
		}
	}

	// send the response with the block
	replicationResponse := &ReplicationResponse{
		Data: []QuorumRound{
			{
				Block:        block,
				Notarization: &notarization,
			},
		},
	}

	err = e.HandleMessage(&Message{
		ReplicationResponse: replicationResponse,
	}, nodes[2])
	require.NoError(t, err)

	for !wal.ContainsNotarization(0) {
		time.Sleep(20 * time.Millisecond)
	}

	// sanity check: ensure we didn't double increment the round!
	require.Equal(t, uint64(1), e.Metadata().Round)
}

// TestMetadataProposedRound ensures the metadata only builds off blocks
// with finalizations or notarizations
func TestMetadataProposedRound(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())

	// assert the proposed block was written to the wal
	wal.AssertWALSize(1)
	require.Zero(t, e.Metadata().Round)
	require.Zero(t, e.Metadata().Seq)
}

func TestEpochVotesForEquivocatedVotes(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()

	nodes := []NodeID{{1}, {2}, {3}, {4}}
	recordedMessages := make(chan *Message, 100)
	comm := &recordingComm{Communication: testutil.NewNoopComm(nodes), BroadcastMessages: recordedMessages}
	conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[3], comm, bb)

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())

	md := e.Metadata()
	_, ok := bb.BuildBlock(context.Background(), md, emptyBlacklist)
	require.True(t, ok)

	block := bb.GetBuiltBlock()

	// the leader and this node are sending the votes for the same block
	leader := nodes[0]
	vote, err := testutil.NewTestVote(block, leader)
	require.NoError(t, err)

	err = e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{
			Vote:  *vote,
			Block: block,
		},
	}, leader)
	require.NoError(t, err)

	// node 1 sends a vote for a different block
	equivocatedBlock := testutil.NewTestBlock(block.Metadata, emptyBlacklist)
	equivocatedBlock.Data = []byte{1, 2, 3}
	equivocatedBlock.ComputeDigest()
	testutil.InjectTestVote(t, e, equivocatedBlock, nodes[1])

	// We should not have sent a notarization yet, since we have not received enough votes for the block we received from the leader
	require.Never(t, func() bool {
		select {
		case msg := <-recordedMessages:
			if msg.Notarization != nil {
				return true
			}
		default:
			return false
		}
		return false
	}, time.Millisecond*500, time.Millisecond*100)

	// node 2 sends a vote for the same block as the leader
	testutil.InjectTestVote(t, e, block, nodes[2])

	// Wait for the notarization to be sent
	timeout := time.After(time.Minute)
	var notarization *Notarization
	for notarization == nil {
		select {
		case msg := <-recordedMessages:
			if msg.Notarization != nil {
				notarization = msg.Notarization
			}
		case <-timeout:
			require.Fail(t, "timed out waiting for notarization")
		}
	}

	for _, signer := range notarization.QC.(testutil.TestQC).Signers() {
		require.NotEqual(t, nodes[1], signer, "Node 1 should not be in the notarization QC")
	}
}

// TestEpochRequestsEmptyRoundDependency ensures that when we receive a block
// that builds off an empty round we don't have, we request those missing empty notarizations.
func TestEpochRequestsEmptyRoundDependency(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	ctx := context.Background()
	blocks := createBlocks(t, nodes, 1)
	recordedMessages := make(chan *Message, 100)
	comm := &recordingComm{Communication: testutil.NewNoopComm(nodes), SentMessages: recordedMessages}
	conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[3], comm, bb)
	conf.ReplicationEnabled = true
	require.NoError(t, storage.Index(ctx, blocks[0].VerifiedBlock, blocks[0].Finalization))
	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())

	advanceRoundFromNotarization(t, e, bb)
	testutil.WaitToEnterRound(t, e, 2)

	skippedMD := blocks[0].VerifiedBlock.BlockHeader().ProtocolMetadata
	skippedMD.Round = 2
	skippedMD.Seq = 1
	skippedMD.Prev = blocks[0].VerifiedBlock.BlockHeader().Digest

	// the next node, proposes a block that doesn't build off the first block. note seq = 1
	_, built := bb.BuildBlock(ctx, skippedMD, emptyBlacklist)
	require.True(t, built)
	block2 := bb.GetBuiltBlock()

	vote1, err := testutil.NewTestVote(block2, nodes[2])
	require.NoError(t, err)

	err = e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{
			Block: block2,
			Vote:  *vote1,
		},
	}, nodes[2])
	require.NoError(t, err)

	for msg := range recordedMessages {
		if msg.ReplicationRequest != nil {
			require.Equal(t, uint64(1), msg.ReplicationRequest.Rounds[0])
			break
		}
	}

	missingEmptyNotarization := testutil.NewEmptyNotarization(nodes, 1)

	// send the response with the block
	replicationResponse := &ReplicationResponse{
		Data: []QuorumRound{
			{
				EmptyNotarization: missingEmptyNotarization,
			},
		},
	}
	err = e.HandleMessage(&Message{
		ReplicationResponse: replicationResponse,
	}, nodes[2])
	require.NoError(t, err)

	for !wal.ContainsEmptyNotarization(1) {
		time.Sleep(20 * time.Millisecond)
	}

	// notarize the block and wait to increase the round
	testutil.InjectTestVote(t, e, block2, nodes[0])
	testutil.InjectTestVote(t, e, block2, nodes[1])

	wal.AssertNotarization(2)
	testutil.WaitToEnterRound(t, e, 3)

	// sanity check: ensure we didn't double increment the round!
	require.Equal(t, uint64(3), e.Metadata().Round)
}

// TestEpochFinalizeVoteRebroadcastSkipsTimedOutFork ensures that after finalizing a future sequence,
// we do not rebroadcast a finalize vote for a timed out round whose block is not an ancestor of the
// finalized block. Otherwise the vote could finalize a second block at the finalized sequence.
//
//	block0 (r0,s0) <-- block1 (r1,s1)   timed out, notarized
//	               \
//	                -- block2 (r2,s1)   finalized
func TestEpochFinalizeVoteRebroadcastSkipsTimedOutFork(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	ctx := context.Background()
	broadcastMessages := make(chan *Message, 100)
	sentMessages := make(chan *Message, 100)
	comm := &recordingComm{Communication: testutil.NewNoopComm(nodes), BroadcastMessages: broadcastMessages, SentMessages: sentMessages}
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[3], comm, bb)
	conf.ReplicationEnabled = true
	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())

	// round 0 is notarized but not finalized, so seq 0 is never committed
	block0, _ := advanceRoundFromNotarization(t, e, bb)
	testutil.WaitToEnterRound(t, e, 1)

	// we time out on round 1, then learn that block1 was notarized anyway
	bb.BlockShouldBeBuilt <- struct{}{}
	testutil.WaitForBlockProposerTimeout(t, e, &e.StartTime, 1)

	_, built := bb.BuildBlock(ctx, e.Metadata(), emptyBlacklist)
	require.True(t, built)
	block1 := bb.GetBuiltBlock()
	require.Equal(t, block0.BlockHeader().Digest, block1.BlockHeader().Prev)
	require.Equal(t, uint64(1), block1.BlockHeader().Seq)

	vote1, err := testutil.NewTestVote(block1, nodes[1])
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{
			Block: block1,
			Vote:  *vote1,
		},
	}, nodes[1]))
	notarization1, err := testutil.NewNotarization(e.Logger, e.SignatureAggregatorCreator(e.Comm.Validators()), block1, nodes[0:3])
	require.NoError(t, err)
	testutil.InjectTestNotarization(t, e, notarization1, nodes[1])

	require.Equal(t, NotarizationRecordType, wal.AssertNotarization(1))
	testutil.WaitToEnterRound(t, e, 2)

	// block2 builds off block0, skipping round 1
	md2 := e.Metadata()
	md2.Seq = 1
	md2.Prev = block0.BlockHeader().Digest

	_, built = bb.BuildBlock(ctx, md2, emptyBlacklist)
	require.True(t, built)
	block2 := bb.GetBuiltBlock()
	require.Equal(t, block0.BlockHeader().Digest, block2.BlockHeader().Prev)
	require.Equal(t, uint64(1), block2.BlockHeader().Seq)

	vote2, err := testutil.NewTestVote(block2, nodes[2])
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{
			Block: block2,
			Vote:  *vote2,
		},
	}, nodes[2]))

	// block2 skips round 1, so we need to send an empty notarization before block2 can be verified
	for msg := range sentMessages {
		if msg.ReplicationRequest != nil {
			require.Equal(t, uint64(1), msg.ReplicationRequest.Rounds[0])
			break
		}
	}

	require.NoError(t, e.HandleMessage(&Message{
		ReplicationResponse: &ReplicationResponse{
			Data: []QuorumRound{{EmptyNotarization: testutil.NewEmptyNotarization(nodes, 1)}},
		},
	}, nodes[2]))

	for !wal.ContainsEmptyNotarization(1) {
		time.Sleep(20 * time.Millisecond)
	}

	testutil.InjectTestVote(t, e, block2, nodes[0])
	testutil.InjectTestVote(t, e, block2, nodes[1])
	wal.AssertNotarization(2)

	// finalizing block2 is a future sequence since seq 0 is not committed
	testutil.InjectTestFinalizeVote(t, e, block2, nodes[0])
	testutil.InjectTestFinalizeVote(t, e, block2, nodes[1])

	var sentFinalizeVoteForBlock1 bool
	for msg := range broadcastMessages {
		if msg.FinalizeVote != nil && msg.FinalizeVote.Finalization.Round == 1 {
			sentFinalizeVoteForBlock1 = true
		}
		if msg.Finalization != nil && msg.Finalization.Finalization.Round == 2 {
			break
		}
	}

	// block1 and block2 are both at seq 1, so we must not finalize vote block1
	require.False(t, sentFinalizeVoteForBlock1)
}

// TestEpochFinalizeVoteRebroadcastForTimedOutAncestor ensures that after finalizing a future sequence,
// we rebroadcast a finalize vote for a timed out round whose block is an ancestor of the finalized block,
// since the finalized block cannot be committed until its ancestor is.
//
//	block0 (r0,s0) <-- block1 (r1,s1) <-- block2 (r2,s2)
//	                   timed out,         finalized
//	                   notarized
func TestEpochFinalizeVoteRebroadcastForTimedOutAncestor(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	ctx := context.Background()
	broadcastMessages := make(chan *Message, 100)
	comm := &recordingComm{Communication: testutil.NewNoopComm(nodes), BroadcastMessages: broadcastMessages}
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[3], comm, bb)
	conf.ReplicationEnabled = true
	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())

	// round 0 is notarized but not finalized, so seq 0 is never committed
	block0, _ := advanceRoundFromNotarization(t, e, bb)
	testutil.WaitToEnterRound(t, e, 1)

	// we time out on round 1, then learn that block1 was notarized anyway
	bb.BlockShouldBeBuilt <- struct{}{}
	testutil.WaitForBlockProposerTimeout(t, e, &e.StartTime, 1)

	_, built := bb.BuildBlock(ctx, e.Metadata(), emptyBlacklist)
	require.True(t, built)
	block1 := bb.GetBuiltBlock()
	require.Equal(t, block0.BlockHeader().Digest, block1.BlockHeader().Prev)
	require.Equal(t, uint64(1), block1.BlockHeader().Seq)

	vote1, err := testutil.NewTestVote(block1, nodes[1])
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{
			Block: block1,
			Vote:  *vote1,
		},
	}, nodes[1]))
	notarization1, err := testutil.NewNotarization(e.Logger, e.SignatureAggregatorCreator(e.Comm.Validators()), block1, nodes[0:3])
	require.NoError(t, err)
	testutil.InjectTestNotarization(t, e, notarization1, nodes[1])

	require.Equal(t, NotarizationRecordType, wal.AssertNotarization(1))
	testutil.WaitToEnterRound(t, e, 2)

	// block2 extends block1
	_, built = bb.BuildBlock(ctx, e.Metadata(), emptyBlacklist)
	require.True(t, built)
	block2 := bb.GetBuiltBlock()
	require.Equal(t, block1.BlockHeader().Digest, block2.BlockHeader().Prev)
	require.Equal(t, uint64(2), block2.BlockHeader().Seq)

	vote2, err := testutil.NewTestVote(block2, nodes[2])
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{
			Block: block2,
			Vote:  *vote2,
		},
	}, nodes[2]))

	testutil.InjectTestVote(t, e, block2, nodes[0])
	testutil.InjectTestVote(t, e, block2, nodes[1])
	wal.AssertNotarization(2)

	// finalizing block2 is a future sequence since seq 0 is not committed
	testutil.InjectTestFinalizeVote(t, e, block2, nodes[0])
	testutil.InjectTestFinalizeVote(t, e, block2, nodes[1])

	var sentFinalizeVoteForBlock1 bool
	for msg := range broadcastMessages {
		if msg.FinalizeVote != nil && msg.FinalizeVote.Finalization.Round == 1 {
			sentFinalizeVoteForBlock1 = true
		}
		if msg.Finalization != nil && msg.Finalization.Finalization.Round == 2 {
			break
		}
	}

	// block2 cannot be committed until block1 is, so we must finalize vote block1
	require.True(t, sentFinalizeVoteForBlock1)
}

// TestEpochFinalizeVoteRebroadcastFollowsFinalizedChain ensures we rebroadcast & create
// finalized votes only for the ancestors of the future finalizations.
// Otherwise, we may end up signing two finalize vote messages for different blocks for the same sequence.
func TestEpochFinalizeVoteRebroadcastFollowsFinalizedChain(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	broadcastMessages := make(chan *Message, 100)
	comm := &recordingComm{Communication: testutil.NewNoopComm(nodes), BroadcastMessages: broadcastMessages}
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[3], comm, bb)
	conf.ReplicationEnabled = true
	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())

	// build two blocks that are notarized(block0, and block1)
	block0, _ := advanceRoundFromNotarization(t, e, bb)
	advanceRoundFromNotarization(t, e, bb)

	// block2 skips round 1, so it needs round 1's empty notarization before it can be verified
	require.NoError(t, e.HandleMessage(&Message{
		ReplicationResponse: &ReplicationResponse{
			Data: []QuorumRound{{EmptyNotarization: testutil.NewEmptyNotarization(nodes, 1)}},
		},
	}, nodes[2]))
	require.True(t, wal.ContainsEmptyNotarization(1))

	md2 := e.Metadata()
	md2.Seq = 1
	md2.Prev = block0.BlockHeader().Digest
	block2, _ := advanceRound(t, e, bb, true, false, &md2)

	// we lead round 3, so we build block3 on top of block2
	block3, _ := advanceRoundFromNotarization(t, e, bb)
	require.Equal(t, block2.BlockHeader().Digest, block3.BlockHeader().Prev)

	for len(broadcastMessages) > 0 {
		<-broadcastMessages
	}

	// This gives block3 a finalization. block3 points to block2 -> block0.
	// Our node also has block1 + its notarization but it is not part of the finalizations chain.
	testutil.InjectTestFinalizeVote(t, e, block3, nodes[0])
	testutil.InjectTestFinalizeVote(t, e, block3, nodes[1])

	var rebroadcastRounds []uint64
	for msg := range broadcastMessages {
		if msg.FinalizeVote != nil {
			rebroadcastRounds = append(rebroadcastRounds, msg.FinalizeVote.Finalization.Round)
		}
		if msg.Finalization != nil && msg.Finalization.Finalization.Round == 3 {
			break
		}
	}
	// block3 is finalized at a future sequence since seq 0 is not committed, which triggers the rebroadcast
	require.Equal(t, uint64(0), e.Storage.NumBlocks())

	// ensure that we don't send a finalization for round 1
	require.Equal(t, []uint64{0, 2}, rebroadcastRounds)
}

// TestEpochPruneRestoredForkFromWal ensures a fork block restored from the WAL is never finalized.
// The WAL holds our empty vote and and a block in round 1. We then have a finalization for for block2 which
// was finalized on top of the empty round 1. This makes block1 a fork so after restarting,
// finalizing at a future sequence must not rebroadcast a finalize vote for block1.
func TestEpochPruneRestoredForkFromWal(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	ctx := context.Background()
	quorum := Quorum(len(nodes))
	broadcastMessages := make(chan *Message, 100)
	comm := &recordingComm{Communication: testutil.NewNoopComm(nodes), BroadcastMessages: broadcastMessages}
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], comm, bb)
	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	appendBlock := func(md ProtocolMetadata) VerifiedBlock {
		_, built := bb.BuildBlock(ctx, md, emptyBlacklist)
		require.True(t, built)
		block := bb.GetBuiltBlock()
		record, err := BlockRecord(block.BlockHeader(), block.Bytes())
		require.NoError(t, err)
		require.NoError(t, wal.Append(record))
		notarizationRecord, err := testutil.NewNotarizationRecord(conf.Logger, sigAggr, block, nodes[0:quorum])
		require.NoError(t, err)
		require.NoError(t, wal.Append(notarizationRecord))
		return block
	}

	md := e.Metadata()
	block0 := appendBlock(md)

	emptyVote := createEmptyVote(EmptyVoteMetadata{Round: 1, Epoch: md.Epoch}, e.ID).Vote
	require.NoError(t, wal.Append(NewEmptyVoteRecord(emptyVote)))

	md.Round, md.Seq, md.Prev = 1, 1, block0.BlockHeader().Digest
	appendBlock(md)
	require.NoError(t, wal.Append(NewEmptyNotarizationRecord(testutil.NewEmptyNotarization(nodes[0:quorum], 1))))

	md.Round = 2
	block2 := appendBlock(md)
	_, finalizationRecord := testutil.NewFinalizationRecord(t, sigAggr, block2, nodes[0:quorum])
	require.NoError(t, wal.Append(finalizationRecord))

	require.NoError(t, e.Start())
	require.Equal(t, uint64(3), e.Metadata().Round)

	block3, _ := advanceRoundFromNotarization(t, e, bb)
	require.Equal(t, block2.BlockHeader().Digest, block3.BlockHeader().Prev)

	// wait for our own finalize vote for block3 so it is not counted below
	for msg := range broadcastMessages {
		if msg.FinalizeVote != nil && msg.FinalizeVote.Finalization.Round == 3 {
			break
		}
	}

	testutil.InjectTestFinalizeVote(t, e, block3, nodes[1])
	testutil.InjectTestFinalizeVote(t, e, block3, nodes[2])

	var rebroadcastRounds []uint64
	for msg := range broadcastMessages {
		if msg.FinalizeVote != nil {
			rebroadcastRounds = append(rebroadcastRounds, msg.FinalizeVote.Finalization.Round)
		}
		if msg.Finalization != nil && msg.Finalization.Finalization.Round == 3 {
			break
		}
	}
	require.Equal(t, uint64(0), e.Storage.NumBlocks())

	// only send a finalize vote for block0. Block2 & 3 are finalized, and block1 is a fork.
	require.Equal(t, []uint64{0}, rebroadcastRounds)
}

// TestEpochReplicatedNotarizationVerifiedAfterFinalizationIsNotFinalized ensures a replicated notarized block
// is dropped if a higher round is finalized while it is being verified. block1's verification is held while
// block2 is finalized. Because block2 is built on top of the empty round 1 at block1's sequence, we should
// not add block1 or sign a finalize vote for it once its verification completes.
func TestEpochReplicatedNotarizationVerifiedAfterFinalizationIsNotFinalized(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	ctx := context.Background()
	broadcastMessages := make(chan *Message, 100)
	comm := &recordingComm{Communication: testutil.NewNoopComm(nodes), BroadcastMessages: broadcastMessages}
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[2], comm, bb)
	conf.ReplicationEnabled = true
	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())

	advanceRoundFromNotarization(t, e, bb)

	// we time out on round 1, which is then empty notarized
	bb.BlockShouldBeBuilt <- struct{}{}
	testutil.WaitForBlockProposerTimeout(t, e, &e.StartTime, 1)
	require.NoError(t, e.HandleMessage(&Message{EmptyNotarization: testutil.NewEmptyNotarization(nodes, 1)}, nodes[1]))

	// we lead round 2, so we build block2 on top of block0 and the empty round 1
	block2, _ := advanceRoundFromNotarization(t, e, bb)

	// block1 is a fork of block2 in round 1 at the same sequence
	md1 := block2.BlockHeader().ProtocolMetadata
	md1.Round = 1
	_, built := bb.BuildBlock(ctx, md1, emptyBlacklist)
	require.True(t, built)
	block1 := bb.GetBuiltBlock()
	block1.VerificationDelay = make(chan struct{})

	notarization1, err := testutil.NewNotarization(e.Logger, e.SignatureAggregatorCreator(e.Comm.Validators()), block1, []NodeID{nodes[0], nodes[1], nodes[3]})
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{
		ReplicationResponse: &ReplicationResponse{
			Data: []QuorumRound{{Block: block1, Notarization: &notarization1}},
		},
	}, nodes[0]))

	testutil.InjectTestFinalizeVote(t, e, block2, nodes[0])
	testutil.InjectTestFinalizeVote(t, e, block2, nodes[1])
	for msg := range broadcastMessages {
		if msg.Finalization != nil && msg.Finalization.Finalization.Round == 2 {
			break
		}
	}
	// block2 is finalized at a future sequence since seq 0 is not committed
	require.Equal(t, uint64(0), e.Storage.NumBlocks())

	close(block1.VerificationDelay)

	// To ensure block1 has finished verifying, we send our node block3. Because the verification scheduler only verifies
	// one block at a time, we know that if block3 is verified, block1's verification task must have completed(it was enqueued before block3).
	blacklist2 := block2.Blacklist()
	_, built = bb.BuildBlock(ctx, e.Metadata(), blacklist2.Next(3, DefaultMaxBlacklistedRounds, nil, nil))
	require.True(t, built)
	block3 := bb.GetBuiltBlock()
	vote3, err := testutil.NewTestVote(block3, nodes[3])
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{
			Block: block3,
			Vote:  *vote3,
		},
	}, nodes[3]))
	wal.AssertBlockProposal(3)

	for len(broadcastMessages) > 0 {
		msg := <-broadcastMessages
		require.Nil(t, msg.FinalizeVote, "signed a finalize vote for a block verified after a higher round was finalized")
	}
}

func TestReplicatedNotarizationRestart(t *testing.T) {
	// A test that makes a notarization be replicated to a node that doesn't have the block,
	// and then restarts the node. The node must recover from the WAL and start successfully.

	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}

	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[1], testutil.NewNoopComm(nodes), bb)
	conf.ReplicationEnabled = true

	leader := LeaderForRound(nodes, 0)
	require.NotEqual(t, conf.ID, leader) // The node is not the leader for round 0.

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	require.NoError(t, e.Start())

	// Build the block the leader proposed for round 0. The node itself never receives it.
	md := e.Metadata()
	_, ok := bb.BuildBlock(context.Background(), md, emptyBlacklist)
	require.True(t, ok)
	block := bb.GetBuiltBlock()

	validators := e.Comm.Validators()
	sigAggr := e.SignatureAggregatorCreator(validators)
	signers := validators.NodeIDs()[:Quorum(len(validators))]

	notarization, err := testutil.NewNotarization(conf.Logger, sigAggr, block, signers)
	require.NoError(t, err)

	// (1) Feed the node the notarization. It has no block for this round, so it must
	// request the block from the network (replication).
	testutil.InjectTestNotarization(t, e, notarization, nodes[2])

	// (2) Feed the node the replication response carrying the authentic notarized block.
	require.NoError(t, e.HandleMessage(&Message{
		ReplicationResponse: &ReplicationResponse{
			Data: []QuorumRound{{Block: block, Notarization: &notarization}},
		},
	}, nodes[2]))

	// (3) The node persisted the notarization to the WAL.
	require.Equal(t, NotarizationRecordType, wal.AssertNotarization(0))

	// (4) Restart: the node must recover from the WAL and start successfully.
	e.Stop()
	e, err = NewEpoch(conf)
	require.NoError(t, err)
	require.NoError(t, e.Start())
	t.Cleanup(e.Stop)
}

// Ensures we don't double increment the round on persisting a notarization
func TestDoubleIncrementOnPersistNotarization(t *testing.T) {
	// add an empty notarization, then a notarization for a previous round
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[3], testutil.NewNoopComm(nodes), bb)
	conf.ReplicationEnabled = true

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())

	advanceRoundFromEmpty(t, e)
	require.Equal(t, uint64(1), e.Metadata().Round)

	// create a notarization for round 0
	md := ProtocolMetadata{
		Epoch: 0,
		Round: 0,
		Seq:   0,
	}
	_, ok := bb.BuildBlock(context.Background(), md, emptyBlacklist)
	require.True(t, ok)

	block := bb.GetBuiltBlock()
	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	notarization, err := testutil.NewNotarization(conf.Logger, sigAggr, block, nodes)
	require.NoError(t, err)

	err = e.HandleMessage(&Message{
		ReplicationResponse: &ReplicationResponse{
			Data: []QuorumRound{
				{
					Block:        block,
					Notarization: &notarization,
				},
			},
		},
	}, nodes[0])
	require.NoError(t, err)

	wal.AssertWALSize(3)
	// ensure the round is still 1
	require.Equal(t, uint64(1), e.Metadata().Round)
}

func TestFailedNotarizationAppendHaltsEpoch(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	comm := &recordingComm{
		Communication:     testutil.NewNoopComm(nodes),
		BroadcastMessages: make(chan *Message, 1000),
	}
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[3], comm, bb)
	// The injected WAL failure logs an ERROR, which fails CI.
	conf.Logger.(*testutil.TestLogger).Silence()

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	// Rounds 0 and 1 are notarized but not finalized, so nothing is committed yet.
	advanceRound(t, e, bb, true, false, nil)
	round1Block, _ := advanceRound(t, e, bb, true, false, nil)

	// Round 2: the leader proposes a block, and we vote for it.
	const failedRound = uint64(2)
	require.Equal(t, failedRound, e.Metadata().Round)
	leader := LeaderForRound(nodes, failedRound)
	_, ok := bb.BuildBlock(context.Background(), e.Metadata(), emptyBlacklist)
	require.True(t, ok)
	block := bb.GetBuiltBlock()
	vote, err := testutil.NewTestVote(block, leader)
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{BlockMessage: &BlockMessage{Vote: *vote, Block: block}}, leader))
	wal.AssertBlockProposal(failedRound)

	// Persisting the notarization of round 2 fails once.
	errDisk := fmt.Errorf("transient disk failure")
	wal.FailNextAppend(NotarizationRecordType, errDisk)
	sigAggr := e.SignatureAggregatorCreator(e.Comm.Validators())
	notarization, err := testutil.NewNotarization(e.Logger, sigAggr, block, nodes[:3])
	require.NoError(t, err)
	err = e.HandleMessage(&Message{Notarization: &notarization}, nodes[1])
	require.ErrorIs(t, err, errDisk)
	require.False(t, wal.ContainsNotarization(failedRound))

	// A peer relays a finalization for round 1, which is above the next sequence to commit.
	// The epoch must stay halted and not act on it.
	finalization, _ := testutil.NewFinalizationRecord(t, sigAggr, round1Block, nodes[:3])
	err = e.HandleMessage(&Message{Finalization: &finalization}, nodes[1])
	require.ErrorIs(t, err, errDisk)

	// Let the round timer of round 2 expire several times over.
	bb.BlockShouldBeBuilt <- struct{}{}
	now := conf.StartTime
	for range 15 {
		now = now.Add(e.MaxProposalWait / 5)
		e.AdvanceTime(now)
		time.Sleep(10 * time.Millisecond)
	}

	var finalizeVoted, emptyVoted bool
	for len(comm.BroadcastMessages) > 0 {
		msg := <-comm.BroadcastMessages
		if msg.FinalizeVote != nil && msg.FinalizeVote.Finalization.Round == failedRound {
			finalizeVoted = true
		}
		if msg.EmptyVoteMessage != nil && msg.EmptyVoteMessage.Vote.Round == failedRound {
			emptyVoted = true
		}
	}
	require.False(t, finalizeVoted && emptyVoted,
		"node signed both a finalize vote and an empty vote for round %d", failedRound)
	require.False(t, finalizeVoted, "node signed a finalize vote for round %d whose notarization was never persisted", failedRound)
}

// ListnerComm is a comm that listens for incoming messages
// and sends them to the [in] channel
type listenerComm struct {
	testutil.NoopComm
	in chan *Message
}

func NewListenerComm(nodeIDs []NodeID) *listenerComm {
	return &listenerComm{
		NoopComm: testutil.NewNoopComm(nodeIDs),
		in:       make(chan *Message, 1),
	}
}

func (b *listenerComm) Send(msg *Message, id NodeID) {
	b.in <- msg
}

func TestRejectsOldNotarizationAndVotes(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	ctx := context.Background()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	initialBlock := createBlocks(t, nodes, 1)[0]
	conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[3], testutil.NewNoopComm(nodes), bb)
	require.NoError(t, storage.Index(ctx, initialBlock.VerifiedBlock, initialBlock.Finalization))

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)

	require.NoError(t, e.Start())
	require.Equal(t, uint64(1), e.Metadata().Seq)

	// send a block for round 1. then finalization then notarization for round 1
	md := e.Metadata()
	_, ok := bb.BuildBlock(context.Background(), md, emptyBlacklist)
	require.True(t, ok)

	block := bb.GetBuiltBlock()

	vote, err := testutil.NewTestVote(block, nodes[1])
	require.NoError(t, err)
	err = e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{
			Vote:  *vote,
			Block: block,
		},
	}, nodes[1])
	require.NoError(t, err)
	wal.AssertBlockProposal(1)

	for i := 0; i < len(nodes); i++ {
		if nodes[i].Equals(e.ID) {
			continue
		}
		testutil.InjectTestFinalizeVote(t, e, block, nodes[i])
	}
	testutil.WaitToEnterRound(t, e, 2)

	increment := 1
	// wait for the empty vote
	for !wal.ContainsEmptyVote(2) {
		if len(bb.BlockShouldBeBuilt) == 0 {
			bb.BlockShouldBeBuilt <- struct{}{}
		}
		e.AdvanceTime(e.StartTime.Add(conf.MaxProposalWait * time.Duration(increment)))
		time.Sleep(100 * time.Millisecond)
		increment++
	}

	// send notarization for round 1, after the finalization was sent
	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	notarization, err := testutil.NewNotarization(conf.Logger, sigAggr, block, nodes)
	require.NoError(t, err)

	err = e.HandleMessage(&Message{
		Notarization: &notarization,
	}, nodes[0])
	require.NoError(t, err)

	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			require.False(t, wal.ContainsNotarization(1), "notarization for old round should not be recorded")
			return
		default:
			if len(bb.BlockShouldBeBuilt) == 0 {
				bb.BlockShouldBeBuilt <- struct{}{}
			}
			wal.AssertHealthy(e.BlockDeserializer, e.QCDeserializer)
			e.AdvanceTime(e.StartTime.Add(conf.MaxProposalWait * time.Duration(increment)))
			time.Sleep(100 * time.Millisecond)
			increment++
		}
	}
}

func TestBlockDeserializer(t *testing.T) {
	var blockDeserializer testutil.BlockDeserializer

	ctx := context.Background()
	tb := testutil.NewTestBlock(ProtocolMetadata{Seq: 1, Round: 2, Epoch: 3}, emptyBlacklist)
	tbBytes := tb.Bytes()
	tb2, err := blockDeserializer.DeserializeBlock(ctx, tbBytes)
	require.NoError(t, err)
	require.Equal(t, tb.BlockHeader().Digest, tb2.BlockHeader().Digest)
}

// advanceRound progresses [e] to a new round. If [notarize] is set, the round will progress due to a notarization.
// If [finalize] is set, the round will advance and the block will be indexed to storage.
// If [injectedMD] is non-nil, it will be used as the metadata for the new block instead of generating one from the epoch.
func advanceRound(t *testing.T, e *Epoch, bb *testutil.TestBlockBuilder, notarize bool, finalize bool, injectedMD *ProtocolMetadata) (VerifiedBlock, *Notarization) {
	require.True(t, notarize || finalize, "must either notarize or finalize a round to advance")
	nextSeqToCommit := e.Storage.NumBlocks()
	nodes := e.Comm.Validators()
	quorum := Quorum(len(nodes))
	// leader is the proposer of the new block for the given round
	leader := LeaderForRound(nodes.NodeIDs(), e.Metadata().Round)
	md := e.Metadata()
	if injectedMD != nil {
		md = *injectedMD
	}

	// only create blocks if we are not the node running the epoch
	isEpochNode := leader.Equals(e.ID)
	if !isEpochNode {
		_, ok := bb.BuildBlock(context.Background(), md, NewBlacklist(uint16(len(e.Comm.Validators())), e.MaxBlacklistedRounds))
		require.True(t, ok)
	}

	block := bb.GetBuiltBlock()

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

	// Wait for the round to be stored in the WAL.
	// This ensures that the block proposal is persisted before we proceed to notarization or finalization.
	e.WAL.(*testutil.TestWAL).AssertBlockProposal(block.Metadata.Round)

	var notarization *Notarization
	if notarize {
		// start at one since our node has already voted
		sigAggr := e.SignatureAggregatorCreator(nodes)
		n, err := testutil.NewNotarization(e.Logger, sigAggr, block, nodes.NodeIDs()[0:quorum])
		testutil.InjectTestNotarization(t, e, n, nodes[1].Id)

		e.WAL.(*testutil.TestWAL).AssertNotarization(block.Metadata.Round)
		require.NoError(t, err)
		notarization = &n
	}

	if finalize {
		for i := 0; i <= quorum; i++ {
			if nodes[i].Id.Equals(e.ID) {
				continue
			}
			testutil.InjectTestFinalizeVote(t, e, block, nodes[i].Id)
		}

		if nextSeqToCommit != block.Metadata.Seq {
			testutil.WaitToEnterRound(t, e, block.Metadata.Round+1)
			return block, notarization
		}

		blockFromStorage := e.Storage.(*testutil.InMemStorage).WaitForBlockCommit(block.Metadata.Seq)
		require.Equal(t, block, blockFromStorage)
	}

	return block, notarization
}

func TestQuorum(t *testing.T) {
	for _, testCase := range []struct {
		n int
		f int
		q int
	}{
		{
			n: 1, f: 0,
			q: 1,
		},
		{
			n: 2, f: 0,
			q: 2,
		},
		{
			n: 3, f: 0,
			q: 2,
		},
		{
			n: 4, f: 1,
			q: 3,
		},
		{
			n: 5, f: 1,
			q: 4,
		},
		{
			n: 6, f: 1,
			q: 4,
		},
		{
			n: 7, f: 2,
			q: 5,
		},
		{
			n: 8, f: 2,
			q: 6,
		},
		{
			n: 9, f: 2,
			q: 6,
		},
		{
			n: 10, f: 3,
			q: 7,
		},
		{
			n: 11, f: 3,
			q: 8,
		},
		{
			n: 12, f: 3,
			q: 8,
		},
	} {
		t.Run(fmt.Sprintf("%d", testCase.n), func(t *testing.T) {
			require.Equal(t, testCase.q, Quorum(testCase.n))
		})
	}
}

// rejectingVerifier accepts every signature except rejected.
type rejectingVerifier struct {
	rejected []byte
}

func (v *rejectingVerifier) VerifySignature(_ []byte, signature SignatureBytes, _ PublicKeyBytes) error {
	if string(signature) == string(v.rejected) {
		return fmt.Errorf("invalid signature")
	}
	return nil
}

// TestEpochVoteSentTwiceKeepsVerifiedVote ensures a node that has already voted in a round
// cannot displace the vote we verified by sending a second one. Otherwise a signature that
// fails verification ends up in the notarization we assemble and broadcast.
func TestEpochVoteSentTwiceKeepsVerifiedVote(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}

	forged := []byte("forged signature")

	comm := &recordingComm{Communication: testutil.NewNoopComm(nodes), BroadcastMessages: make(chan *Message, 100)}
	conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], comm, bb)
	conf.Verifier = &rejectingVerifier{rejected: forged}

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	// nodes[0] leads round 0, so it proposes the block and votes for it itself.
	block := bb.GetBuiltBlock()

	// nodes[1] casts a legitimate vote.
	vote, err := testutil.NewTestVote(block, nodes[1])
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{VoteMessage: vote}, nodes[1]))

	// nodes[1] votes a second time for the same round, with a signature that does not
	// verify. It must be dropped rather than take the place of the vote above.
	require.NoError(t, e.HandleMessage(&Message{VoteMessage: &Vote{
		Vote:      vote.Vote,
		Signature: Signature{Signer: nodes[1], Value: forged},
	}}, nodes[1]))

	// nodes[2] votes, bringing the round to a quorum so a notarization is assembled.
	vote2, err := testutil.NewTestVote(block, nodes[2])
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{VoteMessage: vote2}, nodes[2]))

	timeout := time.After(time.Minute)
	for {
		select {
		case msg := <-comm.BroadcastMessages:
			if msg.Notarization == nil {
				continue
			}
			qc, ok := msg.Notarization.QC.(testutil.TestQC)
			require.True(t, ok)
			for _, sig := range qc {
				require.NotEqual(t, forged, sig.Value,
					"notarization QC contains a signature that failed verification, signer %x", sig.Signer)
			}
			return
		case <-timeout:
			t.Fatal("timed out waiting for a notarization to be broadcast")
		}
	}
}

// TestEpochBlockSentTwiceKeepsVerifiedVote ensures a leader re-sending a buffered proposal
// with different vote signature bytes has that vote verified rather than accepted on the
// strength of the earlier, identical block header.
func TestEpochBlockSentTwiceKeepsVerifiedVote(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}

	forged := []byte("forged signature")

	conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[1], testutil.NewNoopComm(nodes), bb)
	conf.Verifier = &rejectingVerifier{rejected: forged}

	var verificationFailed bool
	conf.Logger.(*testutil.TestLogger).Intercept(func(entry zapcore.Entry) error {
		if entry.Message == "ToBeSignedVote verification failed" {
			verificationFailed = true
		}
		return nil
	})

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	// nodes[2] leads round 2 and sends its proposal early, so it is stored as a future message.
	md := e.Metadata()
	md.Round = 2
	b, ok := bb.BuildBlock(context.Background(), md, emptyBlacklist)
	require.True(t, ok)
	block := b.(Block)

	vote, err := testutil.NewTestVote(block, nodes[2])
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{Vote: *vote, Block: block},
	}, nodes[2]))
	require.False(t, verificationFailed)

	// The same block with a vote that does not verify must be rejected by signature verification
	forgedVote := Vote{Vote: vote.Vote, Signature: Signature{Signer: nodes[2], Value: forged}}
	require.NoError(t, e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{Vote: forgedVote, Block: block},
	}, nodes[2]))
	require.True(t, verificationFailed)
}

// TestEpochBlockVoteHeaderMismatch ensures a block message whose vote is for a
// different block header is dropped, so a later matching proposal is still accepted.
func TestEpochBlockVoteHeaderMismatch(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[1], testutil.NewNoopComm(nodes), bb)

	var storedFutureBlock, alreadyReceived bool
	conf.Logger.(*testutil.TestLogger).Intercept(func(entry zapcore.Entry) error {
		switch entry.Message {
		case "Got block of a future round":
			storedFutureBlock = true
		case "Already received a proposal from this node for the round":
			alreadyReceived = true
		}
		return nil
	})

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	// nodes[2] leads round 2, so its proposal is stored as a future message.
	md := e.Metadata()
	md.Round = 2
	b, ok := bb.BuildBlock(context.Background(), md, emptyBlacklist)
	require.True(t, ok)
	block := b.(Block)

	vote, err := testutil.NewTestVote(block, nodes[2])
	require.NoError(t, err)

	mismatched := *vote
	mismatched.Vote.Round++
	require.NoError(t, e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{Vote: mismatched, Block: block},
	}, nodes[2]))
	require.False(t, storedFutureBlock)

	require.NoError(t, e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{Vote: *vote, Block: block},
	}, nodes[2]))
	require.True(t, storedFutureBlock)
	require.False(t, alreadyReceived)
}

// TestNotarizedNotFinalizedTipCausesEmptyBlockProposal verifies that when the leader
// has a notarized-but-not-finalized tip and no transactions are available, it proposes
// an empty block instead of stalling.
func TestNotarizedNotFinalizedTipCausesEmptyBlockProposal(t *testing.T) {
	for _, testCase := range []struct {
		name            string
		finalizedRounds []bool
	}{
		{
			name:            "round 0 finalized, round 1 only notarized",
			finalizedRounds: []bool{true, false},
		},
		{
			name:            "round 0 finalized, round 1 only notarized, round 2 only notarized",
			finalizedRounds: []bool{true, false, false},
		},
		{
			name:            "round 0 finalized, round 1 only notarized, round 2 finalized",
			finalizedRounds: []bool{true, false, true},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			nodes := []NodeID{{1}, {2}, {3}, {4}}
			// Pick the node's ID such that it will be the leader in the next round.
			nodeID := nodes[len(testCase.finalizedRounds)]

			bb := testutil.NewTestControlledBlockBuilder(t)
			recordingComm := &recordingComm{
				Communication:     testutil.NewNoopComm(nodes),
				BroadcastMessages: make(chan *Message, 100),
				SentMessages:      make(chan *Message, 100),
			}
			conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodeID, recordingComm, bb)
			conf.MaxProposalWait = 50 * time.Millisecond

			e, err := NewEpoch(conf)
			require.NoError(t, err)
			t.Cleanup(e.Stop)
			require.NoError(t, e.Start())

			blocks := make(map[uint64]VerifiedBlock)

			var proposedBlockSeq uint64

			for r, finalized := range testCase.finalizedRounds {
				if finalized {
					notarizeAndFinalizeRound(t, e, &bb.TestBlockBuilder)
					continue
				}
				block := notarizeRoundNotFinalized(t, e, nodes, uint64(r))
				blocks[uint64(r)] = block
			}

			// Because there is a notarized-but-not-finalized tip, after MaxProposalWait the empty-block
			// builder makes it propose an empty block.
			timeout := time.After(30 * time.Second)
			var proposal *VerifiedBlockMessage
			for proposal == nil {
				select {
				case msg := <-recordingComm.BroadcastMessages:
					if msg.VerifiedBlockMessage != nil {
						proposal = msg.VerifiedBlockMessage
						proposedBlockSeq = proposal.VerifiedBlock.BlockHeader().Seq
					}
				case <-timeout:
					require.FailNow(t, "timed out waiting for a block proposal")
				}
			}
			require.Equal(t, uint64(len(testCase.finalizedRounds)), proposal.VerifiedBlock.BlockHeader().Round)
			blocks[proposal.VerifiedBlock.BlockHeader().Round] = proposal.VerifiedBlock

			testCase.finalizedRounds = append(testCase.finalizedRounds, false)

			for r, finalized := range testCase.finalizedRounds {
				if finalized {
					continue
				}

				// Send finalize votes to the node for the notarized-but-not-finalized rounds,
				// so that it can finalize them and advance to the next round.
				// A message from the epoch node itself is dropped with a warning, so skip it.
				for _, signer := range nodes {
					if signer.Equals(nodeID) {
						continue
					}
					testutil.InjectTestFinalizeVote(t, e, blocks[uint64(r)], signer)
				}
			}

			// Make sure the empty block proposal is committed to storage
			conf.Storage.(*testutil.InMemStorage).WaitForBlockCommit(proposedBlockSeq)
		})
	}
}

// TestNotarizedNotFinalizedTipStuckLeaderCausesEmptyNotarization verifies that
// a node with a notarized-but-not-finalized tip with a stuck leader
// and no pending transactions, the node still times out and casts an empty vote for the
// round.
func TestNotarizedNotFinalizedTipStuckLeaderCausesEmptyNotarization(t *testing.T) {
	for _, testCase := range []struct {
		name            string
		finalizedRounds []bool
	}{
		{
			name:            "round 0 finalized, round 1 only notarized",
			finalizedRounds: []bool{true, false},
		},
		{
			name:            "round 0 finalized, round 1 only notarized, round 2 only notarized",
			finalizedRounds: []bool{true, false, false},
		},
		{
			name:            "round 0 finalized, round 1 only notarized, round 2 finalized",
			finalizedRounds: []bool{true, false, true},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			nodes := []NodeID{{1}, {2}, {3}, {4}}
			bb := testutil.NewTestBlockBuilder()
			conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)
			conf.MaxProposalWait = 50 * time.Millisecond
			startTime := conf.StartTime

			e, err := NewEpoch(conf)
			require.NoError(t, err)
			t.Cleanup(e.Stop)
			require.NoError(t, e.Start())

			for r, finalized := range testCase.finalizedRounds {
				if finalized {
					notarizeAndFinalizeRound(t, e, bb)
					continue
				}
				notarizeRoundNotFinalized(t, e, nodes, uint64(r))
			}

			nextRound := uint64(len(testCase.finalizedRounds))

			// The node is now a follower whose leader is stuck, with no pending
			// transactions. The notarized-but-not-finalized tip must still make it time out and
			// cast an empty vote for round 1.
			testutil.WaitForBlockProposerTimeout(t, e, &startTime, nextRound)
			wal.AssertEmptyVote(nextRound)

			// Its empty vote plus a quorum of others assembles an empty notarization.
			for _, from := range []NodeID{nodes[1], nodes[2]} {
				vote := ToBeSignedEmptyVote{EmptyVoteMetadata: EmptyVoteMetadata{Round: nextRound, Epoch: e.Epoch}}
				sig, err := vote.Sign(&testutil.TestSigner{})
				require.NoError(t, err)
				ev := &EmptyVote{Vote: vote, Signature: Signature{Signer: from, Value: sig}}
				require.NoError(t, e.HandleMessage(&Message{EmptyVoteMessage: ev}, from))
			}

			require.Equal(t, EmptyNotarizationRecordType, wal.AssertNotarization(nextRound))
		})
	}
}

// notarizeRoundNotFinalized drives the epoch node (which must be a follower for the given
// round) to notarize the round's block without finalizing it, leaving a
// notarized-but-not-finalized round in the rounds map.
func notarizeRoundNotFinalized(t *testing.T, e *Epoch, nodes []NodeID, round uint64) VerifiedBlock {
	leader := LeaderForRound(nodes, round)
	require.False(t, e.ID.Equals(leader), "epoch node must be a follower for the notarized round")

	md := e.Metadata()
	require.Equal(t, round, md.Round)
	block := testutil.NewTestBlock(md, NewBlacklist(uint16(len(nodes)), DefaultMaxBlacklistedRounds))

	vote, err := testutil.NewTestVote(block, leader)
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{Block: block, Vote: *vote},
	}, leader))

	validators := e.Comm.Validators()
	quorum := Quorum(len(validators))
	sigAggr := e.SignatureAggregatorCreator(validators)
	notarization, err := testutil.NewNotarization(e.Logger, sigAggr, block, validators.NodeIDs()[:quorum])
	require.NoError(t, err)
	// Deliver the notarization from any node other than ourselves (a notarization from
	// self is ignored).
	testutil.InjectTestNotarization(t, e, notarization, leader)

	e.WAL.(*testutil.TestWAL).AssertNotarization(round)
	return block
}

// TestEpochRejectsReplicatedQuorumRoundWithMismatchedHeader asserts that a replicated quorum round whose
// notarization or finalization matches the block's digest but not the rest of its header is rejected before
// anything is persisted: no block record and no notarization is written to the WAL, and no block is committed.
// The genuine quorum round for the same block is then accepted.
func TestEpochRejectsReplicatedQuorumRoundWithMismatchedHeader(t *testing.T) {
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	blacklist := NewBlacklist(uint16(len(nodes)), DefaultMaxBlacklistedRounds)

	mutations := []struct {
		name   string
		mutate func(*BlockHeader)
	}{
		{name: "round", mutate: func(h *BlockHeader) { h.Round++ }},
		{name: "seq", mutate: func(h *BlockHeader) { h.Seq++ }},
		{name: "epoch", mutate: func(h *BlockHeader) { h.Epoch++ }},
	}

	for _, quorumType := range []string{"notarization", "finalization"} {
		for _, m := range mutations {
			t.Run(quorumType+" with mismatched "+m.name, func(t *testing.T) {
				// Our node leads round 3 only, so it neither proposes the block below nor builds one after it.
				conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[3], testutil.NewNoopComm(nodes), testutil.NewTestBlockBuilder())
				conf.ReplicationEnabled = true
				e, err := NewEpoch(conf)
				require.NoError(t, err)
				require.NoError(t, e.Start())
				t.Cleanup(e.Stop)

				block := testutil.NewTestBlock(ProtocolMetadata{Round: 0, Seq: 0}, blacklist)
				sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
				notarization, err := testutil.NewNotarization(e.Logger, sigAggr, block, nodes)
				require.NoError(t, err)
				finalization, _ := testutil.NewFinalizationRecord(t, sigAggr, block, nodes)

				// The quorum certificate refers to the block's digest, but one header field disagrees with the block.
				mismatched := QuorumRound{Block: block}
				genuine := QuorumRound{Block: block}
				if quorumType == "notarization" {
					bad := notarization
					m.mutate(&bad.Vote.BlockHeader)
					mismatched.Notarization = &bad
					genuine.Notarization = &notarization
				} else {
					bad := finalization
					m.mutate(&bad.Finalization.BlockHeader)
					mismatched.Finalization = &bad
					genuine.Finalization = &finalization
				}

				replicate := func(qr QuorumRound) {
					require.NoError(t, e.HandleMessage(&Message{
						ReplicationResponse: &ReplicationResponse{Data: []QuorumRound{qr}},
					}, nodes[0]))
				}

				replicate(mismatched)
				require.Never(t, func() bool {
					records, err := wal.ReadAll()
					require.NoError(t, err)
					return len(records) > 0 || storage.NumBlocks() > 0
				}, 500*time.Millisecond, 50*time.Millisecond,
					"a %s whose %s does not match the block was persisted", quorumType, m.name)

				// The same block with its genuine quorum certificate is accepted.
				replicate(genuine)
				if quorumType == "notarization" {
					wal.AssertNotarization(block.Metadata.Round)
				} else {
					storage.WaitForBlockCommit(block.Metadata.Seq)
				}
			})
		}
	}
}

// TestEpochDropsConsensusMessagesFromOtherEpochs ensures that every consensus message
// type declaring an epoch other than ours is dropped before reaching its handler,
// while the same message carrying our epoch is processed.
func TestEpochDropsConsensusMessagesFromOtherEpochs(t *testing.T) {
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	leader := nodes[0] // leader of round 0
	quorum := Quorum(len(nodes))

	// blockAt builds a block for round 0 that declares the given epoch.
	blockAt := func(e *Epoch, epoch uint64) *testutil.TestBlock {
		md := e.Metadata()
		md.Epoch = epoch
		return testutil.NewTestBlock(md, emptyBlacklist)
	}

	tests := []struct {
		name  string
		from  NodeID
		build func(t *testing.T, e *Epoch, epoch uint64) *Message
	}{
		{
			name: "block message",
			from: leader,
			build: func(t *testing.T, e *Epoch, epoch uint64) *Message {
				block := blockAt(e, epoch)
				vote, err := testutil.NewTestVote(block, leader)
				require.NoError(t, err)
				return &Message{BlockMessage: &BlockMessage{Vote: *vote, Block: block}}
			},
		},
		{
			name: "vote message",
			from: nodes[2],
			build: func(t *testing.T, e *Epoch, epoch uint64) *Message {
				vote, err := testutil.NewTestVote(blockAt(e, epoch), nodes[2])
				require.NoError(t, err)
				return &Message{VoteMessage: vote}
			},
		},
		{
			name: "empty vote message",
			from: nodes[2],
			build: func(t *testing.T, e *Epoch, epoch uint64) *Message {
				vote := ToBeSignedEmptyVote{EmptyVoteMetadata: EmptyVoteMetadata{Round: e.Metadata().Round, Epoch: epoch}}
				sig, err := vote.Sign(&testutil.TestSigner{})
				require.NoError(t, err)
				return &Message{EmptyVoteMessage: &EmptyVote{
					Vote:      vote,
					Signature: Signature{Signer: nodes[2], Value: sig},
				}}
			},
		},
		{
			name: "notarization",
			from: nodes[2],
			build: func(t *testing.T, e *Epoch, epoch uint64) *Message {
				sigAggr := e.SignatureAggregatorCreator(e.Comm.Validators())
				notarization, err := testutil.NewNotarization(e.Logger, sigAggr, blockAt(e, epoch), nodes[:quorum])
				require.NoError(t, err)
				return &Message{Notarization: &notarization}
			},
		},
		{
			name: "empty notarization",
			from: nodes[2],
			build: func(t *testing.T, e *Epoch, epoch uint64) *Message {
				emptyNotarization := testutil.NewEmptyNotarization(nodes[:quorum], e.Metadata().Round)
				emptyNotarization.Vote.Epoch = epoch
				return &Message{EmptyNotarization: emptyNotarization}
			},
		},
		{
			name: "finalize vote",
			from: nodes[2],
			build: func(t *testing.T, e *Epoch, epoch uint64) *Message {
				return &Message{FinalizeVote: testutil.NewTestFinalizeVote(t, blockAt(e, epoch), nodes[2])}
			},
		},
		{
			name: "finalization",
			from: nodes[2],
			build: func(t *testing.T, e *Epoch, epoch uint64) *Message {
				sigAggr := e.SignatureAggregatorCreator(e.Comm.Validators())
				finalization, _ := testutil.NewFinalizationRecord(t, sigAggr, blockAt(e, epoch), nodes[:quorum])
				return &Message{Finalization: &finalization}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bb := testutil.NewTestBlockBuilder()
			// nodes[1] is not the leader of round 0, so it never proposes on its own.
			conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[1], testutil.NewNoopComm(nodes), bb)

			var dropped atomic.Int32
			conf.Logger.(*testutil.TestLogger).Intercept(func(entry zapcore.Entry) error {
				if entry.Message == "Dropping consensus message from a different epoch" {
					dropped.Add(1)
				}
				return nil
			})

			e, err := NewEpoch(conf)
			require.NoError(t, err)
			t.Cleanup(e.Stop)
			require.NoError(t, e.Start())

			ourEpoch := e.Metadata().Epoch
			foreignEpoch := ourEpoch + 1

			// A message from another epoch is dropped and leaves no trace.
			require.NoError(t, e.HandleMessage(tt.build(t, e, foreignEpoch), tt.from))
			require.Equal(t, int32(1), dropped.Load(), "message from epoch %d should have been dropped", foreignEpoch)
			records, err := wal.WriteAheadLog.ReadAll()
			require.NoError(t, err)
			require.Empty(t, records, "a dropped message must not reach the WAL")
			require.Zero(t, storage.NumBlocks())
			require.Zero(t, e.Metadata().Round)

			// The very same message for our epoch is not dropped.
			require.NoError(t, e.HandleMessage(tt.build(t, e, ourEpoch), tt.from))
			require.Equal(t, int32(1), dropped.Load(), "message from our own epoch %d must not be dropped", ourEpoch)
		})
	}
}

// TestEpochDropsConsensusMessagesFromOtherEpochsEndToEnd runs a full round with
// messages of a foreign epoch interleaved with the legitimate ones, and ensures only
// the legitimate ones drive the epoch forward: the foreign proposal is not voted on,
// and the block that ends up committed is the one from our epoch.
func TestEpochDropsConsensusMessagesFromOtherEpochsEndToEnd(t *testing.T) {
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	leader := nodes[0]
	quorum := Quorum(len(nodes))

	bb := testutil.NewTestBlockBuilder()
	conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[1], testutil.NewNoopComm(nodes), bb)

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	md := e.Metadata()
	foreignMD := md
	foreignMD.Epoch = md.Epoch + 1

	// The leader proposes two blocks for round 0: one for a foreign epoch, one for ours.
	foreignBlock := testutil.NewTestBlock(foreignMD, emptyBlacklist)
	block := testutil.NewTestBlock(md, emptyBlacklist)
	require.NotEqual(t, foreignBlock.BlockHeader().Digest, block.BlockHeader().Digest)

	foreignVote, err := testutil.NewTestVote(foreignBlock, leader)
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{BlockMessage: &BlockMessage{Vote: *foreignVote, Block: foreignBlock}}, leader))

	records, err := wal.WriteAheadLog.ReadAll()
	require.NoError(t, err)
	require.Empty(t, records, "the foreign proposal must not be recorded")

	vote, err := testutil.NewTestVote(block, leader)
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{BlockMessage: &BlockMessage{Vote: *vote, Block: block}}, leader))
	wal.AssertBlockProposal(md.Round)

	// A foreign notarization for the foreign block does not advance the round,
	// but a notarization of our block does.
	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	foreignNotarization, err := testutil.NewNotarization(e.Logger, sigAggr, foreignBlock, nodes[:quorum])
	require.NoError(t, err)
	testutil.InjectTestNotarization(t, e, foreignNotarization, nodes[2])
	require.Equal(t, md.Round, e.Metadata().Round)
	require.False(t, wal.ContainsNotarization(md.Round))

	notarization, err := testutil.NewNotarization(e.Logger, sigAggr, block, nodes[:quorum])
	require.NoError(t, err)
	testutil.InjectTestNotarization(t, e, notarization, nodes[2])
	wal.AssertNotarization(md.Round)
	testutil.WaitToEnterRound(t, e, md.Round+1)

	// A foreign finalization does not commit anything, ours commits our block.
	foreignFinalization, _ := testutil.NewFinalizationRecord(t, sigAggr, foreignBlock, nodes[:quorum])
	testutil.InjectTestFinalization(t, e, &foreignFinalization, nodes[2])
	require.Zero(t, storage.NumBlocks())

	finalization, _ := testutil.NewFinalizationRecord(t, sigAggr, block, nodes[:quorum])
	testutil.InjectTestFinalization(t, e, &finalization, nodes[2])
	committed := storage.WaitForBlockCommit(md.Seq)
	require.Equal(t, block.BlockHeader().Digest, committed.BlockHeader().Digest)
}

// TestEpochIgnoresReplicatedQuorumRoundsFromOtherEpochs ensures that quorum rounds
// carried in replication responses are ignored when they belong to a different
// epoch, both for finalized blocks and for empty notarizations, and that the
// same quorum rounds for our epoch are processed.
func TestEpochIgnoresReplicatedQuorumRoundsFromOtherEpochs(t *testing.T) {
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	quorum := Quorum(len(nodes))

	bb := testutil.NewTestBlockBuilder()
	// nodes[1] is not the leader of round 0, so it never proposes on its own.
	conf, _, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[1], testutil.NewNoopComm(nodes), bb)
	conf.ReplicationEnabled = true

	var ignored atomic.Int32
	conf.Logger.(*testutil.TestLogger).Intercept(func(entry zapcore.Entry) error {
		if entry.Message == "Received quorum round for a different epoch, ignoring" {
			ignored.Add(1)
		}
		return nil
	})

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	md := e.Metadata()
	foreignMD := md
	foreignMD.Epoch = md.Epoch + 1
	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())

	// A finalized block of a foreign epoch at the next sequence to commit is ignored.
	foreignBlock := testutil.NewTestBlock(foreignMD, emptyBlacklist)
	foreignFinalization, _ := testutil.NewFinalizationRecord(t, sigAggr, foreignBlock, nodes[:quorum])
	require.NoError(t, e.HandleMessage(&Message{ReplicationResponse: &ReplicationResponse{
		Data: []QuorumRound{{Block: foreignBlock, Finalization: &foreignFinalization}},
	}}, nodes[2]))
	require.Equal(t, int32(1), ignored.Load())
	require.Zero(t, storage.NumBlocks(), "a finalized block from another epoch must not be committed")
	require.Equal(t, md.Round, e.Metadata().Round)

	// An empty notarization of a foreign epoch for our round is ignored too,
	// whether it is carried as data or as the latest round.
	foreignEmptyNotarization := testutil.NewEmptyNotarization(nodes[:quorum], md.Round)
	foreignEmptyNotarization.Vote.Epoch = foreignMD.Epoch
	require.NoError(t, e.HandleMessage(&Message{ReplicationResponse: &ReplicationResponse{
		Data:        []QuorumRound{{EmptyNotarization: foreignEmptyNotarization}},
		LatestRound: &QuorumRound{EmptyNotarization: foreignEmptyNotarization},
	}}, nodes[2]))
	require.Equal(t, int32(3), ignored.Load())
	require.Equal(t, md.Round, e.Metadata().Round, "an empty notarization from another epoch must not advance the round")

	// The same finalized block for our epoch is committed.
	block := testutil.NewTestBlock(md, emptyBlacklist)
	finalization, _ := testutil.NewFinalizationRecord(t, sigAggr, block, nodes[:quorum])
	require.NoError(t, e.HandleMessage(&Message{ReplicationResponse: &ReplicationResponse{
		Data: []QuorumRound{{Block: block, Finalization: &finalization}},
	}}, nodes[2]))
	committed := storage.WaitForBlockCommit(md.Seq)
	require.Equal(t, block.BlockHeader().Digest, committed.BlockHeader().Digest)
	testutil.WaitToEnterRound(t, e, md.Round+1)

	// And an empty notarization for our epoch advances the round.
	emptyNotarization := testutil.NewEmptyNotarization(nodes[:quorum], md.Round+1)
	emptyNotarization.Vote.Epoch = md.Epoch
	require.NoError(t, e.HandleMessage(&Message{ReplicationResponse: &ReplicationResponse{
		LatestRound: &QuorumRound{EmptyNotarization: emptyNotarization},
	}}, nodes[2]))
	testutil.WaitToEnterRound(t, e, md.Round+2)
	require.Equal(t, int32(3), ignored.Load(), "quorum rounds from our own epoch must not be ignored")
}

// TestEpochVoteSentTwiceKeepsBufferedVote asserts that a node which voted before we had
// the round cannot displace that vote by sending a second one.
func TestEpochVoteSentTwiceKeepsBufferedVote(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}

	forged := []byte("forged signature")

	// nodes[0] leads round 0, so we hold messages for that round until its proposal lands.
	comm := &recordingComm{Communication: testutil.NewNoopComm(nodes), BroadcastMessages: make(chan *Message, 100)}
	conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[1], comm, bb)
	conf.Verifier = &rejectingVerifier{rejected: forged}

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	b, ok := bb.BuildBlock(context.Background(), e.Metadata(), emptyBlacklist)
	require.True(t, ok)
	block := b.(Block)

	// We have no round for these votes yet, so they are held without being verified.
	vote, err := testutil.NewTestVote(block, nodes[2])
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{VoteMessage: vote}, nodes[2]))
	require.NoError(t, e.HandleMessage(&Message{VoteMessage: &Vote{
		Vote:      vote.Vote,
		Signature: Signature{Signer: nodes[2], Value: forged},
	}}, nodes[2]))

	// The leader's proposal gives us the round along with its own vote, and the held vote
	// is processed. Together with our own vote that is a quorum.
	leaderVote, err := testutil.NewTestVote(block, nodes[0])
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{BlockMessage: &BlockMessage{
		Vote:  *leaderVote,
		Block: block,
	}}, nodes[0]))

	timeout := time.After(10 * time.Second)
	for {
		select {
		case msg := <-comm.BroadcastMessages:
			if msg.Notarization == nil {
				continue
			}
			return
		case <-timeout:
			t.Fatal("timed out waiting for a notarization to be broadcast")
		}
	}
}

func TestFutureProposalDispatchedOnceAfterReentrantCommit(t *testing.T) {
	// Two proposals for rounds 0 and 1 arrive one after the other.
	// A finalization for round 0 arrives while the proposal for round 0 is still being verified. The
	// finalization is parked in the future messages map and is consumed by the verification task
	// itself once it stores the proposal, so the commit (and the round change) happen inside the
	// verification task's call to maybeLoadFutureMessages.
	//
	// That commit calls startRound, which calls maybeLoadFutureMessages again (nested) and dispatches
	// the parked proposal for round 1. When the nested call returns, the outer maybeLoadFutureMessages
	// notices the round changed and iterates again, but the parked proposal is still in the map,
	// because it is only removed from the map by the verification task, so it
	// dispatches the task to verify the very same proposal a second time.
	// The result is two verification tasks for one block: the second one re-verifies an already
	// verified block and fails to store the proposal because the round already exists.
	nodes := make([]NodeID, 10)
	for i := range nodes {
		nodes[i] = NodeID{byte(i + 1)}
	}
	// The epoch node leads only round 9, so it never proposes any of the blocks used here.
	epochNode := LeaderForRound(nodes, 9)
	quorum := Quorum(len(nodes))
	blacklist := NewBlacklist(uint16(len(nodes)), DefaultMaxBlacklistedRounds)

	bb := testutil.NewTestBlockBuilder()
	comm := &recordingComm{
		Communication:     testutil.NewNoopComm(NodeIDs(nodes)),
		SentMessages:      make(chan *Message, 1000),
		BroadcastMessages: make(chan *Message, 1000),
	}
	conf, _, storage := testutil.DefaultTestNodeEpochConfig(t, epochNode, comm, bb)
	conf.ReplicationEnabled = true

	// Count, by log message, how the proposals are dispatched and verified.
	var scheduledVerifications, repeatedVerifications, rejectedProposals, finalizationsToReplication atomic.Int32
	l := conf.Logger.(*testutil.TestLogger)
	l.Intercept(func(entry zapcore.Entry) error {
		switch {
		case entry.Message == "Scheduling block verification":
			scheduledVerifications.Add(1)
		case strings.Contains(entry.Message, "Attempted to verify an already verified block"):
			repeatedVerifications.Add(1)
		case strings.Contains(entry.Message, "Already received block for round"):
			rejectedProposals.Add(1)
		case strings.Contains(entry.Message, "Received finalization for a pending or future round"):
			finalizationsToReplication.Add(1)
		}
		return nil
	})

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	e.ReplicationEnabled = true
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	// Two blocks: block 0 (round 0, led by node 1) and block 1 (round 1, led by node 2).
	blocks := make([]*testutil.TestBlock, 2)
	var prev Digest
	for i := uint64(0); i < 2; i++ {
		blocks[i] = testutil.NewTestBlock(ProtocolMetadata{Round: i, Seq: i, Prev: prev}, blacklist)
		prev = blocks[i].BlockHeader().Digest
	}
	// Hold block 0 inside Verify so the finalization for it arrives while it is still being verified.
	block0Verifying := make(chan struct{})
	blocks[0].VerificationDelay = block0Verifying

	for i := uint64(0); i < 2; i++ {
		leader := LeaderForRound(nodes, i)
		vote, err := testutil.NewTestVote(blocks[i], leader)
		require.NoError(t, err)
		require.NoError(t, e.HandleMessage(&Message{
			BlockMessage: &BlockMessage{Block: blocks[i], Vote: *vote},
		}, leader))
	}

	// Block 0 cannot be stored before its Verify returns, so this finalization is guaranteed to find
	// no round object and to be parked in the future messages map for the verification task to consume.
	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	finalization0, _ := testutil.NewFinalizationRecord(t, sigAggr, blocks[0], nodes[:quorum])
	require.NoError(t, e.HandleMessage(&Message{Finalization: &finalization0}, nodes[0]))
	require.Zero(t, finalizationsToReplication.Load(), "the finalization should have been parked, not handed to replication")

	// Release block 0. Its verification task stores the proposal, consumes the parked finalization,
	// commits block 0 and starts round 1 - all inside the same task.
	close(block0Verifying)
	storage.WaitForBlockCommit(0)

	// Wait for the round 1 proposal to be verified and voted on, which is the end of its verification task.
	require.Eventually(t, func() bool {
		for {
			select {
			case msg := <-comm.BroadcastMessages:
				if msg.VoteMessage != nil && msg.VoteMessage.Vote.Round == 1 {
					return true
				}
			default:
				return false
			}
		}
	}, 5*time.Second, 10*time.Millisecond, "the epoch should vote on the round 1 proposal")

	// Give any extra, spurious verification task a chance to run before checking the counters.
	require.Never(t, func() bool {
		return repeatedVerifications.Load() > 0 || rejectedProposals.Load() > 0
	}, 500*time.Millisecond, 50*time.Millisecond,
		"the round 1 proposal was dispatched for verification more than once: "+
			"re-verified an already verified block, then failed to store it because the round already exists")

	// One verification per block: block 0 and block 1.
	require.EqualValues(t, 2, scheduledVerifications.Load(), "each proposal should be scheduled for verification exactly once")

	// Sanity: the parked-and-consumed path still commits when the finalization for round 1 arrives.
	finalization1, _ := testutil.NewFinalizationRecord(t, sigAggr, blocks[1], nodes[:quorum])
	require.NoError(t, e.HandleMessage(&Message{Finalization: &finalization1}, nodes[0]))
	storage.WaitForBlockCommit(1)
}

// TestReplicationStatePrunesCommittedSequences tests we prune old sequences from replication state.
func TestReplicationStatePrunesCommittedSequences(t *testing.T) {
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	sigAggr := &testutil.TestSignatureAggregator{N: len(nodes)}
	rng, err := NewRandomSource()
	require.NoError(t, err)

	replicationState := NewReplicationState(testutil.MakeLogger(t, 1), testutil.NewNoopComm(nodes), nodes[0], 10, true, time.Now(), &sync.Mutex{}, rng)
	defer replicationState.Close()

	for seq := uint64(1); seq <= 3; seq++ {
		block := testutil.NewTestBlock(ProtocolMetadata{Seq: seq, Round: seq}, emptyBlacklist)
		finalization, _ := testutil.NewFinalizationRecord(t, sigAggr, block, nodes)
		replicationState.StoreQuorumRound(&QuorumRound{Block: block, Finalization: &finalization})

		_, _, exists := replicationState.GetFinalizedBlockForSequence(seq)
		require.True(t, exists)
	}

	replicationState.MaybeAdvanceState(3, 3, 2)

	for seq := uint64(1); seq <= 2; seq++ {
		_, _, exists := replicationState.GetFinalizedBlockForSequence(seq)
		require.Falsef(t, exists, "seq %d was committed but is still stored", seq)
	}

	_, _, exists := replicationState.GetFinalizedBlockForSequence(3)
	require.True(t, exists, "the next sequence to commit must not be pruned")
}

// TestReplicationRequestsKeepHighestObservedRound asserts a replayed notarization
// for a lower round does not lower the LatestRound sent in replication requests.
func TestReplicationRequestsKeepHighestObservedRound(t *testing.T) {
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	comm := &recordingComm{Communication: testutil.NewNoopComm(nodes)}
	conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], comm, testutil.NewTestBlockBuilder())
	conf.ReplicationEnabled = true

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	// an empty notarization for round 1000 arrives while we are at round 0
	require.NoError(t, e.HandleMessage(&Message{
		EmptyNotarization: testutil.NewEmptyNotarization(nodes[1:], 1000),
	}, nodes[1]))

	// only record the request sent after the replayed notarization
	comm.SentMessages = make(chan *Message, 1)

	// a replayed notarization for round 500 must not lower the advertised round
	block := testutil.NewTestBlock(ProtocolMetadata{Round: 500, Seq: 450}, emptyBlacklist)
	notarization, err := testutil.NewNotarization(conf.Logger, e.SignatureAggregatorCreator(conf.Comm.Validators()), block, nodes[1:])
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{Notarization: &notarization}, nodes[1]))

	msg := <-comm.SentMessages
	require.Equal(t, uint64(1000), msg.ReplicationRequest.LatestRound)
}

// TestEpochFinalizeVoteSentTwiceKeepsBufferedVote asserts that the first finalize vote
// a node sends before we have the round is the one kept, so a second one cannot displace it.
// Whether the round finalizes depends only on which of the two arrived first.
func TestEpochFinalizeVoteSentTwiceKeepsBufferedVote(t *testing.T) {
	for _, tc := range []struct {
		name         string
		invalidFirst bool
		expectCommit bool
	}{
		{name: "valid vote first is kept", invalidFirst: false, expectCommit: true},
		{name: "invalid vote first is kept", invalidFirst: true, expectCommit: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bb := testutil.NewTestBlockBuilder()
			nodes := []NodeID{{1}, {2}, {3}, {4}}

			invalidSig := []byte("invalid signature")

			// nodes[0] leads round 0, so we hold messages for that round until its proposal lands.
			conf, _, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[1], testutil.NewNoopComm(nodes), bb)
			conf.Verifier = &rejectingVerifier{rejected: invalidSig}

			e, err := NewEpoch(conf)
			require.NoError(t, err)
			t.Cleanup(e.Stop)
			require.NoError(t, e.Start())

			b, ok := bb.BuildBlock(context.Background(), e.Metadata(), emptyBlacklist)
			require.True(t, ok)
			block := b.(Block)

			sendInvalid := func() {
				require.NoError(t, e.HandleMessage(&Message{FinalizeVote: &FinalizeVote{
					Finalization: ToBeSignedFinalization{BlockHeader: b.BlockHeader()},
					Signature:    Signature{Signer: nodes[2], Value: invalidSig},
				}}, nodes[2]))
			}

			if tc.invalidFirst {
				sendInvalid()
				testutil.InjectTestFinalizeVote(t, e, b, nodes[2])
			} else {
				testutil.InjectTestFinalizeVote(t, e, b, nodes[2])
				sendInvalid()
			}
			testutil.InjectTestFinalizeVote(t, e, b, nodes[3])

			vote, err := testutil.NewTestVote(block, nodes[0])
			require.NoError(t, err)
			require.NoError(t, e.HandleMessage(&Message{BlockMessage: &BlockMessage{
				Vote:  *vote,
				Block: block,
			}}, nodes[0]))

			// A third vote notarizes the round, which makes us cast our own finalize vote.
			testutil.InjectTestVote(t, e, b, nodes[2])

			if tc.expectCommit {
				require.Eventually(t, func() bool {
					return storage.NumBlocks() == 1
				}, 10*time.Second, 10*time.Millisecond, "round was never finalized")
				return
			}

			// Only nodes[3] and our own vote are valid, one short of a quorum.
			require.Never(t, func() bool {
				return storage.NumBlocks() > 0
			}, 500*time.Millisecond, 50*time.Millisecond, "round finalized with an invalid vote")
		})
	}
}

// TestEpochRepliesWithFinalizationForStaleFinalizeVote asserts that a node which already indexed a
// round answers a late finalize vote for that round with the latest finalization, even if the round has been pruned from memory.
func TestEpochRepliesWithFinalizationForStaleFinalizeVote(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	comm := &recordingComm{Communication: testutil.NewNoopComm(nodes), SentMessages: make(chan *Message, 100)}
	conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], comm, bb)
	conf.MaxRoundWindow = 5

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	staleBlock, _ := notarizeAndFinalizeRound(t, e, bb)
	for range 6 {
		notarizeAndFinalizeRound(t, e, bb)
	}
	for len(comm.SentMessages) > 0 {
		<-comm.SentMessages
	}

	testutil.InjectTestFinalizeVote(t, e, staleBlock, nodes[1])
	reply := <-comm.SentMessages
	require.NotNil(t, reply.Finalization)
	require.Equal(t, uint64(6), reply.Finalization.Finalization.Round)
}

// TestEpochFollowerIgnoresStaleBlacklistOnEpochChange checks that a follower does not skip a
// leader because of the previous epoch's blacklist.
//
// The node starts a new epoch right after a sealing block of the previous epoch. That sealing
// block blacklists the leader of the first round of the new epoch. The new epoch starts from
// an empty blacklist, so the follower must wait for the leader's proposal and vote for it,
// rather than declare the leader blacklisted and vote for an empty block right away.
func TestEpochFollowerIgnoresStaleBlacklistOnEpochChange(t *testing.T) {
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	bb := testutil.NewTestBlockBuilder()

	// The sealing block is at round 1, so the new epoch starts at round 2, led by nodes[2].
	// The previous epoch blacklists that very node (f=1, so two accusations suffice).
	staleBlacklist := NewBlacklist(uint16(len(nodes)), DefaultMaxBlacklistedRounds)
	// Two leaders of orbit 1 accuse nodes[2] of timing out in round 2.
	staleTimeout := map[uint16]uint64{2: 2}
	staleBlacklist = staleBlacklist.Next(2, DefaultMaxBlacklistedRounds, staleTimeout, nil)
	staleBlacklist = staleBlacklist.Next(3, DefaultMaxBlacklistedRounds, staleTimeout, nil)
	require.True(t, staleBlacklist.IsNodeSuspected(2))

	epoch0Block := testutil.NewTestBlock(ProtocolMetadata{Epoch: 0, Round: 0, Seq: 0}, NewBlacklist(uint16(len(nodes)), DefaultMaxBlacklistedRounds))
	sealingBlock := testutil.NewTestBlock(ProtocolMetadata{Epoch: 0, Round: 1, Seq: 1, Prev: epoch0Block.Digest}, staleBlacklist)
	sealingBlock.SealingInfo = &SealingBlockInfo{
		ValidatorSet:         NodeIDs(nodes).EqualWeightedNodes(),
		PrevSealingBlockHash: epoch0Block.Digest,
	}

	sigAggregator := &testutil.TestSignatureAggregator{N: len(nodes)}
	epoch0Finalization, _ := testutil.NewFinalizationRecord(t, sigAggregator, epoch0Block, nodes)
	sealingFinalization, _ := testutil.NewFinalizationRecord(t, sigAggregator, sealingBlock, nodes)

	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)
	require.NoError(t, conf.Storage.Index(context.Background(), epoch0Block, epoch0Finalization))
	require.NoError(t, conf.Storage.Index(context.Background(), sealingBlock, sealingFinalization))

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	md := e.Metadata()
	require.Equal(t, uint64(1), md.Epoch)
	require.Equal(t, uint64(2), md.Round)
	leader := nodes[2]
	require.Equal(t, leader, LeaderForRound(nodes, md.Round))

	// Tell the follower it is time for the leader to propose, which starts the proposal timeout,
	// and let half of it pass. The leader is not late yet.
	bb.BlockShouldBeBuilt <- struct{}{}
	e.AdvanceTime(e.StartTime.Add(conf.MaxProposalWait / 2))

	// The leader proposes the first block of the new epoch, with an empty blacklist.
	block := testutil.NewTestBlock(md, NewBlacklist(uint16(len(nodes)), DefaultMaxBlacklistedRounds))
	vote, err := testutil.NewTestVote(block, leader)
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{Vote: *vote, Block: block},
	}, leader))
	wal.AssertBlockProposal(md.Round)

	// A follower that waited for the leader has not voted for an empty block.
	require.False(t, wal.ContainsEmptyVote(md.Round),
		"the follower voted for an empty block in round %d without waiting for the leader, "+
			"treating it as blacklisted based on the previous epoch's sealing block", md.Round)

	// With the leader's vote and one more, the follower's own vote is what completes the quorum.
	testutil.InjectTestVote(t, e, block, nodes[1])
	wal.AssertNotarization(md.Round)
}

// TestEpochLeaderRecordsTimeoutDespiteStaleBlacklistOnEpochChange checks that a timeout in the
// first round of a new epoch is recorded even if the previous epoch's sealing block blacklists
// the leader of that round. The new epoch starts from an empty blacklist, so the node must
// accuse the leader in the next block it builds.
func TestEpochLeaderRecordsTimeoutDespiteStaleBlacklistOnEpochChange(t *testing.T) {
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	nodeCount := uint16(len(nodes))
	bb := testutil.NewTestBlockBuilder()

	// The sealing block is at round 1, so the new epoch starts at round 2, led by nodes[2].
	// The previous epoch blacklists that very node.
	staleBlacklist := NewBlacklist(nodeCount, DefaultMaxBlacklistedRounds)
	// Two leaders of orbit 1 accuse nodes[2] of timing out in round 2.
	staleTimeout := map[uint16]uint64{2: 2}
	staleBlacklist = staleBlacklist.Next(2, DefaultMaxBlacklistedRounds, staleTimeout, nil)
	staleBlacklist = staleBlacklist.Next(3, DefaultMaxBlacklistedRounds, staleTimeout, nil)
	require.True(t, staleBlacklist.IsNodeSuspected(2))

	epoch0Block := testutil.NewTestBlock(ProtocolMetadata{Epoch: 0, Round: 0, Seq: 0}, NewBlacklist(nodeCount, DefaultMaxBlacklistedRounds))
	sealingBlock := testutil.NewTestBlock(ProtocolMetadata{Epoch: 0, Round: 1, Seq: 1, Prev: epoch0Block.Digest}, staleBlacklist)
	sealingBlock.SealingInfo = &SealingBlockInfo{
		ValidatorSet:         NodeIDs(nodes).EqualWeightedNodes(),
		PrevSealingBlockHash: epoch0Block.Digest,
	}

	sigAggregator := &testutil.TestSignatureAggregator{N: len(nodes)}
	epoch0Finalization, _ := testutil.NewFinalizationRecord(t, sigAggregator, epoch0Block, nodes)
	sealingFinalization, _ := testutil.NewFinalizationRecord(t, sigAggregator, sealingBlock, nodes)

	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)
	require.NoError(t, conf.Storage.Index(context.Background(), epoch0Block, epoch0Finalization))
	require.NoError(t, conf.Storage.Index(context.Background(), sealingBlock, sealingFinalization))

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	require.Equal(t, uint64(1), e.Metadata().Epoch)
	require.Equal(t, uint64(2), e.Metadata().Round)

	// Rounds 2 and 3 time out on their leaders, nodes[2] and nodes[3], before any block of the
	// new epoch is finalized. Empty notarizations redeem nobody, so both timeouts stand.
	for _, leader := range []NodeID{nodes[2], nodes[3]} {
		round := e.Metadata().Round
		require.Equal(t, leader, LeaderForRound(nodes, round))
		emptyNotarization := testutil.NewEmptyNotarization(nodes, round)
		emptyNotarization.Vote.Epoch = e.Metadata().Epoch
		require.NoError(t, e.HandleMessage(&Message{
			EmptyNotarization: emptyNotarization,
		}, nodes[1]))
		wal.AssertNotarization(round)
	}

	// Round 4: our node proposes. It observed both leaders time out in this epoch, so it must
	// accuse both, regardless of what the previous epoch's sealing block said about index 2.
	block, _ := notarizeAndFinalizeRound(t, e, bb)
	round := block.BlockHeader().Round
	require.Equal(t, nodes[0], LeaderForRound(nodes, round))

	// The new epoch starts from a fresh blacklist, and our node accuses the leaders of rounds 2 and 3.
	freshBlacklist := NewBlacklist(nodeCount, DefaultMaxBlacklistedRounds)
	expected := freshBlacklist.Next(round, DefaultMaxBlacklistedRounds, map[uint16]uint64{2: 2, 3: 3}, nil)
	actual := block.Blacklist()
	require.True(t, expected.Equals(&actual),
		"the timeout of nodes[2] was not recorded: expected %s, got %s", expected.String(), actual.String())
}

// TestRebroadcastDoesNotFinalizeVoteOnTimedOutRound asserts that a node never casts a finalize vote for a
// round it has cast an empty vote for. Doing both lets an empty notarization and a finalization form for
// the same round, since the two quorums may then overlap only in nodes that voted both ways.
func TestRebroadcastDoesNotFinalizeVoteOnTimedOutRound(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	comm := &recordingComm{
		Communication:     testutil.NewNoopComm(nodes),
		BroadcastMessages: make(chan *Message, 1000),
	}
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], comm, bb)
	conf.ReplicationEnabled = true

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	// Round 0: we lead, and the round is notarized and finalized normally.
	notarizeAndFinalizeRound(t, e, bb)
	require.Equal(t, uint64(1), e.Metadata().Round)

	// Round 1: the leader never proposes, so we time out and cast an empty vote.
	const timedOutRound = uint64(1)
	bb.BlockShouldBeBuilt <- struct{}{}
	now := conf.StartTime
	testutil.WaitForBlockProposerTimeout(t, e, &now, timedOutRound)
	require.True(t, wal.ContainsEmptyVote(timedOutRound))

	// The other three nodes notarized a block for round 1 regardless, and we learn of it through
	// replication. We store it so the round can still be finalized, but we must not vote to finalize it.
	block := testutil.NewTestBlock(e.Metadata(), emptyBlacklist)
	sigAggr := e.SignatureAggregatorCreator(e.Comm.Validators())
	notarization, err := testutil.NewNotarization(e.Logger, sigAggr, block, nodes[1:])
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{ReplicationResponse: &ReplicationResponse{
		Data: []QuorumRound{{Block: block, Notarization: &notarization}},
	}}, nodes[1]))
	wal.AssertNotarization(timedOutRound)
	testutil.WaitToEnterRound(t, e, timedOutRound+1)

	// Everything broadcast so far, including the empty vote, is not what this test is about.
	for len(comm.BroadcastMessages) > 0 {
		<-comm.BroadcastMessages
	}

	// Round 1 is now notarized but not finalized and the round no longer advances, so NotarizationTime
	// rebroadcasts finalize votes. Drive its clock through several timeouts and make sure none of them is
	// for the round we timed out on.
	step := DefaultFinalizeVoteRebroadcastTimeout / 3
	for range 15 {
		now = now.Add(step)
		e.AdvanceTime(now)
		for len(comm.BroadcastMessages) > 0 {
			msg := <-comm.BroadcastMessages
			if msg.FinalizeVote != nil {
				require.NotEqual(t, timedOutRound, msg.FinalizeVote.Finalization.Round,
					"node cast a finalize vote for round %d after casting an empty vote for it", timedOutRound)
			}
		}
	}
}

// TestReplicationLatestSeqBeyondWindowIsNotStored asserts a LatestSeq beyond the round window is not stored.
func TestReplicationLatestSeqBeyondWindowIsNotStored(t *testing.T) {
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	conf, _, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[1], testutil.NewNoopComm(nodes), testutil.NewTestBlockBuilder())
	conf.ReplicationEnabled = true
	conf.MaxRoundWindow = 2

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	md := e.Metadata()
	var chain []QuorumRound
	for seq := uint64(0); seq <= 3; seq++ {
		md.Seq = seq
		md.Round = seq
		block := testutil.NewTestBlock(md, emptyBlacklist)
		finalization, _ := testutil.NewFinalizationRecord(t, sigAggr, block, nodes)
		chain = append(chain, QuorumRound{Block: block, Finalization: &finalization})
		md.Prev = block.BlockHeader().Digest
	}

	// farAheadBlock is past the max round window, so it shouldn't be stored in replication state
	farAheadBlock := testutil.NewTestBlock(chain[3].Block.BlockHeader().ProtocolMetadata, emptyBlacklist)
	farAheadBlock.Data = []byte("far ahead")
	farAheadBlock.ComputeDigest()
	farAheadFinalization, _ := testutil.NewFinalizationRecord(t, sigAggr, farAheadBlock, nodes)
	require.NoError(t, e.HandleMessage(&Message{ReplicationResponse: &ReplicationResponse{
		LatestSeq: &QuorumRound{Block: farAheadBlock, Finalization: &farAheadFinalization},
	}}, nodes[2]))

	require.NoError(t, e.HandleMessage(&Message{ReplicationResponse: &ReplicationResponse{
		Data: chain[:3],
	}}, nodes[2]))
	storage.WaitForBlockCommit(2)

	require.NoError(t, e.HandleMessage(&Message{ReplicationResponse: &ReplicationResponse{
		Data: chain[3:],
	}}, nodes[2]))
	committed := storage.WaitForBlockCommit(3)
	// Ensure the farAheadBlock was not the one committed
	require.Equal(t, chain[3].Block.BlockHeader().Digest, committed.BlockHeader().Digest)
}

// TestReplicationNextSeqNotarizationBeyondWindowIsStored asserts a notarization for the next sequence to commit
// is stored even when its round is beyond the round window, so the round advances past the empty rounds to process it.
func TestReplicationNextSeqNotarizationBeyondWindowIsStored(t *testing.T) {
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[3], testutil.NewNoopComm(nodes), testutil.NewTestBlockBuilder())
	conf.ReplicationEnabled = true
	conf.MaxRoundWindow = 1

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	// Seq 0 is notarized in round 2, which is past our round 0 plus the window.
	md := e.Metadata()
	md.Round = 2
	block := testutil.NewTestBlock(md, emptyBlacklist)
	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	notarization, err := testutil.NewNotarization(e.Logger, sigAggr, block, nodes)
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{ReplicationResponse: &ReplicationResponse{
		Data: []QuorumRound{{Block: block, Notarization: &notarization}},
	}}, nodes[2]))

	require.Equal(t, NotarizationRecordType, wal.AssertNotarization(2))
}

// TestFinalizeAfterNotarizingBeyondMaxRoundWindow asserts that blocks are finalized after the network
// notarizes more consecutive rounds than MaxRoundWindow without finalizing any.
func TestFinalizeAfterNotarizingBeyondMaxRoundWindow(t *testing.T) {
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	net := testutil.NewControlledNetwork(t, nodes)

	var allowFinalizeVotes atomic.Bool
	for _, node := range nodes {
		comm := testutil.NewTestComm(node, net.BasicInMemoryNetwork, func(msg *Message, _, _ NodeID) bool {
			return allowFinalizeVotes.Load() || (msg.FinalizeVote == nil && msg.Finalization == nil)
		})
		testutil.NewControlledSimplexNode(t, node, net, &testutil.TestNodeConfig{Comm: comm})
	}

	net.StartInstances()
	defer net.StopInstances()

	lastSeq := uint64(3 * DefaultMaxRoundWindow)
	for seq := uint64(0); seq <= lastSeq; seq++ {
		for _, n := range net.Instances {
			testutil.WaitToEnterRound(t, n.E, seq)
		}
		net.TriggerLeaderBlockBuilder(seq)
		for _, n := range net.Instances {
			n.WAL.AssertNotarization(seq)
		}
	}
	for _, n := range net.Instances {
		require.Zero(t, n.Storage.NumBlocks())
	}

	allowFinalizeVotes.Store(true)
	require.Eventually(t, func() bool {
		for _, n := range net.Instances {
			if n.Storage.NumBlocks() < lastSeq+1 {
				for _, n := range net.Instances {
					n.AdvanceTime(DefaultFinalizeVoteRebroadcastTimeout / 3)
				}
				return false
			}
		}
		return true
	}, 10*time.Second, 100*time.Millisecond)
}

// TestEpochAdvancesFromEmptyNotarizationWhileVerifying asserts that a proposal whose round is empty notarized
// while it is being verified finishes verifying and does not double increment the round.
func TestEpochAdvancesFromEmptyNotarizationWhileVerifying(t *testing.T) {
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[3], testutil.NewNoopComm(nodes), testutil.NewTestBlockBuilder())

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	block0 := testutil.NewTestBlock(e.Metadata(), emptyBlacklist)
	block0Verifying := make(chan struct{})
	block0.VerificationDelay = block0Verifying
	vote, err := testutil.NewTestVote(block0, nodes[0])
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{BlockMessage: &BlockMessage{Block: block0, Vote: *vote}}, nodes[0]))

	advanceRoundFromEmpty(t, e)
	require.Equal(t, uint64(1), e.Metadata().Round)

	close(block0Verifying)
	wal.AssertBlockProposal(0)
	require.Equal(t, uint64(1), e.Metadata().Round)
	require.Equal(t, uint64(0), e.Metadata().Seq)

	// The block built off the empty notarization is committed
	block1 := testutil.NewTestBlock(e.Metadata(), emptyBlacklist)
	vote, err = testutil.NewTestVote(block1, nodes[1])
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{BlockMessage: &BlockMessage{Block: block1, Vote: *vote}}, nodes[1]))
	wal.AssertBlockProposal(1)

	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	finalization, _ := testutil.NewFinalizationRecord(t, sigAggr, block1, nodes[:Quorum(len(nodes))])
	require.NoError(t, e.HandleMessage(&Message{Finalization: &finalization}, nodes[1]))
	require.Equal(t, block1, storage.WaitForBlockCommit(0))
}

// TestEpochAdvancesFromNotarizationWhileVerifying asserts that a notarization received while the round's proposal
// is being verified advances the epoch once, and the vote on the proposal does not advance it again.
func TestEpochAdvancesFromNotarizationWhileVerifying(t *testing.T) {
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[3], testutil.NewNoopComm(nodes), testutil.NewTestBlockBuilder())

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	block0 := testutil.NewTestBlock(e.Metadata(), emptyBlacklist)
	block0Verifying := make(chan struct{})
	block0.VerificationDelay = block0Verifying
	vote, err := testutil.NewTestVote(block0, nodes[0])
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{BlockMessage: &BlockMessage{Block: block0, Vote: *vote}}, nodes[0]))

	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	notarization, err := testutil.NewNotarization(conf.Logger, sigAggr, block0, nodes[:Quorum(len(nodes))])
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{Notarization: &notarization}, nodes[1]))
	require.Equal(t, uint64(0), e.Metadata().Round)

	// Verification loads the notarization, advances the round, then votes on round 0 under the same lock
	close(block0Verifying)
	wal.AssertNotarization(0)
	require.Equal(t, uint64(1), e.Metadata().Round)
}

// TestReplicatedFinalizationExecutesBlockDependents asserts that a replicated notarized block waiting on
// an unnotarized parent is verified once the parent is finalized through replication.
func TestReplicatedFinalizationExecutesBlockDependents(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[3], testutil.NewNoopComm(nodes), bb)
	conf.ReplicationEnabled = true

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	block0 := testutil.NewTestBlock(e.Metadata(), emptyBlacklist)
	vote0, err := testutil.NewTestVote(block0, nodes[0])
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{BlockMessage: &BlockMessage{Block: block0, Vote: *vote0}}, nodes[0]))
	wal.AssertBlockProposal(0)

	// Round 0 times out and is empty notarized, so we enter round 1 holding block0 without a notarization.
	bb.BlockShouldBeBuilt <- struct{}{}
	testutil.WaitForBlockProposerTimeout(t, e, &e.StartTime, 0)
	require.NoError(t, e.HandleMessage(&Message{EmptyNotarization: testutil.NewEmptyNotarization(nodes, 0)}, nodes[1]))
	testutil.WaitToEnterRound(t, e, 1)

	// We get a replicated notarization for block1, which is a child of block0.
	// It cannot be scheduled for verification because its parent is not notarized,
	// so it waits in the scheduling queue for now.
	md1 := block0.BlockHeader().ProtocolMetadata
	md1.Round, md1.Seq, md1.Prev = 1, 1, block0.BlockHeader().Digest
	block1 := testutil.NewTestBlock(md1, emptyBlacklist)
	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	notarization1, err := testutil.NewNotarization(e.Logger, sigAggr, block1, nodes[0:3])
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{ReplicationResponse: &ReplicationResponse{
		Data: []QuorumRound{{Block: block1, Notarization: &notarization1}},
	}}, nodes[0]))
	require.False(t, wal.ContainsNotarization(1))

	// Finalizing block0 through replication commits it and must execute block1's verification.
	finalization0, _ := testutil.NewFinalizationRecord(t, sigAggr, block0, nodes[0:3])
	require.NoError(t, e.HandleMessage(&Message{ReplicationResponse: &ReplicationResponse{
		Data: []QuorumRound{{Block: block0, Finalization: &finalization0}},
	}}, nodes[0]))
	require.Equal(t, uint64(1), storage.NumBlocks())

	require.Eventually(t, func() bool {
		return wal.ContainsNotarization(1)
	}, 5*time.Second, 10*time.Millisecond, "block1 was not verified after its parent was finalized")

	// block1 was finalized by the rest of the network, which is what allowed block0 to be finalized
	// despite round 0 being empty notarized. We ensure that finalizing block1 through replication commits it.
	finalization1, _ := testutil.NewFinalizationRecord(t, sigAggr, block1, nodes[0:3])
	require.NoError(t, e.HandleMessage(&Message{ReplicationResponse: &ReplicationResponse{
		Data: []QuorumRound{{Block: block1, Finalization: &finalization1}},
	}}, nodes[0]))
	require.Equal(t, uint64(2), storage.NumBlocks())
}

// TestFailedVerificationDoesNotExecuteBlockDependents asserts that a block which fails verification does not
// schedule the blocks that build on it.
func TestFailedVerificationDoesNotExecuteBlockDependents(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[3], testutil.NewNoopComm(nodes), bb)
	conf.ReplicationEnabled = true

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	// Round 0 times out and is empty notarized, so we enter round 1 without having seen block0.
	block0 := testutil.NewTestBlock(e.Metadata(), emptyBlacklist)
	bb.BlockShouldBeBuilt <- struct{}{}
	testutil.WaitForBlockProposerTimeout(t, e, &e.StartTime, 0)
	require.NoError(t, e.HandleMessage(&Message{EmptyNotarization: testutil.NewEmptyNotarization(nodes, 0)}, nodes[1]))
	testutil.WaitToEnterRound(t, e, 1)

	// block1 cannot be scheduled yet because block0 is not notarized, so it waits in the scheduling queue.
	md1 := block0.BlockHeader().ProtocolMetadata
	md1.Round, md1.Seq, md1.Prev = 1, 1, block0.BlockHeader().Digest
	block1 := testutil.NewTestBlock(md1, emptyBlacklist)
	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	notarization1, err := testutil.NewNotarization(e.Logger, sigAggr, block1, nodes[0:3])
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{ReplicationResponse: &ReplicationResponse{
		Data: []QuorumRound{{Block: block1, Notarization: &notarization1}},
	}}, nodes[0]))
	require.False(t, wal.ContainsNotarization(1))

	// block0 arrives but fails verification.
	block0.VerificationError = fmt.Errorf("block0 failed verification")
	notarization0, err := testutil.NewNotarization(e.Logger, sigAggr, block0, nodes[0:3])
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{ReplicationResponse: &ReplicationResponse{
		Data: []QuorumRound{{Block: block0, Notarization: &notarization0}},
	}}, nodes[0]))

	// The verification for the task of block0 failed, so we shouldn't have the notarization of block 1,
	// because it shouldn't have been scheduled for verification.
	require.Never(t, func() bool {
		return wal.ContainsNotarization(1)
	}, 500*time.Millisecond, 10*time.Millisecond, "block1 was verified although its parent failed verification")

	// block0 is replicated again and verifies this time.
	validBlock0 := testutil.NewTestBlock(block0.BlockHeader().ProtocolMetadata, emptyBlacklist)
	require.Equal(t, block0.BlockHeader().Digest, validBlock0.BlockHeader().Digest)
	require.NoError(t, e.HandleMessage(&Message{ReplicationResponse: &ReplicationResponse{
		Data: []QuorumRound{{Block: validBlock0, Notarization: &notarization0}},
	}}, nodes[0]))

	// Now that block0 is verified, block1 should be scheduled for verification and succeed.
	require.Eventually(t, func() bool {
		return wal.ContainsNotarization(0) && wal.ContainsNotarization(1)
	}, 5*time.Second, 10*time.Millisecond, "block1 was not verified after its parent was verified")
}

// TestRestoreFromWALSkipsOlderEpochRecords asserts that when the WAL has records of an older
// epoch with records of the current one, only the current epoch's records are restored.
// Epoch 0 is sealed at seq 5, and before the sealing block was finalized it notarized a block
// at round 6. Epoch 5 also notarized its first block at round 6. Only the latter is verified.
func TestRestoreFromWALSkipsOlderEpochRecords(t *testing.T) {
	ctx := context.Background()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	quorum := Quorum(len(nodes))
	sigAggr := &testutil.TestSignatureAggregator{N: len(nodes)}
	conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), testutil.NewTestBlockBuilder())

	// Each verified block drains one token, so only the current epoch's block should drain one.
	verifications := make(chan struct{}, 2)
	verifications <- struct{}{}
	verifications <- struct{}{}
	conf.BlockDeserializer = &testutil.BlockDeserializer{DelayedVerification: verifications}

	blocks := createBlocks(t, nodes, 6)
	sealingBlock := blocks[5].VerifiedBlock.(*testutil.TestBlock)
	sealingBlock.SealingInfo = &SealingBlockInfo{
		ValidatorSet:         NodeIDs(nodes).EqualWeightedNodes(),
		PrevSealingBlockHash: blocks[0].VerifiedBlock.BlockHeader().Digest,
	}
	for _, block := range blocks {
		require.NoError(t, storage.Index(ctx, block.VerifiedBlock, block.Finalization))
	}

	sealingDigest := sealingBlock.BlockHeader().Digest

	// maybe this stale block was a telock that got written to the wal
	stale := testutil.NewTestBlock(ProtocolMetadata{Epoch: 0, Seq: 6, Round: 6, Prev: sealingDigest}, emptyBlacklist)
	current := testutil.NewTestBlock(ProtocolMetadata{Epoch: 5, Seq: 6, Round: 6, Prev: sealingDigest}, emptyBlacklist)
	for _, block := range []*testutil.TestBlock{stale, current} {
		blockRecord, err := BlockRecord(block.BlockHeader(), block.Bytes())
		require.NoError(t, err)
		notarizationRecord, err := testutil.NewNotarizationRecord(conf.Logger, sigAggr, block, nodes[:quorum])
		require.NoError(t, err)
		require.NoError(t, wal.Append(blockRecord))
		require.NoError(t, wal.Append(notarizationRecord))
	}

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())
	require.Len(t, verifications, 1, "expected only the current epoch's block to be verified")

	md := e.Metadata()
	require.Equal(t, uint64(5), md.Epoch)
	require.Equal(t, uint64(7), md.Round)
	require.Equal(t, uint64(7), md.Seq)
	require.Equal(t, current.BlockHeader().Digest, md.Prev)
}

// TestReplicatedNotarizationForEmptyNotarizedRound asserts that a replicated notarization is persisted
// for a round whose block is already in the rounds map and which is also empty notarized.
func TestReplicatedNotarizationForEmptyNotarizedRound(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[3], testutil.NewNoopComm(nodes), bb)
	conf.ReplicationEnabled = true

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	block0 := testutil.NewTestBlock(e.Metadata(), emptyBlacklist)
	vote0, err := testutil.NewTestVote(block0, nodes[0])
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{BlockMessage: &BlockMessage{Block: block0, Vote: *vote0}}, nodes[0]))
	wal.AssertBlockProposal(0)

	// Round 0 times out and is empty notarized while we hold block0 without a notarization.
	bb.BlockShouldBeBuilt <- struct{}{}
	testutil.WaitForBlockProposerTimeout(t, e, &e.StartTime, 0)
	require.NoError(t, e.HandleMessage(&Message{EmptyNotarization: testutil.NewEmptyNotarization(nodes, 0)}, nodes[1]))
	testutil.WaitToEnterRound(t, e, 1)
	require.False(t, wal.ContainsNotarization(0))

	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	notarization0, err := testutil.NewNotarization(e.Logger, sigAggr, block0, nodes[0:3])
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{ReplicationResponse: &ReplicationResponse{
		Data: []QuorumRound{{Block: block0, Notarization: &notarization0}},
	}}, nodes[0]))

	require.True(t, wal.ContainsNotarization(0))
	require.Equal(t, uint64(1), e.Metadata().Round)
}

// TestEpochRejectsTelockReplicatedWithEmptyNotarization reproduces https://github.com/ava-labs/Simplex/issues/663.
// After the sealing block of the previous epoch is committed, a validator replicates a quorum round that pairs
// the previous epoch's first Telock and its notarization with an empty notarization of the current epoch for the same round.
// The quorum round is taken to be of the current epoch, so the node makes the Telock its chain tip and builds the
// next block on it. Telocks are never committed, so the block built on the Telock is finalized but never committed,
// and the node halts.
func TestEpochRejectsTelockReplicatedWithEmptyNotarization(t *testing.T) {
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	quorum := Quorum(len(nodes))
	blacklist := NewBlacklist(uint16(len(nodes)), DefaultMaxBlacklistedRounds)

	// The previous epoch ends with the sealing block at seq 1, so the node starts in epoch 1 at round 2.
	epoch0Block := testutil.NewTestBlock(ProtocolMetadata{Epoch: 0, Round: 0, Seq: 0}, blacklist)
	sealingBlock := testutil.NewTestBlock(ProtocolMetadata{Epoch: 0, Round: 1, Seq: 1, Prev: epoch0Block.Digest}, blacklist)
	sealingBlock.SealingInfo = &SealingBlockInfo{
		ValidatorSet:         NodeIDs(nodes).EqualWeightedNodes(),
		PrevSealingBlockHash: epoch0Block.Digest,
	}

	sigAggr := &testutil.TestSignatureAggregator{N: len(nodes)}
	epoch0Finalization, _ := testutil.NewFinalizationRecord(t, sigAggr, epoch0Block, nodes)
	sealingFinalization, _ := testutil.NewFinalizationRecord(t, sigAggr, sealingBlock, nodes)

	bb := testutil.NewTestBlockBuilder()
	conf, _, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[0], testutil.NewNoopComm(nodes), bb)
	conf.ReplicationEnabled = true
	require.NoError(t, conf.Storage.Index(context.Background(), epoch0Block, epoch0Finalization))
	require.NoError(t, conf.Storage.Index(context.Background(), sealingBlock, sealingFinalization))

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	md := e.Metadata()
	require.Equal(t, uint64(1), md.Epoch)
	require.Equal(t, uint64(2), md.Round)
	require.Equal(t, uint64(2), md.Seq)

	// The first Telock of the previous epoch follows the sealing block, notarized in the previous epoch.
	telock := testutil.NewTestBlock(ProtocolMetadata{Epoch: 0, Round: md.Round, Seq: md.Seq, Prev: sealingBlock.Digest}, blacklist)
	telockNotarization, err := testutil.NewNotarization(e.Logger, sigAggr, telock, nodes[:quorum])
	require.NoError(t, err)

	// The first round of our epoch is empty notarized.
	emptyNotarization := testutil.NewEmptyNotarization(nodes[:quorum], md.Round)
	emptyNotarization.Vote.Epoch = md.Epoch

	require.NoError(t, e.HandleMessage(&Message{ReplicationResponse: &ReplicationResponse{
		Data: []QuorumRound{{Block: telock, Notarization: &telockNotarization, EmptyNotarization: emptyNotarization}},
	}}, nodes[2]))

	// The replicated block is verified asynchronously. Give the node time to make the Telock its chain tip,
	// which it does within milliseconds if it accepts the quorum round, and proceed either way.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && e.Metadata().Prev != telock.Digest {
		time.Sleep(10 * time.Millisecond)
	}

	// The next round is notarized and finalized, and its block must be committed after the sealing block.
	block, _ := notarizeAndFinalizeRound(t, e, bb)
	require.Eventually(t, func() bool {
		return storage.NumBlocks() > md.Seq
	}, 5*time.Second, 10*time.Millisecond,
		"the block of round %d was finalized at seq %d on top of the Telock of epoch 0, and the node never committed seq %d",
		block.BlockHeader().Round, block.BlockHeader().Seq, md.Seq)
}

// TestLateNotarizationForPastRoundHaltsLeader tests as scenario where the node holds the block of round 0 without a notarization,
// round 0 is empty notarized, and the node proposes as the leader of round 1.
// A late notarization for round 0 then makes doNotarized restart round 1,
// so the node builds a second block for it, fails to store it, and halts.
func TestLateNotarizationForPastRoundHaltsLeader(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	recordingComm := &recordingComm{
		Communication:     testutil.NewNoopComm(nodes),
		BroadcastMessages: make(chan *Message, 100),
	}
	// nodes[1] is the leader of round 1.
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[1], recordingComm, bb)

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	block0 := testutil.NewTestBlock(e.Metadata(), emptyBlacklist)
	vote0, err := testutil.NewTestVote(block0, nodes[0])
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{BlockMessage: &BlockMessage{Block: block0, Vote: *vote0}}, nodes[0]))
	wal.AssertBlockProposal(0)

	// Round 0 times out and is empty notarized while we hold block0 without a notarization.
	bb.BlockShouldBeBuilt <- struct{}{}
	testutil.WaitForBlockProposerTimeout(t, e, &e.StartTime, 0)
	require.NoError(t, e.HandleMessage(&Message{EmptyNotarization: testutil.NewEmptyNotarization(nodes, 0)}, nodes[2]))
	testutil.WaitToEnterRound(t, e, 1)

	// We lead round 1 and propose a block for it.
	wal.AssertBlockProposal(1)
	waitForProposal(t, recordingComm.BroadcastMessages, 1)

	// A validator sends a notarization for round 0 that it assembled from the votes it received.
	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	notarization0, err := testutil.NewNotarization(e.Logger, sigAggr, block0, nodes[0:3])
	require.NoError(t, err)
	lateNotarization := &Message{Notarization: &notarization0}
	require.NoError(t, e.HandleMessage(lateNotarization, nodes[2]))
	require.True(t, wal.ContainsNotarization(0))

	// The node must keep processing messages.
	require.Never(t, func() bool {
		return e.HandleMessage(lateNotarization, nodes[2]) != nil
	}, time.Second, 10*time.Millisecond, "the late notarization for round 0 halted the leader of round 1")
}

// waitForProposal waits until a block proposal for the given round is broadcast.
func waitForProposal(t *testing.T, broadcast chan *Message, round uint64) {
	timeout := time.After(30 * time.Second)
	for {
		select {
		case msg := <-broadcast:
			if msg.VerifiedBlockMessage != nil && msg.VerifiedBlockMessage.VerifiedBlock.BlockHeader().Round == round {
				return
			}
		case <-timeout:
			require.FailNow(t, "timed out waiting for a block proposal", "round %d", round)
		}
	}
}

// TestEpochRejectsProposalWithWrongSeq asserts that a proposal is rejected when
// its Seq is not one after the parent.
func TestEpochRejectsProposalWithWrongSeq(t *testing.T) {
	for _, tc := range []struct {
		name     string
		seqDelta uint64
		accepted bool
	}{
		{name: "seq one past the parent is accepted", seqDelta: 1, accepted: true},
		{name: "seq two past the parent is rejected", seqDelta: 2, accepted: false},
		{name: "seq equal to the parent is rejected", seqDelta: 0, accepted: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bb := testutil.NewTestBlockBuilder()
			nodes := []NodeID{{1}, {2}, {3}, {4}}
			conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[3], testutil.NewNoopComm(nodes), bb)

			e, err := NewEpoch(conf)
			require.NoError(t, err)
			t.Cleanup(e.Stop)
			require.NoError(t, e.Start())

			// Notarize rounds 0 and 1 without finalizing them, so that the parent of the next proposal
			// is a block at a non-zero sequence that is still in memory, not in storage.
			advanceRoundFromNotarization(t, e, bb)
			parent, _ := advanceRoundFromNotarization(t, e, bb)
			require.Equal(t, uint64(2), e.Metadata().Round)
			require.Equal(t, uint64(1), parent.BlockHeader().Seq)
			// The WAL holds the blocks of rounds 0 and 1 and their notarizations.
			wal.AssertWALSize(4)

			// Propose a block for round 2 that extends the parent, but claims a sequence of our choosing.
			md := e.Metadata()
			require.Equal(t, parent.BlockHeader().Digest, md.Prev)
			md.Seq = parent.BlockHeader().Seq + tc.seqDelta
			block := testutil.NewTestBlock(md, NewBlacklist(uint16(len(nodes)), e.MaxBlacklistedRounds))

			leader := LeaderForRound(nodes, md.Round)
			vote, err := testutil.NewTestVote(block, leader)
			require.NoError(t, err)
			require.NoError(t, e.HandleMessage(&Message{
				BlockMessage: &BlockMessage{
					Vote:  *vote,
					Block: block,
				},
			}, leader))

			if tc.accepted {
				// The block is stored once it has been verified.
				wal.AssertBlockProposal(md.Round)
				return
			}

			// The proposal is dropped, so nothing is ever written for it.
			require.Never(t, func() bool {
				records, err := wal.ReadAll()
				return err != nil || len(records) != 4
			}, 500*time.Millisecond, 10*time.Millisecond)
			require.Equal(t, uint64(2), e.Metadata().Round)
		})
	}
}

// TestEpochKeepsReplicatedNotarizedBlockWhenLeaderRefillsRound checks a scenario where the leader of round 0 sends
// the node block A, while the rest of the validators notarize block B.
// The leader then sends block C for round 0, and its verification is executed.
// The notarization of B is replicated to the node, which deletes A and schedules the verification of B.
// Verification tasks run one at a time, so C is stored first, and B finds the round already filled.
// The node must still store the notarization of B and advance to round 1.
func TestEpochKeepsReplicatedNotarizedBlockWhenLeaderRefillsRound(t *testing.T) {
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	bb := testutil.NewTestBlockBuilder()
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[1], testutil.NewNoopComm(nodes), bb)
	conf.ReplicationEnabled = true

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	leader := LeaderForRound(nodes, 0)
	require.False(t, leader.Equals(e.ID))

	newBlock := func(data string) *testutil.TestBlock {
		block := testutil.NewTestBlock(e.Metadata(), emptyBlacklist)
		block.Data = []byte(data)
		block.ComputeDigest()
		return block
	}
	sendProposal := func(block *testutil.TestBlock) {
		vote, err := testutil.NewTestVote(block, leader)
		require.NoError(t, err)
		require.NoError(t, e.HandleMessage(&Message{
			BlockMessage: &BlockMessage{Vote: *vote, Block: block},
		}, leader))
	}

	// The node stores block A, the leader's proposal to it.
	blockA := newBlock("A")
	sendProposal(blockA)
	wal.AssertBlockProposal(0)

	// The leader sends block C for the same round, and its verification is held open.
	blockC := newBlock("C")
	blockC.VerificationDelay = make(chan struct{})
	sendProposal(blockC)

	// The notarization of block B, which every other validator voted for, is replicated to the node.
	// The node deletes A and schedules the verification of B, behind that of C.
	blockB := newBlock("B")
	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	notarization, err := testutil.NewNotarization(e.Logger, sigAggr, blockB, []NodeID{nodes[0], nodes[2], nodes[3]})
	require.NoError(t, err)
	require.NoError(t, e.HandleMessage(&Message{ReplicationResponse: &ReplicationResponse{
		Data: []QuorumRound{{Block: blockB, Notarization: &notarization}},
	}}, nodes[2]))

	// C is verified and stored in round 0 before B is verified.
	close(blockC.VerificationDelay)

	require.Eventually(t, func() bool {
		return e.Metadata().Round == 1
	}, 5*time.Second, 10*time.Millisecond, "the node dropped the notarized block B of round 0 and is stuck in round %d", e.Metadata().Round)
	require.True(t, wal.ContainsNotarization(0))
}

// TestEpochFutureEmptyVotesAssembleOnRoundStart asserts that empty votes received for a future round
// are kept and assembled into an empty notarization once the epoch reaches that round.
func TestEpochFutureEmptyVotesAssembleOnRoundStart(t *testing.T) {
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[3], testutil.NewNoopComm(nodes), testutil.NewTestBlockBuilder())

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	md := EmptyVoteMetadata{Round: 1, Epoch: e.Metadata().Epoch}
	for _, node := range nodes[:Quorum(len(nodes))] {
		require.NoError(t, e.HandleMessage(&Message{EmptyVoteMessage: createEmptyVote(md, node)}, node))
	}
	require.Equal(t, uint64(0), e.Metadata().Round)

	advanceRoundFromEmpty(t, e)

	emptyRecord := wal.AssertNotarization(1)
	require.Equal(t, EmptyNotarizationRecordType, emptyRecord)
	require.Equal(t, uint64(2), e.Metadata().Round)
}

func TestProposalOnAncientParentIsRejected(t *testing.T) {
	const finalizedRounds = 40

	bb := testutil.NewTestBlockBuilder()
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	comm := &recordingComm{
		Communication: testutil.NewNoopComm(nodes),
		SentMessages:  make(chan *Message, 100),
	}
	conf, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[0], comm, bb)

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	for range finalizedRounds {
		notarizeAndFinalizeRound(t, e, bb)
	}
	// The proposal must come from the leader of the current round, which must not be us.
	// so we advance the round until we are not the leader, then propose on top of block 0.
	for LeaderForRound(nodes, e.Metadata().Round).Equals(e.ID) {
		notarizeAndFinalizeRound(t, e, bb)
	}

	block0, _, err := storage.Retrieve(0)
	require.NoError(t, err)

	walRecords, err := wal.ReadAll()
	require.NoError(t, err)

	walSizeBeforeWrongProposal := len(walRecords)

	round := e.Metadata().Round
	leader := LeaderForRound(nodes, round)
	ancientProposal := testutil.NewTestBlock(ProtocolMetadata{
		Round: round,
		Seq:   1,
		Epoch: e.Epoch,
		Prev:  block0.BlockHeader().Digest,
	}, block0.Blacklist())
	vote, err := testutil.NewTestVote(ancientProposal, leader)
	require.NoError(t, err)

	// Discard what was sent while the chain advanced.
	for len(comm.SentMessages) > 0 {
		<-comm.SentMessages
	}

	require.NoError(t, e.HandleMessage(&Message{
		BlockMessage: &BlockMessage{Vote: *vote, Block: ancientProposal},
	}, leader))

	wal.AssertWALSize(walSizeBeforeWrongProposal) // The proposal is rejected, so nothing is written to the WAL.
}

func TestNoSecondVoteAfterConflictingCertificate(t *testing.T) {
	nodes := []NodeID{{1}, {2}, {3}, {4}}
	signers := []NodeID{nodes[0], nodes[2], nodes[3]}

	for _, tc := range []struct {
		name string
		// certificate builds the message that certifies block B.
		certificate func(t *testing.T, e *Epoch, sigAggr SignatureAggregator, blockB *testutil.TestBlock) *Message
	}{
		{
			name: "Notarization",
			certificate: func(t *testing.T, e *Epoch, sigAggr SignatureAggregator, blockB *testutil.TestBlock) *Message {
				notarization, err := testutil.NewNotarization(e.Logger, sigAggr, blockB, signers)
				require.NoError(t, err)
				return &Message{Notarization: &notarization}
			},
		},
		{
			name: "Finalization",
			certificate: func(t *testing.T, e *Epoch, sigAggr SignatureAggregator, blockB *testutil.TestBlock) *Message {
				finalization, _ := testutil.NewFinalizationRecord(t, sigAggr, blockB, signers)
				return &Message{Finalization: &finalization}
			},
		},
		{
			name: "Replicated notarization",
			certificate: func(t *testing.T, e *Epoch, sigAggr SignatureAggregator, blockB *testutil.TestBlock) *Message {
				notarization, err := testutil.NewNotarization(e.Logger, sigAggr, blockB, signers)
				require.NoError(t, err)
				return &Message{ReplicationResponse: &ReplicationResponse{
					Data: []QuorumRound{{Block: blockB, Notarization: &notarization}},
				}}
			},
		},
		{
			name: "Replicated finalization",
			certificate: func(t *testing.T, e *Epoch, sigAggr SignatureAggregator, blockB *testutil.TestBlock) *Message {
				finalization, _ := testutil.NewFinalizationRecord(t, sigAggr, blockB, signers)
				return &Message{ReplicationResponse: &ReplicationResponse{
					Data: []QuorumRound{{Block: blockB, Finalization: &finalization}},
				}}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bb := testutil.NewTestBlockBuilder()
			recordingComm := &recordingComm{
				Communication:     testutil.NewNoopComm(nodes),
				BroadcastMessages: make(chan *Message, 100),
			}
			conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[1], recordingComm, bb)
			conf.ReplicationEnabled = true

			e, err := NewEpoch(conf)
			require.NoError(t, err)
			t.Cleanup(e.Stop)
			require.NoError(t, e.Start())

			leader := LeaderForRound(nodes, 0)
			newBlock := func(data string) *testutil.TestBlock {
				block := testutil.NewTestBlock(e.Metadata(), emptyBlacklist)
				block.Data = []byte(data)
				block.ComputeDigest()
				return block
			}
			sendProposal := func(block *testutil.TestBlock) {
				vote, err := testutil.NewTestVote(block, leader)
				require.NoError(t, err)
				require.NoError(t, e.HandleMessage(&Message{
					BlockMessage: &BlockMessage{Vote: *vote, Block: block},
				}, leader))
			}

			// The node votes for block A.
			blockA := newBlock("A")
			sendProposal(blockA)
			wal.AssertBlockProposal(0)
			waitForVote(t, recordingComm.BroadcastMessages, blockA.BlockHeader().Digest)

			// The leader sends block C for the same round, and its verification is held open.
			// Verification tasks run one at a time, so C is verified before block B can be,
			// even on the replication paths, which schedule the verification of B.
			blockC := newBlock("C")
			blockC.VerificationDelay = make(chan struct{})
			sendProposal(blockC)

			// The certificate for block B reaches the node, which deletes round 0.
			blockB := newBlock("B")
			sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
			require.NoError(t, e.HandleMessage(tc.certificate(t, e, sigAggr, blockB), nodes[2]))

			// Block C finishes verification, and the node must not vote for it.
			close(blockC.VerificationDelay)
			require.Never(t, func() bool {
				select {
				case msg := <-recordingComm.BroadcastMessages:
					return msg.VoteMessage != nil && msg.VoteMessage.Vote.Digest == blockC.BlockHeader().Digest
				default:
					return false
				}
			}, time.Second, 10*time.Millisecond, "the node voted for block C in round 0 after voting for block A")
		})
	}
}

// waitForVote waits until the node broadcasts a vote for the block with the given digest.
func waitForVote(t *testing.T, broadcast chan *Message, digest Digest) {
	timeout := time.After(30 * time.Second)
	for {
		select {
		case msg := <-broadcast:
			if msg.VoteMessage != nil && msg.VoteMessage.Vote.Digest == digest {
				return
			}
		case <-timeout:
			require.FailNow(t, "timed out waiting for a vote", "digest %s", digest)
		}
	}
}
