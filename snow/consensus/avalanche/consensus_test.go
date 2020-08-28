// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowball"
	"github.com/ava-labs/gecko/snow/consensus/snowstorm"
)

var (
	Tests = []func(*testing.T, Factory){
		MetricsTest,
		ParamsTest,
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
)

func ConsensusTest(t *testing.T, factory Factory) {
	for _, test := range Tests {
		test(t, factory)
	}
}

func MetricsTest(t *testing.T, factory Factory) {
	ctx := snow.DefaultContextTest()

	{
		avl := factory.New()
		params := Parameters{
			Parameters: snowball.Parameters{
				Namespace:    fmt.Sprintf("gecko_%s", ctx.ChainID.String()),
				Metrics:      prometheus.NewRegistry(),
				K:            2,
				Alpha:        2,
				BetaVirtuous: 1,
				BetaRogue:    2,
			},
			Parents:   2,
			BatchSize: 1,
		}
		params.Metrics.Register(prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: params.Namespace,
			Name:      "vtx_processing",
		}))
		avl.Initialize(ctx, params, nil)
	}
	{
		avl := factory.New()
		params := Parameters{
			Parameters: snowball.Parameters{
				Namespace:    fmt.Sprintf("gecko_%s", ctx.ChainID.String()),
				Metrics:      prometheus.NewRegistry(),
				K:            2,
				Alpha:        2,
				BetaVirtuous: 1,
				BetaRogue:    2,
			},
			Parents:   2,
			BatchSize: 1,
		}
		params.Metrics.Register(prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: params.Namespace,
			Name:      "vtx_accepted",
		}))
		avl.Initialize(ctx, params, nil)
	}
	{
		avl := factory.New()
		params := Parameters{
			Parameters: snowball.Parameters{
				Namespace:    fmt.Sprintf("gecko_%s", ctx.ChainID.String()),
				Metrics:      prometheus.NewRegistry(),
				K:            2,
				Alpha:        2,
				BetaVirtuous: 1,
				BetaRogue:    2,
			},
			Parents:   2,
			BatchSize: 1,
		}
		params.Metrics.Register(prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: params.Namespace,
			Name:      "vtx_rejected",
		}))
		avl.Initialize(ctx, params, nil)
	}
}

func ParamsTest(t *testing.T, factory Factory) {
	avl := factory.New()

	ctx := snow.DefaultContextTest()
	params := Parameters{
		Parameters: snowball.Parameters{
			Namespace:         fmt.Sprintf("gecko_%s", ctx.ChainID.String()),
			Metrics:           prometheus.NewRegistry(),
			K:                 2,
			Alpha:             2,
			BetaVirtuous:      1,
			BetaRogue:         2,
			ConcurrentRepolls: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}

	if err := avl.Initialize(ctx, params, nil); err != nil {
		t.Fatal(err)
	}

	if p := avl.Parameters(); p.K != params.K {
		t.Fatalf("Wrong K parameter")
	} else if p.Alpha != params.Alpha {
		t.Fatalf("Wrong Alpha parameter")
	} else if p.BetaVirtuous != params.BetaVirtuous {
		t.Fatalf("Wrong Beta1 parameter")
	} else if p.BetaRogue != params.BetaRogue {
		t.Fatalf("Wrong Beta2 parameter")
	} else if p.Parents != params.Parents {
		t.Fatalf("Wrong Parents parameter")
	}
}

func AddTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			Metrics:           prometheus.NewRegistry(),
			K:                 2,
			Alpha:             2,
			BetaVirtuous:      1,
			BetaRogue:         2,
			ConcurrentRepolls: 1,
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

	if err := avl.Initialize(snow.DefaultContextTest(), params, vts); err != nil {
		t.Fatal(err)
	}

	if !avl.Finalized() {
		t.Fatalf("An empty avalanche instance is not finalized")
	} else if !ids.UnsortedEquals([]ids.ID{vts[0].ID(), vts[1].ID()}, avl.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

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
	} else if avl.Finalized() {
		t.Fatalf("A non-empty avalanche instance is finalized")
	} else if !ids.UnsortedEquals([]ids.ID{vtx0.IDV}, avl.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	}

	tx1 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx1.InputIDsV.Add(utxos[0])

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
	} else if avl.Finalized() {
		t.Fatalf("A non-empty avalanche instance is finalized")
	} else if !ids.UnsortedEquals([]ids.ID{vtx0.IDV}, avl.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	} else if avl.Finalized() {
		t.Fatalf("A non-empty avalanche instance is finalized")
	} else if !ids.UnsortedEquals([]ids.ID{vtx0.IDV}, avl.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	} else if err := avl.Add(vts[0]); err != nil {
		t.Fatal(err)
	} else if avl.Finalized() {
		t.Fatalf("A non-empty avalanche instance is finalized")
	} else if !ids.UnsortedEquals([]ids.ID{vtx0.IDV}, avl.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	}
}

func VertexIssuedTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			Metrics:           prometheus.NewRegistry(),
			K:                 2,
			Alpha:             2,
			BetaVirtuous:      1,
			BetaRogue:         2,
			ConcurrentRepolls: 1,
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

	if err := avl.Initialize(snow.DefaultContextTest(), params, vts); err != nil {
		t.Fatal(err)
	}

	if !avl.VertexIssued(vts[0]) {
		t.Fatalf("Genesis Vertex not reported as issued")
	}

	tx := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx.InputIDsV.Add(utxos[0])

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
			Metrics:           prometheus.NewRegistry(),
			K:                 2,
			Alpha:             2,
			BetaVirtuous:      1,
			BetaRogue:         2,
			ConcurrentRepolls: 1,
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
	tx1.InputIDsV.Add(utxos[0])

	if err := avl.Initialize(snow.DefaultContextTest(), params, vts); err != nil {
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
			Metrics:           prometheus.NewRegistry(),
			K:                 2,
			Alpha:             2,
			BetaVirtuous:      10,
			BetaRogue:         20,
			ConcurrentRepolls: 1,
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

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	if virtuous := avl.Virtuous(); virtuous.Len() != 2 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vts[0].ID()) {
		t.Fatalf("Wrong virtuous")
	} else if !virtuous.Contains(vts[1].ID()) {
		t.Fatalf("Wrong virtuous")
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

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
	tx1.InputIDsV.Add(utxos[0])

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
	tx2.InputIDsV.Add(utxos[1])

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
	} else if virtuous := avl.Virtuous(); virtuous.Len() != 2 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vts[0].ID()) {
		t.Fatalf("Wrong virtuous")
	} else if !virtuous.Contains(vts[1].ID()) {
		t.Fatalf("Wrong virtuous")
	} else if err := avl.Add(vtx2); err != nil {
		t.Fatal(err)
	} else if virtuous := avl.Virtuous(); virtuous.Len() != 2 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vts[0].ID()) {
		t.Fatalf("Wrong virtuous")
	} else if !virtuous.Contains(vts[1].ID()) {
		t.Fatalf("Wrong virtuous")
	} else if err := avl.RecordPoll(votes); err != nil {
		t.Fatal(err)
	} else if virtuous := avl.Virtuous(); virtuous.Len() != 2 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vts[0].ID()) {
		t.Fatalf("Wrong virtuous")
	} else if !virtuous.Contains(vts[1].ID()) {
		t.Fatalf("Wrong virtuous")
	}
}

func VirtuousSkippedUpdateTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			Metrics:           prometheus.NewRegistry(),
			K:                 2,
			Alpha:             2,
			BetaVirtuous:      10,
			BetaRogue:         20,
			ConcurrentRepolls: 1,
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

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	if virtuous := avl.Virtuous(); virtuous.Len() != 2 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vts[0].ID()) {
		t.Fatalf("Wrong virtuous")
	} else if !virtuous.Contains(vts[1].ID()) {
		t.Fatalf("Wrong virtuous")
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

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
	tx1.InputIDsV.Add(utxos[0])

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

func VotingTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			Metrics:           prometheus.NewRegistry(),
			K:                 2,
			Alpha:             2,
			BetaVirtuous:      1,
			BetaRogue:         2,
			ConcurrentRepolls: 1,
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

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

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
	tx1.InputIDsV.Add(utxos[0])

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
	sm.Add(0, vtx1.IDV)
	sm.Add(1, vtx1.IDV)
	if err := avl.RecordPoll(sm); err != nil {
		t.Fatal(err)
	} else if avl.Finalized() {
		t.Fatalf("An avalanche instance finalized too early")
	} else if !ids.UnsortedEquals([]ids.ID{vtx1.IDV}, avl.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	} else if err := avl.RecordPoll(sm); err != nil {
		t.Fatal(err)
	} else if !avl.Finalized() {
		t.Fatalf("An avalanche instance finalized too late")
	} else if !ids.UnsortedEquals([]ids.ID{vtx1.IDV}, avl.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	} else if tx0.Status() != choices.Rejected {
		t.Fatalf("Tx should have been rejected")
	} else if tx1.Status() != choices.Accepted {
		t.Fatalf("Tx should have been accepted")
	}
}

func IgnoreInvalidVotingTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			Metrics:           prometheus.NewRegistry(),
			K:                 3,
			Alpha:             2,
			BetaVirtuous:      1,
			BetaRogue:         1,
			ConcurrentRepolls: 1,
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

	if err := avl.Initialize(snow.DefaultContextTest(), params, vts); err != nil {
		t.Fatal(err)
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

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
	tx1.InputIDsV.Add(utxos[0])

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
			Metrics:           prometheus.NewRegistry(),
			K:                 2,
			Alpha:             2,
			BetaVirtuous:      1,
			BetaRogue:         2,
			ConcurrentRepolls: 1,
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

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

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
	tx1.InputIDsV.Add(utxos[1])

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
	if err := avl.RecordPoll(sm1); err != nil {
		t.Fatal(err)
	} else if avl.Finalized() {
		t.Fatalf("An avalanche instance finalized too early")
	} else if !ids.UnsortedEquals([]ids.ID{vtx2.IDV}, avl.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	} else if tx0.Status() != choices.Accepted {
		t.Fatalf("Tx should have been accepted")
	}

	sm2 := ids.UniqueBag{}
	sm2.Add(0, vtx2.IDV)
	sm2.Add(1, vtx2.IDV)
	if err := avl.RecordPoll(sm2); err != nil {
		t.Fatal(err)
	} else if !avl.Finalized() {
		t.Fatalf("An avalanche instance finalized too late")
	} else if !ids.UnsortedEquals([]ids.ID{vtx2.IDV}, avl.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	} else if tx0.Status() != choices.Accepted {
		t.Fatalf("Tx should have been accepted")
	} else if tx1.Status() != choices.Accepted {
		t.Fatalf("Tx should have been accepted")
	}
}

func SplitVotingTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			Metrics:           prometheus.NewRegistry(),
			K:                 2,
			Alpha:             2,
			BetaVirtuous:      1,
			BetaRogue:         2,
			ConcurrentRepolls: 1,
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

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

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
	if err := avl.RecordPoll(sm1); err != nil {
		t.Fatal(err)
	} else if !avl.Finalized() {
		t.Fatalf("An avalanche instance finalized too late")
	} else if !ids.UnsortedEquals([]ids.ID{vtx0.IDV, vtx1.IDV}, avl.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	} else if tx0.Status() != choices.Accepted {
		t.Fatalf("Tx should have been accepted")
	}
}

func TransitiveRejectionTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			Metrics:           prometheus.NewRegistry(),
			K:                 2,
			Alpha:             2,
			BetaVirtuous:      1,
			BetaRogue:         2,
			ConcurrentRepolls: 1,
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

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

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
	tx1.InputIDsV.Add(utxos[0])

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
	tx2.InputIDsV.Add(utxos[1])

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
	if err := avl.RecordPoll(sm); err != nil {
		t.Fatal(err)
	} else if avl.Finalized() {
		t.Fatalf("An avalanche instance finalized too early")
	} else if !ids.UnsortedEquals([]ids.ID{vtx1.IDV}, avl.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	} else if err := avl.RecordPoll(sm); err != nil {
		t.Fatal(err)
	} else if avl.Finalized() {
		t.Fatalf("An avalanche instance finalized too early")
	} else if !ids.UnsortedEquals([]ids.ID{vtx1.IDV}, avl.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	} else if tx0.Status() != choices.Rejected {
		t.Fatalf("Tx should have been rejected")
	} else if tx1.Status() != choices.Accepted {
		t.Fatalf("Tx should have been accepted")
	} else if tx2.Status() != choices.Processing {
		t.Fatalf("Tx should not have been decided")
	} else if err := avl.RecordPoll(sm); err != nil {
		t.Fatal(err)
	} else if avl.Finalized() {
		t.Fatalf("An avalanche instance finalized too early")
	} else if !ids.UnsortedEquals([]ids.ID{vtx1.IDV}, avl.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	} else if tx0.Status() != choices.Rejected {
		t.Fatalf("Tx should have been rejected")
	} else if tx1.Status() != choices.Accepted {
		t.Fatalf("Tx should have been accepted")
	} else if tx2.Status() != choices.Processing {
		t.Fatalf("Tx should not have been decided")
	}
}

func IsVirtuousTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			Metrics:           prometheus.NewRegistry(),
			K:                 2,
			Alpha:             2,
			BetaVirtuous:      1,
			BetaRogue:         2,
			ConcurrentRepolls: 1,
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

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	if virtuous := avl.Virtuous(); virtuous.Len() != 2 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vts[0].ID()) {
		t.Fatalf("Wrong virtuous")
	} else if !virtuous.Contains(vts[1].ID()) {
		t.Fatalf("Wrong virtuous")
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

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
	tx1.InputIDsV.Add(utxos[0])

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
	} else if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	} else if !avl.IsVirtuous(tx0) {
		t.Fatalf("Should be virtuous.")
	} else if avl.IsVirtuous(tx1) {
		t.Fatalf("Should not be virtuous.")
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	} else if avl.IsVirtuous(tx0) {
		t.Fatalf("Should not be virtuous.")
	} else if avl.IsVirtuous(tx1) {
		t.Fatalf("Should not be virtuous.")
	}
}

func QuiesceTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			Metrics:           prometheus.NewRegistry(),
			K:                 1,
			Alpha:             1,
			BetaVirtuous:      1,
			BetaRogue:         1,
			ConcurrentRepolls: 1,
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

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

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
	tx1.InputIDsV.Add(utxos[0])

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
	tx2.InputIDsV.Add(utxos[1])

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
			Metrics:           prometheus.NewRegistry(),
			K:                 1,
			Alpha:             1,
			BetaVirtuous:      math.MaxInt32,
			BetaRogue:         math.MaxInt32,
			ConcurrentRepolls: 1,
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

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

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
	tx1.InputIDsV.Add(utxos[0])

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
	tx2.InputIDsV.Add(utxos[1])

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
			Metrics:           prometheus.NewRegistry(),
			K:                 1,
			Alpha:             1,
			BetaVirtuous:      math.MaxInt32,
			BetaRogue:         math.MaxInt32,
			ConcurrentRepolls: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{&TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}}

	avl.Initialize(snow.DefaultContextTest(), params, vts)

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
			Metrics:           prometheus.NewRegistry(),
			K:                 1,
			Alpha:             1,
			BetaVirtuous:      1,
			BetaRogue:         1,
			ConcurrentRepolls: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{&TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}}
	utxos := []ids.ID{ids.GenerateTestID()}

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		AcceptV: errors.New(""),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

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
			Metrics:           prometheus.NewRegistry(),
			K:                 1,
			Alpha:             1,
			BetaVirtuous:      1,
			BetaRogue:         1,
			ConcurrentRepolls: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{&TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}}
	utxos := []ids.ID{ids.GenerateTestID()}

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

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
			Metrics:           prometheus.NewRegistry(),
			K:                 1,
			Alpha:             1,
			BetaVirtuous:      1,
			BetaRogue:         1,
			ConcurrentRepolls: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{&TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}}
	utxos := []ids.ID{ids.GenerateTestID()}

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

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
	tx1.InputIDsV.Add(utxos[0])

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
			Metrics:           prometheus.NewRegistry(),
			K:                 1,
			Alpha:             1,
			BetaVirtuous:      1,
			BetaRogue:         1,
			ConcurrentRepolls: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{&TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}}
	utxos := []ids.ID{ids.GenerateTestID()}

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

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
	tx1.InputIDsV.Add(utxos[0])

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
			Metrics:           prometheus.NewRegistry(),
			K:                 1,
			Alpha:             1,
			BetaVirtuous:      1,
			BetaRogue:         1,
			ConcurrentRepolls: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{&TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}}
	utxos := []ids.ID{ids.GenerateTestID()}

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

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
	tx1.InputIDsV.Add(utxos[0])

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
