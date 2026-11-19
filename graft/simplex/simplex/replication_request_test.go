package simplex_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/simplex"
	"github.com/ava-labs/simplex/testutil"
	"github.com/stretchr/testify/require"
)

// TestReplicationRequestIndexedBlocks tests replication requests for indexed blocks.
func TestReplicationRequestIndexedBlocks(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}
	comm := NewListenerComm(nodes)
	ctx := context.Background()
	conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], comm, bb)
	conf.ReplicationEnabled = true

	numBlocks := uint64(10)
	seqs := createBlocks(t, nodes, numBlocks)
	for _, data := range seqs {
		err := conf.Storage.Index(ctx, data.VerifiedBlock, data.Finalization)
		require.NoError(t, err)
	}
	e, err := simplex.NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())
	sequences := []uint64{0, 1, 2, 3}
	req := &common.Message{
		ReplicationRequest: &common.ReplicationRequest{
			Seqs:        sequences,
			LatestRound: numBlocks,
		},
	}

	err = e.HandleMessage(req, nodes[1])
	require.NoError(t, err)

	msg := <-comm.in
	resp := msg.VerifiedReplicationResponse
	require.Nil(t, resp.LatestRound)
	require.Nil(t, resp.LatestFinalizedSeq)

	require.Equal(t, len(sequences), len(resp.Data))
	for i, data := range resp.Data {
		require.Equal(t, seqs[i].Finalization, *data.Finalization)
		require.Equal(t, seqs[i].VerifiedBlock, data.VerifiedBlock)
	}

	// request out of scope
	req = &common.Message{
		ReplicationRequest: &common.ReplicationRequest{
			Seqs: []uint64{11, 12, 13},
		},
	}

	err = e.HandleMessage(req, nodes[1])
	require.NoError(t, err)

	require.Never(t, func() bool { return len(comm.in) > 0 }, 5*time.Second, 100*time.Millisecond)
}

// TestReplicationRequestNotarizations tests replication requests for notarized blocks.
func TestReplicationRequestNotarizations(t *testing.T) {
	// generate 5 blocks & notarizations
	bb := testutil.NewTestBlockBuilder()
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}
	comm := NewListenerComm(nodes)
	noop := testutil.NewNoopComm(nodes)
	conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], noop, bb)
	conf.ReplicationEnabled = true

	e, err := simplex.NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	numBlocks := uint64(5)
	rounds := make(map[uint64]common.VerifiedQuorumRound)
	for i := uint64(0); i < numBlocks; i++ {
		block, notarization := advanceRoundFromNotarization(t, e, bb)

		rounds[i] = common.VerifiedQuorumRound{
			VerifiedBlock: block,
			Notarization:  notarization,
		}
	}

	require.Equal(t, uint64(numBlocks), e.Metadata().Round)

	seqs := make([]uint64, 0, len(rounds))
	for k := range rounds {
		seqs = append(seqs, k)
	}
	req := &common.Message{
		ReplicationRequest: &common.ReplicationRequest{
			Seqs:               seqs,
			LatestRound:        1,
			LatestFinalizedSeq: 0,
		},
	}

	e.Comm = comm
	err = e.HandleMessage(req, nodes[1])
	require.NoError(t, err)

	msg := <-comm.in
	resp := msg.VerifiedReplicationResponse
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.LatestRound)
	require.Nil(t, resp.LatestFinalizedSeq)
	require.Equal(t, *resp.LatestRound, rounds[numBlocks-1])

	for _, round := range resp.Data {
		require.Nil(t, round.EmptyNotarization)
		notarizedBlock, ok := rounds[round.VerifiedBlock.BlockHeader().Round]
		require.True(t, ok)
		require.Equal(t, notarizedBlock.VerifiedBlock, round.VerifiedBlock)
		require.Equal(t, notarizedBlock.Notarization, round.Notarization)
	}

	// now ask for the notarizations as rounds
	req = &common.Message{
		ReplicationRequest: &common.ReplicationRequest{
			Rounds: seqs,
		},
	}

	err = e.HandleMessage(req, nodes[1])
	require.NoError(t, err)

	msg = <-comm.in
	resp = msg.VerifiedReplicationResponse
	for _, round := range resp.Data {
		require.Nil(t, round.EmptyNotarization)
		notarizedBlock, ok := rounds[round.VerifiedBlock.BlockHeader().Round]
		require.True(t, ok)
		require.Equal(t, notarizedBlock.VerifiedBlock, round.VerifiedBlock)
		require.Equal(t, notarizedBlock.Notarization, round.Notarization)
	}
}

