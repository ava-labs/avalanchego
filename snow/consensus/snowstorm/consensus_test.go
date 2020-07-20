// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowball"
)

var (
	Tests = []func(*testing.T, Factory){
		MetricsTest,
		ParamsTest,
		IssuedTest,
		LeftoverInputTest,
		LowerConfidenceTest,
		MiddleConfidenceTest,
		IndependentTest,
		VirtuousTest,
		IsVirtuousTest,
		QuiesceTest,
		AcceptingDependencyTest,
		RejectingDependencyTest,
		VacuouslyAcceptedTest,
		ConflictsTest,
		VirtuousDependsOnRogueTest,
		ErrorOnVacuouslyAcceptedTest,
		ErrorOnAcceptedTest,
		ErrorOnRejectingLowerConfidenceConflictTest,
		ErrorOnRejectingHigherConfidenceConflictTest,
	}

	Red, Green, Blue, Alpha *TestTx
)

//  R - G - B - A
func Setup() {
	Red = &TestTx{}
	Green = &TestTx{}
	Blue = &TestTx{}
	Alpha = &TestTx{}

	for i, color := range []*TestTx{Red, Green, Blue, Alpha} {
		color.IDV = ids.Empty.Prefix(uint64(i))
		color.AcceptV = nil
		color.RejectV = nil
		color.StatusV = choices.Processing

		color.DependenciesV = nil
		color.InputIDsV.Clear()
		color.VerifyV = nil
		color.BytesV = []byte{byte(i)}
	}

	X := ids.Empty.Prefix(4)
	Y := ids.Empty.Prefix(5)
	Z := ids.Empty.Prefix(6)

	Red.InputIDsV.Add(X)

	Green.InputIDsV.Add(X)
	Green.InputIDsV.Add(Y)

	Blue.InputIDsV.Add(Y)
	Blue.InputIDsV.Add(Z)

	Alpha.InputIDsV.Add(Z)
}

// Execute all tests against a consensus implementation
func ConsensusTest(t *testing.T, factory Factory, prefix string) {
	for _, test := range Tests {
		test(t, factory)
	}
	StringTest(t, factory, prefix)
}

func MetricsTest(t *testing.T, factory Factory) {
	Setup()

	{
		params := snowball.Parameters{
			Metrics: prometheus.NewRegistry(),
			K:       2, Alpha: 2, BetaVirtuous: 1, BetaRogue: 2,
		}
		params.Metrics.Register(prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tx_processing",
		}))
		graph := factory.New()
		graph.Initialize(snow.DefaultContextTest(), params)
	}
	{
		params := snowball.Parameters{
			Metrics: prometheus.NewRegistry(),
			K:       2, Alpha: 2, BetaVirtuous: 1, BetaRogue: 2,
		}
		params.Metrics.Register(prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tx_accepted",
		}))
		graph := factory.New()
		graph.Initialize(snow.DefaultContextTest(), params)
	}
	{
		params := snowball.Parameters{
			Metrics: prometheus.NewRegistry(),
			K:       2, Alpha: 2, BetaVirtuous: 1, BetaRogue: 2,
		}
		params.Metrics.Register(prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tx_rejected",
		}))
		graph := factory.New()
		graph.Initialize(snow.DefaultContextTest(), params)
	}
}

func ParamsTest(t *testing.T, factory Factory) {
	Setup()

	graph := factory.New()

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       2, Alpha: 2, BetaVirtuous: 1, BetaRogue: 2,
	}
	graph.Initialize(snow.DefaultContextTest(), params)

	if p := graph.Parameters(); p.K != params.K {
		t.Fatalf("Wrong K parameter")
	} else if p := graph.Parameters(); p.Alpha != params.Alpha {
		t.Fatalf("Wrong Alpha parameter")
	} else if p := graph.Parameters(); p.BetaVirtuous != params.BetaVirtuous {
		t.Fatalf("Wrong Beta1 parameter")
	} else if p := graph.Parameters(); p.BetaRogue != params.BetaRogue {
		t.Fatalf("Wrong Beta2 parameter")
	}
}

func IssuedTest(t *testing.T, factory Factory) {
	Setup()

	graph := factory.New()

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       2, Alpha: 2, BetaVirtuous: 1, BetaRogue: 1,
	}
	graph.Initialize(snow.DefaultContextTest(), params)

	if issued := graph.Issued(Red); issued {
		t.Fatalf("Haven't issued anything yet.")
	} else if err := graph.Add(Red); err != nil {
		t.Fatal(err)
	} else if issued := graph.Issued(Red); !issued {
		t.Fatalf("Have already issued.")
	}

	_ = Blue.Accept()

	if issued := graph.Issued(Blue); !issued {
		t.Fatalf("Have already accepted.")
	}
}

