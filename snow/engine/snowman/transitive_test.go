// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"bytes"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowball"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/validators"
)

var (
	errUnknownBytes = errors.New("unknown bytes")
)

func setup(t *testing.T) (validators.Validator, validators.Set, *common.SenderTest, *VMTest, *Transitive, snowman.Block) {
	config := DefaultConfig()

	vdr := validators.GenerateRandomValidator(1)

	vals := validators.NewSet()
	config.Validators = vals

	vals.Add(vdr)

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)

	vm := &VMTest{}
	vm.T = t
	config.VM = vm

	vm.Default(true)
	vm.CantSetPreference = false

	gBlk := &Blk{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	vm.LastAcceptedF = func() ids.ID { return gBlk.ID() }
	sender.CantGetAcceptedFrontier = false

	te := &Transitive{}

	te.Initialize(config)

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		if !blkID.Equals(gBlk.ID()) {
			t.Fatalf("Wrong block requested")
		}
		return gBlk, nil
	}

	te.finishBootstrapping()

	vm.GetBlockF = nil
	vm.LastAcceptedF = nil
	sender.CantGetAcceptedFrontier = true

	return vdr, vals, sender, vm, te, gBlk
}

func TestEngineShutdown(t *testing.T) {
	_, _, _, vm, transitive, _ := setup(t)
	vmShutdownCalled := false
	vm.ShutdownF = func() { vmShutdownCalled = true }
	vm.CantShutdown = false
	transitive.Shutdown()
	if !vmShutdownCalled {
		t.Fatal("Shutting down the Transitive did not shutdown the VM")
	}
}

func TestEngineAdd(t *testing.T) {
	vdr, _, sender, vm, te, _ := setup(t)

	if !te.Context().ChainID.Equals(ids.Empty) {
		t.Fatalf("Wrong chain ID")
	}

	blk := &Blk{
		parent: &Blk{
			id:     GenerateID(),
			status: choices.Unknown,
		},
		id:     GenerateID(),
		status: choices.Processing,
		bytes:  []byte{1},
	}

	asked := new(bool)
	sender.GetF = func(inVdr ids.ShortID, _ uint32, blkID ids.ID) {
		if *asked {
			t.Fatalf("Asked multiple times")
		}
		*asked = true
		if !vdr.ID().Equals(inVdr) {
			t.Fatalf("Asking wrong validator for block")
		}
		if !blkID.Equals(blk.Parent().ID()) {
			t.Fatalf("Asking for wrong block")
		}
	}

	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, blk.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return blk, nil
	}

	te.Put(vdr.ID(), 0, blk.ID(), blk.Bytes())

	vm.ParseBlockF = nil

	if !*asked {
		t.Fatalf("Didn't ask for a missing block")
	}

	if len(te.blocked) != 1 {
		t.Fatalf("Should have been blocking on request")
	}

	vm.ParseBlockF = func(b []byte) (snowman.Block, error) { return nil, errParseBlock }

	te.Put(vdr.ID(), 0, blk.Parent().ID(), nil)

	vm.ParseBlockF = nil

	if len(te.blocked) != 0 {
		t.Fatalf("Should have finished blocking issue")
	}
}

