// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"bytes"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"gotest.tools/assert"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/queue"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	snowgetter "github.com/ava-labs/avalanchego/snow/engine/snowman/getter"
	"github.com/ava-labs/avalanchego/snow/validators"
)

var errUnknownBlock = errors.New("unknown block")

func newConfig(t *testing.T) (Config, ids.ShortID, *common.SenderTest, *block.TestVM) {
	ctx := snow.DefaultConsensusContextTest()

	peers := validators.NewSet()
	db := memdb.New()
	sender := &common.SenderTest{}
	vm := &block.TestVM{}

	sender.T = t
	vm.T = t

	sender.Default(true)
	vm.Default(true)

	isBootstrapped := false
	subnet := &common.SubnetTest{
		T:               t,
		IsBootstrappedF: func() bool { return isBootstrapped },
		BootstrappedF:   func(ids.ID) { isBootstrapped = true },
	}

	sender.CantSendGetAcceptedFrontier = false

	peer := ids.GenerateTestShortID()
	if err := peers.AddWeight(peer, 1); err != nil {
		t.Fatal(err)
	}

	blocker, _ := queue.NewWithMissing(db, "", prometheus.NewRegistry())

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

	snowGetHandler, err := snowgetter.New(vm, commonConfig)
	if err != nil {
		t.Fatal(err)
	}

	return Config{
		Config:        commonConfig,
		AllGetsServer: snowGetHandler,
		Blocked:       blocker,
		VM:            vm,
		WeightTracker: tracker.NewWeightTracker(commonConfig.Beacons, commonConfig.StartupAlpha),
	}, peer, sender, vm
}

// Single node in the accepted frontier; no need to fetch parent
func TestBootstrapperSingleFrontier(t *testing.T) {
	config, _, _, vm := newConfig(t)

	blkID0 := ids.Empty.Prefix(0)
	blkID1 := ids.Empty.Prefix(1)

	blkBytes0 := []byte{0}
	blkBytes1 := []byte{1}

	blk0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID0,
			StatusV: choices.Accepted,
		},
		HeightV: 0,
		BytesV:  blkBytes0,
	}
	blk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID1,
			StatusV: choices.Processing,
		},
		ParentV: blk0.IDV,
		HeightV: 1,
		BytesV:  blkBytes1,
	}

	vm.CantLastAccepted = false
	vm.LastAcceptedF = func() (ids.ID, error) { return blk0.ID(), nil }
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		assert.Equal(t, blk0.ID(), blkID)
		return blk0, nil
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

	acceptedIDs := []ids.ID{blkID1}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case blkID1:
			return blk1, nil
		case blkID0:
			return blk0, nil
		default:
			t.Fatal(errUnknownBlock)
			panic(errUnknownBlock)
		}
	}
	vm.ParseBlockF = func(blkBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(blkBytes, blkBytes1):
			return blk1, nil
		case bytes.Equal(blkBytes, blkBytes0):
			return blk0, nil
		}
		t.Fatal(errUnknownBlock)
		return nil, errUnknownBlock
	}

	vm.CantSetState = false
	err = bs.ForceAccepted(acceptedIDs)
	switch {
	case err != nil: // should finish
		t.Fatal(err)
	case config.Ctx.GetState() != snow.NormalOp:
		t.Fatalf("Bootstrapping should have finished")
	case blk1.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	}
}

