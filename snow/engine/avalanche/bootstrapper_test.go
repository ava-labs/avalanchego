// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/database/prefixdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/avalanche"
	"github.com/ava-labs/gecko/snow/consensus/snowstorm"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/engine/common/queue"
	"github.com/ava-labs/gecko/snow/networking/router"
	"github.com/ava-labs/gecko/snow/networking/timeout"
	"github.com/ava-labs/gecko/snow/validators"
)

var (
	errUnknownVertex       = errors.New("unknown vertex")
	errParsedUnknownVertex = errors.New("parsed unknown vertex")
)

func newConfig(t *testing.T) (BootstrapConfig, ids.ShortID, *common.SenderTest, *stateTest, *VMTest) {
	ctx := snow.DefaultContextTest()

	peers := validators.NewSet()
	db := memdb.New()
	sender := &common.SenderTest{}
	state := &stateTest{}
	vm := &VMTest{}
	engine := &Transitive{}
	handler := &router.Handler{}
	router := &router.ChainRouter{}
	timeouts := &timeout.Manager{}

	sender.T = t
	state.t = t
	vm.T = t

	sender.Default(true)
	state.Default(true)
	vm.Default(true)

	sender.CantGetAcceptedFrontier = false

	peer := validators.GenerateRandomValidator(1)
	peerID := peer.ID()
	peers.Add(peer)

	handler.Initialize(
		engine,
		make(chan common.Message),
		1,
		"",
		prometheus.NewRegistry(),
	)
	timeouts.Initialize(0)
	router.Initialize(ctx.Log, timeouts, time.Hour, time.Second)

	vtxBlocker, _ := queue.New(prefixdb.New([]byte("vtx"), db))
	txBlocker, _ := queue.New(prefixdb.New([]byte("tx"), db))

	commonConfig := common.Config{
		Context:    ctx,
		Validators: peers,
		Beacons:    peers,
		Alpha:      uint64(peers.Len()/2 + 1),
		Sender:     sender,
	}
	return BootstrapConfig{
		Config:     commonConfig,
		VtxBlocked: vtxBlocker,
		TxBlocked:  txBlocker,
		State:      state,
		VM:         vm,
	}, peerID, sender, state, vm
}

// Three vertices in the accepted frontier. None have parents. No need to fetch anything
func TestBootstrapperSingleFrontier(t *testing.T) {
	config, _, _, state, vm := newConfig(t)

	vtxID0 := ids.Empty.Prefix(0)
	vtxID1 := ids.Empty.Prefix(1)
	vtxID2 := ids.Empty.Prefix(2)

	vtxBytes0 := []byte{0}
	vtxBytes1 := []byte{1}
	vtxBytes2 := []byte{2}

	vtx0 := &Vtx{
		id:     vtxID0,
		height: 0,
		status: choices.Processing,
		bytes:  vtxBytes0,
	}
	vtx1 := &Vtx{
		id:     vtxID1,
		height: 0,
		status: choices.Processing,
		bytes:  vtxBytes1,
	}
	vtx2 := &Vtx{
		id:     vtxID2,
		height: 0,
		status: choices.Processing,
		bytes:  vtxBytes2,
	}

	bs := bootstrapper{}
	bs.metrics.Initialize(config.Context.Log, fmt.Sprintf("gecko_%s", config.Context.ChainID), prometheus.NewRegistry())
	bs.Initialize(config)
	finished := new(bool)
	bs.onFinished = func() error { *finished = true; return nil }

	acceptedIDs := ids.Set{}
	acceptedIDs.Add(vtxID0, vtxID1, vtxID2)

	state.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID0):
			return vtx0, nil
		case vtxID.Equals(vtxID1):
			return vtx1, nil
		case vtxID.Equals(vtxID2):
			return vtx2, nil
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
	}

	state.parseVertex = func(vtxBytes []byte) (avalanche.Vertex, error) {
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

	vm.CantBootstrapping = false
	vm.CantBootstrapped = false

	bs.ForceAccepted(acceptedIDs)

	if !*finished {
		t.Fatalf("Bootstrapping should have finished")
	}
	if vtx0.Status() != choices.Accepted {
		t.Fatalf("Vertex should be accepted")
	}
	if vtx1.Status() != choices.Accepted {
		t.Fatalf("Vertex should be accepted")
	}
	if vtx2.Status() != choices.Accepted {
		t.Fatalf("Vertex should be accepted")
	}
}

