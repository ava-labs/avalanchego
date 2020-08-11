// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"bytes"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/avalanche"
	"github.com/ava-labs/gecko/snow/consensus/snowball"
	"github.com/ava-labs/gecko/snow/consensus/snowstorm"
	"github.com/ava-labs/gecko/snow/engine/avalanche/vertex"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/validators"
)

var (
	errUnknownVertex = errors.New("unknown vertex")
	errFailedParsing = errors.New("failed parsing")
	errMissing       = errors.New("missing")

	Genesis = ids.GenerateTestID()
)

func TestEngineShutdown(t *testing.T) {
	config := DefaultConfig()
	vmShutdownCalled := false
	vm := &vertex.TestVM{}
	vm.ShutdownF = func() error { vmShutdownCalled = true; return nil }
	config.VM = vm

	transitive := &Transitive{}

	transitive.Initialize(config)
	transitive.finishBootstrapping()
	transitive.Finished = true
	transitive.Shutdown()
	if !vmShutdownCalled {
		t.Fatal("Shutting down the Transitive did not shutdown the VM")
	}
}

func TestEngineAdd(t *testing.T) {
	config := DefaultConfig()

	vdr := validators.GenerateRandomValidator(1)

	vals := validators.NewSet()
	config.Validators = vals

	vals.Add(vdr)

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)
	sender.CantGetAcceptedFrontier = false

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	manager.Default(true)

	manager.CantEdge = false

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	if !te.Context().ChainID.Equals(ids.Empty) {
		t.Fatalf("Wrong chain ID")
	}

	vtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []avalanche.Vertex{
			&avalanche.TestVertex{TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Unknown,
			}},
		},
		BytesV: []byte{1},
	}

	asked := new(bool)
	reqID := new(uint32)
	sender.GetF = func(inVdr ids.ShortID, requestID uint32, vtxID ids.ID) {
		*reqID = requestID
		if *asked {
			t.Fatalf("Asked multiple times")
		}
		*asked = true
		if !vdr.ID().Equals(inVdr) {
			t.Fatalf("Asking wrong validator for vertex")
		}
		if !vtx.ParentsV[0].ID().Equals(vtxID) {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	manager.ParseVertexF = func(b []byte) (avalanche.Vertex, error) {
		if !bytes.Equal(b, vtx.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return vtx, nil
	}

	te.Put(vdr.ID(), 0, vtx.ID(), vtx.Bytes())

	manager.ParseVertexF = nil

	if !*asked {
		t.Fatalf("Didn't ask for a missing vertex")
	}

	if len(te.vtxBlocked) != 1 {
		t.Fatalf("Should have been blocking on request")
	}

	manager.ParseVertexF = func(b []byte) (avalanche.Vertex, error) { return nil, errFailedParsing }

	te.Put(vdr.ID(), *reqID, vtx.ParentsV[0].ID(), nil)

	manager.ParseVertexF = nil

	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Should have finished blocking issue")
	}
}

func TestEngineQuery(t *testing.T) {
	config := DefaultConfig()

	vdr := validators.GenerateRandomValidator(1)

	vals := validators.NewSet()
	config.Validators = vals

	vals.Add(vdr)

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)
	sender.CantGetAcceptedFrontier = false

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	manager.Default(true)

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []avalanche.Vertex{gVtx, mVtx}
	utxos := []ids.ID{ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

	vtx0 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
		BytesV:   []byte{0, 1, 2, 3},
	}

	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVertexF = func(id ids.ID) (avalanche.Vertex, error) {
		switch {
		case id.Equals(gVtx.ID()):
			return gVtx, nil
		case id.Equals(mVtx.ID()):
			return mVtx, nil
		}

		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	vertexed := new(bool)
	manager.GetVertexF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if *vertexed {
			t.Fatalf("Sent multiple requests")
		}
		*vertexed = true
		if !vtxID.Equals(vtx0.ID()) {
			t.Fatalf("Wrong vertex requested")
		}
		return nil, errUnknownVertex
	}

	asked := new(bool)
	sender.GetF = func(inVdr ids.ShortID, _ uint32, vtxID ids.ID) {
		if *asked {
			t.Fatalf("Asked multiple times")
		}
		*asked = true
		if !vdr.ID().Equals(inVdr) {
			t.Fatalf("Asking wrong validator for vertex")
		}
		if !vtx0.ID().Equals(vtxID) {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	te.PullQuery(vdr.ID(), 0, vtx0.ID())
	if !*vertexed {
		t.Fatalf("Didn't request vertex")
	}
	if !*asked {
		t.Fatalf("Didn't request vertex from validator")
	}

	queried := new(bool)
	queryRequestID := new(uint32)
	sender.PushQueryF = func(inVdrs ids.ShortSet, requestID uint32, vtxID ids.ID, vtx []byte) {
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		*queryRequestID = requestID
		vdrSet := ids.ShortSet{}
		vdrSet.Add(vdr.ID())
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !vtx0.ID().Equals(vtxID) {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	chitted := new(bool)
	sender.ChitsF = func(inVdr ids.ShortID, _ uint32, prefs ids.Set) {
		if *chitted {
			t.Fatalf("Sent multiple chits")
		}
		*chitted = true
		if prefs.Len() != 1 || !prefs.Contains(vtx0.ID()) {
			t.Fatalf("Wrong chits preferences")
		}
	}

	manager.ParseVertexF = func(b []byte) (avalanche.Vertex, error) {
		if !bytes.Equal(b, vtx0.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return vtx0, nil
	}
	te.Put(vdr.ID(), 0, vtx0.ID(), vtx0.Bytes())
	manager.ParseVertexF = nil

	if !*queried {
		t.Fatalf("Didn't ask for preferences")
	}
	if !*chitted {
		t.Fatalf("Didn't provide preferences")
	}

	vtx1 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
		BytesV:   []byte{5, 4, 3, 2, 1, 9},
	}

	manager.GetVertexF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if vtxID.Equals(vtx0.ID()) {
			return &avalanche.TestVertex{
				TestDecidable: choices.TestDecidable{
					StatusV: choices.Unknown,
				},
			}, nil
		}
		if vtxID.Equals(vtx1.ID()) {
			return nil, errUnknownVertex
		}
		t.Fatalf("Wrong vertex requested")
		panic("Should have failed")
	}

	*asked = false
	sender.GetF = func(inVdr ids.ShortID, _ uint32, vtxID ids.ID) {
		if *asked {
			t.Fatalf("Asked multiple times")
		}
		*asked = true
		if !vdr.ID().Equals(inVdr) {
			t.Fatalf("Asking wrong validator for vertex")
		}
		if !vtx1.ID().Equals(vtxID) {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	s := ids.Set{}
	s.Add(vtx1.ID())
	te.Chits(vdr.ID(), *queryRequestID, s)

	*queried = false
	sender.PushQueryF = func(inVdrs ids.ShortSet, requestID uint32, vtxID ids.ID, vtx []byte) {
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		*queryRequestID = requestID
		vdrSet := ids.ShortSet{}
		vdrSet.Add(vdr.ID())
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !vtx1.ID().Equals(vtxID) {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	manager.ParseVertexF = func(b []byte) (avalanche.Vertex, error) {
		if !bytes.Equal(b, vtx1.Bytes()) {
			t.Fatalf("Wrong bytes")
		}

		manager.GetVertexF = func(vtxID ids.ID) (avalanche.Vertex, error) {
			if vtxID.Equals(vtx0.ID()) {
				return &avalanche.TestVertex{
					TestDecidable: choices.TestDecidable{
						StatusV: choices.Processing,
					},
				}, nil
			}
			if vtxID.Equals(vtx1.ID()) {
				return vtx1, nil
			}
			t.Fatalf("Wrong vertex requested")
			panic("Should have failed")
		}

		return vtx1, nil
	}
	te.Put(vdr.ID(), 0, vtx1.ID(), vtx1.Bytes())
	manager.ParseVertexF = nil

	if vtx0.Status() != choices.Accepted {
		t.Fatalf("Should have executed vertex")
	}
	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Should have finished blocking")
	}

	_ = te.polls.String() // Shouldn't panic

	te.QueryFailed(vdr.ID(), *queryRequestID)
	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Should have finished blocking")
	}
}

func TestEngineMultipleQuery(t *testing.T) {
	config := DefaultConfig()

	config.Params = avalanche.Parameters{
		Parameters: snowball.Parameters{
			Metrics:           prometheus.NewRegistry(),
			K:                 3,
			Alpha:             2,
			BetaVirtuous:      1,
			BetaRogue:         2,
			ConcurrentRepolls: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}

	vdr0 := validators.GenerateRandomValidator(1)
	vdr1 := validators.GenerateRandomValidator(1)
	vdr2 := validators.GenerateRandomValidator(1)

	vals := validators.NewSet()
	config.Validators = vals

	vals.Add(vdr0)
	vals.Add(vdr1)
	vals.Add(vdr2)

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)
	sender.CantGetAcceptedFrontier = false

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []avalanche.Vertex{gVtx, mVtx}
	utxos := []ids.ID{ids.GenerateTestID()}

	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVertexF = func(id ids.ID) (avalanche.Vertex, error) {
		switch {
		case id.Equals(gVtx.ID()):
			return gVtx, nil
		case id.Equals(mVtx.ID()):
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

	vtx0 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	queried := new(bool)
	queryRequestID := new(uint32)
	sender.PushQueryF = func(inVdrs ids.ShortSet, requestID uint32, vtxID ids.ID, vtx []byte) {
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		*queryRequestID = requestID
		vdrSet := ids.ShortSet{}
		vdrSet.Add(vdr0.ID(), vdr1.ID(), vdr2.ID())
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !vtx0.ID().Equals(vtxID) {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	te.insert(vtx0)

	vtx1 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	manager.GetVertexF = func(id ids.ID) (avalanche.Vertex, error) {
		switch {
		case id.Equals(gVtx.ID()):
			return gVtx, nil
		case id.Equals(mVtx.ID()):
			return mVtx, nil
		case id.Equals(vtx0.ID()):
			return vtx0, nil
		case id.Equals(vtx1.ID()):
			return nil, errUnknownVertex
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	asked := new(bool)
	reqID := new(uint32)
	sender.GetF = func(inVdr ids.ShortID, requestID uint32, vtxID ids.ID) {
		*reqID = requestID
		if *asked {
			t.Fatalf("Asked multiple times")
		}
		*asked = true
		if !vdr0.ID().Equals(inVdr) {
			t.Fatalf("Asking wrong validator for vertex")
		}
		if !vtx1.ID().Equals(vtxID) {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	s0 := ids.Set{}
	s0.Add(vtx0.ID())
	s0.Add(vtx1.ID())

	s2 := ids.Set{}
	s2.Add(vtx0.ID())

	te.Chits(vdr0.ID(), *queryRequestID, s0)
	te.QueryFailed(vdr1.ID(), *queryRequestID)
	te.Chits(vdr2.ID(), *queryRequestID, s2)

	// Should be dropped because the query was marked as failed
	te.Chits(vdr1.ID(), *queryRequestID, s0)

	te.GetFailed(vdr0.ID(), *reqID)

	if vtx0.Status() != choices.Accepted {
		t.Fatalf("Should have executed vertex")
	}
	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Should have finished blocking")
	}
}

func TestEngineBlockedIssue(t *testing.T) {
	config := DefaultConfig()

	vdr := validators.GenerateRandomValidator(1)

	vals := validators.NewSet()
	config.Validators = vals

	vals.Add(vdr)

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []avalanche.Vertex{gVtx, mVtx}
	utxos := []ids.ID{ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

	vtx0 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	vtx1 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []avalanche.Vertex{
			&avalanche.TestVertex{TestDecidable: choices.TestDecidable{
				IDV:     vtx0.IDV,
				StatusV: choices.Unknown,
			}},
		},
		HeightV: 1,
		TxsV:    []snowstorm.Tx{tx0},
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	te.insert(vtx1)

	vtx1.ParentsV[0] = vtx0
	te.insert(vtx0)

	if prefs := te.Consensus.Preferences(); prefs.Len() != 1 || !prefs.Contains(vtx1.ID()) {
		t.Fatalf("Should have issued vtx1")
	}
}

func TestEngineAbandonResponse(t *testing.T) {
	config := DefaultConfig()

	vdr := validators.GenerateRandomValidator(1)

	vals := validators.NewSet()
	config.Validators = vals

	vals.Add(vdr)

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []avalanche.Vertex{gVtx, mVtx}
	utxos := []ids.ID{ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

	vtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	manager.GetVertexF = func(id ids.ID) (avalanche.Vertex, error) { return nil, errUnknownVertex }

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	reqID := new(uint32)
	sender.GetF = func(vID ids.ShortID, requestID uint32, vtxID ids.ID) {
		*reqID = requestID
	}

	te.PullQuery(vdr.ID(), 0, vtx.ID())
	te.GetFailed(vdr.ID(), *reqID)

	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Should have removed blocking event")
	}
}

func TestEngineScheduleRepoll(t *testing.T) {
	config := DefaultConfig()

	vdr := validators.GenerateRandomValidator(1)

	vals := validators.NewSet()
	config.Validators = vals

	vals.Add(vdr)

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []avalanche.Vertex{gVtx, mVtx}
	utxos := []ids.ID{ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

	vtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	manager.Default(true)
	manager.CantEdge = false

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)
	sender.CantGetAcceptedFrontier = false

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	requestID := new(uint32)
	sender.PushQueryF = func(_ ids.ShortSet, reqID uint32, _ ids.ID, _ []byte) {
		*requestID = reqID
	}

	te.insert(vtx)

	sender.PushQueryF = nil

	repolled := new(bool)
	sender.PullQueryF = func(_ ids.ShortSet, _ uint32, vtxID ids.ID) {
		*repolled = true
		if !vtxID.Equals(vtx.ID()) {
			t.Fatalf("Wrong vertex queried")
		}
	}

	te.QueryFailed(vdr.ID(), *requestID)

	if !*repolled {
		t.Fatalf("Should have issued a noop")
	}
}

func TestEngineRejectDoubleSpendTx(t *testing.T) {
	config := DefaultConfig()

	config.Params.BatchSize = 2

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)
	sender.CantGetAcceptedFrontier = false

	vdr := validators.GenerateRandomValidator(1)

	vals := validators.NewSet()
	config.Validators = vals

	vals.Add(vdr)

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	manager.Default(true)

	vm := &vertex.TestVM{}
	vm.T = t
	config.VM = vm

	vm.Default(true)

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	gTx := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	utxos := []ids.ID{ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx0.InputIDsV.Add(utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx1.InputIDsV.Add(utxos[0])

	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVertexF = func(id ids.ID) (avalanche.Vertex, error) {
		switch {
		case id.Equals(gVtx.ID()):
			return gVtx, nil
		case id.Equals(mVtx.ID()):
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}
	manager.BuildVertexF = func(_ ids.Set, txs []snowstorm.Tx) (avalanche.Vertex, error) {
		return &avalanche.TestVertex{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Processing,
			},
			ParentsV: []avalanche.Vertex{gVtx, mVtx},
			HeightV:  1,
			TxsV:     txs,
			BytesV:   []byte{1},
		}, nil
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	sender.CantPushQuery = false

	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx0, tx1} }
	te.Notify(common.PendingTxs)
}

func TestEngineRejectDoubleSpendIssuedTx(t *testing.T) {
	config := DefaultConfig()

	config.Params.BatchSize = 2

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)
	sender.CantGetAcceptedFrontier = false

	vdr := validators.GenerateRandomValidator(1)

	vals := validators.NewSet()
	config.Validators = vals

	vals.Add(vdr)

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	manager.Default(true)

	vm := &vertex.TestVM{}
	vm.T = t
	config.VM = vm

	vm.Default(true)

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	gTx := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	utxos := []ids.ID{ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx0.InputIDsV.Add(utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx1.InputIDsV.Add(utxos[0])

	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVertexF = func(id ids.ID) (avalanche.Vertex, error) {
		switch {
		case id.Equals(gVtx.ID()):
			return gVtx, nil
		case id.Equals(mVtx.ID()):
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	manager.BuildVertexF = func(_ ids.Set, txs []snowstorm.Tx) (avalanche.Vertex, error) {
		return &avalanche.TestVertex{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Processing,
			},
			ParentsV: []avalanche.Vertex{gVtx, mVtx},
			HeightV:  1,
			TxsV:     txs,
			BytesV:   []byte{1},
		}, nil
	}

	sender.CantPushQuery = false

	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx0} }
	te.Notify(common.PendingTxs)

	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx1} }
	te.Notify(common.PendingTxs)
}

func TestEngineIssueRepoll(t *testing.T) {
	config := DefaultConfig()

	config.Params.BatchSize = 2

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)
	sender.CantGetAcceptedFrontier = false

	vdr := validators.GenerateRandomValidator(1)

	vals := validators.NewSet()
	config.Validators = vals

	vals.Add(vdr)

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	manager.Default(true)

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVertexF = func(id ids.ID) (avalanche.Vertex, error) {
		switch {
		case id.Equals(gVtx.ID()):
			return gVtx, nil
		case id.Equals(mVtx.ID()):
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	sender.PullQueryF = func(vdrs ids.ShortSet, _ uint32, vtxID ids.ID) {
		vdrSet := ids.ShortSet{}
		vdrSet.Add(vdr.ID())
		if !vdrs.Equals(vdrSet) {
			t.Fatalf("Wrong query recipients")
		}
		if !vtxID.Equals(gVtx.ID()) && !vtxID.Equals(mVtx.ID()) {
			t.Fatalf("Unknown re-query")
		}
	}

	te.repoll()
}

func TestEngineReissue(t *testing.T) {
	config := DefaultConfig()
	config.Params.BatchSize = 2
	config.Params.BetaVirtuous = 5
	config.Params.BetaRogue = 5

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)
	sender.CantGetAcceptedFrontier = false

	vdr := validators.GenerateRandomValidator(1)

	vals := validators.NewSet()
	config.Validators = vals

	vals.Add(vdr)

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	manager.Default(true)

	vm := &vertex.TestVM{}
	vm.T = t
	config.VM = vm

	vm.Default(true)

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	gTx := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx0.InputIDsV.Add(utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx1.InputIDsV.Add(utxos[1])

	tx2 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx2.InputIDsV.Add(utxos[1])

	tx3 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx3.InputIDsV.Add(utxos[0])

	vtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []avalanche.Vertex{gVtx, mVtx},
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx2},
		BytesV:   []byte{42},
	}

	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVertexF = func(id ids.ID) (avalanche.Vertex, error) {
		switch {
		case id.Equals(gVtx.ID()):
			return gVtx, nil
		case id.Equals(mVtx.ID()):
			return mVtx, nil
		case id.Equals(vtx.ID()):
			return vtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	lastVtx := new(avalanche.TestVertex)
	manager.BuildVertexF = func(_ ids.Set, txs []snowstorm.Tx) (avalanche.Vertex, error) {
		lastVtx = &avalanche.TestVertex{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Processing,
			},
			ParentsV: []avalanche.Vertex{gVtx, mVtx},
			HeightV:  1,
			TxsV:     txs,
			BytesV:   []byte{1},
		}
		return lastVtx, nil
	}

	vm.GetTxF = func(id ids.ID) (snowstorm.Tx, error) {
		if !id.Equals(tx0.ID()) {
			t.Fatalf("Wrong tx")
		}
		return tx0, nil
	}

	queryRequestID := new(uint32)
	sender.PushQueryF = func(_ ids.ShortSet, requestID uint32, _ ids.ID, _ []byte) {
		*queryRequestID = requestID
	}

	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx0, tx1} }
	te.Notify(common.PendingTxs)

	manager.ParseVertexF = func(b []byte) (avalanche.Vertex, error) {
		if !bytes.Equal(b, vtx.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return vtx, nil
	}
	te.Put(vdr.ID(), 0, vtx.ID(), vtx.Bytes())
	manager.ParseVertexF = nil

	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx3} }
	te.Notify(common.PendingTxs)

	s := ids.Set{}
	s.Add(vtx.ID())
	te.Chits(vdr.ID(), *queryRequestID, s)

	if len(lastVtx.TxsV) != 1 || !lastVtx.TxsV[0].ID().Equals(tx0.ID()) {
		t.Fatalf("Should have re-issued the tx")
	}
}

func TestEngineLargeIssue(t *testing.T) {
	config := DefaultConfig()
	config.Params.BatchSize = 1
	config.Params.BetaVirtuous = 5
	config.Params.BetaRogue = 5

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)
	sender.CantGetAcceptedFrontier = false

	vdr := validators.GenerateRandomValidator(1)

	vals := validators.NewSet()
	config.Validators = vals

	vals.Add(vdr)

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	manager.Default(true)

	vm := &vertex.TestVM{}
	vm.T = t
	config.VM = vm

	vm.Default(true)

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	gTx := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx0.InputIDsV.Add(utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx1.InputIDsV.Add(utxos[1])

	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVertexF = func(id ids.ID) (avalanche.Vertex, error) {
		switch {
		case id.Equals(gVtx.ID()):
			return gVtx, nil
		case id.Equals(mVtx.ID()):
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	lastVtx := new(avalanche.TestVertex)
	manager.BuildVertexF = func(_ ids.Set, txs []snowstorm.Tx) (avalanche.Vertex, error) {
		lastVtx = &avalanche.TestVertex{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Processing,
			},
			ParentsV: []avalanche.Vertex{gVtx, mVtx},
			HeightV:  1,
			TxsV:     txs,
			BytesV:   []byte{1},
		}
		return lastVtx, nil
	}

	sender.CantPushQuery = false

	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx0, tx1} }
	te.Notify(common.PendingTxs)

	if len(lastVtx.TxsV) != 1 || !lastVtx.TxsV[0].ID().Equals(tx1.ID()) {
		t.Fatalf("Should have issued txs differently")
	}
}

func TestEngineGetVertex(t *testing.T) {
	config := DefaultConfig()

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)
	sender.CantGetAcceptedFrontier = false

	vdr := validators.GenerateRandomValidator(1)

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	manager.Default(true)

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVertexF = func(id ids.ID) (avalanche.Vertex, error) {
		switch {
		case id.Equals(gVtx.ID()):
			return gVtx, nil
		case id.Equals(mVtx.ID()):
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	sender.PutF = func(v ids.ShortID, _ uint32, vtxID ids.ID, vtx []byte) {
		if !v.Equals(vdr.ID()) {
			t.Fatalf("Wrong validator")
		}
		if !mVtx.ID().Equals(vtxID) {
			t.Fatalf("Wrong vertex")
		}
	}

	te.Get(vdr.ID(), 0, mVtx.ID())
}

func TestEngineInsufficientValidators(t *testing.T) {
	config := DefaultConfig()

	vals := validators.NewSet()
	config.Validators = vals

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)
	sender.CantGetAcceptedFrontier = false

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	manager.Default(true)

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []avalanche.Vertex{gVtx, mVtx}

	vtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		BytesV:   []byte{0, 1, 2, 3},
	}

	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVertexF = func(id ids.ID) (avalanche.Vertex, error) {
		switch {
		case id.Equals(gVtx.ID()):
			return gVtx, nil
		case id.Equals(mVtx.ID()):
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	queried := new(bool)
	sender.PushQueryF = func(inVdrs ids.ShortSet, _ uint32, vtxID ids.ID, vtx []byte) {
		*queried = true
	}

	te.insert(vtx)

	if *queried {
		t.Fatalf("Unknown query")
	}
}

func TestEnginePushGossip(t *testing.T) {
	config := DefaultConfig()

	vdr := validators.GenerateRandomValidator(1)
	vals := validators.NewSet()
	vals.Add(vdr)
	config.Validators = vals

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)
	sender.CantGetAcceptedFrontier = false

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	manager.Default(true)

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []avalanche.Vertex{gVtx, mVtx}

	vtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		BytesV:   []byte{0, 1, 2, 3},
	}

	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVertexF = func(id ids.ID) (avalanche.Vertex, error) {
		switch {
		case id.Equals(gVtx.ID()):
			return gVtx, nil
		case id.Equals(mVtx.ID()):
			return mVtx, nil
		case id.Equals(vtx.ID()):
			return vtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	requested := new(bool)
	sender.GetF = func(vdr ids.ShortID, _ uint32, vtxID ids.ID) {
		*requested = true
	}

	manager.ParseVertexF = func(b []byte) (avalanche.Vertex, error) {
		if bytes.Equal(b, vtx.BytesV) {
			return vtx, nil
		}
		t.Fatalf("Unknown vertex bytes")
		panic("Should have errored")
	}

	sender.CantPushQuery = false
	sender.CantChits = false
	te.PushQuery(vdr.ID(), 0, vtx.ID(), vtx.Bytes())

	if *requested {
		t.Fatalf("Shouldn't have requested the vertex")
	}
}

func TestEngineSingleQuery(t *testing.T) {
	config := DefaultConfig()

	vdr := validators.GenerateRandomValidator(1)
	vals := validators.NewSet()
	vals.Add(vdr)
	config.Validators = vals

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)
	sender.CantGetAcceptedFrontier = false

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	manager.Default(true)

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []avalanche.Vertex{gVtx, mVtx}

	vtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		BytesV:   []byte{0, 1, 2, 3},
	}

	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVertexF = func(id ids.ID) (avalanche.Vertex, error) {
		switch {
		case id.Equals(gVtx.ID()):
			return gVtx, nil
		case id.Equals(mVtx.ID()):
			return mVtx, nil
		case id.Equals(vtx.ID()):
			return vtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	sender.CantPushQuery = false
	sender.CantPullQuery = false

	te.insert(vtx)
}

func TestEngineParentBlockingInsert(t *testing.T) {
	config := DefaultConfig()

	vdr := validators.GenerateRandomValidator(1)
	vals := validators.NewSet()
	vals.Add(vdr)
	config.Validators = vals

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)
	sender.CantGetAcceptedFrontier = false

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	manager.Default(true)

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []avalanche.Vertex{gVtx, mVtx}

	missingVtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentsV: vts,
		HeightV:  1,
		BytesV:   []byte{0, 1, 2, 3},
	}

	parentVtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []avalanche.Vertex{missingVtx},
		HeightV:  2,
		BytesV:   []byte{0, 1, 2, 3},
	}

	blockingVtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []avalanche.Vertex{parentVtx},
		HeightV:  3,
		BytesV:   []byte{0, 1, 2, 3},
	}

	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVertexF = func(id ids.ID) (avalanche.Vertex, error) {
		switch {
		case id.Equals(gVtx.ID()):
			return gVtx, nil
		case id.Equals(mVtx.ID()):
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	te.insert(parentVtx)
	te.insert(blockingVtx)

	if len(te.vtxBlocked) != 2 {
		t.Fatalf("Both inserts should be blocking")
	}

	sender.CantPushQuery = false

	missingVtx.StatusV = choices.Processing
	te.insert(missingVtx)

	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Both inserts should not longer be blocking")
	}
}

func TestEngineBlockingChitRequest(t *testing.T) {
	config := DefaultConfig()

	vdr := validators.GenerateRandomValidator(1)
	vals := validators.NewSet()
	vals.Add(vdr)
	config.Validators = vals

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)
	sender.CantGetAcceptedFrontier = false

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	manager.Default(true)

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []avalanche.Vertex{gVtx, mVtx}

	missingVtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentsV: vts,
		HeightV:  1,
		BytesV:   []byte{0, 1, 2, 3},
	}

	parentVtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []avalanche.Vertex{missingVtx},
		HeightV:  2,
		BytesV:   []byte{1, 1, 2, 3},
	}

	blockingVtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []avalanche.Vertex{parentVtx},
		HeightV:  3,
		BytesV:   []byte{2, 1, 2, 3},
	}

	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVertexF = func(id ids.ID) (avalanche.Vertex, error) {
		switch {
		case id.Equals(gVtx.ID()):
			return gVtx, nil
		case id.Equals(mVtx.ID()):
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	te.insert(parentVtx)

	manager.GetVertexF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(blockingVtx.ID()):
			return blockingVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}
	manager.ParseVertexF = func(b []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(b, blockingVtx.Bytes()):
			return blockingVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te.PushQuery(vdr.ID(), 0, blockingVtx.ID(), blockingVtx.Bytes())

	if len(te.vtxBlocked) != 3 {
		t.Fatalf("Both inserts and the query should be blocking")
	}

	sender.CantPushQuery = false
	sender.CantChits = false

	missingVtx.StatusV = choices.Processing
	te.insert(missingVtx)

	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Both inserts should not longer be blocking")
	}
}

func TestEngineBlockingChitResponse(t *testing.T) {
	config := DefaultConfig()

	vdr := validators.GenerateRandomValidator(1)
	vals := validators.NewSet()
	vals.Add(vdr)
	config.Validators = vals

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)
	sender.CantGetAcceptedFrontier = false

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	manager.Default(true)

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []avalanche.Vertex{gVtx, mVtx}

	issuedVtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		BytesV:   []byte{0, 1, 2, 3},
	}

	missingVtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentsV: vts,
		HeightV:  1,
		BytesV:   []byte{1, 1, 2, 3},
	}

	blockingVtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []avalanche.Vertex{missingVtx},
		HeightV:  2,
		BytesV:   []byte{2, 1, 2, 3},
	}

	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVertexF = func(id ids.ID) (avalanche.Vertex, error) {
		switch {
		case id.Equals(gVtx.ID()):
			return gVtx, nil
		case id.Equals(mVtx.ID()):
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	te.insert(blockingVtx)

	queryRequestID := new(uint32)
	sender.PushQueryF = func(inVdrs ids.ShortSet, requestID uint32, vtxID ids.ID, vtx []byte) {
		*queryRequestID = requestID
		vdrSet := ids.ShortSet{}
		vdrSet.Add(vdr.ID())
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !issuedVtx.ID().Equals(vtxID) {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	te.insert(issuedVtx)

	manager.GetVertexF = func(id ids.ID) (avalanche.Vertex, error) {
		switch {
		case id.Equals(blockingVtx.ID()):
			return blockingVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	voteSet := ids.Set{}
	voteSet.Add(blockingVtx.ID())
	te.Chits(vdr.ID(), *queryRequestID, voteSet)

	if len(te.vtxBlocked) != 2 {
		t.Fatalf("The insert should be blocking, as well as the chit response")
	}

	sender.PushQueryF = nil
	sender.CantPushQuery = false
	sender.CantChits = false

	missingVtx.StatusV = choices.Processing
	te.insert(missingVtx)

	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Both inserts should not longer be blocking")
	}
}

func TestEngineMissingTx(t *testing.T) {
	config := DefaultConfig()

	vdr := validators.GenerateRandomValidator(1)
	vals := validators.NewSet()
	vals.Add(vdr)
	config.Validators = vals

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)
	sender.CantGetAcceptedFrontier = false

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	manager.Default(true)

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []avalanche.Vertex{gVtx, mVtx}

	issuedVtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		BytesV:   []byte{0, 1, 2, 3},
	}

	missingVtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentsV: vts,
		HeightV:  1,
		BytesV:   []byte{1, 1, 2, 3},
	}

	blockingVtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []avalanche.Vertex{missingVtx},
		HeightV:  2,
		BytesV:   []byte{2, 1, 2, 3},
	}

	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVertexF = func(id ids.ID) (avalanche.Vertex, error) {
		switch {
		case id.Equals(gVtx.ID()):
			return gVtx, nil
		case id.Equals(mVtx.ID()):
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	te.insert(blockingVtx)

	queryRequestID := new(uint32)
	sender.PushQueryF = func(inVdrs ids.ShortSet, requestID uint32, vtxID ids.ID, vtx []byte) {
		*queryRequestID = requestID
		vdrSet := ids.ShortSet{}
		vdrSet.Add(vdr.ID())
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !issuedVtx.ID().Equals(vtxID) {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	te.insert(issuedVtx)

	manager.GetVertexF = func(id ids.ID) (avalanche.Vertex, error) {
		switch {
		case id.Equals(blockingVtx.ID()):
			return blockingVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	voteSet := ids.Set{}
	voteSet.Add(blockingVtx.ID())
	te.Chits(vdr.ID(), *queryRequestID, voteSet)

	if len(te.vtxBlocked) != 2 {
		t.Fatalf("The insert should be blocking, as well as the chit response")
	}

	sender.PushQueryF = nil
	sender.CantPushQuery = false
	sender.CantChits = false

	missingVtx.StatusV = choices.Processing
	te.insert(missingVtx)

	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Both inserts should not longer be blocking")
	}
}

func TestEngineIssueBlockingTx(t *testing.T) {
	config := DefaultConfig()

	vdr := validators.GenerateRandomValidator(1)

	vals := validators.NewSet()
	config.Validators = vals

	vals.Add(vdr)

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []avalanche.Vertex{gVtx}
	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{tx0},
	}
	tx1.InputIDsV.Add(utxos[1])

	vtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0, tx1},
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	te.insert(vtx)

	if prefs := te.Consensus.Preferences(); !prefs.Contains(vtx.ID()) {
		t.Fatalf("Vertex should be preferred")
	}
}

func TestEngineReissueAbortedVertex(t *testing.T) {
	config := DefaultConfig()

	vdr := validators.GenerateRandomValidator(1)
	vdrID := vdr.ID()

	vals := validators.NewSet()
	config.Validators = vals

	vals.Add(vdr)

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)
	sender.CantGetAcceptedFrontier = false

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	manager.Default(true)

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []avalanche.Vertex{gVtx}

	vtxID0 := ids.GenerateTestID()
	vtxID1 := ids.GenerateTestID()

	vtxBytes0 := []byte{0}
	vtxBytes1 := []byte{1}

	vtx0 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID0,
			StatusV: choices.Unknown,
		},
		ParentsV: vts,
		HeightV:  1,
		BytesV:   vtxBytes0,
	}
	vtx1 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID1,
			StatusV: choices.Processing,
		},
		ParentsV: []avalanche.Vertex{vtx0},
		HeightV:  2,
		BytesV:   vtxBytes1,
	}

	manager.EdgeF = func() []ids.ID {
		return []ids.ID{gVtx.ID()}
	}

	manager.GetVertexF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(gVtx.ID()):
			return gVtx, nil
		}
		t.Fatalf("Unknown vertex requested")
		panic("Unknown vertex requested")
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	manager.EdgeF = nil
	manager.GetVertexF = nil

	requestID := new(uint32)
	sender.GetF = func(vID ids.ShortID, reqID uint32, vtxID ids.ID) {
		*requestID = reqID
	}
	manager.ParseVertexF = func(b []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(b, vtxBytes1):
			return vtx1, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}
	manager.GetVertexF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID1):
			return vtx1, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}

	te.PushQuery(vdrID, 0, vtxID1, vtx1.Bytes())

	sender.GetF = nil
	manager.ParseVertexF = nil

	te.GetFailed(vdrID, *requestID)

	requested := new(bool)
	sender.GetF = func(_ ids.ShortID, _ uint32, vtxID ids.ID) {
		if vtxID.Equals(vtxID0) {
			*requested = true
		}
	}
	manager.GetVertexF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID1):
			return vtx1, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}

	te.PullQuery(vdrID, 0, vtxID1)

	if !*requested {
		t.Fatalf("Should have requested the missing vertex")
	}
}

