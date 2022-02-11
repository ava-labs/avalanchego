// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"bytes"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/bootstrap"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"

	avagetter "github.com/ava-labs/avalanchego/snow/engine/avalanche/getter"
)

var (
	errUnknownVertex = errors.New("unknown vertex")
	errFailedParsing = errors.New("failed parsing")
	errMissing       = errors.New("missing")
)

type dummyHandler struct {
	startEngineF func(startReqID uint32) error
}

func (dh *dummyHandler) onDoneBootstrapping(lastReqID uint32) error {
	lastReqID++
	return dh.startEngineF(lastReqID)
}

func TestEngineShutdown(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	vmShutdownCalled := false
	vm := &vertex.TestVM{}
	vm.T = t
	vm.ShutdownF = func() error { vmShutdownCalled = true; return nil }
	engCfg.VM = vm

	transitive, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := transitive.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if !vmShutdownCalled {
		t.Fatal("Shutting down the Transitive did not shutdown the VM")
	}
}

func TestEngineAdd(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	bootCfg.Sender = sender
	engCfg.Sender = sender

	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false

	manager := vertex.NewTestManager(t)
	engCfg.Manager = manager

	manager.Default(true)

	manager.CantEdge = false

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	if te.Ctx.ChainID != ids.Empty {
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
	sender.SendGetF = func(inVdr ids.ShortID, requestID uint32, vtxID ids.ID) {
		*reqID = requestID
		if *asked {
			t.Fatalf("Asked multiple times")
		}
		*asked = true
		if vdr != inVdr {
			t.Fatalf("Asking wrong validator for vertex")
		}
		if vtx.ParentsV[0].ID() != vtxID {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	manager.ParseVtxF = func(b []byte) (avalanche.Vertex, error) {
		if !bytes.Equal(b, vtx.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return vtx, nil
	}

	if err := te.Put(vdr, 0, vtx.Bytes()); err != nil {
		t.Fatal(err)
	}

	manager.ParseVtxF = nil

	if !*asked {
		t.Fatalf("Didn't ask for a missing vertex")
	}

	if len(te.vtxBlocked) != 1 {
		t.Fatalf("Should have been blocking on request")
	}

	manager.ParseVtxF = func(b []byte) (avalanche.Vertex, error) { return nil, errFailedParsing }

	if err := te.Put(vdr, *reqID, nil); err != nil {
		t.Fatal(err)
	}

	manager.ParseVtxF = nil

	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Should have finished blocking issue")
	}
}

func TestEngineQuery(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	bootCfg.Sender = sender
	engCfg.Sender = sender

	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false

	manager := vertex.NewTestManager(t)
	bootCfg.Manager = manager
	engCfg.Manager = manager

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
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

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
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}

		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	vertexed := new(bool)
	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if *vertexed {
			t.Fatalf("Sent multiple requests")
		}
		*vertexed = true
		if vtxID != vtx0.ID() {
			t.Fatalf("Wrong vertex requested")
		}
		return nil, errUnknownVertex
	}

	asked := new(bool)
	sender.SendGetF = func(inVdr ids.ShortID, _ uint32, vtxID ids.ID) {
		if *asked {
			t.Fatalf("Asked multiple times")
		}
		*asked = true
		if vdr != inVdr {
			t.Fatalf("Asking wrong validator for vertex")
		}
		if vtx0.ID() != vtxID {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	// After receiving the pull query for [vtx0] we will first request [vtx0]
	// from the peer, because it is currently unknown to the engine.
	if err := te.PullQuery(vdr, 0, vtx0.ID()); err != nil {
		t.Fatal(err)
	}

	if !*vertexed {
		t.Fatalf("Didn't request vertex")
	}
	if !*asked {
		t.Fatalf("Didn't request vertex from validator")
	}

	queried := new(bool)
	queryRequestID := new(uint32)
	sender.SendPushQueryF = func(inVdrs ids.ShortSet, requestID uint32, vtxID ids.ID, vtx []byte) {
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		*queryRequestID = requestID
		vdrSet := ids.ShortSet{}
		vdrSet.Add(vdr)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if vtx0.ID() != vtxID {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	chitted := new(bool)
	sender.SendChitsF = func(inVdr ids.ShortID, _ uint32, prefs []ids.ID) {
		if *chitted {
			t.Fatalf("Sent multiple chits")
		}
		*chitted = true
		if len(prefs) != 1 || prefs[0] != vtx0.ID() {
			t.Fatalf("Wrong chits preferences")
		}
	}

	manager.ParseVtxF = func(b []byte) (avalanche.Vertex, error) {
		if !bytes.Equal(b, vtx0.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return vtx0, nil
	}

	// Once the peer returns [vtx0], we will respond to its query and then issue
	// our own push query for [vtx0].
	if err := te.Put(vdr, 0, vtx0.Bytes()); err != nil {
		t.Fatal(err)
	}
	manager.ParseVtxF = nil

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

	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if vtxID == vtx0.ID() {
			return &avalanche.TestVertex{
				TestDecidable: choices.TestDecidable{
					StatusV: choices.Unknown,
				},
			}, nil
		}
		if vtxID == vtx1.ID() {
			return nil, errUnknownVertex
		}
		t.Fatalf("Wrong vertex requested")
		panic("Should have failed")
	}

	*asked = false
	sender.SendGetF = func(inVdr ids.ShortID, _ uint32, vtxID ids.ID) {
		if *asked {
			t.Fatalf("Asked multiple times")
		}
		*asked = true
		if vdr != inVdr {
			t.Fatalf("Asking wrong validator for vertex")
		}
		if vtx1.ID() != vtxID {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	// The peer returned [vtx1] from our query for [vtx0], which means we will
	// need to request the missing [vtx1].
	if err := te.Chits(vdr, *queryRequestID, []ids.ID{vtx1.ID()}); err != nil {
		t.Fatal(err)
	}

	*queried = false
	sender.SendPushQueryF = func(inVdrs ids.ShortSet, requestID uint32, vtxID ids.ID, vtx []byte) {
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		*queryRequestID = requestID
		vdrSet := ids.ShortSet{}
		vdrSet.Add(vdr)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if vtx1.ID() != vtxID {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	manager.ParseVtxF = func(b []byte) (avalanche.Vertex, error) {
		if !bytes.Equal(b, vtx1.Bytes()) {
			t.Fatalf("Wrong bytes")
		}

		manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
			if vtxID == vtx0.ID() {
				return &avalanche.TestVertex{
					TestDecidable: choices.TestDecidable{
						StatusV: choices.Processing,
					},
				}, nil
			}
			if vtxID == vtx1.ID() {
				return vtx1, nil
			}
			t.Fatalf("Wrong vertex requested")
			panic("Should have failed")
		}

		return vtx1, nil
	}

	// Once the peer returns [vtx1], the poll that was issued for [vtx0] will be
	// able to terminate. Additionally the node will issue a push query with
	// [vtx1].
	if err := te.Put(vdr, 0, vtx1.Bytes()); err != nil {
		t.Fatal(err)
	}
	manager.ParseVtxF = nil

	// Because [vtx1] does not transitively reference [vtx0], the transaction
	// vertex for [vtx0] was never voted for. This results in [vtx0] still being
	// in processing.
	if vtx0.Status() != choices.Processing {
		t.Fatalf("Shouldn't have executed the vertex yet")
	}
	if vtx1.Status() != choices.Accepted {
		t.Fatalf("Should have executed the vertex")
	}
	if tx0.Status() != choices.Accepted {
		t.Fatalf("Should have executed the transaction")
	}

	// Make sure there is no memory leak for missing vertex tracking.
	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Should have finished blocking")
	}

	sender.CantSendPullQuery = false

	// Abandon the query for [vtx1]. This will result in a re-query for [vtx0].
	if err := te.QueryFailed(vdr, *queryRequestID); err != nil {
		t.Fatal(err)
	}
	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Should have finished blocking")
	}
}

func TestEngineMultipleQuery(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	engCfg.Params = avalanche.Parameters{
		Parameters: snowball.Parameters{
			K:                     3,
			Alpha:                 2,
			BetaVirtuous:          1,
			BetaRogue:             2,
			ConcurrentRepolls:     1,
			OptimalProcessing:     100,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}

	vdr0 := ids.GenerateTestShortID()
	vdr1 := ids.GenerateTestShortID()
	vdr2 := ids.GenerateTestShortID()

	errs := wrappers.Errs{}
	errs.Add(
		vals.AddWeight(vdr0, 1),
		vals.AddWeight(vdr1, 1),
		vals.AddWeight(vdr2, 1),
	)
	if errs.Errored() {
		t.Fatal(errs.Err)
	}

	sender := &common.SenderTest{T: t}
	bootCfg.Sender = sender
	engCfg.Sender = sender

	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false

	manager := vertex.NewTestManager(t)
	bootCfg.Manager = manager
	engCfg.Manager = manager

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
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	vtx0 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	queried := new(bool)
	queryRequestID := new(uint32)
	sender.SendPushQueryF = func(inVdrs ids.ShortSet, requestID uint32, vtxID ids.ID, vtx []byte) {
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		*queryRequestID = requestID
		vdrSet := ids.ShortSet{}
		vdrSet.Add(vdr0, vdr1, vdr2)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if vtx0.ID() != vtxID {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	if err := te.issue(vtx0); err != nil {
		t.Fatal(err)
	}

	vtx1 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		case vtx0.ID():
			return vtx0, nil
		case vtx1.ID():
			return nil, errUnknownVertex
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	asked := new(bool)
	reqID := new(uint32)
	sender.SendGetF = func(inVdr ids.ShortID, requestID uint32, vtxID ids.ID) {
		*reqID = requestID
		if *asked {
			t.Fatalf("Asked multiple times")
		}
		*asked = true
		if vdr0 != inVdr {
			t.Fatalf("Asking wrong validator for vertex")
		}
		if vtx1.ID() != vtxID {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	s0 := []ids.ID{vtx0.ID(), vtx1.ID()}

	s2 := []ids.ID{vtx0.ID()}

	if err := te.Chits(vdr0, *queryRequestID, s0); err != nil {
		t.Fatal(err)
	}
	if err := te.QueryFailed(vdr1, *queryRequestID); err != nil {
		t.Fatal(err)
	}
	if err := te.Chits(vdr2, *queryRequestID, s2); err != nil {
		t.Fatal(err)
	}

	// Should be dropped because the query was marked as failed
	if err := te.Chits(vdr1, *queryRequestID, s0); err != nil {
		t.Fatal(err)
	}

	if err := te.GetFailed(vdr0, *reqID); err != nil {
		t.Fatal(err)
	}

	if vtx0.Status() != choices.Accepted {
		t.Fatalf("Should have executed vertex")
	}
	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Should have finished blocking")
	}
}

func TestEngineBlockedIssue(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	engCfg.Manager = manager

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
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

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

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	if err := te.issue(vtx1); err != nil {
		t.Fatal(err)
	}

	vtx1.ParentsV[0] = vtx0
	if err := te.issue(vtx0); err != nil {
		t.Fatal(err)
	}

	if prefs := te.Consensus.Preferences(); prefs.Len() != 1 || !prefs.Contains(vtx1.ID()) {
		t.Fatalf("Should have issued vtx1")
	}
}

func TestEngineAbandonResponse(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	bootCfg.Manager = manager
	engCfg.Manager = manager

	sender := &common.SenderTest{T: t}
	bootCfg.Sender = sender
	engCfg.Sender = sender

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
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	vtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) { return nil, errUnknownVertex }

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	reqID := new(uint32)
	sender.SendGetF = func(vID ids.ShortID, requestID uint32, vtxID ids.ID) {
		*reqID = requestID
	}
	sender.CantSendChits = false

	if err := te.PullQuery(vdr, 0, vtx.ID()); err != nil {
		t.Fatal(err)
	}
	if err := te.GetFailed(vdr, *reqID); err != nil {
		t.Fatal(err)
	}

	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Should have removed blocking event")
	}
}

func TestEngineScheduleRepoll(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

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
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	vtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	manager := vertex.NewTestManager(t)
	bootCfg.Manager = manager
	engCfg.Manager = manager

	manager.Default(true)
	manager.CantEdge = false

	sender := &common.SenderTest{T: t}
	bootCfg.Sender = sender
	engCfg.Sender = sender

	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	requestID := new(uint32)
	sender.SendPushQueryF = func(_ ids.ShortSet, reqID uint32, _ ids.ID, _ []byte) {
		*requestID = reqID
	}

	if err := te.issue(vtx); err != nil {
		t.Fatal(err)
	}

	sender.SendPushQueryF = nil

	repolled := new(bool)
	sender.SendPullQueryF = func(_ ids.ShortSet, _ uint32, vtxID ids.ID) {
		*repolled = true
		if vtxID != vtx.ID() {
			t.Fatalf("Wrong vertex queried")
		}
	}

	if err := te.QueryFailed(vdr, *requestID); err != nil {
		t.Fatal(err)
	}

	if !*repolled {
		t.Fatalf("Should have issued a noop")
	}
}

func TestEngineRejectDoubleSpendTx(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	engCfg.Params.BatchSize = 2

	sender := &common.SenderTest{T: t}
	bootCfg.Sender = sender
	engCfg.Sender = sender

	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	engCfg.Manager = manager
	manager.Default(true)

	vm := &vertex.TestVM{TestVM: common.TestVM{T: t}}
	bootCfg.VM = vm
	engCfg.VM = vm
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
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[0])

	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}
	manager.BuildVtxF = func(_ []ids.ID, txs []snowstorm.Tx) (avalanche.Vertex, error) {
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

	vm.CantSetState = false
	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	vm.CantSetState = true
	sender.CantSendPushQuery = false
	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx0, tx1} }
	if err := te.Notify(common.PendingTxs); err != nil {
		t.Fatal(err)
	}
}

func TestEngineRejectDoubleSpendIssuedTx(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	engCfg.Params.BatchSize = 2

	sender := &common.SenderTest{T: t}
	bootCfg.Sender = sender
	engCfg.Sender = sender
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	bootCfg.Manager = manager
	engCfg.Manager = manager
	manager.Default(true)

	vm := &vertex.TestVM{TestVM: common.TestVM{T: t}}
	bootCfg.VM = vm
	engCfg.VM = vm
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
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[0])

	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	vm.CantSetState = false
	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	vm.CantSetState = true
	manager.BuildVtxF = func(_ []ids.ID, txs []snowstorm.Tx) (avalanche.Vertex, error) {
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

	sender.CantSendPushQuery = false

	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx0} }
	if err := te.Notify(common.PendingTxs); err != nil {
		t.Fatal(err)
	}

	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx1} }
	if err := te.Notify(common.PendingTxs); err != nil {
		t.Fatal(err)
	}
}

func TestEngineIssueRepoll(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	engCfg.Params.BatchSize = 2

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	bootCfg.Sender = sender
	engCfg.Sender = sender

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	bootCfg.Manager = manager
	engCfg.Manager = manager

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	sender.SendPullQueryF = func(vdrs ids.ShortSet, _ uint32, vtxID ids.ID) {
		vdrSet := ids.ShortSet{}
		vdrSet.Add(vdr)
		if !vdrs.Equals(vdrSet) {
			t.Fatalf("Wrong query recipients")
		}
		if vtxID != gVtx.ID() && vtxID != mVtx.ID() {
			t.Fatalf("Unknown re-query")
		}
	}

	te.repoll()
	if err := te.errs.Err; err != nil {
		t.Fatal(err)
	}
}

func TestEngineReissue(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	engCfg.Params.BatchSize = 2
	engCfg.Params.BetaVirtuous = 5
	engCfg.Params.BetaRogue = 5

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	bootCfg.Sender = sender
	engCfg.Sender = sender

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	bootCfg.Manager = manager
	engCfg.Manager = manager

	vm := &vertex.TestVM{TestVM: common.TestVM{T: t}}
	vm.Default(true)
	bootCfg.VM = vm
	engCfg.VM = vm

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
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[1])

	tx2 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx2.InputIDsV = append(tx2.InputIDsV, utxos[1])

	tx3 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx3.InputIDsV = append(tx3.InputIDsV, utxos[0])

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
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		case vtx.ID():
			return vtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	vm.CantSetState = false
	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	vm.CantSetState = true
	lastVtx := new(avalanche.TestVertex)
	manager.BuildVtxF = func(_ []ids.ID, txs []snowstorm.Tx) (avalanche.Vertex, error) {
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
		if id != tx0.ID() {
			t.Fatalf("Wrong tx")
		}
		return tx0, nil
	}

	queryRequestID := new(uint32)
	sender.SendPushQueryF = func(_ ids.ShortSet, requestID uint32, _ ids.ID, _ []byte) {
		*queryRequestID = requestID
	}

	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx0, tx1} }
	if err := te.Notify(common.PendingTxs); err != nil {
		t.Fatal(err)
	}

	manager.ParseVtxF = func(b []byte) (avalanche.Vertex, error) {
		if !bytes.Equal(b, vtx.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return vtx, nil
	}

	// must vote on the first poll for the second one to settle
	// *queryRequestID is 1
	if err := te.Chits(vdr, *queryRequestID, []ids.ID{vtx.ID()}); err != nil {
		t.Fatal(err)
	}

	if err := te.Put(vdr, 0, vtx.Bytes()); err != nil {
		t.Fatal(err)
	}
	manager.ParseVtxF = nil

	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx3} }
	if err := te.Notify(common.PendingTxs); err != nil {
		t.Fatal(err)
	}

	// vote on second poll, *queryRequestID is 2
	if err := te.Chits(vdr, *queryRequestID, []ids.ID{vtx.ID()}); err != nil {
		t.Fatal(err)
	}

	// all polls settled

	if len(lastVtx.TxsV) != 1 || lastVtx.TxsV[0].ID() != tx0.ID() {
		t.Fatalf("Should have re-issued the tx")
	}
}

