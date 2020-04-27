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
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/validators"
)

var (
	errFailedParsing = errors.New("failed parsing")
	errMissing       = errors.New("missing")
)

func TestEngineShutdown(t *testing.T) {
	config := DefaultConfig()
	vmShutdownCalled := false
	vm := &VMTest{}
	vm.ShutdownF = func() { vmShutdownCalled = true }
	config.VM = vm

	transitive := &Transitive{}

	transitive.Initialize(config)
	transitive.finishBootstrapping()
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

	st := &stateTest{t: t}
	config.State = st

	st.Default(true)

	st.cantEdge = false

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()

	if !te.Context().ChainID.Equals(ids.Empty) {
		t.Fatalf("Wrong chain ID")
	}

	vtx := &Vtx{
		parents: []avalanche.Vertex{
			&Vtx{
				id:     GenerateID(),
				status: choices.Unknown,
			},
		},
		id:     GenerateID(),
		status: choices.Processing,
		bytes:  []byte{1},
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
		if !vtx.parents[0].ID().Equals(vtxID) {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	st.parseVertex = func(b []byte) (avalanche.Vertex, error) {
		if !bytes.Equal(b, vtx.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return vtx, nil
	}

	te.Put(vdr.ID(), 0, vtx.ID(), vtx.Bytes())

	st.parseVertex = nil

	if !*asked {
		t.Fatalf("Didn't ask for a missing vertex")
	}

	if len(te.vtxBlocked) != 1 {
		t.Fatalf("Should have been blocking on request")
	}

	st.parseVertex = func(b []byte) (avalanche.Vertex, error) { return nil, errFailedParsing }

	te.Put(vdr.ID(), 0, vtx.parents[0].ID(), nil)

	st.parseVertex = nil

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

	st := &stateTest{t: t}
	config.State = st

	st.Default(true)

	gVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}
	mVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	vts := []avalanche.Vertex{gVtx, mVtx}
	utxos := []ids.ID{GenerateID()}

	tx0 := &TestTx{
		TestTx: snowstorm.TestTx{Identifier: GenerateID()},
	}
	tx0.Ins.Add(utxos[0])

	vtx0 := &Vtx{
		parents: vts,
		id:      GenerateID(),
		txs:     []snowstorm.Tx{tx0},
		height:  1,
		status:  choices.Processing,
		bytes:   []byte{0, 1, 2, 3},
	}

	st.edge = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	st.getVertex = func(id ids.ID) (avalanche.Vertex, error) {
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

	vertexed := new(bool)
	st.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
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
		if !Matches(prefs.List(), []ids.ID{vtx0.ID()}) {
			t.Fatalf("Wrong chits preferences")
		}
	}

	st.parseVertex = func(b []byte) (avalanche.Vertex, error) {
		if !bytes.Equal(b, vtx0.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return vtx0, nil
	}
	te.Put(vdr.ID(), 0, vtx0.ID(), vtx0.Bytes())
	st.parseVertex = nil

	if !*queried {
		t.Fatalf("Didn't ask for preferences")
	}
	if !*chitted {
		t.Fatalf("Didn't provide preferences")
	}

	vtx1 := &Vtx{
		parents: vts,
		id:      GenerateID(),
		txs:     []snowstorm.Tx{tx0},
		height:  1,
		status:  choices.Processing,
		bytes:   []byte{5, 4, 3, 2, 1, 9},
	}

	st.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		if vtxID.Equals(vtx0.ID()) {
			return &Vtx{status: choices.Processing}, nil
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

	st.parseVertex = func(b []byte) (avalanche.Vertex, error) {
		if !bytes.Equal(b, vtx1.Bytes()) {
			t.Fatalf("Wrong bytes")
		}

		st.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
			if vtxID.Equals(vtx0.ID()) {
				return &Vtx{status: choices.Processing}, nil
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
	st.parseVertex = nil

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

	st := &stateTest{t: t}
	config.State = st

	gVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}
	mVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	vts := []avalanche.Vertex{gVtx, mVtx}
	utxos := []ids.ID{GenerateID()}

	st.edge = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	st.getVertex = func(id ids.ID) (avalanche.Vertex, error) {
		switch {
		case id.Equals(gVtx.ID()):
			return gVtx, nil
		case id.Equals(mVtx.ID()):
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	tx0 := &TestTx{
		TestTx: snowstorm.TestTx{Identifier: GenerateID()},
	}
	tx0.Ins.Add(utxos[0])

	vtx0 := &Vtx{
		parents: vts,
		id:      GenerateID(),
		txs:     []snowstorm.Tx{tx0},
		height:  1,
		status:  choices.Processing,
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()

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

	vtx1 := &Vtx{
		parents: vts,
		id:      GenerateID(),
		txs:     []snowstorm.Tx{tx0},
		height:  1,
		status:  choices.Processing,
	}

	st.getVertex = func(id ids.ID) (avalanche.Vertex, error) {
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
	sender.GetF = func(inVdr ids.ShortID, _ uint32, vtxID ids.ID) {
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

	te.GetFailed(vdr0.ID(), 0, vtx1.ID())

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

	st := &stateTest{t: t}
	config.State = st

	gVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}
	mVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	vts := []avalanche.Vertex{gVtx, mVtx}
	utxos := []ids.ID{GenerateID()}

	tx0 := &TestTx{
		TestTx: snowstorm.TestTx{Identifier: GenerateID()},
	}
	tx0.Ins.Add(utxos[0])

	vtx0 := &Vtx{
		parents: vts,
		id:      GenerateID(),
		txs:     []snowstorm.Tx{tx0},
		height:  1,
		status:  choices.Processing,
	}

	vtx1 := &Vtx{
		parents: []avalanche.Vertex{&Vtx{
			id:     vtx0.ID(),
			status: choices.Unknown,
		}},
		id:     GenerateID(),
		txs:    []snowstorm.Tx{tx0},
		height: 1,
		status: choices.Processing,
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()

	te.insert(vtx1)

	vtx1.parents[0] = vtx0
	te.insert(vtx0)

	if !Matches(te.Consensus.Preferences().List(), []ids.ID{vtx1.ID()}) {
		t.Fatalf("Should have issued vtx1")
	}
}

func TestEngineAbandonResponse(t *testing.T) {
	config := DefaultConfig()

	vdr := validators.GenerateRandomValidator(1)

	vals := validators.NewSet()
	config.Validators = vals

	vals.Add(vdr)

	st := &stateTest{t: t}
	config.State = st

	gVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}
	mVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	vts := []avalanche.Vertex{gVtx, mVtx}
	utxos := []ids.ID{GenerateID()}

	tx0 := &TestTx{
		TestTx: snowstorm.TestTx{Identifier: GenerateID()},
	}
	tx0.Ins.Add(utxos[0])

	vtx := &Vtx{
		parents: vts,
		id:      GenerateID(),
		txs:     []snowstorm.Tx{tx0},
		height:  1,
		status:  choices.Processing,
	}

	st.getVertex = func(id ids.ID) (avalanche.Vertex, error) { return nil, errUnknownVertex }

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()

	te.PullQuery(vdr.ID(), 0, vtx.ID())
	te.GetFailed(vdr.ID(), 0, vtx.ID())

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

	gVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}
	mVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	vts := []avalanche.Vertex{gVtx, mVtx}
	utxos := []ids.ID{GenerateID()}

	tx0 := &TestTx{
		TestTx: snowstorm.TestTx{Identifier: GenerateID()},
	}
	tx0.Ins.Add(utxos[0])

	vtx := &Vtx{
		parents: vts,
		id:      GenerateID(),
		txs:     []snowstorm.Tx{tx0},
		height:  1,
		status:  choices.Processing,
	}

	st := &stateTest{t: t}
	config.State = st

	st.Default(true)
	st.cantEdge = false

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)
	sender.CantGetAcceptedFrontier = false

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()

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

	st := &stateTest{t: t}
	config.State = st

	st.Default(true)

	vm := &VMTest{}
	vm.T = t
	config.VM = vm

	vm.Default(true)

	gVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}
	mVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	gTx := &TestTx{
		TestTx: snowstorm.TestTx{
			Identifier: GenerateID(),
			Stat:       choices.Accepted,
		},
	}

	utxos := []ids.ID{GenerateID()}

	tx0 := &TestTx{
		TestTx: snowstorm.TestTx{
			Identifier: GenerateID(),
			Deps:       []snowstorm.Tx{gTx},
			Stat:       choices.Processing,
		},
	}
	tx0.Ins.Add(utxos[0])

	tx1 := &TestTx{
		TestTx: snowstorm.TestTx{
			Identifier: GenerateID(),
			Deps:       []snowstorm.Tx{gTx},
			Stat:       choices.Processing,
		},
	}
	tx1.Ins.Add(utxos[0])

	st.edge = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	st.getVertex = func(id ids.ID) (avalanche.Vertex, error) {
		switch {
		case id.Equals(gVtx.ID()):
			return gVtx, nil
		case id.Equals(mVtx.ID()):
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}
	st.buildVertex = func(_ ids.Set, txs []snowstorm.Tx) (avalanche.Vertex, error) {
		consumers := []snowstorm.Tx{}
		for _, tx := range txs {
			consumers = append(consumers, tx)
		}
		return &Vtx{
			parents: []avalanche.Vertex{gVtx, mVtx},
			id:      GenerateID(),
			txs:     consumers,
			status:  choices.Processing,
			bytes:   []byte{1},
		}, nil
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()

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

	st := &stateTest{t: t}
	config.State = st

	st.Default(true)

	vm := &VMTest{}
	vm.T = t
	config.VM = vm

	vm.Default(true)

	gVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}
	mVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	gTx := &TestTx{
		TestTx: snowstorm.TestTx{
			Identifier: GenerateID(),
			Stat:       choices.Accepted,
		},
	}

	utxos := []ids.ID{GenerateID()}

	tx0 := &TestTx{
		TestTx: snowstorm.TestTx{
			Identifier: GenerateID(),
			Deps:       []snowstorm.Tx{gTx},
			Stat:       choices.Processing,
		},
	}
	tx0.Ins.Add(utxos[0])

	tx1 := &TestTx{
		TestTx: snowstorm.TestTx{
			Identifier: GenerateID(),
			Deps:       []snowstorm.Tx{gTx},
			Stat:       choices.Processing,
		},
	}
	tx1.Ins.Add(utxos[0])

	st.edge = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	st.getVertex = func(id ids.ID) (avalanche.Vertex, error) {
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

	st.buildVertex = func(_ ids.Set, txs []snowstorm.Tx) (avalanche.Vertex, error) {
		consumers := []snowstorm.Tx{}
		for _, tx := range txs {
			consumers = append(consumers, tx)
		}
		return &Vtx{
			parents: []avalanche.Vertex{gVtx, mVtx},
			id:      GenerateID(),
			txs:     consumers,
			status:  choices.Processing,
			bytes:   []byte{1},
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

	st := &stateTest{t: t}
	config.State = st

	st.Default(true)

	gVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}
	mVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	st.edge = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	st.getVertex = func(id ids.ID) (avalanche.Vertex, error) {
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

	st := &stateTest{t: t}
	config.State = st

	st.Default(true)

	vm := &VMTest{}
	vm.T = t
	config.VM = vm

	vm.Default(true)

	gVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}
	mVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	gTx := &TestTx{
		TestTx: snowstorm.TestTx{
			Identifier: GenerateID(),
			Stat:       choices.Accepted,
		},
	}

	utxos := []ids.ID{GenerateID(), GenerateID()}

	tx0 := &TestTx{
		TestTx: snowstorm.TestTx{
			Identifier: GenerateID(),
			Deps:       []snowstorm.Tx{gTx},
			Stat:       choices.Processing,
		},
	}
	tx0.Ins.Add(utxos[0])

	tx1 := &TestTx{
		TestTx: snowstorm.TestTx{
			Identifier: GenerateID(),
			Deps:       []snowstorm.Tx{gTx},
			Stat:       choices.Processing,
		},
	}
	tx1.Ins.Add(utxos[1])

	tx2 := &TestTx{
		TestTx: snowstorm.TestTx{
			Identifier: GenerateID(),
			Deps:       []snowstorm.Tx{gTx},
			Stat:       choices.Processing,
		},
	}
	tx2.Ins.Add(utxos[1])

	tx3 := &TestTx{
		TestTx: snowstorm.TestTx{
			Identifier: GenerateID(),
			Deps:       []snowstorm.Tx{gTx},
			Stat:       choices.Processing,
		},
	}
	tx3.Ins.Add(utxos[0])

	vtx := &Vtx{
		parents: []avalanche.Vertex{gVtx, mVtx},
		txs:     []snowstorm.Tx{tx2},
		id:      GenerateID(),
		status:  choices.Processing,
		bytes:   []byte{42},
	}

	st.edge = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	st.getVertex = func(id ids.ID) (avalanche.Vertex, error) {
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

	lastVtx := new(Vtx)
	st.buildVertex = func(_ ids.Set, txs []snowstorm.Tx) (avalanche.Vertex, error) {
		consumers := []snowstorm.Tx{}
		for _, tx := range txs {
			consumers = append(consumers, tx)
		}
		lastVtx = &Vtx{
			parents: []avalanche.Vertex{gVtx, mVtx},
			id:      GenerateID(),
			txs:     consumers,
			status:  choices.Processing,
			bytes:   []byte{1},
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

	st.parseVertex = func(b []byte) (avalanche.Vertex, error) {
		if !bytes.Equal(b, vtx.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return vtx, nil
	}
	te.Put(vdr.ID(), 0, vtx.ID(), vtx.Bytes())
	st.parseVertex = nil

	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx3} }
	te.Notify(common.PendingTxs)

	s := ids.Set{}
	s.Add(vtx.ID())
	te.Chits(vdr.ID(), *queryRequestID, s)

	if len(lastVtx.txs) != 1 || !lastVtx.txs[0].ID().Equals(tx0.ID()) {
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

	st := &stateTest{t: t}
	config.State = st

	st.Default(true)

	vm := &VMTest{}
	vm.T = t
	config.VM = vm

	vm.Default(true)

	gVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}
	mVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	gTx := &TestTx{
		TestTx: snowstorm.TestTx{
			Identifier: GenerateID(),
			Stat:       choices.Accepted,
		},
	}

	utxos := []ids.ID{GenerateID(), GenerateID()}

	tx0 := &TestTx{
		TestTx: snowstorm.TestTx{
			Identifier: GenerateID(),
			Deps:       []snowstorm.Tx{gTx},
			Stat:       choices.Processing,
		},
	}
	tx0.Ins.Add(utxos[0])

	tx1 := &TestTx{
		TestTx: snowstorm.TestTx{
			Identifier: GenerateID(),
			Deps:       []snowstorm.Tx{gTx},
			Stat:       choices.Processing,
		},
	}
	tx1.Ins.Add(utxos[1])

	st.edge = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	st.getVertex = func(id ids.ID) (avalanche.Vertex, error) {
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

	lastVtx := new(Vtx)
	st.buildVertex = func(_ ids.Set, txs []snowstorm.Tx) (avalanche.Vertex, error) {
		consumers := []snowstorm.Tx{}
		for _, tx := range txs {
			consumers = append(consumers, tx)
		}
		lastVtx = &Vtx{
			parents: []avalanche.Vertex{gVtx, mVtx},
			id:      GenerateID(),
			txs:     consumers,
			status:  choices.Processing,
			bytes:   []byte{1},
		}
		return lastVtx, nil
	}

	sender.CantPushQuery = false

	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx0, tx1} }
	te.Notify(common.PendingTxs)

	if len(lastVtx.txs) != 1 || !lastVtx.txs[0].ID().Equals(tx1.ID()) {
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

	st := &stateTest{t: t}
	config.State = st

	st.Default(true)

	gVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}
	mVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	st.edge = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	st.getVertex = func(id ids.ID) (avalanche.Vertex, error) {
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

	st := &stateTest{t: t}
	config.State = st

	st.Default(true)

	gVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}
	mVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	vts := []avalanche.Vertex{gVtx, mVtx}

	vtx := &Vtx{
		parents: vts,
		id:      GenerateID(),
		height:  1,
		status:  choices.Processing,
		bytes:   []byte{0, 1, 2, 3},
	}

	st.edge = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	st.getVertex = func(id ids.ID) (avalanche.Vertex, error) {
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

	st := &stateTest{t: t}
	config.State = st

	st.Default(true)

	gVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}
	mVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	vts := []avalanche.Vertex{gVtx, mVtx}

	vtx := &Vtx{
		parents: vts,
		id:      GenerateID(),
		height:  1,
		status:  choices.Processing,
		bytes:   []byte{0, 1, 2, 3},
	}

	st.edge = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	st.getVertex = func(id ids.ID) (avalanche.Vertex, error) {
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

	requested := new(bool)
	sender.GetF = func(vdr ids.ShortID, _ uint32, vtxID ids.ID) {
		*requested = true
	}

	st.parseVertex = func(b []byte) (avalanche.Vertex, error) {
		if bytes.Equal(b, vtx.bytes) {
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

	st := &stateTest{t: t}
	config.State = st

	st.Default(true)

	gVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}
	mVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	vts := []avalanche.Vertex{gVtx, mVtx}

	vtx := &Vtx{
		parents: vts,
		id:      GenerateID(),
		height:  1,
		status:  choices.Processing,
		bytes:   []byte{0, 1, 2, 3},
	}

	st.edge = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	st.getVertex = func(id ids.ID) (avalanche.Vertex, error) {
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

	st := &stateTest{t: t}
	config.State = st

	st.Default(true)

	gVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}
	mVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	vts := []avalanche.Vertex{gVtx, mVtx}

	missingVtx := &Vtx{
		parents: vts,
		id:      GenerateID(),
		height:  1,
		status:  choices.Unknown,
		bytes:   []byte{0, 1, 2, 3},
	}

	parentVtx := &Vtx{
		parents: []avalanche.Vertex{missingVtx},
		id:      GenerateID(),
		height:  1,
		status:  choices.Processing,
		bytes:   []byte{0, 1, 2, 3},
	}

	blockingVtx := &Vtx{
		parents: []avalanche.Vertex{parentVtx},
		id:      GenerateID(),
		height:  1,
		status:  choices.Processing,
		bytes:   []byte{0, 1, 2, 3},
	}

	st.edge = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	st.getVertex = func(id ids.ID) (avalanche.Vertex, error) {
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

	te.insert(parentVtx)
	te.insert(blockingVtx)

	if len(te.vtxBlocked) != 2 {
		t.Fatalf("Both inserts should be blocking")
	}

	sender.CantPushQuery = false

	missingVtx.status = choices.Processing
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

	st := &stateTest{t: t}
	config.State = st

	st.Default(true)

	gVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}
	mVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	vts := []avalanche.Vertex{gVtx, mVtx}

	missingVtx := &Vtx{
		parents: vts,
		id:      GenerateID(),
		height:  1,
		status:  choices.Unknown,
		bytes:   []byte{0, 1, 2, 3},
	}

	parentVtx := &Vtx{
		parents: []avalanche.Vertex{missingVtx},
		id:      GenerateID(),
		height:  1,
		status:  choices.Processing,
		bytes:   []byte{1, 1, 2, 3},
	}

	blockingVtx := &Vtx{
		parents: []avalanche.Vertex{parentVtx},
		id:      GenerateID(),
		height:  1,
		status:  choices.Processing,
		bytes:   []byte{2, 1, 2, 3},
	}

	st.edge = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	st.getVertex = func(id ids.ID) (avalanche.Vertex, error) {
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

	te.insert(parentVtx)

	st.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(blockingVtx.ID()):
			return blockingVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}
	st.parseVertex = func(b []byte) (avalanche.Vertex, error) {
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

	missingVtx.status = choices.Processing
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

	st := &stateTest{t: t}
	config.State = st

	st.Default(true)

	gVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}
	mVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	vts := []avalanche.Vertex{gVtx, mVtx}

	issuedVtx := &Vtx{
		parents: vts,
		id:      GenerateID(),
		height:  1,
		status:  choices.Processing,
		bytes:   []byte{0, 1, 2, 3},
	}

	missingVtx := &Vtx{
		parents: vts,
		id:      GenerateID(),
		height:  1,
		status:  choices.Unknown,
		bytes:   []byte{0, 1, 2, 3},
	}

	blockingVtx := &Vtx{
		parents: []avalanche.Vertex{missingVtx},
		id:      GenerateID(),
		height:  1,
		status:  choices.Processing,
		bytes:   []byte{2, 1, 2, 3},
	}

	st.edge = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	st.getVertex = func(id ids.ID) (avalanche.Vertex, error) {
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

	st.getVertex = func(id ids.ID) (avalanche.Vertex, error) {
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

	missingVtx.status = choices.Processing
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

	st := &stateTest{t: t}
	config.State = st

	st.Default(true)

	gVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}
	mVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	vts := []avalanche.Vertex{gVtx, mVtx}

	issuedVtx := &Vtx{
		parents: vts,
		id:      GenerateID(),
		height:  1,
		status:  choices.Processing,
		bytes:   []byte{0, 1, 2, 3},
	}

	missingVtx := &Vtx{
		parents: vts,
		id:      GenerateID(),
		height:  1,
		status:  choices.Unknown,
		bytes:   []byte{0, 1, 2, 3},
	}

	blockingVtx := &Vtx{
		parents: []avalanche.Vertex{missingVtx},
		id:      GenerateID(),
		height:  1,
		status:  choices.Processing,
		bytes:   []byte{2, 1, 2, 3},
	}

	st.edge = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	st.getVertex = func(id ids.ID) (avalanche.Vertex, error) {
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

	st.getVertex = func(id ids.ID) (avalanche.Vertex, error) {
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

	missingVtx.status = choices.Processing
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

	st := &stateTest{t: t}
	config.State = st

	gVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	vts := []avalanche.Vertex{gVtx}
	utxos := []ids.ID{GenerateID(), GenerateID()}

	tx0 := &TestTx{
		TestTx: snowstorm.TestTx{
			Identifier: GenerateID(),
			Stat:       choices.Processing,
		},
	}
	tx0.Ins.Add(utxos[0])

	tx1 := &TestTx{
		TestTx: snowstorm.TestTx{
			Identifier: GenerateID(),
			Deps:       []snowstorm.Tx{tx0},
			Stat:       choices.Processing,
		},
	}
	tx1.Ins.Add(utxos[1])

	vtx := &Vtx{
		parents: vts,
		id:      GenerateID(),
		txs:     []snowstorm.Tx{tx0, tx1},
		height:  1,
		status:  choices.Processing,
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()

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

	st := &stateTest{t: t}
	config.State = st

	st.Default(true)

	gVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	vts := []avalanche.Vertex{gVtx}

	vtxID0 := GenerateID()
	vtxID1 := GenerateID()

	vtxBytes0 := []byte{0}
	vtxBytes1 := []byte{1}

	vtx0 := &Vtx{
		parents: vts,
		id:      vtxID0,
		height:  1,
		status:  choices.Unknown,
		bytes:   vtxBytes0,
	}
	vtx1 := &Vtx{
		parents: []avalanche.Vertex{vtx0},
		id:      vtxID1,
		height:  2,
		status:  choices.Processing,
		bytes:   vtxBytes1,
	}

	st.edge = func() []ids.ID {
		return []ids.ID{gVtx.ID()}
	}

	st.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
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

	st.edge = nil
	st.getVertex = nil

	requestID := new(uint32)
	sender.GetF = func(vID ids.ShortID, reqID uint32, vtxID ids.ID) {
		*requestID = reqID
	}
	st.parseVertex = func(b []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(b, vtxBytes1):
			return vtx1, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}
	st.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID1):
			return vtx1, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}

	te.PushQuery(vdrID, 0, vtxID1, vtx1.Bytes())

	sender.GetF = nil
	st.parseVertex = nil

	te.GetFailed(vdrID, *requestID, vtxID0)

	requested := new(bool)
	sender.GetF = func(_ ids.ShortID, _ uint32, vtxID ids.ID) {
		if vtxID.Equals(vtxID0) {
			*requested = true
		}
	}
	st.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
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

	st := &stateTest{t: t}
	config.State = st

	st.Default(true)

	vm := &VMTest{}
	vm.T = t
	config.VM = vm

	vm.Default(true)

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
		txs:    []snowstorm.Tx{tx0},
		height: 1,
		status: choices.Processing,
		bytes:  vtxBytes0,
	}
	vtx1 := &Vtx{
		parents: []avalanche.Vertex{vtx0},
		id:      vtxID1,
		txs:     []snowstorm.Tx{tx1},
		height:  2,
		status:  choices.Processing,
		bytes:   vtxBytes1,
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

	st.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID0):
			return nil, errMissing
		}
		t.Fatalf("Unknown vertex requested")
		panic("Unknown vertex requested")
	}

	sender.GetF = func(inVdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if !vdrID.Equals(inVdr) {
			t.Fatalf("Asking wrong validator for vertex")
		}
		if !vtx0.ID().Equals(vtxID) {
			t.Fatalf("Asking for wrong vertex")
		}
		*requestID = reqID
	}

	te.Accepted(vdrID, *requestID, acceptedFrontier)

	st.getVertex = nil
	sender.GetF = nil

	vm.ParseTxF = func(b []byte) (snowstorm.Tx, error) {
		switch {
		case bytes.Equal(b, txBytes0):
			return tx0, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}
	st.parseVertex = func(b []byte) (avalanche.Vertex, error) {
		switch {
		case bytes.Equal(b, vtxBytes0):
			return vtx0, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}
	st.edge = func() []ids.ID {
		return []ids.ID{vtxID0}
	}
	st.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID0):
			return vtx0, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}

	te.Put(vdrID, *requestID, vtxID0, vtxBytes0)

	vm.ParseTxF = nil
	st.parseVertex = nil
	st.edge = nil
	st.getVertex = nil

	if tx0.Status() != choices.Accepted {
		t.Fatalf("Should have accepted %s", txID0)
	}
	if vtx0.Status() != choices.Accepted {
		t.Fatalf("Should have accepted %s", vtxID0)
	}

	st.parseVertex = func(b []byte) (avalanche.Vertex, error) {
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
	st.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
		switch {
		case vtxID.Equals(vtxID1):
			return vtx1, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}

	te.PushQuery(vdrID, 0, vtxID1, vtxBytes1)

	st.parseVertex = nil
	sender.ChitsF = nil
	sender.PushQueryF = nil
	st.getVertex = nil
}

func TestEngineUndeclaredDependencyDeadlock(t *testing.T) {
	config := DefaultConfig()

	vdr := validators.GenerateRandomValidator(1)

	vals := validators.NewSet()
	config.Validators = vals

	vals.Add(vdr)

	st := &stateTest{t: t}
	config.State = st

	gVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	vts := []avalanche.Vertex{gVtx}
	utxos := []ids.ID{GenerateID(), GenerateID()}

	tx0 := &TestTx{
		TestTx: snowstorm.TestTx{
			Identifier: GenerateID(),
			Stat:       choices.Processing,
		},
	}
	tx0.Ins.Add(utxos[0])

	tx1 := &TestTx{
		TestTx: snowstorm.TestTx{
			Identifier: GenerateID(),
			Stat:       choices.Processing,
			Validity:   errors.New(""),
		},
	}
	tx1.Ins.Add(utxos[1])

	vtx0 := &Vtx{
		parents: vts,
		id:      GenerateID(),
		txs:     []snowstorm.Tx{tx0},
		height:  1,
		status:  choices.Processing,
	}

	vtx1 := &Vtx{
		parents: []avalanche.Vertex{vtx0},
		id:      GenerateID(),
		txs:     []snowstorm.Tx{tx1},
		height:  2,
		status:  choices.Processing,
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()

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

	st.getVertex = func(vtxID ids.ID) (avalanche.Vertex, error) {
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

	st := &stateTest{t: t}
	config.State = st

	gVtx := &Vtx{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	vts := []avalanche.Vertex{gVtx}
	utxos := []ids.ID{GenerateID(), GenerateID()}

	tx0 := &TestTx{
		TestTx: snowstorm.TestTx{
			Identifier: GenerateID(),
			Stat:       choices.Processing,
		},
	}
	tx0.Ins.Add(utxos[0])

	tx1 := &TestTx{
		TestTx: snowstorm.TestTx{
			Identifier: GenerateID(),
			Stat:       choices.Processing,
			Validity:   errors.New(""),
		},
	}
	tx1.Ins.Add(utxos[1])

	vtx := &Vtx{
		parents: vts,
		id:      GenerateID(),
		txs:     []snowstorm.Tx{tx0, tx1},
		height:  1,
		status:  choices.Processing,
	}

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()

	expectedVtxID := GenerateID()
	st.buildVertex = func(_ ids.Set, txs []snowstorm.Tx) (avalanche.Vertex, error) {
		consumers := []snowstorm.Tx{}
		for _, tx := range txs {
			consumers = append(consumers, tx)
		}
		return &Vtx{
			parents: vts,
			id:      expectedVtxID,
			txs:     consumers,
			status:  choices.Processing,
			bytes:   []byte{1},
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
