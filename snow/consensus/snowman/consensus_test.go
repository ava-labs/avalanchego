// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
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

	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err != nil {
		t.Fatal(err)
	}

	if pref := sm.Preference(); pref != GenesisID {
		t.Fatalf("Wrong preference returned")
	} else if !sm.Finalized() {
		t.Fatalf("Wrong should have marked the instance as being finalized")
	}
}

// Make sure that the number of processing blocks is tracked correctly
func NumProcessingTest(t *testing.T, factory Factory) {
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
	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err != nil {
		t.Fatal(err)
	}

	block := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}

	if numProcessing := sm.NumProcessing(); numProcessing != 0 {
		t.Fatalf("expected %d blocks to be processing but returned %d", 0, numProcessing)
	}

	// Adding to the previous preference will update the preference
	if err := sm.Add(context.Background(), block); err != nil {
		t.Fatal(err)
	}

	if numProcessing := sm.NumProcessing(); numProcessing != 1 {
		t.Fatalf("expected %d blocks to be processing but returned %d", 1, numProcessing)
	}

	votes := ids.Bag{}
	votes.Add(block.ID())
	if err := sm.RecordPoll(context.Background(), votes); err != nil {
		t.Fatal(err)
	}

	if numProcessing := sm.NumProcessing(); numProcessing != 0 {
		t.Fatalf("expected %d blocks to be processing but returned %d", 0, numProcessing)
	}
}

// Make sure that adding a block to the tail updates the preference
func AddToTailTest(t *testing.T, factory Factory) {
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
	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err != nil {
		t.Fatal(err)
	}

	block := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}

	// Adding to the previous preference will update the preference
	if err := sm.Add(context.Background(), block); err != nil {
		t.Fatal(err)
	} else if pref := sm.Preference(); pref != block.ID() {
		t.Fatalf("Wrong preference. Expected %s, got %s", block.ID(), pref)
	} else if !sm.IsPreferred(block) {
		t.Fatalf("Should have marked %s as being Preferred", pref)
	}
}

// Make sure that adding a block not to the tail doesn't change the preference
func AddToNonTailTest(t *testing.T, factory Factory) {
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
	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err != nil {
		t.Fatal(err)
	}

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
	if err := sm.Add(context.Background(), firstBlock); err != nil {
		t.Fatal(err)
	} else if pref := sm.Preference(); pref != firstBlock.IDV {
		t.Fatalf("Wrong preference. Expected %s, got %s", firstBlock.IDV, pref)
	}

	// Adding to something other than the previous preference won't update the
	// preference
	if err := sm.Add(context.Background(), secondBlock); err != nil {
		t.Fatal(err)
	} else if pref := sm.Preference(); pref != firstBlock.IDV {
		t.Fatalf("Wrong preference. Expected %s, got %s", firstBlock.IDV, pref)
	}
}

// Make sure that adding a block that is detached from the rest of the tree
// rejects the block
func AddToUnknownTest(t *testing.T, factory Factory) {
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
	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err != nil {
		t.Fatal(err)
	}

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
	if err := sm.Add(context.Background(), block); err != nil {
		t.Fatal(err)
	} else if pref := sm.Preference(); pref != GenesisID {
		t.Fatalf("Wrong preference. Expected %s, got %s", GenesisID, pref)
	} else if status := block.Status(); status != choices.Rejected {
		t.Fatalf("Should have rejected the block")
	}
}

func StatusOrProcessingPreviouslyAcceptedTest(t *testing.T, factory Factory) {
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
	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err != nil {
		t.Fatal(err)
	}

	if Genesis.Status() != choices.Accepted {
		t.Fatalf("Should have marked an accepted block as having been accepted")
	}
	if sm.Processing(Genesis.ID()) {
		t.Fatalf("Shouldn't have marked an accepted block as having been processing")
	}
	if !sm.Decided(Genesis) {
		t.Fatalf("Should have marked an accepted block as having been decided")
	}
	if !sm.IsPreferred(Genesis) {
		t.Fatalf("Should have marked an accepted block as being preferred")
	}
}

func StatusOrProcessingPreviouslyRejectedTest(t *testing.T, factory Factory) {
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
	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err != nil {
		t.Fatal(err)
	}

	block := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Rejected,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}

	if block.Status() == choices.Accepted {
		t.Fatalf("Shouldn't have marked a rejected block as having been accepted")
	}
	if sm.Processing(block.ID()) {
		t.Fatalf("Shouldn't have marked a rejected block as having been processing")
	}
	if !sm.Decided(block) {
		t.Fatalf("Should have marked a rejected block as having been decided")
	}
	if sm.IsPreferred(block) {
		t.Fatalf("Shouldn't have marked a rejected block as being preferred")
	}
}

