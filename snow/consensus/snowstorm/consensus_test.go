// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"errors"
	"path"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	sbcon "github.com/ava-labs/avalanchego/snow/consensus/snowball"
)

type testFunc func(*testing.T, Factory)

var (
	testFuncs = []testFunc{
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
		AddNonEmptyWhitelistTest,
		AddWhitelistedVirtuousTest,
		WhitelistConflictsTest,
		AcceptingDependencyTest,
		AcceptingSlowDependencyTest,
		RejectingDependencyTest,
		VacuouslyAcceptedTest,
		ConflictsTest,
		VirtuousDependsOnRogueTest,
		ErrorOnVacuouslyAcceptedTest,
		ErrorOnAcceptedTest,
		ErrorOnRejectingLowerConfidenceConflictTest,
		ErrorOnRejectingHigherConfidenceConflictTest,
		UTXOCleanupTest,
		RemoveVirtuousTest,
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
		color.InputIDsV = []ids.ID{}
		color.VerifyV = nil
		color.BytesV = []byte{byte(i)}
	}

	X := ids.Empty.Prefix(4)
	Y := ids.Empty.Prefix(5)
	Z := ids.Empty.Prefix(6)

	Red.InputIDsV = append(Red.InputIDsV, X)
	Green.InputIDsV = append(Green.InputIDsV, X)
	Green.InputIDsV = append(Green.InputIDsV, Y)

	Blue.InputIDsV = append(Blue.InputIDsV, Y)
	Blue.InputIDsV = append(Blue.InputIDsV, Z)

	Alpha.InputIDsV = append(Alpha.InputIDsV, Z)

	errs := wrappers.Errs{}
	errs.Add(
		Red.Verify(),
		Green.Verify(),
		Blue.Verify(),
		Alpha.Verify(),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}

// Execute all tests against a consensus implementation
func runConsensusTests(t *testing.T, factory Factory, prefix string) {
	for _, test := range testFuncs {
		Setup()
		t.Run(getTestName(test), func(tt *testing.T) {
			test(tt, factory)
		})
	}
	Setup()
	StringTest(t, factory, prefix)
}

func getTestName(i interface{}) string {
	return strings.Split(path.Base(runtime.FuncForPC(reflect.ValueOf(i).Pointer()).Name()), ".")[1]
}

func MetricsTest(t *testing.T, factory Factory) {
	{
		ctx := snow.DefaultConsensusContextTest()
		params := sbcon.Parameters{
			K:                 2,
			Alpha:             2,
			BetaVirtuous:      1,
			BetaRogue:         2,
			ConcurrentRepolls: 1,
		}
		err := ctx.Registerer.Register(prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tx_processing",
		}))
		if err != nil {
			t.Fatal(err)
		}
		graph := factory.New()
		if err := graph.Initialize(ctx, params); err == nil {
			t.Fatalf("should have errored due to a duplicated metric")
		}
	}
	{
		ctx := snow.DefaultConsensusContextTest()
		params := sbcon.Parameters{
			K:                 2,
			Alpha:             2,
			BetaVirtuous:      1,
			BetaRogue:         2,
			ConcurrentRepolls: 1,
		}
		err := ctx.Registerer.Register(prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tx_accepted",
		}))
		if err != nil {
			t.Fatal(err)
		}
		graph := factory.New()
		if err := graph.Initialize(ctx, params); err == nil {
			t.Fatalf("should have errored due to a duplicated metric")
		}
	}
	{
		ctx := snow.DefaultConsensusContextTest()
		params := sbcon.Parameters{
			K:                 2,
			Alpha:             2,
			BetaVirtuous:      1,
			BetaRogue:         2,
			ConcurrentRepolls: 1,
		}
		err := ctx.Registerer.Register(prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tx_rejected",
		}))
		if err != nil {
			t.Fatal(err)
		}
		graph := factory.New()
		if err := graph.Initialize(ctx, params); err == nil {
			t.Fatalf("should have errored due to a duplicated metric")
		}
	}
}

