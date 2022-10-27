// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/getter"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	errUnknownBlock = errors.New("unknown block")
	errUnknownBytes = errors.New("unknown bytes")
	Genesis         = ids.GenerateTestID()
)

func setup(t *testing.T, commonCfg common.Config, engCfg Config) (ids.NodeID, validators.Set, *common.SenderTest, *block.TestVM, *Transitive, snowman.Block) {
	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender
	commonCfg.Sender = sender
	sender.Default(true)

	vm := &block.TestVM{}
	vm.T = t
	engCfg.VM = vm

	snowGetHandler, err := getter.New(vm, commonCfg)
	if err != nil {
		t.Fatal(err)
	}
	engCfg.AllGetsServer = snowGetHandler

	vm.Default(true)
	vm.CantSetState = false
	vm.CantSetPreference = false

	gBlk := &snowman.TestBlock{TestDecidable: choices.TestDecidable{
		IDV:     Genesis,
		StatusV: choices.Accepted,
	}}

	vm.LastAcceptedF = func() (ids.ID, error) { return gBlk.ID(), nil }

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	if err := te.Start(0); err != nil {
		t.Fatal(err)
	}

	vm.GetBlockF = nil
	vm.LastAcceptedF = nil
	return vdr, vals, sender, vm, te, gBlk
}

func setupDefaultConfig(t *testing.T) (ids.NodeID, validators.Set, *common.SenderTest, *block.TestVM, *Transitive, snowman.Block) {
	commonCfg := common.DefaultConfigTest()
	engCfg := DefaultConfigs()
	return setup(t, commonCfg, engCfg)
}

func TestEngineShutdown(t *testing.T) {
	_, _, _, vm, transitive, _ := setupDefaultConfig(t)
	vmShutdownCalled := false
	vm.ShutdownF = func() error { vmShutdownCalled = true; return nil }
	vm.CantShutdown = false
	if err := transitive.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if !vmShutdownCalled {
		t.Fatal("Shutting down the Transitive did not shutdown the VM")
	}
}

func TestEngineAdd(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setupDefaultConfig(t)

	if te.Ctx.ChainID != ids.Empty {
		t.Fatalf("Wrong chain ID")
	}

	parent := &snowman.TestBlock{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Unknown,
	}}
	blk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: parent.IDV,
		HeightV: 1,
		BytesV:  []byte{1},
	}

	asked := new(bool)
	reqID := new(uint32)
	sender.SendGetF = func(_ context.Context, inVdr ids.NodeID, requestID uint32, blkID ids.ID) {
		*reqID = requestID
		if *asked {
			t.Fatalf("Asked multiple times")
		}
		*asked = true
		if vdr != inVdr {
			t.Fatalf("Asking wrong validator for block")
		}
		if blkID != blk.Parent() {
			t.Fatalf("Asking for wrong block")
		}
	}

	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, blk.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return blk, nil
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case parent.ID():
			return parent, nil
		default:
			return nil, errUnknownBlock
		}
	}

	if err := te.Put(context.Background(), vdr, 0, blk.Bytes()); err != nil {
		t.Fatal(err)
	}

	vm.ParseBlockF = nil

	if !*asked {
		t.Fatalf("Didn't ask for a missing block")
	}

	if len(te.blocked) != 1 {
		t.Fatalf("Should have been blocking on request")
	}

	vm.ParseBlockF = func(b []byte) (snowman.Block, error) { return nil, errUnknownBytes }

	if err := te.Put(context.Background(), vdr, *reqID, nil); err != nil {
		t.Fatal(err)
	}

	vm.ParseBlockF = nil

	if len(te.blocked) != 0 {
		t.Fatalf("Should have finished blocking issue")
	}
}

func TestEngineQuery(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setupDefaultConfig(t)

	blk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{1},
	}

	chitted := new(bool)
	sender.SendChitsF = func(_ context.Context, inVdr ids.NodeID, requestID uint32, prefSet []ids.ID) {
		if *chitted {
			t.Fatalf("Sent multiple chits")
		}
		*chitted = true
		if requestID != 15 {
			t.Fatalf("Wrong request ID")
		}
		if len(prefSet) != 1 {
			t.Fatal("Should only be one vote")
		}
		if gBlk.ID() != prefSet[0] {
			t.Fatalf("Wrong chits block")
		}
	}

	blocked := new(bool)
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		*blocked = true
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	asked := new(bool)
	getRequestID := new(uint32)
	sender.SendGetF = func(_ context.Context, inVdr ids.NodeID, requestID uint32, blkID ids.ID) {
		if *asked {
			t.Fatalf("Asked multiple times")
		}
		*asked = true
		*getRequestID = requestID
		if vdr != inVdr {
			t.Fatalf("Asking wrong validator for block")
		}
		if blk.ID() != blkID && gBlk.ID() != blkID {
			t.Fatalf("Asking for wrong block")
		}
	}

	if err := te.PullQuery(context.Background(), vdr, 15, blk.ID()); err != nil {
		t.Fatal(err)
	}
	if !*chitted {
		t.Fatalf("Didn't respond with chits")
	}
	if !*blocked {
		t.Fatalf("Didn't request block")
	}
	if !*asked {
		t.Fatalf("Didn't request block from validator")
	}

	queried := new(bool)
	queryRequestID := new(uint32)
	sender.SendPushQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, requestID uint32, blkBytes []byte) {
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		*queryRequestID = requestID
		vdrSet := ids.NodeIDSet{}
		vdrSet.Add(vdr)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !bytes.Equal(blk.Bytes(), blkBytes) {
			t.Fatalf("Asking for wrong block")
		}
	}

	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, blk.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return blk, nil
	}
	if err := te.Put(context.Background(), vdr, *getRequestID, blk.Bytes()); err != nil {
		t.Fatal(err)
	}
	vm.ParseBlockF = nil

	if !*queried {
		t.Fatalf("Didn't ask for preferences")
	}

	blk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: blk.IDV,
		HeightV: 2,
		BytesV:  []byte{5, 4, 3, 2, 1, 9},
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case blk.ID():
			return nil, errUnknownBlock
		case blk1.ID():
			return nil, errUnknownBlock
		}
		t.Fatalf("Wrong block requested")
		panic("Should have failed")
	}

	*asked = false
	sender.SendGetF = func(_ context.Context, inVdr ids.NodeID, requestID uint32, blkID ids.ID) {
		if *asked {
			t.Fatalf("Asked multiple times")
		}
		*asked = true
		*getRequestID = requestID
		if vdr != inVdr {
			t.Fatalf("Asking wrong validator for block")
		}
		if blk1.ID() != blkID {
			t.Fatalf("Asking for wrong block")
		}
	}
	if err := te.Chits(context.Background(), vdr, *queryRequestID, []ids.ID{blk1.ID()}); err != nil {
		t.Fatal(err)
	}

	*queried = false
	sender.SendPushQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, requestID uint32, blkBytes []byte) {
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		*queryRequestID = requestID
		vdrSet := ids.NodeIDSet{}
		vdrSet.Add(vdr)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !bytes.Equal(blk1.Bytes(), blkBytes) {
			t.Fatalf("Asking for wrong block")
		}
	}

	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, blk1.Bytes()) {
			t.Fatalf("Wrong bytes")
		}

		vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
			switch blkID {
			case blk.ID():
				return blk, nil
			case blk1.ID():
				return blk1, nil
			}
			t.Fatalf("Wrong block requested")
			panic("Should have failed")
		}

		return blk1, nil
	}
	if err := te.Put(context.Background(), vdr, *getRequestID, blk1.Bytes()); err != nil {
		t.Fatal(err)
	}
	vm.ParseBlockF = nil

	if blk1.Status() != choices.Accepted {
		t.Fatalf("Should have executed block")
	}
	if len(te.blocked) != 0 {
		t.Fatalf("Should have finished blocking")
	}

	_ = te.polls.String() // Shouldn't panic

	if err := te.QueryFailed(context.Background(), vdr, *queryRequestID); err != nil {
		t.Fatal(err)
	}
	if len(te.blocked) != 0 {
		t.Fatalf("Should have finished blocking")
	}
}