func LeftoverInputTest(t *testing.T, factory Factory) {
	Setup()

	graph := factory.New()

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       2, Alpha: 2, BetaVirtuous: 1, BetaRogue: 1,
	}
	graph.Initialize(snow.DefaultContextTest(), params)

	if err := graph.Add(Red); err != nil {
		t.Fatal(err)
	} else if err := graph.Add(Green); err != nil {
		t.Fatal(err)
	} else if prefs := graph.Preferences(); prefs.Len() != 1 {
		t.Fatalf("Wrong number of preferences.")
	} else if !prefs.Contains(Red.ID()) {
		t.Fatalf("Wrong preference. Expected %s got %s", Red.ID(), prefs.List()[0])
	} else if graph.Finalized() {
		t.Fatalf("Finalized too early")
	}

	r := ids.Bag{}
	r.SetThreshold(2)
	r.AddCount(Red.ID(), 2)
	if _, err := graph.RecordPoll(r); err != nil {
		t.Fatal(err)
	} else if prefs := graph.Preferences(); prefs.Len() != 0 {
		t.Fatalf("Wrong number of preferences.")
	} else if !graph.Finalized() {
		t.Fatalf("Finalized too late")
	} else if Red.Status() != choices.Accepted {
		t.Fatalf("%s should have been accepted", Red.ID())
	} else if Green.Status() != choices.Rejected {
		t.Fatalf("%s should have been rejected", Green.ID())
	}
}

func LowerConfidenceTest(t *testing.T, factory Factory) {
	Setup()

	graph := factory.New()

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       2, Alpha: 2, BetaVirtuous: 1, BetaRogue: 1,
	}
	graph.Initialize(snow.DefaultContextTest(), params)

	if err := graph.Add(Red); err != nil {
		t.Fatal(err)
	} else if err := graph.Add(Green); err != nil {
		t.Fatal(err)
	} else if err := graph.Add(Blue); err != nil {
		t.Fatal(err)
	} else if prefs := graph.Preferences(); prefs.Len() != 1 {
		t.Fatalf("Wrong number of preferences.")
	} else if !prefs.Contains(Red.ID()) {
		t.Fatalf("Wrong preference. Expected %s got %s", Red.ID(), prefs.List()[0])
	} else if graph.Finalized() {
		t.Fatalf("Finalized too early")
	}

	r := ids.Bag{}
	r.SetThreshold(2)
	r.AddCount(Red.ID(), 2)
	if _, err := graph.RecordPoll(r); err != nil {
		t.Fatal(err)
	} else if prefs := graph.Preferences(); prefs.Len() != 1 {
		t.Fatalf("Wrong number of preferences.")
	} else if !prefs.Contains(Blue.ID()) {
		t.Fatalf("Wrong preference. Expected %s", Blue.ID())
	} else if graph.Finalized() {
		t.Fatalf("Finalized too early")
	}
}

func MiddleConfidenceTest(t *testing.T, factory Factory) {
	Setup()

	graph := factory.New()

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       2, Alpha: 2, BetaVirtuous: 1, BetaRogue: 1,
	}
	graph.Initialize(snow.DefaultContextTest(), params)

	if err := graph.Add(Red); err != nil {
		t.Fatal(err)
	} else if err := graph.Add(Green); err != nil {
		t.Fatal(err)
	} else if err := graph.Add(Alpha); err != nil {
		t.Fatal(err)
	} else if err := graph.Add(Blue); err != nil {
		t.Fatal(err)
	} else if prefs := graph.Preferences(); prefs.Len() != 2 {
		t.Fatalf("Wrong number of preferences.")
	} else if !prefs.Contains(Red.ID()) {
		t.Fatalf("Wrong preference. Expected %s", Red.ID())
	} else if !prefs.Contains(Alpha.ID()) {
		t.Fatalf("Wrong preference. Expected %s", Alpha.ID())
	} else if graph.Finalized() {
		t.Fatalf("Finalized too early")
	}

	r := ids.Bag{}
	r.SetThreshold(2)
	r.AddCount(Red.ID(), 2)
	if _, err := graph.RecordPoll(r); err != nil {
		t.Fatal(err)
	} else if prefs := graph.Preferences(); prefs.Len() != 1 {
		t.Fatalf("Wrong number of preferences.")
	} else if !prefs.Contains(Alpha.ID()) {
		t.Fatalf("Wrong preference. Expected %s", Alpha.ID())
	} else if graph.Finalized() {
		t.Fatalf("Finalized too early")
	}
}