func TestEngineBootstrappingIntoConsensus(t *testing.T) {
	config := DefaultConfig()

	vdr := validators.GenerateRandomValidator(1)
	vdrID := vdr.ID()

	vals := validators.NewSet()
	config.Validators = vals
	config.Beacons = vals

	vals.Add(vdr)

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	manager.Default(true)

	vm := &vertex.TestVM{}
	vm.T = t
	config.VM = vm

	vm.Default(true)

	vm.CantBootstrapping = false
	vm.CantBootstrapped = false

	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	txID0 := ids.GenerateTestID()
	txID1 := ids.GenerateTestID()

	txBytes0 := []byte{0}
	txBytes1 := []byte{1}

	tx0 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     txID0,
			StatusV: choices.Processing,
		},
		BytesV: txBytes0,
	}
	tx0.InputIDsV.Add(utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     txID1,
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{tx0},
		BytesV:        txBytes1,
	}
	tx1.InputIDsV.Add(utxos[1])

	vtxID0 := ids.GenerateTestID()
	vtxID1 := ids.GenerateTestID()

	vtxBytes0 := []byte{2}
	vtxBytes1 := []byte{3}

	vtx0 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID0,
			StatusV: choices.Processing,
		},
		HeightV: 1,
		TxsV:    []snowstorm.Tx{tx0},
		BytesV:  vtxBytes0,
	}
	vtx1 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID1,
			StatusV: choices.Processing,
		},
		ParentsV: []avalanche.Vertex{vtx0},
		HeightV:  2,
		TxsV:     []snowstorm.Tx{tx1},
		BytesV:   vtxBytes1,
	}

	requested := new(bool)
	requestID := new(uint32)
	sender.GetAcceptedFrontierF = func(vdrs ids.ShortSet, reqID uint32) {
		if vdrs.Len() != 1 {
			t.Fatalf("Should have requested from the validators")
		}
		if !vdrs.Contains(vdrID) {
			t.Fatalf("Should have requested from %s", vdrID)
		}
		*requested = true
		*requestID = reqID
	}

	te := &Transitive{}
	te.Initialize(config)
	te.Startup()

	sender.GetAcceptedFrontierF = nil

	if !*requested {
		t.Fatalf("Should have requested from the validators during Initialize")
	}

	acceptedFrontier := ids.Set{}
	acceptedFrontier.Add(vtxID0)

	*requested = false
	sender.GetAcceptedF = func(vdrs ids.ShortSet, reqID uint32, proposedAccepted ids.Set) {
		if vdrs.Len() != 1 {
			t.Fatalf("Should have requested from the validators")
		}
		if !vdrs.Contains(vdrID) {
			t.Fatalf("Should have requested from %s", vdrID)
		}
		if !acceptedFrontier.Equals(proposedAccepted) {
			t.Fatalf("Wrong proposedAccepted vertices.\nExpected: %s\nGot: %s", acceptedFrontier, proposedAccepted)
		}
		*requested = true
		*requestID = reqID
	}

	te.AcceptedFrontier(vdrID, *requestID, acceptedFrontier)

	if !*requested {
		t.Fatalf("Should have requested from the validators during AcceptedFrontier")
	}

	manager.GetVertexF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID0):
			return nil, errMissing
		}
		t.Fatalf("Unknown vertex requested")
		panic("Unknown vertex requested")
	}

	sender.GetAncestorsF = func(inVdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if !vdrID.Equals(inVdr) {
			t.Fatalf("Asking wrong validator for vertex")
		}
		if !vtx0.ID().Equals(vtxID) {
			t.Fatalf("Asking for wrong vertex")
		}
		*requestID = reqID
	}

	te.Accepted(vdrID, *requestID, acceptedFrontier)

	manager.GetVertexF = nil
	sender.GetF = nil

	vm.ParseTxF = func(b []byte) (snowstorm.Tx, error) {
		switch {
		case bytes.Equal(b, txBytes0):
			return tx0, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}
	manager.ParseVertexF = func(b []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(b, vtxBytes0):
			return vtx0, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{vtxID0}
	}
	manager.GetVertexF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID0):
			return vtx0, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}

	te.MultiPut(vdrID, *requestID, [][]byte{vtxBytes0})

	vm.ParseTxF = nil
	manager.ParseVertexF = nil
	manager.EdgeF = nil
	manager.GetVertexF = nil

	if tx0.Status() != choices.Accepted {
		t.Fatalf("Should have accepted %s", txID0)
	}
	if vtx0.Status() != choices.Accepted {
		t.Fatalf("Should have accepted %s", vtxID0)
	}

	manager.ParseVertexF = func(b []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(b, vtxBytes1):
			return vtx1, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}
	sender.ChitsF = func(inVdr ids.ShortID, _ uint32, chits ids.Set) {
		if !inVdr.Equals(vdrID) {
			t.Fatalf("Sent to the wrong validator")
		}

		expected := ids.Set{}
		expected.Add(vtxID1)

		if !expected.Equals(chits) {
			t.Fatalf("Returned wrong chits")
		}
	}
	sender.PushQueryF = func(vdrs ids.ShortSet, _ uint32, vtxID ids.ID, vtx []byte) {
		if vdrs.Len() != 1 {
			t.Fatalf("Should have requested from the validators")
		}
		if !vdrs.Contains(vdrID) {
			t.Fatalf("Should have requested from %s", vdrID)
		}

		if !vtxID1.Equals(vtxID) {
			t.Fatalf("Sent wrong query ID")
		}
		if !bytes.Equal(vtxBytes1, vtx) {
			t.Fatalf("Sent wrong query bytes")
		}
	}
	manager.GetVertexF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID1):
			return vtx1, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}

	te.PushQuery(vdrID, 0, vtxID1, vtxBytes1)

	manager.ParseVertexF = nil
	sender.ChitsF = nil
	sender.PushQueryF = nil
	manager.GetVertexF = nil
}