func TestEngineQuery(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setup(t)

	blk := &Blk{
		parent: gBlk,
		id:     GenerateID(),
		status: choices.Processing,
		bytes:  []byte{1},
	}

	blocked := new(bool)
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		if *blocked {
			t.Fatalf("Sent multiple requests")
		}
		*blocked = true
		if !blkID.Equals(blk.ID()) {
			t.Fatalf("Wrong block requested")
		}
		return &Blk{id: blkID, status: choices.Unknown}, errUnknownBlock
	}

	asked := new(bool)
	getRequestID := new(uint32)
	sender.GetF = func(inVdr ids.ShortID, requestID uint32, blkID ids.ID) {
		if *asked {
			t.Fatalf("Asked multiple times")
		}
		*asked = true
		*getRequestID = requestID
		if !vdr.ID().Equals(inVdr) {
			t.Fatalf("Asking wrong validator for block")
		}
		if !blk.ID().Equals(blkID) {
			t.Fatalf("Asking for wrong block")
		}
	}

	te.PullQuery(vdr.ID(), 15, blk.ID())
	if !*blocked {
		t.Fatalf("Didn't request block")
	}
	if !*asked {
		t.Fatalf("Didn't request block from validator")
	}

	queried := new(bool)
	queryRequestID := new(uint32)
	sender.PushQueryF = func(inVdrs ids.ShortSet, requestID uint32, blkID ids.ID, blkBytes []byte) {
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
		if !blk.ID().Equals(blkID) {
			t.Fatalf("Asking for wrong block")
		}
	}

	chitted := new(bool)
	sender.ChitsF = func(inVdr ids.ShortID, requestID uint32, prefSet ids.Set) {
		if *chitted {
			t.Fatalf("Sent multiple chits")
		}
		*chitted = true
		if requestID != 15 {
			t.Fatalf("Wrong request ID")
		}
		if prefSet.Len() != 1 {
			t.Fatal("Should only be one vote")
		}
		if !blk.ID().Equals(prefSet.List()[0]) {
			t.Fatalf("Wrong chits block")
		}
	}

	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, blk.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return blk, nil
	}
	te.Put(vdr.ID(), *getRequestID, blk.ID(), blk.Bytes())
	vm.ParseBlockF = nil

	if !*queried {
		t.Fatalf("Didn't ask for preferences")
	}
	if !*chitted {
		t.Fatalf("Didn't provide preferences")
	}

	blk1 := &Blk{
		parent: blk,
		id:     GenerateID(),
		height: 1,
		status: choices.Processing,
		bytes:  []byte{5, 4, 3, 2, 1, 9},
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch {
		case blkID.Equals(blk.ID()):
			return blk, nil
		case blkID.Equals(blk1.ID()):
			return &Blk{id: blkID, status: choices.Unknown}, errUnknownBlock
		}
		t.Fatalf("Wrong block requested")
		panic("Should have failed")
	}

	*asked = false
	sender.GetF = func(inVdr ids.ShortID, requestID uint32, blkID ids.ID) {
		if *asked {
			t.Fatalf("Asked multiple times")
		}
		*asked = true
		*getRequestID = requestID
		if !vdr.ID().Equals(inVdr) {
			t.Fatalf("Asking wrong validator for block")
		}
		if !blk1.ID().Equals(blkID) {
			t.Fatalf("Asking for wrong block")
		}
	}
	blkSet := ids.Set{}
	blkSet.Add(blk1.ID())
	te.Chits(vdr.ID(), *queryRequestID, blkSet)

	*queried = false
	sender.PushQueryF = func(inVdrs ids.ShortSet, requestID uint32, blkID ids.ID, blkBytes []byte) {
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
		if !blkID.Equals(blk1.ID()) {
			t.Fatalf("Asking for wrong block")
		}
	}

	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, blk1.Bytes()) {
			t.Fatalf("Wrong bytes")
		}

		vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
			switch {
			case blkID.Equals(blk.ID()):
				return blk, nil
			case blkID.Equals(blk1.ID()):
				return blk1, nil
			}
			t.Fatalf("Wrong block requested")
			panic("Should have failed")
		}

		return blk1, nil
	}
	te.Put(vdr.ID(), *getRequestID, blk1.ID(), blk1.Bytes())
	vm.ParseBlockF = nil

	if blk1.Status() != choices.Accepted {
		t.Fatalf("Should have executed block")
	}
	if len(te.blocked) != 0 {
		t.Fatalf("Should have finished blocking")
	}

	_ = te.polls.String() // Shouldn't panic

	te.QueryFailed(vdr.ID(), *queryRequestID)
	if len(te.blocked) != 0 {
		t.Fatalf("Should have finished blocking")
	}
}