func StatusOrProcessingUnissuedTest(t *testing.T, factory Factory) {
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
	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err != nil {
		t.Fatal(err)
	}

	block := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}

	if block.Status() == choices.Accepted {
		t.Fatalf("Shouldn't have marked an unissued block as having been accepted")
	}
	if sm.Processing(block.ID()) {
		t.Fatalf("Shouldn't have marked an unissued block as having been processing")
	}
	if sm.Decided(block) {
		t.Fatalf("Should't have marked an unissued block as having been decided")
	}
	if sm.IsPreferred(block) {
		t.Fatalf("Shouldn't have marked an unissued block as being preferred")
	}
}

func StatusOrProcessingIssuedTest(t *testing.T, factory Factory) {
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
	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err != nil {
		t.Fatal(err)
	}

	block := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}

	if err := sm.Add(context.Background(), block); err != nil {
		t.Fatal(err)
	}
	if block.Status() == choices.Accepted {
		t.Fatalf("Shouldn't have marked the block as accepted")
	}
	if !sm.Processing(block.ID()) {
		t.Fatalf("Should have marked the block as processing")
	}
	if sm.Decided(block) {
		t.Fatalf("Shouldn't have marked the block as decided")
	}
	if !sm.IsPreferred(block) {
		t.Fatalf("Should have marked the tail as being preferred")
	}
}

func RecordPollAcceptSingleBlockTest(t *testing.T, factory Factory) {
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
	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err != nil {
		t.Fatal(err)
	}

	block := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}

	if err := sm.Add(context.Background(), block); err != nil {
		t.Fatal(err)
	}

	votes := ids.Bag{}
	votes.Add(block.ID())
	if err := sm.RecordPoll(context.Background(), votes); err != nil {
		t.Fatal(err)
	} else if pref := sm.Preference(); pref != block.ID() {
		t.Fatalf("Preference returned the wrong block")
	} else if sm.Finalized() {
		t.Fatalf("Snowman instance finalized too soon")
	} else if status := block.Status(); status != choices.Processing {
		t.Fatalf("Block's status changed unexpectedly")
	} else if err := sm.RecordPoll(context.Background(), votes); err != nil {
		t.Fatal(err)
	} else if pref := sm.Preference(); pref != block.ID() {
		t.Fatalf("Preference returned the wrong block")
	} else if !sm.Finalized() {
		t.Fatalf("Snowman instance didn't finalize")
	} else if status := block.Status(); status != choices.Accepted {
		t.Fatalf("Block's status should have been set to accepted")
	}
}

func RecordPollAcceptAndRejectTest(t *testing.T, factory Factory) {
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
	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err != nil {
		t.Fatal(err)
	}

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

	if err := sm.Add(context.Background(), firstBlock); err != nil {
		t.Fatal(err)
	} else if err := sm.Add(context.Background(), secondBlock); err != nil {
		t.Fatal(err)
	}

	votes := ids.Bag{}
	votes.Add(firstBlock.ID())

	if err := sm.RecordPoll(context.Background(), votes); err != nil {
		t.Fatal(err)
	} else if pref := sm.Preference(); pref != firstBlock.ID() {
		t.Fatalf("Preference returned the wrong block")
	} else if sm.Finalized() {
		t.Fatalf("Snowman instance finalized too soon")
	} else if status := firstBlock.Status(); status != choices.Processing {
		t.Fatalf("Block's status changed unexpectedly")
	} else if status := secondBlock.Status(); status != choices.Processing {
		t.Fatalf("Block's status changed unexpectedly")
	} else if err := sm.RecordPoll(context.Background(), votes); err != nil {
		t.Fatal(err)
	} else if pref := sm.Preference(); pref != firstBlock.ID() {
		t.Fatalf("Preference returned the wrong block")
	} else if !sm.Finalized() {
		t.Fatalf("Snowman instance didn't finalize")
	} else if status := firstBlock.Status(); status != choices.Accepted {
		t.Fatalf("Block's status should have been set to accepted")
	} else if status := secondBlock.Status(); status != choices.Rejected {
		t.Fatalf("Block's status should have been set to rejected")
	}
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

	votes := ids.Bag{}
	votes.Add(firstBlock.ID())
	votes.Add(secondBlock.ID())

	// The first poll will accept shared bits
	require.NoError(sm.RecordPoll(context.Background(), votes))
	require.Equal(firstBlock.ID(), sm.Preference())
	require.False(sm.Finalized())

	metrics := gatherCounterGauge(t, registerer)
	require.EqualValues(0, metrics["polls_failed"])
	require.EqualValues(1, metrics["polls_successful"])

	// The second poll will do nothing
	require.NoError(sm.RecordPoll(context.Background(), votes))
	require.Equal(firstBlock.ID(), sm.Preference())
	require.False(sm.Finalized())

	metrics = gatherCounterGauge(t, registerer)
	require.EqualValues(1, metrics["polls_failed"])
	require.EqualValues(1, metrics["polls_successful"])
}