// TestReplicationRequestMixed ensures the replication response also includes empty notarizations
func TestReplicationRequestMixed(t *testing.T) {
	// generate 5 blocks & notarizations
	bb := testutil.NewTestBlockBuilder()
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}
	comm := NewListenerComm(nodes)
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], comm, bb)
	conf.ReplicationEnabled = true

	e, err := simplex.NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	numBlocks := uint64(8)
	rounds := make(map[uint64]common.VerifiedQuorumRound)

	numExpectedRounds := 0
	// only produce a notarization for blocks we are the leader, otherwise produce an empty notarization
	for i := range numBlocks {
		numExpectedRounds++
		leaderForRound := bytes.Equal(simplex.LeaderForRound(nodes, uint64(i)), e.ID)
		emptyBlock := !leaderForRound
		if emptyBlock {
			emptyNotarization := testutil.NewEmptyNotarization(nodes, uint64(i))
			require.NoError(t, e.HandleMessage(&common.Message{
				EmptyNotarization: emptyNotarization,
			}, nodes[1]))
			wal.AssertNotarization(uint64(i))
			rounds[i] = common.VerifiedQuorumRound{
				EmptyNotarization: emptyNotarization,
			}
			continue
		}
		block, notarization := advanceRoundFromNotarization(t, e, bb)

		rounds[i] = common.VerifiedQuorumRound{
			VerifiedBlock: block,
			Notarization:  notarization,
		}

	}

	require.Equal(t, uint64(numBlocks), e.Metadata().Round)
	roundsRequested := make([]uint64, 0, len(rounds))
	for k := range rounds {
		roundsRequested = append(roundsRequested, k)
	}

	req := &common.Message{
		ReplicationRequest: &common.ReplicationRequest{
			Rounds:      roundsRequested,
			LatestRound: 1,
		},
	}

	err = e.HandleMessage(req, nodes[1])
	require.NoError(t, err)

	msg := <-comm.in
	resp := msg.VerifiedReplicationResponse
	require.Equal(t, *resp.LatestRound, rounds[numBlocks-1])
	require.Equal(t, numExpectedRounds, len(resp.Data))

	for _, round := range resp.Data {
		notarizedBlock, ok := rounds[round.GetRound()]
		require.True(t, ok)
		require.Equal(t, notarizedBlock.VerifiedBlock, round.VerifiedBlock)
		require.Equal(t, notarizedBlock.Notarization, round.Notarization)
		require.Equal(t, notarizedBlock.EmptyNotarization, round.EmptyNotarization)
	}
}

