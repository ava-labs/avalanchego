// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"math"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowball"
	"github.com/ava-labs/gecko/snow/consensus/snowstorm"
)

func TestTopologicalParams(t *testing.T) { ParamsTest(t, TopologicalFactory{}) }

func TestTopologicalAdd(t *testing.T) { AddTest(t, TopologicalFactory{}) }

func TestTopologicalVertexIssued(t *testing.T) { VertexIssuedTest(t, TopologicalFactory{}) }

func TestTopologicalTxIssued(t *testing.T) { TxIssuedTest(t, TopologicalFactory{}) }

func TestAvalancheVoting(t *testing.T) {
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

	ta := Topological{}
	ta.Initialize(snow.DefaultContextTest(), params, vts)

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

	ta.Add(vtx0)
	ta.Add(vtx1)

	sm := make(ids.UniqueBag)
	sm.Add(0, vtx1.id)
	sm.Add(1, vtx1.id)
	ta.RecordPoll(sm)

	if ta.Finalized() {
		t.Fatalf("An avalanche instance finalized too early")
	} else if !ids.UnsortedEquals([]ids.ID{vtx1.id}, ta.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	}

	ta.RecordPoll(sm)

	if !ta.Finalized() {
		t.Fatalf("An avalanche instance finalized too late")
	} else if !ids.UnsortedEquals([]ids.ID{vtx1.id}, ta.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	} else if tx0.Status() != choices.Rejected {
		t.Fatalf("Tx should have been rejected")
	} else if tx1.Status() != choices.Accepted {
		t.Fatalf("Tx should have been accepted")
	}
}

func TestAvalancheTransitiveVoting(t *testing.T) {
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

	ta := Topological{}
	ta.Initialize(snow.DefaultContextTest(), params, vts)

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

	ta.Add(vtx0)
	ta.Add(vtx1)
	ta.Add(vtx2)

	sm1 := make(ids.UniqueBag)
	sm1.Add(0, vtx0.id)
	sm1.Add(1, vtx2.id)
	ta.RecordPoll(sm1)

	if ta.Finalized() {
		t.Fatalf("An avalanche instance finalized too early")
	} else if !ids.UnsortedEquals([]ids.ID{vtx2.id}, ta.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	} else if tx0.Status() != choices.Accepted {
		t.Fatalf("Tx should have been accepted")
	}

	sm2 := make(ids.UniqueBag)
	sm2.Add(0, vtx2.id)
	sm2.Add(1, vtx2.id)
	ta.RecordPoll(sm2)

	if !ta.Finalized() {
		t.Fatalf("An avalanche instance finalized too late")
	} else if !ids.UnsortedEquals([]ids.ID{vtx2.id}, ta.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	} else if tx0.Status() != choices.Accepted {
		t.Fatalf("Tx should have been accepted")
	} else if tx1.Status() != choices.Accepted {
		t.Fatalf("Tx should have been accepted")
	}
}

func TestAvalancheSplitVoting(t *testing.T) {
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

	ta := Topological{}
	ta.Initialize(snow.DefaultContextTest(), params, vts)

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

	ta.Add(vtx0)
	ta.Add(vtx1)

	sm1 := make(ids.UniqueBag)
	sm1.Add(0, vtx0.id)
	sm1.Add(1, vtx1.id)
	ta.RecordPoll(sm1)

	if !ta.Finalized() {
		t.Fatalf("An avalanche instance finalized too late")
	} else if !ids.UnsortedEquals([]ids.ID{vtx0.id, vtx1.id}, ta.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	} else if tx0.Status() != choices.Accepted {
		t.Fatalf("Tx should have been accepted")
	}
}

func TestAvalancheTransitiveRejection(t *testing.T) {
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

	ta := Topological{}
	ta.Initialize(snow.DefaultContextTest(), params, vts)

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

	ta.Add(vtx0)
	ta.Add(vtx1)
	ta.Add(vtx2)

	sm := make(ids.UniqueBag)
	sm.Add(0, vtx1.id)
	sm.Add(1, vtx1.id)
	ta.RecordPoll(sm)

	if ta.Finalized() {
		t.Fatalf("An avalanche instance finalized too early")
	} else if !ids.UnsortedEquals([]ids.ID{vtx1.id}, ta.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	}

	ta.RecordPoll(sm)

	if ta.Finalized() {
		t.Fatalf("An avalanche instance finalized too early")
	} else if !ids.UnsortedEquals([]ids.ID{vtx1.id}, ta.Preferences().List()) {
		t.Fatalf("Initial frontier failed to be set")
	} else if tx0.Status() != choices.Rejected {
		t.Fatalf("Tx should have been rejected")
	} else if tx1.Status() != choices.Accepted {
		t.Fatalf("Tx should have been accepted")
	} else if tx2.Status() != choices.Processing {
		t.Fatalf("Tx should not have been decided")
	}

	ta.preferenceCache = make(map[[32]byte]bool)
	ta.virtuousCache = make(map[[32]byte]bool)

	ta.update(vtx2)
}