func TestEngineLargeIssue(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()
	engCfg.Params.BatchSize = 1
	engCfg.Params.BetaVirtuous = 5
	engCfg.Params.BetaRogue = 5

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	bootCfg.Sender = sender
	engCfg.Sender = sender

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	bootCfg.Manager = manager
	engCfg.Manager = manager

	vm := &vertex.TestVM{TestVM: common.TestVM{T: t}}
	vm.Default(true)
	bootCfg.VM = vm
	engCfg.VM = vm

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
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[1])

	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	vm.CantSetState = false
	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	vm.CantSetState = true
	lastVtx := new(avalanche.TestVertex)
	manager.BuildVtxF = func(_ []ids.ID, txs []snowstorm.Tx) (avalanche.Vertex, error) {
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

	sender.CantSendPushQuery = false

	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx0, tx1} }
	if err := te.Notify(common.PendingTxs); err != nil {
		t.Fatal(err)
	}

	if len(lastVtx.TxsV) != 1 || lastVtx.TxsV[0].ID() != tx1.ID() {
		t.Fatalf("Should have issued txs differently")
	}
}

func TestEngineGetVertex(t *testing.T) {
	commonCfg, _, engCfg := DefaultConfig()

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	engCfg.Sender = sender

	vdr := validators.GenerateRandomValidator(1)

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	engCfg.Manager = manager
	avaGetHandler, err := avagetter.New(manager, commonCfg)
	if err != nil {
		t.Fatal(err)
	}
	engCfg.AllGetsServer = avaGetHandler

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	sender.SendPutF = func(v ids.ShortID, _ uint32, vtxID ids.ID, vtx []byte) {
		if v != vdr.ID() {
			t.Fatalf("Wrong validator")
		}
		if mVtx.ID() != vtxID {
			t.Fatalf("Wrong vertex")
		}
	}

	if err := te.Get(vdr.ID(), 0, mVtx.ID()); err != nil {
		t.Fatal(err)
	}
}

