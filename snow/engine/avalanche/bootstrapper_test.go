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

	handler.Initialize(engine, make(chan common.Message), 1)
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

func TestBootstrapperSingleFrontier(t *testing.T) {
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

	acceptedIDs := ids.Set{}
	acceptedIDs.Add(
		vtxID0,
		vtxID1,
		vtxID2,
	)

	state.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID0), vtxID.Equals(vtxID1), vtxID.Equals(vtxID2):
			return nil, errUnknownVertex
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
	}

	vtxIDToReqID := map[[32]byte]uint32{}
	sender.GetF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if !vdr.Equals(peerID) {
			t.Fatalf("Should have requested vertex from %s, requested from %s", peerID, vdr)
		}
		switch {
		case vtxID.Equals(vtxID0), vtxID.Equals(vtxID1), vtxID.Equals(vtxID2):
		default:
			t.Fatalf("Requested unknown vertex")
		}

		vtxKey := vtxID.Key()
		if _, ok := vtxIDToReqID[vtxKey]; ok {
			t.Fatalf("Message sent multiple times")
		}
		vtxIDToReqID[vtxKey] = reqID
	}

	vm.CantBootstrapping = false

	bs.ForceAccepted(acceptedIDs)

	vm.CantBootstrapping = true

	state.getVertex = nil
	sender.GetF = nil

	if numReqs := len(vtxIDToReqID); numReqs != 3 {
		t.Fatalf("Should have requested %d vertices, %d were requested", 3, numReqs)
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

	state.edge = func() []ids.ID {
		return []ids.ID{
			vtxID0,
			vtxID1,
			vtxID2,
		}
	}

	state.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID0):
			return vtx0, nil
		case vtxID.Equals(vtxID1):
			return vtx1, nil
		case vtxID.Equals(vtxID2):
			return vtx2, nil
		default:
			t.Fatalf("Requested unknown vertex")
			panic("Requested unknown vertex")
		}
	}

	finished := new(bool)
	bs.onFinished = func() error { *finished = true; return nil }
	vm.CantBootstrapped = false

	for vtxKey, reqID := range vtxIDToReqID {
		vtxID := ids.NewID(vtxKey)

		switch {
		case vtxID.Equals(vtxID0):
			bs.Put(peerID, reqID, vtxID, vtxBytes0)
		case vtxID.Equals(vtxID1):
			bs.Put(peerID, reqID, vtxID, vtxBytes1)
		case vtxID.Equals(vtxID2):
			bs.Put(peerID, reqID, vtxID, vtxBytes2)
		default:
			t.Fatalf("Requested unknown vertex")
		}
	}

	state.parseVertex = nil
	state.edge = nil
	bs.onFinished = nil
	vm.CantBootstrapped = true

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

func TestBootstrapperUnknownByzantineResponse(t *testing.T) {
	config, peerID, sender, state, vm := newConfig(t)

	vtxID0 := ids.Empty.Prefix(0)
	vtxID1 := ids.Empty.Prefix(1)

	vtxBytes0 := []byte{0}
	vtxBytes1 := []byte{1}

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

	bs := bootstrapper{}
	bs.metrics.Initialize(config.Context.Log, fmt.Sprintf("gecko_%s", config.Context.ChainID), prometheus.NewRegistry())
	bs.Initialize(config)

	acceptedIDs := ids.Set{}
	acceptedIDs.Add(
		vtxID0,
	)

	state.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID0):
			return nil, errUnknownVertex
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
	}

	requestID := new(uint32)
	sender.GetF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if !vdr.Equals(peerID) {
			t.Fatalf("Should have requested vertex from %s, requested from %s", peerID, vdr)
		}
		switch {
		case vtxID.Equals(vtxID0):
		default:
			t.Fatalf("Requested unknown vertex")
		}

		*requestID = reqID
	}

	vm.CantBootstrapping = false

	bs.ForceAccepted(acceptedIDs)

	vm.CantBootstrapping = true

	state.getVertex = nil

	state.parseVertex = func(vtxBytes []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(vtxBytes, vtxBytes0):
			return vtx0, nil
		case bytes.Equal(vtxBytes, vtxBytes1):
			return vtx1, nil
		}
		t.Fatal(errParsedUnknownVertex)
		return nil, errParsedUnknownVertex
	}

	state.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID0):
			return vtx0, nil
		case vtxID.Equals(vtxID1):
			return vtx1, nil
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
	}

	finished := new(bool)
	bs.onFinished = func() error { *finished = true; return nil }
	vm.CantBootstrapped = false

	bs.Put(peerID, *requestID, vtxID1, vtxBytes1)
	bs.Put(peerID, *requestID, vtxID0, vtxBytes0)

	vm.CantBootstrapped = true

	state.parseVertex = nil
	state.edge = nil
	bs.onFinished = nil

	if !*finished {
		t.Fatalf("Bootstrapping should have finished")
	}
	if vtx0.Status() != choices.Accepted {
		t.Fatalf("Vertex should be accepted")
	}
	if vtx1.Status() != choices.Processing {
		t.Fatalf("Vertex should be processing")
	}
}