// Accepted frontier has one vertex, which has one vertex as a dependency.
// Requests the unknown vertex and gets back an unexpected request ID.
// Requests again and gets response from unexpected peer.
// Requests again and gets an unexpected vertex.
// Requests again and gets the expected vertex.
func TestBootstrapperByzantineResponses(t *testing.T) {
	config, peerID, sender, state, vm := newConfig(t)

	vtxID0 := ids.Empty.Prefix(0)
	vtxID1 := ids.Empty.Prefix(1)
	vtxID2 := ids.Empty.Prefix(2)

	vtxBytes0 := []byte{0}
	vtxBytes1 := []byte{1}
	vtxBytes2 := []byte{2}

	vtx0 := &Vtx{
		id:     vtxID0,
		height: 0,
		status: choices.Unknown,
		bytes:  vtxBytes0,
	}
	vtx1 := &Vtx{
		id:      vtxID1,
		height:  0,
		parents: []avalanche.Vertex{vtx0},
		status:  choices.Processing,
		bytes:   vtxBytes1,
	}
	vtx2 := &Vtx{
		id:     vtxID2,
		height: 0,
		status: choices.Unknown,
		bytes:  vtxBytes2,
	}

	bs := bootstrapper{}
	bs.metrics.Initialize(config.Context.Log, fmt.Sprintf("gecko_%s", config.Context.ChainID), prometheus.NewRegistry())
	if err := bs.Initialize(config); err != nil {
		t.Fatal(err)
	}
	finished := new(bool)
	bs.onFinished = func() error { *finished = true; return nil }

	acceptedIDs := ids.Set{}
	acceptedIDs.Add(vtxID1)

	state.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID1):
			return vtx1, nil
		case vtxID.Equals(vtxID0):
			return nil, errUnknownVertex
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
	}

	requestID := new(uint32)
	reqVtxID := ids.Empty
	sender.GetAncestorsF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if !vdr.Equals(peerID) {
			t.Fatalf("Should have requested vertex from %s, requested from %s", peerID, vdr)
		}
		if !vtxID.Equals(vtxID0) {
			t.Fatalf("should have requested vtx0")
		}
		*requestID = reqID
		reqVtxID = vtxID
	}

	state.parseVertex = func(vtxBytes []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(vtxBytes, vtxBytes0):
			vtx0.status = choices.Processing
			return vtx0, nil
		case bytes.Equal(vtxBytes, vtxBytes1):
			vtx1.status = choices.Processing
			return vtx1, nil
		case bytes.Equal(vtxBytes, vtxBytes2):
			vtx2.status = choices.Processing
			return vtx2, nil
		}
		t.Fatal(errParsedUnknownVertex)
		return nil, errParsedUnknownVertex
	}
	vm.CantBootstrapping = false

	if err := bs.ForceAccepted(acceptedIDs); err != nil { // should request vtx0
		t.Fatal(err)
	}
	if !reqVtxID.Equals(vtxID0) {
		t.Fatalf("should have requested vtxID0 but requested %s", reqVtxID)
	}

	if err := bs.MultiPut(peerID, *requestID+1, [][]byte{vtxBytes0}); err != nil { // send response with wrong request ID
		t.Fatal(err)
	}
	if !reqVtxID.Equals(vtxID0) {
		t.Fatalf("should have requested vtxID0 but requested %s", reqVtxID)
	}

	oldReqID := *requestID
	if err := bs.MultiPut(ids.NewShortID([20]byte{1, 2, 3}), *requestID, [][]byte{vtxBytes0}); err != nil { // send response from wrong peer
		t.Fatal(err)
	}
	if *requestID != oldReqID {
		t.Fatal("should not have issued new request")
	} else if !reqVtxID.Equals(vtxID0) {
		t.Fatalf("should have requested vtxID0 but requested %s", reqVtxID)
	}

	oldReqID = *requestID
	if err := bs.MultiPut(peerID, *requestID, [][]byte{vtxBytes2}); err != nil { // send unexpected vertex
		t.Fatal(err)
	}
	if *requestID == oldReqID {
		t.Fatal("should have issued new request")
	} else if !reqVtxID.Equals(vtxID0) {
		t.Fatalf("should have requested vtxID0 but requested %s", reqVtxID)
	}

	oldReqID = *requestID
	state.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID1):
			return vtx1, nil
		case vtxID.Equals(vtxID0):
			return vtx0, nil
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
	}

	vm.CantBootstrapped = false

	if err := bs.MultiPut(peerID, *requestID, [][]byte{vtxBytes0}); err != nil { // send expected vertex
		t.Fatal(err)
	}
	if *requestID != oldReqID {
		t.Fatal("should not have issued new request")
	}

	if !*finished {
		t.Fatalf("Bootstrapping should have finished")
	}
	if vtx0.Status() != choices.Accepted {
		t.Fatalf("Vertex should be accepted")
	}
	if vtx1.Status() != choices.Accepted {
		t.Fatalf("Vertex should be accepted")
	}
}

