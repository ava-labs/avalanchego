// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"math/rand"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowball"
)

var (
	GenesisID = ids.Empty.Prefix(0)
	Genesis   = &TestBlock{
		id:     GenesisID,
		status: choices.Accepted,
	}

	Tests = []func(*testing.T, Factory){
		InitializeTest,
		AddToTailTest,
		AddToNonTailTest,
		AddToUnknownTest,
		IssuedPreviouslyAcceptedTest,
		IssuedPreviouslyRejectedTest,
		IssuedUnissuedTest,
		IssuedIssuedTest,
		RecordPollAcceptSingleBlockTest,
		RecordPollAcceptAndRejectTest,
		RecordPollWhenFinalizedTest,
		RecordPollRejectTransitivelyTest,
		RecordPollTransitivelyResetConfidenceTest,
		RecordPollInvalidVoteTest,
		RecordPollTransitiveVotingTest,
		RecordPollDivergedVotingTest,
		MetricsProcessingErrorTest,
		MetricsAcceptedErrorTest,
		MetricsRejectedErrorTest,
		RandomizedConsistencyTest,
	}
)

// Execute all tests against a consensus implementation
func ConsensusTest(t *testing.T, factory Factory) {
	for _, test := range Tests {
		test(t, factory)
	}
}

// Make sure that initialize sets the state correctly
func InitializeTest(t *testing.T, factory Factory) {
	sm := factory.New()

	ctx := snow.DefaultContextTest()
	params := snowball.Parameters{
		Metrics:           prometheus.NewRegistry(),
		K:                 1,
		Alpha:             1,
		BetaVirtuous:      3,
		BetaRogue:         5,
		ConcurrentRepolls: 1,
	}

	sm.Initialize(ctx, params, GenesisID)

	if p := sm.Parameters(); p != params {
		t.Fatalf("Wrong returned parameters")
	}
	if pref := sm.Preference(); !pref.Equals(GenesisID) {
		t.Fatalf("Wrong preference returned")
	}
	if !sm.Finalized() {
		t.Fatalf("Wrong should have marked the instance as being finalized")
	}
}

// Make sure that adding a block to the tail updates the preference
func AddToTailTest(t *testing.T, factory Factory) {
	sm := factory.New()

	ctx := snow.DefaultContextTest()
	params := snowball.Parameters{
		Metrics:           prometheus.NewRegistry(),
		K:                 1,
		Alpha:             1,
		BetaVirtuous:      3,
		BetaRogue:         5,
		ConcurrentRepolls: 1,
	}
	sm.Initialize(ctx, params, GenesisID)

	block := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(1),
	}

	// Adding to the previous preference will update the preference
	sm.Add(block)

	if pref := sm.Preference(); !pref.Equals(block.id) {
		t.Fatalf("Wrong preference. Expected %s, got %s", block.id, pref)
	}
}

// Make sure that adding a block not to the tail doesn't change the preference
func AddToNonTailTest(t *testing.T, factory Factory) {
	sm := factory.New()

	ctx := snow.DefaultContextTest()
	params := snowball.Parameters{
		Metrics:           prometheus.NewRegistry(),
		K:                 1,
		Alpha:             1,
		BetaVirtuous:      3,
		BetaRogue:         5,
		ConcurrentRepolls: 1,
	}
	sm.Initialize(ctx, params, GenesisID)

	firstBlock := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(1),
	}
	secondBlock := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(2),
	}

	// Adding to the previous preference will update the preference
	sm.Add(firstBlock)

	if pref := sm.Preference(); !pref.Equals(firstBlock.id) {
		t.Fatalf("Wrong preference. Expected %s, got %s", firstBlock.id, pref)
	}

	// Adding to something other than the previous preference won't update the
	// preference
	sm.Add(secondBlock)

	if pref := sm.Preference(); !pref.Equals(firstBlock.id) {
		t.Fatalf("Wrong preference. Expected %s, got %s", firstBlock.id, pref)
	}
}

