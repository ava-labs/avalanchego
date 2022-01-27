// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/queue"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"

	avagetter "github.com/ava-labs/avalanchego/snow/engine/avalanche/getter"
)

var (
	errUnknownVertex       = errors.New("unknown vertex")
	errParsedUnknownVertex = errors.New("parsed unknown vertex")
)

func newConfig(t *testing.T) (Config, ids.ShortID, *common.SenderTest, *vertex.TestManager, *vertex.TestVM) {
	ctx := snow.DefaultConsensusContextTest()

	peers := validators.NewSet()
	db := memdb.New()
	sender := &common.SenderTest{T: t}
	manager := vertex.NewTestManager(t)
	vm := &vertex.TestVM{}
	vm.T = t

	isBootstrapped := false
	subnet := &common.SubnetTest{
		T:               t,
		IsBootstrappedF: func() bool { return isBootstrapped },
		BootstrappedF:   func(ids.ID) { isBootstrapped = true },
	}

	sender.Default(true)
	manager.Default(true)
	vm.Default(true)

	sender.CantSendGetAcceptedFrontier = false

	peer := ids.GenerateTestShortID()
	if err := peers.AddWeight(peer, 1); err != nil {
		t.Fatal(err)
	}

	vtxBlocker, err := queue.NewWithMissing(prefixdb.New([]byte("vtx"), db), "vtx", ctx.Registerer)
	if err != nil {
		t.Fatal(err)
	}
	txBlocker, err := queue.New(prefixdb.New([]byte("tx"), db), "tx", ctx.Registerer)
	if err != nil {
		t.Fatal(err)
	}

	commonConfig := common.Config{
		Ctx:                            ctx,
		Validators:                     peers,
		Beacons:                        peers,
		SampleK:                        peers.Len(),
		Alpha:                          peers.Weight()/2 + 1,
		Sender:                         sender,
		Subnet:                         subnet,
		Timer:                          &common.TimerTest{},
		AncestorsMaxContainersSent:     2000,
		AncestorsMaxContainersReceived: 2000,
		SharedCfg:                      &common.SharedConfig{},
	}

	avaGetHandler, err := avagetter.New(manager, commonConfig)
	if err != nil {
		t.Fatal(err)
	}

	return Config{
		Config:        commonConfig,
		AllGetsServer: avaGetHandler,
		VtxBlocked:    vtxBlocker,
		TxBlocked:     txBlocker,
		Manager:       manager,
		VM:            vm,
		WeightTracker: tracker.NewWeightTracker(commonConfig.Beacons, commonConfig.StartupAlpha),
	}, peer, sender, manager, vm
}

// Three vertices in the accepted frontier. None have parents. No need to fetch anything
func TestBootstrapperSingleFrontier(t *testing.T) {
	config, _, _, manager, vm := newConfig(t)

	vtxID0 := ids.Empty.Prefix(0)
	vtxID1 := ids.Empty.Prefix(1)
	vtxID2 := ids.Empty.Prefix(2)

	vtxBytes0 := []byte{0}
	vtxBytes1 := []byte{1}
	vtxBytes2 := []byte{2}

	vtx0 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID0,
			StatusV: choices.Processing,
		},
		HeightV: 0,
		BytesV:  vtxBytes0,
	}
	vtx1 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID1,
			StatusV: choices.Processing,
		},
		HeightV: 0,
		BytesV:  vtxBytes1,
	}
	vtx2 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID2,
			StatusV: choices.Processing,
		},
		HeightV: 0,
		BytesV:  vtxBytes2,
	}

	bs, err := New(
		config,
		func(lastReqID uint32) error { config.Ctx.SetState(snow.NormalOp); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := bs.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	acceptedIDs := []ids.ID{vtxID0, vtxID1, vtxID2}

	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch vtxID {
		case vtxID0:
			return vtx0, nil
		case vtxID1:
			return vtx1, nil
		case vtxID2:
			return vtx2, nil
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
	}

	manager.ParseVtxF = func(vtxBytes []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(vtxBytes, vtxBytes0):
			return vtx0, nil
		case bytes.Equal(vtxBytes, vtxBytes1):
			return vtx1, nil
		case bytes.Equal(vtxBytes, vtxBytes2):
			return vtx2, nil
		}
		t.Fatal(errParsedUnknownVertex)
		return nil, errParsedUnknownVertex
	}

	vm.CantSetState = false
	if err := bs.ForceAccepted(acceptedIDs); err != nil {
		t.Fatal(err)
	}

	switch {
	case config.Ctx.GetState() != snow.NormalOp:
		t.Fatalf("Bootstrapping should have finished")
	case vtx0.Status() != choices.Accepted:
		t.Fatalf("Vertex should be accepted")
	case vtx1.Status() != choices.Accepted:
		t.Fatalf("Vertex should be accepted")
	case vtx2.Status() != choices.Accepted:
		t.Fatalf("Vertex should be accepted")
	}
}