func TestEngineMultipleQuery(t *testing.T) {
	engCfg := DefaultConfigs()
	engCfg.Params = snowball.Parameters{
		K:                       3,
		Alpha:                   2,
		BetaVirtuous:            1,
		BetaRogue:               2,
		ConcurrentRepolls:       1,
		OptimalProcessing:       1,
		MaxOutstandingItems:     1,
		MaxItemProcessingTime:   1,
		MixedQueryNumPushNonVdr: 3,
	}

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr0 := ids.GenerateTestNodeID()
	vdr1 := ids.GenerateTestNodeID()
	vdr2 := ids.GenerateTestNodeID()

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
	engCfg.Sender = sender
	sender.Default(true)

	vm := &block.TestVM{}
	vm.T = t
	engCfg.VM = vm

	vm.Default(true)
	vm.CantSetState = false
	vm.CantSetPreference = false

	gBlk := &snowman.TestBlock{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vm.LastAcceptedF = func() (ids.ID, error) { return gBlk.ID(), nil }
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		if blkID != gBlk.ID() {
			t.Fatalf("Wrong block requested")
		}
		return gBlk, nil
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	if err := te.Start(0); err != nil {
		t.Fatal(err)
	}

	vm.GetBlockF = nil
	vm.LastAcceptedF = nil

	blk0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: gBlk.IDV,
		HeightV: 1,
		BytesV:  []byte{1},
	}

	queried := new(bool)
	queryRequestID := new(uint32)
	sender.SendPushQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, requestID uint32, blkBytes []byte) {
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		*queryRequestID = requestID
		vdrSet := ids.NodeIDSet{}
		vdrSet.Add(vdr0, vdr1, vdr2)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !bytes.Equal(blk0.Bytes(), blkBytes) {
			t.Fatalf("Asking for wrong block")
		}
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	if err := te.issue(context.Background(), blk0); err != nil {
		t.Fatal(err)
	}

	blk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: blk0.IDV,
		HeightV: 2,
		BytesV:  []byte{2},
	}

	vm.GetBlockF = func(id ids.ID) (snowman.Block, error) {
		switch id {
		case gBlk.ID():
			return gBlk, nil
		case blk0.ID():
			return blk0, nil
		case blk1.ID():
			return nil, errUnknownBlock
		}
		t.Fatalf("Unknown block")
		panic("Should have errored")
	}

	asked := new(bool)
	getRequestID := new(uint32)
	sender.SendGetF = func(_ context.Context, inVdr ids.NodeID, requestID uint32, blkID ids.ID) {
		if *asked {
			t.Fatalf("Asked multiple times")
		}
		*asked = true
		*getRequestID = requestID
		if vdr0 != inVdr {
			t.Fatalf("Asking wrong validator for block")
		}
		if blk1.ID() != blkID {
			t.Fatalf("Asking for wrong block")
		}
	}
	blkSet := []ids.ID{blk1.ID()}
	if err := te.Chits(context.Background(), vdr0, *queryRequestID, blkSet); err != nil {
		t.Fatal(err)
	}
	if err := te.Chits(context.Background(), vdr1, *queryRequestID, blkSet); err != nil {
		t.Fatal(err)
	}

	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
			switch {
			case blkID == blk0.ID():
				return blk0, nil
			case blkID == blk1.ID():
				return blk1, nil
			}
			t.Fatalf("Wrong block requested")
			panic("Should have failed")
		}

		return blk1, nil
	}

	*queried = false
	secondQueryRequestID := new(uint32)
	sender.SendPushQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, requestID uint32, blkBytes []byte) {
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		*secondQueryRequestID = requestID
		vdrSet := ids.NodeIDSet{}
		vdrSet.Add(vdr0, vdr1, vdr2)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !bytes.Equal(blk1.Bytes(), blkBytes) {
			t.Fatalf("Asking for wrong block")
		}
	}
	if err := te.Put(context.Background(), vdr0, *getRequestID, blk1.Bytes()); err != nil {
		t.Fatal(err)
	}

	// Should be dropped because the query was already filled
	blkSet = []ids.ID{blk0.ID()}
	if err := te.Chits(context.Background(), vdr2, *queryRequestID, blkSet); err != nil {
		t.Fatal(err)
	}

	if blk1.Status() != choices.Accepted {
		t.Fatalf("Should have executed block")
	}
	if len(te.blocked) != 0 {
		t.Fatalf("Should have finished blocking")
	}
}

func TestEngineBlockedIssue(t *testing.T) {
	_, _, sender, vm, te, gBlk := setupDefaultConfig(t)

	sender.Default(false)

	blk0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{1},
	}
	blk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: blk0.IDV,
		HeightV: 2,
		BytesV:  []byte{2},
	}

	sender.SendGetF = func(context.Context, ids.NodeID, uint32, ids.ID) {}
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case blk0.ID():
			return blk0, nil
		default:
			return nil, errUnknownBlock
		}
	}

	if err := te.issue(context.Background(), blk1); err != nil {
		t.Fatal(err)
	}

	blk0.StatusV = choices.Processing
	if err := te.issue(context.Background(), blk0); err != nil {
		t.Fatal(err)
	}

	if blk1.ID() != te.Consensus.Preference() {
		t.Fatalf("Should have issued blk1")
	}
}

func TestEngineAbandonResponse(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setupDefaultConfig(t)

	sender.Default(false)

	blk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{1},
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch {
		case blkID == gBlk.ID():
			return gBlk, nil
		case blkID == blk.ID():
			return nil, errUnknownBlock
		}
		t.Fatalf("Wrong block requested")
		return nil, errUnknownBlock
	}

	if err := te.issue(context.Background(), blk); err != nil {
		t.Fatal(err)
	}
	if err := te.QueryFailed(context.Background(), vdr, 1); err != nil {
		t.Fatal(err)
	}

	if len(te.blocked) != 0 {
		t.Fatalf("Should have removed blocking event")
	}
}

func TestEngineFetchBlock(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setupDefaultConfig(t)

	sender.Default(false)

	vm.GetBlockF = func(id ids.ID) (snowman.Block, error) {
		if id == gBlk.ID() {
			return gBlk, nil
		}
		t.Fatalf("Unknown block")
		panic("Should have failed")
	}

	added := new(bool)
	sender.SendPutF = func(_ context.Context, inVdr ids.NodeID, requestID uint32, blk []byte) {
		if vdr != inVdr {
			t.Fatalf("Wrong validator")
		}
		if requestID != 123 {
			t.Fatalf("Wrong request id")
		}
		if !bytes.Equal(gBlk.Bytes(), blk) {
			t.Fatalf("Asking for wrong block")
		}
		*added = true
	}

	if err := te.Get(context.Background(), vdr, 123, gBlk.ID()); err != nil {
		t.Fatal(err)
	}

	if !*added {
		t.Fatalf("Should have sent block to peer")
	}
}

func TestEnginePushQuery(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setupDefaultConfig(t)

	sender.Default(true)

	blk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{1},
	}

	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if bytes.Equal(b, blk.Bytes()) {
			return blk, nil
		}
		return nil, errUnknownBytes
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case blk.ID():
			return blk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	chitted := new(bool)
	sender.SendChitsF = func(_ context.Context, inVdr ids.NodeID, requestID uint32, votes []ids.ID) {
		if *chitted {
			t.Fatalf("Sent chit multiple times")
		}
		*chitted = true
		if inVdr != vdr {
			t.Fatalf("Asking wrong validator for preference")
		}
		if requestID != 20 {
			t.Fatalf("Wrong request id")
		}
		if len(votes) != 1 {
			t.Fatal("votes should only have one element")
		}
		if gBlk.ID() != votes[0] {
			t.Fatalf("Asking for wrong block")
		}
	}

	queried := new(bool)
	sender.SendPushQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, _ uint32, blkBytes []byte) {
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		vdrSet := ids.NodeIDSet{}
		vdrSet.Add(vdr)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !bytes.Equal(blk.Bytes(), blkBytes) {
			t.Fatalf("Asking for wrong block")
		}
	}

	if err := te.PushQuery(context.Background(), vdr, 20, blk.Bytes()); err != nil {
		t.Fatal(err)
	}

	if !*chitted {
		t.Fatalf("Should have sent a chit to the peer")
	}
	if !*queried {
		t.Fatalf("Should have sent a query to the peer")
	}
}

func TestEngineBuildBlock(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setupDefaultConfig(t)

	sender.Default(true)

	blk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{1},
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	queried := new(bool)
	sender.SendPushQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, _ uint32, blkBytes []byte) {
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		vdrSet := ids.NodeIDSet{}
		vdrSet.Add(vdr)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
	}

	vm.BuildBlockF = func() (snowman.Block, error) { return blk, nil }
	if err := te.Notify(common.PendingTxs); err != nil {
		t.Fatal(err)
	}

	if !*queried {
		t.Fatalf("Should have sent a query to the peer")
	}
}

func TestEngineRepoll(t *testing.T) {
	vdr, _, sender, _, te, _ := setupDefaultConfig(t)

	sender.Default(true)

	queried := new(bool)
	sender.SendPullQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, _ uint32, blkID ids.ID) {
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		vdrSet := ids.NodeIDSet{}
		vdrSet.Add(vdr)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
	}

	te.repoll(context.Background())

	if !*queried {
		t.Fatalf("Should have sent a query to the peer")
	}
}