func IndependentTest(t *testing.T, factory Factory) {
	Setup()

	graph := factory.New()

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       2, Alpha: 2, BetaVirtuous: 2, BetaRogue: 2,
	}
	graph.Initialize(snow.DefaultContextTest(), params)

	if err := graph.Add(Red); err != nil {
		t.Fatal(err)
	} else if err := graph.Add(Alpha); err != nil {
		t.Fatal(err)
	} else if prefs := graph.Preferences(); prefs.Len() != 2 {
		t.Fatalf("Wrong number of preferences.")
	} else if !prefs.Contains(Red.ID()) {
		t.Fatalf("Wrong preference. Expected %s", Red.ID())
	} else if !prefs.Contains(Alpha.ID()) {
		t.Fatalf("Wrong preference. Expected %s", Alpha.ID())
	} else if graph.Finalized() {
		t.Fatalf("Finalized too early")
	}

	ra := ids.Bag{}
	ra.SetThreshold(2)
	ra.AddCount(Red.ID(), 2)
	ra.AddCount(Alpha.ID(), 2)
	if _, err := graph.RecordPoll(ra); err != nil {
		t.Fatal(err)
	} else if prefs := graph.Preferences(); prefs.Len() != 2 {
		t.Fatalf("Wrong number of preferences.")
	} else if !prefs.Contains(Red.ID()) {
		t.Fatalf("Wrong preference. Expected %s", Red.ID())
	} else if !prefs.Contains(Alpha.ID()) {
		t.Fatalf("Wrong preference. Expected %s", Alpha.ID())
	} else if graph.Finalized() {
		t.Fatalf("Finalized too early")
	} else if _, err := graph.RecordPoll(ra); err != nil {
		t.Fatal(err)
	} else if prefs := graph.Preferences(); prefs.Len() != 0 {
		t.Fatalf("Wrong number of preferences.")
	} else if !graph.Finalized() {
		t.Fatalf("Finalized too late")
	}
}

func VirtuousTest(t *testing.T, factory Factory) {
	Setup()

	graph := factory.New()

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       2, Alpha: 2, BetaVirtuous: 1, BetaRogue: 1,
	}
	graph.Initialize(snow.DefaultContextTest(), params)

	if err := graph.Add(Red); err != nil {
		t.Fatal(err)
	} else if virtuous := graph.Virtuous(); virtuous.Len() != 1 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(Red.ID()) {
		t.Fatalf("Wrong virtuous. Expected %s", Red.ID())
	} else if err := graph.Add(Alpha); err != nil {
		t.Fatal(err)
	} else if virtuous := graph.Virtuous(); virtuous.Len() != 2 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(Red.ID()) {
		t.Fatalf("Wrong virtuous. Expected %s", Red.ID())
	} else if !virtuous.Contains(Alpha.ID()) {
		t.Fatalf("Wrong virtuous. Expected %s", Alpha.ID())
	} else if err := graph.Add(Green); err != nil {
		t.Fatal(err)
	} else if virtuous := graph.Virtuous(); virtuous.Len() != 1 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(Alpha.ID()) {
		t.Fatalf("Wrong virtuous. Expected %s", Alpha.ID())
	} else if err := graph.Add(Blue); err != nil {
		t.Fatal(err)
	} else if virtuous := graph.Virtuous(); virtuous.Len() != 0 {
		t.Fatalf("Wrong number of virtuous.")
	}
}

func IsVirtuousTest(t *testing.T, factory Factory) {
	Setup()

	graph := factory.New()

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       2, Alpha: 2, BetaVirtuous: 1, BetaRogue: 1,
	}
	graph.Initialize(snow.DefaultContextTest(), params)

	if !graph.IsVirtuous(Red) {
		t.Fatalf("Should be virtuous")
	} else if !graph.IsVirtuous(Green) {
		t.Fatalf("Should be virtuous")
	} else if !graph.IsVirtuous(Blue) {
		t.Fatalf("Should be virtuous")
	} else if !graph.IsVirtuous(Alpha) {
		t.Fatalf("Should be virtuous")
	} else if err := graph.Add(Red); err != nil {
		t.Fatal(err)
	} else if !graph.IsVirtuous(Red) {
		t.Fatalf("Should be virtuous")
	} else if graph.IsVirtuous(Green) {
		t.Fatalf("Should not be virtuous")
	} else if !graph.IsVirtuous(Blue) {
		t.Fatalf("Should be virtuous")
	} else if !graph.IsVirtuous(Alpha) {
		t.Fatalf("Should be virtuous")
	} else if err := graph.Add(Green); err != nil {
		t.Fatal(err)
	} else if graph.IsVirtuous(Red) {
		t.Fatalf("Should not be virtuous")
	} else if graph.IsVirtuous(Green) {
		t.Fatalf("Should not be virtuous")
	} else if graph.IsVirtuous(Blue) {
		t.Fatalf("Should not be virtuous")
	}
}