func TestEngineInsufficientValidators(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	bootCfg.Sender = sender
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	bootCfg.Manager = manager
	engCfg.Manager = manager

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
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	queried := new(bool)
	sender.SendPushQueryF = func(inVdrs ids.ShortSet, _ uint32, vtxID ids.ID, vtx []byte) {
		*queried = true
	}

	if err := te.issue(vtx); err != nil {
		t.Fatal(err)
	}

	if *queried {
		t.Fatalf("Unknown query")
	}
}

func TestEnginePushGossip(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	bootCfg.Sender = sender
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	bootCfg.Manager = manager
	engCfg.Manager = manager

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
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		case vtx.ID():
			return vtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	requested := new(bool)
	sender.SendGetF = func(vdr ids.ShortID, _ uint32, vtxID ids.ID) {
		*requested = true
	}

	manager.ParseVtxF = func(b []byte) (avalanche.Vertex, error) {
		if bytes.Equal(b, vtx.BytesV) {
			return vtx, nil
		}
		t.Fatalf("Unknown vertex bytes")
		panic("Should have errored")
	}

	sender.CantSendPushQuery = false
	sender.CantSendChits = false
	if err := te.PushQuery(vdr, 0, vtx.Bytes()); err != nil {
		t.Fatal(err)
	}

	if *requested {
		t.Fatalf("Shouldn't have requested the vertex")
	}
}

func TestEngineSingleQuery(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	bootCfg.Sender = sender
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	bootCfg.Manager = manager
	engCfg.Manager = manager

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
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		case vtx.ID():
			return vtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	sender.CantSendPushQuery = false
	sender.CantSendPullQuery = false

	if err := te.issue(vtx); err != nil {
		t.Fatal(err)
	}
}

func TestEngineParentBlockingInsert(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	bootCfg.Sender = sender
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	bootCfg.Manager = manager
	engCfg.Manager = manager

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
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	if err := te.issue(parentVtx); err != nil {
		t.Fatal(err)
	}
	if err := te.issue(blockingVtx); err != nil {
		t.Fatal(err)
	}

	if len(te.vtxBlocked) != 2 {
		t.Fatalf("Both inserts should be blocking")
	}

	sender.CantSendPushQuery = false

	missingVtx.StatusV = choices.Processing
	if err := te.issue(missingVtx); err != nil {
		t.Fatal(err)
	}

	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Both inserts should not longer be blocking")
	}
}