// Vertex has a dependency and tx has a dependency
func TestBootstrapperTxDependencies(t *testing.T) {
	config, peerID, sender, state, vm := newConfig(t)

	utxos := []ids.ID{GenerateID(), GenerateID()}

	txID0 := GenerateID()
	txID1 := GenerateID()

	txBytes0 := []byte{0}
	txBytes1 := []byte{1}

	tx0 := &TestTx{
		TestTx: snowstorm.TestTx{
			Identifier: txID0,
			Stat:       choices.Processing,
		},
		bytes: txBytes0,
	}
	tx0.Ins.Add(utxos[0])

	tx1 := &TestTx{ // Depends on tx0
		TestTx: snowstorm.TestTx{
			Identifier: txID1,
			Deps:       []snowstorm.Tx{tx0},
			Stat:       choices.Processing,
		},
		bytes: txBytes1,
	}
	tx1.Ins.Add(utxos[1])

	vtxID0 := GenerateID()
	vtxID1 := GenerateID()

	vtxBytes0 := []byte{2}
	vtxBytes1 := []byte{3}
	vm.ParseTxF = func(b []byte) (snowstorm.Tx, error) {
		if bytes.Compare(b, txBytes0) == 0 {
			return tx0, nil
		} else if bytes.Compare(b, txBytes1) == 0 {
			return tx1, nil
		}
		return nil, errors.New("wrong tx")
	}

	vtx0 := &Vtx{
		id:     vtxID0,
		txs:    []snowstorm.Tx{tx1},
		height: 0,
		status: choices.Unknown,
		bytes:  vtxBytes0,
	}
	vtx1 := &Vtx{ // Depends on vtx0
		parents: []avalanche.Vertex{vtx0},
		id:      vtxID1,
		txs:     []snowstorm.Tx{tx0},
		height:  1,
		status:  choices.Processing,
		bytes:   vtxBytes1,
	}

	bs := bootstrapper{}
	bs.metrics.Initialize(config.Context.Log, fmt.Sprintf("gecko_%s", config.Context.ChainID), prometheus.NewRegistry())
	bs.Initialize(config)
	finished := new(bool)
	bs.onFinished = func() error { *finished = true; return nil }

	acceptedIDs := ids.Set{}
	acceptedIDs.Add(vtxID1)

	state.parseVertex = func(vtxBytes []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(vtxBytes, vtxBytes1):
			return vtx1, nil
		case bytes.Equal(vtxBytes, vtxBytes0):
			return vtx0, nil
		}
		t.Fatal(errParsedUnknownVertex)
		return nil, errParsedUnknownVertex
	}
	state.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID1):
			return vtx1, nil
		case vtxID.Equals(vtxID0):
			return nil, errUnknownVertex
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
	}

	reqIDPtr := new(uint32)
	sender.GetAncestorsF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if !vdr.Equals(peerID) {
			t.Fatalf("Should have requested vertex from %s, requested from %s", peerID, vdr)
		}
		switch {
		case vtxID.Equals(vtxID0):
		default:
			t.Fatal(errUnknownVertex)
		}

		*reqIDPtr = reqID
	}

	vm.CantBootstrapping = false

	if err := bs.ForceAccepted(acceptedIDs); err != nil { // should request vtx0
		t.Fatal(err)
	}

	state.parseVertex = func(vtxBytes []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(vtxBytes, vtxBytes1):
			return vtx1, nil
		case bytes.Equal(vtxBytes, vtxBytes0):
			vtx0.status = choices.Processing
			return vtx0, nil
		}
		t.Fatal(errParsedUnknownVertex)
		return nil, errParsedUnknownVertex
	}

	vm.CantBootstrapped = false

	if err := bs.MultiPut(peerID, *reqIDPtr, [][]byte{vtxBytes0}); err != nil {
		t.Fatal(err)
	}

	if !*finished {
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
	config, peerID, sender, state, vm := newConfig(t)

	utxos := []ids.ID{GenerateID(), GenerateID()}

	txID0 := GenerateID()
	txID1 := GenerateID()

	txBytes1 := []byte{1}

	tx0 := &TestTx{
		TestTx: snowstorm.TestTx{
			Identifier: txID0,
			Stat:       choices.Unknown,
		},
	}

	tx1 := &TestTx{ // depends on tx0
		TestTx: snowstorm.TestTx{
			Identifier: txID1,
			Deps:       []snowstorm.Tx{tx0},
			Stat:       choices.Processing,
		},
		bytes: txBytes1,
	}
	tx1.Ins.Add(utxos[1])

	vtxID0 := GenerateID()
	vtxID1 := GenerateID()

	vtxBytes0 := []byte{2}
	vtxBytes1 := []byte{3}

	vtx0 := &Vtx{
		id:     vtxID0,
		height: 0,
		status: choices.Unknown,
		bytes:  vtxBytes0,
	}
	vtx1 := &Vtx{ // depends on vtx0
		parents: []avalanche.Vertex{vtx0},
		id:      vtxID1,
		txs:     []snowstorm.Tx{tx1},
		height:  1,
		status:  choices.Processing,
		bytes:   vtxBytes1,
	}

	bs := bootstrapper{}
	bs.metrics.Initialize(config.Context.Log, fmt.Sprintf("gecko_%s", config.Context.ChainID), prometheus.NewRegistry())
	bs.Initialize(config)
	finished := new(bool)
	bs.onFinished = func() error { *finished = true; return nil }

	acceptedIDs := ids.Set{}
	acceptedIDs.Add(vtxID1)

	state.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID1):
			return vtx1, nil
		case vtxID.Equals(vtxID0):
			return nil, errUnknownVertex
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
	}
	state.parseVertex = func(vtxBytes []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(vtxBytes, vtxBytes1):
			return vtx1, nil
		case bytes.Equal(vtxBytes, vtxBytes0):
			vtx0.status = choices.Processing
			return vtx0, nil
		}
		t.Fatal(errParsedUnknownVertex)
		return nil, errParsedUnknownVertex
	}

	reqIDPtr := new(uint32)
	sender.GetAncestorsF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if !vdr.Equals(peerID) {
			t.Fatalf("Should have requested vertex from %s, requested from %s", peerID, vdr)
		}
		switch {
		case vtxID.Equals(vtxID0):
		default:
			t.Fatalf("Requested wrong vertex")
		}

		*reqIDPtr = reqID
	}

	vm.CantBootstrapping = false

	if err := bs.ForceAccepted(acceptedIDs); err != nil { // should request vtx1
		t.Fatal(err)
	}

	vm.CantBootstrapped = false

	if err := bs.MultiPut(peerID, *reqIDPtr, [][]byte{vtxBytes0}); err != nil {
		t.Fatal(err)
	}

	if !*finished {
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

func TestBootstrapperAcceptedFrontier(t *testing.T) {
	config, _, _, state, _ := newConfig(t)

	vtxID0 := GenerateID()
	vtxID1 := GenerateID()
	vtxID2 := GenerateID()

	bs := bootstrapper{}
	bs.metrics.Initialize(config.Context.Log, fmt.Sprintf("gecko_%s", config.Context.ChainID), prometheus.NewRegistry())
	bs.Initialize(config)

	state.edge = func() []ids.ID {
		return []ids.ID{
			vtxID0,
			vtxID1,
		}
	}

	accepted := bs.CurrentAcceptedFrontier()

	state.edge = nil

	if !accepted.Contains(vtxID0) {
		t.Fatalf("Vtx should be accepted")
	}
	if !accepted.Contains(vtxID1) {
		t.Fatalf("Vtx should be accepted")
	}
	if accepted.Contains(vtxID2) {
		t.Fatalf("Vtx shouldn't be accepted")
	}
}

func TestBootstrapperFilterAccepted(t *testing.T) {
	config, _, _, state, _ := newConfig(t)

	vtxID0 := GenerateID()
	vtxID1 := GenerateID()
	vtxID2 := GenerateID()

	vtx0 := &Vtx{
		id:     vtxID0,
		status: choices.Accepted,
	}
	vtx1 := &Vtx{
		id:     vtxID1,
		status: choices.Accepted,
	}

	bs := bootstrapper{}
	bs.metrics.Initialize(config.Context.Log, fmt.Sprintf("gecko_%s", config.Context.ChainID), prometheus.NewRegistry())
	bs.Initialize(config)

	vtxIDs := ids.Set{}
	vtxIDs.Add(
		vtxID0,
		vtxID1,
		vtxID2,
	)

	state.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID0):
			return vtx0, nil
		case vtxID.Equals(vtxID1):
			return vtx1, nil
		case vtxID.Equals(vtxID2):
			return nil, errUnknownVertex
		}
		t.Fatal(errUnknownVertex)
		return nil, errUnknownVertex
	}

	accepted := bs.FilterAccepted(vtxIDs)

	state.getVertex = nil

	if !accepted.Contains(vtxID0) {
		t.Fatalf("Vtx should be accepted")
	}
	if !accepted.Contains(vtxID1) {
		t.Fatalf("Vtx should be accepted")
	}
	if accepted.Contains(vtxID2) {
		t.Fatalf("Vtx shouldn't be accepted")
	}
}