// Accepted frontier has one vertex, which has one vertex as a dependency.
// Requests again and gets an unexpected vertex.
// Requests again and gets the expected vertex and an additional vertex that should not be accepted.
func TestBootstrapperByzantineResponses(t *testing.T) {
	config, peerID, sender, manager, vm := newConfig(t)

	vtxID0 := ids.Empty.Prefix(0)
	vtxID1 := ids.Empty.Prefix(1)
	vtxID2 := ids.Empty.Prefix(2)

	vtxBytes0 := []byte{0}
	vtxBytes1 := []byte{1}
	vtxBytes2 := []byte{2}

	vtx0 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID0,
			StatusV: choices.Unknown,
		},
		HeightV: 0,
		BytesV:  vtxBytes0,
	}
	vtx1 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID1,
			StatusV: choices.Processing,
		},
		ParentsV: []avalanche.Vertex{vtx0},
		HeightV:  1,
		BytesV:   vtxBytes1,
	}
	// Should not receive transitive votes from [vtx1]
	vtx2 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID2,
			StatusV: choices.Unknown,
		},
		HeightV: 0,
		BytesV:  vtxBytes2,
	}

	bs, err := New(
		config,
		func(lastReqID uint32) error { config.Ctx.SetState(snow.NormalOp); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := bs.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	acceptedIDs := []ids.ID{vtxID1}

	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch vtxID {
		case vtxID1:
			return vtx1, nil
		case vtxID0:
			return nil, errUnknownVertex
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
	}

	requestID := new(uint32)
	reqVtxID := ids.Empty
	sender.SendGetAncestorsF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		switch {
		case vdr != peerID:
			t.Fatalf("Should have requested vertex from %s, requested from %s",
				peerID, vdr)
		case vtxID != vtxID0:
			t.Fatalf("should have requested vtx0")
		}
		*requestID = reqID
		reqVtxID = vtxID
	}

	manager.ParseVtxF = func(vtxBytes []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(vtxBytes, vtxBytes0):
			vtx0.StatusV = choices.Processing
			return vtx0, nil
		case bytes.Equal(vtxBytes, vtxBytes1):
			vtx1.StatusV = choices.Processing
			return vtx1, nil
		case bytes.Equal(vtxBytes, vtxBytes2):
			vtx2.StatusV = choices.Processing
			return vtx2, nil
		}
		t.Fatal(errParsedUnknownVertex)
		return nil, errParsedUnknownVertex
	}

	vm.CantSetState = false
	if err := bs.ForceAccepted(acceptedIDs); err != nil { // should request vtx0
		t.Fatal(err)
	} else if reqVtxID != vtxID0 {
		t.Fatalf("should have requested vtxID0 but requested %s", reqVtxID)
	}

	oldReqID := *requestID
	err = bs.Ancestors(peerID, *requestID, [][]byte{vtxBytes2})
	switch {
	case err != nil: // send unexpected vertex
		t.Fatal(err)
	case *requestID == oldReqID:
		t.Fatal("should have issued new request")
	case reqVtxID != vtxID0:
		t.Fatalf("should have requested vtxID0 but requested %s", reqVtxID)
	}

	oldReqID = *requestID
	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch vtxID {
		case vtxID1:
			return vtx1, nil
		case vtxID0:
			return vtx0, nil
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
	}

	if err := bs.Ancestors(peerID, *requestID, [][]byte{vtxBytes0, vtxBytes2}); err != nil { // send expected vertex and vertex that should not be accepted
		t.Fatal(err)
	}

	switch {
	case *requestID != oldReqID:
		t.Fatal("should not have issued new request")
	case config.Ctx.GetState() != snow.NormalOp:
		t.Fatalf("Bootstrapping should have finished")
	case vtx0.Status() != choices.Accepted:
		t.Fatalf("Vertex should be accepted")
	case vtx1.Status() != choices.Accepted:
		t.Fatalf("Vertex should be accepted")
	}
	if vtx2.Status() == choices.Accepted {
		t.Fatalf("Vertex should not have been accepted")
	}
}