func TestVoteCanceling(t *testing.T) {
	engCfg := DefaultConfigs()
	engCfg.Params = snowball.Parameters{
		K:                       3,
		Alpha:                   2,
		BetaVirtuous:            1,
		BetaRogue:               2,
		ConcurrentRepolls:       1,
		OptimalProcessing:       1,
		MaxOutstandingItems:     1,
		MaxItemProcessingTime:   1,
		MixedQueryNumPushNonVdr: 3,
	}

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr0 := ids.GenerateTestNodeID()
	vdr1 := ids.GenerateTestNodeID()
	vdr2 := ids.GenerateTestNodeID()

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
	engCfg.Sender = sender
	sender.Default(true)

	vm := &block.TestVM{}
	vm.T = t
	engCfg.VM = vm

	vm.Default(true)
	vm.CantSetState = false
	vm.CantSetPreference = false

	gBlk := &snowman.TestBlock{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vm.LastAcceptedF = func() (ids.ID, error) { return gBlk.ID(), nil }
	vm.GetBlockF = func(id ids.ID) (snowman.Block, error) {
		switch id {
		case gBlk.ID():
			return gBlk, nil
		default:
			t.Fatalf("Loaded unknown block")
			panic("Should have failed")
		}
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	if err := te.Start(0); err != nil {
		t.Fatal(err)
	}

	vm.LastAcceptedF = nil

	blk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: gBlk.IDV,
		HeightV: 1,
		BytesV:  []byte{1},
	}

	queried := new(bool)
	queryRequestID := new(uint32)
	sender.SendPushQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, requestID uint32, blkBytes []byte) {
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		*queryRequestID = requestID
		vdrSet := ids.NodeIDSet{}
		vdrSet.Add(vdr0, vdr1, vdr2)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !bytes.Equal(blk.Bytes(), blkBytes) {
			t.Fatalf("Asking for wrong block")
		}
	}

	if err := te.issue(context.Background(), blk); err != nil {
		t.Fatal(err)
	}

	if te.polls.Len() != 1 {
		t.Fatalf("Shouldn't have finished blocking issue")
	}

	if err := te.QueryFailed(context.Background(), vdr0, *queryRequestID); err != nil {
		t.Fatal(err)
	}

	if te.polls.Len() != 1 {
		t.Fatalf("Shouldn't have finished blocking issue")
	}

	repolled := new(bool)
	sender.SendPullQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, requestID uint32, blkID ids.ID) {
		*repolled = true
	}
	if err := te.QueryFailed(context.Background(), vdr1, *queryRequestID); err != nil {
		t.Fatal(err)
	}

	if !*repolled {
		t.Fatalf("Should have finished blocking issue and repolled the network")
	}
}

func TestEngineNoQuery(t *testing.T) {
	engCfg := DefaultConfigs()

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender
	sender.Default(true)

	gBlk := &snowman.TestBlock{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vm := &block.TestVM{}
	vm.T = t
	vm.LastAcceptedF = func() (ids.ID, error) { return gBlk.ID(), nil }

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		if blkID == gBlk.ID() {
			return gBlk, nil
		}
		return nil, errUnknownBlock
	}

	engCfg.VM = vm

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	if err := te.Start(0); err != nil {
		t.Fatal(err)
	}

	blk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: gBlk.IDV,
		HeightV: 1,
		BytesV:  []byte{1},
	}

	if err := te.issue(context.Background(), blk); err != nil {
		t.Fatal(err)
	}
}

func TestEngineNoRepollQuery(t *testing.T) {
	engCfg := DefaultConfigs()

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender
	sender.Default(true)

	gBlk := &snowman.TestBlock{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vm := &block.TestVM{}
	vm.T = t
	vm.LastAcceptedF = func() (ids.ID, error) { return gBlk.ID(), nil }

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		if blkID == gBlk.ID() {
			return gBlk, nil
		}
		return nil, errUnknownBlock
	}

	engCfg.VM = vm

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	if err := te.Start(0); err != nil {
		t.Fatal(err)
	}

	te.repoll(context.Background())
}

func TestEngineAbandonQuery(t *testing.T) {
	vdr, _, sender, vm, te, _ := setupDefaultConfig(t)

	sender.Default(true)

	blkID := ids.GenerateTestID()

	vm.GetBlockF = func(id ids.ID) (snowman.Block, error) {
		switch id {
		case blkID:
			return nil, errUnknownBlock
		default:
			t.Fatalf("Loaded unknown block")
			panic("Should have failed")
		}
	}

	reqID := new(uint32)
	sender.SendGetF = func(_ context.Context, _ ids.NodeID, requestID uint32, _ ids.ID) {
		*reqID = requestID
	}

	sender.CantSendChits = false

	if err := te.PullQuery(context.Background(), vdr, 0, blkID); err != nil {
		t.Fatal(err)
	}

	if te.blkReqs.Len() != 1 {
		t.Fatalf("Should have issued request")
	}

	if err := te.GetFailed(context.Background(), vdr, *reqID); err != nil {
		t.Fatal(err)
	}

	if te.blkReqs.Len() != 0 {
		t.Fatalf("Should have removed request")
	}
}

func TestEngineAbandonChit(t *testing.T) {
	require := require.New(t)

	vdr, _, sender, vm, te, gBlk := setupDefaultConfig(t)

	sender.Default(true)

	blk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{1},
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case blk.ID():
			return nil, errUnknownBlock
		}
		t.Fatalf("Wrong block requested")
		return nil, errUnknownBlock
	}

	var reqID uint32
	sender.SendPushQueryF = func(_ context.Context, _ ids.NodeIDSet, requestID uint32, _ []byte) {
		reqID = requestID
	}

	err := te.issue(context.Background(), blk)
	require.NoError(err)

	fakeBlkID := ids.GenerateTestID()
	vm.GetBlockF = func(id ids.ID) (snowman.Block, error) {
		require.Equal(fakeBlkID, id)
		return nil, errUnknownBlock
	}

	sender.SendGetF = func(_ context.Context, _ ids.NodeID, requestID uint32, _ ids.ID) {
		reqID = requestID
	}

	// Register a voter dependency on an unknown block.
	err = te.Chits(context.Background(), vdr, reqID, []ids.ID{fakeBlkID})
	require.NoError(err)
	require.Len(te.blocked, 1)

	sender.CantSendPullQuery = false

	err = te.GetFailed(context.Background(), vdr, reqID)
	require.NoError(err)
	require.Empty(te.blocked)
}

func TestEngineAbandonChitWithUnexpectedPutBlock(t *testing.T) {
	require := require.New(t)

	vdr, _, sender, vm, te, gBlk := setupDefaultConfig(t)

	sender.Default(true)

	blk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{1},
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case blk.ID():
			return nil, errUnknownBlock
		}
		t.Fatalf("Wrong block requested")
		return nil, errUnknownBlock
	}

	var reqID uint32
	sender.SendPushQueryF = func(_ context.Context, _ ids.NodeIDSet, requestID uint32, _ []byte) {
		reqID = requestID
	}

	err := te.issue(context.Background(), blk)
	require.NoError(err)

	fakeBlkID := ids.GenerateTestID()
	vm.GetBlockF = func(id ids.ID) (snowman.Block, error) {
		require.Equal(fakeBlkID, id)
		return nil, errUnknownBlock
	}

	sender.SendGetF = func(_ context.Context, _ ids.NodeID, requestID uint32, _ ids.ID) {
		reqID = requestID
	}

	// Register a voter dependency on an unknown block.
	err = te.Chits(context.Background(), vdr, reqID, []ids.ID{fakeBlkID})
	require.NoError(err)
	require.Len(te.blocked, 1)

	sender.CantSendPullQuery = false

	gBlkBytes := gBlk.Bytes()
	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		require.Equal(gBlkBytes, b)
		return gBlk, nil
	}

	// Respond with an unexpected block and verify that the request is correctly
	// cleared.
	err = te.Put(context.Background(), vdr, reqID, gBlkBytes)
	require.NoError(err)
	require.Empty(te.blocked)
}

func TestEngineBlockingChitRequest(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setupDefaultConfig(t)

	sender.Default(true)

	missingBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{1},
	}
	parentBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: missingBlk.IDV,
		HeightV: 2,
		BytesV:  []byte{2},
	}
	blockingBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: parentBlk.IDV,
		HeightV: 3,
		BytesV:  []byte{3},
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case blockingBlk.ID():
			return blockingBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	sender.SendGetF = func(context.Context, ids.NodeID, uint32, ids.ID) {}

	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, blockingBlk.Bytes()):
			return blockingBlk, nil
		default:
			t.Fatalf("Loaded unknown block")
			panic("Should have failed")
		}
	}

	if err := te.issue(context.Background(), parentBlk); err != nil {
		t.Fatal(err)
	}

	sender.CantSendChits = false

	if err := te.PushQuery(context.Background(), vdr, 0, blockingBlk.Bytes()); err != nil {
		t.Fatal(err)
	}

	if len(te.blocked) != 2 {
		t.Fatalf("Both inserts should be blocking")
	}

	sender.CantSendPushQuery = false

	missingBlk.StatusV = choices.Processing
	if err := te.issue(context.Background(), missingBlk); err != nil {
		t.Fatal(err)
	}

	if len(te.blocked) != 0 {
		t.Fatalf("Both inserts should not longer be blocking")
	}
}

func TestEngineBlockingChitResponse(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setupDefaultConfig(t)

	sender.Default(true)

	issuedBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{1},
	}
	missingBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{2},
	}
	blockingBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: missingBlk.IDV,
		HeightV: 2,
		BytesV:  []byte{3},
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case blockingBlk.ID():
			return blockingBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	if err := te.issue(context.Background(), blockingBlk); err != nil {
		t.Fatal(err)
	}

	queryRequestID := new(uint32)
	sender.SendPushQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, requestID uint32, blkBytes []byte) {
		*queryRequestID = requestID
		vdrSet := ids.NodeIDSet{}
		vdrSet.Add(vdr)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !bytes.Equal(issuedBlk.Bytes(), blkBytes) {
			t.Fatalf("Asking for wrong block")
		}
	}

	if err := te.issue(context.Background(), issuedBlk); err != nil {
		t.Fatal(err)
	}

	sender.SendPushQueryF = nil
	sender.CantSendPushQuery = false

	if err := te.Chits(context.Background(), vdr, *queryRequestID, []ids.ID{blockingBlk.ID()}); err != nil {
		t.Fatal(err)
	}

	require.Len(t, te.blocked, 2)
	sender.CantSendPullQuery = false

	missingBlk.StatusV = choices.Processing
	if err := te.issue(context.Background(), missingBlk); err != nil {
		t.Fatal(err)
	}
}

