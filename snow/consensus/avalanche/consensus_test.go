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

func GenerateID() ids.ID {
	offset++
	return ids.Empty.Prefix(offset)
}

var (
	Genesis = GenerateID()
	offset  = uint64(0)

	Tests = []func(*testing.T, Factory){
		MetricsTest,
		ParamsTest,
		AddTest,
		VertexIssuedTest,
		TxIssuedTest,
		VirtuousTest,
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

	avl.Initialize(ctx, params, nil)

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
			Metrics:      prometheus.NewRegistry(),
			K:            2,
			Alpha:        2,
			BetaVirtuous: 1,
			BetaRogue:    2,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{&Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}, &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}}
	utxos := []ids.ID{GenerateID()}

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	if !avl.Finalized() {
		t.Fatalf("An empty avalanche instance is not finalized")
	} else if !ids.UnsortedEquals([]ids.ID{vts[0].ID(), vts[1].ID()}, avl.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	}

	tx0 := &snowstorm.TestTx{Identifier: GenerateID()}
	tx0.Ins.Add(utxos[0])

	vtx0 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx0},
		height:       1,
		status:       choices.Processing,
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	} else if avl.Finalized() {
		t.Fatalf("A non-empty avalanche instance is finalized")
	} else if !ids.UnsortedEquals([]ids.ID{vtx0.id}, avl.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	}

	tx1 := &snowstorm.TestTx{Identifier: GenerateID()}
	tx1.Ins.Add(utxos[0])

	vtx1 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx1},
		height:       1,
		status:       choices.Processing,
	}

	if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	} else if avl.Finalized() {
		t.Fatalf("A non-empty avalanche instance is finalized")
	} else if !ids.UnsortedEquals([]ids.ID{vtx0.id}, avl.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	} else if avl.Finalized() {
		t.Fatalf("A non-empty avalanche instance is finalized")
	} else if !ids.UnsortedEquals([]ids.ID{vtx0.id}, avl.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	} else if err := avl.Add(vts[0]); err != nil {
		t.Fatal(err)
	} else if avl.Finalized() {
		t.Fatalf("A non-empty avalanche instance is finalized")
	} else if !ids.UnsortedEquals([]ids.ID{vtx0.id}, avl.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	}
}