// Vertex has a dependency and tx has a dependency
func TestBootstrapperTxDependencies(t *testing.T) {
	config, peerID, sender, manager, vm := newConfig(t)

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
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	// Depends on tx0
	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     txID1,
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{tx0},
		BytesV:        txBytes1,
	}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[1])

	vtxID0 := ids.GenerateTestID()
	vtxID1 := ids.GenerateTestID()

	vtxBytes0 := []byte{2}
	vtxBytes1 := []byte{3}
	vm.ParseTxF = func(b []byte) (snowstorm.Tx, error) {
		switch {
		case bytes.Equal(b, txBytes0):
			return tx0, nil
		case bytes.Equal(b, txBytes1):
			return tx1, nil
		default:
			return nil, errors.New("wrong tx")
		}
	}

	vtx0 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID0,
			StatusV: choices.Unknown,
		},
		HeightV: 0,
		TxsV:    []snowstorm.Tx{tx1},
		BytesV:  vtxBytes0,
	}
	vtx1 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID1,
			StatusV: choices.Processing,
		},
		ParentsV: []avalanche.Vertex{vtx0}, // Depends on vtx0
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
		BytesV:   vtxBytes1,
	}

	bs, err := New(
		config,
		func(lastReqID uint32) error { config.Ctx.SetState(snow.NormalOp); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := bs.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	acceptedIDs := []ids.ID{vtxID1}

	manager.ParseVtxF = func(vtxBytes []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(vtxBytes, vtxBytes1):
			return vtx1, nil
		case bytes.Equal(vtxBytes, vtxBytes0):
			return vtx0, nil
		}
		t.Fatal(errParsedUnknownVertex)
		return nil, errParsedUnknownVertex
	}
	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch vtxID {
		case vtxID1:
			return vtx1, nil
		case vtxID0:
			return nil, errUnknownVertex
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
	}

	reqIDPtr := new(uint32)
	sender.SendGetAncestorsF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if vdr != peerID {
			t.Fatalf("Should have requested vertex from %s, requested from %s", peerID, vdr)
		}
		switch vtxID {
		case vtxID0:
		default:
			t.Fatal(errUnknownVertex)
		}

		*reqIDPtr = reqID
	}

	vm.CantSetState = false
	if err := bs.ForceAccepted(acceptedIDs); err != nil { // should request vtx0
		t.Fatal(err)
	}

	manager.ParseVtxF = func(vtxBytes []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(vtxBytes, vtxBytes1):
			return vtx1, nil
		case bytes.Equal(vtxBytes, vtxBytes0):
			vtx0.StatusV = choices.Processing
			return vtx0, nil
		}
		t.Fatal(errParsedUnknownVertex)
		return nil, errParsedUnknownVertex
	}

	if err := bs.Ancestors(peerID, *reqIDPtr, [][]byte{vtxBytes0}); err != nil {
		t.Fatal(err)
	}

	if config.Ctx.GetState() != snow.NormalOp {
		t.Fatalf("Should have finished bootstrapping")
	}
	if tx0.Status() != choices.Accepted {
		t.Fatalf("Tx should be accepted")
	}
	if tx1.Status() != choices.Accepted {
		t.Fatalf("Tx should be accepted")
	}

	if vtx0.Status() != choices.Accepted {
		t.Fatalf("Vertex should be accepted")
	}
	if vtx1.Status() != choices.Accepted {
		t.Fatalf("Vertex should be accepted")
	}
}

