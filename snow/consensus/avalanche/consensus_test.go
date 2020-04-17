// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"fmt"
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
)

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

	numProcessing := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: params.Namespace,
			Name:      "vtx_processing",
		})
	numAccepted := prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: params.Namespace,
			Name:      "vtx_accepted",
		})
	numRejected := prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: params.Namespace,
			Name:      "vtx_rejected",
		})

	params.Metrics.Register(numProcessing)
	params.Metrics.Register(numAccepted)
	params.Metrics.Register(numRejected)

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

	avl.Add(vtx0)

	if avl.Finalized() {
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

	avl.Add(vtx1)

	if avl.Finalized() {
		t.Fatalf("A non-empty avalanche instance is finalized")
	} else if !ids.UnsortedEquals([]ids.ID{vtx0.id}, avl.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	}

	avl.Add(vtx1)

	if avl.Finalized() {
		t.Fatalf("A non-empty avalanche instance is finalized")
	} else if !ids.UnsortedEquals([]ids.ID{vtx0.id}, avl.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	}

	avl.Add(vts[0])

	if avl.Finalized() {
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
	}

	avl.Add(vtx)

	if !avl.VertexIssued(vtx) {
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

	avl.Add(vtx)

	if !avl.TxIssued(tx1) {
		t.Fatalf("Tx reported as not issued")
	}
}