func VertexIssuedTest(t *testing.T, factory Factory) {
	avl := factory.New()

	params := Parameters{
		Parameters: snowball.Parameters{
			Metrics:      prometheus.NewRegistry(),
			K:            2,
			Alpha:        2,
			BetaVirtuous: 1,
			BetaRogue:    2,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{&Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}, &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}}
	utxos := []ids.ID{GenerateID()}

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	if !avl.VertexIssued(vts[0]) {
		t.Fatalf("Genesis Vertex not reported as issued")
	}

	tx := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx.Ins.Add(utxos[0])

	vtx := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx},
		height:       1,
		status:       choices.Processing,
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
			Metrics:      prometheus.NewRegistry(),
			K:            2,
			Alpha:        2,
			BetaVirtuous: 1,
			BetaRogue:    2,
		},
		Parents:   2,
		BatchSize: 1,
	}

	tx0 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Accepted,
	}
	vts := []Vertex{&Vtx{
		id:     GenerateID(),
		txs:    []snowstorm.Tx{tx0},
		status: choices.Accepted,
	}}
	utxos := []ids.ID{GenerateID()}

	tx1 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx1.Ins.Add(utxos[0])

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	if !avl.TxIssued(tx0) {
		t.Fatalf("Genesis Tx not reported as issued")
	} else if avl.TxIssued(tx1) {
		t.Fatalf("Tx reported as issued")
	}

	vtx := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx1},
		height:       1,
		status:       choices.Processing,
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
			BetaVirtuous:      1,
			BetaRogue:         2,
			ConcurrentRepolls: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}
	vts := []Vertex{&Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}, &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}}
	utxos := []ids.ID{GenerateID(), GenerateID()}

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	if virtuous := avl.Virtuous(); virtuous.Len() != 2 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vts[0].ID()) {
		t.Fatalf("Wrong virtuous")
	} else if !virtuous.Contains(vts[1].ID()) {
		t.Fatalf("Wrong virtuous")
	}

	tx0 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx0.Ins.Add(utxos[0])

	vtx0 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx0},
		height:       1,
		status:       choices.Processing,
	}

	tx1 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx1.Ins.Add(utxos[0])

	vtx1 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx1},
		height:       1,
		status:       choices.Processing,
	}

	tx2 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx2.Ins.Add(utxos[1])

	vtx2 := &Vtx{
		dependencies: []Vertex{vtx0},
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx2},
		height:       2,
		status:       choices.Processing,
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	} else if virtuous := avl.Virtuous(); virtuous.Len() != 1 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vtx0.id) {
		t.Fatalf("Wrong virtuous")
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	} else if virtuous := avl.Virtuous(); virtuous.Len() != 1 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vtx0.id) {
		t.Fatalf("Wrong virtuous")
	} else if err := avl.RecordPoll(ids.UniqueBag{}); err != nil {
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
	} else if err := avl.RecordPoll(ids.UniqueBag{}); err != nil {
		t.Fatal(err)
	} else if virtuous := avl.Virtuous(); virtuous.Len() != 2 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vts[0].ID()) {
		t.Fatalf("Wrong virtuous")
	} else if !virtuous.Contains(vts[1].ID()) {
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
	vts := []Vertex{&Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}, &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}}
	utxos := []ids.ID{GenerateID()}

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx0.Ins.Add(utxos[0])

	vtx0 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx0},
		height:       1,
		status:       choices.Processing,
	}

	tx1 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx1.Ins.Add(utxos[0])

	vtx1 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx1},
		height:       1,
		status:       choices.Processing,
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	}

	sm := ids.UniqueBag{}
	sm.Add(0, vtx1.id)
	sm.Add(1, vtx1.id)
	if err := avl.RecordPoll(sm); err != nil {
		t.Fatal(err)
	} else if avl.Finalized() {
		t.Fatalf("An avalanche instance finalized too early")
	} else if !ids.UnsortedEquals([]ids.ID{vtx1.id}, avl.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	} else if err := avl.RecordPoll(sm); err != nil {
		t.Fatal(err)
	} else if !avl.Finalized() {
		t.Fatalf("An avalanche instance finalized too late")
	} else if !ids.UnsortedEquals([]ids.ID{vtx1.id}, avl.Preferences().List()) {
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
			Metrics:      prometheus.NewRegistry(),
			K:            3,
			Alpha:        2,
			BetaVirtuous: 1,
			BetaRogue:    1,
		},
		Parents:   2,
		BatchSize: 1,
	}

	vts := []Vertex{&Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}, &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}}
	utxos := []ids.ID{GenerateID()}

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx0.Ins.Add(utxos[0])

	vtx0 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx0},
		height:       1,
		status:       choices.Processing,
	}

	tx1 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx1.Ins.Add(utxos[0])

	vtx1 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx1},
		height:       1,
		status:       choices.Processing,
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	}

	sm := ids.UniqueBag{}
	sm.Add(0, vtx0.id)
	sm.Add(1, vtx1.id)

	// Add Illegal Vote cast by Response 2
	sm.Add(2, vtx0.id)
	sm.Add(2, vtx1.id)

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
	vts := []Vertex{&Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}, &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}}
	utxos := []ids.ID{GenerateID(), GenerateID()}

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx0.Ins.Add(utxos[0])

	vtx0 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx0},
		height:       1,
		status:       choices.Processing,
	}

	tx1 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx1.Ins.Add(utxos[1])

	vtx1 := &Vtx{
		dependencies: []Vertex{vtx0},
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx1},
		height:       2,
		status:       choices.Processing,
	}

	vtx2 := &Vtx{
		dependencies: []Vertex{vtx1},
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx1},
		height:       3,
		status:       choices.Processing,
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx2); err != nil {
		t.Fatal(err)
	}

	sm1 := ids.UniqueBag{}
	sm1.Add(0, vtx0.id)
	sm1.Add(1, vtx2.id)
	if err := avl.RecordPoll(sm1); err != nil {
		t.Fatal(err)
	} else if avl.Finalized() {
		t.Fatalf("An avalanche instance finalized too early")
	} else if !ids.UnsortedEquals([]ids.ID{vtx2.id}, avl.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	} else if tx0.Status() != choices.Accepted {
		t.Fatalf("Tx should have been accepted")
	}

	sm2 := ids.UniqueBag{}
	sm2.Add(0, vtx2.id)
	sm2.Add(1, vtx2.id)
	if err := avl.RecordPoll(sm2); err != nil {
		t.Fatal(err)
	} else if !avl.Finalized() {
		t.Fatalf("An avalanche instance finalized too late")
	} else if !ids.UnsortedEquals([]ids.ID{vtx2.id}, avl.Preferences().List()) {
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
	vts := []Vertex{&Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}, &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}}
	utxos := []ids.ID{GenerateID()}

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx0.Ins.Add(utxos[0])

	vtx0 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx0},
		height:       1,
		status:       choices.Processing,
	}

	vtx1 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx0},
		height:       1,
		status:       choices.Processing,
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	}

	sm1 := ids.UniqueBag{}
	sm1.Add(0, vtx0.id) // peer 0 votes for the tx though vtx0
	sm1.Add(1, vtx1.id) // peer 1 votes for the tx though vtx1
	if err := avl.RecordPoll(sm1); err != nil {
		t.Fatal(err)
	} else if !avl.Finalized() {
		t.Fatalf("An avalanche instance finalized too late")
	} else if !ids.UnsortedEquals([]ids.ID{vtx0.id, vtx1.id}, avl.Preferences().List()) {
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
	vts := []Vertex{&Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}, &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}}
	utxos := []ids.ID{GenerateID(), GenerateID()}

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx0.Ins.Add(utxos[0])

	vtx0 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx0},
		height:       1,
		status:       choices.Processing,
	}

	tx1 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx1.Ins.Add(utxos[0])

	vtx1 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx1},
		height:       1,
		status:       choices.Processing,
	}

	tx2 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx2.Ins.Add(utxos[1])

	vtx2 := &Vtx{
		dependencies: []Vertex{vtx0},
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx2},
		height:       2,
		status:       choices.Processing,
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx2); err != nil {
		t.Fatal(err)
	}

	sm := ids.UniqueBag{}
	sm.Add(0, vtx1.id)
	sm.Add(1, vtx1.id)
	if err := avl.RecordPoll(sm); err != nil {
		t.Fatal(err)
	} else if avl.Finalized() {
		t.Fatalf("An avalanche instance finalized too early")
	} else if !ids.UnsortedEquals([]ids.ID{vtx1.id}, avl.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	} else if err := avl.RecordPoll(sm); err != nil {
		t.Fatal(err)
	} else if avl.Finalized() {
		t.Fatalf("An avalanche instance finalized too early")
	} else if !ids.UnsortedEquals([]ids.ID{vtx1.id}, avl.Preferences().List()) {
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
	} else if !ids.UnsortedEquals([]ids.ID{vtx1.id}, avl.Preferences().List()) {
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
	vts := []Vertex{&Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}, &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}}
	utxos := []ids.ID{GenerateID(), GenerateID()}

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	if virtuous := avl.Virtuous(); virtuous.Len() != 2 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vts[0].ID()) {
		t.Fatalf("Wrong virtuous")
	} else if !virtuous.Contains(vts[1].ID()) {
		t.Fatalf("Wrong virtuous")
	}

	tx0 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx0.Ins.Add(utxos[0])

	vtx0 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx0},
		height:       1,
		status:       choices.Processing,
	}

	tx1 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx1.Ins.Add(utxos[0])

	vtx1 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx1},
		height:       1,
		status:       choices.Processing,
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
	vts := []Vertex{&Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}, &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}}
	utxos := []ids.ID{GenerateID(), GenerateID()}

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx0.Ins.Add(utxos[0])

	vtx0 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx0},
		height:       1,
		status:       choices.Processing,
	}

	tx1 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx1.Ins.Add(utxos[0])

	vtx1 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx1},
		height:       1,
		status:       choices.Processing,
	}

	tx2 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx2.Ins.Add(utxos[1])

	vtx2 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx2},
		height:       2,
		status:       choices.Processing,
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
	sm.Add(0, vtx2.id)
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
	vts := []Vertex{&Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}, &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}}
	utxos := []ids.ID{GenerateID(), GenerateID()}

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx0.Ins.Add(utxos[0])

	vtx0 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx0},
		height:       1,
		status:       choices.Processing,
	}

	tx1 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx1.Ins.Add(utxos[0])

	vtx1 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx1},
		height:       1,
		status:       choices.Processing,
	}

	tx2 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx2.Ins.Add(utxos[1])

	vtx2 := &Vtx{
		dependencies: []Vertex{vtx0},
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx2},
		height:       2,
		status:       choices.Processing,
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
	sm.Add(0, vtx1.id)
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
	vts := []Vertex{&Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}}

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
		Validity:   errors.New(""),
	}

	vtx0 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx0},
		height:       1,
		status:       choices.Processing,
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
	vts := []Vertex{&Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}}
	utxos := []ids.ID{GenerateID()}

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
		Validity:   errors.New(""),
	}
	tx0.Ins.Add(utxos[0])

	vtx0 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx0},
		height:       1,
		status:       choices.Processing,
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	}

	votes := ids.UniqueBag{}
	votes.Add(0, vtx0.id)
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
	vts := []Vertex{&Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}}
	utxos := []ids.ID{GenerateID()}

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx0.Ins.Add(utxos[0])

	vtx0 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx0},
		height:       1,
		status:       choices.Processing,
		Validity:     errors.New(""),
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	}

	votes := ids.UniqueBag{}
	votes.Add(0, vtx0.id)
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
	vts := []Vertex{&Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}}
	utxos := []ids.ID{GenerateID()}

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx0.Ins.Add(utxos[0])

	vtx0 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx0},
		height:       1,
		status:       choices.Processing,
	}

	tx1 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx1.Ins.Add(utxos[0])

	vtx1 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx1},
		height:       1,
		status:       choices.Processing,
		Validity:     errors.New(""),
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	}

	votes := ids.UniqueBag{}
	votes.Add(0, vtx0.id)
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
	vts := []Vertex{&Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}}
	utxos := []ids.ID{GenerateID()}

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx0.Ins.Add(utxos[0])

	vtx0 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx0},
		height:       1,
		status:       choices.Processing,
	}

	tx1 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx1.Ins.Add(utxos[0])

	vtx1 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx1},
		height:       1,
		status:       choices.Processing,
		Validity:     errors.New(""),
	}

	vtx2 := &Vtx{
		dependencies: []Vertex{vtx1},
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx1},
		height:       1,
		status:       choices.Processing,
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx2); err != nil {
		t.Fatal(err)
	}

	votes := ids.UniqueBag{}
	votes.Add(0, vtx0.id)
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
	vts := []Vertex{&Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}}
	utxos := []ids.ID{GenerateID()}

	avl.Initialize(snow.DefaultContextTest(), params, vts)

	tx0 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx0.Ins.Add(utxos[0])

	vtx0 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx0},
		height:       1,
		status:       choices.Processing,
	}

	tx1 := &snowstorm.TestTx{
		Identifier: GenerateID(),
		Stat:       choices.Processing,
	}
	tx1.Ins.Add(utxos[0])

	vtx1 := &Vtx{
		dependencies: vts,
		id:           GenerateID(),
		txs:          []snowstorm.Tx{tx1},
		height:       1,
		status:       choices.Processing,
	}

	vtx2 := &Vtx{
		dependencies: []Vertex{vtx1},
		id:           GenerateID(),
		height:       1,
		status:       choices.Processing,
		Validity:     errors.New(""),
	}

	if err := avl.Add(vtx0); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx1); err != nil {
		t.Fatal(err)
	} else if err := avl.Add(vtx2); err != nil {
		t.Fatal(err)
	}

	votes := ids.UniqueBag{}
	votes.Add(0, vtx0.id)
	if err := avl.RecordPoll(votes); err == nil {
		t.Fatalf("Should have errored on vertex rejection")
	}
}