// Unfulfilled tx dependency
func TestBootstrapperMissingTxDependency(t *testing.T) {
	config, peerID, sender, manager, vm := newConfig(t)

	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	txID0 := ids.GenerateTestID()
	txID1 := ids.GenerateTestID()

	txBytes1 := []byte{1}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     txID0,
		StatusV: choices.Unknown,
	}}

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     txID1,
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{tx0},
		BytesV:        txBytes1,
	}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[1])

	vtxID0 := ids.GenerateTestID()
	vtxID1 := ids.GenerateTestID()

	vtxBytes0 := []byte{2}
	vtxBytes1 := []byte{3}

	vtx0 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID0,
			StatusV: choices.Unknown,
		},
		HeightV: 0,
		BytesV:  vtxBytes0,
	}
	vtx1 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID1,
			StatusV: choices.Processing,
		},
		ParentsV: []avalanche.Vertex{vtx0}, // depends on vtx0
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1},
		BytesV:   vtxBytes1,
	}

	bs, err := New(
		config,
		func(lastReqID uint32) error { config.Ctx.SetState(snow.NormalOp); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := bs.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	acceptedIDs := []ids.ID{vtxID1}

	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch vtxID {
		case vtxID1:
			return vtx1, nil
		case vtxID0:
			return nil, errUnknownVertex
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
	}
	manager.ParseVtxF = func(vtxBytes []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(vtxBytes, vtxBytes1):
			return vtx1, nil
		case bytes.Equal(vtxBytes, vtxBytes0):
			vtx0.StatusV = choices.Processing
			return vtx0, nil
		}
		t.Fatal(errParsedUnknownVertex)
		return nil, errParsedUnknownVertex
	}

	reqIDPtr := new(uint32)
	sender.SendGetAncestorsF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if vdr != peerID {
			t.Fatalf("Should have requested vertex from %s, requested from %s", peerID, vdr)
		}
		switch {
		case vtxID == vtxID0:
		default:
			t.Fatalf("Requested wrong vertex")
		}

		*reqIDPtr = reqID
	}

	vm.CantSetState = false
	if err := bs.ForceAccepted(acceptedIDs); err != nil { // should request vtx1
		t.Fatal(err)
	}

	if err := bs.Ancestors(peerID, *reqIDPtr, [][]byte{vtxBytes0}); err != nil {
		t.Fatal(err)
	}

	if config.Ctx.GetState() != snow.NormalOp {
		t.Fatalf("Bootstrapping should have finished")
	}
	if tx0.Status() != choices.Unknown { // never saw this tx
		t.Fatalf("Tx should be unknown")
	}
	if tx1.Status() != choices.Processing { // can't accept because we don't have tx0
		t.Fatalf("Tx should be processing")
	}

	if vtx0.Status() != choices.Accepted {
		t.Fatalf("Vertex should be accepted")
	}
	if vtx1.Status() != choices.Processing { // can't accept because we don't have tx1 accepted
		t.Fatalf("Vertex should be processing")
	}
}