func QuiesceTest(t *testing.T, factory Factory) {
	Setup()

	graph := factory.New()

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       2, Alpha: 2, BetaVirtuous: 1, BetaRogue: 1,
	}
	graph.Initialize(snow.DefaultContextTest(), params)

	if !graph.Quiesce() {
		t.Fatalf("Should quiesce")
	} else if err := graph.Add(Red); err != nil {
		t.Fatal(err)
	} else if graph.Quiesce() {
		t.Fatalf("Shouldn't quiesce")
	} else if err := graph.Add(Green); err != nil {
		t.Fatal(err)
	} else if !graph.Quiesce() {
		t.Fatalf("Should quiesce")
	}
}

func AcceptingDependencyTest(t *testing.T, factory Factory) {
	Setup()

	graph := factory.New()

	purple := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(7),
			StatusV: choices.Processing,
		},
		DependenciesV: []Tx{Red},
	}
	purple.InputIDsV.Add(ids.Empty.Prefix(8))

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}
	graph.Initialize(snow.DefaultContextTest(), params)

	if err := graph.Add(Red); err != nil {
		t.Fatal(err)
	} else if err := graph.Add(Green); err != nil {
		t.Fatal(err)
	} else if err := graph.Add(purple); err != nil {
		t.Fatal(err)
	} else if prefs := graph.Preferences(); prefs.Len() != 2 {
		t.Fatalf("Wrong number of preferences.")
	} else if !prefs.Contains(Red.ID()) {
		t.Fatalf("Wrong preference. Expected %s", Red.ID())
	} else if !prefs.Contains(purple.ID()) {
		t.Fatalf("Wrong preference. Expected %s", purple.ID())
	} else if Red.Status() != choices.Processing {
		t.Fatalf("Wrong status. %s should be %s", Red.ID(), choices.Processing)
	} else if Green.Status() != choices.Processing {
		t.Fatalf("Wrong status. %s should be %s", Green.ID(), choices.Processing)
	} else if purple.Status() != choices.Processing {
		t.Fatalf("Wrong status. %s should be %s", purple.ID(), choices.Processing)
	}

	g := ids.Bag{}
	g.Add(Green.ID())
	if _, err := graph.RecordPoll(g); err != nil {
		t.Fatal(err)
	} else if prefs := graph.Preferences(); prefs.Len() != 2 {
		t.Fatalf("Wrong number of preferences.")
	} else if !prefs.Contains(Green.ID()) {
		t.Fatalf("Wrong preference. Expected %s", Green.ID())
	} else if !prefs.Contains(purple.ID()) {
		t.Fatalf("Wrong preference. Expected %s", purple.ID())
	} else if Red.Status() != choices.Processing {
		t.Fatalf("Wrong status. %s should be %s", Red.ID(), choices.Processing)
	} else if Green.Status() != choices.Processing {
		t.Fatalf("Wrong status. %s should be %s", Green.ID(), choices.Processing)
	} else if purple.Status() != choices.Processing {
		t.Fatalf("Wrong status. %s should be %s", purple.ID(), choices.Processing)
	}

	rp := ids.Bag{}
	rp.Add(Red.ID(), purple.ID())
	if _, err := graph.RecordPoll(rp); err != nil {
		t.Fatal(err)
	} else if prefs := graph.Preferences(); prefs.Len() != 2 {
		t.Fatalf("Wrong number of preferences.")
	} else if !prefs.Contains(Green.ID()) {
		t.Fatalf("Wrong preference. Expected %s", Green.ID())
	} else if !prefs.Contains(purple.ID()) {
		t.Fatalf("Wrong preference. Expected %s", purple.ID())
	} else if Red.Status() != choices.Processing {
		t.Fatalf("Wrong status. %s should be %s", Red.ID(), choices.Processing)
	} else if Green.Status() != choices.Processing {
		t.Fatalf("Wrong status. %s should be %s", Green.ID(), choices.Processing)
	} else if purple.Status() != choices.Processing {
		t.Fatalf("Wrong status. %s should be %s", purple.ID(), choices.Processing)
	}

	r := ids.Bag{}
	r.Add(Red.ID())
	if _, err := graph.RecordPoll(r); err != nil {
		t.Fatal(err)
	} else if prefs := graph.Preferences(); prefs.Len() != 0 {
		t.Fatalf("Wrong number of preferences.")
	} else if Red.Status() != choices.Accepted {
		t.Fatalf("Wrong status. %s should be %s", Red.ID(), choices.Accepted)
	} else if Green.Status() != choices.Rejected {
		t.Fatalf("Wrong status. %s should be %s", Green.ID(), choices.Rejected)
	} else if purple.Status() != choices.Accepted {
		t.Fatalf("Wrong status. %s should be %s", purple.ID(), choices.Accepted)
	}
}

