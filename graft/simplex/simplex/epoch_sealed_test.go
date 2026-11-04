// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex_test

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/ava-labs/avalanchego/graft/simplex/common"
	. "github.com/ava-labs/avalanchego/graft/simplex/simplex"
	"github.com/ava-labs/avalanchego/graft/simplex/testutil"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

const (
	// sealingTestBlocks is the number of blocks (rounds/seqs 0..8) proposed by other nodes.
	// It is also the round the epoch node itself leads (round 9).
	sealingTestBlocks = uint64(9)
	// sealingTestSealingSeq is the sequence of the (potential) sealing block - the 5th block.
	sealingTestSealingSeq = uint64(4)
)

// sealingBlockTestEnv is the fixture produced by setupSealingBlockTest. It holds a started
// epoch and the block chain, so the test body can drive the epoch with messages and then
// assert on the result.
type sealingBlockTestEnv struct {
	e            *Epoch
	comm         *recordingComm
	storage      *testutil.InMemStorage
	nodes        []NodeID
	quorum       int
	sigAggr      SignatureAggregator
	blocks       []*testutil.TestBlock
	verified     map[uint64]*atomic.Bool
	sealed       *atomic.Bool
	buildAborted *atomic.Bool
}

// allVerified reports whether every block in the chain has been verified.
func (env *sealingBlockTestEnv) allVerified() bool {
	for seq := uint64(0); seq < sealingTestBlocks; seq++ {
		if !env.verified[seq].Load() {
			return false
		}
	}
	return true
}

// verifiedAfterSealingBlock reports whether any block after the sealing block has been verified.
func (env *sealingBlockTestEnv) verifiedAfterSealingBlock() bool {
	for seq := sealingTestSealingSeq + 1; seq < sealingTestBlocks; seq++ {
		if env.verified[seq].Load() {
			return true
		}
	}
	return false
}

// proposedLeaderRound reports whether the epoch has broadcast a proposal for the round it leads.
func (env *sealingBlockTestEnv) proposedLeaderRound() bool {
	return leaderProposedBlock(env.comm, sealingTestBlocks)
}

