// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"errors"
	"math"
	"path"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
)

type testFunc func(*testing.T, Factory)

var testFuncs = []testFunc{
	MetricsTest,
	ParamsTest,
	NumProcessingTest,
	AddTest,
	VertexIssuedTest,
	TxIssuedTest,
	VirtuousTest,
	VirtuousSkippedUpdateTest,
	VotingTest,
	IgnoreInvalidVotingTest,
	IgnoreInvalidTransactionVertexVotingTest,
	TransitiveVotingTest,
	SplitVotingTest,
	TransitiveRejectionTest,
	IsVirtuousTest,
	QuiesceTest,
	QuiesceAfterVotingTest,
	TransactionVertexTest,
	OrphansTest,
	OrphansUpdateTest,
	ErrorOnVacuousAcceptTest,
	ErrorOnTxAcceptTest,
	ErrorOnVtxAcceptTest,
	ErrorOnVtxRejectTest,
	ErrorOnParentVtxRejectTest,
	ErrorOnTransitiveVtxRejectTest,
	SilenceTransactionVertexEventsTest,
}

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

func MetricsTest(t *testing.T, factory Factory) {
	ctx := snow.DefaultConsensusContextTest()

	{
		avl := factory.New()
		params := Parameters{
			Parameters: snowball.Parameters{
				K:                 2,
				Alpha:             2,
				BetaVirtuous:      1,
				BetaRogue:         2,
				ConcurrentRepolls: 1,
				OptimalProcessing: 1,
			},
			Parents:   2,
			BatchSize: 1,
		}
		err := ctx.Registerer.Register(prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "vtx_processing",
		}))
		if err != nil {
			t.Fatal(err)
		}
		if err := avl.Initialize(ctx, params, nil); err == nil {
			t.Fatalf("should have failed due to registering a duplicated statistic")
		}
	}
	{
		avl := factory.New()
		params := Parameters{
			Parameters: snowball.Parameters{
				K:                 2,
				Alpha:             2,
				BetaVirtuous:      1,
				BetaRogue:         2,
				ConcurrentRepolls: 1,
				OptimalProcessing: 1,
			},
			Parents:   2,
			BatchSize: 1,
		}
		err := ctx.Registerer.Register(prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "vtx_accepted",
		}))
		if err != nil {
			t.Fatal(err)
		}
		if err := avl.Initialize(ctx, params, nil); err == nil {
			t.Fatalf("should have failed due to registering a duplicated statistic")
		}
	}
	{
		avl := factory.New()
		params := Parameters{
			Parameters: snowball.Parameters{
				K:                 2,
				Alpha:             2,
				BetaVirtuous:      1,
				BetaRogue:         2,
				ConcurrentRepolls: 1,
				OptimalProcessing: 1,
			},
			Parents:   2,
			BatchSize: 1,
		}
		err := ctx.Registerer.Register(prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "vtx_rejected",
		}))
		if err != nil {
			t.Fatal(err)
		}
		if err := avl.Initialize(ctx, params, nil); err == nil {
			t.Fatalf("should have failed due to registering a duplicated statistic")
		}
	}
}

func ParamsTest(t *testing.T, factory Factory) {
	avl := factory.New()

	ctx := snow.DefaultConsensusContextTest()
	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     2,
			Alpha:                 2,
			BetaVirtuous:          1,
			BetaRogue:             2,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}

	if err := avl.Initialize(ctx, params, nil); err != nil {
		t.Fatal(err)
	}

	p := avl.Parameters()
	switch {
	case p.K != params.K:
		t.Fatalf("Wrong K parameter")
	case p.Alpha != params.Alpha:
		t.Fatalf("Wrong Alpha parameter")
	case p.BetaVirtuous != params.BetaVirtuous:
		t.Fatalf("Wrong Beta1 parameter")
	case p.BetaRogue != params.BetaRogue:
		t.Fatalf("Wrong Beta2 parameter")
	case p.Parents != params.Parents:
		t.Fatalf("Wrong Parents parameter")
	}
}

func NumProcessingTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     1,
			Alpha:                 1,
			BetaVirtuous:          1,
			BetaRogue:             1,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
	}
	utxos := []ids.ID{ids.GenerateTestID()}

	if err := avl.Initialize(snow.DefaultConsensusContextTest(), params, vts); err != nil {
		t.Fatal(err)
	}

	if numProcessing := avl.NumProcessing(); numProcessing != 0 {
		t.Fatalf("expected %d vertices processing but returned %d", 0, numProcessing)
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	}

	if numProcessing := avl.NumProcessing(); numProcessing != 1 {
		t.Fatalf("expected %d vertices processing but returned %d", 1, numProcessing)
	}

	tx1 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[0])

	vtx1 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1},
	}

	if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	}

	if numProcessing := avl.NumProcessing(); numProcessing != 2 {
		t.Fatalf("expected %d vertices processing but returned %d", 2, numProcessing)
	}

	if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	}

	if numProcessing := avl.NumProcessing(); numProcessing != 2 {
		t.Fatalf("expected %d vertices processing but returned %d", 2, numProcessing)
	}

	if err := avl.Add(vts[0]); err != nil {
		t.Fatal(err)
	}

	if numProcessing := avl.NumProcessing(); numProcessing != 2 {
		t.Fatalf("expected %d vertices processing but returned %d", 2, numProcessing)
	}

	votes := ids.UniqueBag{}
	votes.Add(0, vtx0.ID())
	if err := avl.RecordPoll(votes); err != nil {
		t.Fatal(err)
	}

	if numProcessing := avl.NumProcessing(); numProcessing != 0 {
		t.Fatalf("expected %d vertices processing but returned %d", 0, numProcessing)
	}
}

func AddTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     2,
			Alpha:                 2,
			BetaVirtuous:          1,
			BetaRogue:             2,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}

	seedVertices := []Vertex{
		&TestVertex{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Accepted,
			},
		},
		&TestVertex{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Accepted,
			},
		},
	}

	ctx := snow.DefaultConsensusContextTest()
	// track consensus events to ensure idempotency in case of redundant vertex adds
	consensusEvents := snow.NewEventDispatcherTracker()
	ctx.ConsensusDispatcher = consensusEvents

	if err := avl.Initialize(ctx, params, seedVertices); err != nil {
		t.Fatal(err)
	}

	if !avl.Finalized() {
		t.Fatal("An empty avalanche instance is not finalized")
	}
	if !ids.UnsortedEquals([]ids.ID{seedVertices[0].ID(), seedVertices[1].ID()}, avl.Preferences().List()) {
		t.Fatal("Initial frontier failed to be set")
	}

	utxos := []ids.ID{ids.GenerateTestID()}
	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: seedVertices,
		HeightV:  1,
		TxsV: []snowstorm.Tx{
			&snowstorm.TestTx{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				InputIDsV: utxos,
			},
		},
	}
	vtx1 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: seedVertices,
		HeightV:  1,
		TxsV: []snowstorm.Tx{
			&snowstorm.TestTx{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				InputIDsV: utxos,
			},
		},
	}

	tt := []struct {
		toAdd         Vertex
		err           error
		finalized     bool
		preferenceSet []ids.ID
		issued        int
		accepted      int
	}{
		{
			toAdd:         vtx0,
			err:           nil,
			finalized:     false,
			preferenceSet: []ids.ID{vtx0.IDV},
			issued:        1, // on "add", it should be issued
			accepted:      0,
		},
		{
			toAdd:         vtx1,
			err:           nil,
			finalized:     false,
			preferenceSet: []ids.ID{vtx0.IDV},
			issued:        1, // on "add", it should be issued
			accepted:      0,
		},
		{
			toAdd:         seedVertices[0],
			err:           nil,
			finalized:     false,
			preferenceSet: []ids.ID{vtx0.IDV},
			issued:        0, // initialized vertex should not belong to in-processing nodes
			accepted:      0,
		},
	}
	for i, tv := range tt {
		for _, j := range []int{1, 2} { // duplicate vertex add should be skipped
			err := avl.Add(tv.toAdd)
			if err != tv.err {
				t.Fatalf("#%d-%d: expected error %v, got %v", i, j, tv.err, err)
			}
			finalized := avl.Finalized()
			if finalized != tv.finalized {
				t.Fatalf("#%d-%d: expected finalized %v, got %v", i, j, finalized, tv.finalized)
			}
			preferenceSet := avl.Preferences().List()
			if !ids.UnsortedEquals(tv.preferenceSet, preferenceSet) {
				t.Fatalf("#%d-%d: expected preferenceSet %v, got %v", i, j, preferenceSet, tv.preferenceSet)
			}
			if issued, _ := consensusEvents.IsIssued(tv.toAdd.ID()); issued != tv.issued {
				t.Fatalf("#%d-%d: expected issued %d, got %d", i, j, tv.issued, issued)
			}
			if accepted, _ := consensusEvents.IsAccepted(tv.toAdd.ID()); accepted != tv.accepted {
				t.Fatalf("#%d-%d: expected accepted %d, got %d", i, j, tv.accepted, accepted)
			}
		}
	}
}

func VertexIssuedTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     2,
			Alpha:                 2,
			BetaVirtuous:          1,
			BetaRogue:             2,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
	}
	utxos := []ids.ID{ids.GenerateTestID()}

	if err := avl.Initialize(snow.DefaultConsensusContextTest(), params, vts); err != nil {
		t.Fatal(err)
	}

	if !avl.VertexIssued(vts[0]) {
		t.Fatalf("Genesis Vertex not reported as issued")
	}

	tx := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx.InputIDsV = append(tx.InputIDsV, utxos[0])

	vtx := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx},
	}

	if avl.VertexIssued(vtx) {
		t.Fatalf("Vertex reported as issued")
	} else if err := avl.Add(vtx); err != nil {
		t.Fatal(err)
	} else if !avl.VertexIssued(vtx) {
		t.Fatalf("Vertex reported as not issued")
	}
}

func TxIssuedTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     2,
			Alpha:                 2,
			BetaVirtuous:          1,
			BetaRogue:             2,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	vts := []Vertex{&TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		},
		TxsV: []snowstorm.Tx{tx0},
	}}
	utxos := []ids.ID{ids.GenerateTestID()}

	tx1 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[0])

	if err := avl.Initialize(snow.DefaultConsensusContextTest(), params, vts); err != nil {
		t.Fatal(err)
	}

	if !avl.TxIssued(tx0) {
		t.Fatalf("Genesis Tx not reported as issued")
	} else if avl.TxIssued(tx1) {
		t.Fatalf("Tx reported as issued")
	}

	vtx := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		HeightV: 1,
		TxsV:    []snowstorm.Tx{tx1},
	}

	if err := avl.Add(vtx); err != nil {
		t.Fatal(err)
	} else if !avl.TxIssued(tx1) {
		t.Fatalf("Tx reported as not issued")
	}
}

func VirtuousTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     2,
			Alpha:                 2,
			BetaVirtuous:          10,
			BetaRogue:             20,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
	}
	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	err := avl.Initialize(snow.DefaultConsensusContextTest(), params, vts)
	if err != nil {
		t.Fatal(err)
	}

	virtuous := avl.Virtuous()
	switch {
	case virtuous.Len() != 2:
		t.Fatalf("Wrong number of virtuous.")
	case !virtuous.Contains(vts[0].ID()):
		t.Fatalf("Wrong virtuous")
	case !virtuous.Contains(vts[1].ID()):
		t.Fatalf("Wrong virtuous")
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	tx1 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[0])

	vtx1 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1},
	}

	tx2 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx2.InputIDsV = append(tx2.InputIDsV, utxos[1])

	vtx2 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []Vertex{vtx0},
		HeightV:  2,
		TxsV:     []snowstorm.Tx{tx2},
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	} else if virtuous := avl.Virtuous(); virtuous.Len() != 1 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vtx0.IDV) {
		t.Fatalf("Wrong virtuous")
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	} else if virtuous := avl.Virtuous(); virtuous.Len() != 1 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vtx0.IDV) {
		t.Fatalf("Wrong virtuous")
	}

	votes := ids.UniqueBag{}
	votes.Add(0, vtx1.ID())
	votes.Add(1, vtx1.ID())

	if err := avl.RecordPoll(votes); err != nil {
		t.Fatal(err)
	}

	virtuous = avl.Virtuous()
	switch {
	case virtuous.Len() != 2:
		t.Fatalf("Wrong number of virtuous.")
	case !virtuous.Contains(vts[0].ID()):
		t.Fatalf("Wrong virtuous")
	case !virtuous.Contains(vts[1].ID()):
		t.Fatalf("Wrong virtuous")
	}

	if err := avl.Add(vtx2); err != nil {
		t.Fatal(err)
	}

	virtuous = avl.Virtuous()
	switch {
	case virtuous.Len() != 2:
		t.Fatalf("Wrong number of virtuous.")
	case !virtuous.Contains(vts[0].ID()):
		t.Fatalf("Wrong virtuous")
	case !virtuous.Contains(vts[1].ID()):
		t.Fatalf("Wrong virtuous")
	}

	if err := avl.RecordPoll(votes); err != nil {
		t.Fatal(err)
	}

	virtuous = avl.Virtuous()
	switch {
	case virtuous.Len() != 2:
		t.Fatalf("Wrong number of virtuous.")
	case !virtuous.Contains(vts[0].ID()):
		t.Fatalf("Wrong virtuous")
	case !virtuous.Contains(vts[1].ID()):
		t.Fatalf("Wrong virtuous")
	}
}

func VirtuousSkippedUpdateTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     2,
			Alpha:                 2,
			BetaVirtuous:          10,
			BetaRogue:             20,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
	}
	utxos := []ids.ID{
		ids.GenerateTestID(),
		ids.GenerateTestID(),
	}

	err := avl.Initialize(snow.DefaultConsensusContextTest(), params, vts)
	if err != nil {
		t.Fatal(err)
	}

	virtuous := avl.Virtuous()
	switch {
	case virtuous.Len() != 2:
		t.Fatalf("Wrong number of virtuous.")
	case !virtuous.Contains(vts[0].ID()):
		t.Fatalf("Wrong virtuous")
	case !virtuous.Contains(vts[1].ID()):
		t.Fatalf("Wrong virtuous")
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	tx1 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[0])

	vtx1 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1},
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	} else if virtuous := avl.Virtuous(); virtuous.Len() != 1 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vtx0.IDV) {
		t.Fatalf("Wrong virtuous")
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	} else if virtuous := avl.Virtuous(); virtuous.Len() != 1 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vtx0.IDV) {
		t.Fatalf("Wrong virtuous")
	} else if err := avl.RecordPoll(ids.UniqueBag{}); err != nil {
		t.Fatal(err)
	} else if virtuous := avl.Virtuous(); virtuous.Len() != 1 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vtx0.IDV) {
		t.Fatalf("Wrong virtuous")
	}
}

