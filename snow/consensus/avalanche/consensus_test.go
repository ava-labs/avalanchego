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
	TransitiveVotingTest,
	SplitVotingTest,
	TransitiveRejectionTest,
	IsVirtuousTest,
	QuiesceTest,
	OrphansTest,
	ErrorOnVacuousAcceptTest,
	ErrorOnTxAcceptTest,
	ErrorOnVtxAcceptTest,
	ErrorOnVtxRejectTest,
	ErrorOnParentVtxRejectTest,
	ErrorOnTransitiveVtxRejectTest,
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

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	} else if avl.Quiesce() {
		t.Fatalf("Shouldn't quiesce")
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	} else if !avl.Quiesce() {
		t.Fatalf("Should quiesce")
	} else if err := avl.Add(vtx2); err != nil {
		t.Fatal(err)
	} else if avl.Quiesce() {
		t.Fatalf("Shouldn't quiesce")
	}

	sm := ids.UniqueBag{}
	sm.Add(0, vtx2.IDV)
	if err := avl.RecordPoll(sm); err != nil {
		t.Fatal(err)
	} else if !avl.Quiesce() {
		t.Fatalf("Should quiesce")
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

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	} else if orphans := avl.Orphans(); orphans.Len() != 0 {
		t.Fatalf("Wrong number of orphans")
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	} else if orphans := avl.Orphans(); orphans.Len() != 0 {
		t.Fatalf("Wrong number of orphans")
	} else if err := avl.Add(vtx2); err != nil {
		t.Fatal(err)
	} else if orphans := avl.Orphans(); orphans.Len() != 0 {
		t.Fatalf("Wrong number of orphans")
	}

	sm := ids.UniqueBag{}
	sm.Add(0, vtx1.IDV)
	if err := avl.RecordPoll(sm); err != nil {
		t.Fatal(err)
	} else if orphans := avl.Orphans(); orphans.Len() != 1 {
		t.Fatalf("Wrong number of orphans")
	} else if !orphans.Contains(tx2.ID()) {
		t.Fatalf("Wrong orphan")
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