func ParamsTest(t *testing.T, factory Factory) {
	graph := factory.New()

	params := sbcon.Parameters{
		K:                     2,
		Alpha:                 2,
		BetaVirtuous:          1,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	err := graph.Initialize(snow.DefaultConsensusContextTest(), params)
	if err != nil {
		t.Fatal(err)
	}

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
	graph := factory.New()

	params := sbcon.Parameters{
		K:                     2,
		Alpha:                 2,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	err := graph.Initialize(snow.DefaultConsensusContextTest(), params)
	if err != nil {
		t.Fatal(err)
	}

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
	graph := factory.New()

	params := sbcon.Parameters{
		K:                     2,
		Alpha:                 2,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	err := graph.Initialize(snow.DefaultConsensusContextTest(), params)
	if err != nil {
		t.Fatal(err)
	}

	if err := graph.Add(Red); err != nil {
		t.Fatal(err)
	} else if err := graph.Add(Green); err != nil {
		t.Fatal(err)
	}

	prefs := graph.Preferences()
	switch {
	case prefs.Len() != 1:
		t.Fatalf("Wrong number of preferences.")
	case !prefs.Contains(Red.ID()):
		t.Fatalf("Wrong preference. Expected %s got %s", Red.ID(), prefs.List()[0])
	case graph.Finalized():
		t.Fatalf("Finalized too early")
	}

	r := ids.Bag{}
	r.SetThreshold(2)
	r.AddCount(Red.ID(), 2)
	if updated, err := graph.RecordPoll(r); err != nil {
		t.Fatal(err)
	} else if !updated {
		t.Fatalf("Should have updated the frontiers")
	}

	prefs = graph.Preferences()
	switch {
	case prefs.Len() != 0:
		t.Fatalf("Wrong number of preferences.")
	case !graph.Finalized():
		t.Fatalf("Finalized too late")
	case Red.Status() != choices.Accepted:
		t.Fatalf("%s should have been accepted", Red.ID())
	case Green.Status() != choices.Rejected:
		t.Fatalf("%s should have been rejected", Green.ID())
	}
}

func LowerConfidenceTest(t *testing.T, factory Factory) {
	graph := factory.New()

	params := sbcon.Parameters{
		K:                     2,
		Alpha:                 2,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	err := graph.Initialize(snow.DefaultConsensusContextTest(), params)
	if err != nil {
		t.Fatal(err)
	}

	if err := graph.Add(Red); err != nil {
		t.Fatal(err)
	}
	if err := graph.Add(Green); err != nil {
		t.Fatal(err)
	}
	if err := graph.Add(Blue); err != nil {
		t.Fatal(err)
	}

	prefs := graph.Preferences()
	switch {
	case prefs.Len() != 1:
		t.Fatalf("Wrong number of preferences.")
	case !prefs.Contains(Red.ID()):
		t.Fatalf("Wrong preference. Expected %s got %s", Red.ID(), prefs.List()[0])
	case graph.Finalized():
		t.Fatalf("Finalized too early")
	}

	r := ids.Bag{}
	r.SetThreshold(2)
	r.AddCount(Red.ID(), 2)
	if updated, err := graph.RecordPoll(r); err != nil {
		t.Fatal(err)
	} else if !updated {
		t.Fatalf("Should have updated the frontiers")
	}

	prefs = graph.Preferences()
	switch {
	case prefs.Len() != 1:
		t.Fatalf("Wrong number of preferences.")
	case !prefs.Contains(Blue.ID()):
		t.Fatalf("Wrong preference. Expected %s", Blue.ID())
	case graph.Finalized():
		t.Fatalf("Finalized too early")
	}
}

func MiddleConfidenceTest(t *testing.T, factory Factory) {
	graph := factory.New()

	params := sbcon.Parameters{
		K:                     2,
		Alpha:                 2,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	err := graph.Initialize(snow.DefaultConsensusContextTest(), params)
	if err != nil {
		t.Fatal(err)
	}

	if err := graph.Add(Red); err != nil {
		t.Fatal(err)
	}
	if err := graph.Add(Green); err != nil {
		t.Fatal(err)
	}
	if err := graph.Add(Alpha); err != nil {
		t.Fatal(err)
	}
	if err := graph.Add(Blue); err != nil {
		t.Fatal(err)
	}

	prefs := graph.Preferences()
	switch {
	case prefs.Len() != 2:
		t.Fatalf("Wrong number of preferences.")
	case !prefs.Contains(Red.ID()):
		t.Fatalf("Wrong preference. Expected %s", Red.ID())
	case !prefs.Contains(Alpha.ID()):
		t.Fatalf("Wrong preference. Expected %s", Alpha.ID())
	case graph.Finalized():
		t.Fatalf("Finalized too early")
	}

	r := ids.Bag{}
	r.SetThreshold(2)
	r.AddCount(Red.ID(), 2)
	if updated, err := graph.RecordPoll(r); err != nil {
		t.Fatal(err)
	} else if !updated {
		t.Fatalf("Should have updated the frontiers")
	}

	prefs = graph.Preferences()
	switch {
	case prefs.Len() != 1:
		t.Fatalf("Wrong number of preferences.")
	case !prefs.Contains(Alpha.ID()):
		t.Fatalf("Wrong preference. Expected %s", Alpha.ID())
	case graph.Finalized():
		t.Fatalf("Finalized too early")
	}
}

func IndependentTest(t *testing.T, factory Factory) {
	graph := factory.New()

	params := sbcon.Parameters{
		K:                     2,
		Alpha:                 2,
		BetaVirtuous:          2,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	err := graph.Initialize(snow.DefaultConsensusContextTest(), params)
	if err != nil {
		t.Fatal(err)
	}

	if err := graph.Add(Red); err != nil {
		t.Fatal(err)
	}
	if err := graph.Add(Alpha); err != nil {
		t.Fatal(err)
	}

	prefs := graph.Preferences()
	switch {
	case prefs.Len() != 2:
		t.Fatalf("Wrong number of preferences.")
	case !prefs.Contains(Red.ID()):
		t.Fatalf("Wrong preference. Expected %s", Red.ID())
	case !prefs.Contains(Alpha.ID()):
		t.Fatalf("Wrong preference. Expected %s", Alpha.ID())
	case graph.Finalized():
		t.Fatalf("Finalized too early")
	}

	ra := ids.Bag{}
	ra.SetThreshold(2)
	ra.AddCount(Red.ID(), 2)
	ra.AddCount(Alpha.ID(), 2)
	if updated, err := graph.RecordPoll(ra); err != nil {
		t.Fatal(err)
	} else if updated {
		t.Fatalf("Shouldn't have updated the frontiers")
	} else if prefs := graph.Preferences(); prefs.Len() != 2 {
		t.Fatalf("Wrong number of preferences.")
	} else if !prefs.Contains(Red.ID()) {
		t.Fatalf("Wrong preference. Expected %s", Red.ID())
	} else if !prefs.Contains(Alpha.ID()) {
		t.Fatalf("Wrong preference. Expected %s", Alpha.ID())
	} else if graph.Finalized() {
		t.Fatalf("Finalized too early")
	} else if updated, err := graph.RecordPoll(ra); err != nil {
		t.Fatal(err)
	} else if !updated {
		t.Fatalf("Should have updated the frontiers")
	} else if prefs := graph.Preferences(); prefs.Len() != 0 {
		t.Fatalf("Wrong number of preferences.")
	} else if !graph.Finalized() {
		t.Fatalf("Finalized too late")
	}
}

func VirtuousTest(t *testing.T, factory Factory) {
	graph := factory.New()

	params := sbcon.Parameters{
		K:                     2,
		Alpha:                 2,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	err := graph.Initialize(snow.DefaultConsensusContextTest(), params)
	if err != nil {
		t.Fatal(err)
	}

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
	graph := factory.New()

	params := sbcon.Parameters{
		K:                     2,
		Alpha:                 2,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	if err := graph.Initialize(snow.DefaultConsensusContextTest(), params); err != nil {
		t.Fatal(err)
	}

	switch {
	case !graph.IsVirtuous(Red):
		t.Fatalf("Should be virtuous")
	case !graph.IsVirtuous(Green):
		t.Fatalf("Should be virtuous")
	case !graph.IsVirtuous(Blue):
		t.Fatalf("Should be virtuous")
	case !graph.IsVirtuous(Alpha):
		t.Fatalf("Should be virtuous")
	}

	err := graph.Add(Red)
	switch {
	case err != nil:
		t.Fatal(err)
	case !graph.IsVirtuous(Red):
		t.Fatalf("Should be virtuous")
	case graph.IsVirtuous(Green):
		t.Fatalf("Should not be virtuous")
	case !graph.IsVirtuous(Blue):
		t.Fatalf("Should be virtuous")
	case !graph.IsVirtuous(Alpha):
		t.Fatalf("Should be virtuous")
	}

	err = graph.Add(Green)
	switch {
	case err != nil:
		t.Fatal(err)
	case graph.IsVirtuous(Red):
		t.Fatalf("Should not be virtuous")
	case graph.IsVirtuous(Green):
		t.Fatalf("Should not be virtuous")
	case graph.IsVirtuous(Blue):
		t.Fatalf("Should not be virtuous")
	}
}

func QuiesceTest(t *testing.T, factory Factory) {
	graph := factory.New()

	params := sbcon.Parameters{
		K:                     2,
		Alpha:                 2,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	err := graph.Initialize(snow.DefaultConsensusContextTest(), params)
	if err != nil {
		t.Fatal(err)
	}

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

func AddNonEmptyWhitelistTest(t *testing.T, factory Factory) {
	graph := factory.New()

	params := sbcon.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	err := graph.Initialize(snow.DefaultConsensusContextTest(), params)
	if err != nil {
		t.Fatal(err)
	}

	inputA := ids.Empty.Prefix(1000)
	inputB := ids.Empty.Prefix(2000)
	inputC := ids.Empty.Prefix(3000)
	inputD := ids.Empty.Prefix(4000)
	inputE := ids.Empty.Prefix(5000)

	/*
	                    [tx1]
	                   ⬈     ⬉
	              [tx2]       [tx3]
	            ⬈      ⬉      ⬈
	       [tx6]         [tx4]
	         ⬆             ⬆
	    {stop stx7}    {stop stx5}
	   Add stx5 => no conflict
	   Add  tx6 => stx5 conflicts with tx6
	   Add stx7 => stx5 conflicts with tx6
	               stx5 conflicts with stx7
	               stx7 conflicts with tx3
	               stx7 conflicts with tx4
	               stx7 conflicts with stx5
	*/
	tx1 := createTestTx(1, inputA, inputB)

	// tx1 is prev tx that spends A, so tx2.out = tx1
	// tx1 is prev tx that spends B, so tx3.out = tx1
	tx2 := createTestTx(2, inputA, inputD)
	tx3 := createTestTx(3, inputB)

	// tx2 is prev tx that spends A, so tx4.out = tx2
	// tx3 is prev tx that spends B, so tx4.out = tx3
	tx4 := createTestTx(4, inputA, inputB, inputC)

	// stx5 as stop vertex
	// when added, no conflict
	stx5 := createTestTx(5)
	stx5.InputIDsV = []ids.ID{stx5.IDV}
	stx5.WhitelistIsV = true
	stx5.WhitelistV = ids.Set{
		tx1.IDV: struct{}{},
		tx2.IDV: struct{}{},
		tx3.IDV: struct{}{},
		tx4.IDV: struct{}{},
	}

	// tx2 is prev tx that spends D, so tx6.out = tx2
	tx6 := createTestTx(6, inputD, inputE)

	// stx7 as stop vertex
	// tx6 is prev tx that spends E, so stx7.out = tx6
	stx7 := createTestTx(7)
	stx7.InputIDsV = []ids.ID{stx7.IDV}
	stx7.WhitelistIsV = true
	stx7.WhitelistV = ids.Set{
		tx1.IDV: struct{}{},
		tx2.IDV: struct{}{},
		tx6.IDV: struct{}{},
	}

	txs := []*TestTx{tx1, tx2, tx3, tx4, stx5, tx6, stx7}
	for i, tx := range txs {
		t.Logf("adding transaction[%02d]: %v", i+1, tx.IDV)
		if err := tx.Verify(); err != nil {
			t.Fatal(err)
		}
		if err := graph.Add(tx); err != nil {
			t.Fatal(err)
		}
	}

	vset1 := graph.Virtuous()
	if !vset1.Equals(ids.Set{}) {
		t.Fatalf("unexpected virtuous %v", vset1)
	}
	pset1 := graph.Preferences()
	if !pset1.Equals(ids.Set{
		tx1.IDV:  struct{}{},
		stx5.IDV: struct{}{},
	}) {
		t.Fatalf("unexpected preferences %v", pset1)
	}
	if graph.Finalized() {
		t.Fatal("unexpected Finalized")
	}

	r := ids.Bag{}
	r.SetThreshold(2)
	r.AddCount(tx1.ID(), 2)

	updated, err := graph.RecordPoll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Fatal("should have updated the frontiers")
	}

	vset2 := graph.Virtuous()
	if !vset2.Equals(ids.Set{}) {
		t.Fatalf("unexpected virtuous %v", vset2)
	}
	pset2 := graph.Preferences()
	if !pset2.Equals(ids.Set{
		stx5.IDV: struct{}{},
	}) {
		t.Fatalf("unexpected preferences %v", pset2)
	}
}

func AddWhitelistedVirtuousTest(t *testing.T, factory Factory) {
	graph := factory.New()

	params := sbcon.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	err := graph.Initialize(snow.DefaultConsensusContextTest(), params)
	if err != nil {
		t.Fatal(err)
	}

	tx0 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: []ids.ID{ids.GenerateTestID()},
		BytesV:    utils.RandomBytes(32),
	}
	tx1 := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV:    []ids.ID{ids.GenerateTestID()},
		BytesV:       utils.RandomBytes(32),
		WhitelistIsV: true,
	}

	txs := []*TestTx{tx0, tx1}
	for _, tx := range txs {
		if err := graph.Add(tx); err != nil {
			t.Fatal(err)
		}
	}

	vset := graph.Virtuous()
	if vset.Len() != 0 {
		t.Fatalf("unexpected virtuous %v", vset)
	}
}

// When a transaction supporting whitelisting is added to the conflict graph,
// all txs outside of its whitelist should be marked in conflict.
func WhitelistConflictsTest(t *testing.T, factory Factory) {
	graph := factory.New()

	params := sbcon.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	err := graph.Initialize(snow.DefaultConsensusContextTest(), params)
	if err != nil {
		t.Fatal(err)
	}

	n := 10
	txIDs := make([]ids.ID, n)
	for i := range txIDs {
		txIDs[i] = ids.GenerateTestID()
	}
	allTxIDs := ids.NewSet(n)
	allTxIDs.Add(txIDs...)

	// each spending each other
	allTxs := make([]Tx, n)
	for i, txID := range txIDs {
		tx := &TestTx{
			TestDecidable: choices.TestDecidable{
				IDV:     txID,
				AcceptV: nil,
				StatusV: choices.Processing,
			},
			InputIDsV:    []ids.ID{txID},
			WhitelistV:   nil,
			WhitelistIsV: false,
		}
		allTxs[i] = tx
		if err := graph.Add(tx); err != nil {
			t.Fatal(err)
		}
	}

	whitelist := ids.NewSet(1)
	whitelist.Add(ids.GenerateTestID())

	// make whitelist transaction that conflicts with tx outside of its
	// whitelist
	wlTxID := ids.GenerateTestID()
	wlTx := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     wlTxID,
			AcceptV: nil,
			StatusV: choices.Processing,
		},
		InputIDsV:     []ids.ID{wlTxID},
		WhitelistV:    whitelist,
		WhitelistIsV:  true,
		WhitelistErrV: nil,
	}
	if err := graph.Add(wlTx); err != nil {
		t.Fatal(err)
	}

	for _, tx := range allTxs {
		conflicts := graph.Conflicts(tx)
		if conflicts.Len() != 1 {
			t.Fatal("wrong number of conflicts")
		}
		if !conflicts.Contains(wlTxID) {
			t.Fatal("unexpected conflict")
		}
	}

	// the transitive vertex should be conflicting with everything
	conflicts := graph.Conflicts(wlTx)
	if !allTxIDs.Equals(conflicts) {
		t.Fatal("transitive vertex outs != all txs")
	}
}

func createTestTx(id uint64, inputIDs ...ids.ID) *TestTx {
	return &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(id),
			StatusV: choices.Processing,
		},
		InputIDsV: inputIDs,
		BytesV:    []byte{byte(id)},
	}
}

func AcceptingDependencyTest(t *testing.T, factory Factory) {
	graph := factory.New()

	purple := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(7),
			StatusV: choices.Processing,
		},
		DependenciesV: []Tx{Red},
	}
	purple.InputIDsV = append(purple.InputIDsV, ids.Empty.Prefix(8))

	params := sbcon.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	if err := graph.Initialize(snow.DefaultConsensusContextTest(), params); err != nil {
		t.Fatal(err)
	}

	if err := graph.Add(Red); err != nil {
		t.Fatal(err)
	}
	if err := graph.Add(Green); err != nil {
		t.Fatal(err)
	}
	if err := graph.Add(purple); err != nil {
		t.Fatal(err)
	}

	prefs := graph.Preferences()
	switch {
	case prefs.Len() != 2:
		t.Fatalf("Wrong number of preferences.")
	case !prefs.Contains(Red.ID()):
		t.Fatalf("Wrong preference. Expected %s", Red.ID())
	case !prefs.Contains(purple.ID()):
		t.Fatalf("Wrong preference. Expected %s", purple.ID())
	case Red.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", Red.ID(), choices.Processing)
	case Green.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", Green.ID(), choices.Processing)
	case purple.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", purple.ID(), choices.Processing)
	}

	g := ids.Bag{}
	g.Add(Green.ID())
	if updated, err := graph.RecordPoll(g); err != nil {
		t.Fatal(err)
	} else if !updated {
		t.Fatalf("Should have updated the frontiers")
	}

	prefs = graph.Preferences()
	switch {
	case prefs.Len() != 2:
		t.Fatalf("Wrong number of preferences.")
	case !prefs.Contains(Green.ID()):
		t.Fatalf("Wrong preference. Expected %s", Green.ID())
	case !prefs.Contains(purple.ID()):
		t.Fatalf("Wrong preference. Expected %s", purple.ID())
	case Red.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", Red.ID(), choices.Processing)
	case Green.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", Green.ID(), choices.Processing)
	case purple.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", purple.ID(), choices.Processing)
	}

	rp := ids.Bag{}
	rp.Add(Red.ID(), purple.ID())
	if updated, err := graph.RecordPoll(rp); err != nil {
		t.Fatal(err)
	} else if updated {
		t.Fatalf("Shouldn't have updated the frontiers")
	}

	prefs = graph.Preferences()
	switch {
	case prefs.Len() != 2:
		t.Fatalf("Wrong number of preferences.")
	case !prefs.Contains(Green.ID()):
		t.Fatalf("Wrong preference. Expected %s", Green.ID())
	case !prefs.Contains(purple.ID()):
		t.Fatalf("Wrong preference. Expected %s", purple.ID())
	case Red.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", Red.ID(), choices.Processing)
	case Green.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", Green.ID(), choices.Processing)
	case purple.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", purple.ID(), choices.Processing)
	}

	r := ids.Bag{}
	r.Add(Red.ID())
	if updated, err := graph.RecordPoll(r); err != nil {
		t.Fatal(err)
	} else if !updated {
		t.Fatalf("Should have updated the frontiers")
	}

	prefs = graph.Preferences()
	switch {
	case prefs.Len() != 0:
		t.Fatalf("Wrong number of preferences.")
	case Red.Status() != choices.Accepted:
		t.Fatalf("Wrong status. %s should be %s", Red.ID(), choices.Accepted)
	case Green.Status() != choices.Rejected:
		t.Fatalf("Wrong status. %s should be %s", Green.ID(), choices.Rejected)
	case purple.Status() != choices.Accepted:
		t.Fatalf("Wrong status. %s should be %s", purple.ID(), choices.Accepted)
	}
}

type singleAcceptTx struct {
	Tx

	t        *testing.T
	accepted bool
}

func (tx *singleAcceptTx) Accept() error {
	if tx.accepted {
		tx.t.Fatalf("accept called multiple times")
	}
	tx.accepted = true
	return tx.Tx.Accept()
}

func AcceptingSlowDependencyTest(t *testing.T, factory Factory) {
	graph := factory.New()

	rawPurple := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(7),
			StatusV: choices.Processing,
		},
		DependenciesV: []Tx{Red},
	}
	rawPurple.InputIDsV = append(rawPurple.InputIDsV, ids.Empty.Prefix(8))

	purple := &singleAcceptTx{
		Tx: rawPurple,
		t:  t,
	}

	params := sbcon.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	err := graph.Initialize(snow.DefaultConsensusContextTest(), params)
	if err != nil {
		t.Fatal(err)
	}

	if err := graph.Add(Red); err != nil {
		t.Fatal(err)
	}
	if err := graph.Add(Green); err != nil {
		t.Fatal(err)
	}
	if err := graph.Add(purple); err != nil {
		t.Fatal(err)
	}

	prefs := graph.Preferences()
	switch {
	case prefs.Len() != 2:
		t.Fatalf("Wrong number of preferences.")
	case !prefs.Contains(Red.ID()):
		t.Fatalf("Wrong preference. Expected %s", Red.ID())
	case !prefs.Contains(purple.ID()):
		t.Fatalf("Wrong preference. Expected %s", purple.ID())
	case Red.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", Red.ID(), choices.Processing)
	case Green.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", Green.ID(), choices.Processing)
	case purple.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", purple.ID(), choices.Processing)
	}

	g := ids.Bag{}
	g.Add(Green.ID())
	if updated, err := graph.RecordPoll(g); err != nil {
		t.Fatal(err)
	} else if !updated {
		t.Fatalf("Should have updated the frontiers")
	}

	prefs = graph.Preferences()
	switch {
	case prefs.Len() != 2:
		t.Fatalf("Wrong number of preferences.")
	case !prefs.Contains(Green.ID()):
		t.Fatalf("Wrong preference. Expected %s", Green.ID())
	case !prefs.Contains(purple.ID()):
		t.Fatalf("Wrong preference. Expected %s", purple.ID())
	case Red.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", Red.ID(), choices.Processing)
	case Green.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", Green.ID(), choices.Processing)
	case purple.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", purple.ID(), choices.Processing)
	}

	p := ids.Bag{}
	p.Add(purple.ID())
	if updated, err := graph.RecordPoll(p); err != nil {
		t.Fatal(err)
	} else if updated {
		t.Fatalf("Shouldn't have updated the frontiers")
	}

	prefs = graph.Preferences()
	switch {
	case prefs.Len() != 2:
		t.Fatalf("Wrong number of preferences.")
	case !prefs.Contains(Green.ID()):
		t.Fatalf("Wrong preference. Expected %s", Green.ID())
	case !prefs.Contains(purple.ID()):
		t.Fatalf("Wrong preference. Expected %s", purple.ID())
	case Red.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", Red.ID(), choices.Processing)
	case Green.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", Green.ID(), choices.Processing)
	case purple.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", purple.ID(), choices.Processing)
	}

	rp := ids.Bag{}
	rp.Add(Red.ID(), purple.ID())
	if updated, err := graph.RecordPoll(rp); err != nil {
		t.Fatal(err)
	} else if updated {
		t.Fatalf("Shouldn't have updated the frontiers")
	}

	prefs = graph.Preferences()
	switch {
	case prefs.Len() != 2:
		t.Fatalf("Wrong number of preferences.")
	case !prefs.Contains(Green.ID()):
		t.Fatalf("Wrong preference. Expected %s", Green.ID())
	case !prefs.Contains(purple.ID()):
		t.Fatalf("Wrong preference. Expected %s", purple.ID())
	case Red.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", Red.ID(), choices.Processing)
	case Green.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", Green.ID(), choices.Processing)
	case purple.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", purple.ID(), choices.Processing)
	}

	r := ids.Bag{}
	r.Add(Red.ID())
	if updated, err := graph.RecordPoll(r); err != nil {
		t.Fatal(err)
	} else if !updated {
		t.Fatalf("Should have updated the frontiers")
	}

	prefs = graph.Preferences()
	switch {
	case prefs.Len() != 0:
		t.Fatalf("Wrong number of preferences.")
	case Red.Status() != choices.Accepted:
		t.Fatalf("Wrong status. %s should be %s", Red.ID(), choices.Accepted)
	case Green.Status() != choices.Rejected:
		t.Fatalf("Wrong status. %s should be %s", Green.ID(), choices.Rejected)
	case purple.Status() != choices.Accepted:
		t.Fatalf("Wrong status. %s should be %s", purple.ID(), choices.Accepted)
	}
}