func TestEngineBlockingChitRequest(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	bootCfg.Sender = sender
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	bootCfg.Manager = manager
	engCfg.Manager = manager

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
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	if err := te.issue(parentVtx); err != nil {
		t.Fatal(err)
	}

	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if vtxID == blockingVtx.ID() {
			return blockingVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}
	manager.ParseVtxF = func(b []byte) (avalanche.Vertex, error) {
		if bytes.Equal(b, blockingVtx.Bytes()) {
			return blockingVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	if err := te.PushQuery(vdr, 0, blockingVtx.Bytes()); err != nil {
		t.Fatal(err)
	}

	if len(te.vtxBlocked) != 3 {
		t.Fatalf("Both inserts and the query should be blocking")
	}

	sender.CantSendPushQuery = false
	sender.CantSendChits = false

	missingVtx.StatusV = choices.Processing
	if err := te.issue(missingVtx); err != nil {
		t.Fatal(err)
	}

	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Both inserts should not longer be blocking")
	}
}

func TestEngineBlockingChitResponse(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	bootCfg.Sender = sender
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	engCfg.Manager = manager
	bootCfg.Manager = manager

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
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	if err := te.issue(blockingVtx); err != nil {
		t.Fatal(err)
	}

	queryRequestID := new(uint32)
	sender.SendPushQueryF = func(inVdrs ids.ShortSet, requestID uint32, vtxID ids.ID, vtx []byte) {
		*queryRequestID = requestID
		vdrSet := ids.ShortSet{}
		vdrSet.Add(vdr)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if issuedVtx.ID() != vtxID {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	if err := te.issue(issuedVtx); err != nil {
		t.Fatal(err)
	}

	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
		if id == blockingVtx.ID() {
			return blockingVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	if err := te.Chits(vdr, *queryRequestID, []ids.ID{blockingVtx.ID()}); err != nil {
		t.Fatal(err)
	}

	if len(te.vtxBlocked) != 2 {
		t.Fatalf("The insert should be blocking, as well as the chit response")
	}

	sender.SendPushQueryF = nil
	sender.CantSendPushQuery = false
	sender.CantSendChits = false

	missingVtx.StatusV = choices.Processing
	if err := te.issue(missingVtx); err != nil {
		t.Fatal(err)
	}

	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Both inserts should not longer be blocking")
	}
}

func TestEngineMissingTx(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	bootCfg.Sender = sender
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	bootCfg.Manager = manager
	engCfg.Manager = manager

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
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	if err := te.issue(blockingVtx); err != nil {
		t.Fatal(err)
	}

	queryRequestID := new(uint32)
	sender.SendPushQueryF = func(inVdrs ids.ShortSet, requestID uint32, vtxID ids.ID, vtx []byte) {
		*queryRequestID = requestID
		vdrSet := ids.ShortSet{}
		vdrSet.Add(vdr)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if issuedVtx.ID() != vtxID {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	if err := te.issue(issuedVtx); err != nil {
		t.Fatal(err)
	}

	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
		if id == blockingVtx.ID() {
			return blockingVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	if err := te.Chits(vdr, *queryRequestID, []ids.ID{blockingVtx.ID()}); err != nil {
		t.Fatal(err)
	}

	if len(te.vtxBlocked) != 2 {
		t.Fatalf("The insert should be blocking, as well as the chit response")
	}

	sender.SendPushQueryF = nil
	sender.CantSendPushQuery = false
	sender.CantSendChits = false

	missingVtx.StatusV = choices.Processing
	if err := te.issue(missingVtx); err != nil {
		t.Fatal(err)
	}

	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Both inserts should not longer be blocking")
	}
}

func TestEngineIssueBlockingTx(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	bootCfg.Manager = manager
	engCfg.Manager = manager

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
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{tx0},
	}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[1])

	vtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0, tx1},
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	if err := te.issue(vtx); err != nil {
		t.Fatal(err)
	}

	if prefs := te.Consensus.Preferences(); !prefs.Contains(vtx.ID()) {
		t.Fatalf("Vertex should be preferred")
	}
}

func TestEngineReissueAbortedVertex(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	bootCfg.Sender = sender
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	bootCfg.Manager = manager
	engCfg.Manager = manager

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

	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if vtxID == gVtx.ID() {
			return gVtx, nil
		}
		t.Fatalf("Unknown vertex requested")
		panic("Unknown vertex requested")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	manager.EdgeF = nil
	manager.GetVtxF = nil

	requestID := new(uint32)
	sender.SendGetF = func(vID ids.ShortID, reqID uint32, vtxID ids.ID) {
		*requestID = reqID
	}
	manager.ParseVtxF = func(b []byte) (avalanche.Vertex, error) {
		if bytes.Equal(b, vtxBytes1) {
			return vtx1, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}
	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if vtxID == vtxID1 {
			return vtx1, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}

	if err := te.PushQuery(vdr, 0, vtx1.Bytes()); err != nil {
		t.Fatal(err)
	}

	sender.SendGetF = nil
	manager.ParseVtxF = nil
	sender.CantSendChits = false

	if err := te.GetFailed(vdr, *requestID); err != nil {
		t.Fatal(err)
	}

	requested := new(bool)
	sender.SendGetF = func(_ ids.ShortID, _ uint32, vtxID ids.ID) {
		if vtxID == vtxID0 {
			*requested = true
		}
	}
	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if vtxID == vtxID1 {
			return vtx1, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}

	if err := te.PullQuery(vdr, 0, vtxID1); err != nil {
		t.Fatal(err)
	}

	if !*requested {
		t.Fatalf("Should have requested the missing vertex")
	}
}

func TestEngineBootstrappingIntoConsensus(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Beacons = vals
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	bootCfg.SampleK = vals.Len()

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	bootCfg.Sender = sender
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	bootCfg.Manager = manager
	engCfg.Manager = manager

	vm := &vertex.TestVM{TestVM: common.TestVM{T: t}}
	vm.Default(true)
	bootCfg.VM = vm
	engCfg.VM = vm

	vm.CantSetState = false
	vm.CantConnected = false

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
	sender.SendGetAcceptedFrontierF = func(vdrs ids.ShortSet, reqID uint32) {
		if vdrs.Len() != 1 {
			t.Fatalf("Should have requested from the validators")
		}
		if !vdrs.Contains(vdr) {
			t.Fatalf("Should have requested from %s", vdr)
		}
		*requested = true
		*requestID = reqID
	}

	dh := &dummyHandler{}
	bootstrapper, err := bootstrap.New(
		bootCfg,
		dh.onDoneBootstrapping,
	)
	if err != nil {
		t.Fatal(err)
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}
	dh.startEngineF = te.Start

	startReqID := uint32(0)
	if err := bootstrapper.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	if err := bootstrapper.Connected(vdr, version.CurrentApp); err != nil {
		t.Fatal(err)
	}

	sender.SendGetAcceptedFrontierF = nil

	if !*requested {
		t.Fatalf("Should have requested from the validators during Initialize")
	}

	acceptedFrontier := []ids.ID{vtxID0}

	*requested = false
	sender.SendGetAcceptedF = func(vdrs ids.ShortSet, reqID uint32, proposedAccepted []ids.ID) {
		if vdrs.Len() != 1 {
			t.Fatalf("Should have requested from the validators")
		}
		if !vdrs.Contains(vdr) {
			t.Fatalf("Should have requested from %s", vdr)
		}
		if !ids.Equals(acceptedFrontier, proposedAccepted) {
			t.Fatalf("Wrong proposedAccepted vertices.\nExpected: %s\nGot: %s", acceptedFrontier, proposedAccepted)
		}
		*requested = true
		*requestID = reqID
	}

	if err := bootstrapper.AcceptedFrontier(vdr, *requestID, acceptedFrontier); err != nil {
		t.Fatal(err)
	}

	if !*requested {
		t.Fatalf("Should have requested from the validators during AcceptedFrontier")
	}

	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if vtxID == vtxID0 {
			return nil, errMissing
		}
		t.Fatalf("Unknown vertex requested")
		panic("Unknown vertex requested")
	}

	sender.SendGetAncestorsF = func(inVdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if vdr != inVdr {
			t.Fatalf("Asking wrong validator for vertex")
		}
		if vtx0.ID() != vtxID {
			t.Fatalf("Asking for wrong vertex")
		}
		*requestID = reqID
	}

	if err := bootstrapper.Accepted(vdr, *requestID, acceptedFrontier); err != nil {
		t.Fatal(err)
	}

	manager.GetVtxF = nil
	sender.SendGetF = nil

	vm.ParseTxF = func(b []byte) (snowstorm.Tx, error) {
		if bytes.Equal(b, txBytes0) {
			return tx0, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}
	manager.ParseVtxF = func(b []byte) (avalanche.Vertex, error) {
		if bytes.Equal(b, vtxBytes0) {
			return vtx0, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{vtxID0}
	}
	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if vtxID == vtxID0 {
			return vtx0, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}

	if err := bootstrapper.Ancestors(vdr, *requestID, [][]byte{vtxBytes0}); err != nil {
		t.Fatal(err)
	}

	vm.ParseTxF = nil
	manager.ParseVtxF = nil
	manager.EdgeF = nil
	manager.GetVtxF = nil

	if tx0.Status() != choices.Accepted {
		t.Fatalf("Should have accepted %s", txID0)
	}
	if vtx0.Status() != choices.Accepted {
		t.Fatalf("Should have accepted %s", vtxID0)
	}

	manager.ParseVtxF = func(b []byte) (avalanche.Vertex, error) {
		if bytes.Equal(b, vtxBytes1) {
			return vtx1, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}
	sender.SendChitsF = func(inVdr ids.ShortID, _ uint32, chits []ids.ID) {
		if inVdr != vdr {
			t.Fatalf("Sent to the wrong validator")
		}

		expected := []ids.ID{vtxID1}

		if !ids.Equals(expected, chits) {
			t.Fatalf("Returned wrong chits")
		}
	}
	sender.SendPushQueryF = func(vdrs ids.ShortSet, _ uint32, vtxID ids.ID, vtx []byte) {
		if vdrs.Len() != 1 {
			t.Fatalf("Should have requested from the validators")
		}
		if !vdrs.Contains(vdr) {
			t.Fatalf("Should have requested from %s", vdr)
		}

		if vtxID1 != vtxID {
			t.Fatalf("Sent wrong query ID")
		}
		if !bytes.Equal(vtxBytes1, vtx) {
			t.Fatalf("Sent wrong query bytes")
		}
	}
	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if vtxID == vtxID1 {
			return vtx1, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}

	if err := te.PushQuery(vdr, 0, vtxBytes1); err != nil {
		t.Fatal(err)
	}

	manager.ParseVtxF = nil
	sender.SendChitsF = nil
	sender.SendPushQueryF = nil
	manager.GetVtxF = nil
}

func TestEngineReBootstrapFails(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()
	bootCfg.Alpha = 1
	bootCfg.RetryBootstrap = true
	bootCfg.RetryBootstrapWarnFrequency = 4

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Beacons = vals
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	bootCfg.SampleK = vals.Len()

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	bootCfg.Sender = sender
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	bootCfg.Manager = manager
	engCfg.Manager = manager

	vm := &vertex.TestVM{TestVM: common.TestVM{T: t}}
	vm.Default(true)
	bootCfg.VM = vm
	engCfg.VM = vm

	vm.CantSetState = false

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

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     txID1,
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{tx0},
		BytesV:        txBytes1,
	}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[1])

	requested := new(bool)
	requestID := new(uint32)
	sender.SendGetAcceptedFrontierF = func(vdrs ids.ShortSet, reqID uint32) {
		// instead of triggering the timeout here, we'll just invoke the GetAcceptedFrontierFailed func
		//
		// s.router.GetAcceptedFrontierFailed(vID, s.ctx.ChainID, requestID)
		// -> chain.GetAcceptedFrontierFailed(validatorID, requestID)
		// ---> h.sendReliableMsg(message{
		//			messageType: constants.GetAcceptedFrontierFailedMsg,
		//			validatorID: validatorID,
		//			requestID:   requestID,
		//		})
		// -----> h.engine.GetAcceptedFrontierFailed(msg.validatorID, msg.requestID)
		// -------> return b.AcceptedFrontier(validatorID, requestID, nil)

		// ensure the request is made to the correct validators
		if vdrs.Len() != 1 {
			t.Fatalf("Should have requested from the validators")
		}
		if !vdrs.Contains(vdr) {
			t.Fatalf("Should have requested from %s", vdr)
		}
		*requested = true
		*requestID = reqID
	}

	dh := &dummyHandler{}
	bootstrapper, err := bootstrap.New(
		bootCfg,
		dh.onDoneBootstrapping,
	)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := bootstrapper.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	if !*requested {
		t.Fatalf("Should have requested from the validators during Initialize")
	}

	// reset requested
	*requested = false
	sender.SendGetAcceptedF = func(vdrs ids.ShortSet, reqID uint32, proposedAccepted []ids.ID) {
		if vdrs.Len() != 1 {
			t.Fatalf("Should have requested from the validators")
		}
		if !vdrs.Contains(vdr) {
			t.Fatalf("Should have requested from %s", vdr)
		}
		*requested = true
		*requestID = reqID
	}

	// mimic a GetAcceptedFrontierFailedMsg
	// only validator that was requested timed out on the request
	if err := bootstrapper.GetAcceptedFrontierFailed(vdr, *requestID); err != nil {
		t.Fatal(err)
	}

	// mimic a GetAcceptedFrontierFailedMsg
	// only validator that was requested timed out on the request
	if err := bootstrapper.GetAcceptedFrontierFailed(vdr, *requestID); err != nil {
		t.Fatal(err)
	}

	bootCfg.Ctx.Registerer = prometheus.NewRegistry()

	// re-register the Transitive
	bootstrapper2, err := bootstrap.New(
		bootCfg,
		dh.onDoneBootstrapping,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := bootstrapper2.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	if err := bootstrapper2.GetAcceptedFailed(vdr, *requestID); err != nil {
		t.Fatal(err)
	}

	if err := bootstrapper2.GetAcceptedFailed(vdr, *requestID); err != nil {
		t.Fatal(err)
	}

	if !*requested {
		t.Fatalf("Should have requested from the validators during AcceptedFrontier")
	}
}

func TestEngineReBootstrappingIntoConsensus(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()
	bootCfg.Alpha = 1
	bootCfg.RetryBootstrap = true
	bootCfg.RetryBootstrapWarnFrequency = 4

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Beacons = vals
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	bootCfg.SampleK = vals.Len()

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	bootCfg.Sender = sender
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	bootCfg.Manager = manager
	engCfg.Manager = manager

	vm := &vertex.TestVM{TestVM: common.TestVM{T: t}}
	vm.Default(true)
	bootCfg.VM = vm
	engCfg.VM = vm

	vm.CantSetState = false
	vm.CantConnected = false

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
	sender.SendGetAcceptedFrontierF = func(vdrs ids.ShortSet, reqID uint32) {
		if vdrs.Len() != 1 {
			t.Fatalf("Should have requested from the validators")
		}
		if !vdrs.Contains(vdr) {
			t.Fatalf("Should have requested from %s", vdr)
		}
		*requested = true
		*requestID = reqID
	}

	dh := &dummyHandler{}
	bootstrapper, err := bootstrap.New(
		bootCfg,
		dh.onDoneBootstrapping,
	)
	if err != nil {
		t.Fatal(err)
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}
	dh.startEngineF = te.Start

	startReqID := uint32(0)
	if err := bootstrapper.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	if err := bootstrapper.Connected(vdr, version.CurrentApp); err != nil {
		t.Fatal(err)
	}

	// fail the AcceptedFrontier
	if err := bootstrapper.GetAcceptedFrontierFailed(vdr, *requestID); err != nil {
		t.Fatal(err)
	}

	// fail the GetAcceptedFailed
	if err := bootstrapper.GetAcceptedFailed(vdr, *requestID); err != nil {
		t.Fatal(err)
	}

	if !*requested {
		t.Fatalf("Should have requested from the validators during Initialize")
	}

	acceptedFrontier := []ids.ID{vtxID0}

	*requested = false
	sender.SendGetAcceptedF = func(vdrs ids.ShortSet, reqID uint32, proposedAccepted []ids.ID) {
		if vdrs.Len() != 1 {
			t.Fatalf("Should have requested from the validators")
		}
		if !vdrs.Contains(vdr) {
			t.Fatalf("Should have requested from %s", vdr)
		}
		if !ids.Equals(acceptedFrontier, proposedAccepted) {
			t.Fatalf("Wrong proposedAccepted vertices.\nExpected: %s\nGot: %s", acceptedFrontier, proposedAccepted)
		}
		*requested = true
		*requestID = reqID
	}

	if err := bootstrapper.AcceptedFrontier(vdr, *requestID, acceptedFrontier); err != nil {
		t.Fatal(err)
	}

	if !*requested {
		t.Fatalf("Should have requested from the validators during AcceptedFrontier")
	}

	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if vtxID == vtxID0 {
			return nil, errMissing
		}
		t.Fatalf("Unknown vertex requested")
		panic("Unknown vertex requested")
	}

	sender.SendGetAncestorsF = func(inVdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if vdr != inVdr {
			t.Fatalf("Asking wrong validator for vertex")
		}
		if vtx0.ID() != vtxID {
			t.Fatalf("Asking for wrong vertex")
		}
		*requestID = reqID
	}

	if err := bootstrapper.Accepted(vdr, *requestID, acceptedFrontier); err != nil {
		t.Fatal(err)
	}

	manager.GetVtxF = nil

	vm.ParseTxF = func(b []byte) (snowstorm.Tx, error) {
		if bytes.Equal(b, txBytes0) {
			return tx0, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}
	manager.ParseVtxF = func(b []byte) (avalanche.Vertex, error) {
		if bytes.Equal(b, vtxBytes0) {
			return vtx0, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{vtxID0}
	}
	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if vtxID == vtxID0 {
			return vtx0, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}

	if err := bootstrapper.Ancestors(vdr, *requestID, [][]byte{vtxBytes0}); err != nil {
		t.Fatal(err)
	}

	sender.SendGetAcceptedFrontierF = nil
	sender.SendGetF = nil
	vm.ParseTxF = nil
	manager.ParseVtxF = nil
	manager.EdgeF = nil
	manager.GetVtxF = nil

	if tx0.Status() != choices.Accepted {
		t.Fatalf("Should have accepted %s", txID0)
	}
	if vtx0.Status() != choices.Accepted {
		t.Fatalf("Should have accepted %s", vtxID0)
	}

	manager.ParseVtxF = func(b []byte) (avalanche.Vertex, error) {
		if bytes.Equal(b, vtxBytes1) {
			return vtx1, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}
	sender.SendChitsF = func(inVdr ids.ShortID, _ uint32, chits []ids.ID) {
		if inVdr != vdr {
			t.Fatalf("Sent to the wrong validator")
		}

		expected := []ids.ID{vtxID1}

		if !ids.Equals(expected, chits) {
			t.Fatalf("Returned wrong chits")
		}
	}
	sender.SendPushQueryF = func(vdrs ids.ShortSet, _ uint32, vtxID ids.ID, vtx []byte) {
		if vdrs.Len() != 1 {
			t.Fatalf("Should have requested from the validators")
		}
		if !vdrs.Contains(vdr) {
			t.Fatalf("Should have requested from %s", vdr)
		}

		if vtxID1 != vtxID {
			t.Fatalf("Sent wrong query ID")
		}
		if !bytes.Equal(vtxBytes1, vtx) {
			t.Fatalf("Sent wrong query bytes")
		}
	}
	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if vtxID == vtxID1 {
			return vtx1, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}

	if err := bootstrapper.PushQuery(vdr, 0, vtxBytes1); err != nil {
		t.Fatal(err)
	}

	manager.ParseVtxF = nil
	sender.SendChitsF = nil
	sender.SendPushQueryF = nil
	manager.GetVtxF = nil
}

func TestEngineUndeclaredDependencyDeadlock(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	bootCfg.Manager = manager
	engCfg.Manager = manager

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
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		VerifyV: errors.New(""),
	}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[1])

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

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	te.Sender = sender

	reqID := new(uint32)
	sender.SendPushQueryF = func(_ ids.ShortSet, requestID uint32, _ ids.ID, _ []byte) {
		*reqID = requestID
	}

	if err := te.issue(vtx0); err != nil {
		t.Fatal(err)
	}

	sender.SendPushQueryF = func(ids.ShortSet, uint32, ids.ID, []byte) {
		t.Fatalf("should have failed verification")
	}

	if err := te.issue(vtx1); err != nil {
		t.Fatal(err)
	}

	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch vtxID {
		case vtx0.ID():
			return vtx0, nil
		case vtx1.ID():
			return vtx1, nil
		}
		return nil, errors.New("Unknown vtx")
	}

	if err := te.Chits(vdr, *reqID, []ids.ID{vtx1.ID()}); err != nil {
		t.Fatal(err)
	}

	if status := vtx0.Status(); status != choices.Accepted {
		t.Fatalf("should have accepted the vertex due to transitive voting")
	}
}

func TestEnginePartiallyValidVertex(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	bootCfg.Manager = manager
	engCfg.Manager = manager

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
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		VerifyV: errors.New(""),
	}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[1])

	vtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0, tx1},
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	expectedVtxID := ids.GenerateTestID()
	manager.BuildVtxF = func(_ []ids.ID, txs []snowstorm.Tx) (avalanche.Vertex, error) {
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

	sender := &common.SenderTest{T: t}
	te.Sender = sender

	sender.SendPushQueryF = func(_ ids.ShortSet, _ uint32, vtxID ids.ID, _ []byte) {
		if expectedVtxID != vtxID {
			t.Fatalf("wrong vertex queried")
		}
	}

	if err := te.issue(vtx); err != nil {
		t.Fatal(err)
	}
}

func TestEngineGossip(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	bootCfg.Sender = sender
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	bootCfg.Manager = manager
	engCfg.Manager = manager

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID()} }
	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if vtxID == gVtx.ID() {
			return gVtx, nil
		}
		t.Fatal(errUnknownVertex)
		return nil, errUnknownVertex
	}

	called := new(bool)
	sender.SendGossipF = func(vtxID ids.ID, vtxBytes []byte) {
		*called = true
		if vtxID != gVtx.ID() {
			t.Fatal(errUnknownVertex)
		}
		if !bytes.Equal(vtxBytes, gVtx.Bytes()) {
			t.Fatal(errUnknownVertex)
		}
	}

	if err := te.Gossip(); err != nil {
		t.Fatal(err)
	}

	if !*called {
		t.Fatalf("Should have gossiped the vertex")
	}
}