func TestEngineRetryFetch(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setupDefaultConfig(t)

	sender.Default(true)

	missingBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{1},
	}

	vm.CantGetBlock = false

	reqID := new(uint32)
	sender.SendGetF = func(_ context.Context, _ ids.NodeID, requestID uint32, _ ids.ID) {
		*reqID = requestID
	}
	sender.CantSendChits = false

	if err := te.PullQuery(context.Background(), vdr, 0, missingBlk.ID()); err != nil {
		t.Fatal(err)
	}

	vm.CantGetBlock = true
	sender.SendGetF = nil

	if err := te.GetFailed(context.Background(), vdr, *reqID); err != nil {
		t.Fatal(err)
	}

	vm.CantGetBlock = false

	called := new(bool)
	sender.SendGetF = func(context.Context, ids.NodeID, uint32, ids.ID) {
		*called = true
	}

	if err := te.PullQuery(context.Background(), vdr, 0, missingBlk.ID()); err != nil {
		t.Fatal(err)
	}

	vm.CantGetBlock = true
	sender.SendGetF = nil

	if !*called {
		t.Fatalf("Should have requested the block again")
	}
}

func TestEngineUndeclaredDependencyDeadlock(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setupDefaultConfig(t)

	sender.Default(true)

	validBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{1},
	}
	invalidBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: validBlk.IDV,
		HeightV: 2,
		VerifyV: errors.New(""),
		BytesV:  []byte{2},
	}

	invalidBlkID := invalidBlk.ID()

	reqID := new(uint32)
	sender.SendPushQueryF = func(_ context.Context, _ ids.NodeIDSet, requestID uint32, _ []byte) {
		*reqID = requestID
	}
	sender.SendPullQueryF = func(_ context.Context, _ ids.NodeIDSet, requestID uint32, _ ids.ID) {}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case validBlk.ID():
			return validBlk, nil
		case invalidBlk.ID():
			return invalidBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}
	if err := te.issue(context.Background(), validBlk); err != nil {
		t.Fatal(err)
	}
	sender.SendPushQueryF = nil
	if err := te.issue(context.Background(), invalidBlk); err != nil {
		t.Fatal(err)
	}

	if err := te.Chits(context.Background(), vdr, *reqID, []ids.ID{invalidBlkID}); err != nil {
		t.Fatal(err)
	}

	if status := validBlk.Status(); status != choices.Accepted {
		t.Log(status)
		t.Fatalf("Should have bubbled invalid votes to the valid parent")
	}
}

func TestEngineGossip(t *testing.T) {
	_, _, sender, vm, te, gBlk := setupDefaultConfig(t)

	vm.LastAcceptedF = func() (ids.ID, error) { return gBlk.ID(), nil }
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		if blkID == gBlk.ID() {
			return gBlk, nil
		}
		t.Fatal(errUnknownBlock)
		return nil, errUnknownBlock
	}

	called := new(bool)
	sender.SendGossipF = func(_ context.Context, blkBytes []byte) {
		*called = true
		if !bytes.Equal(blkBytes, gBlk.Bytes()) {
			t.Fatal(errUnknownBytes)
		}
	}

	if err := te.Gossip(); err != nil {
		t.Fatal(err)
	}

	if !*called {
		t.Fatalf("Should have gossiped the block")
	}
}

func TestEngineInvalidBlockIgnoredFromUnexpectedPeer(t *testing.T) {
	vdr, vdrs, sender, vm, te, gBlk := setupDefaultConfig(t)

	secondVdr := ids.GenerateTestNodeID()
	if err := vdrs.AddWeight(secondVdr, 1); err != nil {
		t.Fatal(err)
	}

	sender.Default(true)

	missingBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{1},
	}
	pendingBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: missingBlk.IDV,
		HeightV: 2,
		BytesV:  []byte{2},
	}

	parsed := new(bool)
	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if bytes.Equal(b, pendingBlk.Bytes()) {
			*parsed = true
			return pendingBlk, nil
		}
		return nil, errUnknownBlock
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case pendingBlk.ID():
			if !*parsed {
				return nil, errUnknownBlock
			}
			return pendingBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	reqID := new(uint32)
	sender.SendGetF = func(_ context.Context, reqVdr ids.NodeID, requestID uint32, blkID ids.ID) {
		*reqID = requestID
		if reqVdr != vdr {
			t.Fatalf("Wrong validator requested")
		}
		if blkID != missingBlk.ID() {
			t.Fatalf("Wrong block requested")
		}
	}
	sender.CantSendChits = false

	if err := te.PushQuery(context.Background(), vdr, 0, pendingBlk.Bytes()); err != nil {
		t.Fatal(err)
	}

	if err := te.Put(context.Background(), secondVdr, *reqID, []byte{3}); err != nil {
		t.Fatal(err)
	}

	*parsed = false
	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if bytes.Equal(b, missingBlk.Bytes()) {
			*parsed = true
			return missingBlk, nil
		}
		return nil, errUnknownBlock
	}
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case missingBlk.ID():
			if !*parsed {
				return nil, errUnknownBlock
			}
			return missingBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}
	sender.CantSendPushQuery = false

	missingBlk.StatusV = choices.Processing

	if err := te.Put(context.Background(), vdr, *reqID, missingBlk.Bytes()); err != nil {
		t.Fatal(err)
	}

	pref := te.Consensus.Preference()
	if pref != pendingBlk.ID() {
		t.Fatalf("Shouldn't have abandoned the pending block")
	}
}

func TestEnginePushQueryRequestIDConflict(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setupDefaultConfig(t)

	sender.Default(true)

	missingBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{1},
	}
	pendingBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: missingBlk.IDV,
		HeightV: 2,
		BytesV:  []byte{2},
	}

	parsed := new(bool)
	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if bytes.Equal(b, pendingBlk.Bytes()) {
			*parsed = true
			return pendingBlk, nil
		}
		return nil, errUnknownBlock
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case pendingBlk.ID():
			if !*parsed {
				return nil, errUnknownBlock
			}
			return pendingBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	reqID := new(uint32)
	sender.SendGetF = func(_ context.Context, reqVdr ids.NodeID, requestID uint32, blkID ids.ID) {
		*reqID = requestID
		if reqVdr != vdr {
			t.Fatalf("Wrong validator requested")
		}
		if blkID != missingBlk.ID() {
			t.Fatalf("Wrong block requested")
		}
	}
	sender.CantSendChits = false

	if err := te.PushQuery(context.Background(), vdr, 0, pendingBlk.Bytes()); err != nil {
		t.Fatal(err)
	}

	sender.SendGetF = nil
	sender.CantSendGet = false

	if err := te.PushQuery(context.Background(), vdr, *reqID, []byte{3}); err != nil {
		t.Fatal(err)
	}

	*parsed = false
	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if bytes.Equal(b, missingBlk.Bytes()) {
			*parsed = true
			return missingBlk, nil
		}
		return nil, errUnknownBlock
	}
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case missingBlk.ID():
			if !*parsed {
				return nil, errUnknownBlock
			}
			return missingBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}
	sender.CantSendPushQuery = false

	if err := te.Put(context.Background(), vdr, *reqID, missingBlk.Bytes()); err != nil {
		t.Fatal(err)
	}

	pref := te.Consensus.Preference()
	if pref != pendingBlk.ID() {
		t.Fatalf("Shouldn't have abandoned the pending block")
	}
}

func TestEngineAggressivePolling(t *testing.T) {
	engCfg := DefaultConfigs()
	engCfg.Params.ConcurrentRepolls = 2

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender
	sender.Default(true)

	vm := &block.TestVM{}
	vm.T = t
	engCfg.VM = vm

	vm.Default(true)
	vm.CantSetState = false
	vm.CantSetPreference = false

	gBlk := &snowman.TestBlock{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vm.LastAcceptedF = func() (ids.ID, error) { return gBlk.ID(), nil }
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		if blkID != gBlk.ID() {
			t.Fatalf("Wrong block requested")
		}
		return gBlk, nil
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	if err := te.Start(0); err != nil {
		t.Fatal(err)
	}

	vm.GetBlockF = nil
	vm.LastAcceptedF = nil

	pendingBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: gBlk.IDV,
		HeightV: 1,
		BytesV:  []byte{1},
	}

	parsed := new(bool)
	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if bytes.Equal(b, pendingBlk.Bytes()) {
			*parsed = true
			return pendingBlk, nil
		}
		return nil, errUnknownBlock
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case pendingBlk.ID():
			if !*parsed {
				return nil, errUnknownBlock
			}
			return pendingBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	numPushed := new(int)
	sender.SendPushQueryF = func(context.Context, ids.NodeIDSet, uint32, []byte) { *numPushed++ }

	numPulled := new(int)
	sender.SendPullQueryF = func(context.Context, ids.NodeIDSet, uint32, ids.ID) { *numPulled++ }

	if err := te.Put(context.Background(), vdr, 0, pendingBlk.Bytes()); err != nil {
		t.Fatal(err)
	}

	if *numPushed != 1 {
		t.Fatalf("Should have initially sent a push query")
	}

	if *numPulled != 1 {
		t.Fatalf("Should have sent an additional pull query")
	}
}