func RejectingDependencyTest(t *testing.T, factory Factory) {
	graph := factory.New()

	purple := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(7),
			StatusV: choices.Processing,
		},
		DependenciesV: []Tx{Red, Blue},
	}
	purple.InputIDsV = append(purple.InputIDsV, ids.Empty.Prefix(8))

	params := sbcon.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	err := graph.Initialize(snow.DefaultConsensusContextTest(), params)
	if err != nil {
		t.Fatal(err)
	}

	if err := graph.Add(Red); err != nil {
		t.Fatal(err)
	}
	if err := graph.Add(Green); err != nil {
		t.Fatal(err)
	}
	if err := graph.Add(Blue); err != nil {
		t.Fatal(err)
	}
	if err := graph.Add(purple); err != nil {
		t.Fatal(err)
	}

	prefs := graph.Preferences()
	switch {
	case prefs.Len() != 2:
		t.Fatalf("Wrong number of preferences.")
	case !prefs.Contains(Red.ID()):
		t.Fatalf("Wrong preference. Expected %s", Red.ID())
	case !prefs.Contains(purple.ID()):
		t.Fatalf("Wrong preference. Expected %s", purple.ID())
	case Red.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", Red.ID(), choices.Processing)
	case Green.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", Green.ID(), choices.Processing)
	case Blue.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", Blue.ID(), choices.Processing)
	case purple.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", purple.ID(), choices.Processing)
	}

	gp := ids.Bag{}
	gp.Add(Green.ID(), purple.ID())
	if updated, err := graph.RecordPoll(gp); err != nil {
		t.Fatal(err)
	} else if !updated {
		t.Fatalf("Should have updated the frontiers")
	}

	prefs = graph.Preferences()
	switch {
	case prefs.Len() != 2:
		t.Fatalf("Wrong number of preferences.")
	case !prefs.Contains(Green.ID()):
		t.Fatalf("Wrong preference. Expected %s", Green.ID())
	case !prefs.Contains(purple.ID()):
		t.Fatalf("Wrong preference. Expected %s", purple.ID())
	case Red.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", Red.ID(), choices.Processing)
	case Green.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", Green.ID(), choices.Processing)
	case Blue.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", Blue.ID(), choices.Processing)
	case purple.Status() != choices.Processing:
		t.Fatalf("Wrong status. %s should be %s", purple.ID(), choices.Processing)
	}

	if updated, err := graph.RecordPoll(gp); err != nil {
		t.Fatal(err)
	} else if !updated {
		t.Fatalf("Should have updated the frontiers")
	}

	prefs = graph.Preferences()
	switch {
	case prefs.Len() != 0:
		t.Fatalf("Wrong number of preferences.")
	case Red.Status() != choices.Rejected:
		t.Fatalf("Wrong status. %s should be %s", Red.ID(), choices.Rejected)
	case Green.Status() != choices.Accepted:
		t.Fatalf("Wrong status. %s should be %s", Green.ID(), choices.Accepted)
	case Blue.Status() != choices.Rejected:
		t.Fatalf("Wrong status. %s should be %s", Blue.ID(), choices.Rejected)
	case purple.Status() != choices.Rejected:
		t.Fatalf("Wrong status. %s should be %s", purple.ID(), choices.Rejected)
	}
}

