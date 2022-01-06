// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
	"github.com/stretchr/testify/assert"
)

func TestInnerBlockMappingPostFork(t *testing.T) {
	assert := assert.New(t)
	activationTime := time.Time{} // enable ProBlks
	minPChainHeight := uint64(0)
	innerVM, _, proVM, innerGenBlk, DBManager := initTestProposerVM(t, activationTime, minPChainHeight)

	// build a chain accepting a bunch of blocks
	var (
		blkNumber    = uint64(10)
		prevInnerBlk = snowman.Block(innerGenBlk)
		lastInnerBlk snowman.Block
		innerBlks    = make(map[ids.ID]snowman.Block)
		lastProBlk   snowman.Block
		proBlks      = make(map[ids.ID]snowman.Block)
		err          error
	)

	innerBlks[innerGenBlk.ID()] = innerGenBlk
	innerVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		blk, found := innerBlks[blkID]
		if !found {
			return nil, errUnknownBlock
		}
		return blk, nil
	}
	innerVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		for _, blk := range innerBlks {
			if bytes.Equal(b, blk.Bytes()) {
				return blk, nil
			}
		}
		return nil, errUnknownBlock
	}
	innerVM.LastAcceptedF = func() (ids.ID, error) { return prevInnerBlk.ID(), nil }

	for blkCount := uint64(1); blkCount <= blkNumber; blkCount++ {
		lastInnerBlk = &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{uint8(blkCount)},
			ParentV:    prevInnerBlk.ID(),
			HeightV:    blkCount,
			TimestampV: prevInnerBlk.Timestamp().Add(proposer.MaxDelay),
		}
		innerBlks[lastInnerBlk.ID()] = lastInnerBlk

		innerVM.BuildBlockF = func() (snowman.Block, error) { return lastInnerBlk, nil }
		proVM.Set(proVM.Time().Add(proposer.MaxDelay))
		lastProBlk, err = proVM.BuildBlock()

		assert.NoError(err)
		assert.NoError(proVM.SetPreference(lastProBlk.ID()))
		assert.NoError(lastProBlk.Accept())

		proBlks[lastProBlk.ID()] = lastProBlk
		prevInnerBlk = lastInnerBlk
	}

	// check that mapping is fully built
	for height := uint64(1); height <= blkNumber; height++ {
		_, err := proVM.State.GetBlkIDByHeight(height)
		assert.NoError(err)
	}

	// Entirely delete the mapping to show it gets reconstructed
	for height := uint64(1); height <= blkNumber; height++ {
		assert.NoError(proVM.State.DeleteBlkIDByHeight(height))
	}

	// show repairs rebuilds the mapping
	assert.NoError(proVM.repairInnerBlockMapping())

	// check that mapping is fully built
	for height := uint64(1); height <= blkNumber; height++ {
		_, err := proVM.State.GetBlkIDByHeight(height)
		assert.NoError(err)
	}

	// build a different VM on the same DB to simulated restart
	innerVM.InitializeF = func(*snow.Context, manager.Manager,
		[]byte, []byte, []byte, chan<- common.Message,
		[]*common.Fx, common.AppSender) error {
		return nil
	}
	newProVM := New(innerVM, activationTime, minPChainHeight)
	if err := newProVM.Initialize(
		snow.DefaultContextTest(),
		DBManager, // same DB as previous proVM
		[]byte("genesis state"),
		nil, nil, nil, nil, nil,
	); err != nil {
		t.Fatalf("failed to initialize proposerVM with %s", err)
	}

	// check that mapping is fully built
	for height := uint64(1); height <= blkNumber; height++ {
		_, err := proVM.State.GetBlkIDByHeight(height)
		assert.NoError(err)
	}
}