func TestAvalancheVirtuous(t *testing.T) {
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

	ta := Topological{}
	ta.Initialize(snow.DefaultContextTest(), params, vts)

	if virtuous := ta.Virtuous(); virtuous.Len() != 2 {
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

	ta.Add(vtx0)

	if virtuous := ta.Virtuous(); virtuous.Len() != 1 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vtx0.id) {
		t.Fatalf("Wrong virtuous")
	}

	ta.Add(vtx1)

	if virtuous := ta.Virtuous(); virtuous.Len() != 1 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vtx0.id) {
		t.Fatalf("Wrong virtuous")
	}

	ta.updateFrontiers()

	if virtuous := ta.Virtuous(); virtuous.Len() != 2 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vts[0].ID()) {
		t.Fatalf("Wrong virtuous")
	} else if !virtuous.Contains(vts[1].ID()) {
		t.Fatalf("Wrong virtuous")
	}

	ta.Add(vtx2)

	if virtuous := ta.Virtuous(); virtuous.Len() != 2 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vts[0].ID()) {
		t.Fatalf("Wrong virtuous")
	} else if !virtuous.Contains(vts[1].ID()) {
		t.Fatalf("Wrong virtuous")
	}

	ta.updateFrontiers()

	if virtuous := ta.Virtuous(); virtuous.Len() != 2 {
		t.Fatalf("Wrong number of virtuous.")
	} else if !virtuous.Contains(vts[0].ID()) {
		t.Fatalf("Wrong virtuous")
	} else if !virtuous.Contains(vts[1].ID()) {
		t.Fatalf("Wrong virtuous")
	}
}

func TestAvalancheIsVirtuous(t *testing.T) {
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

	ta := Topological{}
	ta.Initialize(snow.DefaultContextTest(), params, vts)

	if virtuous := ta.Virtuous(); virtuous.Len() != 2 {
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

	if !ta.IsVirtuous(tx0) {
		t.Fatalf("Should be virtuous.")
	} else if !ta.IsVirtuous(tx1) {
		t.Fatalf("Should be virtuous.")
	}

	ta.Add(vtx0)

	if !ta.IsVirtuous(tx0) {
		t.Fatalf("Should be virtuous.")
	} else if ta.IsVirtuous(tx1) {
		t.Fatalf("Should not be virtuous.")
	}

	ta.Add(vtx1)

	if ta.IsVirtuous(tx0) {
		t.Fatalf("Should not be virtuous.")
	} else if ta.IsVirtuous(tx1) {
		t.Fatalf("Should not be virtuous.")
	}
}

func TestAvalancheQuiesce(t *testing.T) {
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

	ta := Topological{}
	ta.Initialize(snow.DefaultContextTest(), params, vts)

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

	ta.Add(vtx0)

	if ta.Quiesce() {
		t.Fatalf("Shouldn't quiesce")
	}

	ta.Add(vtx1)

	if !ta.Quiesce() {
		t.Fatalf("Should quiesce")
	}

	ta.Add(vtx2)

	if ta.Quiesce() {
		t.Fatalf("Shouldn't quiesce")
	}

	sm := make(ids.UniqueBag)
	sm.Add(0, vtx2.id)
	ta.RecordPoll(sm)

	if !ta.Quiesce() {
		t.Fatalf("Should quiesce")
	}
}

func TestAvalancheOrphans(t *testing.T) {
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

	ta := Topological{}
	ta.Initialize(snow.DefaultContextTest(), params, vts)

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

	ta.Add(vtx0)

	if orphans := ta.Orphans(); orphans.Len() != 0 {
		t.Fatalf("Wrong number of orphans")
	}

	ta.Add(vtx1)

	if orphans := ta.Orphans(); orphans.Len() != 0 {
		t.Fatalf("Wrong number of orphans")
	}

	ta.Add(vtx2)

	if orphans := ta.Orphans(); orphans.Len() != 0 {
		t.Fatalf("Wrong number of orphans")
	}

	sm := make(ids.UniqueBag)
	sm.Add(0, vtx1.id)
	ta.RecordPoll(sm)

	if orphans := ta.Orphans(); orphans.Len() != 1 {
		t.Fatalf("Wrong number of orphans")
	} else if !orphans.Contains(tx2.ID()) {
		t.Fatalf("Wrong orphan")
	}
}