func TestEngineUndeclaredDependencyDeadlock(t *testing.T) {
	config := DefaultConfig()

	vdr := validators.GenerateRandomValidator(1)

	vals := validators.NewSet()
	config.Validators = vals

	vals.Add(vdr)

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []avalanche.Vertex{gVtx}
	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		VerifyV: errors.New(""),
	}
	tx1.InputIDsV.Add(utxos[1])

	vtx0 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}
	vtx1 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []avalanche.Vertex{vtx0},
		HeightV:  2,
		TxsV:     []snowstorm.Tx{tx1},
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	sender := &common.SenderTest{}
	sender.T = t
	te.Config.Sender = sender

	reqID := new(uint32)
	sender.PushQueryF = func(_ ids.ShortSet, requestID uint32, _ ids.ID, _ []byte) {
		*reqID = requestID
	}

	te.insert(vtx0)

	sender.PushQueryF = func(ids.ShortSet, uint32, ids.ID, []byte) {
		t.Fatalf("should have failed verification")
	}

	te.insert(vtx1)

	manager.GetVertexF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtx0.ID()):
			return vtx0, nil
		case vtxID.Equals(vtx1.ID()):
			return vtx1, nil
		}
		return nil, errors.New("Unknown vtx")
	}

	votes := ids.Set{}
	votes.Add(vtx1.ID())
	te.Chits(vdr.ID(), *reqID, votes)

	if status := vtx0.Status(); status != choices.Accepted {
		t.Fatalf("should have accepted the vertex due to transitive voting")
	}
}