func VacuouslyAcceptedTest(t *testing.T, factory Factory) {
	graph := factory.New()

	purple := &TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.Empty.Prefix(7),
		StatusV: choices.Processing,
	}}

	params := sbcon.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	err := graph.Initialize(snow.DefaultConsensusContextTest(), params)
	if err != nil {
		t.Fatal(err)
	}

	if err := graph.Add(purple); err != nil {
		t.Fatal(err)
	} else if prefs := graph.Preferences(); prefs.Len() != 0 {
		t.Fatalf("Wrong number of preferences.")
	} else if status := purple.Status(); status != choices.Accepted {
		t.Fatalf("Wrong status. %s should be %s", purple.ID(), choices.Accepted)
	}
}

func ConflictsTest(t *testing.T, factory Factory) {
	graph := factory.New()

	params := sbcon.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	err := graph.Initialize(snow.DefaultConsensusContextTest(), params)
	if err != nil {
		t.Fatal(err)
	}

	conflictInputID := ids.Empty.Prefix(0)

	purple := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(6),
			StatusV: choices.Processing,
		},
		InputIDsV: []ids.ID{conflictInputID},
	}

	orange := &TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(7),
			StatusV: choices.Processing,
		},
		InputIDsV: []ids.ID{conflictInputID},
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
	graph := factory.New()

	params := sbcon.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	err := graph.Initialize(snow.DefaultConsensusContextTest(), params)
	if err != nil {
		t.Fatal(err)
	}

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

	rogue1.InputIDsV = append(rogue1.InputIDsV, input1)
	rogue2.InputIDsV = append(rogue2.InputIDsV, input1)

	virtuous.InputIDsV = append(virtuous.InputIDsV, input2)

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
	if updated, err := graph.RecordPoll(votes); err != nil {
		t.Fatal(err)
	} else if updated {
		t.Fatalf("Shouldn't have updated the frontiers")
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
	graph := factory.New()

	purple := &TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.Empty.Prefix(7),
		AcceptV: errors.New(""),
		StatusV: choices.Processing,
	}}

	params := sbcon.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	err := graph.Initialize(snow.DefaultConsensusContextTest(), params)
	if err != nil {
		t.Fatal(err)
	}

	if err := graph.Add(purple); err == nil {
		t.Fatalf("Should have errored on acceptance")
	}
}