// Make sure that adding a block that is detached from the rest of the tree
// rejects the block
func AddToUnknownTest(t *testing.T, factory Factory) {
	sm := factory.New()

	ctx := snow.DefaultContextTest()
	params := snowball.Parameters{
		Metrics:           prometheus.NewRegistry(),
		K:                 1,
		Alpha:             1,
		BetaVirtuous:      3,
		BetaRogue:         5,
		ConcurrentRepolls: 1,
	}
	sm.Initialize(ctx, params, GenesisID)

	block := &TestBlock{
		parent: &TestBlock{id: ids.Empty.Prefix(1)},
		id:     ids.Empty.Prefix(2),
	}

	// Adding a block with an unknown parent means the parent must have already
	// been rejected. Therefore the block should be immediately rejected
	sm.Add(block)

	if pref := sm.Preference(); !pref.Equals(GenesisID) {
		t.Fatalf("Wrong preference. Expected %s, got %s", GenesisID, pref)
	} else if status := block.Status(); status != choices.Rejected {
		t.Fatalf("Should have rejected the block")
	}
}

func IssuedPreviouslyAcceptedTest(t *testing.T, factory Factory) {
	sm := factory.New()

	ctx := snow.DefaultContextTest()
	params := snowball.Parameters{
		Metrics:           prometheus.NewRegistry(),
		K:                 1,
		Alpha:             1,
		BetaVirtuous:      3,
		BetaRogue:         5,
		ConcurrentRepolls: 1,
	}
	sm.Initialize(ctx, params, GenesisID)

	if !sm.Issued(Genesis) {
		t.Fatalf("Should have marked an accepted block as having been issued")
	}
}

func IssuedPreviouslyRejectedTest(t *testing.T, factory Factory) {
	sm := factory.New()

	ctx := snow.DefaultContextTest()
	params := snowball.Parameters{
		Metrics:           prometheus.NewRegistry(),
		K:                 1,
		Alpha:             1,
		BetaVirtuous:      3,
		BetaRogue:         5,
		ConcurrentRepolls: 1,
	}
	sm.Initialize(ctx, params, GenesisID)

	block := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(1),
		status: choices.Rejected,
	}

	if !sm.Issued(block) {
		t.Fatalf("Should have marked a rejected block as having been issued")
	}
}

func IssuedUnissuedTest(t *testing.T, factory Factory) {
	sm := factory.New()

	ctx := snow.DefaultContextTest()
	params := snowball.Parameters{
		Metrics:           prometheus.NewRegistry(),
		K:                 1,
		Alpha:             1,
		BetaVirtuous:      3,
		BetaRogue:         5,
		ConcurrentRepolls: 1,
	}
	sm.Initialize(ctx, params, GenesisID)

	block := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(1),
		status: choices.Processing,
	}

	if sm.Issued(block) {
		t.Fatalf("Shouldn't have marked an unissued block as having been issued")
	}
}

func IssuedIssuedTest(t *testing.T, factory Factory) {
	sm := factory.New()

	ctx := snow.DefaultContextTest()
	params := snowball.Parameters{
		Metrics:           prometheus.NewRegistry(),
		K:                 1,
		Alpha:             1,
		BetaVirtuous:      3,
		BetaRogue:         5,
		ConcurrentRepolls: 1,
	}
	sm.Initialize(ctx, params, GenesisID)

	block := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(1),
		status: choices.Processing,
	}

	sm.Add(block)

	if !sm.Issued(block) {
		t.Fatalf("Should have marked a pending block as having been issued")
	}
}

func RecordPollAcceptSingleBlockTest(t *testing.T, factory Factory) {
	sm := factory.New()

	ctx := snow.DefaultContextTest()
	params := snowball.Parameters{
		Metrics:           prometheus.NewRegistry(),
		K:                 1,
		Alpha:             1,
		BetaVirtuous:      2,
		BetaRogue:         3,
		ConcurrentRepolls: 1,
	}
	sm.Initialize(ctx, params, GenesisID)

	block := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(1),
		status: choices.Processing,
	}

	sm.Add(block)

	votes := ids.Bag{}
	votes.Add(block.id)

	sm.RecordPoll(votes)

	if pref := sm.Preference(); !pref.Equals(block.id) {
		t.Fatalf("Preference returned the wrong block")
	} else if sm.Finalized() {
		t.Fatalf("Snowman instance finalized too soon")
	} else if status := block.Status(); status != choices.Processing {
		t.Fatalf("Block's status changed unexpectedly")
	}

	sm.RecordPoll(votes)

	if pref := sm.Preference(); !pref.Equals(block.id) {
		t.Fatalf("Preference returned the wrong block")
	} else if !sm.Finalized() {
		t.Fatalf("Snowman instance didn't finalize")
	} else if status := block.Status(); status != choices.Accepted {
		t.Fatalf("Block's status should have been set to accepted")
	}
}

