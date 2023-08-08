// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"
	"errors"
	"path"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/utils/bag"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

type testFunc func(*testing.T, Factory)

var (
	GenesisID        = ids.Empty.Prefix(0)
	GenesisHeight    = uint64(0)
	GenesisTimestamp = time.Unix(1, 0)
	Genesis          = &TestBlock{TestDecidable: choices.TestDecidable{
		IDV:     GenesisID,
		StatusV: choices.Accepted,
	}}

	testFuncs = []testFunc{
		InitializeTest,
		NumProcessingTest,
		AddToTailTest,
		AddToNonTailTest,
		AddToUnknownTest,
		StatusOrProcessingPreviouslyAcceptedTest,
		StatusOrProcessingPreviouslyRejectedTest,
		StatusOrProcessingUnissuedTest,
		StatusOrProcessingIssuedTest,
		RecordPollAcceptSingleBlockTest,
		RecordPollAcceptAndRejectTest,
		RecordPollSplitVoteNoChangeTest,
		RecordPollWhenFinalizedTest,
		RecordPollRejectTransitivelyTest,
		RecordPollTransitivelyResetConfidenceTest,
		RecordPollInvalidVoteTest,
		RecordPollTransitiveVotingTest,
		RecordPollDivergedVotingTest,
		RecordPollDivergedVotingWithNoConflictingBitTest,
		RecordPollChangePreferredChainTest,
		MetricsProcessingErrorTest,
		MetricsAcceptedErrorTest,
		MetricsRejectedErrorTest,
		ErrorOnInitialRejectionTest,
		ErrorOnAcceptTest,
		ErrorOnRejectSiblingTest,
		ErrorOnTransitiveRejectionTest,
		RandomizedConsistencyTest,
		ErrorOnAddDecidedBlock,
		ErrorOnAddDuplicateBlockID,
	}

	errTest = errors.New("non-nil error")
)

// Execute all tests against a consensus implementation
func runConsensusTests(t *testing.T, factory Factory) {
	for _, test := range testFuncs {
		t.Run(getTestName(test), func(tt *testing.T) {
			test(tt, factory)
		})
	}
}

func getTestName(i interface{}) string {
	return strings.Split(path.Base(runtime.FuncForPC(reflect.ValueOf(i).Pointer()).Name()), ".")[1]
}

// Make sure that initialize sets the state correctly
func InitializeTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          3,
		BetaRogue:             5,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	require.Equal(GenesisID, sm.Preference())
	require.True(sm.NumProcessing() == 0) // finalized when there is no processing block
}

// Make sure that the number of processing blocks is tracked correctly
func NumProcessingTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	block := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}

	require.Zero(sm.NumProcessing())

	// Adding to the previous preference will update the preference
	require.NoError(sm.Add(context.Background(), block))

	require.Equal(1, sm.NumProcessing())

	votes := bag.Bag[ids.ID]{}
	votes.Add(block.ID())
	require.NoError(sm.RecordPoll(context.Background(), votes))

	require.Zero(sm.NumProcessing())
}

// Make sure that adding a block to the tail updates the preference
func AddToTailTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          3,
		BetaRogue:             5,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	block := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}

	// Adding to the previous preference will update the preference
	require.NoError(sm.Add(context.Background(), block))
	require.Equal(block.ID(), sm.Preference())
	require.True(sm.IsPreferred(block))
}

// Make sure that adding a block not to the tail doesn't change the preference
func AddToNonTailTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          3,
		BetaRogue:             5,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	firstBlock := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}
	secondBlock := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}

	// Adding to the previous preference will update the preference
	require.NoError(sm.Add(context.Background(), firstBlock))
	require.Equal(firstBlock.IDV, sm.Preference())

	// Adding to something other than the previous preference won't update the
	// preference
	require.NoError(sm.Add(context.Background(), secondBlock))
	require.Equal(firstBlock.IDV, sm.Preference())
}

