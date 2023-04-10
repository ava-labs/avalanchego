// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"context"
	"errors"
	"math"
	"path"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/utils/bag"
	"github.com/ava-labs/avalanchego/utils/compare"
	"github.com/ava-labs/avalanchego/utils/set"
)

type testFunc func(*testing.T, Factory)

var (
	testFuncs = []testFunc{
		MetricsTest,
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
		StopVertexVerificationUnequalBetaValuesTest,
		StopVertexVerificationEqualBetaValuesTest,
		AcceptParentOfPreviouslyRejectedVertexTest,
		RejectParentOfPreviouslyRejectedVertexTest,
		QuiesceAfterRejectedVertexTest,
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

	errTest = errors.New("non-nil error")
)

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
		err := ctx.AvalancheRegisterer.Register(prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "vtx_processing",
		}))
		if err != nil {
			t.Fatal(err)
		}
		if err := avl.Initialize(context.Background(), ctx, params, nil); err == nil {
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
		err := ctx.AvalancheRegisterer.Register(prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "vtx_accepted",
		}))
		if err != nil {
			t.Fatal(err)
		}
		if err := avl.Initialize(context.Background(), ctx, params, nil); err == nil {
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
		err := ctx.AvalancheRegisterer.Register(prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "vtx_rejected",
		}))
		if err != nil {
			t.Fatal(err)
		}
		if err := avl.Initialize(context.Background(), ctx, params, nil); err == nil {
			t.Fatalf("should have failed due to registering a duplicated statistic")
		}
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

	if err := avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts); err != nil {
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

	if err := avl.Add(context.Background(), vtx0); err != nil {
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

	if err := avl.Add(context.Background(), vtx1); err != nil {
		t.Fatal(err)
	}

	if numProcessing := avl.NumProcessing(); numProcessing != 2 {
		t.Fatalf("expected %d vertices processing but returned %d", 2, numProcessing)
	}

	if err := avl.Add(context.Background(), vtx1); err != nil {
		t.Fatal(err)
	}

	if numProcessing := avl.NumProcessing(); numProcessing != 2 {
		t.Fatalf("expected %d vertices processing but returned %d", 2, numProcessing)
	}

	if err := avl.Add(context.Background(), vts[0]); err != nil {
		t.Fatal(err)
	}

	if numProcessing := avl.NumProcessing(); numProcessing != 2 {
		t.Fatalf("expected %d vertices processing but returned %d", 2, numProcessing)
	}

	votes := bag.UniqueBag[ids.ID]{}
	votes.Add(0, vtx0.ID())
	if err := avl.RecordPoll(context.Background(), votes); err != nil {
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
	vertexEvents := snow.NewAcceptorTracker()
	ctx.VertexAcceptor = vertexEvents

	if err := avl.Initialize(context.Background(), ctx, params, seedVertices); err != nil {
		t.Fatal(err)
	}

	if !avl.Finalized() {
		t.Fatal("An empty avalanche instance is not finalized")
	}
	if !compare.UnsortedEquals([]ids.ID{seedVertices[0].ID(), seedVertices[1].ID()}, avl.Preferences().List()) {
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
			err := avl.Add(context.Background(), tv.toAdd)
			if err != tv.err {
				t.Fatalf("#%d-%d: expected error %v, got %v", i, j, tv.err, err)
			}
			finalized := avl.Finalized()
			if finalized != tv.finalized {
				t.Fatalf("#%d-%d: expected finalized %v, got %v", i, j, finalized, tv.finalized)
			}
			preferenceSet := avl.Preferences().List()
			if !compare.UnsortedEquals(tv.preferenceSet, preferenceSet) {
				t.Fatalf("#%d-%d: expected preferenceSet %v, got %v", i, j, preferenceSet, tv.preferenceSet)
			}
			if accepted, _ := vertexEvents.IsAccepted(tv.toAdd.ID()); accepted != tv.accepted {
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

	if err := avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts); err != nil {
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
	} else if err := avl.Add(context.Background(), vtx); err != nil {
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

	if err := avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts); err != nil {
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

	if err := avl.Add(context.Background(), vtx); err != nil {
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

	err := avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts)
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

	if err := avl.Add(context.Background(), vtx0); err != nil {
		t.Fatal(err)
	} else if virtuous := avl.Virtuous(); virtuous.Len() != 1 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vtx0.IDV) {
		t.Fatalf("Wrong virtuous")
	} else if err := avl.Add(context.Background(), vtx1); err != nil {
		t.Fatal(err)
	} else if virtuous := avl.Virtuous(); virtuous.Len() != 1 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vtx0.IDV) {
		t.Fatalf("Wrong virtuous")
	}

	votes := bag.UniqueBag[ids.ID]{}
	votes.Add(0, vtx1.ID())
	votes.Add(1, vtx1.ID())

	if err := avl.RecordPoll(context.Background(), votes); err != nil {
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

	if err := avl.Add(context.Background(), vtx2); err != nil {
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

	if err := avl.RecordPoll(context.Background(), votes); err != nil {
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

	err := avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts)
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

	if err := avl.Add(context.Background(), vtx0); err != nil {
		t.Fatal(err)
	} else if virtuous := avl.Virtuous(); virtuous.Len() != 1 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vtx0.IDV) {
		t.Fatalf("Wrong virtuous")
	} else if err := avl.Add(context.Background(), vtx1); err != nil {
		t.Fatal(err)
	} else if virtuous := avl.Virtuous(); virtuous.Len() != 1 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vtx0.IDV) {
		t.Fatalf("Wrong virtuous")
	} else if err := avl.RecordPoll(context.Background(), bag.UniqueBag[ids.ID]{}); err != nil {
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

	err := avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts)
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
	if err := avl.Add(context.Background(), vtx0); err != nil {
		t.Fatal(err)
	}
	if err := avl.Add(context.Background(), vtx1); err != nil {
		t.Fatal(err)
	}

	// create poll results, all vote for vtx1, not for vtx0
	sm := bag.UniqueBag[ids.ID]{}
	sm.Add(0, vtx1.IDV)
	sm.Add(1, vtx1.IDV)

	// "BetaRogue" is 2, thus consensus should not be finalized yet
	err = avl.RecordPoll(context.Background(), sm)
	switch {
	case err != nil:
		t.Fatal(err)
	case avl.Finalized():
		t.Fatalf("An avalanche instance finalized too early")
	case !compare.UnsortedEquals([]ids.ID{vtx1.IDV}, avl.Preferences().List()):
		t.Fatalf("Initial frontier failed to be set")
	case tx0.Status() != choices.Processing:
		t.Fatalf("Tx should have been Processing")
	case tx1.Status() != choices.Processing:
		t.Fatalf("Tx should have been Processing")
	}

	// second poll should reach consensus,
	// and the other vertex of conflict transaction should be rejected
	err = avl.RecordPoll(context.Background(), sm)
	switch {
	case err != nil:
		t.Fatal(err)
	case !avl.Finalized():
		t.Fatalf("An avalanche instance finalized too late")
	case !compare.UnsortedEquals([]ids.ID{vtx1.IDV}, avl.Preferences().List()):
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

	if err := avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts); err != nil {
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

	if err := avl.Add(context.Background(), vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(context.Background(), vtx1); err != nil {
		t.Fatal(err)
	}

	sm := bag.UniqueBag[ids.ID]{}
	sm.Add(0, vtx0.IDV)
	sm.Add(1, vtx1.IDV)

	// Add Illegal Vote cast by Response 2
	sm.Add(2, vtx0.IDV)
	sm.Add(2, vtx1.IDV)

	if err := avl.RecordPoll(context.Background(), sm); err != nil {
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

	if err := avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts); err != nil {
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

	if err := avl.Add(context.Background(), vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(context.Background(), vtx1); err != nil {
		t.Fatal(err)
	}

	sm := bag.UniqueBag[ids.ID]{}
	sm.Add(0, vtx0.IDV)
	sm.Add(1, vtx1.IDV)

	// Add Illegal Vote cast by Response 2
	sm.Add(2, vtx0.IDV)
	sm.Add(2, vtx1.IDV)

	if err := avl.RecordPoll(context.Background(), sm); err != nil {
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

	err := avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts)
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

	if err := avl.Add(context.Background(), vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(context.Background(), vtx1); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(context.Background(), vtx2); err != nil {
		t.Fatal(err)
	}

	sm1 := bag.UniqueBag[ids.ID]{}
	sm1.Add(0, vtx0.IDV)
	sm1.Add(1, vtx2.IDV)

	err = avl.RecordPoll(context.Background(), sm1)
	switch {
	case err != nil:
		t.Fatal(err)
	case avl.Finalized():
		t.Fatalf("An avalanche instance finalized too early")
	case !compare.UnsortedEquals([]ids.ID{vtx2.IDV}, avl.Preferences().List()):
		t.Fatalf("Initial frontier failed to be set")
	case tx0.Status() != choices.Accepted:
		t.Fatalf("Tx should have been accepted")
	}

	sm2 := bag.UniqueBag[ids.ID]{}
	sm2.Add(0, vtx2.IDV)
	sm2.Add(1, vtx2.IDV)

	err = avl.RecordPoll(context.Background(), sm2)
	switch {
	case err != nil:
		t.Fatal(err)
	case !avl.Finalized():
		t.Fatalf("An avalanche instance finalized too late")
	case !compare.UnsortedEquals([]ids.ID{vtx2.IDV}, avl.Preferences().List()):
		t.Fatalf("Initial frontier failed to be set")
	case tx0.Status() != choices.Accepted:
		t.Fatalf("Tx should have been accepted")
	case tx1.Status() != choices.Accepted:
		t.Fatalf("Tx should have been accepted")
	}
}

func StopVertexVerificationUnequalBetaValuesTest(t *testing.T, factory Factory) {
	require := require.New(t)

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

	require.NoError(avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts))

	tx0 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: utxos,
	}
	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: utxos,
	}

	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}
	vtx1A := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts[:1],
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1},
	}
	vtx1B := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts[1:],
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1},
	}
	stopVertex := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV:      []Vertex{vtx1B},
		HasWhitelistV: true,
		WhitelistV: set.Set[ids.ID]{
			vtx1B.IDV: struct{}{},
			tx1.IDV:   struct{}{},
		},
		HeightV: 2,
	}

	require.NoError(avl.Add(context.Background(), vtx0))
	require.NoError(avl.Add(context.Background(), vtx1A))
	require.NoError(avl.Add(context.Background(), vtx1B))

	sm1 := bag.UniqueBag[ids.ID]{}
	sm1.Add(0, vtx1A.IDV, vtx1B.IDV)

	// Transaction vertex for vtx1A is now accepted
	require.NoError(avl.RecordPoll(context.Background(), sm1))
	require.Equal(choices.Processing, tx0.Status())
	require.Equal(choices.Processing, tx1.Status())
	require.Equal(choices.Processing, vtx0.Status())
	require.Equal(choices.Processing, vtx1A.Status())
	require.Equal(choices.Processing, vtx1B.Status())

	// Because vtx1A isn't accepted, the stopVertex verification passes
	require.NoError(avl.Add(context.Background(), stopVertex))

	// Because vtx1A is now accepted, the stopVertex should be rejected.
	// However, because BetaVirtuous < BetaRogue it is possible for the
	// stopVertex to be processing.
	require.NoError(avl.RecordPoll(context.Background(), sm1))
	require.Equal(choices.Rejected, tx0.Status())
	require.Equal(choices.Accepted, tx1.Status())
	require.Equal(choices.Rejected, vtx0.Status())
	require.Equal(choices.Accepted, vtx1A.Status())
	require.Equal(choices.Accepted, vtx1B.Status())
	require.Equal(choices.Processing, stopVertex.Status())
}