func RejectingDependencyTest(t *testing.T, factory Factory) {
	Setup()

	graph := factory.New()

	purple := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(7),
			StatusV: choices.Processing,
		},
		DependenciesV: []Tx{Red, Blue},
	}
	purple.InputIDsV.Add(ids.Empty.Prefix(8))

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}
	graph.Initialize(snow.DefaultContextTest(), params)

	if err := graph.Add(Red); err != nil {
		t.Fatal(err)
	} else if err := graph.Add(Green); err != nil {
		t.Fatal(err)
	} else if err := graph.Add(Blue); err != nil {
		t.Fatal(err)
	} else if err := graph.Add(purple); err != nil {
		t.Fatal(err)
	} else if prefs := graph.Preferences(); prefs.Len() != 2 {
		t.Fatalf("Wrong number of preferences.")
	} else if !prefs.Contains(Red.ID()) {
		t.Fatalf("Wrong preference. Expected %s", Red.ID())
	} else if !prefs.Contains(purple.ID()) {
		t.Fatalf("Wrong preference. Expected %s", purple.ID())
	} else if Red.Status() != choices.Processing {
		t.Fatalf("Wrong status. %s should be %s", Red.ID(), choices.Processing)
	} else if Green.Status() != choices.Processing {
		t.Fatalf("Wrong status. %s should be %s", Green.ID(), choices.Processing)
	} else if Blue.Status() != choices.Processing {
		t.Fatalf("Wrong status. %s should be %s", Blue.ID(), choices.Processing)
	} else if purple.Status() != choices.Processing {
		t.Fatalf("Wrong status. %s should be %s", purple.ID(), choices.Processing)
	}

	gp := ids.Bag{}
	gp.Add(Green.ID(), purple.ID())
	if _, err := graph.RecordPoll(gp); err != nil {
		t.Fatal(err)
	} else if prefs := graph.Preferences(); prefs.Len() != 2 {
		t.Fatalf("Wrong number of preferences.")
	} else if !prefs.Contains(Green.ID()) {
		t.Fatalf("Wrong preference. Expected %s", Green.ID())
	} else if !prefs.Contains(purple.ID()) {
		t.Fatalf("Wrong preference. Expected %s", purple.ID())
	} else if Red.Status() != choices.Processing {
		t.Fatalf("Wrong status. %s should be %s", Red.ID(), choices.Processing)
	} else if Green.Status() != choices.Processing {
		t.Fatalf("Wrong status. %s should be %s", Green.ID(), choices.Processing)
	} else if Blue.Status() != choices.Processing {
		t.Fatalf("Wrong status. %s should be %s", Blue.ID(), choices.Processing)
	} else if purple.Status() != choices.Processing {
		t.Fatalf("Wrong status. %s should be %s", purple.ID(), choices.Processing)
	} else if _, err := graph.RecordPoll(gp); err != nil {
		t.Fatal(err)
	} else if prefs := graph.Preferences(); prefs.Len() != 0 {
		t.Fatalf("Wrong number of preferences.")
	} else if Red.Status() != choices.Rejected {
		t.Fatalf("Wrong status. %s should be %s", Red.ID(), choices.Rejected)
	} else if Green.Status() != choices.Accepted {
		t.Fatalf("Wrong status. %s should be %s", Green.ID(), choices.Accepted)
	} else if Blue.Status() != choices.Rejected {
		t.Fatalf("Wrong status. %s should be %s", Blue.ID(), choices.Rejected)
	} else if purple.Status() != choices.Rejected {
		t.Fatalf("Wrong status. %s should be %s", purple.ID(), choices.Rejected)
	}
}

func VacuouslyAcceptedTest(t *testing.T, factory Factory) {
	Setup()

	graph := factory.New()

	purple := &TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.Empty.Prefix(7),
		StatusV: choices.Processing,
	}}

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}
	graph.Initialize(snow.DefaultContextTest(), params)

	if err := graph.Add(purple); err != nil {
		t.Fatal(err)
	} else if prefs := graph.Preferences(); prefs.Len() != 0 {
		t.Fatalf("Wrong number of preferences.")
	} else if status := purple.Status(); status != choices.Accepted {
		t.Fatalf("Wrong status. %s should be %s", purple.ID(), choices.Accepted)
	}
}

func ConflictsTest(t *testing.T, factory Factory) {
	Setup()

	graph := factory.New()

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}
	graph.Initialize(snow.DefaultContextTest(), params)

	conflictInputID := ids.Empty.Prefix(0)

	insPurple := ids.Set{}
	insPurple.Add(conflictInputID)

	purple := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(6),
			StatusV: choices.Processing,
		},
		InputIDsV: insPurple,
	}

	insOrange := ids.Set{}
	insOrange.Add(conflictInputID)

	orange := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(7),
			StatusV: choices.Processing,
		},
		InputIDsV: insOrange,
	}

	if err := graph.Add(purple); err != nil {
		t.Fatal(err)
	} else if orangeConflicts := graph.Conflicts(orange); orangeConflicts.Len() != 1 {
		t.Fatalf("Wrong number of conflicts")
	} else if !orangeConflicts.Contains(purple.IDV) {
		t.Fatalf("Conflicts does not contain the right transaction")
	} else if err := graph.Add(orange); err != nil {
		t.Fatal(err)
	} else if orangeConflicts := graph.Conflicts(orange); orangeConflicts.Len() != 1 {
		t.Fatalf("Wrong number of conflicts")
	} else if !orangeConflicts.Contains(purple.IDV) {
		t.Fatalf("Conflicts does not contain the right transaction")
	}
}