// Requests the unknown block and gets back a Ancestors with unexpected request ID.
// Requests again and gets response from unexpected peer.
// Requests again and gets an unexpected block.
// Requests again and gets the expected block.
func TestBootstrapperUnknownByzantineResponse(t *testing.T) {
	config, peerID, sender, vm := newConfig(t)

	blkID0 := ids.Empty.Prefix(0)
	blkID1 := ids.Empty.Prefix(1)
	blkID2 := ids.Empty.Prefix(2)

	blkBytes0 := []byte{0}
	blkBytes1 := []byte{1}
	blkBytes2 := []byte{2}

	blk0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID0,
			StatusV: choices.Accepted,
		},
		HeightV: 0,
		BytesV:  blkBytes0,
	}
	blk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID1,
			StatusV: choices.Unknown,
		},
		ParentV: blk0.IDV,
		HeightV: 1,
		BytesV:  blkBytes1,
	}
	blk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID2,
			StatusV: choices.Processing,
		},
		ParentV: blk1.IDV,
		HeightV: 2,
		BytesV:  blkBytes2,
	}

	vm.CantLastAccepted = false
	vm.LastAcceptedF = func() (ids.ID, error) { return blk0.ID(), nil }
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		assert.Equal(t, blk0.ID(), blkID)
		return blk0, nil
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

	acceptedIDs := []ids.ID{blkID2}

	parsedBlk1 := false
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case blkID0:
			return blk0, nil
		case blkID1:
			if parsedBlk1 {
				return blk1, nil
			}
			return nil, errUnknownBlock
		case blkID2:
			return blk2, nil
		default:
			t.Fatal(errUnknownBlock)
			panic(errUnknownBlock)
		}
	}
	vm.ParseBlockF = func(blkBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(blkBytes, blkBytes0):
			return blk0, nil
		case bytes.Equal(blkBytes, blkBytes1):
			blk1.StatusV = choices.Processing
			parsedBlk1 = true
			return blk1, nil
		case bytes.Equal(blkBytes, blkBytes2):
			return blk2, nil
		}
		t.Fatal(errUnknownBlock)
		return nil, errUnknownBlock
	}

	requestID := new(uint32)
	sender.SendGetAncestorsF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if vdr != peerID {
			t.Fatalf("Should have requested block from %s, requested from %s", peerID, vdr)
		}
		switch {
		case vtxID == blkID1:
		default:
			t.Fatalf("should have requested blk1")
		}
		*requestID = reqID
	}

	vm.CantSetState = false
	if err := bs.ForceAccepted(acceptedIDs); err != nil { // should request blk1
		t.Fatal(err)
	}

	oldReqID := *requestID
	if err := bs.Ancestors(peerID, *requestID+1, [][]byte{blkBytes1}); err != nil { // respond with wrong request ID
		t.Fatal(err)
	} else if oldReqID != *requestID {
		t.Fatal("should not have sent new request")
	}

	if err := bs.Ancestors(ids.ShortID{1, 2, 3}, *requestID, [][]byte{blkBytes1}); err != nil { // respond from wrong peer
		t.Fatal(err)
	} else if oldReqID != *requestID {
		t.Fatal("should not have sent new request")
	}

	if err := bs.Ancestors(peerID, *requestID, [][]byte{blkBytes0}); err != nil { // respond with wrong block
		t.Fatal(err)
	} else if oldReqID == *requestID {
		t.Fatal("should have sent new request")
	}

	err = bs.Ancestors(peerID, *requestID, [][]byte{blkBytes1})
	switch {
	case err != nil: // respond with right block
		t.Fatal(err)
	case config.Ctx.GetState() != snow.NormalOp:
		t.Fatalf("Bootstrapping should have finished")
	case blk0.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk1.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk2.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	}
}

// There are multiple needed blocks and Ancestors returns one at a time
func TestBootstrapperPartialFetch(t *testing.T) {
	config, peerID, sender, vm := newConfig(t)

	blkID0 := ids.Empty.Prefix(0)
	blkID1 := ids.Empty.Prefix(1)
	blkID2 := ids.Empty.Prefix(2)
	blkID3 := ids.Empty.Prefix(3)

	blkBytes0 := []byte{0}
	blkBytes1 := []byte{1}
	blkBytes2 := []byte{2}
	blkBytes3 := []byte{3}

	blk0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID0,
			StatusV: choices.Accepted,
		},
		HeightV: 0,
		BytesV:  blkBytes0,
	}
	blk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID1,
			StatusV: choices.Unknown,
		},
		ParentV: blk0.IDV,
		HeightV: 1,
		BytesV:  blkBytes1,
	}
	blk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID2,
			StatusV: choices.Unknown,
		},
		ParentV: blk1.IDV,
		HeightV: 2,
		BytesV:  blkBytes2,
	}
	blk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID3,
			StatusV: choices.Processing,
		},
		ParentV: blk2.IDV,
		HeightV: 3,
		BytesV:  blkBytes3,
	}

	vm.CantLastAccepted = false
	vm.LastAcceptedF = func() (ids.ID, error) { return blk0.ID(), nil }
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		assert.Equal(t, blk0.ID(), blkID)
		return blk0, nil
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

	acceptedIDs := []ids.ID{blkID3}

	parsedBlk1 := false
	parsedBlk2 := false
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case blkID0:
			return blk0, nil
		case blkID1:
			if parsedBlk1 {
				return blk1, nil
			}
			return nil, errUnknownBlock
		case blkID2:
			if parsedBlk2 {
				return blk2, nil
			}
			return nil, errUnknownBlock
		case blkID3:
			return blk3, nil
		default:
			t.Fatal(errUnknownBlock)
			panic(errUnknownBlock)
		}
	}
	vm.ParseBlockF = func(blkBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(blkBytes, blkBytes0):
			return blk0, nil
		case bytes.Equal(blkBytes, blkBytes1):
			blk1.StatusV = choices.Processing
			parsedBlk1 = true
			return blk1, nil
		case bytes.Equal(blkBytes, blkBytes2):
			blk2.StatusV = choices.Processing
			parsedBlk2 = true
			return blk2, nil
		case bytes.Equal(blkBytes, blkBytes3):
			return blk3, nil
		}
		t.Fatal(errUnknownBlock)
		return nil, errUnknownBlock
	}

	requestID := new(uint32)
	requested := ids.Empty
	sender.SendGetAncestorsF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if vdr != peerID {
			t.Fatalf("Should have requested block from %s, requested from %s", peerID, vdr)
		}
		switch vtxID {
		case blkID1, blkID2:
		default:
			t.Fatalf("should have requested blk1 or blk2")
		}
		*requestID = reqID
		requested = vtxID
	}

	vm.CantSetState = false
	if err := bs.ForceAccepted(acceptedIDs); err != nil { // should request blk2
		t.Fatal(err)
	}

	if err := bs.Ancestors(peerID, *requestID, [][]byte{blkBytes2}); err != nil { // respond with blk2
		t.Fatal(err)
	} else if requested != blkID1 {
		t.Fatal("should have requested blk1")
	}

	if err := bs.Ancestors(peerID, *requestID, [][]byte{blkBytes1}); err != nil { // respond with blk1
		t.Fatal(err)
	} else if requested != blkID1 {
		t.Fatal("should not have requested another block")
	}

	switch {
	case config.Ctx.GetState() != snow.NormalOp:
		t.Fatalf("Bootstrapping should have finished")
	case blk0.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk1.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk2.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	}
}

