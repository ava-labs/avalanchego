// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/engine/common/queue"
	"github.com/ava-labs/gecko/snow/networking/router"
	"github.com/ava-labs/gecko/snow/networking/timeout"
	"github.com/ava-labs/gecko/snow/validators"
)

var (
	errUnknownBlock = errors.New("unknown block")
)

func newConfig(t *testing.T) (BootstrapConfig, ids.ShortID, *common.SenderTest, *VMTest) {
	ctx := snow.DefaultContextTest()

	peers := validators.NewSet()
	db := memdb.New()
	sender := &common.SenderTest{}
	vm := &VMTest{}
	engine := &Transitive{}
	handler := &router.Handler{}
	router := &router.ChainRouter{}
	timeouts := &timeout.Manager{}

	sender.T = t
	vm.T = t

	sender.Default(true)
	vm.Default(true)

	sender.CantGetAcceptedFrontier = false

	peer := validators.GenerateRandomValidator(1)
	peerID := peer.ID()
	peers.Add(peer)

	handler.Initialize(engine, make(chan common.Message), 1)
	timeouts.Initialize(0)
	router.Initialize(ctx.Log, timeouts, time.Hour, time.Second)

	blocker, _ := queue.New(db)

	commonConfig := common.Config{
		Context:    ctx,
		Validators: peers,
		Beacons:    peers,
		Alpha:      uint64(peers.Len()/2 + 1),
		Sender:     sender,
	}
	return BootstrapConfig{
		Config:  commonConfig,
		Blocked: blocker,
		VM:      vm,
	}, peerID, sender, vm
}

func TestBootstrapperSingleFrontier(t *testing.T) {
	config, peerID, sender, vm := newConfig(t)

	blkID0 := ids.Empty.Prefix(0)
	blkID1 := ids.Empty.Prefix(1)

	blkBytes0 := []byte{0}
	blkBytes1 := []byte{1}

	blk0 := &Blk{
		id:     blkID0,
		height: 0,
		status: choices.Accepted,
		bytes:  blkBytes0,
	}
	blk1 := &Blk{
		parent: blk0,
		id:     blkID1,
		height: 1,
		status: choices.Processing,
		bytes:  blkBytes1,
	}

	bs := bootstrapper{}
	bs.metrics.Initialize(config.Context.Log, fmt.Sprintf("gecko_%s", config.Context.ChainID), prometheus.NewRegistry())
	bs.Initialize(config)

	acceptedIDs := ids.Set{}
	acceptedIDs.Add(blkID1)

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch {
		case blkID.Equals(blkID1):
			return nil, errUnknownBlock
		default:
			t.Fatal(errUnknownBlock)
			panic(errUnknownBlock)
		}
	}

	reqID := new(uint32)
	sender.GetF = func(vdr ids.ShortID, innerReqID uint32, blkID ids.ID) {
		if !vdr.Equals(peerID) {
			t.Fatalf("Should have requested block from %s, requested from %s", peerID, vdr)
		}
		switch {
		case blkID.Equals(blkID1):
		default:
			t.Fatalf("Requested unknown vertex")
		}

		*reqID = innerReqID
	}
	vm.CantBootstrapping = false

	bs.ForceAccepted(acceptedIDs)

	vm.GetBlockF = nil
	sender.GetF = nil
	vm.CantBootstrapping = true

	vm.ParseBlockF = func(blkBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(blkBytes, blkBytes1):
			return blk1, nil
		}
		t.Fatal(errUnknownBlock)
		return nil, errUnknownBlock
	}

	finished := new(bool)
	bs.onFinished = func() error { *finished = true; return nil }
	vm.CantBootstrapped = false

	bs.Put(peerID, *reqID, blkID1, blkBytes1)

	vm.ParseBlockF = nil
	bs.onFinished = nil
	vm.CantBootstrapped = true

	if !*finished {
		t.Fatalf("Bootstrapping should have finished")
	}
	if blk1.Status() != choices.Accepted {
		t.Fatalf("Block should be accepted")
	}
}