func TestEngineInvalidVertexIgnoredFromUnexpectedPeer(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	secondVdr := ids.GenerateTestShortID()

	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}
	if err := vals.AddWeight(secondVdr, 1); err != nil {
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	bootCfg.Sender = sender
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	bootCfg.Manager = manager
	engCfg.Manager = manager

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
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	tx1 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[1])

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

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	parsed := new(bool)
	manager.ParseVtxF = func(b []byte) (avalanche.Vertex, error) {
		if bytes.Equal(b, vtx1.Bytes()) {
			*parsed = true
			return vtx1, nil
		}
		return nil, errUnknownVertex
	}

	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if !*parsed {
			return nil, errUnknownVertex
		}

		if vtxID == vtx1.ID() {
			return vtx1, nil
		}
		return nil, errUnknownVertex
	}

	reqID := new(uint32)
	sender.SendGetF = func(reqVdr ids.ShortID, requestID uint32, vtxID ids.ID) {
		*reqID = requestID
		if reqVdr != vdr {
			t.Fatalf("Wrong validator requested")
		}
		if vtxID != vtx0.ID() {
			t.Fatalf("Wrong vertex requested")
		}
	}

	if err := te.PushQuery(vdr, 0, vtx1.Bytes()); err != nil {
		t.Fatal(err)
	}

	if err := te.Put(secondVdr, *reqID, []byte{3}); err != nil {
		t.Fatal(err)
	}

	*parsed = false
	manager.ParseVtxF = func(b []byte) (avalanche.Vertex, error) {
		if bytes.Equal(b, vtx0.Bytes()) {
			*parsed = true
			return vtx0, nil
		}
		return nil, errUnknownVertex
	}

	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if !*parsed {
			return nil, errUnknownVertex
		}

		if vtxID == vtx0.ID() {
			return vtx0, nil
		}
		return nil, errUnknownVertex
	}
	sender.CantSendPushQuery = false
	sender.CantSendChits = false

	vtx0.StatusV = choices.Processing

	if err := te.Put(vdr, *reqID, vtx0.Bytes()); err != nil {
		t.Fatal(err)
	}

	prefs := te.Consensus.Preferences()
	if !prefs.Contains(vtx1.ID()) {
		t.Fatalf("Shouldn't have abandoned the pending vertex")
	}
}