func TestEngineDoubleChit(t *testing.T) {
	engCfg := DefaultConfigs()
	engCfg.Params = snowball.Parameters{
		K:                       2,
		Alpha:                   2,
		BetaVirtuous:            1,
		BetaRogue:               2,
		ConcurrentRepolls:       1,
		OptimalProcessing:       1,
		MaxOutstandingItems:     1,
		MaxItemProcessingTime:   1,
		MixedQueryNumPushNonVdr: 2,
	}

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr0 := ids.GenerateTestNodeID()
	vdr1 := ids.GenerateTestNodeID()

	if err := vals.AddWeight(vdr0, 1); err != nil {
		t.Fatal(err)
	}
	if err := vals.AddWeight(vdr1, 1); err != nil {
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender

	sender.Default(true)

	vm := &block.TestVM{}
	vm.T = t
	engCfg.VM = vm

	vm.Default(true)
	vm.CantSetState = false
	vm.CantSetPreference = false

	gBlk := &snowman.TestBlock{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vm.LastAcceptedF = func() (ids.ID, error) { return gBlk.ID(), nil }
	vm.GetBlockF = func(id ids.ID) (snowman.Block, error) {
		if id == gBlk.ID() {
			return gBlk, nil
		}
		t.Fatalf("Unknown block")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	if err := te.Start(0); err != nil {
		t.Fatal(err)
	}

	vm.LastAcceptedF = nil

	blk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: gBlk.IDV,
		HeightV: 1,
		BytesV:  []byte{1},
	}

	queried := new(bool)
	queryRequestID := new(uint32)
	sender.SendPushQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, requestID uint32, blkBytes []byte) {
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		*queryRequestID = requestID
		vdrSet := ids.NodeIDSet{}
		vdrSet.Add(vdr0, vdr1)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !bytes.Equal(blk.Bytes(), blkBytes) {
			t.Fatalf("Asking for wrong block")
		}
	}

	if err := te.issue(context.Background(), blk); err != nil {
		t.Fatal(err)
	}

	vm.GetBlockF = func(id ids.ID) (snowman.Block, error) {
		switch id {
		case gBlk.ID():
			return gBlk, nil
		case blk.ID():
			return blk, nil
		}
		t.Fatalf("Unknown block")
		panic("Should have errored")
	}

	blkSet := []ids.ID{blk.ID()}

	if status := blk.Status(); status != choices.Processing {
		t.Fatalf("Wrong status: %s ; expected: %s", status, choices.Processing)
	}

	if err := te.Chits(context.Background(), vdr0, *queryRequestID, blkSet); err != nil {
		t.Fatal(err)
	}

	if status := blk.Status(); status != choices.Processing {
		t.Fatalf("Wrong status: %s ; expected: %s", status, choices.Processing)
	}

	if err := te.Chits(context.Background(), vdr0, *queryRequestID, blkSet); err != nil {
		t.Fatal(err)
	}

	if status := blk.Status(); status != choices.Processing {
		t.Fatalf("Wrong status: %s ; expected: %s", status, choices.Processing)
	}

	if err := te.Chits(context.Background(), vdr1, *queryRequestID, blkSet); err != nil {
		t.Fatal(err)
	}

	if status := blk.Status(); status != choices.Accepted {
		t.Fatalf("Wrong status: %s ; expected: %s", status, choices.Accepted)
	}
}

func TestEngineBuildBlockLimit(t *testing.T) {
	engCfg := DefaultConfigs()
	engCfg.Params.K = 1
	engCfg.Params.Alpha = 1
	engCfg.Params.OptimalProcessing = 1

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender
	sender.Default(true)

	vm := &block.TestVM{}
	vm.T = t
	engCfg.VM = vm

	vm.Default(true)
	vm.CantSetState = false
	vm.CantSetPreference = false

	gBlk := &snowman.TestBlock{TestDecidable: choices.TestDecidable{
		IDV:     Genesis,
		StatusV: choices.Accepted,
	}}

	vm.LastAcceptedF = func() (ids.ID, error) { return gBlk.ID(), nil }
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		if blkID != gBlk.ID() {
			t.Fatalf("Wrong block requested")
		}
		return gBlk, nil
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

	if err := te.Start(0); err != nil {
		t.Fatal(err)
	}

	vm.GetBlockF = nil
	vm.LastAcceptedF = nil

	blk0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: gBlk.IDV,
		HeightV: 1,
		BytesV:  []byte{1},
	}
	blk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: blk0.IDV,
		HeightV: 2,
		BytesV:  []byte{2},
	}
	blks := []snowman.Block{blk0, blk1}

	var (
		queried bool
		reqID   uint32
	)
	sender.SendPushQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, rID uint32, _ []byte) {
		reqID = rID
		if queried {
			t.Fatalf("Asked multiple times")
		}
		queried = true
		vdrSet := ids.NodeIDSet{}
		vdrSet.Add(vdr)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	blkToReturn := 0
	vm.BuildBlockF = func() (snowman.Block, error) {
		if blkToReturn >= len(blks) {
			t.Fatalf("Built too many blocks")
		}
		blk := blks[blkToReturn]
		blkToReturn++
		return blk, nil
	}
	if err := te.Notify(common.PendingTxs); err != nil {
		t.Fatal(err)
	}

	if !queried {
		t.Fatalf("Should have sent a query to the peer")
	}

	queried = false
	if err := te.Notify(common.PendingTxs); err != nil {
		t.Fatal(err)
	}

	if queried {
		t.Fatalf("Shouldn't have sent a query to the peer")
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case blk0.ID():
			return blk0, nil
		default:
			return nil, errUnknownBlock
		}
	}

	if err := te.Chits(context.Background(), vdr, reqID, []ids.ID{blk0.ID()}); err != nil {
		t.Fatal(err)
	}

	if !queried {
		t.Fatalf("Should have sent a query to the peer")
	}
}

func TestEngineReceiveNewRejectedBlock(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setupDefaultConfig(t)

	acceptedBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{1},
	}
	rejectedBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{2},
	}
	pendingBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: rejectedBlk.IDV,
		HeightV: 2,
		BytesV:  []byte{3},
	}

	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, acceptedBlk.Bytes()):
			return acceptedBlk, nil
		case bytes.Equal(b, rejectedBlk.Bytes()):
			return rejectedBlk, nil
		case bytes.Equal(b, pendingBlk.Bytes()):
			return pendingBlk, nil
		default:
			t.Fatalf("Unknown block bytes")
			return nil, nil
		}
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case acceptedBlk.ID():
			return acceptedBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	var (
		asked bool
		reqID uint32
	)
	sender.SendPushQueryF = func(_ context.Context, _ ids.NodeIDSet, rID uint32, blkBytes []byte) {
		asked = true
		reqID = rID
	}

	if err := te.Put(context.Background(), vdr, 0, acceptedBlk.Bytes()); err != nil {
		t.Fatal(err)
	}

	if !asked {
		t.Fatalf("Didn't query for the new block")
	}

	if err := te.Chits(context.Background(), vdr, reqID, []ids.ID{acceptedBlk.ID()}); err != nil {
		t.Fatal(err)
	}

	sender.SendPushQueryF = nil
	asked = false

	sender.SendGetF = func(_ context.Context, _ ids.NodeID, rID uint32, _ ids.ID) {
		asked = true
		reqID = rID
	}

	if err := te.Put(context.Background(), vdr, 0, pendingBlk.Bytes()); err != nil {
		t.Fatal(err)
	}

	if !asked {
		t.Fatalf("Didn't request the missing block")
	}

	rejectedBlk.StatusV = choices.Rejected

	if err := te.Put(context.Background(), vdr, reqID, rejectedBlk.Bytes()); err != nil {
		t.Fatal(err)
	}

	if te.blkReqs.Len() != 0 {
		t.Fatalf("Should have finished all requests")
	}
}

func TestEngineRejectionAmplification(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setupDefaultConfig(t)

	acceptedBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{1},
	}
	rejectedBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{2},
	}
	pendingBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: rejectedBlk.IDV,
		HeightV: 2,
		BytesV:  []byte{3},
	}

	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, acceptedBlk.Bytes()):
			return acceptedBlk, nil
		case bytes.Equal(b, rejectedBlk.Bytes()):
			return rejectedBlk, nil
		case bytes.Equal(b, pendingBlk.Bytes()):
			return pendingBlk, nil
		default:
			t.Fatalf("Unknown block bytes")
			return nil, nil
		}
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case acceptedBlk.ID():
			return acceptedBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	var (
		queried bool
		reqID   uint32
	)
	sender.SendPushQueryF = func(_ context.Context, _ ids.NodeIDSet, rID uint32, _ []byte) {
		queried = true
		reqID = rID
	}

	if err := te.Put(context.Background(), vdr, 0, acceptedBlk.Bytes()); err != nil {
		t.Fatal(err)
	}

	if !queried {
		t.Fatalf("Didn't query for the new block")
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case acceptedBlk.ID():
			return acceptedBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	if err := te.Chits(context.Background(), vdr, reqID, []ids.ID{acceptedBlk.ID()}); err != nil {
		t.Fatal(err)
	}

	if !te.Consensus.Finalized() {
		t.Fatalf("Should have finalized the consensus instance")
	}

	queried = false
	var asked bool
	sender.SendPushQueryF = func(context.Context, ids.NodeIDSet, uint32, []byte) {
		queried = true
	}
	sender.SendGetF = func(_ context.Context, _ ids.NodeID, rID uint32, blkID ids.ID) {
		asked = true
		reqID = rID

		if blkID != rejectedBlk.ID() {
			t.Fatalf("requested %s but should have requested %s", blkID, rejectedBlk.ID())
		}
	}

	if err := te.Put(context.Background(), vdr, 0, pendingBlk.Bytes()); err != nil {
		t.Fatal(err)
	}

	if queried {
		t.Fatalf("Queried for the pending block")
	}
	if !asked {
		t.Fatalf("Should have asked for the missing block")
	}

	rejectedBlk.StatusV = choices.Processing
	if err := te.Put(context.Background(), vdr, reqID, rejectedBlk.Bytes()); err != nil {
		t.Fatal(err)
	}

	if queried {
		t.Fatalf("Queried for the rejected block")
	}
}