// Creates two conflicting transactions in different vertices
// and make sure only one is accepted
func VotingTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     2,
			Alpha:                 2,
			BetaVirtuous:          1,
			BetaRogue:             2,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
	}
	utxos := []ids.ID{ids.GenerateTestID()}

	err := avl.Initialize(snow.DefaultConsensusContextTest(), params, vts)
	if err != nil {
		t.Fatal(err)
	}

	// create two different transactions with the same input UTXO (double-spend)
	tx0 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: []ids.ID{utxos[0]},
	}
	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: []ids.ID{utxos[0]},
	}

	// put them in different vertices
	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}
	vtx1 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1},
	}

	// issue two vertices with conflicting transaction to the consensus instance
	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	}
	if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	}

	// create poll results, all vote for vtx1, not for vtx0
	sm := ids.UniqueBag{}
	sm.Add(0, vtx1.IDV)
	sm.Add(1, vtx1.IDV)

	// "BetaRogue" is 2, thus consensus should not be finalized yet
	err = avl.RecordPoll(sm)
	switch {
	case err != nil:
		t.Fatal(err)
	case avl.Finalized():
		t.Fatalf("An avalanche instance finalized too early")
	case !ids.UnsortedEquals([]ids.ID{vtx1.IDV}, avl.Preferences().List()):
		t.Fatalf("Initial frontier failed to be set")
	case tx0.Status() != choices.Processing:
		t.Fatalf("Tx should have been Processing")
	case tx1.Status() != choices.Processing:
		t.Fatalf("Tx should have been Processing")
	}

	// second poll should reach consensus,
	// and the other vertex of conflict transaction should be rejected
	err = avl.RecordPoll(sm)
	switch {
	case err != nil:
		t.Fatal(err)
	case !avl.Finalized():
		t.Fatalf("An avalanche instance finalized too late")
	case !ids.UnsortedEquals([]ids.ID{vtx1.IDV}, avl.Preferences().List()):
		// rejected vertex ID (vtx0) must have been removed from the preferred set
		t.Fatalf("Initial frontier failed to be set")
	case tx0.Status() != choices.Rejected:
		t.Fatalf("Tx should have been rejected")
	case tx1.Status() != choices.Accepted:
		t.Fatalf("Tx should have been accepted")
	}
}

func IgnoreInvalidVotingTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     3,
			Alpha:                 2,
			BetaVirtuous:          1,
			BetaRogue:             1,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}

	vts := []Vertex{
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
	}
	utxos := []ids.ID{ids.GenerateTestID()}

	if err := avl.Initialize(snow.DefaultConsensusContextTest(), params, vts); err != nil {
		t.Fatal(err)
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	tx1 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[0])

	vtx1 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1},
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	}

	sm := ids.UniqueBag{}
	sm.Add(0, vtx0.IDV)
	sm.Add(1, vtx1.IDV)

	// Add Illegal Vote cast by Response 2
	sm.Add(2, vtx0.IDV)
	sm.Add(2, vtx1.IDV)

	if err := avl.RecordPoll(sm); err != nil {
		t.Fatal(err)
	} else if avl.Finalized() {
		t.Fatalf("An avalanche instance finalized too early")
	}
}

func IgnoreInvalidTransactionVertexVotingTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     3,
			Alpha:                 2,
			BetaVirtuous:          1,
			BetaRogue:             1,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}

	vts := []Vertex{
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
	}

	if err := avl.Initialize(snow.DefaultConsensusContextTest(), params, vts); err != nil {
		t.Fatal(err)
	}

	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
	}

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: []ids.ID{vtx0.ID()},
	}

	vtx1 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1},
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	}

	sm := ids.UniqueBag{}
	sm.Add(0, vtx0.IDV)
	sm.Add(1, vtx1.IDV)

	// Add Illegal Vote cast by Response 2
	sm.Add(2, vtx0.IDV)
	sm.Add(2, vtx1.IDV)

	if err := avl.RecordPoll(sm); err != nil {
		t.Fatal(err)
	} else if avl.Finalized() {
		t.Fatalf("An avalanche instance finalized too early")
	}
}

func TransitiveVotingTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     2,
			Alpha:                 2,
			BetaVirtuous:          1,
			BetaRogue:             2,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
	}
	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	err := avl.Initialize(snow.DefaultConsensusContextTest(), params, vts)
	if err != nil {
		t.Fatal(err)
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	tx1 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[1])

	vtx1 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []Vertex{vtx0},
		HeightV:  2,
		TxsV:     []snowstorm.Tx{tx1},
	}

	vtx2 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []Vertex{vtx1},
		HeightV:  3,
		TxsV:     []snowstorm.Tx{tx1},
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx2); err != nil {
		t.Fatal(err)
	}

	sm1 := ids.UniqueBag{}
	sm1.Add(0, vtx0.IDV)
	sm1.Add(1, vtx2.IDV)

	err = avl.RecordPoll(sm1)
	switch {
	case err != nil:
		t.Fatal(err)
	case avl.Finalized():
		t.Fatalf("An avalanche instance finalized too early")
	case !ids.UnsortedEquals([]ids.ID{vtx2.IDV}, avl.Preferences().List()):
		t.Fatalf("Initial frontier failed to be set")
	case tx0.Status() != choices.Accepted:
		t.Fatalf("Tx should have been accepted")
	}

	sm2 := ids.UniqueBag{}
	sm2.Add(0, vtx2.IDV)
	sm2.Add(1, vtx2.IDV)

	err = avl.RecordPoll(sm2)
	switch {
	case err != nil:
		t.Fatal(err)
	case !avl.Finalized():
		t.Fatalf("An avalanche instance finalized too late")
	case !ids.UnsortedEquals([]ids.ID{vtx2.IDV}, avl.Preferences().List()):
		t.Fatalf("Initial frontier failed to be set")
	case tx0.Status() != choices.Accepted:
		t.Fatalf("Tx should have been accepted")
	case tx1.Status() != choices.Accepted:
		t.Fatalf("Tx should have been accepted")
	}
}

func SplitVotingTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     2,
			Alpha:                 2,
			BetaVirtuous:          1,
			BetaRogue:             2,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
	}
	utxos := []ids.ID{ids.GenerateTestID()}

	err := avl.Initialize(snow.DefaultConsensusContextTest(), params, vts)
	if err != nil {
		t.Fatal(err)
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	vtx1 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	}

	sm1 := ids.UniqueBag{}
	sm1.Add(0, vtx0.IDV) // peer 0 votes for the tx though vtx0
	sm1.Add(1, vtx1.IDV) // peer 1 votes for the tx though vtx1

	err = avl.RecordPoll(sm1)
	switch {
	case err != nil:
		t.Fatal(err)
	case avl.Finalized(): // avalanche shouldn't be finalized because the vertex transactions are still processing
		t.Fatalf("An avalanche instance finalized too late")
	case !ids.UnsortedEquals([]ids.ID{vtx0.IDV, vtx1.IDV}, avl.Preferences().List()):
		t.Fatalf("Initial frontier failed to be set")
	case tx0.Status() != choices.Accepted:
		t.Fatalf("Tx should have been accepted")
	}

	// Give alpha votes for both tranaction vertices
	sm2 := ids.UniqueBag{}
	sm2.Add(0, vtx0.IDV, vtx1.IDV) // peer 0 votes for vtx0 and vtx1
	sm2.Add(1, vtx0.IDV, vtx1.IDV) // peer 1 votes for vtx0 and vtx1

	err = avl.RecordPoll(sm2)
	switch {
	case err != nil:
		t.Fatal(err)
	case !avl.Finalized():
		t.Fatalf("An avalanche instance finalized too late")
	case !ids.UnsortedEquals([]ids.ID{vtx0.IDV, vtx1.IDV}, avl.Preferences().List()):
		t.Fatalf("Initial frontier failed to be set")
	case tx0.Status() != choices.Accepted:
		t.Fatalf("Tx should have been accepted")
	}
}

func TransitiveRejectionTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     2,
			Alpha:                 2,
			BetaVirtuous:          1,
			BetaRogue:             2,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
	}
	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	err := avl.Initialize(snow.DefaultConsensusContextTest(), params, vts)
	if err != nil {
		t.Fatal(err)
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	tx1 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[0])

	vtx1 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1},
	}

	tx2 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx2.InputIDsV = append(tx2.InputIDsV, utxos[1])

	vtx2 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []Vertex{vtx0},
		HeightV:  2,
		TxsV:     []snowstorm.Tx{tx2},
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx2); err != nil {
		t.Fatal(err)
	}

	sm := ids.UniqueBag{}
	sm.Add(0, vtx1.IDV)
	sm.Add(1, vtx1.IDV)

	err = avl.RecordPoll(sm)
	switch {
	case err != nil:
		t.Fatal(err)
	case avl.Finalized():
		t.Fatalf("An avalanche instance finalized too early")
	case !ids.UnsortedEquals([]ids.ID{vtx1.IDV}, avl.Preferences().List()):
		t.Fatalf("Initial frontier failed to be set")
	}

	err = avl.RecordPoll(sm)
	switch {
	case err != nil:
		t.Fatal(err)
	case avl.Finalized():
		t.Fatalf("An avalanche instance finalized too early")
	case !ids.UnsortedEquals([]ids.ID{vtx1.IDV}, avl.Preferences().List()):
		t.Fatalf("Initial frontier failed to be set")
	case tx0.Status() != choices.Rejected:
		t.Fatalf("Tx should have been rejected")
	case tx1.Status() != choices.Accepted:
		t.Fatalf("Tx should have been accepted")
	case tx2.Status() != choices.Processing:
		t.Fatalf("Tx should not have been decided")
	}

	err = avl.RecordPoll(sm)
	switch {
	case err != nil:
		t.Fatal(err)
	case avl.Finalized():
		t.Fatalf("An avalanche instance finalized too early")
	case !ids.UnsortedEquals([]ids.ID{vtx1.IDV}, avl.Preferences().List()):
		t.Fatalf("Initial frontier failed to be set")
	case tx0.Status() != choices.Rejected:
		t.Fatalf("Tx should have been rejected")
	case tx1.Status() != choices.Accepted:
		t.Fatalf("Tx should have been accepted")
	case tx2.Status() != choices.Processing:
		t.Fatalf("Tx should not have been decided")
	}
}

func IsVirtuousTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     2,
			Alpha:                 2,
			BetaVirtuous:          1,
			BetaRogue:             2,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
	}
	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	err := avl.Initialize(snow.DefaultConsensusContextTest(), params, vts)
	if err != nil {
		t.Fatal(err)
	}

	virtuous := avl.Virtuous()
	switch {
	case virtuous.Len() != 2:
		t.Fatalf("Wrong number of virtuous.")
	case !virtuous.Contains(vts[0].ID()):
		t.Fatalf("Wrong virtuous")
	case !virtuous.Contains(vts[1].ID()):
		t.Fatalf("Wrong virtuous")
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	tx1 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[0])

	vtx1 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1},
	}

	if !avl.IsVirtuous(tx0) {
		t.Fatalf("Should be virtuous.")
	} else if !avl.IsVirtuous(tx1) {
		t.Fatalf("Should be virtuous.")
	}

	err = avl.Add(vtx0)
	switch {
	case err != nil:
		t.Fatal(err)
	case !avl.IsVirtuous(tx0):
		t.Fatalf("Should be virtuous.")
	case avl.IsVirtuous(tx1):
		t.Fatalf("Should not be virtuous.")
	}

	err = avl.Add(vtx1)
	switch {
	case err != nil:
		t.Fatal(err)
	case avl.IsVirtuous(tx0):
		t.Fatalf("Should not be virtuous.")
	case avl.IsVirtuous(tx1):
		t.Fatalf("Should not be virtuous.")
	}
}

func QuiesceTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     1,
			Alpha:                 1,
			BetaVirtuous:          1,
			BetaRogue:             2,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
	}
	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	err := avl.Initialize(snow.DefaultConsensusContextTest(), params, vts)
	if err != nil {
		t.Fatal(err)
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	tx1 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[0])

	vtx1 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1},
	}

	tx2 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx2.InputIDsV = append(tx2.InputIDsV, utxos[1])

	vtx2 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx2},
	}

	// Add [vtx0] containing [tx0]. Because [tx0] is virtuous, the instance
	// shouldn't quiesce.
	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	}
	if avl.Quiesce() {
		t.Fatalf("Shouldn't quiesce")
	}

	// Add [vtx1] containing [tx1]. Because [tx1] conflicts with [tx0], neither
	// [tx0] nor [tx1] are now virtuous. This means there are no virtuous
	// transaction left in the consensus instance and it can quiesce.
	if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	}

	// The virtuous frontier is only updated sometimes, so force the frontier to
	// be re-calculated by changing the preference of tx1.
	sm1 := ids.UniqueBag{}
	sm1.Add(0, vtx1.IDV)
	if err := avl.RecordPoll(sm1); err != nil {
		t.Fatal(err)
	}

	if !avl.Quiesce() {
		t.Fatalf("Should quiesce")
	}

	// Add [vtx2] containing [tx2]. Because [tx2] is virtuous, the instance
	// shouldn't quiesce, even though [tx0] and [tx1] conflict.
	if err := avl.Add(vtx2); err != nil {
		t.Fatal(err)
	}
	if avl.Quiesce() {
		t.Fatalf("Shouldn't quiesce")
	}

	sm2 := ids.UniqueBag{}
	sm2.Add(0, vtx2.IDV)
	if err := avl.RecordPoll(sm2); err != nil {
		t.Fatal(err)
	}

	// Because [tx2] was accepted, there is again no remaining virtuous
	// transactions left in consensus.
	if !avl.Quiesce() {
		t.Fatalf("Should quiesce")
	}
}

func QuiesceAfterVotingTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     1,
			Alpha:                 1,
			BetaVirtuous:          2,
			BetaRogue:             2,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
	}
	utxos := []ids.ID{
		ids.GenerateTestID(),
		ids.GenerateTestID(),
	}

	err := avl.Initialize(snow.DefaultConsensusContextTest(), params, vts)
	if err != nil {
		t.Fatal(err)
	}

	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV: []snowstorm.Tx{
			&snowstorm.TestTx{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				InputIDsV: utxos[:1],
			},
		},
	}
	vtx1 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV: []snowstorm.Tx{
			&snowstorm.TestTx{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				InputIDsV: utxos[:1],
			},
		},
	}
	vtx2 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV: []snowstorm.Tx{
			&snowstorm.TestTx{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				InputIDsV: utxos[1:],
			},
		},
	}
	vtx3 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{},
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	}
	if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	}
	if err := avl.Add(vtx2); err != nil {
		t.Fatal(err)
	}
	if err := avl.Add(vtx3); err != nil {
		t.Fatal(err)
	}

	// Because [vtx2] and [vtx3] are virtuous, the instance shouldn't quiesce.
	if avl.Quiesce() {
		t.Fatalf("Shouldn't quiesce")
	}

	sm12 := ids.UniqueBag{}
	sm12.Add(0, vtx1.IDV, vtx2.IDV)
	if err := avl.RecordPoll(sm12); err != nil {
		t.Fatal(err)
	}

	// Because [vtx2] and [vtx3] are still processing, the instance shouldn't
	// quiesce.
	if avl.Quiesce() {
		t.Fatalf("Shouldn't quiesce")
	}

	sm023 := ids.UniqueBag{}
	sm023.Add(0, vtx0.IDV, vtx2.IDV, vtx3.IDV)
	if err := avl.RecordPoll(sm023); err != nil {
		t.Fatal(err)
	}

	// Because [vtx3] is still processing, the instance shouldn't quiesce.
	if avl.Quiesce() {
		t.Fatalf("Shouldn't quiesce")
	}

	sm3 := ids.UniqueBag{}
	sm3.Add(0, vtx3.IDV)
	if err := avl.RecordPoll(sm3); err != nil {
		t.Fatal(err)
	}

	// Because [vtx0] and [vtx1] are conflicting and [vtx2] and [vtx3] are
	// accepted, the instance can quiesce.
	if !avl.Quiesce() {
		t.Fatalf("Should quiesce")
	}
}

func TransactionVertexTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     1,
			Alpha:                 1,
			BetaVirtuous:          1,
			BetaRogue:             1,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	seedVertices := []Vertex{
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
	}

	err := avl.Initialize(snow.DefaultConsensusContextTest(), params, seedVertices)
	if err != nil {
		t.Fatal(err)
	}

	// Add a vertex with no transactions to test that the transaction vertex is
	// required to be accepted before the vertex is.
	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: seedVertices,
		HeightV:  1,
	}
	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	}
	if !avl.VertexIssued(vtx0) {
		t.Fatal("vertex with no transaction must have been issued")
	}

	// Because the transaction vertex should be processing, the vertex should
	// still be processing.
	if vtx0.Status() != choices.Processing {
		t.Fatalf("vertex with no transaction should still be processing, got %v", vtx0.Status())
	}

	// After voting for the transaction vertex beta times, the vertex should
	// also be accepted.
	bags := ids.UniqueBag{}
	bags.Add(0, vtx0.IDV)
	bags.Add(1, vtx0.IDV)
	if err := avl.RecordPoll(bags); err != nil {
		t.Fatalf("unexpected RecordPoll error %v", err)
	}

	switch {
	case vtx0.Status() != choices.Accepted:
		t.Fatalf("vertex with no transaction should have been accepted after polling, got %v", vtx0.Status())
	case !avl.Finalized():
		t.Fatal("expected finalized avalanche instance")
	case !ids.UnsortedEquals([]ids.ID{vtx0.IDV}, avl.Preferences().List()):
		t.Fatalf("unexpected frontier %v", avl.Preferences().List())
	}
}

func OrphansTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     1,
			Alpha:                 1,
			BetaVirtuous:          math.MaxInt32,
			BetaRogue:             math.MaxInt32,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
	}
	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	err := avl.Initialize(snow.DefaultConsensusContextTest(), params, vts)
	if err != nil {
		t.Fatal(err)
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	tx1 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[0])

	vtx1 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1},
	}

	tx2 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx2.InputIDsV = append(tx2.InputIDsV, utxos[1])

	vtx2 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []Vertex{vtx0},
		HeightV:  2,
		TxsV:     []snowstorm.Tx{tx2},
	}

	// [vtx0] contains [tx0], both of which will be preferred, so [tx0] is not
	// an orphan.
	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	}
	if orphans := avl.Orphans(); orphans.Len() != 0 {
		t.Fatalf("Wrong number of orphans")
	}

	// [vtx1] contains [tx1], which conflicts with [tx0]. [tx0] is contained in
	// a preferred vertex, and neither [tx0] nor [tx1] are virtuous.
	if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	}
	if orphans := avl.Orphans(); orphans.Len() != 0 {
		t.Fatalf("Wrong number of orphans")
	}

	// [vtx2] contains [tx2], both of which will be preferred, so [tx2] is not
	// an orphan.
	if err := avl.Add(vtx2); err != nil {
		t.Fatal(err)
	}
	if orphans := avl.Orphans(); orphans.Len() != 0 {
		t.Fatalf("Wrong number of orphans")
	}

	sm := ids.UniqueBag{}
	sm.Add(0, vtx1.IDV)
	if err := avl.RecordPoll(sm); err != nil {
		t.Fatal(err)
	}

	// By voting for [vtx1], [vtx2] is no longer preferred because it's parent
	// [vtx0] contains [tx0] that is not preferred. Because [tx2] is virtuous,
	// but no longer contained in a preferred vertex, it should now be
	// considered an orphan.
	orphans := avl.Orphans()
	if orphans.Len() != 1 {
		t.Fatalf("Wrong number of orphans")
	}
	if !orphans.Contains(tx2.ID()) {
		t.Fatalf("Wrong orphan")
	}
}

func OrphansUpdateTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     1,
			Alpha:                 1,
			BetaVirtuous:          math.MaxInt32,
			BetaRogue:             math.MaxInt32,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	seedVertices := []Vertex{
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
		&TestVertex{TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		}},
	}
	err := avl.Initialize(snow.DefaultConsensusContextTest(), params, seedVertices)
	if err != nil {
		t.Fatal(err)
	}

	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	// tx0 is a virtuous transaction.
	tx0 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: utxos[:1],
	}
	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: seedVertices,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	// tx1 conflicts with tx2.
	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: utxos[1:],
	}
	vtx1 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: seedVertices,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1},
	}

	tx2 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: utxos[1:],
	}
	vtx2 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: seedVertices,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx2},
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	}
	if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	}
	if err := avl.Add(vtx2); err != nil {
		t.Fatal(err)
	}

	// vtx0 is virtuous, so it should be preferred. vtx1 and vtx2 conflict, but
	// vtx1 was issued before vtx2, so vtx1 should be preferred and vtx2 should
	// not be preferred.
	expectedPreferredSet := ids.Set{
		vtx0.ID(): struct{}{},
		vtx1.ID(): struct{}{},
	}
	preferenceSet := avl.Preferences().List()
	if !ids.UnsortedEquals(expectedPreferredSet.List(), preferenceSet) {
		t.Fatalf("expected preferenceSet %v, got %v", expectedPreferredSet, preferenceSet)
	}

	// Record a successful poll to change the preference from vtx1 to vtx2 and
	// update the orphan set.
	votes := ids.UniqueBag{}
	votes.Add(0, vtx2.IDV)
	if err := avl.RecordPoll(votes); err != nil {
		t.Fatal(err)
	}

	// Because vtx2 was voted for over vtx1, they should be swapped in the
	// preferred set.
	expectedPreferredSet = ids.Set{
		vtx0.ID(): struct{}{},
		vtx2.ID(): struct{}{},
	}
	preferenceSet = avl.Preferences().List()
	if !ids.UnsortedEquals(expectedPreferredSet.List(), preferenceSet) {
		t.Fatalf("expected preferenceSet %v, got %v", expectedPreferredSet, preferenceSet)
	}

	// Because there are no virtuous transactions that are not in a preferred
	// vertex, there should be no orphans.
	expectedOrphanSet := ids.Set{}
	orphanSet := avl.Orphans()
	if !ids.UnsortedEquals(expectedOrphanSet.List(), orphanSet.List()) {
		t.Fatalf("expected orphanSet %v, got %v", expectedOrphanSet, orphanSet)
	}
}

func ErrorOnVacuousAcceptTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     1,
			Alpha:                 1,
			BetaVirtuous:          math.MaxInt32,
			BetaRogue:             math.MaxInt32,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{&TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}}

	err := avl.Initialize(snow.DefaultConsensusContextTest(), params, vts)
	if err != nil {
		t.Fatal(err)
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		AcceptV: errors.New(""),
		StatusV: choices.Processing,
	}}

	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	if err := avl.Add(vtx0); err == nil {
		t.Fatalf("Should have errored on vertex issuance")
	}
}

func ErrorOnTxAcceptTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     1,
			Alpha:                 1,
			BetaVirtuous:          1,
			BetaRogue:             1,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{&TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}}
	utxos := []ids.ID{ids.GenerateTestID()}

	err := avl.Initialize(snow.DefaultConsensusContextTest(), params, vts)
	if err != nil {
		t.Fatal(err)
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		AcceptV: errors.New(""),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	}

	votes := ids.UniqueBag{}
	votes.Add(0, vtx0.IDV)
	if err := avl.RecordPoll(votes); err == nil {
		t.Fatalf("Should have errored on vertex acceptance")
	}
}

func ErrorOnVtxAcceptTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     1,
			Alpha:                 1,
			BetaVirtuous:          1,
			BetaRogue:             1,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{&TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}}
	utxos := []ids.ID{ids.GenerateTestID()}

	err := avl.Initialize(snow.DefaultConsensusContextTest(), params, vts)
	if err != nil {
		t.Fatal(err)
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			AcceptV: errors.New(""),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	}

	votes := ids.UniqueBag{}
	votes.Add(0, vtx0.IDV)
	if err := avl.RecordPoll(votes); err == nil {
		t.Fatalf("Should have errored on vertex acceptance")
	}
}

func ErrorOnVtxRejectTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     1,
			Alpha:                 1,
			BetaVirtuous:          1,
			BetaRogue:             1,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{&TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}}
	utxos := []ids.ID{ids.GenerateTestID()}

	err := avl.Initialize(snow.DefaultConsensusContextTest(), params, vts)
	if err != nil {
		t.Fatal(err)
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	tx1 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[0])

	vtx1 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			RejectV: errors.New(""),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1},
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	}

	votes := ids.UniqueBag{}
	votes.Add(0, vtx0.IDV)
	if err := avl.RecordPoll(votes); err == nil {
		t.Fatalf("Should have errored on vertex rejection")
	}
}

func ErrorOnParentVtxRejectTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     1,
			Alpha:                 1,
			BetaVirtuous:          1,
			BetaRogue:             1,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{&TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}}
	utxos := []ids.ID{ids.GenerateTestID()}

	err := avl.Initialize(snow.DefaultConsensusContextTest(), params, vts)
	if err != nil {
		t.Fatal(err)
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	tx1 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[0])

	vtx1 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			RejectV: errors.New(""),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1},
	}

	vtx2 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []Vertex{vtx1},
		HeightV:  2,
		TxsV:     []snowstorm.Tx{tx1},
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx2); err != nil {
		t.Fatal(err)
	}

	votes := ids.UniqueBag{}
	votes.Add(0, vtx0.IDV)
	if err := avl.RecordPoll(votes); err == nil {
		t.Fatalf("Should have errored on vertex rejection")
	}
}

func ErrorOnTransitiveVtxRejectTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     1,
			Alpha:                 1,
			BetaVirtuous:          1,
			BetaRogue:             1,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{&TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}}
	utxos := []ids.ID{ids.GenerateTestID()}

	err := avl.Initialize(snow.DefaultConsensusContextTest(), params, vts)
	if err != nil {
		t.Fatal(err)
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	tx1 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[0])

	vtx1 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1},
	}

	vtx2 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			RejectV: errors.New(""),
			StatusV: choices.Processing,
		},
		ParentsV: []Vertex{vtx1},
		HeightV:  1,
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx2); err != nil {
		t.Fatal(err)
	}

	votes := ids.UniqueBag{}
	votes.Add(0, vtx0.IDV)
	if err := avl.RecordPoll(votes); err == nil {
		t.Fatalf("Should have errored on vertex rejection")
	}
}

func SilenceTransactionVertexEventsTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			K:                     1,
			Alpha:                 1,
			BetaVirtuous:          1,
			BetaRogue:             1,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{&TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}}

	ctx := snow.DefaultConsensusContextTest()
	tracker := snow.NewEventDispatcherTracker()
	ctx.DecisionDispatcher = tracker

	err := avl.Initialize(ctx, params, vts)
	if err != nil {
		t.Fatal(err)
	}

	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	}

	if _, issued := tracker.IsIssued(vtx0.ID()); issued {
		t.Fatalf("Shouldn't have reported the transaction vertex as issued")
	}

	votes := ids.UniqueBag{}
	votes.Add(0, vtx0.IDV)
	if err := avl.RecordPoll(votes); err != nil {
		t.Fatal(err)
	}

	if _, accepted := tracker.IsAccepted(vtx0.ID()); accepted {
		t.Fatalf("Shouldn't have reported the transaction vertex as accepted")
	}
}