func RecordPollWhenFinalizedTest(t *testing.T, factory Factory) {
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
	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err != nil {
		t.Fatal(err)
	}

	votes := ids.Bag{}
	votes.Add(GenesisID)
	if err := sm.RecordPoll(context.Background(), votes); err != nil {
		t.Fatal(err)
	} else if !sm.Finalized() {
		t.Fatalf("Consensus should still be finalized")
	} else if pref := sm.Preference(); GenesisID != pref {
		t.Fatalf("Wrong preference listed")
	}
}

func RecordPollRejectTransitivelyTest(t *testing.T, factory Factory) {
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
	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err != nil {
		t.Fatal(err)
	}

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

	if err := sm.Add(context.Background(), block0); err != nil {
		t.Fatal(err)
	} else if err := sm.Add(context.Background(), block1); err != nil {
		t.Fatal(err)
	} else if err := sm.Add(context.Background(), block2); err != nil {
		t.Fatal(err)
	}

	// Current graph structure:
	//   G
	//  / \
	// 0   1
	//     |
	//     2
	// Tail = 0

	votes := ids.Bag{}
	votes.Add(block0.ID())
	if err := sm.RecordPoll(context.Background(), votes); err != nil {
		t.Fatal(err)
	}

	// Current graph structure:
	// 0
	// Tail = 0

	if !sm.Finalized() {
		t.Fatalf("Finalized too late")
	} else if pref := sm.Preference(); block0.ID() != pref {
		t.Fatalf("Wrong preference listed")
	} else if status := block0.Status(); status != choices.Accepted {
		t.Fatalf("Wrong status returned")
	} else if status := block1.Status(); status != choices.Rejected {
		t.Fatalf("Wrong status returned")
	} else if status := block2.Status(); status != choices.Rejected {
		t.Fatalf("Wrong status returned")
	}
}

func RecordPollTransitivelyResetConfidenceTest(t *testing.T, factory Factory) {
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
	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err != nil {
		t.Fatal(err)
	}

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

	if err := sm.Add(context.Background(), block0); err != nil {
		t.Fatal(err)
	} else if err := sm.Add(context.Background(), block1); err != nil {
		t.Fatal(err)
	} else if err := sm.Add(context.Background(), block2); err != nil {
		t.Fatal(err)
	} else if err := sm.Add(context.Background(), block3); err != nil {
		t.Fatal(err)
	}

	// Current graph structure:
	//   G
	//  / \
	// 0   1
	//    / \
	//   2   3

	votesFor2 := ids.Bag{}
	votesFor2.Add(block2.ID())
	if err := sm.RecordPoll(context.Background(), votesFor2); err != nil {
		t.Fatal(err)
	} else if sm.Finalized() {
		t.Fatalf("Finalized too early")
	} else if pref := sm.Preference(); block2.ID() != pref {
		t.Fatalf("Wrong preference listed")
	}

	emptyVotes := ids.Bag{}
	if err := sm.RecordPoll(context.Background(), emptyVotes); err != nil {
		t.Fatal(err)
	} else if sm.Finalized() {
		t.Fatalf("Finalized too early")
	} else if pref := sm.Preference(); block2.ID() != pref {
		t.Fatalf("Wrong preference listed")
	} else if err := sm.RecordPoll(context.Background(), votesFor2); err != nil {
		t.Fatal(err)
	} else if sm.Finalized() {
		t.Fatalf("Finalized too early")
	} else if pref := sm.Preference(); block2.ID() != pref {
		t.Fatalf("Wrong preference listed")
	}

	votesFor3 := ids.Bag{}
	votesFor3.Add(block3.ID())
	if err := sm.RecordPoll(context.Background(), votesFor3); err != nil {
		t.Fatal(err)
	} else if sm.Finalized() {
		t.Fatalf("Finalized too early")
	} else if pref := sm.Preference(); block2.ID() != pref {
		t.Fatalf("Wrong preference listed")
	} else if err := sm.RecordPoll(context.Background(), votesFor3); err != nil {
		t.Fatal(err)
	} else if !sm.Finalized() {
		t.Fatalf("Finalized too late")
	} else if pref := sm.Preference(); block3.ID() != pref {
		t.Fatalf("Wrong preference listed")
	} else if status := block0.Status(); status != choices.Rejected {
		t.Fatalf("Wrong status returned")
	} else if status := block1.Status(); status != choices.Accepted {
		t.Fatalf("Wrong status returned")
	} else if status := block2.Status(); status != choices.Rejected {
		t.Fatalf("Wrong status returned")
	} else if status := block3.Status(); status != choices.Accepted {
		t.Fatalf("Wrong status returned")
	}
}