// Make sure that adding a block that is detached from the rest of the tree
// rejects the block
func AddToUnknownTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          3,
		BetaRogue:             5,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	parent := &TestBlock{TestDecidable: choices.TestDecidable{
		IDV:     ids.Empty.Prefix(1),
		StatusV: choices.Unknown,
	}}

	block := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2),
			StatusV: choices.Processing,
		},
		ParentV: parent.IDV,
		HeightV: parent.HeightV + 1,
	}

	// Adding a block with an unknown parent means the parent must have already
	// been rejected. Therefore the block should be immediately rejected
	require.NoError(sm.Add(context.Background(), block))
	require.Equal(GenesisID, sm.Preference())
	require.Equal(choices.Rejected, block.Status())
}

func StatusOrProcessingPreviouslyAcceptedTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          3,
		BetaRogue:             5,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	require.Equal(choices.Accepted, Genesis.Status())
	require.False(sm.Processing(Genesis.ID()))
	require.True(sm.Decided(Genesis))
	require.True(sm.IsPreferred(Genesis))
}

func StatusOrProcessingPreviouslyRejectedTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          3,
		BetaRogue:             5,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	block := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Rejected,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}

	require.Equal(choices.Rejected, block.Status())
	require.False(sm.Processing(block.ID()))
	require.True(sm.Decided(block))
	require.False(sm.IsPreferred(block))
}

func StatusOrProcessingUnissuedTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          3,
		BetaRogue:             5,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	block := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}

	require.Equal(choices.Processing, block.Status())
	require.False(sm.Processing(block.ID()))
	require.False(sm.Decided(block))
	require.False(sm.IsPreferred(block))
}

func StatusOrProcessingIssuedTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          3,
		BetaRogue:             5,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	block := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}

	require.NoError(sm.Add(context.Background(), block))
	require.Equal(choices.Processing, block.Status())
	require.True(sm.Processing(block.ID()))
	require.False(sm.Decided(block))
	require.True(sm.IsPreferred(block))
}

func RecordPollAcceptSingleBlockTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          2,
		BetaRogue:             3,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	block := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}

	require.NoError(sm.Add(context.Background(), block))

	votes := bag.Bag[ids.ID]{}
	votes.Add(block.ID())
	require.NoError(sm.RecordPoll(context.Background(), votes))
	require.Equal(block.ID(), sm.Preference())
	require.False(!(sm.NumProcessing() > 0))
	require.Equal(choices.Processing, block.Status())

	require.NoError(sm.RecordPoll(context.Background(), votes))
	require.Equal(block.ID(), sm.Preference())
	require.True(!(sm.NumProcessing() > 0))
	require.Equal(choices.Accepted, block.Status())
}

func RecordPollAcceptAndRejectTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	firstBlock := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}
	secondBlock := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}

	require.NoError(sm.Add(context.Background(), firstBlock))
	require.NoError(sm.Add(context.Background(), secondBlock))

	votes := bag.Bag[ids.ID]{}
	votes.Add(firstBlock.ID())

	require.NoError(sm.RecordPoll(context.Background(), votes))
	require.Equal(firstBlock.ID(), sm.Preference())
	require.False(!(sm.NumProcessing() > 0))
	require.Equal(choices.Processing, firstBlock.Status())
	require.Equal(choices.Processing, secondBlock.Status())

	require.NoError(sm.RecordPoll(context.Background(), votes))
	require.Equal(firstBlock.ID(), sm.Preference())
	require.True(!(sm.NumProcessing() > 0))
	require.Equal(choices.Accepted, firstBlock.Status())
	require.Equal(choices.Rejected, secondBlock.Status())
}