func StopVertexVerificationEqualBetaValuesTest(t *testing.T, factory Factory) {
	require := require.New(t)

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
	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	require.NoError(avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts))

	tx0 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: utxos,
	}
	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: utxos,
	}

	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}
	vtx1A := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts[:1],
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1},
	}
	vtx1B := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts[1:],
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1},
	}
	stopVertex := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV:      []Vertex{vtx1B},
		HasWhitelistV: true,
		WhitelistV: set.Set[ids.ID]{
			vtx1B.IDV: struct{}{},
			tx1.IDV:   struct{}{},
		},
		HeightV: 2,
	}

	require.NoError(avl.Add(context.Background(), vtx0))
	require.NoError(avl.Add(context.Background(), vtx1A))
	require.NoError(avl.Add(context.Background(), vtx1B))

	sm1 := bag.UniqueBag[ids.ID]{}
	sm1.Add(0, vtx1A.IDV, vtx1B.IDV)

	// Transaction vertex for vtx1A can not be accepted because BetaVirtuous is
	// equal to BetaRogue
	require.NoError(avl.RecordPoll(context.Background(), sm1))
	require.Equal(choices.Processing, tx0.Status())
	require.Equal(choices.Processing, tx1.Status())
	require.Equal(choices.Processing, vtx0.Status())
	require.Equal(choices.Processing, vtx1A.Status())
	require.Equal(choices.Processing, vtx1B.Status())

	// Because vtx1A isn't accepted, the stopVertex verification passes
	require.NoError(avl.Add(context.Background(), stopVertex))

	// Because vtx1A is now accepted, the stopVertex should be rejected
	require.NoError(avl.RecordPoll(context.Background(), sm1))
	require.Equal(choices.Rejected, tx0.Status())
	require.Equal(choices.Accepted, tx1.Status())
	require.Equal(choices.Rejected, vtx0.Status())
	require.Equal(choices.Accepted, vtx1A.Status())
	require.Equal(choices.Accepted, vtx1B.Status())
	require.Equal(choices.Rejected, stopVertex.Status())
}