func RecordPollAcceptAndRejectTest(t *testing.T, factory Factory) {
	sm := factory.New()

	ctx := snow.DefaultContextTest()
	params := snowball.Parameters{
		Metrics:           prometheus.NewRegistry(),
		K:                 1,
		Alpha:             1,
		BetaVirtuous:      1,
		BetaRogue:         2,
		ConcurrentRepolls: 1,
	}
	sm.Initialize(ctx, params, GenesisID)

	firstBlock := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(1),
		status: choices.Processing,
	}
	secondBlock := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(2),
		status: choices.Processing,
	}

	sm.Add(firstBlock)
	sm.Add(secondBlock)

	votes := ids.Bag{}
	votes.Add(firstBlock.id)

	sm.RecordPoll(votes)

	if pref := sm.Preference(); !pref.Equals(firstBlock.id) {
		t.Fatalf("Preference returned the wrong block")
	} else if sm.Finalized() {
		t.Fatalf("Snowman instance finalized too soon")
	} else if status := firstBlock.Status(); status != choices.Processing {
		t.Fatalf("Block's status changed unexpectedly")
	} else if status := secondBlock.Status(); status != choices.Processing {
		t.Fatalf("Block's status changed unexpectedly")
	}

	sm.RecordPoll(votes)

	if pref := sm.Preference(); !pref.Equals(firstBlock.id) {
		t.Fatalf("Preference returned the wrong block")
	} else if !sm.Finalized() {
		t.Fatalf("Snowman instance didn't finalize")
	} else if status := firstBlock.Status(); status != choices.Accepted {
		t.Fatalf("Block's status should have been set to accepted")
	} else if status := secondBlock.Status(); status != choices.Rejected {
		t.Fatalf("Block's status should have been set to rejected")
	}
}

func RecordPollWhenFinalizedTest(t *testing.T, factory Factory) {
	sm := factory.New()

	ctx := snow.DefaultContextTest()
	params := snowball.Parameters{
		Metrics:           prometheus.NewRegistry(),
		K:                 1,
		Alpha:             1,
		BetaVirtuous:      1,
		BetaRogue:         2,
		ConcurrentRepolls: 1,
	}
	sm.Initialize(ctx, params, GenesisID)

	votes := ids.Bag{}
	votes.Add(GenesisID)
	sm.RecordPoll(votes)

	if !sm.Finalized() {
		t.Fatalf("Consensus should still be finalized")
	} else if pref := sm.Preference(); !GenesisID.Equals(pref) {
		t.Fatalf("Wrong preference listed")
	}
}

func RecordPollRejectTransitivelyTest(t *testing.T, factory Factory) {
	sm := factory.New()

	ctx := snow.DefaultContextTest()
	params := snowball.Parameters{
		Metrics:           prometheus.NewRegistry(),
		K:                 1,
		Alpha:             1,
		BetaVirtuous:      1,
		BetaRogue:         1,
		ConcurrentRepolls: 1,
	}
	sm.Initialize(ctx, params, GenesisID)

	block0 := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(1),
		status: choices.Processing,
	}
	block1 := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(2),
		status: choices.Processing,
	}
	block2 := &TestBlock{
		parent: block1,
		id:     ids.Empty.Prefix(3),
		status: choices.Processing,
	}

	sm.Add(block0)
	sm.Add(block1)
	sm.Add(block2)

	// Current graph structure:
	//   G
	//  / \
	// 0   1
	//     |
	//     2
	// Tail = 0

	votes := ids.Bag{}
	votes.Add(block0.id)
	sm.RecordPoll(votes)

	// Current graph structure:
	// 0
	// Tail = 0

	if !sm.Finalized() {
		t.Fatalf("Finalized too late")
	} else if pref := sm.Preference(); !block0.id.Equals(pref) {
		t.Fatalf("Wrong preference listed")
	}

	if status := block0.Status(); status != choices.Accepted {
		t.Fatalf("Wrong status returned")
	} else if status := block1.Status(); status != choices.Rejected {
		t.Fatalf("Wrong status returned")
	} else if status := block2.Status(); status != choices.Rejected {
		t.Fatalf("Wrong status returned")
	}
}