func TestEnginePushQueryRequestIDConflict(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	bootCfg.Sender = sender
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	bootCfg.Manager = manager
	engCfg.Manager = manager

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
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	tx1 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[1])

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

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	parsed := new(bool)
	manager.ParseVtxF = func(b []byte) (avalanche.Vertex, error) {
		if bytes.Equal(b, vtx1.Bytes()) {
			*parsed = true
			return vtx1, nil
		}
		return nil, errUnknownVertex
	}

	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if !*parsed {
			return nil, errUnknownVertex
		}

		if vtxID == vtx1.ID() {
			return vtx1, nil
		}
		return nil, errUnknownVertex
	}

	reqID := new(uint32)
	sender.SendGetF = func(reqVdr ids.ShortID, requestID uint32, vtxID ids.ID) {
		*reqID = requestID
		if reqVdr != vdr {
			t.Fatalf("Wrong validator requested")
		}
		if vtxID != vtx0.ID() {
			t.Fatalf("Wrong vertex requested")
		}
	}

	if err := te.PushQuery(vdr, 0, vtx1.Bytes()); err != nil {
		t.Fatal(err)
	}

	sender.SendGetF = nil
	sender.CantSendGet = false

	if err := te.PushQuery(vdr, *reqID, []byte{3}); err != nil {
		t.Fatal(err)
	}

	*parsed = false
	manager.ParseVtxF = func(b []byte) (avalanche.Vertex, error) {
		if bytes.Equal(b, vtx0.Bytes()) {
			*parsed = true
			return vtx0, nil
		}
		return nil, errUnknownVertex
	}

	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if !*parsed {
			return nil, errUnknownVertex
		}

		if vtxID == vtx0.ID() {
			return vtx0, nil
		}
		return nil, errUnknownVertex
	}
	sender.CantSendPushQuery = false
	sender.CantSendChits = false

	vtx0.StatusV = choices.Processing

	if err := te.Put(vdr, *reqID, vtx0.Bytes()); err != nil {
		t.Fatal(err)
	}

	prefs := te.Consensus.Preferences()
	if !prefs.Contains(vtx1.ID()) {
		t.Fatalf("Shouldn't have abandoned the pending vertex")
	}
}