func TestReplicationRequestTailingEmptyNotarizations(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}
	comm := NewListenerComm(nodes)
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], comm, bb)
	conf.ReplicationEnabled = true

	e, err := simplex.NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	numBlocks := uint64(8)
	rounds := make(map[uint64]common.VerifiedQuorumRound)
	// only produce a notarization for blocks we are the leader, otherwise produce an empty notarization
	for i := range numBlocks {
		emptyNotarization := testutil.NewEmptyNotarization(nodes, uint64(i))
		require.NoError(t, e.HandleMessage(&common.Message{
			EmptyNotarization: emptyNotarization,
		}, nodes[1]))
		wal.AssertNotarization(uint64(i))
		rounds[i] = common.VerifiedQuorumRound{
			EmptyNotarization: emptyNotarization,
		}
	}

	require.Equal(t, uint64(numBlocks), e.Metadata().Round)
	roundsRequested := make([]uint64, 0, len(rounds))
	for k := range rounds {
		roundsRequested = append(roundsRequested, k)
	}

	req := &common.Message{
		ReplicationRequest: &common.ReplicationRequest{
			Rounds:      roundsRequested,
			LatestRound: 1,
		},
	}

	err = e.HandleMessage(req, nodes[1])
	require.NoError(t, err)

	msg := <-comm.in
	resp := msg.VerifiedReplicationResponse

	require.Equal(t, *resp.LatestRound, rounds[numBlocks-1])
	require.Equal(t, len(roundsRequested), len(resp.Data))
	for _, round := range resp.Data {
		notarizedBlock, ok := rounds[round.GetRound()]
		require.True(t, ok)
		require.Equal(t, notarizedBlock.VerifiedBlock, round.VerifiedBlock)
		require.Equal(t, notarizedBlock.Notarization, round.Notarization)
		require.Equal(t, notarizedBlock.EmptyNotarization, round.EmptyNotarization)
	}
}

func TestReplicationRequestUnknownSeqsAndRounds(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}
	comm := NewListenerComm(nodes)
	conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], comm, bb)
	conf.ReplicationEnabled = true

	e, err := simplex.NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	req := &common.Message{
		ReplicationRequest: &common.ReplicationRequest{
			Rounds:      []uint64{100, 101, 102},
			Seqs:        []uint64{200, 201, 202},
			LatestRound: 1,
		},
	}

	err = e.HandleMessage(req, nodes[1])
	require.NoError(t, err)

	require.Never(t, func() bool { return len(comm.in) > 0 }, 5*time.Second, 100*time.Millisecond)
}

// TestNonValidatorReplicationRequestServed ensures that a replication request
// from a non-validator node is still served when replication is enabled.
func TestNonValidatorReplicationRequestServed(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}
	comm := NewListenerComm(nodes)
	ctx := context.Background()
	conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], comm, bb)
	conf.ReplicationEnabled = true

	numBlocks := uint64(10)
	seqs := createBlocks(t, nodes, numBlocks)
	for _, data := range seqs {
		err := conf.Storage.Index(ctx, data.VerifiedBlock, data.Finalization)
		require.NoError(t, err)
	}
	e, err := simplex.NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	// a node that is not part of the validator set
	nonValidator := common.NodeID{5}

	sequences := []uint64{0, 1, 2, 3}
	req := &common.Message{
		ReplicationRequest: &common.ReplicationRequest{
			Seqs:        sequences,
			LatestRound: numBlocks,
		},
	}

	err = e.HandleMessage(req, nonValidator)
	require.NoError(t, err)

	msg := <-comm.in
	resp := msg.VerifiedReplicationResponse
	require.Equal(t, len(sequences), len(resp.Data))
	for i, data := range resp.Data {
		require.Equal(t, seqs[i].Finalization, *data.Finalization)
		require.Equal(t, seqs[i].VerifiedBlock, data.VerifiedBlock)
	}
}

// TestNonValidatorNonReplicationMessageDropped ensures that non-replication
// messages from a non-validator node are dropped and have no effect on the
// epoch's state.
func TestNonValidatorNonReplicationMessageDropped(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}
	comm := NewListenerComm(nodes)
	conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], comm, bb)
	conf.ReplicationEnabled = true

	e, err := simplex.NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	nonValidator := common.NodeID{5}

	// an empty notarization from a validator would advance the round; from a
	// non-validator it must be dropped, leaving the round unchanged.
	emptyNotarization := testutil.NewEmptyNotarization(nodes, 0)
	err = e.HandleMessage(&common.Message{
		EmptyNotarization: emptyNotarization,
	}, nonValidator)
	require.NoError(t, err)

	require.Equal(t, uint64(0), e.Metadata().Round)
}