func TestInnerBlockMappingPreFork(t *testing.T) {
	assert := assert.New(t)
	activationTime := mockable.MaxTime // disable ProBlks
	minPChainHeight := uint64(0)
	innerVM, _, proVM, innerGenBlk, DBManager := initTestProposerVM(t, activationTime, minPChainHeight)

	// build a chain accepting a bunch of blocks
	var (
		blkNumber    = uint64(10)
		prevInnerBlk = snowman.Block(innerGenBlk)
		lastInnerBlk snowman.Block
		innerBlks    = make(map[ids.ID]snowman.Block)
		lastProBlk   snowman.Block
		proBlks      = make(map[ids.ID]snowman.Block)
		err          error
	)

	innerBlks[innerGenBlk.ID()] = innerGenBlk
	innerVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		blk, found := innerBlks[blkID]
		if !found {
			return nil, errUnknownBlock
		}
		return blk, nil
	}
	innerVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		for _, blk := range innerBlks {
			if bytes.Equal(b, blk.Bytes()) {
				return blk, nil
			}
		}
		return nil, errUnknownBlock
	}
	innerVM.LastAcceptedF = func() (ids.ID, error) { return prevInnerBlk.ID(), nil }

	for blkCount := uint64(1); blkCount <= blkNumber; blkCount++ {
		lastInnerBlk = &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{uint8(blkCount)},
			ParentV:    prevInnerBlk.ID(),
			HeightV:    blkCount,
			TimestampV: prevInnerBlk.Timestamp(),
		}
		innerBlks[lastInnerBlk.ID()] = lastInnerBlk

		innerVM.BuildBlockF = func() (snowman.Block, error) { return lastInnerBlk, nil }
		lastProBlk, err = proVM.BuildBlock()

		assert.NoError(err)
		assert.NoError(proVM.SetPreference(lastProBlk.ID()))
		assert.NoError(lastProBlk.Accept())

		proBlks[lastProBlk.ID()] = lastProBlk
		prevInnerBlk = lastInnerBlk
	}

	// mapping should be empty
	for height := uint64(1); height <= blkNumber; height++ {
		_, err := proVM.State.GetBlkIDByHeight(height)
		assert.Error(err, database.ErrNotFound)
	}
	// fork height should track highest accepted preFork block
	assert.True(proVM.latestPreForkHeight == lastProBlk.Height())

	// show repairs rebuilds the mapping
	assert.NoError(proVM.repairInnerBlockMapping())

	// mapping should be empty
	for height := uint64(1); height <= blkNumber; height++ {
		_, err := proVM.State.GetBlkIDByHeight(height)
		assert.Error(err, database.ErrNotFound)
	}
	// fork height should track highest accepted preFork block
	assert.True(proVM.latestPreForkHeight == lastProBlk.Height())

	// build a different VM on the same DB to simulated restart
	innerVM.InitializeF = func(*snow.Context, manager.Manager,
		[]byte, []byte, []byte, chan<- common.Message,
		[]*common.Fx, common.AppSender) error {
		return nil
	}
	newProVM := New(innerVM, activationTime, minPChainHeight)
	if err := newProVM.Initialize(
		snow.DefaultContextTest(),
		DBManager, // same DB as previous proVM
		[]byte("genesis state"),
		nil, nil, nil, nil, nil,
	); err != nil {
		t.Fatalf("failed to initialize proposerVM with %s", err)
	}

	// Show the mapping is there after newProVM initialization
	for height := uint64(1); height <= blkNumber; height++ {
		_, err := proVM.State.GetBlkIDByHeight(height)
		assert.Error(err, database.ErrNotFound)
	}
	// fork height should track highest accepted preFork block
	assert.True(proVM.latestPreForkHeight == lastProBlk.Height())
}