func TestEnginePartiallyValidVertex(t *testing.T) {
	config := DefaultConfig()

	vdr := validators.GenerateRandomValidator(1)

	vals := validators.NewSet()
	config.Validators = vals

	vals.Add(vdr)

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []avalanche.Vertex{gVtx}
	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		VerifyV: errors.New(""),
	}
	tx1.InputIDsV.Add(utxos[1])

	vtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0, tx1},
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	expectedVtxID := ids.GenerateTestID()
	manager.BuildVertexF = func(_ ids.Set, txs []snowstorm.Tx) (avalanche.Vertex, error) {
		return &avalanche.TestVertex{
			TestDecidable: choices.TestDecidable{
				IDV:     expectedVtxID,
				StatusV: choices.Processing,
			},
			ParentsV: vts,
			HeightV:  1,
			TxsV:     txs,
			BytesV:   []byte{1},
		}, nil
	}

	sender := &common.SenderTest{}
	sender.T = t
	te.Config.Sender = sender

	sender.PushQueryF = func(_ ids.ShortSet, _ uint32, vtxID ids.ID, _ []byte) {
		if !expectedVtxID.Equals(vtxID) {
			t.Fatalf("wrong vertex queried")
		}
	}

	te.insert(vtx)
}

func TestEngineGossip(t *testing.T) {
	config := DefaultConfig()

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID()} }
	manager.GetVertexF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(gVtx.ID()):
			return gVtx, nil
		}
		t.Fatal(errUnknownVertex)
		return nil, errUnknownVertex
	}

	called := new(bool)
	sender.GossipF = func(vtxID ids.ID, vtxBytes []byte) {
		*called = true
		switch {
		case !vtxID.Equals(gVtx.ID()):
			t.Fatal(errUnknownVertex)
		}
		switch {
		case !bytes.Equal(vtxBytes, gVtx.Bytes()):
			t.Fatal(errUnknownVertex)
		}
	}

	te.Gossip()

	if !*called {
		t.Fatalf("Should have gossiped the vertex")
	}
}