// There are multiple needed blocks and Ancestors returns all at once
func TestBootstrapperAncestors(t *testing.T) {
	config, peerID, sender, vm := newConfig(t)

	blkID0 := ids.Empty.Prefix(0)
	blkID1 := ids.Empty.Prefix(1)
	blkID2 := ids.Empty.Prefix(2)
	blkID3 := ids.Empty.Prefix(3)

	blkBytes0 := []byte{0}
	blkBytes1 := []byte{1}
	blkBytes2 := []byte{2}
	blkBytes3 := []byte{3}

	blk0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID0,
			StatusV: choices.Accepted,
		},
		HeightV: 0,
		BytesV:  blkBytes0,
	}
	blk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID1,
			StatusV: choices.Unknown,
		},
		ParentV: blk0.IDV,
		HeightV: 1,
		BytesV:  blkBytes1,
	}
	blk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID2,
			StatusV: choices.Unknown,
		},
		ParentV: blk1.IDV,
		HeightV: 2,
		BytesV:  blkBytes2,
	}
	blk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID3,
			StatusV: choices.Processing,
		},
		ParentV: blk2.IDV,
		HeightV: 3,
		BytesV:  blkBytes3,
	}

	vm.CantSetState = false
	vm.CantLastAccepted = false
	vm.LastAcceptedF = func() (ids.ID, error) { return blk0.ID(), nil }
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		assert.Equal(t, blk0.ID(), blkID)
		return blk0, nil
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

	acceptedIDs := []ids.ID{blkID3}

	parsedBlk1 := false
	parsedBlk2 := false
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case blkID0:
			return blk0, nil
		case blkID1:
			if parsedBlk1 {
				return blk1, nil
			}
			return nil, errUnknownBlock
		case blkID2:
			if parsedBlk2 {
				return blk2, nil
			}
			return nil, errUnknownBlock
		case blkID3:
			return blk3, nil
		default:
			t.Fatal(errUnknownBlock)
			panic(errUnknownBlock)
		}
	}
	vm.ParseBlockF = func(blkBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(blkBytes, blkBytes0):
			return blk0, nil
		case bytes.Equal(blkBytes, blkBytes1):
			blk1.StatusV = choices.Processing
			parsedBlk1 = true
			return blk1, nil
		case bytes.Equal(blkBytes, blkBytes2):
			blk2.StatusV = choices.Processing
			parsedBlk2 = true
			return blk2, nil
		case bytes.Equal(blkBytes, blkBytes3):
			return blk3, nil
		}
		t.Fatal(errUnknownBlock)
		return nil, errUnknownBlock
	}

	requestID := new(uint32)
	requested := ids.Empty
	sender.SendGetAncestorsF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if vdr != peerID {
			t.Fatalf("Should have requested block from %s, requested from %s", peerID, vdr)
		}
		switch vtxID {
		case blkID1, blkID2:
		default:
			t.Fatalf("should have requested blk1 or blk2")
		}
		*requestID = reqID
		requested = vtxID
	}

	if err := bs.ForceAccepted(acceptedIDs); err != nil { // should request blk2
		t.Fatal(err)
	}

	if err := bs.Ancestors(peerID, *requestID, [][]byte{blkBytes2, blkBytes1}); err != nil { // respond with blk2 and blk1
		t.Fatal(err)
	} else if requested != blkID2 {
		t.Fatal("should not have requested another block")
	}

	switch {
	case config.Ctx.GetState() != snow.NormalOp:
		t.Fatalf("Bootstrapping should have finished")
	case blk0.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk1.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk2.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	}
}