func ErrorOnAcceptedTest(t *testing.T, factory Factory) {
	graph := factory.New()

	purple := &TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.Empty.Prefix(7),
		AcceptV: errors.New(""),
		StatusV: choices.Processing,
	}}
	purple.InputIDsV = append(purple.InputIDsV, ids.Empty.Prefix(4))

	params := sbcon.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	err := graph.Initialize(snow.DefaultConsensusContextTest(), params)
	if err != nil {
		t.Fatal(err)
	}

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
	graph := factory.New()

	X := ids.Empty.Prefix(4)

	purple := &TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.Empty.Prefix(7),
		StatusV: choices.Processing,
	}}
	purple.InputIDsV = append(purple.InputIDsV, X)

	pink := &TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.Empty.Prefix(8),
		RejectV: errors.New(""),
		StatusV: choices.Processing,
	}}
	pink.InputIDsV = append(pink.InputIDsV, X)

	params := sbcon.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	err := graph.Initialize(snow.DefaultConsensusContextTest(), params)
	if err != nil {
		t.Fatal(err)
	}

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
	graph := factory.New()

	X := ids.Empty.Prefix(4)

	purple := &TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.Empty.Prefix(7),
		StatusV: choices.Processing,
	}}
	purple.InputIDsV = append(purple.InputIDsV, X)

	pink := &TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.Empty.Prefix(8),
		RejectV: errors.New(""),
		StatusV: choices.Processing,
	}}
	pink.InputIDsV = append(pink.InputIDsV, X)

	params := sbcon.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	err := graph.Initialize(snow.DefaultConsensusContextTest(), params)
	if err != nil {
		t.Fatal(err)
	}

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