func TestEngineMultipleQuery(t *testing.T) {
	config := DefaultConfig()

	config.Params = snowball.Parameters{
		Metrics:           prometheus.NewRegistry(),
		K:                 3,
		Alpha:             2,
		BetaVirtuous:      1,
		BetaRogue:         2,
		ConcurrentRepolls: 1,
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

	vm := &VMTest{}
	vm.T = t
	config.VM = vm

	vm.Default(true)
	vm.CantSetPreference = false

	gBlk := &Blk{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	vm.LastAcceptedF = func() ids.ID { return gBlk.ID() }
	sender.CantGetAcceptedFrontier = false

	te := &Transitive{}
	te.Initialize(config)

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		if !blkID.Equals(gBlk.ID()) {
			t.Fatalf("Wrong block requested")
		}
		return gBlk, nil
	}

	te.finishBootstrapping()

	vm.GetBlockF = nil
	vm.LastAcceptedF = nil
	sender.CantGetAcceptedFrontier = true

	blk0 := &Blk{
		parent: gBlk,
		id:     GenerateID(),
		status: choices.Processing,
		bytes:  []byte{1},
	}

	queried := new(bool)
	queryRequestID := new(uint32)
	sender.PushQueryF = func(inVdrs ids.ShortSet, requestID uint32, blkID ids.ID, blkBytes []byte) {
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
		if !blk0.ID().Equals(blkID) {
			t.Fatalf("Asking for wrong block")
		}
	}

	te.insert(blk0)

	blk1 := &Blk{
		parent: blk0,
		id:     GenerateID(),
		status: choices.Processing,
		bytes:  []byte{1},
	}

	vm.GetBlockF = func(id ids.ID) (snowman.Block, error) {
		switch {
		case id.Equals(gBlk.ID()):
			return gBlk, nil
		case id.Equals(blk0.ID()):
			return blk0, nil
		case id.Equals(blk1.ID()):
			return &Blk{id: blk0.ID(), status: choices.Unknown}, errUnknownBlock
		}
		t.Fatalf("Unknown block")
		panic("Should have errored")
	}

	asked := new(bool)
	getRequestID := new(uint32)
	sender.GetF = func(inVdr ids.ShortID, requestID uint32, blkID ids.ID) {
		if *asked {
			t.Fatalf("Asked multiple times")
		}
		*asked = true
		*getRequestID = requestID
		if !vdr0.ID().Equals(inVdr) {
			t.Fatalf("Asking wrong validator for block")
		}
		if !blk1.ID().Equals(blkID) {
			t.Fatalf("Asking for wrong block")
		}
	}
	blkSet := ids.Set{}
	blkSet.Add(blk1.ID())
	te.Chits(vdr0.ID(), *queryRequestID, blkSet)
	te.Chits(vdr1.ID(), *queryRequestID, blkSet)

	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
			switch {
			case blkID.Equals(blk0.ID()):
				return blk0, nil
			case blkID.Equals(blk1.ID()):
				return blk1, nil
			}
			t.Fatalf("Wrong block requested")
			panic("Should have failed")
		}

		return blk1, nil
	}

	*queried = false
	secondQueryRequestID := new(uint32)
	sender.PushQueryF = func(inVdrs ids.ShortSet, requestID uint32, blkID ids.ID, blkBytes []byte) {
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		*secondQueryRequestID = requestID
		vdrSet := ids.ShortSet{}
		vdrSet.Add(vdr0.ID(), vdr1.ID(), vdr2.ID())
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !blk1.ID().Equals(blkID) {
			t.Fatalf("Asking for wrong block")
		}
	}
	te.Put(vdr0.ID(), *getRequestID, blk1.ID(), blk1.Bytes())

	// Should be dropped because the query was already filled
	blkSet = ids.Set{}
	blkSet.Add(blk0.ID())
	te.Chits(vdr2.ID(), *queryRequestID, blkSet)

	if blk1.Status() != choices.Accepted {
		t.Fatalf("Should have executed block")
	}
	if len(te.blocked) != 0 {
		t.Fatalf("Should have finished blocking")
	}
}

func TestEngineBlockedIssue(t *testing.T) {
	_, _, sender, _, te, gBlk := setup(t)

	sender.Default(false)

	blk0 := &Blk{
		parent: gBlk,
		id:     GenerateID(),
		status: choices.Unknown,
		bytes:  []byte{1},
	}

	blk1 := &Blk{
		parent: blk0,
		id:     GenerateID(),
		status: choices.Processing,
		bytes:  []byte{1},
	}

	te.insert(blk1)

	blk0.status = choices.Processing
	te.insert(blk0)

	if !blk1.ID().Equals(te.Consensus.Preference()) {
		t.Fatalf("Should have issued blk1")
	}
}