// Ancestors only contains 1 of the two needed vertices; have to issue another GetAncestors
func TestBootstrapperIncompleteAncestors(t *testing.T) {
	config, peerID, sender, manager, vm := newConfig(t)

	vtxID0 := ids.Empty.Prefix(0)
	vtxID1 := ids.Empty.Prefix(1)
	vtxID2 := ids.Empty.Prefix(2)

	vtxBytes0 := []byte{0}
	vtxBytes1 := []byte{1}
	vtxBytes2 := []byte{2}

	vtx0 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID0,
			StatusV: choices.Unknown,
		},
		HeightV: 0,
		BytesV:  vtxBytes0,
	}
	vtx1 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID1,
			StatusV: choices.Unknown,
		},
		ParentsV: []avalanche.Vertex{vtx0},
		HeightV:  1,
		BytesV:   vtxBytes1,
	}
	vtx2 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID2,
			StatusV: choices.Processing,
		},
		ParentsV: []avalanche.Vertex{vtx1},
		HeightV:  2,
		BytesV:   vtxBytes2,
	}

	bs, err := New(
		config,
		func(lastReqID uint32) error { config.Ctx.SetState(snow.NormalOp); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := bs.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	acceptedIDs := []ids.ID{vtxID2}

	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID == vtxID0:
			return nil, errUnknownVertex
		case vtxID == vtxID1:
			return nil, errUnknownVertex
		case vtxID == vtxID2:
			return vtx2, nil
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
	}
	manager.ParseVtxF = func(vtxBytes []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(vtxBytes, vtxBytes0):
			vtx0.StatusV = choices.Processing
			return vtx0, nil

		case bytes.Equal(vtxBytes, vtxBytes1):
			vtx1.StatusV = choices.Processing
			return vtx1, nil
		case bytes.Equal(vtxBytes, vtxBytes2):
			return vtx2, nil
		}
		t.Fatal(errParsedUnknownVertex)
		return nil, errParsedUnknownVertex
	}
	reqIDPtr := new(uint32)
	requested := ids.Empty
	sender.SendGetAncestorsF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if vdr != peerID {
			t.Fatalf("Should have requested vertex from %s, requested from %s", peerID, vdr)
		}
		switch vtxID {
		case vtxID1, vtxID0:
		default:
			t.Fatal(errUnknownVertex)
		}
		*reqIDPtr = reqID
		requested = vtxID
	}

	vm.CantSetState = false
	if err := bs.ForceAccepted(acceptedIDs); err != nil { // should request vtx1
		t.Fatal(err)
	} else if requested != vtxID1 {
		t.Fatal("requested wrong vtx")
	}

	err = bs.Ancestors(peerID, *reqIDPtr, [][]byte{vtxBytes1})
	switch {
	case err != nil: // Provide vtx1; should request vtx0
		t.Fatal(err)
	case bs.Context().GetState() == snow.NormalOp:
		t.Fatalf("should not have finished")
	case requested != vtxID0:
		t.Fatal("should hae requested vtx0")
	}

	err = bs.Ancestors(peerID, *reqIDPtr, [][]byte{vtxBytes0})
	switch {
	case err != nil: // Provide vtx0; can finish now
		t.Fatal(err)
	case bs.Context().GetState() != snow.NormalOp:
		t.Fatal("should have finished")
	case vtx0.Status() != choices.Accepted:
		t.Fatal("should be accepted")
	case vtx1.Status() != choices.Accepted:
		t.Fatal("should be accepted")
	case vtx2.Status() != choices.Accepted:
		t.Fatal("should be accepted")
	}
}

func TestBootstrapperFinalized(t *testing.T) {
	config, peerID, sender, manager, vm := newConfig(t)

	vtxID0 := ids.Empty.Prefix(0)
	vtxID1 := ids.Empty.Prefix(1)

	vtxBytes0 := []byte{0}
	vtxBytes1 := []byte{1}

	vtx0 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID0,
			StatusV: choices.Unknown,
		},
		HeightV: 0,
		BytesV:  vtxBytes0,
	}
	vtx1 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID1,
			StatusV: choices.Unknown,
		},
		ParentsV: []avalanche.Vertex{vtx0},
		HeightV:  1,
		BytesV:   vtxBytes1,
	}

	bs, err := New(
		config,
		func(lastReqID uint32) error { config.Ctx.SetState(snow.NormalOp); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := bs.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	acceptedIDs := []ids.ID{vtxID0, vtxID1}

	parsedVtx0 := false
	parsedVtx1 := false
	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch vtxID {
		case vtxID0:
			if parsedVtx0 {
				return vtx0, nil
			}
			return nil, errUnknownVertex
		case vtxID1:
			if parsedVtx1 {
				return vtx1, nil
			}
			return nil, errUnknownVertex
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
	}
	manager.ParseVtxF = func(vtxBytes []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(vtxBytes, vtxBytes0):
			vtx0.StatusV = choices.Processing
			parsedVtx0 = true
			return vtx0, nil
		case bytes.Equal(vtxBytes, vtxBytes1):
			vtx1.StatusV = choices.Processing
			parsedVtx1 = true
			return vtx1, nil
		}
		t.Fatal(errUnknownVertex)
		return nil, errUnknownVertex
	}

	requestIDs := map[ids.ID]uint32{}
	sender.SendGetAncestorsF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if vdr != peerID {
			t.Fatalf("Should have requested block from %s, requested from %s", peerID, vdr)
		}
		requestIDs[vtxID] = reqID
	}

	vm.CantSetState = false
	if err := bs.ForceAccepted(acceptedIDs); err != nil { // should request vtx0 and vtx1
		t.Fatal(err)
	}

	reqID, ok := requestIDs[vtxID1]
	if !ok {
		t.Fatalf("should have requested vtx1")
	}

	if err := bs.Ancestors(peerID, reqID, [][]byte{vtxBytes1, vtxBytes0}); err != nil {
		t.Fatal(err)
	}

	reqID, ok = requestIDs[vtxID0]
	if !ok {
		t.Fatalf("should have requested vtx0")
	}

	err = bs.GetAncestorsFailed(peerID, reqID)
	switch {
	case err != nil:
		t.Fatal(err)
	case config.Ctx.GetState() != snow.NormalOp:
		t.Fatalf("Bootstrapping should have finished")
	case vtx0.Status() != choices.Accepted:
		t.Fatalf("Vertex should be accepted")
	case vtx1.Status() != choices.Accepted:
		t.Fatalf("Vertex should be accepted")
	}
}