func TestEngineAggressivePolling(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	engCfg.Params.ConcurrentRepolls = 3
	engCfg.Params.BetaRogue = 3

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	bootCfg.Sender = sender
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	engCfg.Manager = manager

	vm := &vertex.TestVM{TestVM: common.TestVM{T: t}}
	vm.Default(true)
	bootCfg.VM = vm
	engCfg.VM = vm

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
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	tx1 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[1])

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

	vm.CantSetState = false
	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	vm.CantSetState = true
	parsed := new(bool)
	manager.ParseVtxF = func(b []byte) (avalanche.Vertex, error) {
		if bytes.Equal(b, vtx.Bytes()) {
			*parsed = true
			return vtx, nil
		}
		return nil, errUnknownVertex
	}

	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if !*parsed {
			return nil, errUnknownVertex
		}

		if vtxID == vtx.ID() {
			return vtx, nil
		}
		return nil, errUnknownVertex
	}

	numPushQueries := new(int)
	sender.SendPushQueryF = func(ids.ShortSet, uint32, ids.ID, []byte) { *numPushQueries++ }

	numPullQueries := new(int)
	sender.SendPullQueryF = func(ids.ShortSet, uint32, ids.ID) { *numPullQueries++ }

	vm.CantPendingTxs = false

	if err := te.Put(vdr, 0, vtx.Bytes()); err != nil {
		t.Fatal(err)
	}

	if *numPushQueries != 1 {
		t.Fatalf("should have issued one push query")
	}
	if *numPullQueries != 2 {
		t.Fatalf("should have issued two pull queries")
	}
}

func TestEngineDuplicatedIssuance(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()
	engCfg.Params.BatchSize = 1
	engCfg.Params.BetaVirtuous = 5
	engCfg.Params.BetaRogue = 5

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	bootCfg.Sender = sender
	engCfg.Sender = sender

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	engCfg.Manager = manager

	manager.Default(true)

	vm := &vertex.TestVM{TestVM: common.TestVM{T: t}}
	vm.Default(true)
	bootCfg.VM = vm
	engCfg.VM = vm

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
	tx.InputIDsV = append(tx.InputIDsV, utxos[0])

	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	vm.CantSetState = false
	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	vm.CantSetState = true
	lastVtx := new(avalanche.TestVertex)
	manager.BuildVtxF = func(_ []ids.ID, txs []snowstorm.Tx) (avalanche.Vertex, error) {
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

	sender.CantSendPushQuery = false

	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx} }
	if err := te.Notify(common.PendingTxs); err != nil {
		t.Fatal(err)
	}

	if len(lastVtx.TxsV) != 1 || lastVtx.TxsV[0].ID() != tx.ID() {
		t.Fatalf("Should have issued txs differently")
	}

	manager.BuildVtxF = func([]ids.ID, []snowstorm.Tx) (avalanche.Vertex, error) {
		t.Fatalf("shouldn't have attempted to issue a duplicated tx")
		return nil, nil
	}

	if err := te.Notify(common.PendingTxs); err != nil {
		t.Fatal(err)
	}
}

func TestEngineDoubleChit(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	engCfg.Params.Alpha = 2
	engCfg.Params.K = 2

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr0 := ids.GenerateTestShortID()
	vdr1 := ids.GenerateTestShortID()

	if err := vals.AddWeight(vdr0, 1); err != nil {
		t.Fatal(err)
	}
	if err := vals.AddWeight(vdr1, 1); err != nil {
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	bootCfg.Sender = sender
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	bootCfg.Manager = manager
	engCfg.Manager = manager

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
	tx.InputIDsV = append(tx.InputIDsV, utxos[0])

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
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	reqID := new(uint32)
	sender.SendPushQueryF = func(inVdrs ids.ShortSet, requestID uint32, vtxID ids.ID, _ []byte) {
		*reqID = requestID
		if inVdrs.Len() != 2 {
			t.Fatalf("Wrong number of validators")
		}
		if vtxID != vtx.ID() {
			t.Fatalf("Wrong vertex requested")
		}
	}
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
		if id == vtx.ID() {
			return vtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	if err := te.issue(vtx); err != nil {
		t.Fatal(err)
	}

	votes := []ids.ID{vtx.ID()}

	if status := tx.Status(); status != choices.Processing {
		t.Fatalf("Wrong tx status: %s ; expected: %s", status, choices.Processing)
	}

	if err := te.Chits(vdr0, *reqID, votes); err != nil {
		t.Fatal(err)
	}

	if status := tx.Status(); status != choices.Processing {
		t.Fatalf("Wrong tx status: %s ; expected: %s", status, choices.Processing)
	}

	if err := te.Chits(vdr0, *reqID, votes); err != nil {
		t.Fatal(err)
	}

	if status := tx.Status(); status != choices.Processing {
		t.Fatalf("Wrong tx status: %s ; expected: %s", status, choices.Processing)
	}

	if err := te.Chits(vdr1, *reqID, votes); err != nil {
		t.Fatal(err)
	}

	if status := tx.Status(); status != choices.Accepted {
		t.Fatalf("Wrong tx status: %s ; expected: %s", status, choices.Accepted)
	}
}

func TestEngineBubbleVotes(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	err := vals.AddWeight(vdr, 1)
	assert.NoError(t, err)

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	bootCfg.Sender = sender
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	bootCfg.Manager = manager
	engCfg.Manager = manager

	utxos := []ids.ID{
		ids.GenerateTestID(),
		ids.GenerateTestID(),
		ids.GenerateTestID(),
	}

	tx0 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: utxos[:1],
	}
	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: utxos[1:2],
	}
	tx2 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: utxos[1:2],
	}

	vtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		HeightV: 0,
		TxsV:    []snowstorm.Tx{tx0},
		BytesV:  []byte{0},
	}

	missingVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Unknown,
	}}

	pendingVtx0 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []avalanche.Vertex{vtx, missingVtx},
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1},
		BytesV:   []byte{1},
	}

	pendingVtx1 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []avalanche.Vertex{pendingVtx0},
		HeightV:  2,
		TxsV:     []snowstorm.Tx{tx2},
		BytesV:   []byte{2},
	}

	manager.EdgeF = func() []ids.ID { return nil }
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
		switch id {
		case vtx.ID():
			return vtx, nil
		case missingVtx.ID():
			return nil, errMissing
		case pendingVtx0.ID():
			return pendingVtx0, nil
		case pendingVtx1.ID():
			return pendingVtx1, nil
		}
		assert.FailNow(t, "unknown vertex", "vtxID: %s", id)
		panic("should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	queryReqID := new(uint32)
	queried := new(bool)
	sender.SendPushQueryF = func(inVdrs ids.ShortSet, requestID uint32, vtxID ids.ID, _ []byte) {
		assert.Len(t, inVdrs, 1, "wrong number of validators")
		*queryReqID = requestID
		assert.Equal(t, vtx.ID(), vtxID, "wrong vertex requested")
		*queried = true
	}

	getReqID := new(uint32)
	fetched := new(bool)
	sender.SendGetF = func(inVdr ids.ShortID, requestID uint32, vtxID ids.ID) {
		assert.Equal(t, vdr, inVdr, "wrong validator")
		*getReqID = requestID
		assert.Equal(t, missingVtx.ID(), vtxID, "wrong vertex requested")
		*fetched = true
	}

	issued, err := te.issueFrom(vdr, pendingVtx1)
	assert.NoError(t, err)
	assert.False(t, issued, "shouldn't have been able to issue %s", pendingVtx1.ID())
	assert.True(t, *queried, "should have queried for %s", vtx.ID())
	assert.True(t, *fetched, "should have fetched %s", missingVtx.ID())

	// can't apply votes yet because pendingVtx0 isn't issued because missingVtx
	// is missing
	err = te.Chits(vdr, *queryReqID, []ids.ID{pendingVtx1.ID()})
	assert.NoError(t, err)
	assert.Equal(t, choices.Processing, tx0.Status(), "wrong tx status")
	assert.Equal(t, choices.Processing, tx1.Status(), "wrong tx status")

	// vote for pendingVtx1 should be bubbled up to pendingVtx0 and then to vtx
	err = te.GetFailed(vdr, *getReqID)
	assert.NoError(t, err)
	assert.Equal(t, choices.Accepted, tx0.Status(), "wrong tx status")
	assert.Equal(t, choices.Processing, tx1.Status(), "wrong tx status")
}