func TestEngineAbandonResponse(t *testing.T) {
	vdr, _, sender, _, te, gBlk := setup(t)

	sender.Default(false)

	blk := &Blk{
		parent: gBlk,
		id:     GenerateID(),
		status: choices.Unknown,
		bytes:  []byte{1},
	}

	te.insert(blk)
	te.QueryFailed(vdr.ID(), 1)

	if len(te.blocked) != 0 {
		t.Fatalf("Should have removed blocking event")
	}
}

func TestEngineFetchBlock(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setup(t)

	sender.Default(false)

	vm.GetBlockF = func(id ids.ID) (snowman.Block, error) {
		if id.Equals(gBlk.ID()) {
			return gBlk, nil
		}
		t.Fatalf("Unknown block")
		panic("Should have failed")
	}

	added := new(bool)
	sender.PutF = func(inVdr ids.ShortID, requestID uint32, blkID ids.ID, blk []byte) {
		if !vdr.ID().Equals(inVdr) {
			t.Fatalf("Wrong validator")
		}
		if requestID != 123 {
			t.Fatalf("Wrong request id")
		}
		if !gBlk.ID().Equals(blkID) {
			t.Fatalf("Wrong blockID")
		}
		*added = true
	}

	te.Get(vdr.ID(), 123, gBlk.ID())

	if !*added {
		t.Fatalf("Should have sent block to peer")
	}
}

func TestEnginePushQuery(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setup(t)

	sender.Default(true)

	blk := &Blk{
		parent: gBlk,
		id:     GenerateID(),
		status: choices.Processing,
		bytes:  []byte{1},
	}

	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if bytes.Equal(b, blk.Bytes()) {
			return blk, nil
		}
		return nil, errUnknownBytes
	}

	vm.GetBlockF = func(id ids.ID) (snowman.Block, error) {
		if id.Equals(blk.ID()) {
			return blk, nil
		}
		t.Fatal(errUnknownBytes)
		panic(errUnknownBytes)
	}

	chitted := new(bool)
	sender.ChitsF = func(inVdr ids.ShortID, requestID uint32, votes ids.Set) {
		if *chitted {
			t.Fatalf("Sent chit multiple times")
		}
		*chitted = true
		if !inVdr.Equals(vdr.ID()) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if requestID != 20 {
			t.Fatalf("Wrong request id")
		}
		if votes.Len() != 1 {
			t.Fatal("votes should only have one element")
		}
		vote := votes.List()[0]
		if !blk.ID().Equals(vote) {
			t.Fatalf("Asking for wrong block")
		}
	}

	queried := new(bool)
	sender.PushQueryF = func(inVdrs ids.ShortSet, _ uint32, blkID ids.ID, blkBytes []byte) {
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		vdrSet := ids.ShortSet{}
		vdrSet.Add(vdr.ID())
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !blk.ID().Equals(blkID) {
			t.Fatalf("Asking for wrong block")
		}
	}

	te.PushQuery(vdr.ID(), 20, blk.ID(), blk.Bytes())

	if !*chitted {
		t.Fatalf("Should have sent a chit to the peer")
	}
	if !*queried {
		t.Fatalf("Should have sent a query to the peer")
	}
}

func TestEngineBuildBlock(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setup(t)

	sender.Default(true)

	blk := &Blk{
		parent: gBlk,
		id:     GenerateID(),
		status: choices.Processing,
		bytes:  []byte{1},
	}

	queried := new(bool)
	sender.PushQueryF = func(inVdrs ids.ShortSet, _ uint32, blkID ids.ID, blkBytes []byte) {
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		vdrSet := ids.ShortSet{}
		vdrSet.Add(vdr.ID())
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
	}

	vm.BuildBlockF = func() (snowman.Block, error) { return blk, nil }
	te.Notify(common.PendingTxs)

	if !*queried {
		t.Fatalf("Should have sent a query to the peer")
	}
}

func TestEngineRepoll(t *testing.T) {
	vdr, _, sender, _, te, _ := setup(t)

	sender.Default(true)

	queried := new(bool)
	sender.PullQueryF = func(inVdrs ids.ShortSet, _ uint32, blkID ids.ID) {
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		vdrSet := ids.ShortSet{}
		vdrSet.Add(vdr.ID())
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
	}

	te.repoll()

	if !*queried {
		t.Fatalf("Should have sent a query to the peer")
	}
}