func VirtuousDependsOnRogueTest(t *testing.T, factory Factory) {
	Setup()

	graph := factory.New()

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}
	graph.Initialize(snow.DefaultContextTest(), params)

	rogue1 := &TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.Empty.Prefix(0),
		StatusV: choices.Processing,
	}}
	rogue2 := &TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.Empty.Prefix(1),
		StatusV: choices.Processing,
	}}
	virtuous := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2),
			StatusV: choices.Processing,
		},
		DependenciesV: []Tx{rogue1},
	}

	input1 := ids.Empty.Prefix(3)
	input2 := ids.Empty.Prefix(4)

	rogue1.InputIDsV.Add(input1)
	rogue2.InputIDsV.Add(input1)

	virtuous.InputIDsV.Add(input2)

	if err := graph.Add(rogue1); err != nil {
		t.Fatal(err)
	} else if err := graph.Add(rogue2); err != nil {
		t.Fatal(err)
	} else if err := graph.Add(virtuous); err != nil {
		t.Fatal(err)
	}

	votes := ids.Bag{}
	votes.Add(rogue1.ID())
	votes.Add(virtuous.ID())
	if _, err := graph.RecordPoll(votes); err != nil {
		t.Fatal(err)
	} else if status := rogue1.Status(); status != choices.Processing {
		t.Fatalf("Rogue Tx is %s expected %s", status, choices.Processing)
	} else if status := rogue2.Status(); status != choices.Processing {
		t.Fatalf("Rogue Tx is %s expected %s", status, choices.Processing)
	} else if status := virtuous.Status(); status != choices.Processing {
		t.Fatalf("Virtuous Tx is %s expected %s", status, choices.Processing)
	} else if !graph.Quiesce() {
		t.Fatalf("Should quiesce as there are no pending virtuous transactions")
	}
}

func ErrorOnVacuouslyAcceptedTest(t *testing.T, factory Factory) {
	Setup()

	graph := factory.New()

	purple := &TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.Empty.Prefix(7),
		AcceptV: errors.New(""),
		StatusV: choices.Processing,
	}}

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}
	graph.Initialize(snow.DefaultContextTest(), params)

	if err := graph.Add(purple); err == nil {
		t.Fatalf("Should have errored on acceptance")
	}
}

func ErrorOnAcceptedTest(t *testing.T, factory Factory) {
	Setup()

	graph := factory.New()

	purple := &TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.Empty.Prefix(7),
		AcceptV: errors.New(""),
		StatusV: choices.Processing,
	}}
	purple.InputIDsV.Add(ids.Empty.Prefix(4))

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}
	graph.Initialize(snow.DefaultContextTest(), params)

	if err := graph.Add(purple); err != nil {
		t.Fatal(err)
	}

	votes := ids.Bag{}
	votes.Add(purple.ID())
	if _, err := graph.RecordPoll(votes); err == nil {
		t.Fatalf("Should have errored on accepting an invalid tx")
	}
}

func ErrorOnRejectingLowerConfidenceConflictTest(t *testing.T, factory Factory) {
	Setup()

	graph := factory.New()

	X := ids.Empty.Prefix(4)

	purple := &TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.Empty.Prefix(7),
		StatusV: choices.Processing,
	}}
	purple.InputIDsV.Add(X)

	pink := &TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.Empty.Prefix(8),
		RejectV: errors.New(""),
		StatusV: choices.Processing,
	}}
	pink.InputIDsV.Add(X)

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 1,
	}
	graph.Initialize(snow.DefaultContextTest(), params)

	if err := graph.Add(purple); err != nil {
		t.Fatal(err)
	} else if err := graph.Add(pink); err != nil {
		t.Fatal(err)
	}

	votes := ids.Bag{}
	votes.Add(purple.ID())
	if _, err := graph.RecordPoll(votes); err == nil {
		t.Fatalf("Should have errored on rejecting an invalid tx")
	}
}

