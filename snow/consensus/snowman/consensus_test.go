// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"fmt"
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

func CollectTest(t *testing.T, factory Factory) {
	sm := factory.New()

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       2, Alpha: 2, BetaVirtuous: 1, BetaRogue: 2,
	}
	sm.Initialize(snow.DefaultContextTest(), params, Genesis.ID())

	dep1 := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(2),
	}
	sm.Add(dep1)

	dep0 := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(1),
	}
	sm.Add(dep0)

	dep2 := &TestBlock{
		parent: dep0,
		id:     ids.Empty.Prefix(3),
	}
	sm.Add(dep2)

	dep3 := &TestBlock{
		parent: dep0,
		id:     ids.Empty.Prefix(4),
	}
	sm.Add(dep3)

	// Current graph structure:
	//       G
	//      / \
	//     0   1
	//    / \
	//   2   3
	// Tail = 1

	dep2_2 := ids.Bag{}
	dep2_2.AddCount(dep2.id, 2)
	sm.RecordPoll(dep2_2)

	// Current graph structure:
	//       G
	//      / \
	//     0   1
	//    / \
	//   2   3
	// Tail = 2

	if sm.Finalized() {
		t.Fatalf("Finalized too early")
	} else if !dep2.id.Equals(sm.Preference()) {
		t.Fatalf("Wrong preference listed")
	}

	dep3_2 := ids.Bag{}
	dep3_2.AddCount(dep3.id, 2)
	sm.RecordPoll(dep3_2)

	// Current graph structure:
	//     0
	//    / \
	//   2   3
	// Tail = 2

	if sm.Finalized() {
		t.Fatalf("Finalized too early")
	} else if !dep2.id.Equals(sm.Preference()) {
		t.Fatalf("Wrong preference listed")
	}

	sm.RecordPoll(dep2_2)

	// Current graph structure:
	//     0
	//    / \
	//   2   3
	// Tail = 2

	if sm.Finalized() {
		t.Fatalf("Finalized too early")
	} else if !dep2.id.Equals(sm.Preference()) {
		t.Fatalf("Wrong preference listed")
	}

	sm.RecordPoll(dep2_2)

	// Current graph structure:
	//   2
	// Tail = 2

	if !sm.Finalized() {
		t.Fatalf("Finalized too late")
	} else if !dep2.id.Equals(sm.Preference()) {
		t.Fatalf("Wrong preference listed")
	}

	if dep0.Status() != choices.Accepted {
		t.Fatalf("Should have accepted")
	} else if dep1.Status() != choices.Rejected {
		t.Fatalf("Should have rejected")
	} else if dep2.Status() != choices.Accepted {
		t.Fatalf("Should have accepted")
	} else if dep3.Status() != choices.Rejected {
		t.Fatalf("Should have rejected")
	}
}

func CollectNothingTest(t *testing.T, factory Factory) {
	sm := factory.New()

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}
	sm.Initialize(snow.DefaultContextTest(), params, Genesis.ID())

	// Current graph structure:
	//       G
	// Tail = G

	genesis1 := ids.Bag{}
	genesis1.AddCount(Genesis.ID(), 1)
	sm.RecordPoll(genesis1)

	// Current graph structure:
	//       G
	// Tail = G

	if !sm.Finalized() {
		t.Fatalf("Finalized too late")
	} else if !Genesis.ID().Equals(sm.Preference()) {
		t.Fatalf("Wrong preference listed")
	}
}