func TestVoteCanceling(t *testing.T) {
	config := DefaultConfig()

	config.Params = snowball.Parameters{
		Metrics:           prometheus.NewRegistry(),
		K:                 3,
		Alpha:             2,
		BetaVirtuous:      1,
		BetaRogue:         2,
		ConcurrentRepolls: 1,
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

	vm := &VMTest{}
	vm.T = t
	config.VM = vm

	vm.Default(true)
	vm.CantSetPreference = false

	gBlk := &Blk{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	vm.LastAcceptedF = func() ids.ID { return gBlk.ID() }
	vm.GetBlockF = func(id ids.ID) (snowman.Block, error) {
		switch {
		case id.Equals(gBlk.ID()):
			return gBlk, nil
		default:
			t.Fatalf("Loaded unknown block")
			panic("Should have failed")
		}
	}
	sender.CantGetAcceptedFrontier = false

	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()

	vm.LastAcceptedF = nil
	sender.CantGetAcceptedFrontier = true

	blk := &Blk{
		parent: gBlk,
		id:     GenerateID(),
		status: choices.Processing,
		bytes:  []byte{1},
	}

	queried := new(bool)
	queryRequestID := new(uint32)
	sender.PushQueryF = func(inVdrs ids.ShortSet, requestID uint32, blkID ids.ID, blkBytes []byte) {
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
		if !blk.ID().Equals(blkID) {
			t.Fatalf("Asking for wrong block")
		}
	}

	te.insert(blk)

	if len(te.polls.m) != 1 {
		t.Fatalf("Shouldn't have finished blocking issue")
	}

	te.QueryFailed(vdr0.ID(), *queryRequestID)

	if len(te.polls.m) != 1 {
		t.Fatalf("Shouldn't have finished blocking issue")
	}

	repolled := new(bool)
	sender.PullQueryF = func(inVdrs ids.ShortSet, requestID uint32, blkID ids.ID) {
		*repolled = true
	}
	te.QueryFailed(vdr1.ID(), *queryRequestID)

	if !*repolled {
		t.Fatalf("Should have finished blocking issue and repolled the network")
	}
}

func TestEngineNoQuery(t *testing.T) {
	config := DefaultConfig()

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)
	sender.CantGetAcceptedFrontier = false

	gBlk := &Blk{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	vm := &VMTest{}
	vm.T = t
	vm.LastAcceptedF = func() ids.ID { return gBlk.ID() }

	config.VM = vm
	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()

	blk := &Blk{
		parent: gBlk,
		id:     GenerateID(),
		status: choices.Processing,
		bytes:  []byte{1},
	}

	te.insert(blk)
}

func TestEngineNoRepollQuery(t *testing.T) {
	config := DefaultConfig()

	sender := &common.SenderTest{}
	sender.T = t
	config.Sender = sender

	sender.Default(true)
	sender.CantGetAcceptedFrontier = false

	gBlk := &Blk{
		id:     GenerateID(),
		status: choices.Accepted,
	}

	vm := &VMTest{}
	vm.T = t
	vm.LastAcceptedF = func() ids.ID { return gBlk.ID() }

	config.VM = vm
	te := &Transitive{}
	te.Initialize(config)
	te.finishBootstrapping()

	te.repoll()
}

func TestEngineAbandonQuery(t *testing.T) {
	vdr, _, sender, vm, te, _ := setup(t)

	sender.Default(true)

	blkID := GenerateID()

	vm.GetBlockF = func(id ids.ID) (snowman.Block, error) {
		switch {
		case id.Equals(blkID):
			return &Blk{status: choices.Unknown}, errUnknownBlock
		default:
			t.Fatalf("Loaded unknown block")
			panic("Should have failed")
		}
	}
	sender.CantGet = false

	te.PullQuery(vdr.ID(), 0, blkID)

	if len(te.blocked) != 1 {
		t.Fatalf("Should have blocked on request")
	}

	te.GetFailed(vdr.ID(), 0, blkID)

	if len(te.blocked) != 0 {
		t.Fatalf("Should have removed request")
	}
}

func TestEngineAbandonChit(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setup(t)

	sender.Default(true)

	blk := &Blk{
		parent: gBlk,
		id:     GenerateID(),
		status: choices.Processing,
		bytes:  []byte{1},
	}

	sender.CantPushQuery = false

	te.insert(blk)

	fakeBlkID := GenerateID()
	vm.GetBlockF = func(id ids.ID) (snowman.Block, error) {
		switch {
		case id.Equals(fakeBlkID):
			return &Blk{status: choices.Unknown}, errUnknownBlock
		default:
			t.Fatalf("Loaded unknown block")
			panic("Should have failed")
		}
	}
	sender.CantGet = false
	fakeBlkIDSet := ids.Set{}
	fakeBlkIDSet.Add(fakeBlkID)
	te.Chits(vdr.ID(), 0, fakeBlkIDSet)

	if len(te.blocked) != 1 {
		t.Fatalf("Should have blocked on request")
	}

	te.GetFailed(vdr.ID(), 0, fakeBlkID)

	if len(te.blocked) != 0 {
		t.Fatalf("Should have removed request")
	}
}

func TestEngineBlockingChitRequest(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setup(t)

	sender.Default(true)

	missingBlk := &Blk{
		parent: gBlk,
		id:     GenerateID(),
		status: choices.Unknown,
		bytes:  []byte{1},
	}
	parentBlk := &Blk{
		parent: missingBlk,
		id:     GenerateID(),
		status: choices.Processing,
		bytes:  []byte{1},
	}
	blockingBlk := &Blk{
		parent: parentBlk,
		id:     GenerateID(),
		status: choices.Processing,
		bytes:  []byte{1},
	}

	te.insert(parentBlk)

	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, blockingBlk.Bytes()):
			return blockingBlk, nil
		default:
			t.Fatalf("Loaded unknown block")
			panic("Should have failed")
		}
	}
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch {
		case blkID.Equals(blockingBlk.ID()):
			return blockingBlk, nil
		default:
			t.Fatalf("Loaded unknown block")
			panic("Should have failed")
		}
	}

	te.PushQuery(vdr.ID(), 0, blockingBlk.ID(), blockingBlk.Bytes())

	if len(te.blocked) != 3 {
		t.Fatalf("Both inserts should be blocking in addition to the chit request")
	}

	sender.CantPushQuery = false
	sender.CantChits = false

	missingBlk.status = choices.Processing
	te.insert(missingBlk)

	if len(te.blocked) != 0 {
		t.Fatalf("Both inserts should not longer be blocking")
	}
}