func ErrorOnRejectingHigherConfidenceConflictTest(t *testing.T, factory Factory) {
	Setup()

	graph := factory.New()

	X := ids.Empty.Prefix(4)

	purple := &TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.Empty.Prefix(7),
		StatusV: choices.Processing,
	}}
	purple.InputIDsV.Add(X)

	pink := &TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.Empty.Prefix(8),
		RejectV: errors.New(""),
		StatusV: choices.Processing,
	}}
	pink.InputIDsV.Add(X)

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 1,
	}
	graph.Initialize(snow.DefaultContextTest(), params)

	if err := graph.Add(pink); err != nil {
		t.Fatal(err)
	} else if err := graph.Add(purple); err != nil {
		t.Fatal(err)
	}

	votes := ids.Bag{}
	votes.Add(purple.ID())
	if _, err := graph.RecordPoll(votes); err == nil {
		t.Fatalf("Should have errored on rejecting an invalid tx")
	}
}

func StringTest(t *testing.T, factory Factory, prefix string) {
	Setup()

	graph := factory.New()

	params := snowball.Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       2, Alpha: 2, BetaVirtuous: 1, BetaRogue: 2,
	}
	graph.Initialize(snow.DefaultContextTest(), params)

	if err := graph.Add(Red); err != nil {
		t.Fatal(err)
	} else if err := graph.Add(Green); err != nil {
		t.Fatal(err)
	} else if err := graph.Add(Blue); err != nil {
		t.Fatal(err)
	} else if err := graph.Add(Alpha); err != nil {
		t.Fatal(err)
	} else if prefs := graph.Preferences(); prefs.Len() != 1 {
		t.Fatalf("Wrong number of preferences.")
	} else if !prefs.Contains(Red.ID()) {
		t.Fatalf("Wrong preference. Expected %s got %s", Red.ID(), prefs.List()[0])
	} else if graph.Finalized() {
		t.Fatalf("Finalized too early")
	}

	rb := ids.Bag{}
	rb.SetThreshold(2)
	rb.AddCount(Red.ID(), 2)
	rb.AddCount(Blue.ID(), 2)
	if _, err := graph.RecordPoll(rb); err != nil {
		t.Fatal(err)
	} else if err := graph.Add(Blue); err != nil {
		t.Fatal(err)
	}

	{
		expected := prefix + "(\n" +
			"    Choice[0] = ID:  LUC1cmcxnfNR9LdkACS2ccGKLEK7SYqB4gLLTycQfg1koyfSq Confidence: 1 Bias: 1\n" +
			"    Choice[1] = ID:  TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES Confidence: 0 Bias: 0\n" +
			"    Choice[2] = ID:  Zda4gsqTjRaX6XVZekVNi3ovMFPHDRQiGbzYuAb7Nwqy1rGBc Confidence: 0 Bias: 0\n" +
			"    Choice[3] = ID: 2mcwQKiD8VEspmMJpL1dc7okQQ5dDVAWeCBZ7FWBFAbxpv3t7w Confidence: 1 Bias: 1\n" +
			")"
		if str := graph.String(); str != expected {
			t.Fatalf("Expected %s, got %s", expected, str)
		}
	}

	if prefs := graph.Preferences(); prefs.Len() != 2 {
		t.Fatalf("Wrong number of preferences.")
	} else if !prefs.Contains(Red.ID()) {
		t.Fatalf("Wrong preference. Expected %s", Red.ID())
	} else if !prefs.Contains(Blue.ID()) {
		t.Fatalf("Wrong preference. Expected %s", Blue.ID())
	} else if graph.Finalized() {
		t.Fatalf("Finalized too early")
	}

	ga := ids.Bag{}
	ga.SetThreshold(2)
	ga.AddCount(Green.ID(), 2)
	ga.AddCount(Alpha.ID(), 2)
	if _, err := graph.RecordPoll(ga); err != nil {
		t.Fatal(err)
	}

	{
		expected := prefix + "(\n" +
			"    Choice[0] = ID:  LUC1cmcxnfNR9LdkACS2ccGKLEK7SYqB4gLLTycQfg1koyfSq Confidence: 0 Bias: 1\n" +
			"    Choice[1] = ID:  TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES Confidence: 1 Bias: 1\n" +
			"    Choice[2] = ID:  Zda4gsqTjRaX6XVZekVNi3ovMFPHDRQiGbzYuAb7Nwqy1rGBc Confidence: 1 Bias: 1\n" +
			"    Choice[3] = ID: 2mcwQKiD8VEspmMJpL1dc7okQQ5dDVAWeCBZ7FWBFAbxpv3t7w Confidence: 0 Bias: 1\n" +
			")"
		if str := graph.String(); str != expected {
			t.Fatalf("Expected %s, got %s", expected, str)
		}
	}

	if prefs := graph.Preferences(); prefs.Len() != 2 {
		t.Fatalf("Wrong number of preferences.")
	} else if !prefs.Contains(Red.ID()) {
		t.Fatalf("Wrong preference. Expected %s", Red.ID())
	} else if !prefs.Contains(Blue.ID()) {
		t.Fatalf("Wrong preference. Expected %s", Blue.ID())
	} else if graph.Finalized() {
		t.Fatalf("Finalized too early")
	}

	empty := ids.Bag{}
	if _, err := graph.RecordPoll(empty); err != nil {
		t.Fatal(err)
	}

	{
		expected := prefix + "(\n" +
			"    Choice[0] = ID:  LUC1cmcxnfNR9LdkACS2ccGKLEK7SYqB4gLLTycQfg1koyfSq Confidence: 0 Bias: 1\n" +
			"    Choice[1] = ID:  TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES Confidence: 0 Bias: 1\n" +
			"    Choice[2] = ID:  Zda4gsqTjRaX6XVZekVNi3ovMFPHDRQiGbzYuAb7Nwqy1rGBc Confidence: 0 Bias: 1\n" +
			"    Choice[3] = ID: 2mcwQKiD8VEspmMJpL1dc7okQQ5dDVAWeCBZ7FWBFAbxpv3t7w Confidence: 0 Bias: 1\n" +
			")"
		if str := graph.String(); str != expected {
			t.Fatalf("Expected %s, got %s", expected, str)
		}
	}

	if prefs := graph.Preferences(); prefs.Len() != 2 {
		t.Fatalf("Wrong number of preferences.")
	} else if !prefs.Contains(Red.ID()) {
		t.Fatalf("Wrong preference. Expected %s", Red.ID())
	} else if !prefs.Contains(Blue.ID()) {
		t.Fatalf("Wrong preference. Expected %s", Blue.ID())
	} else if graph.Finalized() {
		t.Fatalf("Finalized too early")
	} else if _, err := graph.RecordPoll(ga); err != nil {
		t.Fatal(err)
	}

	{
		expected := prefix + "(\n" +
			"    Choice[0] = ID:  LUC1cmcxnfNR9LdkACS2ccGKLEK7SYqB4gLLTycQfg1koyfSq Confidence: 0 Bias: 1\n" +
			"    Choice[1] = ID:  TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES Confidence: 1 Bias: 2\n" +
			"    Choice[2] = ID:  Zda4gsqTjRaX6XVZekVNi3ovMFPHDRQiGbzYuAb7Nwqy1rGBc Confidence: 1 Bias: 2\n" +
			"    Choice[3] = ID: 2mcwQKiD8VEspmMJpL1dc7okQQ5dDVAWeCBZ7FWBFAbxpv3t7w Confidence: 0 Bias: 1\n" +
			")"
		if str := graph.String(); str != expected {
			t.Fatalf("Expected %s, got %s", expected, str)
		}
	}

	if prefs := graph.Preferences(); prefs.Len() != 2 {
		t.Fatalf("Wrong number of preferences.")
	} else if !prefs.Contains(Green.ID()) {
		t.Fatalf("Wrong preference. Expected %s", Green.ID())
	} else if !prefs.Contains(Alpha.ID()) {
		t.Fatalf("Wrong preference. Expected %s", Alpha.ID())
	} else if graph.Finalized() {
		t.Fatalf("Finalized too early")
	} else if _, err := graph.RecordPoll(ga); err != nil {
		t.Fatal(err)
	}

	{
		expected := prefix + "()"
		if str := graph.String(); str != expected {
			t.Fatalf("Expected %s, got %s", expected, str)
		}
	}

	if prefs := graph.Preferences(); prefs.Len() != 0 {
		t.Fatalf("Wrong number of preferences.")
	} else if !graph.Finalized() {
		t.Fatalf("Finalized too late")
	} else if Green.Status() != choices.Accepted {
		t.Fatalf("%s should have been accepted", Green.ID())
	} else if Alpha.Status() != choices.Accepted {
		t.Fatalf("%s should have been accepted", Alpha.ID())
	} else if Red.Status() != choices.Rejected {
		t.Fatalf("%s should have been rejected", Red.ID())
	} else if Blue.Status() != choices.Rejected {
		t.Fatalf("%s should have been rejected", Blue.ID())
	} else if _, err := graph.RecordPoll(rb); err != nil {
		t.Fatal(err)
	}

	{
		expected := prefix + "()"
		if str := graph.String(); str != expected {
			t.Fatalf("Expected %s, got %s", expected, str)
		}
	}

	if prefs := graph.Preferences(); prefs.Len() != 0 {
		t.Fatalf("Wrong number of preferences.")
	} else if !graph.Finalized() {
		t.Fatalf("Finalized too late")
	} else if Green.Status() != choices.Accepted {
		t.Fatalf("%s should have been accepted", Green.ID())
	} else if Alpha.Status() != choices.Accepted {
		t.Fatalf("%s should have been accepted", Alpha.ID())
	} else if Red.Status() != choices.Rejected {
		t.Fatalf("%s should have been rejected", Red.ID())
	} else if Blue.Status() != choices.Rejected {
		t.Fatalf("%s should have been rejected", Blue.ID())
	}
}