func RecordPollInvalidVoteTest(t *testing.T, factory Factory) {
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
	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err != nil {
		t.Fatal(err)
	}

	block := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}
	unknownBlockID := ids.Empty.Prefix(2)

	if err := sm.Add(context.Background(), block); err != nil {
		t.Fatal(err)
	}

	validVotes := ids.Bag{}
	validVotes.Add(block.ID())
	if err := sm.RecordPoll(context.Background(), validVotes); err != nil {
		t.Fatal(err)
	}

	invalidVotes := ids.Bag{}
	invalidVotes.Add(unknownBlockID)
	if err := sm.RecordPoll(context.Background(), invalidVotes); err != nil {
		t.Fatal(err)
	} else if err := sm.RecordPoll(context.Background(), validVotes); err != nil {
		t.Fatal(err)
	} else if sm.Finalized() {
		t.Fatalf("Finalized too early")
	} else if pref := sm.Preference(); block.ID() != pref {
		t.Fatalf("Wrong preference listed")
	}
}

func RecordPollTransitiveVotingTest(t *testing.T, factory Factory) {
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
	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err != nil {
		t.Fatal(err)
	}

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

	if err := sm.Add(context.Background(), block0); err != nil {
		t.Fatal(err)
	} else if err := sm.Add(context.Background(), block1); err != nil {
		t.Fatal(err)
	} else if err := sm.Add(context.Background(), block2); err != nil {
		t.Fatal(err)
	} else if err := sm.Add(context.Background(), block3); err != nil {
		t.Fatal(err)
	} else if err := sm.Add(context.Background(), block4); err != nil {
		t.Fatal(err)
	}

	// Current graph structure:
	//   G
	//   |
	//   0
	//  / \
	// 1   3
	// |   |
	// 2   4
	// Tail = 2

	votes0_2_4 := ids.Bag{}
	votes0_2_4.Add(
		block0.ID(),
		block2.ID(),
		block4.ID(),
	)
	if err := sm.RecordPoll(context.Background(), votes0_2_4); err != nil {
		t.Fatal(err)
	}

	// Current graph structure:
	//   0
	//  / \
	// 1   3
	// |   |
	// 2   4
	// Tail = 2

	pref := sm.Preference()
	switch {
	case block2.ID() != pref:
		t.Fatalf("Wrong preference listed")
	case sm.Finalized():
		t.Fatalf("Finalized too early")
	case block0.Status() != choices.Accepted:
		t.Fatalf("Should have accepted")
	case block1.Status() != choices.Processing:
		t.Fatalf("Should have accepted")
	case block2.Status() != choices.Processing:
		t.Fatalf("Should have accepted")
	case block3.Status() != choices.Processing:
		t.Fatalf("Should have rejected")
	case block4.Status() != choices.Processing:
		t.Fatalf("Should have rejected")
	}

	dep2_2_2 := ids.Bag{}
	dep2_2_2.AddCount(block2.ID(), 3)
	if err := sm.RecordPoll(context.Background(), dep2_2_2); err != nil {
		t.Fatal(err)
	}

	// Current graph structure:
	//   2
	// Tail = 2

	pref = sm.Preference()
	switch {
	case block2.ID() != pref:
		t.Fatalf("Wrong preference listed")
	case !sm.Finalized():
		t.Fatalf("Finalized too late")
	case block0.Status() != choices.Accepted:
		t.Fatalf("Should have accepted")
	case block1.Status() != choices.Accepted:
		t.Fatalf("Should have accepted")
	case block2.Status() != choices.Accepted:
		t.Fatalf("Should have accepted")
	case block3.Status() != choices.Rejected:
		t.Fatalf("Should have rejected")
	case block4.Status() != choices.Rejected:
		t.Fatalf("Should have rejected")
	}
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
	err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp)
	require.NoError(err)

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

	err = sm.Add(context.Background(), block0)
	require.NoError(err)

	err = sm.Add(context.Background(), block1)
	require.NoError(err)

	// The first bit is contested as either 0 or 1. When voting for [block0] and
	// when the first bit is 1, the following bits have been decided to follow
	// the 255 remaining bits of [block0].
	votes0 := ids.Bag{}
	votes0.Add(block0.ID())
	err = sm.RecordPoll(context.Background(), votes0)
	require.NoError(err)

	// Although we are adding in [block2] here - the underlying snowball
	// instance has already decided it is rejected. Snowman doesn't actually
	// know that though, because that is an implementation detail of the
	// Snowball trie that is used.
	err = sm.Add(context.Background(), block2)
	require.NoError(err)

	// Because [block2] is effectively rejected, [block3] is also effectively
	// rejected.
	err = sm.Add(context.Background(), block3)
	require.NoError(err)

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
	votes3 := ids.Bag{}
	votes3.Add(block3.ID())
	err = sm.RecordPoll(context.Background(), votes3)
	require.NoError(err)

	require.True(sm.Finalized(), "finalized too late")
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
	votes0 := ids.Bag{}
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
	votes3 := ids.Bag{}
	votes3.Add(block3.ID())
	require.NoError(sm.RecordPoll(context.Background(), votes3))

	require.False(sm.Finalized(), "finalized too early")
	require.Equal(choices.Processing, block0.Status())
	require.Equal(choices.Processing, block1.Status())
	require.Equal(choices.Processing, block2.Status())
	require.Equal(choices.Processing, block3.Status())
}