func TestBootstrapperVertexDependencies(t *testing.T) {
	config, peerID, sender, state, vm := newConfig(t)

	vtxID0 := ids.Empty.Prefix(0)
	vtxID1 := ids.Empty.Prefix(1)

	vtxBytes0 := []byte{0}
	vtxBytes1 := []byte{1}

	vtx0 := &Vtx{
		id:     vtxID0,
		height: 0,
		status: choices.Unknown,
		bytes:  vtxBytes0,
	}
	vtx1 := &Vtx{
		parents: []avalanche.Vertex{vtx0},
		id:      vtxID1,
		height:  1,
		status:  choices.Processing,
		bytes:   vtxBytes1,
	}

	bs := bootstrapper{}
	bs.metrics.Initialize(config.Context.Log, fmt.Sprintf("gecko_%s", config.Context.ChainID), prometheus.NewRegistry())
	bs.Initialize(config)

	acceptedIDs := ids.Set{}
	acceptedIDs.Add(
		vtxID1,
	)

	state.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID1):
			return nil, errUnknownVertex
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
	}

	reqIDPtr := new(uint32)
	sender.GetF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if !vdr.Equals(peerID) {
			t.Fatalf("Should have requested vertex from %s, requested from %s", peerID, vdr)
		}
		switch {
		case vtxID.Equals(vtxID1):
		default:
			t.Fatalf("Requested unknown vertex")
		}

		*reqIDPtr = reqID
	}

	vm.CantBootstrapping = false

	bs.ForceAccepted(acceptedIDs)

	vm.CantBootstrapping = true

	state.getVertex = nil
	sender.GetF = nil

	state.parseVertex = func(vtxBytes []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(vtxBytes, vtxBytes1):
			return vtx1, nil
		}
		t.Fatal(errParsedUnknownVertex)
		return nil, errParsedUnknownVertex
	}
	sender.GetF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
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

	bs.Put(peerID, *reqIDPtr, vtxID1, vtxBytes1)

	state.parseVertex = nil
	sender.GetF = nil

	if vtx0.Status() != choices.Unknown {
		t.Fatalf("Vertex should be unknown")
	}
	if vtx1.Status() != choices.Processing {
		t.Fatalf("Vertex should be processing")
	}

	vtx0.status = choices.Processing

	state.parseVertex = func(vtxBytes []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(vtxBytes, vtxBytes0):
			return vtx0, nil
		case bytes.Equal(vtxBytes, vtxBytes1):
			return vtx1, nil
		}
		t.Fatal(errParsedUnknownVertex)
		return nil, errParsedUnknownVertex
	}

	state.edge = func() []ids.ID {
		return []ids.ID{
			vtxID0,
			vtxID1,
		}
	}

	state.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID0):
			return vtx0, nil
		case vtxID.Equals(vtxID1):
			return vtx1, nil
		default:
			t.Fatalf("Requested unknown vertex")
			panic("Requested unknown vertex")
		}
	}

	finished := new(bool)
	bs.onFinished = func() error { *finished = true; return nil }
	vm.CantBootstrapped = false

	bs.Put(peerID, *reqIDPtr, vtxID0, vtxBytes0)

	state.parseVertex = nil
	bs.onFinished = nil
	vm.CantBootstrapped = true

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

	tx1 := &TestTx{
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
		txs:    []snowstorm.Tx{tx1},
		height: 0,
		status: choices.Unknown,
		bytes:  vtxBytes0,
	}
	vtx1 := &Vtx{
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

	acceptedIDs := ids.Set{}
	acceptedIDs.Add(
		vtxID1,
	)

	state.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID1):
			return nil, errUnknownVertex
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
	}

	reqIDPtr := new(uint32)
	sender.GetF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if !vdr.Equals(peerID) {
			t.Fatalf("Should have requested vertex from %s, requested from %s", peerID, vdr)
		}
		switch {
		case vtxID.Equals(vtxID1):
		default:
			t.Fatal(errUnknownVertex)
		}

		*reqIDPtr = reqID
	}

	vm.CantBootstrapping = false

	bs.ForceAccepted(acceptedIDs)

	vm.CantBootstrapping = true

	state.getVertex = nil
	sender.GetF = nil

	state.parseVertex = func(vtxBytes []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(vtxBytes, vtxBytes1):
			return vtx1, nil
		}
		t.Fatal(errParsedUnknownVertex)
		return nil, errParsedUnknownVertex
	}
	sender.GetF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
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

	bs.Put(peerID, *reqIDPtr, vtxID1, vtxBytes1)

	state.parseVertex = nil
	sender.GetF = nil

	if tx0.Status() != choices.Processing {
		t.Fatalf("Tx should be processing")
	}
	if tx1.Status() != choices.Processing {
		t.Fatalf("Tx should be processing")
	}

	if vtx0.Status() != choices.Unknown {
		t.Fatalf("Vertex should be unknown")
	}
	if vtx1.Status() != choices.Processing {
		t.Fatalf("Vertex should be processing")
	}

	tx0.Stat = choices.Processing
	vtx0.status = choices.Processing

	vm.ParseTxF = func(txBytes []byte) (snowstorm.Tx, error) {
		switch {
		case bytes.Equal(txBytes, txBytes0):
			return tx0, nil
		case bytes.Equal(txBytes, txBytes1):
			return tx1, nil
		}
		t.Fatal(errParsedUnknownVertex)
		return nil, errParsedUnknownVertex
	}
	state.parseVertex = func(vtxBytes []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(vtxBytes, vtxBytes0):
			return vtx0, nil
		case bytes.Equal(vtxBytes, vtxBytes1):
			return vtx1, nil
		}
		t.Fatal(errParsedUnknownVertex)
		return nil, errParsedUnknownVertex
	}

	state.edge = func() []ids.ID {
		return []ids.ID{
			vtxID0,
			vtxID1,
		}
	}

	state.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID0):
			return vtx0, nil
		case vtxID.Equals(vtxID1):
			return vtx1, nil
		default:
			t.Fatalf("Requested unknown vertex")
			panic("Requested unknown vertex")
		}
	}

	finished := new(bool)
	bs.onFinished = func() error { *finished = true; return nil }
	vm.CantBootstrapped = false

	bs.Put(peerID, *reqIDPtr, vtxID0, vtxBytes0)

	state.parseVertex = nil
	bs.onFinished = nil
	vm.CantBootstrapped = true

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