// Test that the node will not issue a block into consensus that it knows will
// be rejected because the parent is rejected.
func TestEngineTransitiveRejectionAmplificationDueToRejectedParent(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setupDefaultConfig(t)

	acceptedBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{1},
	}
	rejectedBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Rejected,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{2},
	}
	pendingBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			RejectV: errors.New("shouldn't have issued to consensus"),
			StatusV: choices.Processing,
		},
		ParentV: rejectedBlk.IDV,
		HeightV: 2,
		BytesV:  []byte{3},
	}

	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, acceptedBlk.Bytes()):
			return acceptedBlk, nil
		case bytes.Equal(b, rejectedBlk.Bytes()):
			return rejectedBlk, nil
		case bytes.Equal(b, pendingBlk.Bytes()):
			return pendingBlk, nil
		default:
			t.Fatalf("Unknown block bytes")
			return nil, nil
		}
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case acceptedBlk.ID():
			return acceptedBlk, nil
		case rejectedBlk.ID():
			return rejectedBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	var (
		queried bool
		reqID   uint32
	)
	sender.SendPushQueryF = func(_ context.Context, _ ids.NodeIDSet, rID uint32, _ []byte) {
		queried = true
		reqID = rID
	}

	if err := te.Put(context.Background(), vdr, 0, acceptedBlk.Bytes()); err != nil {
		t.Fatal(err)
	}

	if !queried {
		t.Fatalf("Didn't query for the new block")
	}

	if err := te.Chits(context.Background(), vdr, reqID, []ids.ID{acceptedBlk.ID()}); err != nil {
		t.Fatal(err)
	}

	if !te.Consensus.Finalized() {
		t.Fatalf("Should have finalized the consensus instance")
	}

	if err := te.Put(context.Background(), vdr, 0, pendingBlk.Bytes()); err != nil {
		t.Fatal(err)
	}

	if !te.Consensus.Finalized() {
		t.Fatalf("Should have finalized the consensus instance")
	}

	if len(te.pending) != 0 {
		t.Fatalf("Shouldn't have any pending blocks")
	}
}

// Test that the node will not issue a block into consensus that it knows will
// be rejected because the parent is failing verification.
func TestEngineTransitiveRejectionAmplificationDueToInvalidParent(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setupDefaultConfig(t)

	acceptedBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{1},
	}
	rejectedBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		VerifyV: errors.New("invalid"),
		BytesV:  []byte{2},
	}
	pendingBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			RejectV: errors.New("shouldn't have issued to consensus"),
			StatusV: choices.Processing,
		},
		ParentV: rejectedBlk.IDV,
		HeightV: 2,
		BytesV:  []byte{3},
	}

	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, acceptedBlk.Bytes()):
			return acceptedBlk, nil
		case bytes.Equal(b, rejectedBlk.Bytes()):
			return rejectedBlk, nil
		case bytes.Equal(b, pendingBlk.Bytes()):
			return pendingBlk, nil
		default:
			t.Fatalf("Unknown block bytes")
			return nil, nil
		}
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	var (
		queried bool
		reqID   uint32
	)
	sender.SendPushQueryF = func(_ context.Context, _ ids.NodeIDSet, rID uint32, blkBytes []byte) {
		queried = true
		reqID = rID
	}

	if err := te.Put(context.Background(), vdr, 0, acceptedBlk.Bytes()); err != nil {
		t.Fatal(err)
	}

	if !queried {
		t.Fatalf("Didn't query for the new block")
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case rejectedBlk.ID():
			return rejectedBlk, nil
		case acceptedBlk.ID():
			return acceptedBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	if err := te.Chits(context.Background(), vdr, reqID, []ids.ID{acceptedBlk.ID()}); err != nil {
		t.Fatal(err)
	}

	if !te.Consensus.Finalized() {
		t.Fatalf("Should have finalized the consensus instance")
	}

	if err := te.Put(context.Background(), vdr, 0, pendingBlk.Bytes()); err != nil {
		t.Fatal(err)
	}

	if !te.Consensus.Finalized() {
		t.Fatalf("Should have finalized the consensus instance")
	}

	if len(te.pending) != 0 {
		t.Fatalf("Shouldn't have any pending blocks")
	}
}

// Test that the node will not gossip a block that isn't preferred.
func TestEngineNonPreferredAmplification(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setupDefaultConfig(t)

	preferredBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{1},
	}
	nonPreferredBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{2},
	}

	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, preferredBlk.Bytes()):
			return preferredBlk, nil
		case bytes.Equal(b, nonPreferredBlk.Bytes()):
			return nonPreferredBlk, nil
		default:
			t.Fatalf("Unknown block bytes")
			return nil, nil
		}
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	sender.SendPushQueryF = func(_ context.Context, _ ids.NodeIDSet, _ uint32, blkBytes []byte) {
		if bytes.Equal(nonPreferredBlk.Bytes(), blkBytes) {
			t.Fatalf("gossiped non-preferred block")
		}
	}
	sender.SendPullQueryF = func(_ context.Context, _ ids.NodeIDSet, _ uint32, blkID ids.ID) {
		if blkID == nonPreferredBlk.ID() {
			t.Fatalf("gossiped non-preferred block")
		}
	}

	if err := te.Put(context.Background(), vdr, 0, preferredBlk.Bytes()); err != nil {
		t.Fatal(err)
	}

	if err := te.Put(context.Background(), vdr, 0, nonPreferredBlk.Bytes()); err != nil {
		t.Fatal(err)
	}
}