func TestEngineInvalidVertexIgnoredFromUnexpectedPeer(t *testing.T) {
	config := DefaultConfig()

	vdr := validators.GenerateRandomValidator(1)
	secondVdr := validators.GenerateRandomValidator(1)

	vals := validators.NewSet()
	config.Validators = vals

	vals.Add(vdr)
	vals.Add(secondVdr)

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	gVtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		},
		BytesV: []byte{0},
	}

	vts := []avalanche.Vertex{gVtx}
	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

	tx1 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx1.InputIDsV.Add(utxos[1])

	vtx0 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
		BytesV:   []byte{1},
	}
	vtx1 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []avalanche.Vertex{vtx0},
		HeightV:  2,
		TxsV:     []snowstorm.Tx{tx1},
		BytesV:   []byte{2},
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	parsed := new(bool)
	manager.ParseVertexF = func(b []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(b, vtx1.Bytes()):
			*parsed = true
			return vtx1, nil
		}
		return nil, errUnknownVertex
	}

	manager.GetVertexF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if !*parsed {
			return nil, errUnknownVertex
		}

		switch {
		case vtxID.Equals(vtx1.ID()):
			return vtx1, nil
		}
		return nil, errUnknownVertex
	}

	reqID := new(uint32)
	sender.GetF = func(reqVdr ids.ShortID, requestID uint32, vtxID ids.ID) {
		*reqID = requestID
		if !reqVdr.Equals(vdr.ID()) {
			t.Fatalf("Wrong validator requested")
		}
		if !vtxID.Equals(vtx0.ID()) {
			t.Fatalf("Wrong vertex requested")
		}
	}

	te.PushQuery(vdr.ID(), 0, vtx1.ID(), vtx1.Bytes())

	te.Put(secondVdr.ID(), *reqID, vtx0.ID(), []byte{3})

	*parsed = false
	manager.ParseVertexF = func(b []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(b, vtx0.Bytes()):
			*parsed = true
			return vtx0, nil
		}
		return nil, errUnknownVertex
	}

	manager.GetVertexF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if !*parsed {
			return nil, errUnknownVertex
		}

		switch {
		case vtxID.Equals(vtx0.ID()):
			return vtx0, nil
		}
		return nil, errUnknownVertex
	}
	sender.CantPushQuery = false
	sender.CantChits = false

	vtx0.StatusV = choices.Processing

	te.Put(vdr.ID(), *reqID, vtx0.ID(), vtx0.Bytes())

	prefs := te.Consensus.Preferences()
	if !prefs.Contains(vtx1.ID()) {
		t.Fatalf("Shouldn't have abandoned the pending vertex")
	}
}