func TestBootstrapperMissingTxDependency(t *testing.T) {
	config, peerID, sender, state, vm := newConfig(t)

	utxos := []ids.ID{GenerateID(), GenerateID()}

	txID0 := GenerateID()
	txID1 := GenerateID()

	txBytes0 := []byte{0}
	txBytes1 := []byte{1}

	tx0 := &TestTx{
		TestTx: snowstorm.TestTx{
			Identifier: txID0,
			Stat:       choices.Unknown,
		},
	}

	tx1 := &TestTx{
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
	vtx1 := &Vtx{
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

	acceptedIDs := ids.Set{}
	acceptedIDs.Add(
		vtxID1,
	)

	state.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID1):
			return nil, errUnknownVertex
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
	}

	reqIDPtr := new(uint32)
	sender.GetF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if !vdr.Equals(peerID) {
			t.Fatalf("Should have requested vertex from %s, requested from %s", peerID, vdr)
		}
		switch {
		case vtxID.Equals(vtxID1):
		default:
			t.Fatalf("Requested unknown vertex")
		}

		*reqIDPtr = reqID
	}
	vm.CantBootstrapping = false

	bs.ForceAccepted(acceptedIDs)

	state.getVertex = nil
	sender.GetF = nil
	vm.CantBootstrapping = true

	state.parseVertex = func(vtxBytes []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(vtxBytes, vtxBytes1):
			return vtx1, nil
		}
		t.Fatal(errParsedUnknownVertex)
		return nil, errParsedUnknownVertex
	}
	sender.GetF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
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

	bs.Put(peerID, *reqIDPtr, vtxID1, vtxBytes1)

	state.parseVertex = nil
	sender.GetF = nil

	if tx0.Status() != choices.Unknown {
		t.Fatalf("Tx should be unknown")
	}
	if tx1.Status() != choices.Processing {
		t.Fatalf("Tx should be processing")
	}

	if vtx0.Status() != choices.Unknown {
		t.Fatalf("Vertex should be unknown")
	}
	if vtx1.Status() != choices.Processing {
		t.Fatalf("Vertex should be processing")
	}

	vtx0.status = choices.Processing

	vm.ParseTxF = func(txBytes []byte) (snowstorm.Tx, error) {
		switch {
		case bytes.Equal(txBytes, txBytes0):
			return tx0, nil
		case bytes.Equal(txBytes, txBytes1):
			return tx1, nil
		}
		t.Fatal(errParsedUnknownVertex)
		return nil, errParsedUnknownVertex
	}
	state.parseVertex = func(vtxBytes []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(vtxBytes, vtxBytes0):
			return vtx0, nil
		case bytes.Equal(vtxBytes, vtxBytes1):
			return vtx1, nil
		}
		t.Fatal(errParsedUnknownVertex)
		return nil, errParsedUnknownVertex
	}

	state.edge = func() []ids.ID {
		return []ids.ID{
			vtxID0,
		}
	}

	state.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID0):
			return vtx0, nil
		default:
			t.Fatalf("Requested unknown vertex")
			panic("Requested unknown vertex")
		}
	}

	finished := new(bool)
	bs.onFinished = func() error { *finished = true; return nil }
	vm.CantBootstrapped = false

	bs.Put(peerID, *reqIDPtr, vtxID0, vtxBytes0)

	state.parseVertex = nil
	bs.onFinished = nil
	vm.CantBootstrapped = true

	if !*finished {
		t.Fatalf("Bootstrapping should have finished")
	}
	if tx0.Status() != choices.Unknown {
		t.Fatalf("Tx should be unknown")
	}
	if tx1.Status() != choices.Processing {
		t.Fatalf("Tx should be processing")
	}

	if vtx0.Status() != choices.Accepted {
		t.Fatalf("Vertex should be accepted")
	}
	if vtx1.Status() != choices.Processing {
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

func TestBootstrapperPartialFetch(t *testing.T) {
	config, _, sender, state, vm := newConfig(t)

	vtxID0 := ids.Empty.Prefix(0)
	vtxID1 := ids.Empty.Prefix(1)

	vtxBytes0 := []byte{0}

	vtx0 := &Vtx{
		id:     vtxID0,
		height: 0,
		status: choices.Processing,
		bytes:  vtxBytes0,
	}

	bs := bootstrapper{}
	bs.metrics.Initialize(config.Context.Log, fmt.Sprintf("gecko_%s", config.Context.ChainID), prometheus.NewRegistry())
	bs.Initialize(config)

	acceptedIDs := ids.Set{}
	acceptedIDs.Add(
		vtxID0,
		vtxID1,
	)

	state.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID0):
			return vtx0, nil
		case vtxID.Equals(vtxID1):
			return nil, errUnknownVertex
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
	}

	sender.CantGet = false
	vm.CantBootstrapping = false

	bs.ForceAccepted(acceptedIDs)

	if bs.finished {
		t.Fatalf("should have requested a vertex")
	}

	if bs.pending.Len() != 1 {
		t.Fatalf("wrong number pending")
	}
}