func CollectTransRejectTest(t *testing.T, factory Factory) {
	sm := factory.New()

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}
	sm.Initialize(snow.DefaultContextTest(), params, Genesis.ID())

	dep1 := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(2),
	}
	sm.Add(dep1)

	dep0 := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(1),
	}
	sm.Add(dep0)

	dep2 := &TestBlock{
		parent: dep0,
		id:     ids.Empty.Prefix(3),
	}
	sm.Add(dep2)

	// Current graph structure:
	//       G
	//      / \
	//     0   1
	//    /
	//   2
	// Tail = 1

	dep1_1 := ids.Bag{}
	dep1_1.AddCount(dep1.id, 1)
	sm.RecordPoll(dep1_1)
	sm.RecordPoll(dep1_1)

	// Current graph structure:
	//       1
	// Tail = 1

	if !sm.Finalized() {
		t.Fatalf("Finalized too late")
	} else if !dep1.id.Equals(sm.Preference()) {
		t.Fatalf("Wrong preference listed")
	}

	if dep0.Status() != choices.Rejected {
		t.Fatalf("Should have rejected")
	} else if dep1.Status() != choices.Accepted {
		t.Fatalf("Should have accepted")
	} else if dep2.Status() != choices.Rejected {
		t.Fatalf("Should have rejected")
	}
}

func CollectTransResetTest(t *testing.T, factory Factory) {
	sm := factory.New()

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}
	sm.Initialize(snow.DefaultContextTest(), params, Genesis.ID())

	dep1 := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(2),
		status: choices.Processing,
	}
	sm.Add(dep1)

	dep0 := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(1),
		status: choices.Processing,
	}
	sm.Add(dep0)

	dep2 := &TestBlock{
		parent: dep0,
		id:     ids.Empty.Prefix(3),
		status: choices.Processing,
	}
	sm.Add(dep2)

	// Current graph structure:
	//       G
	//      / \
	//     0   1
	//    /
	//   2
	// Tail = 1

	dep1_1 := ids.Bag{}
	dep1_1.AddCount(dep1.id, 1)
	sm.RecordPoll(dep1_1)

	// Current graph structure:
	//       G
	//      / \
	//     0   1
	//    /
	//   2
	// Tail = 1

	dep2_1 := ids.Bag{}
	dep2_1.AddCount(dep2.id, 1)
	sm.RecordPoll(dep2_1)

	if sm.Finalized() {
		t.Fatalf("Finalized too early")
	} else if status := dep0.Status(); status != choices.Processing {
		t.Fatalf("Shouldn't have accepted yet %s", status)
	}

	if !dep1.id.Equals(sm.Preference()) {
		t.Fatalf("Wrong preference listed")
	}

	sm.RecordPoll(dep2_1)
	sm.RecordPoll(dep2_1)

	if !sm.Finalized() {
		t.Fatalf("Finalized too late")
	} else if dep0.Status() != choices.Accepted {
		t.Fatalf("Should have accepted")
	} else if dep1.Status() != choices.Rejected {
		t.Fatalf("Should have rejected")
	} else if dep2.Status() != choices.Accepted {
		t.Fatalf("Should have accepted")
	}
}