func AcceptParentOfPreviouslyRejectedVertexTest(t *testing.T, factory Factory) {
	require := require.New(t)

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
	}
	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	require.NoError(avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts))

	tx0 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: utxos[:1],
	}

	tx1A := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: utxos[1:],
	}
	tx1B := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: utxos[1:],
	}

	vtx1A := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1A},
	}

	vtx0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}
	vtx1B := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []Vertex{vtx0},
		HeightV:  2,
		TxsV:     []snowstorm.Tx{tx1B},
	}

	require.NoError(avl.Add(context.Background(), vtx0))
	require.NoError(avl.Add(context.Background(), vtx1A))
	require.NoError(avl.Add(context.Background(), vtx1B))

	sm1 := bag.UniqueBag[ids.ID]{}
	sm1.Add(0, vtx1A.IDV)

	require.NoError(avl.RecordPoll(context.Background(), sm1))
	require.Equal(choices.Accepted, tx1A.Status())
	require.Equal(choices.Accepted, vtx1A.Status())
	require.Equal(choices.Rejected, tx1B.Status())
	require.Equal(choices.Rejected, vtx1B.Status())
	require.Equal(1, avl.NumProcessing())
	require.Equal(choices.Processing, tx0.Status())
	require.Equal(choices.Processing, vtx0.Status())

	sm0 := bag.UniqueBag[ids.ID]{}
	sm0.Add(0, vtx0.IDV)

	require.NoError(avl.RecordPoll(context.Background(), sm0))
	require.Zero(avl.NumProcessing())
	require.Equal(choices.Accepted, tx0.Status())
	require.Equal(choices.Accepted, vtx0.Status())

	prefs := avl.Preferences()
	require.Len(prefs, 2)
	require.Contains(prefs, vtx0.ID())
	require.Contains(prefs, vtx1A.ID())
}