func UTXOCleanupTest(t *testing.T, factory Factory) {
	graph := factory.New()

	params := sbcon.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	err := graph.Initialize(snow.DefaultConsensusContextTest(), params)
	assert.NoError(t, err)

	err = graph.Add(Red)
	assert.NoError(t, err)

	err = graph.Add(Green)
	assert.NoError(t, err)

	redVotes := ids.Bag{}
	redVotes.Add(Red.ID())
	changed, err := graph.RecordPoll(redVotes)
	assert.NoError(t, err)
	assert.False(t, changed, "shouldn't have accepted the red tx")

	changed, err = graph.RecordPoll(redVotes)
	assert.NoError(t, err)
	assert.True(t, changed, "should have accepted the red tx")

	assert.Equal(t, choices.Accepted, Red.Status())
	assert.Equal(t, choices.Rejected, Green.Status())

	err = graph.Add(Blue)
	assert.NoError(t, err)

	blueVotes := ids.Bag{}
	blueVotes.Add(Blue.ID())
	changed, err = graph.RecordPoll(blueVotes)
	assert.NoError(t, err)
	assert.True(t, changed, "should have accepted the blue tx")

	assert.Equal(t, choices.Accepted, Blue.Status())
}

func RemoveVirtuousTest(t *testing.T, factory Factory) {
	graph := factory.New()

	params := sbcon.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	err := graph.Initialize(snow.DefaultConsensusContextTest(), params)
	assert.NoError(t, err)

	err = graph.Add(Red)
	assert.NoError(t, err)

	virtuous := graph.Virtuous()
	assert.NotEmpty(t, virtuous, "a virtuous transaction was added but not tracked")

	err = graph.Remove(Red.ID())
	assert.NoError(t, err)

	virtuous = graph.Virtuous()
	assert.Empty(t, virtuous, "removal of a virtuous transaction should have emptied the virtuous set")
}