// MultiPut only contains 1 of the two needed vertices; have to issue another GetAncestors
func TestBootstrapperIncompleteMultiPut(t *testing.T) {
	config, peerID, sender, state, vm := newConfig(t)

	vtxID0 := ids.Empty.Prefix(0)
	vtxID1 := ids.Empty.Prefix(1)
	vtxID2 := ids.Empty.Prefix(2)

	vtxBytes0 := []byte{0}
	vtxBytes1 := []byte{1}
	vtxBytes2 := []byte{2}

	vtx0 := &Vtx{
		id:     vtxID0,
		height: 0,
		status: choices.Unknown,
		bytes:  vtxBytes0,
	}
	vtx1 := &Vtx{
		id:      vtxID1,
		height:  0,
		parents: []avalanche.Vertex{vtx0},
		status:  choices.Unknown,
		bytes:   vtxBytes1,
	}
	vtx2 := &Vtx{
		id:      vtxID2,
		height:  0,
		parents: []avalanche.Vertex{vtx1},
		status:  choices.Processing,
		bytes:   vtxBytes2,
	}

	bs := bootstrapper{}
	bs.metrics.Initialize(config.Context.Log, fmt.Sprintf("gecko_%s", config.Context.ChainID), prometheus.NewRegistry())
	bs.Initialize(config)
	finished := new(bool)
	bs.onFinished = func() error { *finished = true; return nil }

	acceptedIDs := ids.Set{}
	acceptedIDs.Add(vtxID2)

	state.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID0):
			return nil, errUnknownVertex
		case vtxID.Equals(vtxID1):
			return nil, errUnknownVertex
		case vtxID.Equals(vtxID2):
			return vtx2, nil
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
	}
	state.parseVertex = func(vtxBytes []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(vtxBytes, vtxBytes0):
			vtx0.status = choices.Processing
			return vtx0, nil

		case bytes.Equal(vtxBytes, vtxBytes1):
			vtx1.status = choices.Processing
			return vtx1, nil
		case bytes.Equal(vtxBytes, vtxBytes2):
			return vtx2, nil
		}
		t.Fatal(errParsedUnknownVertex)
		return nil, errParsedUnknownVertex
	}
	reqIDPtr := new(uint32)
	requested := ids.Empty
	sender.GetAncestorsF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if !vdr.Equals(peerID) {
			t.Fatalf("Should have requested vertex from %s, requested from %s", peerID, vdr)
		}
		switch {
		case vtxID.Equals(vtxID1), vtxID.Equals(vtxID0):
		default:
			t.Fatal(errUnknownVertex)
		}
		*reqIDPtr = reqID
		requested = vtxID
	}

	vm.CantBootstrapping = false

	if err := bs.ForceAccepted(acceptedIDs); err != nil { // should request vtx1
		t.Fatal(err)
	} else if !requested.Equals(vtxID1) {
		t.Fatal("requested wrong vtx")
	}

	if err := bs.MultiPut(peerID, *reqIDPtr, [][]byte{vtxBytes1}); err != nil { // Provide vtx1; should request vtx0
		t.Fatal(err)
	} else if bs.finished {
		t.Fatalf("should not have finished")
	} else if !requested.Equals(vtxID0) {
		t.Fatal("should hae requested vtx0")
	}

	vm.CantBootstrapped = false

	if err := bs.MultiPut(peerID, *reqIDPtr, [][]byte{vtxBytes0}); err != nil { // Provide vtx0; can finish now
		t.Fatal(err)
	} else if !bs.finished {
		t.Fatal("should have finished")
	} else if vtx0.Status() != choices.Accepted {
		t.Fatal("should be accepted")
	} else if vtx1.Status() != choices.Accepted {
		t.Fatal("should be accepted")
	} else if vtx2.Status() != choices.Accepted {
		t.Fatal("should be accepted")
	}
}