func TestEngineBlockingChitResponse(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setup(t)

	sender.Default(true)

	issuedBlk := &Blk{
		parent: gBlk,
		id:     GenerateID(),
		status: choices.Processing,
		bytes:  []byte{1},
	}

	missingBlk := &Blk{
		parent: gBlk,
		id:     GenerateID(),
		status: choices.Unknown,
		bytes:  []byte{1},
	}
	blockingBlk := &Blk{
		parent: missingBlk,
		id:     GenerateID(),
		status: choices.Processing,
		bytes:  []byte{1},
	}

	te.insert(blockingBlk)

	queryRequestID := new(uint32)
	sender.PushQueryF = func(inVdrs ids.ShortSet, requestID uint32, blkID ids.ID, blkBytes []byte) {
		*queryRequestID = requestID
		vdrSet := ids.ShortSet{}
		vdrSet.Add(vdr.ID())
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !blkID.Equals(issuedBlk.ID()) {
			t.Fatalf("Asking for wrong block")
		}
	}

	te.insert(issuedBlk)

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch {
		case blkID.Equals(blockingBlk.ID()):
			return blockingBlk, nil
		default:
			t.Fatalf("Loaded unknown block")
			panic("Should have failed")
		}
	}
	blockingBlkIDSet := ids.Set{}
	blockingBlkIDSet.Add(blockingBlk.ID())
	te.Chits(vdr.ID(), *queryRequestID, blockingBlkIDSet)

	if len(te.blocked) != 2 {
		t.Fatalf("The insert and the chit should be blocking")
	}

	sender.PushQueryF = nil
	sender.CantPushQuery = false

	missingBlk.status = choices.Processing
	te.insert(missingBlk)
}