func TestBootstrapperUnknownByzantineResponse(t *testing.T) {
	config, peerID, sender, vm := newConfig(t)

	blkID0 := ids.Empty.Prefix(0)
	blkID1 := ids.Empty.Prefix(1)
	blkID2 := ids.Empty.Prefix(2)

	blkBytes0 := []byte{0}
	blkBytes1 := []byte{1}
	blkBytes2 := []byte{2}

	blk0 := &Blk{
		id:     blkID0,
		height: 0,
		status: choices.Accepted,
		bytes:  blkBytes0,
	}
	blk1 := &Blk{
		parent: blk0,
		id:     blkID1,
		height: 1,
		status: choices.Processing,
		bytes:  blkBytes1,
	}
	blk2 := &Blk{
		parent: blk1,
		id:     blkID2,
		height: 2,
		status: choices.Processing,
		bytes:  blkBytes2,
	}

	bs := bootstrapper{}
	bs.metrics.Initialize(config.Context.Log, fmt.Sprintf("gecko_%s", config.Context.ChainID), prometheus.NewRegistry())
	bs.Initialize(config)

	acceptedIDs := ids.Set{}
	acceptedIDs.Add(blkID1)

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch {
		case blkID.Equals(blkID1):
			return nil, errUnknownBlock
		default:
			t.Fatal(errUnknownBlock)
			panic(errUnknownBlock)
		}
	}

	requestID := new(uint32)
	sender.GetF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if !vdr.Equals(peerID) {
			t.Fatalf("Should have requested block from %s, requested from %s", peerID, vdr)
		}
		switch {
		case vtxID.Equals(blkID1):
		default:
			t.Fatalf("Requested unknown block")
		}

		*requestID = reqID
	}
	vm.CantBootstrapping = false

	bs.ForceAccepted(acceptedIDs)

	vm.GetBlockF = nil
	vm.CantBootstrapping = true

	vm.ParseBlockF = func(blkBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(blkBytes, blkBytes1):
			return blk1, nil
		case bytes.Equal(blkBytes, blkBytes2):
			return blk2, nil
		}
		t.Fatal(errUnknownBlock)
		return nil, errUnknownBlock
	}

	finished := new(bool)
	bs.onFinished = func() error { *finished = true; return nil }
	vm.CantBootstrapped = false

	bs.Put(peerID, *requestID, blkID2, blkBytes2)
	bs.Put(peerID, *requestID, blkID1, blkBytes1)

	vm.ParseBlockF = nil
	vm.CantBootstrapped = true

	if !*finished {
		t.Fatalf("Bootstrapping should have finished")
	}
	if blk1.Status() != choices.Accepted {
		t.Fatalf("Block should be accepted")
	}
	if blk2.Status() != choices.Processing {
		t.Fatalf("Block should be processing")
	}
}