func TestEnginePushQueryRequestIDConflict(t *testing.T) {
	config := DefaultConfig()

	vdr := validators.GenerateRandomValidator(1)

	vals := validators.NewSet()
	config.Validators = vals

	vals.Add(vdr)

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	gVtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		},
		BytesV: []byte{0},
	}

	vts := []avalanche.Vertex{gVtx}
	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

	tx1 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx1.InputIDsV.Add(utxos[1])

	vtx0 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
		BytesV:   []byte{1},
	}

	vtx1 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []avalanche.Vertex{vtx0},
		HeightV:  2,
		TxsV:     []snowstorm.Tx{tx1},
		BytesV:   []byte{2},
	}

	randomVtxID := ids.GenerateTestID()

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	parsed := new(bool)
	manager.ParseVertexF = func(b []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(b, vtx1.Bytes()):
			*parsed = true
			return vtx1, nil
		}
		return nil, errUnknownVertex
	}

	manager.GetVertexF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if !*parsed {
			return nil, errUnknownVertex
		}

		switch {
		case vtxID.Equals(vtx1.ID()):
			return vtx1, nil
		}
		return nil, errUnknownVertex
	}

	reqID := new(uint32)
	sender.GetF = func(reqVdr ids.ShortID, requestID uint32, vtxID ids.ID) {
		*reqID = requestID
		if !reqVdr.Equals(vdr.ID()) {
			t.Fatalf("Wrong validator requested")
		}
		if !vtxID.Equals(vtx0.ID()) {
			t.Fatalf("Wrong vertex requested")
		}
	}

	te.PushQuery(vdr.ID(), 0, vtx1.ID(), vtx1.Bytes())

	sender.GetF = nil
	sender.CantGet = false

	te.PushQuery(vdr.ID(), *reqID, randomVtxID, []byte{3})

	*parsed = false
	manager.ParseVertexF = func(b []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(b, vtx0.Bytes()):
			*parsed = true
			return vtx0, nil
		}
		return nil, errUnknownVertex
	}

	manager.GetVertexF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if !*parsed {
			return nil, errUnknownVertex
		}

		switch {
		case vtxID.Equals(vtx0.ID()):
			return vtx0, nil
		}
		return nil, errUnknownVertex
	}
	sender.CantPushQuery = false
	sender.CantChits = false

	vtx0.StatusV = choices.Processing

	te.Put(vdr.ID(), *reqID, vtx0.ID(), vtx0.Bytes())

	prefs := te.Consensus.Preferences()
	if !prefs.Contains(vtx1.ID()) {
		t.Fatalf("Shouldn't have abandoned the pending vertex")
	}
}