func RejectParentOfPreviouslyRejectedVertexTest(t *testing.T, factory Factory) {
	require := require.New(t)

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
	}
	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	require.NoError(avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts))

	tx0A := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: utxos[:1],
	}
	tx0B := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: utxos[:1],
	}

	tx1A := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: utxos[1:],
	}
	tx1B := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: utxos[1:],
	}

	vtx0A := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0A},
	}
	vtx1A := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1A},
	}

	vtx0B := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0B},
	}
	vtx1B := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []Vertex{vtx0B},
		HeightV:  2,
		TxsV:     []snowstorm.Tx{tx1B},
	}

	require.NoError(avl.Add(context.Background(), vtx0A))
	require.NoError(avl.Add(context.Background(), vtx1A))
	require.NoError(avl.Add(context.Background(), vtx0B))
	require.NoError(avl.Add(context.Background(), vtx1B))

	sm1 := bag.UniqueBag[ids.ID]{}
	sm1.Add(0, vtx1A.IDV)

	require.NoError(avl.RecordPoll(context.Background(), sm1))
	require.Equal(choices.Accepted, tx1A.Status())
	require.Equal(choices.Accepted, vtx1A.Status())
	require.Equal(choices.Rejected, tx1B.Status())
	require.Equal(choices.Rejected, vtx1B.Status())
	require.Equal(2, avl.NumProcessing())
	require.Equal(choices.Processing, tx0A.Status())
	require.Equal(choices.Processing, vtx0A.Status())
	require.Equal(choices.Processing, tx0B.Status())
	require.Equal(choices.Processing, vtx0B.Status())

	sm0 := bag.UniqueBag[ids.ID]{}
	sm0.Add(0, vtx0A.IDV)

	require.NoError(avl.RecordPoll(context.Background(), sm0))
	require.Zero(avl.NumProcessing())
	require.Equal(choices.Accepted, tx0A.Status())
	require.Equal(choices.Accepted, vtx0A.Status())
	require.Equal(choices.Rejected, tx0B.Status())
	require.Equal(choices.Rejected, vtx0B.Status())

	orphans := avl.Orphans()
	require.Empty(orphans)
}