func RecordPollSplitVoteNoChangeTest(t *testing.T, factory Factory) {
	require := require.New(t)
	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	registerer := prometheus.NewRegistry()
	ctx.Registerer = registerer

	params := snowball.Parameters{
		K:                     2,
		Alpha:                 2,
		BetaVirtuous:          1,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	firstBlock := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}
	secondBlock := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}

	require.NoError(sm.Add(context.Background(), firstBlock))
	require.NoError(sm.Add(context.Background(), secondBlock))

	votes := bag.Bag[ids.ID]{}
	votes.Add(firstBlock.ID())
	votes.Add(secondBlock.ID())

	// The first poll will accept shared bits
	require.NoError(sm.RecordPoll(context.Background(), votes))
	require.Equal(firstBlock.ID(), sm.Preference())
	require.False(sm.NumProcessing() > 0)

	metrics := gatherCounterGauge(t, registerer)
	require.Zero(metrics["polls_failed"])
	require.Equal(float64(1), metrics["polls_successful"])

	// The second poll will do nothing
	require.NoError(sm.RecordPoll(context.Background(), votes))
	require.Equal(firstBlock.ID(), sm.Preference())
	require.False(sm.NumProcessing() > 0)

	metrics = gatherCounterGauge(t, registerer)
	require.Equal(float64(1), metrics["polls_failed"])
	require.Equal(float64(1), metrics["polls_successful"])
}

func RecordPollWhenFinalizedTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	votes := bag.Bag[ids.ID]{}
	votes.Add(GenesisID)
	require.NoError(sm.RecordPoll(context.Background(), votes))
	require.True(!(sm.NumProcessing() > 0))
	require.Equal(GenesisID, sm.Preference())
}

func RecordPollRejectTransitivelyTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	block0 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}
	block1 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}
	block2 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(3),
			StatusV: choices.Processing,
		},
		ParentV: block1.IDV,
		HeightV: block1.HeightV + 1,
	}

	require.NoError(sm.Add(context.Background(), block0))
	require.NoError(sm.Add(context.Background(), block1))
	require.NoError(sm.Add(context.Background(), block2))

	// Current graph structure:
	//   G
	//  / \
	// 0   1
	//     |
	//     2
	// Tail = 0

	votes := bag.Bag[ids.ID]{}
	votes.Add(block0.ID())
	require.NoError(sm.RecordPoll(context.Background(), votes))

	// Current graph structure:
	// 0
	// Tail = 0

	require.True(!(sm.NumProcessing() > 0))
	require.Equal(block0.ID(), sm.Preference())
	require.Equal(choices.Accepted, block0.Status())
	require.Equal(choices.Rejected, block1.Status())
	require.Equal(choices.Rejected, block2.Status())
}

func RecordPollTransitivelyResetConfidenceTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          2,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	block0 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}
	block1 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}
	block2 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(3),
			StatusV: choices.Processing,
		},
		ParentV: block1.IDV,
		HeightV: block1.HeightV + 1,
	}
	block3 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(4),
			StatusV: choices.Processing,
		},
		ParentV: block1.IDV,
		HeightV: block1.HeightV + 1,
	}

	require.NoError(sm.Add(context.Background(), block0))
	require.NoError(sm.Add(context.Background(), block1))
	require.NoError(sm.Add(context.Background(), block2))
	require.NoError(sm.Add(context.Background(), block3))

	// Current graph structure:
	//   G
	//  / \
	// 0   1
	//    / \
	//   2   3

	votesFor2 := bag.Bag[ids.ID]{}
	votesFor2.Add(block2.ID())
	require.NoError(sm.RecordPoll(context.Background(), votesFor2))
	require.False(!(sm.NumProcessing() > 0))
	require.Equal(block2.ID(), sm.Preference())

	emptyVotes := bag.Bag[ids.ID]{}
	require.NoError(sm.RecordPoll(context.Background(), emptyVotes))
	require.False(!(sm.NumProcessing() > 0))
	require.Equal(block2.ID(), sm.Preference())

	require.NoError(sm.RecordPoll(context.Background(), votesFor2))
	require.False(!(sm.NumProcessing() > 0))
	require.Equal(block2.ID(), sm.Preference())

	votesFor3 := bag.Bag[ids.ID]{}
	votesFor3.Add(block3.ID())
	require.NoError(sm.RecordPoll(context.Background(), votesFor3))
	require.False(!(sm.NumProcessing() > 0))
	require.Equal(block2.ID(), sm.Preference())

	require.NoError(sm.RecordPoll(context.Background(), votesFor3))
	require.True(!(sm.NumProcessing() > 0))
	require.Equal(block3.ID(), sm.Preference())
	require.Equal(choices.Rejected, block0.Status())
	require.Equal(choices.Accepted, block1.Status())
	require.Equal(choices.Rejected, block2.Status())
	require.Equal(choices.Accepted, block3.Status())
}

func RecordPollInvalidVoteTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          2,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	block := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}
	unknownBlockID := ids.Empty.Prefix(2)

	require.NoError(sm.Add(context.Background(), block))

	validVotes := bag.Bag[ids.ID]{}
	validVotes.Add(block.ID())
	require.NoError(sm.RecordPoll(context.Background(), validVotes))

	invalidVotes := bag.Bag[ids.ID]{}
	invalidVotes.Add(unknownBlockID)
	require.NoError(sm.RecordPoll(context.Background(), invalidVotes))
	require.NoError(sm.RecordPoll(context.Background(), validVotes))
	require.False(!(sm.NumProcessing() > 0))
	require.Equal(block.ID(), sm.Preference())
}

func RecordPollTransitiveVotingTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     3,
		Alpha:                 3,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	block0 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}
	block1 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2),
			StatusV: choices.Processing,
		},
		ParentV: block0.IDV,
		HeightV: block0.HeightV + 1,
	}
	block2 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(3),
			StatusV: choices.Processing,
		},
		ParentV: block1.IDV,
		HeightV: block1.HeightV + 1,
	}
	block3 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(4),
			StatusV: choices.Processing,
		},
		ParentV: block0.IDV,
		HeightV: block0.HeightV + 1,
	}
	block4 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(5),
			StatusV: choices.Processing,
		},
		ParentV: block3.IDV,
		HeightV: block3.HeightV + 1,
	}

	require.NoError(sm.Add(context.Background(), block0))
	require.NoError(sm.Add(context.Background(), block1))
	require.NoError(sm.Add(context.Background(), block2))
	require.NoError(sm.Add(context.Background(), block3))
	require.NoError(sm.Add(context.Background(), block4))

	// Current graph structure:
	//   G
	//   |
	//   0
	//  / \
	// 1   3
	// |   |
	// 2   4
	// Tail = 2

	votes0_2_4 := bag.Bag[ids.ID]{}
	votes0_2_4.Add(
		block0.ID(),
		block2.ID(),
		block4.ID(),
	)
	require.NoError(sm.RecordPoll(context.Background(), votes0_2_4))

	// Current graph structure:
	//   0
	//  / \
	// 1   3
	// |   |
	// 2   4
	// Tail = 2

	require.False(!(sm.NumProcessing() > 0))
	require.Equal(block2.ID(), sm.Preference())
	require.Equal(choices.Accepted, block0.Status())
	require.Equal(choices.Processing, block1.Status())
	require.Equal(choices.Processing, block2.Status())
	require.Equal(choices.Processing, block3.Status())
	require.Equal(choices.Processing, block4.Status())

	dep2_2_2 := bag.Bag[ids.ID]{}
	dep2_2_2.AddCount(block2.ID(), 3)
	require.NoError(sm.RecordPoll(context.Background(), dep2_2_2))

	// Current graph structure:
	//   2
	// Tail = 2

	require.True(!(sm.NumProcessing() > 0))
	require.Equal(block2.ID(), sm.Preference())
	require.Equal(choices.Accepted, block0.Status())
	require.Equal(choices.Accepted, block1.Status())
	require.Equal(choices.Accepted, block2.Status())
	require.Equal(choices.Rejected, block3.Status())
	require.Equal(choices.Rejected, block4.Status())
}