func TestEngineRetryFetch(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setup(t)

	sender.Default(true)

	missingBlk := &Blk{
		parent: gBlk,
		id:     GenerateID(),
		height: 1,
		status: choices.Unknown,
		bytes:  []byte{1},
	}

	vm.CantGetBlock = false
	sender.CantGet = false

	te.PullQuery(vdr.ID(), 0, missingBlk.ID())

	vm.CantGetBlock = true
	sender.CantGet = true

	te.GetFailed(vdr.ID(), 0, missingBlk.ID())

	vm.CantGetBlock = false

	called := new(bool)
	sender.GetF = func(ids.ShortID, uint32, ids.ID) {
		*called = true
	}

	te.PullQuery(vdr.ID(), 0, missingBlk.ID())

	vm.CantGetBlock = true
	sender.CantGet = true

	if !*called {
		t.Fatalf("Should have requested the block again")
	}
}

func TestEngineUndeclaredDependencyDeadlock(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setup(t)

	sender.Default(true)

	validBlk := &Blk{
		parent: gBlk,
		id:     GenerateID(),
		height: 1,
		status: choices.Processing,
		bytes:  []byte{1},
	}

	invalidBlk := &Blk{
		parent:   validBlk,
		id:       GenerateID(),
		height:   2,
		status:   choices.Processing,
		validity: errors.New("invalid due to an undeclared dependency"),
		bytes:    []byte{2},
	}

	validBlkID := validBlk.ID()
	invalidBlkID := invalidBlk.ID()

	reqID := new(uint32)
	sender.PushQueryF = func(_ ids.ShortSet, requestID uint32, _ ids.ID, _ []byte) {
		*reqID = requestID
	}

	te.insert(validBlk)

	sender.PushQueryF = nil

	te.insert(invalidBlk)

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch {
		case blkID.Equals(validBlkID):
			return validBlk, nil
		case blkID.Equals(invalidBlkID):
			return invalidBlk, nil
		}
		return nil, errUnknownBlock
	}

	votes := ids.Set{}
	votes.Add(invalidBlkID)
	te.Chits(vdr.ID(), *reqID, votes)

	vm.GetBlockF = nil

	if status := validBlk.Status(); status != choices.Accepted {
		t.Fatalf("Should have bubbled invalid votes to the valid parent")
	}
}

func TestEngineInvalidBlockIgnoredFromUnexpectedPeer(t *testing.T) {
	vdr, vdrs, sender, vm, te, gBlk := setup(t)

	secondVdr := validators.GenerateRandomValidator(1)
	vdrs.Add(secondVdr)

	sender.Default(true)

	missingBlk := &Blk{
		parent: gBlk,
		id:     GenerateID(),
		height: 1,
		status: choices.Unknown,
		bytes:  []byte{1},
	}

	pendingBlk := &Blk{
		parent: missingBlk,
		id:     GenerateID(),
		height: 2,
		status: choices.Processing,
		bytes:  []byte{2},
	}

	parsed := new(bool)
	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, pendingBlk.Bytes()):
			*parsed = true
			return pendingBlk, nil
		}
		return nil, errUnknownBlock
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		if !*parsed {
			return nil, errUnknownBlock
		}

		switch {
		case blkID.Equals(pendingBlk.ID()):
			return pendingBlk, nil
		}
		return nil, errUnknownBlock
	}

	reqID := new(uint32)
	sender.GetF = func(reqVdr ids.ShortID, requestID uint32, blkID ids.ID) {
		*reqID = requestID
		if !reqVdr.Equals(vdr.ID()) {
			t.Fatalf("Wrong validator requested")
		}
		if !blkID.Equals(missingBlk.ID()) {
			t.Fatalf("Wrong block requested")
		}
	}

	te.PushQuery(vdr.ID(), 0, pendingBlk.ID(), pendingBlk.Bytes())

	te.Put(secondVdr.ID(), *reqID, missingBlk.ID(), []byte{3})

	*parsed = false
	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, missingBlk.Bytes()):
			*parsed = true
			return missingBlk, nil
		}
		return nil, errUnknownBlock
	}
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		if !*parsed {
			return nil, errUnknownBlock
		}

		switch {
		case blkID.Equals(missingBlk.ID()):
			return missingBlk, nil
		}
		return nil, errUnknownBlock
	}
	sender.CantPushQuery = false

	te.Put(vdr.ID(), *reqID, missingBlk.ID(), missingBlk.Bytes())

	pref := te.Consensus.Preference()
	if !pref.Equals(pendingBlk.ID()) {
		t.Fatalf("Shouldn't have abandoned the pending block")
	}
}