func TestNilReplicationResponse(t *testing.T) {
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}
	net := testutil.NewControlledNetwork(t, nodes)

	normalNode0 := testutil.NewControlledSimplexNode(t, nodes[0], net, nil)
	normalNode0.Start()
	defer net.StopInstances()

	err := normalNode0.HandleMessage(&common.Message{
		ReplicationResponse: &common.ReplicationResponse{
			Data: []common.QuorumRound{{}},
		},
	}, nodes[1])
	require.NoError(t, err)
}

// TestMalformedReplicationResponse tests that a malformed replication response is handled correctly.
// This replication response is malformed since it must also include a notarization or
// finalization.
func TestMalformedReplicationResponse(t *testing.T) {
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}
	net := testutil.NewControlledNetwork(t, nodes)

	normalNode0 := testutil.NewControlledSimplexNode(t, nodes[0], net, nil)
	normalNode0.Start()
	defer net.StopInstances()

	err := normalNode0.HandleMessage(&common.Message{
		ReplicationResponse: &common.ReplicationResponse{
			Data: []common.QuorumRound{{
				Block: &testutil.TestBlock{},
			}},
		},
	}, nodes[1])
	require.NoError(t, err)
}

// TestReplicationRequestSeqsAndRoundsTruncated ensures a request with more seqs and more rounds
// than MaxRoundWindow is answered with both truncated to the lowest MaxRoundWindow entries
// rather than being dropped.
func TestReplicationRequestSeqsAndRoundsTruncated(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}
	comm := NewListenerComm(nodes)
	ctx := context.Background()
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], comm, bb)
	conf.ReplicationEnabled = true

	// index maxRoundsWindow + 2 blocks: 0...11
	numIndexed := conf.MaxRoundWindow + 2
	numNotarized := uint64(5)
	blocks := createBlocks(t, nodes, numIndexed+numNotarized)
	seqs := blocks[:numIndexed]
	for _, data := range seqs {
		require.NoError(t, conf.Storage.Index(ctx, data.VerifiedBlock, data.Finalization))
	}

	// notarize a few rounds past the indexed blocks: rounds 12...16
	// not finalized
	quorom := common.Quorum(len(nodes))
	notarized := make([]common.VerifiedQuorumRound, 0, numNotarized)

	for _, data := range blocks[numIndexed:] {
		blockBytes := data.VerifiedBlock.Bytes()
		br, err := common.BlockRecord(data.VerifiedBlock.BlockHeader(), blockBytes)
		require.NoError(t, err)
		require.NoError(t, wal.Append(br))

		notarization, err := testutil.NewNotarization(conf.Logger, &testutil.TestSignatureAggregator{N: len(nodes)}, data.VerifiedBlock, nodes[:quorom])
		require.NoError(t, err)
		require.NoError(t, wal.Append(common.NewQuorumRecord(notarization.QC.Bytes(), notarization.Vote.Bytes(), common.NotarizationRecordType)))

		notarized = append(notarized, common.VerifiedQuorumRound{
			VerifiedBlock: data.VerifiedBlock,
			Notarization:  &notarization,
		})
	}

	// oversized, unsorted seqs seqs request: 0 ...2*MaxRoundWindow
	// truncation keeps seqs 0...9
	requestedSeqs := make([]uint64, 0, 2*conf.MaxRoundWindow)
	for i := 2 * conf.MaxRoundWindow; i > 0; i-- {
		requestedSeqs = append(requestedSeqs, i-1)
	}

	// oversized, unsorted rounds request: 4..24
	// truncation keeps rounds 4...13
	roundStart := numIndexed - 8 // 4
	requestedRounds := make([]uint64, 0, 2*conf.MaxRoundWindow)
	for i := roundStart + 2*conf.MaxRoundWindow; i > roundStart; i-- {
		requestedRounds = append(requestedRounds, i-1)
	}

	expected := make([]common.VerifiedQuorumRound, 0, conf.MaxRoundWindow+2)
	for i := uint64(0); i < conf.MaxRoundWindow; i++ {
		expected = append(expected, common.VerifiedQuorumRound{
			VerifiedBlock: seqs[i].VerifiedBlock,
			Finalization:  &seqs[i].Finalization,
		})
	}
	expected = append(expected, notarized[:2]...)

	e, err := simplex.NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	require.NoError(t, e.HandleMessage(&common.Message{
		ReplicationRequest: &common.ReplicationRequest{
			Seqs:   requestedSeqs,
			Rounds: requestedRounds,
		},
	}, nodes[1]))

	msg := <-comm.in
	resp := msg.VerifiedReplicationResponse
	requireEqualQuorumRounds(t, expected, resp.Data)

}