func TestInnerBlockMappingAcrossFork(t *testing.T) {
	assert := assert.New(t)
	activationTime := genesisTimestamp.Add(10 * time.Second)
	minPChainHeight := uint64(0)
	innerVM, _, proVM, innerGenBlk, DBManager := initTestProposerVM(t, activationTime, minPChainHeight)

	// build a chain accepting a bunch of blocks
	var (
		blkNumber    = uint64(10)
		forkHeight   = blkNumber / 2
		prevInnerBlk = snowman.Block(innerGenBlk)
		lastInnerBlk snowman.Block
		innerBlks    = make(map[ids.ID]snowman.Block)
		lastProBlk   snowman.Block
		proBlks      = make(map[ids.ID]snowman.Block)
		err          error
	)

	innerBlks[innerGenBlk.ID()] = innerGenBlk
	innerVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		blk, found := innerBlks[blkID]
		if !found {
			return nil, errUnknownBlock
		}
		return blk, nil
	}
	innerVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		for _, blk := range innerBlks {
			if bytes.Equal(b, blk.Bytes()) {
				return blk, nil
			}
		}
		return nil, errUnknownBlock
	}
	innerVM.LastAcceptedF = func() (ids.ID, error) { return prevInnerBlk.ID(), nil }

	// build preFork blocks first
	for blkCount := uint64(1); blkCount < forkHeight; blkCount++ {
		lastInnerBlk = &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{uint8(blkCount)},
			ParentV:    prevInnerBlk.ID(),
			HeightV:    blkCount,
			TimestampV: prevInnerBlk.Timestamp(),
		}
		innerBlks[lastInnerBlk.ID()] = lastInnerBlk

		innerVM.BuildBlockF = func() (snowman.Block, error) { return lastInnerBlk, nil }
		proVM.Set(genesisTimestamp)
		lastProBlk, err = proVM.BuildBlock()

		assert.NoError(err)
		assert.NoError(proVM.SetPreference(lastProBlk.ID()))
		assert.NoError(lastProBlk.Accept())

		proBlks[lastProBlk.ID()] = lastProBlk
		prevInnerBlk = lastInnerBlk
	}

	// build fork block
	lastInnerBlk = &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{uint8(forkHeight)},
		ParentV:    prevInnerBlk.ID(),
		HeightV:    forkHeight,
		TimestampV: activationTime.Add(time.Second),
	}
	innerBlks[lastInnerBlk.ID()] = lastInnerBlk

	innerVM.BuildBlockF = func() (snowman.Block, error) { return lastInnerBlk, nil }
	proVM.Set(genesisTimestamp)
	lastProBlk, err = proVM.BuildBlock()

	assert.NoError(err)
	assert.NoError(proVM.SetPreference(lastProBlk.ID()))
	assert.NoError(lastProBlk.Accept())

	proBlks[lastProBlk.ID()] = lastProBlk
	prevInnerBlk = lastInnerBlk

	// build postFork blocks then
	proVM.Set(activationTime)
	for blkCount := forkHeight + 1; blkCount <= blkNumber; blkCount++ {
		lastInnerBlk = &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{uint8(blkCount)},
			ParentV:    prevInnerBlk.ID(),
			HeightV:    blkCount,
			TimestampV: prevInnerBlk.Timestamp().Add(proposer.MaxDelay),
		}
		innerBlks[lastInnerBlk.ID()] = lastInnerBlk

		innerVM.BuildBlockF = func() (snowman.Block, error) { return lastInnerBlk, nil }
		proVM.Set(lastInnerBlk.Timestamp().Add(proposer.MaxDelay))
		lastProBlk, err = proVM.BuildBlock()

		assert.NoError(err)
		assert.NoError(proVM.SetPreference(lastProBlk.ID()))
		assert.NoError(lastProBlk.Accept())

		proBlks[lastProBlk.ID()] = lastProBlk
		prevInnerBlk = lastInnerBlk
	}

	// check that mapping is fully built
	assert.True(proVM.latestPreForkHeight == forkHeight)
	for height := uint64(1); height <= blkNumber; height++ {
		_, err := proVM.State.GetBlkIDByHeight(height)
		if height <= forkHeight {
			// preFork blocks should not be in mapping
			assert.Error(err, database.ErrNotFound)
		} else {
			// postFork blocks should be in mapping
			assert.NoError(err)
		}
	}

	// Entirely delete the mapping to show it gets reconstructed
	for height := uint64(1); height <= blkNumber; height++ {
		assert.NoError(proVM.State.DeleteBlkIDByHeight(height))
	}
	proVM.latestPreForkHeight = 0

	// show repairs rebuilds the mapping
	assert.NoError(proVM.repairInnerBlockMapping())

	assert.True(proVM.latestPreForkHeight == forkHeight)
	for height := uint64(1); height <= blkNumber; height++ {
		_, err := proVM.State.GetBlkIDByHeight(height)
		if height <= forkHeight {
			// preFork blocks should not be in mapping
			assert.Error(err, database.ErrNotFound)
		} else {
			// postFork blocks should be in mapping
			assert.NoError(err)
		}
	}

	// build a different VM on the same DB to simulated restart
	innerVM.InitializeF = func(*snow.Context, manager.Manager,
		[]byte, []byte, []byte, chan<- common.Message,
		[]*common.Fx, common.AppSender) error {
		return nil
	}
	newProVM := New(innerVM, activationTime, minPChainHeight)
	if err := newProVM.Initialize(
		snow.DefaultContextTest(),
		DBManager, // same DB as previous proVM
		[]byte("genesis state"),
		nil, nil, nil, nil, nil,
	); err != nil {
		t.Fatalf("failed to initialize proposerVM with %s", err)
	}

	// Show the mapping is there after newProVM initialization
	assert.True(newProVM.latestPreForkHeight == forkHeight)
	for height := uint64(1); height <= blkNumber; height++ {
		_, err := newProVM.State.GetBlkIDByHeight(height)
		if height <= forkHeight {
			// preFork blocks should not be in mapping
			assert.Error(err, database.ErrNotFound)
		} else {
			// postFork blocks should be in mapping
			assert.NoError(err)
		}
	}
}