func TestEpochStopsAtSealingBlock(t *testing.T) {
	// Verifies that a sealing block stops the epoch from
	// executing (verifying) any block that comes after it.
	//
	// The epoch instance is the last node in the leadership order of a 10-node membership,
	// so it never proposes any of the first 9 blocks (rounds 0-8) - each is proposed by a
	// different node - but it is the leader of the last round (round 9). Each case delivers
	// all 9 blocks as block messages and then feeds finalizations for the first 5 sequences
	// (0-4) and notarizations for the remaining 4 (5-8), driving the epoch to the round it
	// leads, and then asserts:
	//   - Without a sealing block: all 9 blocks are verified, showing the messages do schedule
	//     every block for execution, and the epoch then proposes a block for the round it is the leader of.
	//   - With the 5th block (seq 4) being a sealing block: only the blocks up to and including
	//     the sealing block are verified and committed; the rest are not, because committing the
	//     sealing block seals the epoch and stops it from verifying any later block.

	for _, tc := range []struct {
		name    string
		sealing bool
		verify  func(t *testing.T, env *sealingBlockTestEnv)
	}{
		{
			name:    "without sealing block",
			sealing: false,
			verify: func(t *testing.T, env *sealingBlockTestEnv) {
				// Without a sealing block, every block should eventually be verified.
				require.Eventually(t, env.allVerified, 5*time.Second, 10*time.Millisecond,
					"all blocks should be verified when there is no sealing block")
				require.False(t, env.sealed.Load(), "epoch should not seal when there is no sealing block")

				// Having advanced through every preceding round, the epoch reaches the round
				// it is the leader of and, because the epoch is not sealed, proposes a block for that round.
				require.Eventually(t, env.proposedLeaderRound, 5*time.Second, 10*time.Millisecond,
					"the epoch node should propose a block for the round it leads when the epoch is not sealed")
			},
		},
		{
			name:    "with sealing block",
			sealing: true,
			verify: func(t *testing.T, env *sealingBlockTestEnv) {
				// The blocks after the sealing block must never be verified.
				require.Never(t, env.verifiedAfterSealingBlock, 500*time.Millisecond, 50*time.Millisecond,
					"no block after the sealing block should be verified")

				require.True(t, env.sealed.Load(), "epoch should have been sealed after committing the sealing block")
				require.Equal(t, sealingTestSealingSeq+1, env.storage.NumBlocks(),
					"only the blocks up to and including the sealing block should be committed")
				for seq := uint64(0); seq <= sealingTestSealingSeq; seq++ {
					require.True(t, env.verified[seq].Load(), "block %d (up to the sealing block) should have been verified", seq)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := setupSealingBlockTest(t, sealingTestBlocks, tc.sealing)

			// Deliver every block as a block message from the block's leader.
			for i := uint64(0); i < sealingTestBlocks; i++ {
				leader := LeaderForRound(env.nodes, i)
				vote, err := testutil.NewTestVote(env.blocks[i], leader)
				require.NoError(t, err)
				err = env.e.HandleMessage(&Message{
					BlockMessage: &BlockMessage{
						Block: env.blocks[i],
						Vote:  *vote,
					},
				}, leader)
				require.NoError(t, err)
			}

			// Feed finalizations for the first 5 sequences (0-4) up to and including the sealing block (seq 4).
			for i := uint64(0); i <= sealingTestSealingSeq; i++ {
				finalization, _ := testutil.NewFinalizationRecord(t, env.sigAggr, env.blocks[i], env.nodes[:env.quorum])
				require.NoError(t, env.e.HandleMessage(&Message{Finalization: &finalization}, env.nodes[0]))
				env.storage.WaitForBlockCommit(i)
			}

			// Notarize the remaining 4 sequences (5-8).
			for i := sealingTestSealingSeq + 1; i < sealingTestBlocks; i++ {
				notarization, err := testutil.NewNotarization(env.e.Logger, env.sigAggr, env.blocks[i], env.nodes[:env.quorum])
				require.NoError(t, err)
				testutil.InjectTestNotarization(t, env.e, notarization, env.nodes[0])
			}

			tc.verify(t, env)
		})
	}
}

func TestEpochBuildsInRoundAfterSealingBlock(t *testing.T) {
	// Tests that we don't propose a block after a sealing block.
	//
	// The epoch node is chosen to be the leader of round sealingSeq+1. Each case delivers and
	// finalizes blocks 0..sealingSeq (all led by other nodes); committing the last block
	// progresses the epoch to the next round, which the epoch node leads. Each case then asserts:
	//   - Without a sealing block: the epoch is not sealed, so it builds and proposes a block for
	//     the round it leads.
	//   - With the last block (seq 4) being a sealing block: committing it seals the epoch, so
	//     when the epoch reaches the round it leads it aborts block building and proposes nothing.

	const roundAfterSealing = sealingTestSealingSeq + 1

	for _, tc := range []struct {
		name    string
		sealing bool
		verify  func(t *testing.T, env *sealingBlockTestEnv)
	}{
		{
			name:    "without sealing block",
			sealing: false,
			verify: func(t *testing.T, env *sealingBlockTestEnv) {
				// Not sealed, so the epoch node builds and proposes a block for the round it leads.
				require.Eventually(t, func() bool { return leaderProposedBlock(env.comm, roundAfterSealing) }, 5*time.Second, 10*time.Millisecond,
					"the epoch node should propose a block for the round it leads right after the block")
				require.False(t, env.sealed.Load(), "epoch should not seal when there is no sealing block")
			},
		},
		{
			name:    "with sealing block",
			sealing: true,
			verify: func(t *testing.T, env *sealingBlockTestEnv) {
				// The epoch reaches the round it leads and attempts to build, but aborts because
				// it is sealed - it does not build a block after the sealing block. Waiting on the
				// abort also synchronizes past the seal, which happens right before it.
				require.Eventually(t, env.buildAborted.Load, 5*time.Second, 10*time.Millisecond,
					"a sealed epoch should abort block building in the round it leads")
				require.True(t, env.sealed.Load(), "epoch should have been sealed after committing the sealing block")

				// It must never propose a block for that round either.
				require.Never(t, func() bool { return leaderProposedBlock(env.comm, roundAfterSealing) }, 500*time.Millisecond, 50*time.Millisecond,
					"a sealed epoch must not propose a block in the round it leads")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := setupSealingBlockTest(t, roundAfterSealing, tc.sealing)

			// Deliver blocks 0..sealingSeq as block messages from their leaders (none is the epoch node).
			for i := uint64(0); i <= sealingTestSealingSeq; i++ {
				leader := LeaderForRound(env.nodes, i)
				vote, err := testutil.NewTestVote(env.blocks[i], leader)
				require.NoError(t, err)
				require.NoError(t, env.e.HandleMessage(&Message{
					BlockMessage: &BlockMessage{
						Block: env.blocks[i],
						Vote:  *vote,
					},
				}, leader))
			}

			// Finalize 0..sealingSeq. Committing the last block progresses the epoch to the
			// next round, where the epoch node is the leader.
			for i := uint64(0); i <= sealingTestSealingSeq; i++ {
				finalization, _ := testutil.NewFinalizationRecord(t, env.sigAggr, env.blocks[i], env.nodes[:env.quorum])
				require.NoError(t, env.e.HandleMessage(&Message{Finalization: &finalization}, env.nodes[0]))
				env.storage.WaitForBlockCommit(i)
			}

			tc.verify(t, env)
		})
	}
}

// setupSealingBlockTest builds and starts a 10-node epoch whose node is the leader of
// epochLeaderRound, and constructs the chain of blocks the test will feed it. When
// markSealing is true, the seq-4 block is marked as a sealing block. It returns a fixture
// the test body uses to drive the epoch and assert on the result.
func setupSealingBlockTest(t *testing.T, epochLeaderRound uint64, markSealing bool) *sealingBlockTestEnv {
	nodes := make([]NodeID, 10)
	for i := range nodes {
		nodes[i] = NodeID{byte(i + 1)}
	}

	// Leadership is round-robin over the (distinct) nodes, so the epoch node leads
	// epochLeaderRound and no other round in the [0, len(nodes)) range.
	epochNode := LeaderForRound(nodes, epochLeaderRound)

	quorum := Quorum(len(nodes))
	blacklist := NewBlacklist(DefaultMaxBlacklistedRounds)

	bb := testutil.NewTestBlockBuilder()
	comm := &recordingComm{
		Communication:     testutil.NewNoopComm(NodeIDs(nodes)),
		SentMessages:      make(chan *Message, 1000),
		BroadcastMessages: make(chan *Message, 1000),
	}
	conf, _, storage := testutil.DefaultTestNodeEpochConfig(t, epochNode, comm, bb)
	conf.ReplicationEnabled = true

	// Intercept logs so we can observe when the sealing block seals the epoch, and when a
	// sealed epoch aborts building a block in a round it leads.
	sealed := &atomic.Bool{}
	buildAborted := &atomic.Bool{}
	l := conf.Logger.(*testutil.TestLogger)
	l.Intercept(func(entry zapcore.Entry) error {
		if strings.Contains(entry.Message, "Committed a sealing block, epoch is sealed") {
			sealed.Store(true)
		}
		if strings.Contains(entry.Message, "Aborting block building because the epoch is sealed") {
			buildAborted.Store(true)
		}
		return nil
	})

	e, err := NewEpoch(conf)
	require.NoError(t, err)
	e.ReplicationEnabled = true
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	// Build a chain of 9 blocks (rounds/seqs 0..8), recording which blocks are verified.
	verified := make(map[uint64]*atomic.Bool)
	for i := uint64(0); i < sealingTestBlocks; i++ {
		verified[i] = &atomic.Bool{}
	}

	blocks := make([]*testutil.TestBlock, sealingTestBlocks)
	var prev Digest
	for i := uint64(0); i < sealingTestBlocks; i++ {
		md := ProtocolMetadata{Round: i, Seq: i, Prev: prev}
		block := testutil.NewTestBlock(md, blacklist)
		seq := i
		block.OnVerify = func() {
			verified[seq].Store(true)
		}
		blocks[i] = block
		prev = block.BlockHeader().Digest
	}

	// If we are sealing the epoch, mark the sealing block with a validator set so the epoch knows it is a sealing block.
	if markSealing {
		blocks[sealingTestSealingSeq].SealingInfo = &SealingBlockInfo{
			ValidatorSet:         NodeIDs(nodes).EqualWeightedNodes(),
			PrevSealingBlockHash: blocks[0].Digest,
		}
	}

	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())

	return &sealingBlockTestEnv{
		e:            e,
		comm:         comm,
		storage:      storage,
		nodes:        nodes,
		quorum:       quorum,
		sigAggr:      sigAggr,
		blocks:       blocks,
		verified:     verified,
		sealed:       sealed,
		buildAborted: buildAborted,
	}
}

// leaderProposedBlock reports whether the epoch has broadcast a proposal for the given round.
func leaderProposedBlock(comm *recordingComm, round uint64) bool {
	for {
		select {
		case msg := <-comm.BroadcastMessages:
			if msg.VerifiedBlockMessage != nil && msg.VerifiedBlockMessage.VerifiedBlock.BlockHeader().Round == round {
				return true
			}
		default:
			return false
		}
	}
}