func TestBootstrapperDependency(t *testing.T) {
	config, peerID, sender, vm := newConfig(t)

	blkID0 := ids.Empty.Prefix(0)
	blkID1 := ids.Empty.Prefix(1)
	blkID2 := ids.Empty.Prefix(2)

	blkBytes0 := []byte{0}
	blkBytes1 := []byte{1}
	blkBytes2 := []byte{2}

	blk0 := &Blk{
		id:     blkID0,
		height: 0,
		status: choices.Accepted,
		bytes:  blkBytes0,
	}
	blk1 := &Blk{
		parent: blk0,
		id:     blkID1,
		height: 1,
		status: choices.Unknown,
		bytes:  blkBytes1,
	}
	blk2 := &Blk{
		parent: blk1,
		id:     blkID2,
		height: 2,
		status: choices.Processing,
		bytes:  blkBytes2,
	}

	bs := bootstrapper{}
	bs.metrics.Initialize(config.Context.Log, fmt.Sprintf("gecko_%s", config.Context.ChainID), prometheus.NewRegistry())
	bs.Initialize(config)

	acceptedIDs := ids.Set{}
	acceptedIDs.Add(blkID2)

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch {
		case blkID.Equals(blkID2):
			return blk2, nil
		default:
			t.Fatalf("Requested unknown block")
			panic("Requested unknown block")
		}
	}

	requestID := new(uint32)
	sender.GetF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if !vdr.Equals(peerID) {
			t.Fatalf("Should have requested block from %s, requested from %s", peerID, vdr)
		}
		switch {
		case vtxID.Equals(blkID1):
		default:
			t.Fatalf("Requested unknown block")
		}

		*requestID = reqID
	}
	vm.CantBootstrapping = false

	bs.ForceAccepted(acceptedIDs)

	vm.GetBlockF = nil
	sender.GetF = nil
	vm.CantBootstrapping = true

	vm.ParseBlockF = func(blkBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(blkBytes, blkBytes1):
			return blk1, nil
		case bytes.Equal(blkBytes, blkBytes2):
			return blk2, nil
		}
		t.Fatal(errUnknownBlock)
		return nil, errUnknownBlock
	}

	blk1.status = choices.Processing

	finished := new(bool)
	bs.onFinished = func() error { *finished = true; return nil }
	vm.CantBootstrapped = false

	bs.Put(peerID, *requestID, blkID1, blkBytes1)

	if !*finished {
		t.Fatalf("Bootstrapping should have finished")
	}
	if blk1.Status() != choices.Accepted {
		t.Fatalf("Block should be accepted")
	}
	if blk2.Status() != choices.Accepted {
		t.Fatalf("Block should be accepted")
	}
}

func TestBootstrapperAcceptedFrontier(t *testing.T) {
	config, _, _, vm := newConfig(t)

	blkID := GenerateID()

	bs := bootstrapper{}
	bs.metrics.Initialize(config.Context.Log, fmt.Sprintf("gecko_%s", config.Context.ChainID), prometheus.NewRegistry())
	bs.Initialize(config)

	vm.LastAcceptedF = func() ids.ID { return blkID }

	accepted := bs.CurrentAcceptedFrontier()

	if accepted.Len() != 1 {
		t.Fatalf("Only one block should be accepted")
	}
	if !accepted.Contains(blkID) {
		t.Fatalf("Blk should be accepted")
	}
}

func TestBootstrapperFilterAccepted(t *testing.T) {
	config, _, _, vm := newConfig(t)

	blkID0 := GenerateID()
	blkID1 := GenerateID()
	blkID2 := GenerateID()

	blk0 := &Blk{
		id:     blkID0,
		status: choices.Accepted,
	}
	blk1 := &Blk{
		id:     blkID1,
		status: choices.Accepted,
	}

	bs := bootstrapper{}
	bs.metrics.Initialize(config.Context.Log, fmt.Sprintf("gecko_%s", config.Context.ChainID), prometheus.NewRegistry())
	bs.Initialize(config)

	blkIDs := ids.Set{}
	blkIDs.Add(
		blkID0,
		blkID1,
		blkID2,
	)

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch {
		case blkID.Equals(blkID0):
			return blk0, nil
		case blkID.Equals(blkID1):
			return blk1, nil
		case blkID.Equals(blkID2):
			return nil, errUnknownBlock
		}
		t.Fatal(errUnknownBlock)
		return nil, errUnknownBlock
	}
	vm.CantBootstrapping = false

	accepted := bs.FilterAccepted(blkIDs)

	if accepted.Len() != 2 {
		t.Fatalf("Two blocks should be accepted")
	}
	if !accepted.Contains(blkID0) {
		t.Fatalf("Blk should be accepted")
	}
	if !accepted.Contains(blkID1) {
		t.Fatalf("Blk should be accepted")
	}
	if accepted.Contains(blkID2) {
		t.Fatalf("Blk shouldn't be accepted")
	}
}