// Test that in the following scenario, if block B fails verification, votes
// will still be bubbled through to the valid block A. This is a regression test
// to ensure that the consensus engine correctly handles the case that votes can
// be bubbled correctly through a block that cannot pass verification until one
// of its ancestors has been marked as accepted.
//  G
//  |
//  A
//  |
//  B
func TestEngineBubbleVotesThroughInvalidBlock(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setupDefaultConfig(t)

	// [blk1] is a child of [gBlk] and currently passes verification
	blk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{1},
	}
	// [blk2] is a child of [blk1] and cannot pass verification until [blk1]
	// has been marked as accepted.
	blk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: blk1.ID(),
		HeightV: 2,
		BytesV:  []byte{2},
		VerifyV: errors.New("blk2 does not pass verification until after blk1 is accepted"),
	}

	// The VM should be able to parse [blk1] and [blk2]
	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, blk1.Bytes()):
			return blk1, nil
		case bytes.Equal(b, blk2.Bytes()):
			return blk2, nil
		default:
			t.Fatalf("Unknown block bytes")
			return nil, nil
		}
	}

	// The VM should only be able to retrieve [gBlk] from storage
	// TODO GetBlockF should be updated after blocks are verified/accepted
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	asked := new(bool)
	reqID := new(uint32)
	sender.SendGetF = func(_ context.Context, inVdr ids.NodeID, requestID uint32, blkID ids.ID) {
		*reqID = requestID
		if *asked {
			t.Fatalf("Asked multiple times")
		}
		if blkID != blk1.ID() {
			t.Fatalf("Expected engine to request blk1")
		}
		if inVdr != vdr {
			t.Fatalf("Expected engine to request blk2 from vdr")
		}
		*asked = true
	}
	// Receive Gossip message for [blk2] first and expect the sender to issue a Get request for
	// its ancestor: [blk1].
	if err := te.Put(context.Background(), vdr, constants.GossipMsgRequestID, blk2.Bytes()); err != nil {
		t.Fatal(err)
	}

	if !*asked {
		t.Fatalf("Didn't ask for missing blk1")
	}

	// Prepare to PushQuery [blk1] after our Get request is fulfilled. We should not PushQuery
	// [blk2] since it currently fails verification.
	queried := new(bool)
	queryRequestID := new(uint32)
	sender.SendPushQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, requestID uint32, blkBytes []byte) {
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		*queryRequestID = requestID
		vdrSet := ids.NodeIDSet{}
		vdrSet.Add(vdr)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !bytes.Equal(blk1.Bytes(), blkBytes) {
			t.Fatalf("Asking for wrong block")
		}
	}

	// Answer the request, this should allow [blk1] to be issued and cause [blk2] to
	// fail verification.
	if err := te.Put(context.Background(), vdr, *reqID, blk1.Bytes()); err != nil {
		t.Fatal(err)
	}

	// now blk1 is verified, vm can return it
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case blk1.ID():
			return blk1, nil
		default:
			return nil, errUnknownBlock
		}
	}

	if !*queried {
		t.Fatalf("Didn't ask for preferences regarding blk1")
	}

	sendReqID := new(uint32)
	reqVdr := new(ids.NodeID)
	// Update GetF to produce a more detailed error message in the case that receiving a Chits
	// message causes us to send another Get request.
	sender.SendGetF = func(_ context.Context, inVdr ids.NodeID, requestID uint32, blkID ids.ID) {
		switch blkID {
		case blk1.ID():
			t.Fatal("Unexpectedly sent a Get request for blk1")
		case blk2.ID():
			*sendReqID = requestID
			*reqVdr = inVdr
			return
		default:
			t.Fatal("Unexpectedly sent a Get request for unknown block")
		}
	}

	sender.SendPullQueryF = func(_ context.Context, _ ids.NodeIDSet, _ uint32, blkID ids.ID) {
		switch blkID {
		case blk1.ID():
			t.Fatal("Unexpectedly sent a PullQuery request for blk1")
		case blk2.ID():
			t.Fatal("Unexpectedly sent a PullQuery request for blk2")
		default:
			t.Fatal("Unexpectedly sent a PullQuery request for unknown block")
		}
	}

	// Now we are expecting a Chits message, and we receive it for blk2 instead of blk1
	// The votes should be bubbled through blk2 despite the fact that it is failing verification.
	if err := te.Chits(context.Background(), vdr, *queryRequestID, []ids.ID{blk2.ID()}); err != nil {
		t.Fatal(err)
	}

	if err := te.Put(context.Background(), *reqVdr, *sendReqID, blk2.Bytes()); err != nil {
		t.Fatal(err)
	}

	// The vote should be bubbled through [blk2], such that [blk1] gets marked as Accepted.
	if blk1.Status() != choices.Accepted {
		t.Fatalf("Expected blk1 to be Accepted, but found status: %s", blk1.Status())
	}

	// Now that [blk1] has been marked as Accepted, [blk2] can pass verification.
	blk2.VerifyV = nil
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case blk1.ID():
			return blk1, nil
		case blk2.ID():
			return blk2, nil
		default:
			return nil, errUnknownBlock
		}
	}
	*queried = false
	// Prepare to PushQuery [blk2] after receiving a Gossip message with [blk2].
	sender.SendPushQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, requestID uint32, blkBytes []byte) {
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		*queryRequestID = requestID
		vdrSet := ids.NodeIDSet{}
		vdrSet.Add(vdr)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !bytes.Equal(blk2.Bytes(), blkBytes) {
			t.Fatalf("Asking for wrong block")
		}
	}
	// Expect that the Engine will send a PushQuery after receiving this Gossip message for [blk2].
	if err := te.Put(context.Background(), vdr, constants.GossipMsgRequestID, blk2.Bytes()); err != nil {
		t.Fatal(err)
	}

	if !*queried {
		t.Fatalf("Didn't ask for preferences regarding blk2")
	}

	// After a single vote for [blk2], it should be marked as accepted.
	if err := te.Chits(context.Background(), vdr, *queryRequestID, []ids.ID{blk2.ID()}); err != nil {
		t.Fatal(err)
	}

	if blk2.Status() != choices.Accepted {
		t.Fatalf("Expected blk2 to be Accepted, but found status: %s", blk2.Status())
	}
}

// Test that in the following scenario, if block B fails verification, votes
// will still be bubbled through from block C to the valid block A. This is a
// regression test to ensure that the consensus engine correctly handles the
// case that votes can be bubbled correctly through a chain that cannot pass
// verification until one of its ancestors has been marked as accepted.
//  G
//  |
//  A
//  |
//  B
//  |
//  C
func TestEngineBubbleVotesThroughInvalidChain(t *testing.T) {
	vdr, _, sender, vm, te, gBlk := setupDefaultConfig(t)

	// [blk1] is a child of [gBlk] and currently passes verification
	blk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{1},
	}
	// [blk2] is a child of [blk1] and cannot pass verification until [blk1]
	// has been marked as accepted.
	blk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: blk1.ID(),
		HeightV: 2,
		BytesV:  []byte{2},
		VerifyV: errors.New("blk2 does not pass verification until after blk1 is accepted"),
	}
	// [blk3] is a child of [blk2] and will not attempt to be issued until
	// [blk2] has successfully been verified.
	blk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: blk2.ID(),
		HeightV: 3,
		BytesV:  []byte{3},
	}

	// The VM should be able to parse [blk1], [blk2], and [blk3]
	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, blk1.Bytes()):
			return blk1, nil
		case bytes.Equal(b, blk2.Bytes()):
			return blk2, nil
		case bytes.Equal(b, blk3.Bytes()):
			return blk3, nil
		default:
			t.Fatalf("Unknown block bytes")
			return nil, nil
		}
	}

	// The VM should be able to retrieve [gBlk] and [blk1] from storage
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case blk1.ID():
			return blk1, nil
		default:
			return nil, errUnknownBlock
		}
	}

	asked := new(bool)
	reqID := new(uint32)
	sender.SendGetF = func(_ context.Context, inVdr ids.NodeID, requestID uint32, blkID ids.ID) {
		*reqID = requestID
		if *asked {
			t.Fatalf("Asked multiple times")
		}
		if blkID != blk2.ID() {
			t.Fatalf("Expected engine to request blk2")
		}
		if inVdr != vdr {
			t.Fatalf("Expected engine to request blk2 from vdr")
		}
		*asked = true
	}
	// Receive Gossip message for [blk3] first and expect the sender to issue a
	// Get request for its ancestor: [blk2].
	if err := te.Put(context.Background(), vdr, constants.GossipMsgRequestID, blk3.Bytes()); err != nil {
		t.Fatal(err)
	}

	if !*asked {
		t.Fatalf("Didn't ask for missing blk2")
	}

	// Prepare to PushQuery [blk1] after our request for [blk2] is fulfilled.
	// We should not PushQuery [blk2] since it currently fails verification.
	// We should not PushQuery [blk3] because [blk2] wasn't issued.
	queried := new(bool)
	queryRequestID := new(uint32)
	sender.SendPushQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, requestID uint32, blkBytes []byte) {
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		*queryRequestID = requestID
		vdrSet := ids.NodeIDSet{}
		vdrSet.Add(vdr)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !bytes.Equal(blk1.Bytes(), blkBytes) {
			t.Fatalf("Asking for wrong block")
		}
	}

	// Answer the request, this should result in [blk1] being issued as well.
	if err := te.Put(context.Background(), vdr, *reqID, blk2.Bytes()); err != nil {
		t.Fatal(err)
	}

	if !*queried {
		t.Fatalf("Didn't ask for preferences regarding blk1")
	}

	sendReqID := new(uint32)
	reqVdr := new(ids.NodeID)
	// Update GetF to produce a more detailed error message in the case that receiving a Chits
	// message causes us to send another Get request.
	sender.SendGetF = func(_ context.Context, inVdr ids.NodeID, requestID uint32, blkID ids.ID) {
		switch blkID {
		case blk1.ID():
			t.Fatal("Unexpectedly sent a Get request for blk1")
		case blk2.ID():
			t.Logf("sending get for blk2 with %d", requestID)
			*sendReqID = requestID
			*reqVdr = inVdr
			return
		case blk3.ID():
			t.Logf("sending get for blk3 with %d", requestID)
			*sendReqID = requestID
			*reqVdr = inVdr
			return
		default:
			t.Fatal("Unexpectedly sent a Get request for unknown block")
		}
	}

	sender.SendPullQueryF = func(_ context.Context, _ ids.NodeIDSet, _ uint32, blkID ids.ID) {
		switch blkID {
		case blk1.ID():
			t.Fatal("Unexpectedly sent a PullQuery request for blk1")
		case blk2.ID():
			t.Fatal("Unexpectedly sent a PullQuery request for blk2")
		case blk3.ID():
			t.Fatal("Unexpectedly sent a PullQuery request for blk3")
		default:
			t.Fatal("Unexpectedly sent a PullQuery request for unknown block")
		}
	}

	// Now we are expecting a Chits message, and we receive it for [blk3]
	// instead of blk1. This will cause the node to again request [blk3].
	if err := te.Chits(context.Background(), vdr, *queryRequestID, []ids.ID{blk3.ID()}); err != nil {
		t.Fatal(err)
	}

	// Drop the re-request for blk3 to cause the poll to termindate. The votes
	// should be bubbled through blk3 despite the fact that it hasn't been
	// issued.
	if err := te.GetFailed(context.Background(), *reqVdr, *sendReqID); err != nil {
		t.Fatal(err)
	}

	// The vote should be bubbled through [blk3] and [blk2] such that [blk1]
	// gets marked as Accepted.
	if blk1.Status() != choices.Accepted {
		t.Fatalf("Expected blk1 to be Accepted, but found status: %s", blk1.Status())
	}
}

func TestMixedQueryNumPushSet(t *testing.T) {
	for i := 0; i < 3; i++ {
		t.Run(
			fmt.Sprint(i),
			func(t *testing.T) {
				engCfg := DefaultConfigs()
				engCfg.Params.MixedQueryNumPushVdr = i
				te, err := newTransitive(engCfg)
				if err != nil {
					t.Fatal(err)
				}
				if te.Params.MixedQueryNumPushVdr != i {
					t.Fatalf("expected to push query %v validators but got %v", i, te.Config.Params.MixedQueryNumPushVdr)
				}
			},
		)
	}
}