func TestEngineAggressivePolling(t *testing.T) {
	config := DefaultConfig()

	config.Params.ConcurrentRepolls = 3

	vdr := validators.GenerateRandomValidator(1)

	vals := validators.NewSet()
	config.Validators = vals

	vals.Add(vdr)

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	gVtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		},
		BytesV: []byte{0},
	}

	vts := []avalanche.Vertex{gVtx}
	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV.Add(utxos[0])

	tx1 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx1.InputIDsV.Add(utxos[1])

	vtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
		BytesV:   []byte{1},
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	parsed := new(bool)
	manager.ParseVertexF = func(b []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(b, vtx.Bytes()):
			*parsed = true
			return vtx, nil
		}
		return nil, errUnknownVertex
	}

	manager.GetVertexF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if !*parsed {
			return nil, errUnknownVertex
		}

		switch {
		case vtxID.Equals(vtx.ID()):
			return vtx, nil
		}
		return nil, errUnknownVertex
	}

	numPushQueries := new(int)
	sender.PushQueryF = func(ids.ShortSet, uint32, ids.ID, []byte) { *numPushQueries++ }

	numPullQueries := new(int)
	sender.PullQueryF = func(ids.ShortSet, uint32, ids.ID) { *numPullQueries++ }

	te.Put(vdr.ID(), 0, vtx.ID(), vtx.Bytes())

	if *numPushQueries != 1 {
		t.Fatalf("should have issued one push query")
	}
	if *numPullQueries != 2 {
		t.Fatalf("should have issued one pull query")
	}
}