func RecordPollDivergedVotingTest(t *testing.T, factory Factory) {
	sm := factory.New()
	require := require.New(t)

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	block0 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.ID{0x0f}, // 1111
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}
	block1 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.ID{0x08}, // 0001
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}
	block2 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.ID{0x01}, // 1000
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}
	block3 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Processing,
		},
		ParentV: block2.IDV,
		HeightV: block2.HeightV + 1,
	}

	require.NoError(sm.Add(context.Background(), block0))

	require.NoError(sm.Add(context.Background(), block1))

	// The first bit is contested as either 0 or 1. When voting for [block0] and
	// when the first bit is 1, the following bits have been decided to follow
	// the 255 remaining bits of [block0].
	votes0 := bag.Bag[ids.ID]{}
	votes0.Add(block0.ID())
	require.NoError(sm.RecordPoll(context.Background(), votes0))

	// Although we are adding in [block2] here - the underlying snowball
	// instance has already decided it is rejected. Snowman doesn't actually
	// know that though, because that is an implementation detail of the
	// Snowball trie that is used.
	require.NoError(sm.Add(context.Background(), block2))

	// Because [block2] is effectively rejected, [block3] is also effectively
	// rejected.
	require.NoError(sm.Add(context.Background(), block3))

	require.Equal(block0.ID(), sm.Preference())
	require.Equal(choices.Processing, block0.Status(), "should not be accepted yet")
	require.Equal(choices.Processing, block1.Status(), "should not be rejected yet")
	require.Equal(choices.Processing, block2.Status(), "should not be rejected yet")
	require.Equal(choices.Processing, block3.Status(), "should not be rejected yet")

	// Current graph structure:
	//       G
	//     /   \
	//    *     |
	//   / \    |
	//  0   2   1
	//      |
	//      3
	// Tail = 0

	// Transitively votes for [block2] by voting for its child [block3].
	// Because [block2] shares the first bit with [block0] and the following
	// bits have been finalized for [block0], the voting results in accepting
	// [block0]. When [block0] is accepted, [block1] and [block2] are rejected
	// as conflicting. [block2]'s child, [block3], is then rejected
	// transitively.
	votes3 := bag.Bag[ids.ID]{}
	votes3.Add(block3.ID())
	require.NoError(sm.RecordPoll(context.Background(), votes3))

	require.True(sm.NumProcessing() == 0, "finalized too late")
	require.Equal(choices.Accepted, block0.Status(), "should be accepted")
	require.Equal(choices.Rejected, block1.Status())
	require.Equal(choices.Rejected, block2.Status())
	require.Equal(choices.Rejected, block3.Status())
}

func RecordPollDivergedVotingWithNoConflictingBitTest(t *testing.T, factory Factory) {
	sm := factory.New()
	require := require.New(t)

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	block0 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.ID{0x06}, // 0110
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}
	block1 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.ID{0x08}, // 0001
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}
	block2 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.ID{0x01}, // 1000
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}
	block3 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Processing,
		},
		ParentV: block2.IDV,
		HeightV: block2.HeightV + 1,
	}

	require.NoError(sm.Add(context.Background(), block0))
	require.NoError(sm.Add(context.Background(), block1))

	// When voting for [block0], we end up finalizing the first bit as 0. The
	// second bit is contested as either 0 or 1. For when the second bit is 1,
	// the following bits have been decided to follow the 254 remaining bits of
	// [block0].
	votes0 := bag.Bag[ids.ID]{}
	votes0.Add(block0.ID())
	require.NoError(sm.RecordPoll(context.Background(), votes0))

	// Although we are adding in [block2] here - the underlying snowball
	// instance has already decided it is rejected. Snowman doesn't actually
	// know that though, because that is an implementation detail of the
	// Snowball trie that is used.
	require.NoError(sm.Add(context.Background(), block2))

	// Because [block2] is effectively rejected, [block3] is also effectively
	// rejected.
	require.NoError(sm.Add(context.Background(), block3))

	require.Equal(block0.ID(), sm.Preference())
	require.Equal(choices.Processing, block0.Status(), "should not be decided yet")
	require.Equal(choices.Processing, block1.Status(), "should not be decided yet")
	require.Equal(choices.Processing, block2.Status(), "should not be decided yet")
	require.Equal(choices.Processing, block3.Status(), "should not be decided yet")

	// Current graph structure:
	//       G
	//     /   \
	//    *     |
	//   / \    |
	//  0   1   2
	//          |
	//          3
	// Tail = 0

	// Transitively votes for [block2] by voting for its child [block3]. Because
	// [block2] doesn't share any processing bits with [block0] or [block1], the
	// votes are over only rejected bits. Therefore, the votes for [block2] are
	// dropped. Although the votes for [block3] are still applied, [block3] will
	// only be marked as accepted after [block2] is marked as accepted; which
	// will never happen.
	votes3 := bag.Bag[ids.ID]{}
	votes3.Add(block3.ID())
	require.NoError(sm.RecordPoll(context.Background(), votes3))

	require.False(sm.NumProcessing() > 0, "finalized too early")
	require.Equal(choices.Processing, block0.Status())
	require.Equal(choices.Processing, block1.Status())
	require.Equal(choices.Processing, block2.Status())
	require.Equal(choices.Processing, block3.Status())
}

func RecordPollChangePreferredChainTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          10,
		BetaRogue:             10,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	a1Block := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}
	b1Block := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}
	a2Block := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: a1Block.IDV,
		HeightV: a1Block.HeightV + 1,
	}
	b2Block := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: b1Block.IDV,
		HeightV: b1Block.HeightV + 1,
	}

	require.NoError(sm.Add(context.Background(), a1Block))
	require.NoError(sm.Add(context.Background(), a2Block))
	require.NoError(sm.Add(context.Background(), b1Block))
	require.NoError(sm.Add(context.Background(), b2Block))

	require.Equal(a2Block.ID(), sm.Preference())

	require.True(sm.IsPreferred(a1Block))
	require.True(sm.IsPreferred(a2Block))
	require.False(sm.IsPreferred(b1Block))
	require.False(sm.IsPreferred(b2Block))

	b2Votes := bag.Bag[ids.ID]{}
	b2Votes.Add(b2Block.ID())

	require.NoError(sm.RecordPoll(context.Background(), b2Votes))

	require.Equal(b2Block.ID(), sm.Preference())
	require.False(sm.IsPreferred(a1Block))
	require.False(sm.IsPreferred(a2Block))
	require.True(sm.IsPreferred(b1Block))
	require.True(sm.IsPreferred(b2Block))

	a1Votes := bag.Bag[ids.ID]{}
	a1Votes.Add(a1Block.ID())

	require.NoError(sm.RecordPoll(context.Background(), a1Votes))
	require.NoError(sm.RecordPoll(context.Background(), a1Votes))

	require.Equal(a2Block.ID(), sm.Preference())
	require.True(sm.IsPreferred(a1Block))
	require.True(sm.IsPreferred(a2Block))
	require.False(sm.IsPreferred(b1Block))
	require.False(sm.IsPreferred(b2Block))
}

func MetricsProcessingErrorTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	numProcessing := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "blks_processing",
		})

	require.NoError(ctx.Registerer.Register(numProcessing))

	err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp)
	require.Error(err) //nolint:forbidigo // error is not exported https://github.com/prometheus/client_golang/blob/main/prometheus/registry.go#L315
}

func MetricsAcceptedErrorTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	numAccepted := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "blks_accepted_count",
		})

	require.NoError(ctx.Registerer.Register(numAccepted))

	err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp)
	require.Error(err) //nolint:forbidigo // error is not exported https://github.com/prometheus/client_golang/blob/main/prometheus/registry.go#L315
}

func MetricsRejectedErrorTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	numRejected := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "blks_rejected_count",
		})

	require.NoError(ctx.Registerer.Register(numRejected))

	err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp)
	require.Error(err) //nolint:forbidigo // error is not exported https://github.com/prometheus/client_golang/blob/main/prometheus/registry.go#L315
}

func ErrorOnInitialRejectionTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	rejectedBlock := &TestBlock{TestDecidable: choices.TestDecidable{
		IDV:     ids.Empty.Prefix(1),
		StatusV: choices.Rejected,
	}}

	block := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2),
			RejectV: errTest,
			StatusV: choices.Processing,
		},
		ParentV: rejectedBlock.IDV,
		HeightV: rejectedBlock.HeightV + 1,
	}

	err := sm.Add(context.Background(), block)
	require.ErrorIs(err, errTest)
}

func ErrorOnAcceptTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	block := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			AcceptV: errTest,
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}

	require.NoError(sm.Add(context.Background(), block))

	votes := bag.Bag[ids.ID]{}
	votes.Add(block.ID())
	err := sm.RecordPoll(context.Background(), votes)
	require.ErrorIs(err, errTest)
}

func ErrorOnRejectSiblingTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	block0 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}
	block1 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2),
			RejectV: errTest,
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}

	require.NoError(sm.Add(context.Background(), block0))
	require.NoError(sm.Add(context.Background(), block1))

	votes := bag.Bag[ids.ID]{}
	votes.Add(block0.ID())
	err := sm.RecordPoll(context.Background(), votes)
	require.ErrorIs(err, errTest)
}

func ErrorOnTransitiveRejectionTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	block0 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}
	block1 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}
	block2 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(3),
			RejectV: errTest,
			StatusV: choices.Processing,
		},
		ParentV: block1.IDV,
		HeightV: block1.HeightV + 1,
	}

	require.NoError(sm.Add(context.Background(), block0))
	require.NoError(sm.Add(context.Background(), block1))
	require.NoError(sm.Add(context.Background(), block2))

	votes := bag.Bag[ids.ID]{}
	votes.Add(block0.ID())
	err := sm.RecordPoll(context.Background(), votes)
	require.ErrorIs(err, errTest)
}

func RandomizedConsistencyTest(t *testing.T, factory Factory) {
	require := require.New(t)

	numColors := 50
	numNodes := 100
	params := snowball.Parameters{
		K:                     20,
		Alpha:                 15,
		BetaVirtuous:          20,
		BetaRogue:             30,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	seed := int64(0)

	sampler.Seed(seed)

	n := Network{}
	n.Initialize(params, numColors)

	for i := 0; i < numNodes; i++ {
		require.NoError(n.AddNode(factory.New()))
	}

	for n.NumProcessing() > 0 {
		require.NoError(n.Round())
	}

	require.True(n.Agreement())
}

func ErrorOnAddDecidedBlock(t *testing.T, factory Factory) {
	sm := factory.New()
	require := require.New(t)

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	block0 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.ID{0x03}, // 0b0011
			StatusV: choices.Accepted,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}
	err := sm.Add(context.Background(), block0)
	require.ErrorIs(err, errDuplicateAdd)
}

func ErrorOnAddDuplicateBlockID(t *testing.T, factory Factory) {
	sm := factory.New()
	require := require.New(t)

	ctx := snow.DefaultConsensusContextTest()
	params := snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))

	block0 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.ID{0x03}, // 0b0011
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}
	block1 := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.ID{0x03}, // 0b0011, same as block0
			StatusV: choices.Processing,
		},
		ParentV: block0.IDV,
		HeightV: block0.HeightV + 1,
	}

	require.NoError(sm.Add(context.Background(), block0))
	err := sm.Add(context.Background(), block1)
	require.ErrorIs(err, errDuplicateAdd)
}

func gatherCounterGauge(t *testing.T, reg *prometheus.Registry) map[string]float64 {
	ms, err := reg.Gather()
	require.NoError(t, err)
	mss := make(map[string]float64)
	for _, mf := range ms {
		name := mf.GetName()
		for _, m := range mf.GetMetric() {
			cnt := m.GetCounter()
			if cnt != nil {
				mss[name] = cnt.GetValue()
				break
			}
			gg := m.GetGauge()
			if gg != nil {
				mss[name] = gg.GetValue()
				break
			}
		}
	}
	return mss
}