// requireEqualQuorumRounds compares quorum rounds semantically, avoiding
// reflect.DeepEqual on canoto-encoded values.
func requireEqualQuorumRounds(t *testing.T, expected, actual []common.VerifiedQuorumRound) {
	require.Equal(t, len(expected), len(actual))
	for i := range expected {
		exp, act := expected[i], actual[i]
		require.Equal(t, exp.VerifiedBlock.BlockHeader().Digest, act.VerifiedBlock.BlockHeader().Digest)

		if exp.Finalization != nil {
			require.NotNil(t, act.Finalization)
			require.True(t, exp.Finalization.Finalization.Equals(&act.Finalization.Finalization.BlockHeader))
			require.Equal(t, exp.Finalization.QC, act.Finalization.QC)
		} else {
			require.Nil(t, act.Finalization)
		}

		if exp.Notarization != nil {
			require.NotNil(t, act.Notarization)
			require.True(t, exp.Notarization.Vote.Equals(&act.Notarization.Vote.BlockHeader))
			require.Equal(t, exp.Notarization.QC, act.Notarization.QC)
		} else {
			require.Nil(t, act.Notarization)
		}

		require.Equal(t, exp.EmptyNotarization, act.EmptyNotarization)
	}
}

// TestReplicationRequestSizeLimited ensures a replication response is capped
// at MaxReplicationResponseSize estimated bytes, starting wit the lowest seqs first.
// Sequences that do not fit are requested again by the requester
// so a partial response is not lost.
func TestReplicationRequestSizeLimited(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}
	comm := NewListenerComm(nodes)
	ctx := context.Background()
	conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], comm, bb)
	conf.ReplicationEnabled = true

	numBlocks := uint64(8) // within MaxRoundWindow
	seqs := createBlocks(t, nodes, numBlocks)
	for _, data := range seqs {
		require.NoError(t, conf.Storage.Index(ctx, data.VerifiedBlock, data.Finalization))
	}

	// budget for the first three quorum rounds plus half of the fourth, so that
	// exactly three fit. Rounds are not uniformly sized (the encoding omits
	// zero-valued fields), so each one has to be measured separately.
	roundSize := func(i int) int {
		return (&common.VerifiedQuorumRound{
			VerifiedBlock: seqs[i].VerifiedBlock,
			Finalization:  &seqs[i].Finalization,
		}).Size()
	}
	conf.MaxReplicationResponseSize = roundSize(0) + roundSize(1) + roundSize(2) + roundSize(3)/2

	e, err := simplex.NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	requested := make([]uint64, numBlocks)
	for i := range requested {
		requested[i] = uint64(i)
	}
	require.NoError(t, e.HandleMessage(&common.Message{
		ReplicationRequest: &common.ReplicationRequest{
			Seqs: requested,
		},
	}, nodes[1]))

	msg := <-comm.in
	resp := msg.VerifiedReplicationResponse

	// only the lowest seqs that fit under the budget are sent
	require.Len(t, resp.Data, 3)
	total := 0
	for i, data := range resp.Data {
		require.Equal(t, uint64(i), data.VerifiedBlock.BlockHeader().Seq)
		size := data.Size()
		require.NoError(t, err)
		total += size
	}
	require.LessOrEqual(t, total, conf.MaxReplicationResponseSize)
}