func StringTest(t *testing.T, factory Factory, prefix string) {
	graph := factory.New()

	params := sbcon.Parameters{
		K:                     2,
		Alpha:                 2,
		BetaVirtuous:          1,
		BetaRogue:             2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	err := graph.Initialize(snow.DefaultConsensusContextTest(), params)
	if err != nil {
		t.Fatal(err)
	}

	if err := graph.Add(Red); err != nil {
		t.Fatal(err)
	}
	if err := graph.Add(Green); err != nil {
		t.Fatal(err)
	}
	if err := graph.Add(Blue); err != nil {
		t.Fatal(err)
	}
	if err := graph.Add(Alpha); err != nil {
		t.Fatal(err)
	}

	prefs := graph.Preferences()
	switch {
	case prefs.Len() != 1:
		t.Fatalf("Wrong number of preferences.")
	case !prefs.Contains(Red.ID()):
		t.Fatalf("Wrong preference. Expected %s got %s", Red.ID(), prefs.List()[0])
	case graph.Finalized():
		t.Fatalf("Finalized too early")
	}

	rb := ids.Bag{}
	rb.SetThreshold(2)
	rb.AddCount(Red.ID(), 2)
	rb.AddCount(Blue.ID(), 2)
	if changed, err := graph.RecordPoll(rb); err != nil {
		t.Fatal(err)
	} else if !changed {
		t.Fatalf("Should have caused the frontiers to recalculate")
	} else if err := graph.Add(Blue); err != nil {
		t.Fatal(err)
	}

	{
		expected := prefix + "(\n" +
			"    Choice[0] = ID:  LUC1cmcxnfNR9LdkACS2ccGKLEK7SYqB4gLLTycQfg1koyfSq SB(NumSuccessfulPolls = 1, Confidence = 1)\n" +
			"    Choice[1] = ID:  TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES SB(NumSuccessfulPolls = 0, Confidence = 0)\n" +
			"    Choice[2] = ID:  Zda4gsqTjRaX6XVZekVNi3ovMFPHDRQiGbzYuAb7Nwqy1rGBc SB(NumSuccessfulPolls = 0, Confidence = 0)\n" +
			"    Choice[3] = ID: 2mcwQKiD8VEspmMJpL1dc7okQQ5dDVAWeCBZ7FWBFAbxpv3t7w SB(NumSuccessfulPolls = 1, Confidence = 1)\n" +
			")"
		if str := graph.String(); str != expected {
			t.Fatalf("Expected %s, got %s", expected, str)
		}
	}

	prefs = graph.Preferences()
	switch {
	case prefs.Len() != 2:
		t.Fatalf("Wrong number of preferences.")
	case !prefs.Contains(Red.ID()):
		t.Fatalf("Wrong preference. Expected %s", Red.ID())
	case !prefs.Contains(Blue.ID()):
		t.Fatalf("Wrong preference. Expected %s", Blue.ID())
	case graph.Finalized():
		t.Fatalf("Finalized too early")
	}

	ga := ids.Bag{}
	ga.SetThreshold(2)
	ga.AddCount(Green.ID(), 2)
	ga.AddCount(Alpha.ID(), 2)
	if changed, err := graph.RecordPoll(ga); err != nil {
		t.Fatal(err)
	} else if changed {
		t.Fatalf("Shouldn't have caused the frontiers to recalculate")
	}

	{
		expected := prefix + "(\n" +
			"    Choice[0] = ID:  LUC1cmcxnfNR9LdkACS2ccGKLEK7SYqB4gLLTycQfg1koyfSq SB(NumSuccessfulPolls = 1, Confidence = 0)\n" +
			"    Choice[1] = ID:  TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES SB(NumSuccessfulPolls = 1, Confidence = 1)\n" +
			"    Choice[2] = ID:  Zda4gsqTjRaX6XVZekVNi3ovMFPHDRQiGbzYuAb7Nwqy1rGBc SB(NumSuccessfulPolls = 1, Confidence = 1)\n" +
			"    Choice[3] = ID: 2mcwQKiD8VEspmMJpL1dc7okQQ5dDVAWeCBZ7FWBFAbxpv3t7w SB(NumSuccessfulPolls = 1, Confidence = 0)\n" +
			")"
		if str := graph.String(); str != expected {
			t.Fatalf("Expected %s, got %s", expected, str)
		}
	}

	prefs = graph.Preferences()
	switch {
	case prefs.Len() != 2:
		t.Fatalf("Wrong number of preferences.")
	case !prefs.Contains(Red.ID()):
		t.Fatalf("Wrong preference. Expected %s", Red.ID())
	case !prefs.Contains(Blue.ID()):
		t.Fatalf("Wrong preference. Expected %s", Blue.ID())
	case graph.Finalized():
		t.Fatalf("Finalized too early")
	}

	empty := ids.Bag{}
	if changed, err := graph.RecordPoll(empty); err != nil {
		t.Fatal(err)
	} else if changed {
		t.Fatalf("Shouldn't have caused the frontiers to recalculate")
	}

	{
		expected := prefix + "(\n" +
			"    Choice[0] = ID:  LUC1cmcxnfNR9LdkACS2ccGKLEK7SYqB4gLLTycQfg1koyfSq SB(NumSuccessfulPolls = 1, Confidence = 0)\n" +
			"    Choice[1] = ID:  TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES SB(NumSuccessfulPolls = 1, Confidence = 0)\n" +
			"    Choice[2] = ID:  Zda4gsqTjRaX6XVZekVNi3ovMFPHDRQiGbzYuAb7Nwqy1rGBc SB(NumSuccessfulPolls = 1, Confidence = 0)\n" +
			"    Choice[3] = ID: 2mcwQKiD8VEspmMJpL1dc7okQQ5dDVAWeCBZ7FWBFAbxpv3t7w SB(NumSuccessfulPolls = 1, Confidence = 0)\n" +
			")"
		if str := graph.String(); str != expected {
			t.Fatalf("Expected %s, got %s", expected, str)
		}
	}

	prefs = graph.Preferences()
	switch {
	case prefs.Len() != 2:
		t.Fatalf("Wrong number of preferences.")
	case !prefs.Contains(Red.ID()):
		t.Fatalf("Wrong preference. Expected %s", Red.ID())
	case !prefs.Contains(Blue.ID()):
		t.Fatalf("Wrong preference. Expected %s", Blue.ID())
	case graph.Finalized():
		t.Fatalf("Finalized too early")
	}

	if changed, err := graph.RecordPoll(ga); err != nil {
		t.Fatal(err)
	} else if !changed {
		t.Fatalf("Should have caused the frontiers to recalculate")
	}

	{
		expected := prefix + "(\n" +
			"    Choice[0] = ID:  LUC1cmcxnfNR9LdkACS2ccGKLEK7SYqB4gLLTycQfg1koyfSq SB(NumSuccessfulPolls = 1, Confidence = 0)\n" +
			"    Choice[1] = ID:  TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES SB(NumSuccessfulPolls = 2, Confidence = 1)\n" +
			"    Choice[2] = ID:  Zda4gsqTjRaX6XVZekVNi3ovMFPHDRQiGbzYuAb7Nwqy1rGBc SB(NumSuccessfulPolls = 2, Confidence = 1)\n" +
			"    Choice[3] = ID: 2mcwQKiD8VEspmMJpL1dc7okQQ5dDVAWeCBZ7FWBFAbxpv3t7w SB(NumSuccessfulPolls = 1, Confidence = 0)\n" +
			")"
		if str := graph.String(); str != expected {
			t.Fatalf("Expected %s, got %s", expected, str)
		}
	}

	prefs = graph.Preferences()
	switch {
	case prefs.Len() != 2:
		t.Fatalf("Wrong number of preferences.")
	case !prefs.Contains(Green.ID()):
		t.Fatalf("Wrong preference. Expected %s", Green.ID())
	case !prefs.Contains(Alpha.ID()):
		t.Fatalf("Wrong preference. Expected %s", Alpha.ID())
	case graph.Finalized():
		t.Fatalf("Finalized too early")
	}

	if changed, err := graph.RecordPoll(ga); err != nil {
		t.Fatal(err)
	} else if !changed {
		t.Fatalf("Should have caused the frontiers to recalculate")
	}

	{
		expected := prefix + "()"
		if str := graph.String(); str != expected {
			t.Fatalf("Expected %s, got %s", expected, str)
		}
	}

	prefs = graph.Preferences()
	switch {
	case prefs.Len() != 0:
		t.Fatalf("Wrong number of preferences.")
	case !graph.Finalized():
		t.Fatalf("Finalized too late")
	case Green.Status() != choices.Accepted:
		t.Fatalf("%s should have been accepted", Green.ID())
	case Alpha.Status() != choices.Accepted:
		t.Fatalf("%s should have been accepted", Alpha.ID())
	case Red.Status() != choices.Rejected:
		t.Fatalf("%s should have been rejected", Red.ID())
	case Blue.Status() != choices.Rejected:
		t.Fatalf("%s should have been rejected", Blue.ID())
	}

	if changed, err := graph.RecordPoll(rb); err != nil {
		t.Fatal(err)
	} else if changed {
		t.Fatalf("Shouldn't have caused the frontiers to recalculate")
	}

	{
		expected := prefix + "()"
		if str := graph.String(); str != expected {
			t.Fatalf("Expected %s, got %s", expected, str)
		}
	}

	prefs = graph.Preferences()
	switch {
	case prefs.Len() != 0:
		t.Fatalf("Wrong number of preferences.")
	case !graph.Finalized():
		t.Fatalf("Finalized too late")
	case Green.Status() != choices.Accepted:
		t.Fatalf("%s should have been accepted", Green.ID())
	case Alpha.Status() != choices.Accepted:
		t.Fatalf("%s should have been accepted", Alpha.ID())
	case Red.Status() != choices.Rejected:
		t.Fatalf("%s should have been rejected", Red.ID())
	case Blue.Status() != choices.Rejected:
		t.Fatalf("%s should have been rejected", Blue.ID())
	}
}