func RecordPollTransitivelyResetConfidenceTest(t *testing.T, factory Factory) {
	sm := factory.New()

	ctx := snow.DefaultContextTest()
	params := snowball.Parameters{
		Metrics:           prometheus.NewRegistry(),
		K:                 1,
		Alpha:             1,
		BetaVirtuous:      2,
		BetaRogue:         2,
		ConcurrentRepolls: 1,
	}
	sm.Initialize(ctx, params, GenesisID)

	block0 := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(1),
		status: choices.Processing,
	}
	block1 := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(2),
		status: choices.Processing,
	}
	block2 := &TestBlock{
		parent: block1,
		id:     ids.Empty.Prefix(3),
		status: choices.Processing,
	}
	block3 := &TestBlock{
		parent: block1,
		id:     ids.Empty.Prefix(4),
		status: choices.Processing,
	}

	sm.Add(block0)
	sm.Add(block1)
	sm.Add(block2)
	sm.Add(block3)

	// Current graph structure:
	//   G
	//  / \
	// 0   1
	//    / \
	//   2   3

	votesFor2 := ids.Bag{}
	votesFor2.Add(block2.id)
	sm.RecordPoll(votesFor2)

	if sm.Finalized() {
		t.Fatalf("Finalized too early")
	} else if pref := sm.Preference(); !block2.id.Equals(pref) {
		t.Fatalf("Wrong preference listed")
	}

	emptyVotes := ids.Bag{}
	sm.RecordPoll(emptyVotes)

	if sm.Finalized() {
		t.Fatalf("Finalized too early")
	} else if pref := sm.Preference(); !block2.id.Equals(pref) {
		t.Fatalf("Wrong preference listed")
	}

	sm.RecordPoll(votesFor2)

	if sm.Finalized() {
		t.Fatalf("Finalized too early")
	} else if pref := sm.Preference(); !block2.id.Equals(pref) {
		t.Fatalf("Wrong preference listed")
	}

	votesFor3 := ids.Bag{}
	votesFor3.Add(block3.id)
	sm.RecordPoll(votesFor3)

	if sm.Finalized() {
		t.Fatalf("Finalized too early")
	} else if pref := sm.Preference(); !block2.id.Equals(pref) {
		t.Fatalf("Wrong preference listed")
	}

	sm.RecordPoll(votesFor3)

	if !sm.Finalized() {
		t.Fatalf("Finalized too late")
	} else if pref := sm.Preference(); !block3.id.Equals(pref) {
		t.Fatalf("Wrong preference listed")
	}

	if status := block0.Status(); status != choices.Rejected {
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

	ctx := snow.DefaultContextTest()
	params := snowball.Parameters{
		Metrics:           prometheus.NewRegistry(),
		K:                 1,
		Alpha:             1,
		BetaVirtuous:      2,
		BetaRogue:         2,
		ConcurrentRepolls: 1,
	}
	sm.Initialize(ctx, params, GenesisID)

	block := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(1),
		status: choices.Processing,
	}
	unknownBlockID := ids.Empty.Prefix(2)

	sm.Add(block)

	validVotes := ids.Bag{}
	validVotes.Add(block.id)
	sm.RecordPoll(validVotes)

	invalidVotes := ids.Bag{}
	invalidVotes.Add(unknownBlockID)
	sm.RecordPoll(invalidVotes)

	sm.RecordPoll(validVotes)

	if sm.Finalized() {
		t.Fatalf("Finalized too early")
	} else if pref := sm.Preference(); !block.id.Equals(pref) {
		t.Fatalf("Wrong preference listed")
	}
}