func RecordPollChangePreferredChainTest(t *testing.T, factory Factory) {
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
	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err != nil {
		t.Fatal(err)
	}

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

	if err := sm.Add(context.Background(), a1Block); err != nil {
		t.Fatal(err)
	}
	if err := sm.Add(context.Background(), a2Block); err != nil {
		t.Fatal(err)
	}
	if err := sm.Add(context.Background(), b1Block); err != nil {
		t.Fatal(err)
	}
	if err := sm.Add(context.Background(), b2Block); err != nil {
		t.Fatal(err)
	}

	if sm.Preference() != a2Block.ID() {
		t.Fatal("Wrong preference reported")
	}

	if !sm.IsPreferred(a1Block) {
		t.Fatalf("Should have reported a1 as being preferred")
	}
	if !sm.IsPreferred(a2Block) {
		t.Fatalf("Should have reported a2 as being preferred")
	}
	if sm.IsPreferred(b1Block) {
		t.Fatalf("Shouldn't have reported b1 as being preferred")
	}
	if sm.IsPreferred(b2Block) {
		t.Fatalf("Shouldn't have reported b2 as being preferred")
	}

	b2Votes := ids.Bag{}
	b2Votes.Add(b2Block.ID())

	if err := sm.RecordPoll(context.Background(), b2Votes); err != nil {
		t.Fatal(err)
	}

	if sm.Preference() != b2Block.ID() {
		t.Fatal("Wrong preference reported")
	}

	if sm.IsPreferred(a1Block) {
		t.Fatalf("Shouldn't have reported a1 as being preferred")
	}
	if sm.IsPreferred(a2Block) {
		t.Fatalf("Shouldn't have reported a2 as being preferred")
	}
	if !sm.IsPreferred(b1Block) {
		t.Fatalf("Should have reported b1 as being preferred")
	}
	if !sm.IsPreferred(b2Block) {
		t.Fatalf("Should have reported b2 as being preferred")
	}

	a1Votes := ids.Bag{}
	a1Votes.Add(a1Block.ID())

	if err := sm.RecordPoll(context.Background(), a1Votes); err != nil {
		t.Fatal(err)
	}
	if err := sm.RecordPoll(context.Background(), a1Votes); err != nil {
		t.Fatal(err)
	}

	if sm.Preference() != a2Block.ID() {
		t.Fatal("Wrong preference reported")
	}

	if !sm.IsPreferred(a1Block) {
		t.Fatalf("Should have reported a1 as being preferred")
	}
	if !sm.IsPreferred(a2Block) {
		t.Fatalf("Should have reported a2 as being preferred")
	}
	if sm.IsPreferred(b1Block) {
		t.Fatalf("Shouldn't have reported b1 as being preferred")
	}
	if sm.IsPreferred(b2Block) {
		t.Fatalf("Shouldn't have reported b2 as being preferred")
	}
}