// Test that Ancestors accepts the parents of the first vertex returned
func TestBootstrapperAcceptsAncestorsParents(t *testing.T) {
	config, peerID, sender, manager, vm := newConfig(t)

	vtxID0 := ids.Empty.Prefix(0)
	vtxID1 := ids.Empty.Prefix(1)
	vtxID2 := ids.Empty.Prefix(2)

	vtxBytes0 := []byte{0}
	vtxBytes1 := []byte{1}
	vtxBytes2 := []byte{2}

	vtx0 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID0,
			StatusV: choices.Unknown,
		},
		HeightV: 0,
		BytesV:  vtxBytes0,
	}
	vtx1 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID1,
			StatusV: choices.Unknown,
		},
		ParentsV: []avalanche.Vertex{vtx0},
		HeightV:  1,
		BytesV:   vtxBytes1,
	}
	vtx2 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID2,
			StatusV: choices.Unknown,
		},
		ParentsV: []avalanche.Vertex{vtx1},
		HeightV:  2,
		BytesV:   vtxBytes2,
	}

	bs, err := New(
		config,
		func(lastReqID uint32) error { config.Ctx.SetState(snow.NormalOp); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := bs.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	acceptedIDs := []ids.ID{vtxID2}

	parsedVtx0 := false
	parsedVtx1 := false
	parsedVtx2 := false
	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch vtxID {
		case vtxID0:
			if parsedVtx0 {
				return vtx0, nil
			}
			return nil, errUnknownVertex
		case vtxID1:
			if parsedVtx1 {
				return vtx1, nil
			}
			return nil, errUnknownVertex
		case vtxID2:
			if parsedVtx2 {
				return vtx2, nil
			}
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
		return nil, errUnknownVertex
	}
	manager.ParseVtxF = func(vtxBytes []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(vtxBytes, vtxBytes0):
			vtx0.StatusV = choices.Processing
			parsedVtx0 = true
			return vtx0, nil
		case bytes.Equal(vtxBytes, vtxBytes1):
			vtx1.StatusV = choices.Processing
			parsedVtx1 = true
			return vtx1, nil
		case bytes.Equal(vtxBytes, vtxBytes2):
			vtx2.StatusV = choices.Processing
			parsedVtx2 = true
			return vtx2, nil
		}
		t.Fatal(errUnknownVertex)
		return nil, errUnknownVertex
	}

	requestIDs := map[ids.ID]uint32{}
	sender.SendGetAncestorsF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if vdr != peerID {
			t.Fatalf("Should have requested block from %s, requested from %s", peerID, vdr)
		}
		requestIDs[vtxID] = reqID
	}

	vm.CantSetState = false
	if err := bs.ForceAccepted(acceptedIDs); err != nil { // should request vtx2
		t.Fatal(err)
	}

	reqID, ok := requestIDs[vtxID2]
	if !ok {
		t.Fatalf("should have requested vtx2")
	}

	if err := bs.Ancestors(peerID, reqID, [][]byte{vtxBytes2, vtxBytes1, vtxBytes0}); err != nil {
		t.Fatal(err)
	}

	switch {
	case config.Ctx.GetState() != snow.NormalOp:
		t.Fatalf("Bootstrapping should have finished")
	case vtx0.Status() != choices.Accepted:
		t.Fatalf("Vertex should be accepted")
	case vtx1.Status() != choices.Accepted:
		t.Fatalf("Vertex should be accepted")
	case vtx2.Status() != choices.Accepted:
		t.Fatalf("Vertex should be accepted")
	}
}