func QuiesceAfterRejectedVertexTest(t *testing.T, factory Factory) {
	require := require.New(t)

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
	}
	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	require.NoError(avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts))

	txA := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: utxos,
	}
	txB := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: utxos,
	}

	vtxA := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{txA},
	}

	vtxB0 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{txB},
	}
	vtxB1 := &TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []Vertex{vtxB0},
		HeightV:  2,
		TxsV:     []snowstorm.Tx{txB},
	}

	require.NoError(avl.Add(context.Background(), vtxA))
	require.NoError(avl.Add(context.Background(), vtxB0))
	require.NoError(avl.Add(context.Background(), vtxB1))

	sm1 := bag.UniqueBag[ids.ID]{}
	sm1.Add(0, vtxA.IDV)

	require.NoError(avl.RecordPoll(context.Background(), sm1))
	require.Equal(choices.Accepted, txA.Status())
	require.Equal(choices.Accepted, vtxA.Status())
	require.Equal(choices.Rejected, txB.Status())
	require.Equal(choices.Rejected, vtxB0.Status())
	require.Equal(choices.Rejected, vtxB1.Status())
	require.Zero(avl.NumProcessing())
	require.True(avl.Finalized())
	require.True(avl.Quiesce())
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

	err := avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts)
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

	if err := avl.Add(context.Background(), vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(context.Background(), vtx1); err != nil {
		t.Fatal(err)
	}

	sm1 := bag.UniqueBag[ids.ID]{}
	sm1.Add(0, vtx0.IDV) // peer 0 votes for the tx though vtx0
	sm1.Add(1, vtx1.IDV) // peer 1 votes for the tx though vtx1

	err = avl.RecordPoll(context.Background(), sm1)
	switch {
	case err != nil:
		t.Fatal(err)
	case avl.Finalized(): // avalanche shouldn't be finalized because the vertex transactions are still processing
		t.Fatalf("An avalanche instance finalized too late")
	case !compare.UnsortedEquals([]ids.ID{vtx0.IDV, vtx1.IDV}, avl.Preferences().List()):
		t.Fatalf("Initial frontier failed to be set")
	case tx0.Status() != choices.Accepted:
		t.Fatalf("Tx should have been accepted")
	}

	// Give alpha votes for both tranaction vertices
	sm2 := bag.UniqueBag[ids.ID]{}
	sm2.Add(0, vtx0.IDV, vtx1.IDV) // peer 0 votes for vtx0 and vtx1
	sm2.Add(1, vtx0.IDV, vtx1.IDV) // peer 1 votes for vtx0 and vtx1

	err = avl.RecordPoll(context.Background(), sm2)
	switch {
	case err != nil:
		t.Fatal(err)
	case !avl.Finalized():
		t.Fatalf("An avalanche instance finalized too late")
	case !compare.UnsortedEquals([]ids.ID{vtx0.IDV, vtx1.IDV}, avl.Preferences().List()):
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

	err := avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts)
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

	if err := avl.Add(context.Background(), vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(context.Background(), vtx1); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(context.Background(), vtx2); err != nil {
		t.Fatal(err)
	}

	sm := bag.UniqueBag[ids.ID]{}
	sm.Add(0, vtx1.IDV)
	sm.Add(1, vtx1.IDV)

	err = avl.RecordPoll(context.Background(), sm)
	switch {
	case err != nil:
		t.Fatal(err)
	case avl.Finalized():
		t.Fatalf("An avalanche instance finalized too early")
	case !compare.UnsortedEquals([]ids.ID{vtx1.IDV}, avl.Preferences().List()):
		t.Fatalf("Initial frontier failed to be set")
	}

	err = avl.RecordPoll(context.Background(), sm)
	switch {
	case err != nil:
		t.Fatal(err)
	case avl.Finalized():
		t.Fatalf("An avalanche instance finalized too early")
	case !compare.UnsortedEquals([]ids.ID{vtx1.IDV}, avl.Preferences().List()):
		t.Fatalf("Initial frontier failed to be set")
	case tx0.Status() != choices.Rejected:
		t.Fatalf("Tx should have been rejected")
	case tx1.Status() != choices.Accepted:
		t.Fatalf("Tx should have been accepted")
	case tx2.Status() != choices.Processing:
		t.Fatalf("Tx should not have been decided")
	}

	err = avl.RecordPoll(context.Background(), sm)
	switch {
	case err != nil:
		t.Fatal(err)
	case avl.Finalized():
		t.Fatalf("An avalanche instance finalized too early")
	case !compare.UnsortedEquals([]ids.ID{vtx1.IDV}, avl.Preferences().List()):
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

	err := avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts)
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

	err = avl.Add(context.Background(), vtx0)
	switch {
	case err != nil:
		t.Fatal(err)
	case !avl.IsVirtuous(tx0):
		t.Fatalf("Should be virtuous.")
	case avl.IsVirtuous(tx1):
		t.Fatalf("Should not be virtuous.")
	}

	err = avl.Add(context.Background(), vtx1)
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

	err := avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts)
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
	if err := avl.Add(context.Background(), vtx0); err != nil {
		t.Fatal(err)
	}
	if avl.Quiesce() {
		t.Fatalf("Shouldn't quiesce")
	}

	// Add [vtx1] containing [tx1]. Because [tx1] conflicts with [tx0], neither
	// [tx0] nor [tx1] are now virtuous. This means there are no virtuous
	// transaction left in the consensus instance and it can quiesce.
	if err := avl.Add(context.Background(), vtx1); err != nil {
		t.Fatal(err)
	}

	// The virtuous frontier is only updated sometimes, so force the frontier to
	// be re-calculated by changing the preference of tx1.
	sm1 := bag.UniqueBag[ids.ID]{}
	sm1.Add(0, vtx1.IDV)
	if err := avl.RecordPoll(context.Background(), sm1); err != nil {
		t.Fatal(err)
	}

	if !avl.Quiesce() {
		t.Fatalf("Should quiesce")
	}

	// Add [vtx2] containing [tx2]. Because [tx2] is virtuous, the instance
	// shouldn't quiesce, even though [tx0] and [tx1] conflict.
	if err := avl.Add(context.Background(), vtx2); err != nil {
		t.Fatal(err)
	}
	if avl.Quiesce() {
		t.Fatalf("Shouldn't quiesce")
	}

	sm2 := bag.UniqueBag[ids.ID]{}
	sm2.Add(0, vtx2.IDV)
	if err := avl.RecordPoll(context.Background(), sm2); err != nil {
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

	err := avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts)
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

	if err := avl.Add(context.Background(), vtx0); err != nil {
		t.Fatal(err)
	}
	if err := avl.Add(context.Background(), vtx1); err != nil {
		t.Fatal(err)
	}
	if err := avl.Add(context.Background(), vtx2); err != nil {
		t.Fatal(err)
	}
	if err := avl.Add(context.Background(), vtx3); err != nil {
		t.Fatal(err)
	}

	// Because [vtx2] and [vtx3] are virtuous, the instance shouldn't quiesce.
	if avl.Quiesce() {
		t.Fatalf("Shouldn't quiesce")
	}

	sm12 := bag.UniqueBag[ids.ID]{}
	sm12.Add(0, vtx1.IDV, vtx2.IDV)
	if err := avl.RecordPoll(context.Background(), sm12); err != nil {
		t.Fatal(err)
	}

	// Because [vtx2] and [vtx3] are still processing, the instance shouldn't
	// quiesce.
	if avl.Quiesce() {
		t.Fatalf("Shouldn't quiesce")
	}

	sm023 := bag.UniqueBag[ids.ID]{}
	sm023.Add(0, vtx0.IDV, vtx2.IDV, vtx3.IDV)
	if err := avl.RecordPoll(context.Background(), sm023); err != nil {
		t.Fatal(err)
	}

	// Because [vtx3] is still processing, the instance shouldn't quiesce.
	if avl.Quiesce() {
		t.Fatalf("Shouldn't quiesce")
	}

	sm3 := bag.UniqueBag[ids.ID]{}
	sm3.Add(0, vtx3.IDV)
	if err := avl.RecordPoll(context.Background(), sm3); err != nil {
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

	err := avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, seedVertices)
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
	if err := avl.Add(context.Background(), vtx0); err != nil {
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
	bags := bag.UniqueBag[ids.ID]{}
	bags.Add(0, vtx0.IDV)
	bags.Add(1, vtx0.IDV)
	if err := avl.RecordPoll(context.Background(), bags); err != nil {
		t.Fatalf("unexpected RecordPoll error %v", err)
	}

	switch {
	case vtx0.Status() != choices.Accepted:
		t.Fatalf("vertex with no transaction should have been accepted after polling, got %v", vtx0.Status())
	case !avl.Finalized():
		t.Fatal("expected finalized avalanche instance")
	case !compare.UnsortedEquals([]ids.ID{vtx0.IDV}, avl.Preferences().List()):
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

	err := avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts)
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
	if err := avl.Add(context.Background(), vtx0); err != nil {
		t.Fatal(err)
	}
	if orphans := avl.Orphans(); orphans.Len() != 0 {
		t.Fatalf("Wrong number of orphans")
	}

	// [vtx1] contains [tx1], which conflicts with [tx0]. [tx0] is contained in
	// a preferred vertex, and neither [tx0] nor [tx1] are virtuous.
	if err := avl.Add(context.Background(), vtx1); err != nil {
		t.Fatal(err)
	}
	if orphans := avl.Orphans(); orphans.Len() != 0 {
		t.Fatalf("Wrong number of orphans")
	}

	// [vtx2] contains [tx2], both of which will be preferred, so [tx2] is not
	// an orphan.
	if err := avl.Add(context.Background(), vtx2); err != nil {
		t.Fatal(err)
	}
	if orphans := avl.Orphans(); orphans.Len() != 0 {
		t.Fatalf("Wrong number of orphans")
	}

	sm := bag.UniqueBag[ids.ID]{}
	sm.Add(0, vtx1.IDV)
	if err := avl.RecordPoll(context.Background(), sm); err != nil {
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
	err := avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, seedVertices)
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

	if err := avl.Add(context.Background(), vtx0); err != nil {
		t.Fatal(err)
	}
	if err := avl.Add(context.Background(), vtx1); err != nil {
		t.Fatal(err)
	}
	if err := avl.Add(context.Background(), vtx2); err != nil {
		t.Fatal(err)
	}

	// vtx0 is virtuous, so it should be preferred. vtx1 and vtx2 conflict, but
	// vtx1 was issued before vtx2, so vtx1 should be preferred and vtx2 should
	// not be preferred.
	expectedPreferredSet := set.Set[ids.ID]{
		vtx0.ID(): struct{}{},
		vtx1.ID(): struct{}{},
	}
	preferenceSet := avl.Preferences().List()
	if !compare.UnsortedEquals(expectedPreferredSet.List(), preferenceSet) {
		t.Fatalf("expected preferenceSet %v, got %v", expectedPreferredSet, preferenceSet)
	}

	// Record a successful poll to change the preference from vtx1 to vtx2 and
	// update the orphan set.
	votes := bag.UniqueBag[ids.ID]{}
	votes.Add(0, vtx2.IDV)
	if err := avl.RecordPoll(context.Background(), votes); err != nil {
		t.Fatal(err)
	}

	// Because vtx2 was voted for over vtx1, they should be swapped in the
	// preferred set.
	expectedPreferredSet = set.Set[ids.ID]{
		vtx0.ID(): struct{}{},
		vtx2.ID(): struct{}{},
	}
	preferenceSet = avl.Preferences().List()
	if !compare.UnsortedEquals(expectedPreferredSet.List(), preferenceSet) {
		t.Fatalf("expected preferenceSet %v, got %v", expectedPreferredSet, preferenceSet)
	}

	// Because there are no virtuous transactions that are not in a preferred
	// vertex, there should be no orphans.
	expectedOrphanSet := set.Set[ids.ID]{}
	orphanSet := avl.Orphans()
	if !compare.UnsortedEquals(expectedOrphanSet.List(), orphanSet.List()) {
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

	err := avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts)
	if err != nil {
		t.Fatal(err)
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		AcceptV: errTest,
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

	if err := avl.Add(context.Background(), vtx0); err == nil {
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

	err := avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts)
	if err != nil {
		t.Fatal(err)
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		AcceptV: errTest,
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

	if err := avl.Add(context.Background(), vtx0); err != nil {
		t.Fatal(err)
	}

	votes := bag.UniqueBag[ids.ID]{}
	votes.Add(0, vtx0.IDV)
	if err := avl.RecordPoll(context.Background(), votes); err == nil {
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

	err := avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts)
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
			AcceptV: errTest,
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	if err := avl.Add(context.Background(), vtx0); err != nil {
		t.Fatal(err)
	}

	votes := bag.UniqueBag[ids.ID]{}
	votes.Add(0, vtx0.IDV)
	if err := avl.RecordPoll(context.Background(), votes); err == nil {
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

	err := avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts)
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
			RejectV: errTest,
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1},
	}

	if err := avl.Add(context.Background(), vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(context.Background(), vtx1); err != nil {
		t.Fatal(err)
	}

	votes := bag.UniqueBag[ids.ID]{}
	votes.Add(0, vtx0.IDV)
	if err := avl.RecordPoll(context.Background(), votes); err == nil {
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

	err := avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts)
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
			RejectV: errTest,
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

	if err := avl.Add(context.Background(), vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(context.Background(), vtx1); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(context.Background(), vtx2); err != nil {
		t.Fatal(err)
	}

	votes := bag.UniqueBag[ids.ID]{}
	votes.Add(0, vtx0.IDV)
	if err := avl.RecordPoll(context.Background(), votes); err == nil {
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

	err := avl.Initialize(context.Background(), snow.DefaultConsensusContextTest(), params, vts)
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
			RejectV: errTest,
			StatusV: choices.Processing,
		},
		ParentsV: []Vertex{vtx1},
		HeightV:  1,
	}

	if err := avl.Add(context.Background(), vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(context.Background(), vtx1); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(context.Background(), vtx2); err != nil {
		t.Fatal(err)
	}

	votes := bag.UniqueBag[ids.ID]{}
	votes.Add(0, vtx0.IDV)
	if err := avl.RecordPoll(context.Background(), votes); err == nil {
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
	tracker := snow.NewAcceptorTracker()
	ctx.TxAcceptor = tracker

	err := avl.Initialize(context.Background(), ctx, params, vts)
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

	if err := avl.Add(context.Background(), vtx0); err != nil {
		t.Fatal(err)
	}

	votes := bag.UniqueBag[ids.ID]{}
	votes.Add(0, vtx0.IDV)
	if err := avl.RecordPoll(context.Background(), votes); err != nil {
		t.Fatal(err)
	}

	if _, accepted := tracker.IsAccepted(vtx0.ID()); accepted {
		t.Fatalf("Shouldn't have reported the transaction vertex as accepted")
	}
}