func TestBootstrapperPartialFetch(t *testing.T) {
	config, _, sender, vm := newConfig(t)

	blkID0 := ids.Empty.Prefix(0)
	blkID1 := ids.Empty.Prefix(1)

	blkBytes0 := []byte{0}

	blk0 := &Blk{
		id:     blkID0,
		height: 0,
		status: choices.Accepted,
		bytes:  blkBytes0,
	}

	bs := bootstrapper{}
	bs.metrics.Initialize(config.Context.Log, fmt.Sprintf("gecko_%s", config.Context.ChainID), prometheus.NewRegistry())
	bs.Initialize(config)

	acceptedIDs := ids.Set{}
	acceptedIDs.Add(
		blkID0,
		blkID1,
	)

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch {
		case blkID.Equals(blkID0):
			return blk0, nil
		case blkID.Equals(blkID1):
			return nil, errUnknownBlock
		default:
			t.Fatal(errUnknownBlock)
			panic(errUnknownBlock)
		}
	}

	sender.CantGet = false
	bs.onFinished = func() error { return nil }
	vm.CantBootstrapping = false

	bs.ForceAccepted(acceptedIDs)

	if bs.finished {
		t.Fatalf("should have requested a block")
	}

	if bs.pending.Len() != 1 {
		t.Fatalf("wrong number pending")
	}
}

func TestBootstrapperWrongIDByzantineResponse(t *testing.T) {
	config, peerID, sender, vm := newConfig(t)

	blkID0 := ids.Empty.Prefix(0)
	blkID1 := ids.Empty.Prefix(1)
	blkID2 := ids.Empty.Prefix(2)

	blkBytes0 := []byte{0}
	blkBytes1 := []byte{1}
	blkBytes2 := []byte{2}

	blk0 := &Blk{
		id:     blkID0,
		height: 0,
		status: choices.Accepted,
		bytes:  blkBytes0,
	}
	blk1 := &Blk{
		parent: blk0,
		id:     blkID1,
		height: 1,
		status: choices.Processing,
		bytes:  blkBytes1,
	}
	blk2 := &Blk{
		parent: blk1,
		id:     blkID2,
		height: 2,
		status: choices.Processing,
		bytes:  blkBytes2,
	}

	bs := bootstrapper{}
	bs.metrics.Initialize(config.Context.Log, fmt.Sprintf("gecko_%s", config.Context.ChainID), prometheus.NewRegistry())
	bs.Initialize(config)

	acceptedIDs := ids.Set{}
	acceptedIDs.Add(blkID1)

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch {
		case blkID.Equals(blkID1):
			return nil, errUnknownBlock
		default:
			t.Fatal(errUnknownBlock)
			panic(errUnknownBlock)
		}
	}

	requestID := new(uint32)
	sender.GetF = func(vdr ids.ShortID, reqID uint32, vtxID ids.ID) {
		if !vdr.Equals(peerID) {
			t.Fatalf("Should have requested block from %s, requested from %s", peerID, vdr)
		}
		switch {
		case vtxID.Equals(blkID1):
		default:
			t.Fatalf("Requested unknown block")
		}

		*requestID = reqID
	}
	vm.CantBootstrapping = false

	bs.ForceAccepted(acceptedIDs)

	vm.GetBlockF = nil
	sender.GetF = nil
	vm.CantBootstrapping = true

	vm.ParseBlockF = func(blkBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(blkBytes, blkBytes2):
			return blk2, nil
		}
		t.Fatal(errUnknownBlock)
		return nil, errUnknownBlock
	}

	sender.CantGet = false

	bs.Put(peerID, *requestID, blkID1, blkBytes2)

	sender.CantGet = true

	vm.ParseBlockF = func(blkBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(blkBytes, blkBytes1):
			return blk1, nil
		}
		t.Fatal(errUnknownBlock)
		return nil, errUnknownBlock
	}

	finished := new(bool)
	bs.onFinished = func() error { *finished = true; return nil }
	vm.CantBootstrapped = false

	bs.Put(peerID, *requestID, blkID1, blkBytes1)

	vm.ParseBlockF = nil
	vm.CantBootstrapped = true

	if !*finished {
		t.Fatalf("Bootstrapping should have finished")
	}
	if blk1.Status() != choices.Accepted {
		t.Fatalf("Block should be accepted")
	}
	if blk2.Status() != choices.Processing {
		t.Fatalf("Block should be processing")
	}
}