func TestBootstrapperFinalized(t *testing.T) {
	config, peerID, sender, vm := newConfig(t)

	blkID0 := ids.Empty.Prefix(0)
	blkID1 := ids.Empty.Prefix(1)
	blkID2 := ids.Empty.Prefix(2)

	blkBytes0 := []byte{0}
	blkBytes1 := []byte{1}
	blkBytes2 := []byte{2}

	blk0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID0,
			StatusV: choices.Accepted,
		},
		HeightV: 0,
		BytesV:  blkBytes0,
	}
	blk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID1,
			StatusV: choices.Unknown,
		},
		ParentV: blk0.IDV,
		HeightV: 1,
		BytesV:  blkBytes1,
	}
	blk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID2,
			StatusV: choices.Unknown,
		},
		ParentV: blk1.IDV,
		HeightV: 2,
		BytesV:  blkBytes2,
	}

	vm.CantLastAccepted = false
	vm.LastAcceptedF = func() (ids.ID, error) { return blk0.ID(), nil }
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		assert.Equal(t, blk0.ID(), blkID)
		return blk0, nil
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

	parsedBlk1 := false
	parsedBlk2 := false
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case blkID0:
			return blk0, nil
		case blkID1:
			if parsedBlk1 {
				return blk1, nil
			}
			return nil, errUnknownBlock
		case blkID2:
			if parsedBlk2 {
				return blk2, nil
			}
			return nil, errUnknownBlock
		default:
			t.Fatal(errUnknownBlock)
			panic(errUnknownBlock)
		}
	}
	vm.ParseBlockF = func(blkBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(blkBytes, blkBytes0):
			return blk0, nil
		case bytes.Equal(blkBytes, blkBytes1):
			blk1.StatusV = choices.Processing
			parsedBlk1 = true
			return blk1, nil
		case bytes.Equal(blkBytes, blkBytes2):
			blk2.StatusV = choices.Processing
			parsedBlk2 = true
			return blk2, nil
		}
		t.Fatal(errUnknownBlock)
		return nil, errUnknownBlock
	}

	requestIDs := map[ids.ID]uint32{}
	sender.SendGetAncestorsF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if vdr != peerID {
			t.Fatalf("Should have requested block from %s, requested from %s", peerID, vdr)
		}
		requestIDs[vtxID] = reqID
	}

	vm.CantSetState = false
	if err := bs.ForceAccepted([]ids.ID{blkID1, blkID2}); err != nil { // should request blk2 and blk1
		t.Fatal(err)
	}

	reqIDBlk2, ok := requestIDs[blkID2]
	if !ok {
		t.Fatalf("should have requested blk2")
	}

	if err := bs.Ancestors(peerID, reqIDBlk2, [][]byte{blkBytes2, blkBytes1}); err != nil {
		t.Fatal(err)
	}

	switch {
	case config.Ctx.GetState() != snow.NormalOp:
		t.Fatalf("Bootstrapping should have finished")
	case blk0.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk1.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk2.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	}
}

