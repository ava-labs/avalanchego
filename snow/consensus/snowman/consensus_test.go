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
	Genesis = &Blk{
		id:     ids.Empty.Prefix(0),
		status: choices.Accepted,
	}
)

func ParamsTest(t *testing.T, factory Factory) {
	sm := factory.New()

	ctx := snow.DefaultContextTest()
	params := snowball.Parameters{
		Namespace: fmt.Sprintf("gecko_%s", ctx.ChainID),
		Metrics:   prometheus.NewRegistry(),
		K:         1, Alpha: 1, BetaVirtuous: 3, BetaRogue: 5,
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

	params.Metrics.Register(numProcessing)
	params.Metrics.Register(numAccepted)
	params.Metrics.Register(numRejected)

	sm.Initialize(ctx, params, Genesis.ID())

	if p := sm.Parameters(); p.K != params.K {
		t.Fatalf("Wrong K parameter")
	} else if p.Alpha != params.Alpha {
		t.Fatalf("Wrong Alpha parameter")
	} else if p.BetaVirtuous != params.BetaVirtuous {
		t.Fatalf("Wrong Beta1 parameter")
	} else if p.BetaRogue != params.BetaRogue {
		t.Fatalf("Wrong Beta2 parameter")
	}
}

func AddTest(t *testing.T, factory Factory) {
	sm := factory.New()

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       1, Alpha: 1, BetaVirtuous: 3, BetaRogue: 5,
	}
	sm.Initialize(snow.DefaultContextTest(), params, Genesis.ID())

	if pref := sm.Preference(); !pref.Equals(Genesis.ID()) {
		t.Fatalf("Wrong preference. Expected %s, got %s", Genesis.ID(), pref)
	}

	dep0 := &Blk{
		parent: Genesis,
		id:     ids.Empty.Prefix(1),
	}
	sm.Add(dep0)
	if pref := sm.Preference(); !pref.Equals(dep0.id) {
		t.Fatalf("Wrong preference. Expected %s, got %s", dep0.id, pref)
	}

	dep1 := &Blk{
		parent: Genesis,
		id:     ids.Empty.Prefix(2),
	}
	sm.Add(dep1)
	if pref := sm.Preference(); !pref.Equals(dep0.id) {
		t.Fatalf("Wrong preference. Expected %s, got %s", dep0.id, pref)
	}

	dep2 := &Blk{
		parent: dep0,
		id:     ids.Empty.Prefix(3),
	}
	sm.Add(dep2)
	if pref := sm.Preference(); !pref.Equals(dep2.id) {
		t.Fatalf("Wrong preference. Expected %s, got %s", dep2.id, pref)
	}

	dep3 := &Blk{
		parent: &Blk{id: ids.Empty.Prefix(4)},
		id:     ids.Empty.Prefix(5),
	}
	sm.Add(dep3)
	if pref := sm.Preference(); !pref.Equals(dep2.id) {
		t.Fatalf("Wrong preference. Expected %s, got %s", dep2.id, pref)
	}
}

func CollectTest(t *testing.T, factory Factory) {
	sm := factory.New()

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       2, Alpha: 2, BetaVirtuous: 1, BetaRogue: 2,
	}
	sm.Initialize(snow.DefaultContextTest(), params, Genesis.ID())

	dep1 := &Blk{
		parent: Genesis,
		id:     ids.Empty.Prefix(2),
	}
	sm.Add(dep1)

	dep0 := &Blk{
		parent: Genesis,
		id:     ids.Empty.Prefix(1),
	}
	sm.Add(dep0)

	dep2 := &Blk{
		parent: dep0,
		id:     ids.Empty.Prefix(3),
	}
	sm.Add(dep2)

	dep3 := &Blk{
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

	dep1 := &Blk{
		parent: Genesis,
		id:     ids.Empty.Prefix(2),
	}
	sm.Add(dep1)

	dep0 := &Blk{
		parent: Genesis,
		id:     ids.Empty.Prefix(1),
	}
	sm.Add(dep0)

	dep2 := &Blk{
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

	dep1 := &Blk{
		parent: Genesis,
		id:     ids.Empty.Prefix(2),
		status: choices.Processing,
	}
	sm.Add(dep1)

	dep0 := &Blk{
		parent: Genesis,
		id:     ids.Empty.Prefix(1),
		status: choices.Processing,
	}
	sm.Add(dep0)

	dep2 := &Blk{
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

	dep0 := &Blk{
		parent: Genesis,
		id:     ids.Empty.Prefix(1),
	}
	sm.Add(dep0)

	dep1 := &Blk{
		parent: dep0,
		id:     ids.Empty.Prefix(2),
	}
	sm.Add(dep1)

	dep2 := &Blk{
		parent: dep1,
		id:     ids.Empty.Prefix(3),
	}
	sm.Add(dep2)

	dep3 := &Blk{
		parent: dep0,
		id:     ids.Empty.Prefix(4),
	}
	sm.Add(dep3)

	dep4 := &Blk{
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

	dep0 := &Blk{
		parent: Genesis,
		id:     ids.NewID([32]byte{0x0f}), // 0b1111
	}
	sm.Add(dep0)

	dep1 := &Blk{
		parent: Genesis,
		id:     ids.NewID([32]byte{0x08}), // 0b1000
	}
	sm.Add(dep1)

	dep0_1 := ids.Bag{}
	dep0_1.AddCount(dep0.id, 1)
	sm.RecordPoll(dep0_1)

	dep2 := &Blk{
		parent: Genesis,
		id:     ids.NewID([32]byte{0x01}), // 0b0001
	}
	sm.Add(dep2)

	// dep2 is already rejected.

	dep3 := &Blk{
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

func IssuedTest(t *testing.T, factory Factory) {
	sm := factory.New()

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}

	sm.Initialize(snow.DefaultContextTest(), params, Genesis.ID())

	dep0 := &Blk{
		parent: Genesis,
		id:     ids.NewID([32]byte{0}),
		status: choices.Processing,
	}

	if sm.Issued(dep0) {
		t.Fatalf("Hasn't been issued yet")
	}

	sm.Add(dep0)

	if !sm.Issued(dep0) {
		t.Fatalf("Has been issued")
	}

	dep1 := &Blk{
		parent: Genesis,
		id:     ids.NewID([32]byte{0x1}), // 0b0001
		status: choices.Accepted,
	}

	if !sm.Issued(dep1) {
		t.Fatalf("Has accepted status")
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