func TestSendMixedQuery(t *testing.T) {
	type test struct {
		isVdr bool
	}
	tests := []test{
		{isVdr: true},
		{isVdr: false},
	}
	for _, tt := range tests {
		t.Run(
			fmt.Sprintf("is validator: %v", tt.isVdr),
			func(t *testing.T) {
				engConfig := DefaultConfigs()
				commonCfg := common.DefaultConfigTest()
				// Override the parameters k and MixedQueryNumPushNonVdr,
				// and update the validator set to have k validators.
				engConfig.Params.Alpha = 12
				engConfig.Params.MixedQueryNumPushNonVdr = 12
				engConfig.Params.MixedQueryNumPushVdr = 14
				engConfig.Params.K = 20
				_, vdrSet, sender, vm, te, gBlk := setup(t, commonCfg, engConfig)

				vdrsList := []validators.Validator{}
				vdrs := ids.NodeIDSet{}
				for i := 0; i < te.Config.Params.K; i++ {
					vdr := ids.GenerateTestNodeID()
					vdrs.Add(vdr)
					vdrsList = append(vdrsList, validators.NewValidator(vdr, 1))
				}
				if tt.isVdr {
					vdrs.Add(te.Ctx.NodeID)
					vdrsList = append(vdrsList, validators.NewValidator(te.Ctx.NodeID, 1))
				}
				if err := vdrSet.Set(vdrsList); err != nil {
					t.Fatal(err)
				}

				// [blk1] is a child of [gBlk] and passes verification
				blk1 := &snowman.TestBlock{
					TestDecidable: choices.TestDecidable{
						IDV:     ids.GenerateTestID(),
						StatusV: choices.Processing,
					},
					ParentV: gBlk.ID(),
					HeightV: 1,
					BytesV:  []byte{1},
				}

				// The VM should be able to parse [blk1]
				vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
					switch {
					case bytes.Equal(b, blk1.Bytes()):
						return blk1, nil
					default:
						t.Fatalf("Unknown block bytes")
						return nil, nil
					}
				}

				// The VM should only be able to retrieve [gBlk] from storage
				vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
					switch blkID {
					case gBlk.ID():
						return gBlk, nil
					default:
						return nil, errUnknownBlock
					}
				}

				pullQuerySent := new(bool)
				pullQueryReqID := new(uint32)
				pullQueriedVdrs := ids.NodeIDSet{}
				sender.SendPullQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, requestID uint32, blkID ids.ID) {
					switch {
					case *pullQuerySent:
						t.Fatalf("Asked multiple times")
					case blkID != blk1.ID():
						t.Fatalf("Expected engine to request blk1")
					}
					pullQueriedVdrs.Union(inVdrs)
					*pullQuerySent = true
					*pullQueryReqID = requestID
				}

				pushQuerySent := new(bool)
				pushQueryReqID := new(uint32)
				pushQueriedVdrs := ids.NodeIDSet{}
				sender.SendPushQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, requestID uint32, blkBytes []byte) {
					switch {
					case *pushQuerySent:
						t.Fatal("Asked multiple times")
					case !bytes.Equal(blkBytes, blk1.Bytes()):
						t.Fatal("got unexpected block bytes instead of blk1")
					}
					*pushQuerySent = true
					*pushQueryReqID = requestID
					pushQueriedVdrs.Union(inVdrs)
				}

				// Give the engine blk1. It should insert it into consensus and send a mixed query
				// consisting of 12 push queries and 8 pull queries.
				if err := te.Put(context.Background(), vdrSet.List()[0].ID(), constants.GossipMsgRequestID, blk1.Bytes()); err != nil {
					t.Fatal(err)
				}

				switch {
				case !*pullQuerySent:
					t.Fatal("expected us to send pull queries")
				case !*pushQuerySent:
					t.Fatal("expected us to send push queries")
				case *pushQueryReqID != *pullQueryReqID:
					t.Fatalf("expected equal push query (%v) and pull query (%v) req IDs", *pushQueryReqID, *pullQueryReqID)
				case pushQueriedVdrs.Len()+pullQueriedVdrs.Len() != te.Config.Params.K:
					t.Fatalf("expected num push queried (%d) + num pull queried (%d) to be %d", pushQueriedVdrs.Len(), pullQueriedVdrs.Len(), te.Config.Params.K)
				case !tt.isVdr && pushQueriedVdrs.Len() != te.Params.MixedQueryNumPushNonVdr:
					t.Fatalf("expected num push queried (%d) to be %d", pushQueriedVdrs.Len(), te.Params.MixedQueryNumPushNonVdr)
				case tt.isVdr && pushQueriedVdrs.Len() != te.Params.MixedQueryNumPushVdr:
					t.Fatalf("expected num push queried (%d) to be %d", pushQueriedVdrs.Len(), te.Params.MixedQueryNumPushVdr)
				}

				pullQueriedVdrs.Union(pushQueriedVdrs) // Now this holds all queried validators (push and pull)
				for vdr := range pullQueriedVdrs {
					if !vdrs.Contains(vdr) {
						t.Fatalf("got unexpected vdr %v", vdr)
					}
				}
			})
	}
}

func TestEngineBuildBlockWithCachedNonVerifiedParent(t *testing.T) {
	require := require.New(t)
	vdr, _, sender, vm, te, gBlk := setupDefaultConfig(t)

	sender.Default(true)

	grandParentBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: gBlk.ID(),
		HeightV: 1,
		BytesV:  []byte{1},
	}

	parentBlkA := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: grandParentBlk.ID(),
		HeightV: 2,
		VerifyV: errors.New(""), // Reports as invalid
		BytesV:  []byte{2},
	}

	// Note that [parentBlkB] has the same [ID()] as [parentBlkA];
	// it's a different instantiation of the same block.
	parentBlkB := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     parentBlkA.IDV,
			StatusV: choices.Processing,
		},
		ParentV: parentBlkA.ParentV,
		HeightV: parentBlkA.HeightV,
		BytesV:  parentBlkA.BytesV,
	}

	// Child of [parentBlkA]/[parentBlkB]
	childBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: parentBlkA.ID(),
		HeightV: 3,
		BytesV:  []byte{3},
	}

	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		require.Equal(grandParentBlk.BytesV, b)
		return grandParentBlk, nil
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case grandParentBlk.IDV:
			return grandParentBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	queryRequestGPID := new(uint32)
	sender.SendPushQueryF = func(_ context.Context, _ ids.NodeIDSet, requestID uint32, blkBytes []byte) {
		require.Equal(grandParentBlk.Bytes(), blkBytes)
		*queryRequestGPID = requestID
	}

	// Give the engine the grandparent
	err := te.Put(context.Background(), vdr, 0, grandParentBlk.BytesV)
	require.NoError(err)

	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		require.Equal(parentBlkA.BytesV, b)
		return parentBlkA, nil
	}

	// Give the node [parentBlkA]/[parentBlkB].
	// When it's parsed we get [parentBlkA] (not [parentBlkB]).
	// [parentBlkA] fails verification and gets put into [te.nonVerifiedCache].
	err = te.Put(context.Background(), vdr, 0, parentBlkA.BytesV)
	require.NoError(err)

	vm.ParseBlockF = func(b []byte) (snowman.Block, error) {
		require.Equal(parentBlkB.BytesV, b)
		return parentBlkB, nil
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlk.ID():
			return gBlk, nil
		case grandParentBlk.IDV:
			return grandParentBlk, nil
		case parentBlkB.IDV:
			return parentBlkB, nil
		default:
			return nil, errUnknownBlock
		}
	}

	queryRequestAID := new(uint32)
	sender.SendPushQueryF = func(_ context.Context, _ ids.NodeIDSet, requestID uint32, blkBytes []byte) {
		require.Equal(parentBlkA.Bytes(), blkBytes)
		*queryRequestAID = requestID
	}
	sender.CantSendPullQuery = false

	// Give the engine [parentBlkA]/[parentBlkB] again.
	// This time when we parse it we get [parentBlkB] (not [parentBlkA]).
	// When we fetch it using [GetBlockF] we get [parentBlkB].
	// Note that [parentBlkB] doesn't fail verification and is issued into consensus.
	// This evicts [parentBlkA] from [te.nonVerifiedCache].
	err = te.Put(context.Background(), vdr, 0, parentBlkA.BytesV)
	require.NoError(err)

	// Give 2 chits for [parentBlkA]/[parentBlkB]
	err = te.Chits(context.Background(), vdr, *queryRequestAID, []ids.ID{parentBlkB.IDV})
	require.NoError(err)

	err = te.Chits(context.Background(), vdr, *queryRequestGPID, []ids.ID{parentBlkB.IDV})
	require.NoError(err)

	// Assert that the blocks' statuses are correct.
	// The evicted [parentBlkA] shouldn't be changed.
	require.Equal(choices.Processing, parentBlkA.Status())
	require.Equal(choices.Accepted, parentBlkB.Status())

	vm.BuildBlockF = func() (snowman.Block, error) {
		return childBlk, nil
	}

	sentQuery := new(bool)
	sender.SendPushQueryF = func(context.Context, ids.NodeIDSet, uint32, []byte) {
		*sentQuery = true
	}

	// Should issue a new block and send a query for it.
	err = te.Notify(common.PendingTxs)
	require.NoError(err)
	require.True(*sentQuery)
}