func TestEngineIssue(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()
	engCfg.Params.BatchSize = 1
	engCfg.Params.BetaVirtuous = 1
	engCfg.Params.BetaRogue = 1
	engCfg.Params.OptimalProcessing = 1

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	bootCfg.Sender = sender
	engCfg.Sender = sender

	vals := validators.NewSet()
	wt := tracker.NewWeightTracker(vals, bootCfg.StartupAlpha)
	bootCfg.Validators = vals
	bootCfg.WeightTracker = wt
	engCfg.Validators = vals

	vdr := ids.GenerateTestShortID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	bootCfg.Manager = manager
	engCfg.Manager = manager

	vm := &vertex.TestVM{TestVM: common.TestVM{T: t}}
	vm.Default(true)
	bootCfg.VM = vm
	engCfg.VM = vm

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
		InputIDsV:     utxos[:1],
	}
	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
		InputIDsV:     utxos[1:],
	}

	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	vm.CantSetState = false
	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	vm.CantSetState = true
	numBuilt := 0
	manager.BuildVtxF = func(_ []ids.ID, txs []snowstorm.Tx) (avalanche.Vertex, error) {
		numBuilt++
		vtx := &avalanche.TestVertex{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Processing,
			},
			ParentsV: []avalanche.Vertex{gVtx, mVtx},
			HeightV:  1,
			TxsV:     txs,
			BytesV:   []byte{1},
		}

		manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
			switch id {
			case gVtx.ID():
				return gVtx, nil
			case mVtx.ID():
				return mVtx, nil
			case vtx.ID():
				return vtx, nil
			}
			t.Fatalf("Unknown vertex")
			panic("Should have errored")
		}

		return vtx, nil
	}

	var (
		vtxID          ids.ID
		queryRequestID uint32
	)
	sender.SendPushQueryF = func(inVdrs ids.ShortSet, requestID uint32, vID ids.ID, vtx []byte) {
		vtxID = vID
		queryRequestID = requestID
	}

	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx0, tx1} }
	if err := te.Notify(common.PendingTxs); err != nil {
		t.Fatal(err)
	}

	if numBuilt != 1 {
		t.Fatalf("Should have issued txs differently")
	}

	if err := te.Chits(vdr, queryRequestID, []ids.ID{vtxID}); err != nil {
		t.Fatal(err)
	}

	if numBuilt != 2 {
		t.Fatalf("Should have issued txs differently")
	}
}

// Test that a transaction is abandoned if a dependency fails verification,
// even if there are outstanding requests for vertices when the
// dependency fails verification.
func TestAbandonTx(t *testing.T) {
	assert := assert.New(t)
	_, bootCfg, engCfg := DefaultConfig()
	engCfg.Params.BatchSize = 1
	engCfg.Params.BetaVirtuous = 1
	engCfg.Params.BetaRogue = 1
	engCfg.Params.OptimalProcessing = 1

	sender := &common.SenderTest{
		T:                           t,
		CantSendGetAcceptedFrontier: false,
	}
	sender.Default(true)
	bootCfg.Sender = sender
	engCfg.Sender = sender

	engCfg.Validators = validators.NewSet()
	vdr := ids.GenerateTestShortID()
	if err := engCfg.Validators.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	manager.CantEdge = false
	manager.CantGetVtx = false
	bootCfg.Manager = manager
	engCfg.Manager = manager

	vm := &vertex.TestVM{TestVM: common.TestVM{T: t}}
	vm.Default(true)
	vm.CantSetState = false

	bootCfg.VM = vm
	engCfg.VM = vm

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	startReqID := uint32(0)
	if err := te.Start(startReqID); err != nil {
		t.Fatal(err)
	}

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	gTx := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	tx0 := &snowstorm.TestTx{ // Fails verification
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
		InputIDsV:     []ids.ID{gTx.ID()},
		BytesV:        utils.RandomBytes(32),
		VerifyV:       errors.New(""),
	}

	tx1 := &snowstorm.TestTx{ // Depends on tx0
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{tx0},
		InputIDsV:     []ids.ID{gTx.ID()},
		BytesV:        utils.RandomBytes(32),
	}

	vtx0 := &avalanche.TestVertex{ // Contains tx0, which will fail verification
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentsV: []avalanche.Vertex{gVtx},
		HeightV:  gVtx.HeightV + 1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	// Contains tx1, which depends on tx0.
	// vtx0 and vtx1 are siblings.
	vtx1 := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentsV: []avalanche.Vertex{gVtx},
		HeightV:  gVtx.HeightV + 1,
		TxsV:     []snowstorm.Tx{tx1},
	}

	// Cause the engine to send a Get request for vtx1, vtx0, and some other vtx that doesn't exist
	sender.CantSendGet = false
	err = te.PullQuery(vdr, 0, vtx1.ID())
	assert.NoError(err)
	err = te.PullQuery(vdr, 0, vtx0.ID())
	assert.NoError(err)
	err = te.PullQuery(vdr, 0, ids.GenerateTestID())
	assert.NoError(err)

	// Give the engine vtx1. It should wait to issue vtx1
	// until tx0 is issued, because tx1 depends on tx0.
	manager.ParseVtxF = func(b []byte) (avalanche.Vertex, error) {
		if bytes.Equal(b, vtx1.BytesV) {
			vtx1.StatusV = choices.Processing
			return vtx1, nil
		}
		assert.FailNow("should have asked to parse vtx1")
		return nil, errors.New("should have asked to parse vtx1")
	}
	err = te.Put(vdr, 0, vtx1.Bytes())
	assert.NoError(err)

	// Verify that vtx1 is waiting to be issued.
	assert.True(te.pending.Contains(vtx1.ID()))

	// Give the engine vtx0. It should try to issue vtx0
	// but then abandon it because tx0 fails verification.
	manager.ParseVtxF = func(b []byte) (avalanche.Vertex, error) {
		if bytes.Equal(b, vtx0.BytesV) {
			vtx0.StatusV = choices.Processing
			return vtx0, nil
		}
		assert.FailNow("should have asked to parse vtx0")
		return nil, errors.New("should have asked to parse vtx0")
	}
	sender.CantSendChits = false // Engine will respond to the PullQuerys since the vertices were abandoned
	err = te.Put(vdr, 0, vtx0.Bytes())
	assert.NoError(err)

	// Despite the fact that there is still an outstanding vertex request,
	// vtx1 should have been abandoned because tx0 failed verification
	assert.False(te.pending.Contains(vtx1.ID()))
	// sanity check that there is indeed an outstanding vertex request
	assert.True(te.outstandingVtxReqs.Len() == 1)
}