func RecordPollTransitiveVotingTest(t *testing.T, factory Factory) {
	sm := factory.New()

	ctx := snow.DefaultContextTest()
	params := snowball.Parameters{
		Metrics:           prometheus.NewRegistry(),
		K:                 3,
		Alpha:             3,
		BetaVirtuous:      1,
		BetaRogue:         1,
		ConcurrentRepolls: 1,
	}
	sm.Initialize(ctx, params, GenesisID)

	block0 := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(1),
		status: choices.Processing,
	}
	block1 := &TestBlock{
		parent: block0,
		id:     ids.Empty.Prefix(2),
		status: choices.Processing,
	}
	block2 := &TestBlock{
		parent: block1,
		id:     ids.Empty.Prefix(3),
		status: choices.Processing,
	}
	block3 := &TestBlock{
		parent: block0,
		id:     ids.Empty.Prefix(4),
		status: choices.Processing,
	}
	block4 := &TestBlock{
		parent: block3,
		id:     ids.Empty.Prefix(5),
		status: choices.Processing,
	}

	sm.Add(block0)
	sm.Add(block1)
	sm.Add(block2)
	sm.Add(block3)
	sm.Add(block4)

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
	votes0_2_4.Add(block0.id)
	votes0_2_4.Add(block2.id)
	votes0_2_4.Add(block4.id)
	sm.RecordPoll(votes0_2_4)

	// Current graph structure:
	//   0
	//  / \
	// 1   3
	// |   |
	// 2   4
	// Tail = 2

	if pref := sm.Preference(); !block2.id.Equals(pref) {
		t.Fatalf("Wrong preference listed")
	} else if sm.Finalized() {
		t.Fatalf("Finalized too early")
	} else if block0.Status() != choices.Accepted {
		t.Fatalf("Should have accepted")
	} else if block1.Status() != choices.Processing {
		t.Fatalf("Should have accepted")
	} else if block2.Status() != choices.Processing {
		t.Fatalf("Should have accepted")
	} else if block3.Status() != choices.Processing {
		t.Fatalf("Should have rejected")
	} else if block4.Status() != choices.Processing {
		t.Fatalf("Should have rejected")
	}

	dep2_2_2 := ids.Bag{}
	dep2_2_2.AddCount(block2.id, 3)
	sm.RecordPoll(dep2_2_2)

	// Current graph structure:
	//   2
	// Tail = 2

	if pref := sm.Preference(); !block2.id.Equals(pref) {
		t.Fatalf("Wrong preference listed")
	} else if !sm.Finalized() {
		t.Fatalf("Finalized too late")
	} else if block0.Status() != choices.Accepted {
		t.Fatalf("Should have accepted")
	} else if block1.Status() != choices.Accepted {
		t.Fatalf("Should have accepted")
	} else if block2.Status() != choices.Accepted {
		t.Fatalf("Should have accepted")
	} else if block3.Status() != choices.Rejected {
		t.Fatalf("Should have rejected")
	} else if block4.Status() != choices.Rejected {
		t.Fatalf("Should have rejected")
	}
}

func RecordPollDivergedVotingTest(t *testing.T, factory Factory) {
	sm := factory.New()

	ctx := snow.DefaultContextTest()
	params := snowball.Parameters{
		Metrics:           prometheus.NewRegistry(),
		K:                 1,
		Alpha:             1,
		BetaVirtuous:      1,
		BetaRogue:         2,
		ConcurrentRepolls: 1,
	}
	sm.Initialize(ctx, params, GenesisID)

	block0 := &TestBlock{
		parent: Genesis,
		id:     ids.NewID([32]byte{0x0f}), // 0b1111
		status: choices.Processing,
	}
	block1 := &TestBlock{
		parent: Genesis,
		id:     ids.NewID([32]byte{0x08}), // 0b1000
		status: choices.Processing,
	}
	block2 := &TestBlock{
		parent: Genesis,
		id:     ids.NewID([32]byte{0x01}), // 0b0001
		status: choices.Processing,
	}
	block3 := &TestBlock{
		parent: block2,
		id:     ids.Empty.Prefix(1),
		status: choices.Processing,
	}

	sm.Add(block0)
	sm.Add(block1)

	votes0 := ids.Bag{}
	votes0.Add(block0.id)
	sm.RecordPoll(votes0)

	sm.Add(block2)

	// dep2 is already rejected.

	sm.Add(block3)

	if status := block0.Status(); status == choices.Accepted {
		t.Fatalf("Shouldn't be accepted yet")
	}

	// Transitively increases dep2. However, dep2 shares the first bit with
	// dep0. Because dep2 is already rejected, this will accept dep0.
	votes3 := ids.Bag{}
	votes3.Add(block3.id)
	sm.RecordPoll(votes3)

	if !sm.Finalized() {
		t.Fatalf("Finalized too late")
	} else if status := block0.Status(); status != choices.Accepted {
		t.Fatalf("Should be accepted")
	}
}