func TestRestartBootstrapping(t *testing.T) {
	config, peerID, sender, vm := newConfig(t)

	blkID0 := ids.Empty.Prefix(0)
	blkID1 := ids.Empty.Prefix(1)
	blkID2 := ids.Empty.Prefix(2)
	blkID3 := ids.Empty.Prefix(3)
	blkID4 := ids.Empty.Prefix(4)

	blkBytes0 := []byte{0}
	blkBytes1 := []byte{1}
	blkBytes2 := []byte{2}
	blkBytes3 := []byte{3}
	blkBytes4 := []byte{4}

	blk0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID0,
			StatusV: choices.Accepted,
		},
		HeightV: 0,
		BytesV:  blkBytes0,
	}
	blk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID1,
			StatusV: choices.Unknown,
		},
		ParentV: blk0.IDV,
		HeightV: 1,
		BytesV:  blkBytes1,
	}
	blk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID2,
			StatusV: choices.Unknown,
		},
		ParentV: blk1.IDV,
		HeightV: 2,
		BytesV:  blkBytes2,
	}
	blk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID3,
			StatusV: choices.Unknown,
		},
		ParentV: blk2.IDV,
		HeightV: 3,
		BytesV:  blkBytes3,
	}
	blk4 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID4,
			StatusV: choices.Unknown,
		},
		ParentV: blk3.IDV,
		HeightV: 4,
		BytesV:  blkBytes4,
	}

	vm.CantLastAccepted = false
	vm.LastAcceptedF = func() (ids.ID, error) { return blk0.ID(), nil }
	parsedBlk1 := false
	parsedBlk2 := false
	parsedBlk3 := false
	parsedBlk4 := false
	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case blkID0:
			return blk0, nil
		case blkID1:
			if parsedBlk1 {
				return blk1, nil
			}
			return nil, errUnknownBlock
		case blkID2:
			if parsedBlk2 {
				return blk2, nil
			}
			return nil, errUnknownBlock
		case blkID3:
			if parsedBlk3 {
				return blk3, nil
			}
			return nil, errUnknownBlock
		case blkID4:
			if parsedBlk4 {
				return blk4, nil
			}
			return nil, errUnknownBlock
		default:
			t.Fatal(errUnknownBlock)
			panic(errUnknownBlock)
		}
	}
	vm.ParseBlockF = func(blkBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(blkBytes, blkBytes0):
			return blk0, nil
		case bytes.Equal(blkBytes, blkBytes1):
			blk1.StatusV = choices.Processing
			parsedBlk1 = true
			return blk1, nil
		case bytes.Equal(blkBytes, blkBytes2):
			blk2.StatusV = choices.Processing
			parsedBlk2 = true
			return blk2, nil
		case bytes.Equal(blkBytes, blkBytes3):
			blk3.StatusV = choices.Processing
			parsedBlk3 = true
			return blk3, nil
		case bytes.Equal(blkBytes, blkBytes4):
			blk4.StatusV = choices.Processing
			parsedBlk4 = true
			return blk4, nil
		}
		t.Fatal(errUnknownBlock)
		return nil, errUnknownBlock
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

	requestIDs := map[ids.ID]uint32{}
	sender.SendGetAncestorsF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if vdr != peerID {
			t.Fatalf("Should have requested block from %s, requested from %s", peerID, vdr)
		}
		requestIDs[vtxID] = reqID
	}

	vm.CantSetState = false

	// Force Accept blk3
	if err := bs.ForceAccepted([]ids.ID{blkID3}); err != nil { // should request blk3
		t.Fatal(err)
	}

	reqID, ok := requestIDs[blkID3]
	if !ok {
		t.Fatalf("should have requested blk3")
	}

	if err := bs.Ancestors(peerID, reqID, [][]byte{blkBytes3, blkBytes2}); err != nil {
		t.Fatal(err)
	}

	if _, ok := requestIDs[blkID1]; !ok {
		t.Fatal("should have requested blk1")
	}

	// Remove request, so we can restart bootstrapping via ForceAccepted
	if removed := bs.OutstandingRequests.RemoveAny(blkID1); !removed {
		t.Fatal("Expected to find an outstanding request for blk1")
	}
	requestIDs = map[ids.ID]uint32{}

	if err := bs.ForceAccepted([]ids.ID{blkID4}); err != nil {
		t.Fatal(err)
	}

	blk1RequestID, ok := requestIDs[blkID1]
	if !ok {
		t.Fatal("should have re-requested blk1 on restart")
	}
	blk4RequestID, ok := requestIDs[blkID4]
	if !ok {
		t.Fatal("should have requested blk4 as new accepted frontier")
	}

	if err := bs.Ancestors(peerID, blk1RequestID, [][]byte{blkBytes1}); err != nil {
		t.Fatal(err)
	}

	if config.Ctx.GetState() == snow.NormalOp {
		t.Fatal("Bootstrapping should not have finished with outstanding request for blk4")
	}

	if err := bs.Ancestors(peerID, blk4RequestID, [][]byte{blkBytes4}); err != nil {
		t.Fatal(err)
	}

	switch {
	case config.Ctx.GetState() != snow.NormalOp:
		t.Fatalf("Bootstrapping should have finished")
	case blk0.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk1.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk2.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk3.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	case blk4.Status() != choices.Accepted:
		t.Fatalf("Block should be accepted")
	}
}