func TestInnerBlockMappingBackwardCompatiblity(t *testing.T) {
	// say the chain of preFork and postFork blocks is already accepted.
	// Show that repairs can build mapping from scratch.

	assert := assert.New(t)
	activationTime := mockable.MaxTime // disable ProBlks
	minPChainHeight := uint64(0)
	innerVM, _, proVM, innerGenBlk, DBManager := initTestProposerVM(t, activationTime, minPChainHeight)

	// store some preFork blocks
	// build a chain accepting a bunch of blocks
	var (
		blkNumber    = uint64(10)
		forkHeight   = blkNumber / 2
		prevInnerBlk = snowman.Block(innerGenBlk)
		lastInnerBlk snowman.Block
		innerBlks    = make(map[ids.ID]snowman.Block)
		lastProBlk   snowman.Block
		proBlks      = make(map[ids.ID]snowman.Block)
	)

	innerBlks[innerGenBlk.ID()] = innerGenBlk
	innerVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		blk, found := innerBlks[blkID]
		if !found {
			return nil, errUnknownBlock
		}
		return blk, nil
	}
	innerVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		for _, blk := range innerBlks {
			if bytes.Equal(b, blk.Bytes()) {
				return blk, nil
			}
		}
		return nil, errUnknownBlock
	}
	innerVM.LastAcceptedF = func() (ids.ID, error) { return prevInnerBlk.ID(), nil }

	// store preFork blocks first
	for blkCount := uint64(1); blkCount <= forkHeight; blkCount++ {
		lastInnerBlk = &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Accepted, // set status to accepted already
			},
			BytesV:     []byte{uint8(blkCount)},
			ParentV:    prevInnerBlk.ID(),
			HeightV:    blkCount,
			TimestampV: prevInnerBlk.Timestamp(),
		}
		innerBlks[lastInnerBlk.ID()] = lastInnerBlk

		lastProBlk = &preForkBlock{
			Block: lastInnerBlk,
			vm:    proVM,
		}

		proBlks[lastProBlk.ID()] = lastProBlk
		prevInnerBlk = lastInnerBlk
	}

	// store postFork blocks
	for blkCount := forkHeight + 1; blkCount <= blkNumber; blkCount++ {
		lastInnerBlk = &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Accepted, // set status to accepted already
			},
			BytesV:     []byte{uint8(blkCount)},
			ParentV:    prevInnerBlk.ID(),
			HeightV:    blkCount,
			TimestampV: prevInnerBlk.Timestamp(),
		}
		innerBlks[lastInnerBlk.ID()] = lastInnerBlk

		statelessChild, err := block.BuildUnsigned(
			lastProBlk.ID(),
			time.Time{},
			0, /*pChainHeight*/
			lastInnerBlk.Bytes(),
		)
		assert.NoError(err)
		postForkBlk := &postForkBlock{
			SignedBlock: statelessChild,
			postForkCommonComponents: postForkCommonComponents{
				vm:       proVM,
				innerBlk: lastInnerBlk,
				status:   choices.Accepted, // set status to accepted already
			},
		}

		assert.NoError(proVM.storePostForkBlock(postForkBlk))
		assert.NoError(proVM.State.SetLastAccepted(postForkBlk.ID()))

		lastProBlk = postForkBlk
		proBlks[lastProBlk.ID()] = lastProBlk
		prevInnerBlk = lastInnerBlk
	}

	// mapping is currently empty
	assert.True(proVM.latestPreForkHeight == 0)
	for height := uint64(1); height <= blkNumber; height++ {
		_, err := proVM.State.GetBlkIDByHeight(height)
		assert.Error(err, database.ErrNotFound)
	}

	// show repairs builds the mapping
	assert.NoError(proVM.repairInnerBlockMapping())

	assert.True(proVM.latestPreForkHeight == forkHeight)
	for height := uint64(1); height <= blkNumber; height++ {
		_, err := proVM.State.GetBlkIDByHeight(height)
		if height <= forkHeight {
			// preFork blocks should not be in mapping
			assert.Error(err, database.ErrNotFound)
		} else {
			// postFork blocks should be in mapping
			assert.NoError(err)
		}
	}

	// build a different VM on the same DB to simulated restart
	innerVM.InitializeF = func(*snow.Context, manager.Manager,
		[]byte, []byte, []byte, chan<- common.Message,
		[]*common.Fx, common.AppSender) error {
		return nil
	}
	newProVM := New(innerVM, activationTime, minPChainHeight)
	if err := newProVM.Initialize(
		snow.DefaultContextTest(),
		DBManager, // same DB as previous proVM
		[]byte("genesis state"),
		nil, nil, nil, nil, nil,
	); err != nil {
		t.Fatalf("failed to initialize proposerVM with %s", err)
	}

	// Show the mapping is there after newProVM initialization
	assert.True(newProVM.latestPreForkHeight == forkHeight)
	for height := uint64(1); height <= blkNumber; height++ {
		_, err := newProVM.State.GetBlkIDByHeight(height)
		if height <= forkHeight {
			// preFork blocks should not be in mapping
			assert.Error(err, database.ErrNotFound)
		} else {
			// postFork blocks should be in mapping
			assert.NoError(err)
		}
	}
}