func MetricsProcessingErrorTest(t *testing.T, factory Factory) {
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

	if err := ctx.Registerer.Register(numProcessing); err != nil {
		t.Fatal(err)
	}

	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err == nil {
		t.Fatalf("should have errored during initialization due to a duplicate metric")
	}
}

func MetricsAcceptedErrorTest(t *testing.T, factory Factory) {
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

	if err := ctx.Registerer.Register(numAccepted); err != nil {
		t.Fatal(err)
	}

	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err == nil {
		t.Fatalf("should have errored during initialization due to a duplicate metric")
	}
}

func MetricsRejectedErrorTest(t *testing.T, factory Factory) {
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

	if err := ctx.Registerer.Register(numRejected); err != nil {
		t.Fatal(err)
	}

	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err == nil {
		t.Fatalf("should have errored during initialization due to a duplicate metric")
	}
}

func ErrorOnInitialRejectionTest(t *testing.T, factory Factory) {
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

	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err != nil {
		t.Fatal(err)
	}

	rejectedBlock := &TestBlock{TestDecidable: choices.TestDecidable{
		IDV:     ids.Empty.Prefix(1),
		StatusV: choices.Rejected,
	}}

	block := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2),
			RejectV: errors.New(""),
			StatusV: choices.Processing,
		},
		ParentV: rejectedBlock.IDV,
		HeightV: rejectedBlock.HeightV + 1,
	}

	if err := sm.Add(context.Background(), block); err == nil {
		t.Fatalf("Should have errored on rejecting the rejectable block")
	}
}

func ErrorOnAcceptTest(t *testing.T, factory Factory) {
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

	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err != nil {
		t.Fatal(err)
	}

	block := &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1),
			AcceptV: errors.New(""),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}

	if err := sm.Add(context.Background(), block); err != nil {
		t.Fatal(err)
	}

	votes := ids.Bag{}
	votes.Add(block.ID())
	if err := sm.RecordPoll(context.Background(), votes); err == nil {
		t.Fatalf("Should have errored on accepted the block")
	}
}

func ErrorOnRejectSiblingTest(t *testing.T, factory Factory) {
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

	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err != nil {
		t.Fatal(err)
	}

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
			RejectV: errors.New(""),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.IDV,
		HeightV: Genesis.HeightV + 1,
	}

	if err := sm.Add(context.Background(), block0); err != nil {
		t.Fatal(err)
	} else if err := sm.Add(context.Background(), block1); err != nil {
		t.Fatal(err)
	}

	votes := ids.Bag{}
	votes.Add(block0.ID())
	if err := sm.RecordPoll(context.Background(), votes); err == nil {
		t.Fatalf("Should have errored on rejecting the block's sibling")
	}
}

func ErrorOnTransitiveRejectionTest(t *testing.T, factory Factory) {
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

	if err := sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp); err != nil {
		t.Fatal(err)
	}

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
			RejectV: errors.New(""),
			StatusV: choices.Processing,
		},
		ParentV: block1.IDV,
		HeightV: block1.HeightV + 1,
	}

	if err := sm.Add(context.Background(), block0); err != nil {
		t.Fatal(err)
	} else if err := sm.Add(context.Background(), block1); err != nil {
		t.Fatal(err)
	} else if err := sm.Add(context.Background(), block2); err != nil {
		t.Fatal(err)
	}

	votes := ids.Bag{}
	votes.Add(block0.ID())
	if err := sm.RecordPoll(context.Background(), votes); err == nil {
		t.Fatalf("Should have errored on transitively rejecting the block")
	}
}

func RandomizedConsistencyTest(t *testing.T, factory Factory) {
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
		if err := n.AddNode(factory.New()); err != nil {
			t.Fatal(err)
		}
	}

	for !n.Finalized() {
		if err := n.Round(); err != nil {
			t.Fatal(err)
		}
	}

	if !n.Agreement() {
		t.Fatalf("Network agreed on inconsistent values")
	}
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
	require.ErrorIs(sm.Add(context.Background(), block0), errDuplicateAdd)
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
	require.ErrorIs(sm.Add(context.Background(), block1), errDuplicateAdd)
}

func gatherCounterGauge(t *testing.T, reg *prometheus.Registry) map[string]float64 {
	ms, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
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