func CollectTransVoteTest(t *testing.T, factory Factory) {
	sm := factory.New()

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       3, Alpha: 3, BetaVirtuous: 1, BetaRogue: 1,
	}
	sm.Initialize(snow.DefaultContextTest(), params, Genesis.ID())

	dep0 := &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(1),
	}
	sm.Add(dep0)

	dep1 := &TestBlock{
		parent: dep0,
		id:     ids.Empty.Prefix(2),
	}
	sm.Add(dep1)

	dep2 := &TestBlock{
		parent: dep1,
		id:     ids.Empty.Prefix(3),
	}
	sm.Add(dep2)

	dep3 := &TestBlock{
		parent: dep0,
		id:     ids.Empty.Prefix(4),
	}
	sm.Add(dep3)

	dep4 := &TestBlock{
		parent: dep3,
		id:     ids.Empty.Prefix(5),
	}
	sm.Add(dep4)

	// Current graph structure:
	//       G
	//      /
	//     0
	//    / \
	//   1   3
	//  /     \
	// 2       4
	// Tail = 2

	dep0_2_4_1 := ids.Bag{}
	dep0_2_4_1.AddCount(dep0.id, 1)
	dep0_2_4_1.AddCount(dep2.id, 1)
	dep0_2_4_1.AddCount(dep4.id, 1)
	sm.RecordPoll(dep0_2_4_1)

	// Current graph structure:
	//     0
	//    / \
	//   1   3
	//  /     \
	// 2       4
	// Tail = 2

	if !dep2.id.Equals(sm.Preference()) {
		t.Fatalf("Wrong preference listed")
	}

	dep2_3 := ids.Bag{}
	dep2_3.AddCount(dep2.id, 3)
	sm.RecordPoll(dep2_3)

	// Current graph structure:
	//   2
	// Tail = 2

	if !dep2.id.Equals(sm.Preference()) {
		t.Fatalf("Wrong preference listed")
	}

	if !sm.Finalized() {
		t.Fatalf("Finalized too late")
	} else if dep0.Status() != choices.Accepted {
		t.Fatalf("Should have accepted")
	} else if dep1.Status() != choices.Accepted {
		t.Fatalf("Should have accepted")
	} else if dep2.Status() != choices.Accepted {
		t.Fatalf("Should have accepted")
	} else if dep3.Status() != choices.Rejected {
		t.Fatalf("Should have rejected")
	} else if dep4.Status() != choices.Rejected {
		t.Fatalf("Should have rejected")
	}
}

func DivergedVotingTest(t *testing.T, factory Factory) {
	sm := factory.New()

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}
	sm.Initialize(snow.DefaultContextTest(), params, Genesis.ID())

	dep0 := &TestBlock{
		parent: Genesis,
		id:     ids.NewID([32]byte{0x0f}), // 0b1111
	}
	sm.Add(dep0)

	dep1 := &TestBlock{
		parent: Genesis,
		id:     ids.NewID([32]byte{0x08}), // 0b1000
	}
	sm.Add(dep1)

	dep0_1 := ids.Bag{}
	dep0_1.AddCount(dep0.id, 1)
	sm.RecordPoll(dep0_1)

	dep2 := &TestBlock{
		parent: Genesis,
		id:     ids.NewID([32]byte{0x01}), // 0b0001
	}
	sm.Add(dep2)

	// dep2 is already rejected.

	dep3 := &TestBlock{
		parent: dep2,
		id:     ids.Empty.Prefix(3),
	}
	sm.Add(dep3)

	if dep0.Status() == choices.Accepted {
		t.Fatalf("Shouldn't be accepted yet")
	}

	// Transitively increases dep2. However, dep2 shares the first bit with
	// dep0. Because dep2 is already rejected, this will accept dep0.
	dep3_1 := ids.Bag{}
	dep3_1.AddCount(dep3.id, 1)
	sm.RecordPoll(dep3_1)

	if !sm.Finalized() {
		t.Fatalf("Finalized too late")
	} else if dep0.Status() != choices.Accepted {
		t.Fatalf("Should be accepted")
	}
}

func MetricsErrorTest(t *testing.T, factory Factory) {
	sm := factory.New()

	ctx := snow.DefaultContextTest()
	params := snowball.Parameters{
		Namespace: fmt.Sprintf("gecko_%s", ctx.ChainID),
		Metrics:   prometheus.NewRegistry(),
		K:         1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}

	numProcessing := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: params.Namespace,
			Name:      "processing",
		})
	numAccepted := prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: params.Namespace,
			Name:      "accepted",
		})
	numRejected := prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: params.Namespace,
			Name:      "rejected",
		})

	if err := params.Metrics.Register(numProcessing); err != nil {
		t.Fatal(err)
	}
	if err := params.Metrics.Register(numAccepted); err != nil {
		t.Fatal(err)
	}
	if err := params.Metrics.Register(numRejected); err != nil {
		t.Fatal(err)
	}

	sm.Initialize(ctx, params, Genesis.ID())
}

func ConsistentTest(t *testing.T, factory Factory) {
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