func TestRestartBootstrapping(t *testing.T) {
	config, peerID, sender, manager, vm := newConfig(t)

	vtxID0 := ids.GenerateTestID()
	vtxID1 := ids.GenerateTestID()
	vtxID2 := ids.GenerateTestID()
	vtxID3 := ids.GenerateTestID()
	vtxID4 := ids.GenerateTestID()
	vtxID5 := ids.GenerateTestID()

	vtxBytes0 := []byte{0}
	vtxBytes1 := []byte{1}
	vtxBytes2 := []byte{2}
	vtxBytes3 := []byte{3}
	vtxBytes4 := []byte{4}
	vtxBytes5 := []byte{5}

	vtx0 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID0,
			StatusV: choices.Unknown,
		},
		HeightV: 0,
		BytesV:  vtxBytes0,
	}
	vtx1 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID1,
			StatusV: choices.Unknown,
		},
		ParentsV: []avalanche.Vertex{vtx0},
		HeightV:  1,
		BytesV:   vtxBytes1,
	}
	vtx2 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID2,
			StatusV: choices.Unknown,
		},
		ParentsV: []avalanche.Vertex{vtx1},
		HeightV:  2,
		BytesV:   vtxBytes2,
	}
	vtx3 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID3,
			StatusV: choices.Unknown,
		},
		ParentsV: []avalanche.Vertex{vtx2},
		HeightV:  3,
		BytesV:   vtxBytes3,
	}
	vtx4 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID4,
			StatusV: choices.Unknown,
		},
		ParentsV: []avalanche.Vertex{vtx2},
		HeightV:  3,
		BytesV:   vtxBytes4,
	}
	vtx5 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID5,
			StatusV: choices.Unknown,
		},
		ParentsV: []avalanche.Vertex{vtx4},
		HeightV:  4,
		BytesV:   vtxBytes5,
	}

	bsIntf, err := New(
		config,
		func(lastReqID uint32) error { config.Ctx.SetState(snow.NormalOp); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	bs, ok := bsIntf.(*bootstrapper)
	if !ok {
		t.Fatal("unexpected bootstrapper type")
	}

	startReqID := uint32(0)
	if err := bs.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	parsedVtx0 := false
	parsedVtx1 := false
	parsedVtx2 := false
	parsedVtx3 := false
	parsedVtx4 := false
	parsedVtx5 := false
	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch vtxID {
		case vtxID0:
			if parsedVtx0 {
				return vtx0, nil
			}
			return nil, errUnknownVertex
		case vtxID1:
			if parsedVtx1 {
				return vtx1, nil
			}
			return nil, errUnknownVertex
		case vtxID2:
			if parsedVtx2 {
				return vtx2, nil
			}
		case vtxID3:
			if parsedVtx3 {
				return vtx3, nil
			}
		case vtxID4:
			if parsedVtx4 {
				return vtx4, nil
			}
		case vtxID5:
			if parsedVtx5 {
				return vtx5, nil
			}
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
		return nil, errUnknownVertex
	}
	manager.ParseVtxF = func(vtxBytes []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(vtxBytes, vtxBytes0):
			vtx0.StatusV = choices.Processing
			parsedVtx0 = true
			return vtx0, nil
		case bytes.Equal(vtxBytes, vtxBytes1):
			vtx1.StatusV = choices.Processing
			parsedVtx1 = true
			return vtx1, nil
		case bytes.Equal(vtxBytes, vtxBytes2):
			vtx2.StatusV = choices.Processing
			parsedVtx2 = true
			return vtx2, nil
		case bytes.Equal(vtxBytes, vtxBytes3):
			vtx3.StatusV = choices.Processing
			parsedVtx3 = true
			return vtx3, nil
		case bytes.Equal(vtxBytes, vtxBytes4):
			vtx4.StatusV = choices.Processing
			parsedVtx4 = true
			return vtx4, nil
		case bytes.Equal(vtxBytes, vtxBytes5):
			vtx5.StatusV = choices.Processing
			parsedVtx5 = true
			return vtx5, nil
		}
		t.Fatal(errUnknownVertex)
		return nil, errUnknownVertex
	}

	requestIDs := map[ids.ID]uint32{}
	sender.SendGetAncestorsF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if vdr != peerID {
			t.Fatalf("Should have requested block from %s, requested from %s", peerID, vdr)
		}
		requestIDs[vtxID] = reqID
	}

	vm.CantSetState = false
	if err := bs.ForceAccepted([]ids.ID{vtxID3, vtxID4}); err != nil { // should request vtx3 and vtx4
		t.Fatal(err)
	}

	vtx3ReqID, ok := requestIDs[vtxID3]
	if !ok {
		t.Fatal("should have requested vtx4")
	}
	_, ok = requestIDs[vtxID4]
	if !ok {
		t.Fatal("should have requested vtx4")
	}

	if err := bs.Ancestors(peerID, vtx3ReqID, [][]byte{vtxBytes3, vtxBytes2}); err != nil {
		t.Fatal(err)
	}

	_, ok = requestIDs[vtxID1]
	if !ok {
		t.Fatal("should have requested vtx1")
	}

	if removed := bs.OutstandingRequests.RemoveAny(vtxID4); !removed {
		t.Fatal("expected to find outstanding requested for vtx4")
	}

	if removed := bs.OutstandingRequests.RemoveAny(vtxID1); !removed {
		t.Fatal("expected to find outstanding requested for vtx1")
	}
	bs.needToFetch.Clear()
	requestIDs = map[ids.ID]uint32{}

	if err := bs.ForceAccepted([]ids.ID{vtxID5, vtxID3}); err != nil {
		t.Fatal(err)
	}

	vtx1ReqID, ok := requestIDs[vtxID1]
	if !ok {
		t.Fatal("should have re-requested vtx1 from pending on prior run")
	}
	_, ok = requestIDs[vtxID4]
	if !ok {
		t.Fatal("should have re-requested vtx4 from pending on prior run")
	}
	vtx5ReqID, ok := requestIDs[vtxID5]
	if !ok {
		t.Fatal("should have requested vtx5")
	}
	if _, ok := requestIDs[vtxID3]; ok {
		t.Fatal("should not have re-requested vtx3 since it has been processed")
	}

	if err := bs.Ancestors(peerID, vtx5ReqID, [][]byte{vtxBytes5, vtxBytes4, vtxBytes2, vtxBytes1}); err != nil {
		t.Fatal(err)
	}

	_, ok = requestIDs[vtxID0]
	if !ok {
		t.Fatal("should have requested vtx0 after ancestors ended prior to it")
	}

	if err := bs.Ancestors(peerID, vtx1ReqID, [][]byte{vtxBytes1, vtxBytes0}); err != nil {
		t.Fatal(err)
	}

	switch {
	case config.Ctx.GetState() != snow.NormalOp:
		t.Fatalf("Bootstrapping should have finished")
	case vtx0.Status() != choices.Accepted:
		t.Fatalf("Vertex should be accepted")
	case vtx1.Status() != choices.Accepted:
		t.Fatalf("Vertex should be accepted")
	case vtx2.Status() != choices.Accepted:
		t.Fatalf("Vertex should be accepted")
	case vtx3.Status() != choices.Accepted:
		t.Fatalf("Vertex should be accepted")
	case vtx4.Status() != choices.Accepted:
		t.Fatalf("Vertex should be accepted")
	case vtx5.Status() != choices.Accepted:
		t.Fatalf("Vertex should be accepted")
	}
}