func TestBootstrapperWrongIDByzantineResponse(t *testing.T) {
	config, peerID, sender, state, vm := newConfig(t)

	vtxID0 := ids.Empty.Prefix(0)
	vtxID1 := ids.Empty.Prefix(1)

	vtxBytes0 := []byte{0}
	vtxBytes1 := []byte{1}

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

	bs := bootstrapper{}
	bs.metrics.Initialize(config.Context.Log, fmt.Sprintf("gecko_%s", config.Context.ChainID), prometheus.NewRegistry())
	bs.Initialize(config)

	acceptedIDs := ids.Set{}
	acceptedIDs.Add(
		vtxID0,
	)

	state.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID0):
			return nil, errUnknownVertex
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
	}

	requestID := new(uint32)
	sender.GetF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if !vdr.Equals(peerID) {
			t.Fatalf("Should have requested vertex from %s, requested from %s", peerID, vdr)
		}
		switch {
		case vtxID.Equals(vtxID0):
		default:
			t.Fatalf("Requested unknown vertex")
		}

		*requestID = reqID
	}
	vm.CantBootstrapping = false

	bs.ForceAccepted(acceptedIDs)

	state.getVertex = nil
	sender.GetF = nil
	vm.CantBootstrapping = true

	state.parseVertex = func(vtxBytes []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(vtxBytes, vtxBytes0):
			return vtx0, nil
		case bytes.Equal(vtxBytes, vtxBytes1):
			return vtx1, nil
		}
		t.Fatal(errParsedUnknownVertex)
		return nil, errParsedUnknownVertex
	}

	state.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID0):
			return vtx0, nil
		case vtxID.Equals(vtxID1):
			return vtx1, nil
		default:
			t.Fatal(errUnknownVertex)
			panic(errUnknownVertex)
		}
	}

	finished := new(bool)
	bs.onFinished = func() error { *finished = true; return nil }
	sender.CantGet = false
	vm.CantBootstrapped = false

	bs.Put(peerID, *requestID, vtxID0, vtxBytes1)

	sender.CantGet = true

	bs.Put(peerID, *requestID, vtxID0, vtxBytes0)

	state.parseVertex = nil
	state.edge = nil
	bs.onFinished = nil
	vm.CantBootstrapped = true

	if !*finished {
		t.Fatalf("Bootstrapping should have finished")
	}
	if vtx0.Status() != choices.Accepted {
		t.Fatalf("Vertex should be accepted")
	}
	if vtx1.Status() != choices.Processing {
		t.Fatalf("Vertex should be processing")
	}
}