func MetricsProcessingErrorTest(t *testing.T, factory Factory) {
	sm := factory.New()

	ctx := snow.DefaultContextTest()
	params := snowball.Parameters{
		Metrics:           prometheus.NewRegistry(),
		K:                 1,
		Alpha:             1,
		BetaVirtuous:      1,
		BetaRogue:         1,
		ConcurrentRepolls: 1,
	}

	numProcessing := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: params.Namespace,
			Name:      "processing",
		})

	if err := params.Metrics.Register(numProcessing); err != nil {
		t.Fatal(err)
	}

	sm.Initialize(ctx, params, GenesisID)

	block := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(1),
		status: choices.Processing,
	}

	sm.Add(block)

	votes := ids.Bag{}
	votes.Add(block.id)

	sm.RecordPoll(votes)

	if !sm.Finalized() {
		t.Fatalf("Snowman instance didn't finalize")
	}
}

func MetricsAcceptedErrorTest(t *testing.T, factory Factory) {
	sm := factory.New()

	ctx := snow.DefaultContextTest()
	params := snowball.Parameters{
		Metrics:           prometheus.NewRegistry(),
		K:                 1,
		Alpha:             1,
		BetaVirtuous:      1,
		BetaRogue:         1,
		ConcurrentRepolls: 1,
	}

	numAccepted := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: params.Namespace,
			Name:      "accepted",
		})

	if err := params.Metrics.Register(numAccepted); err != nil {
		t.Fatal(err)
	}

	sm.Initialize(ctx, params, GenesisID)

	block := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(1),
		status: choices.Processing,
	}

	sm.Add(block)

	votes := ids.Bag{}
	votes.Add(block.id)

	sm.RecordPoll(votes)

	if !sm.Finalized() {
		t.Fatalf("Snowman instance didn't finalize")
	}
}

func MetricsRejectedErrorTest(t *testing.T, factory Factory) {
	sm := factory.New()

	ctx := snow.DefaultContextTest()
	params := snowball.Parameters{
		Metrics:           prometheus.NewRegistry(),
		K:                 1,
		Alpha:             1,
		BetaVirtuous:      1,
		BetaRogue:         1,
		ConcurrentRepolls: 1,
	}

	numRejected := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: params.Namespace,
			Name:      "rejected",
		})

	if err := params.Metrics.Register(numRejected); err != nil {
		t.Fatal(err)
	}

	sm.Initialize(ctx, params, GenesisID)

	block := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(1),
		status: choices.Processing,
	}

	sm.Add(block)

	votes := ids.Bag{}
	votes.Add(block.id)

	sm.RecordPoll(votes)

	if !sm.Finalized() {
		t.Fatalf("Snowman instance didn't finalize")
	}
}

func RandomizedConsistencyTest(t *testing.T, factory Factory) {
	numColors := 50
	numNodes := 100
	params := snowball.Parameters{
		Metrics:      prometheus.NewRegistry(),
		K:            20,
		Alpha:        15,
		BetaVirtuous: 20,
		BetaRogue:    30,
	}
	seed := int64(0)

	rand.Seed(seed)

	n := Network{}
	n.Initialize(params, numColors)

	for i := 0; i < numNodes; i++ {
		n.AddNode(factory.New())
	}

	for !n.Finalized() {
		n.Round()
	}

	if !n.Agreement() {
		t.Fatalf("Network agreed on inconsistent values")
	}
}