// TestReplicationRequestSizeLimitedLatestFinalizedSeq ensures that when the latest
// finalized block alone exceeds the response size limit, no response is sent at all.
// An oversized message would be rejected by the network anyway.
func TestReplicationRequestSizeLimitedLatestFinalizedSeq(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}
	comm := NewListenerComm(nodes)
	ctx := context.Background()
	conf, _, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], comm, bb)
	conf.ReplicationEnabled = true
	// silencing the error logger in the test because it breaks github CI
	conf.Logger.(*testutil.TestLogger).Silence()
	conf.MaxReplicationResponseSize = 1

	numBlocks := uint64(4)
	seqs := createBlocks(t, nodes, numBlocks)
	for _, data := range seqs {
		require.NoError(t, conf.Storage.Index(ctx, data.VerifiedBlock, data.Finalization))
	}

	e, err := simplex.NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	// ask for data and the latest finalized seq: the latest finalized seq must arrive, the data must not
	require.NoError(t, e.HandleMessage(&common.Message{
		ReplicationRequest: &common.ReplicationRequest{
			Seqs:               []uint64{0, 1, 2},
			LatestFinalizedSeq: 1,
		},
	}, nodes[1]))

	require.Empty(t, comm.in)
}

// TestReplicationRequestSizeLimitedBothLatest ensures that when both latest fields are
// requested but only one fits, the latest finalized seq is sent and the latest round
// is omitted rather than pushing the response over the limit.
func TestReplicationRequestSizeLimitedBothLatest(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}
	comm := NewListenerComm(nodes)
	ctx := context.Background()
	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[0], comm, bb)
	conf.ReplicationEnabled = true
	conf.MaxReplicationResponseSize = 1

	// blocks 0..3 indexed to storage (provides lastBlock for LatestFinalizedSeq)
	// block 4 notarized via a Wal record (provides a round for LatestRound)
	blocks := createBlocks(t, nodes, 5)
	for _, data := range blocks[:4] {
		require.NoError(t, conf.Storage.Index(ctx, data.VerifiedBlock, data.Finalization))
	}
	notarizedBlock := blocks[4].VerifiedBlock
	blockBytes := notarizedBlock.Bytes()
	br, err := common.BlockRecord(notarizedBlock.BlockHeader(), blockBytes)
	require.NoError(t, err)
	require.NoError(t, wal.Append(br))
	notarization, err := testutil.NewNotarization(conf.Logger,
		&testutil.TestSignatureAggregator{N: len(nodes)},
		notarizedBlock, nodes[:common.Quorum(len(nodes))])
	require.NoError(t, err)
	require.NoError(t, wal.Append(common.NewQuorumRecord(notarization.QC.Bytes(),
		notarization.Vote.Bytes(), common.NotarizationRecordType)))

	// budget exactly one fnalized quorom round, leaving no room for the latest round
	conf.MaxReplicationResponseSize = (&common.VerifiedQuorumRound{
		VerifiedBlock: blocks[3].VerifiedBlock,
		Finalization:  &blocks[3].Finalization,
	}).Size()

	e, err := simplex.NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	require.NoError(t, e.HandleMessage(&common.Message{
		ReplicationRequest: &common.ReplicationRequest{
			LatestRound:        1,
			LatestFinalizedSeq: 1,
		},
	}, nodes[1]))
	msg := <-comm.in
	resp := msg.VerifiedReplicationResponse
	require.NotNil(t, resp.LatestFinalizedSeq)
	require.Nil(t, resp.LatestRound)
	require.Empty(t, resp.Data)
}