func TestEngineDuplicatedIssuance(t *testing.T) {
	config := DefaultConfig()
	config.Params.BatchSize = 1
	config.Params.BetaVirtuous = 5
	config.Params.BetaRogue = 5

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)
	sender.CantGetAcceptedFrontier = false

	vdr := validators.GenerateRandomValidator(1)

	vals := validators.NewSet()
	config.Validators = vals

	vals.Add(vdr)

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	manager.Default(true)

	vm := &vertex.TestVM{}
	vm.T = t
	config.VM = vm

	vm.Default(true)

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	gTx := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	tx := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx.InputIDsV.Add(utxos[0])

	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVertexF = func(id ids.ID) (avalanche.Vertex, error) {
		switch {
		case id.Equals(gVtx.ID()):
			return gVtx, nil
		case id.Equals(mVtx.ID()):
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	lastVtx := new(avalanche.TestVertex)
	manager.BuildVertexF = func(_ ids.Set, txs []snowstorm.Tx) (avalanche.Vertex, error) {
		lastVtx = &avalanche.TestVertex{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Processing,
			},
			ParentsV: []avalanche.Vertex{gVtx, mVtx},
			HeightV:  1,
			TxsV:     txs,
			BytesV:   []byte{1},
		}
		return lastVtx, nil
	}

	sender.CantPushQuery = false

	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx} }
	te.Notify(common.PendingTxs)

	if len(lastVtx.TxsV) != 1 || !lastVtx.TxsV[0].ID().Equals(tx.ID()) {
		t.Fatalf("Should have issued txs differently")
	}

	manager.BuildVertexF = func(ids.Set, []snowstorm.Tx) (avalanche.Vertex, error) {
		t.Fatalf("shouldn't have attempted to issue a duplicated tx")
		return nil, nil
	}

	te.Notify(common.PendingTxs)
}

func TestEngineDoubleChit(t *testing.T) {
	config := DefaultConfig()

	config.Params.Alpha = 2
	config.Params.K = 2

	vdr0 := validators.GenerateRandomValidator(1)
	vdr1 := validators.GenerateRandomValidator(1)
	vals := validators.NewSet()
	vals.Add(vdr0)
	vals.Add(vdr1)
	config.Validators = vals

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)
	sender.CantGetAcceptedFrontier = false

	manager := &vertex.TestManager{T: t}
	config.Manager = manager

	manager.Default(true)

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []avalanche.Vertex{gVtx, mVtx}
	utxos := []ids.ID{ids.GenerateTestID()}

	tx := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx.InputIDsV.Add(utxos[0])

	vtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx},
		BytesV:   []byte{1, 1, 2, 3},
	}

	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVertexF = func(id ids.ID) (avalanche.Vertex, error) {
		switch {
		case id.Equals(gVtx.ID()):
			return gVtx, nil
		case id.Equals(mVtx.ID()):
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()
	te.Finished = true

	reqID := new(uint32)
	sender.PushQueryF = func(inVdrs ids.ShortSet, requestID uint32, vtxID ids.ID, _ []byte) {
		*reqID = requestID
		if inVdrs.Len() != 2 {
			t.Fatalf("Wrong number of validators")
		}
		if !vtxID.Equals(vtx.ID()) {
			t.Fatalf("Wrong vertex requested")
		}
	}
	manager.GetVertexF = func(id ids.ID) (avalanche.Vertex, error) {
		switch {
		case id.Equals(vtx.ID()):
			return vtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te.insert(vtx)

	votes := ids.Set{}
	votes.Add(vtx.ID())

	if status := tx.Status(); status != choices.Processing {
		t.Fatalf("Wrong tx status: %s ; expected: %s", status, choices.Processing)
	}

	te.Chits(vdr0.ID(), *reqID, votes)

	if status := tx.Status(); status != choices.Processing {
		t.Fatalf("Wrong tx status: %s ; expected: %s", status, choices.Processing)
	}

	te.Chits(vdr0.ID(), *reqID, votes)

	if status := tx.Status(); status != choices.Processing {
		t.Fatalf("Wrong tx status: %s ; expected: %s", status, choices.Processing)
	}

	te.Chits(vdr1.ID(), *reqID, votes)

	if status := tx.Status(); status != choices.Accepted {
		t.Fatalf("Wrong tx status: %s ; expected: %s", status, choices.Accepted)
	}
}
