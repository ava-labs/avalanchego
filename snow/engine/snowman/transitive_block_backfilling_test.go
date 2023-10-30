// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

func TestGetAncestorsRequestIssuedIfBlockBackfillingIsEnabled(t *testing.T) {
	require := require.New(t)

	engCfg, vm, sender, err := setupBlockBackfillingTests(t)
	require.NoError(err)

	// create the engine
	te, err := newTransitive(engCfg)
	require.NoError(err)

	// enable block backfilling and check blocks request starts with block provided by VM
	reqBlk := ids.GenerateTestID()
	vm.BackfillBlocksEnabledF = func(ctx context.Context) (ids.ID, error) {
		return reqBlk, nil
	}

	var issuedBlkID ids.ID
	sender.SendGetAncestorsF = func(ctx context.Context, ni ids.NodeID, u uint32, blkID ids.ID) {
		issuedBlkID = blkID
	}

	dummyCtx := context.Background()
	reqNum := uint32(0)
	require.NoError(te.Start(dummyCtx, reqNum))
	require.Equal(reqBlk, issuedBlkID)
}

func TestGetAncestorsRequestNotIssuedIfBlockBackfillingIsNotEnabled(t *testing.T) {
	require := require.New(t)

	engCfg, vm, sender, err := setupBlockBackfillingTests(t)
	require.NoError(err)

	// create the engine
	te, err := newTransitive(engCfg)
	require.NoError(err)

	// disable block backfilling
	vm.BackfillBlocksEnabledF = func(ctx context.Context) (ids.ID, error) {
		return ids.Empty, block.ErrBlockBackfillingNotEnabled
	}

	// this will make engine Start fail if SendGetAncestor is attempted
	sender.CantSendGetAncestors = true

	dummyCtx := context.Background()
	reqNum := uint32(0)
	require.NoError(te.Start(dummyCtx, reqNum))
}

func TestEngineErrsIfBlockBackfillingIsEnabledCheckErrs(t *testing.T) {
	require := require.New(t)

	engCfg, vm, _, err := setupBlockBackfillingTests(t)
	require.NoError(err)

	// create the engine
	te, err := newTransitive(engCfg)
	require.NoError(err)

	// let BackfillBlocksEnabled err with non-flag error
	customErr := errors.New("a custom error")
	vm.BackfillBlocksEnabledF = func(ctx context.Context) (ids.ID, error) {
		return ids.Empty, customErr
	}

	dummyCtx := context.Background()
	reqNum := uint32(0)
	err = te.Start(dummyCtx, reqNum)
	require.ErrorIs(err, customErr)
}

func TestEngineErrsIfThereAreNoPeersToDownloadBlocksFrom(t *testing.T) {
	require := require.New(t)

	engCfg, vm, _, err := setupBlockBackfillingTests(t)
	require.NoError(err)

	// drop validators, so that there are no peers connected to request blocks from
	vals := validators.NewManager()
	engCfg.Validators = vals

	// create the engine
	te, err := newTransitive(engCfg)
	require.NoError(err)

	// enable block backfilling
	reqBlk := ids.GenerateTestID()
	vm.BackfillBlocksEnabledF = func(ctx context.Context) (ids.ID, error) {
		return reqBlk, nil
	}

	dummyCtx := context.Background()
	reqNum := uint32(0)
	err = te.Start(dummyCtx, reqNum)
	require.ErrorIs(err, errNoPeersToDownloadBlocksFrom)
}

func TestAncestorsProcessing(t *testing.T) {
	require := require.New(t)

	engCfg, vm, sender, err := setupBlockBackfillingTests(t)
	require.NoError(err)

	// create the engine
	te, err := newTransitive(engCfg)
	require.NoError(err)

	// enable block backfilling
	reqBlkFirst := ids.GenerateTestID()
	vm.BackfillBlocksEnabledF = func(ctx context.Context) (ids.ID, error) {
		return reqBlkFirst, nil
	}
	issuedBlk := ids.Empty
	sender.SendGetAncestorsF = func(ctx context.Context, ni ids.NodeID, u uint32, blkID ids.ID) {
		issuedBlk = blkID
	}

	// issue blocks request
	dummyCtx := context.Background()
	startReqNum := uint32(0)
	require.NoError(te.Start(dummyCtx, startReqNum))

	// process GetAncestor response
	var (
		nodeID        = engCfg.Validators.GetValidatorIDs(engCfg.Ctx.SubnetID)[0]
		responseReqID = startReqNum + 1
		blkBytes      = [][]byte{{1}, {2}, {3}}
		pushedBlks    [][]byte
		reqBlkSecond  = ids.GenerateTestID()
	)
	vm.BackfillBlocksF = func(ctx context.Context, b [][]byte) (ids.ID, error) {
		pushedBlks = b
		return reqBlkSecond, nil
	}

	{
		// handle Ancestor response from unexpected nodeID
		wrongNodeID := ids.GenerateTestNodeID()
		require.NotEqual(nodeID, wrongNodeID)
		require.NoError(te.Ancestors(dummyCtx, wrongNodeID, responseReqID, blkBytes))
		require.Nil(pushedBlks) // blocks from wrong NodeID are not pushed to VM
	}
	{
		// handle Ancestor response with wrong requestID
		wrongReqID := uint32(2023)
		require.NotEqual(responseReqID, wrongReqID)
		require.NoError(te.Ancestors(dummyCtx, nodeID, wrongReqID, blkBytes))
		require.Nil(pushedBlks) // blocks from wrong NodeID are not pushed to VM
	}
	{
		// handle empty Ancestor response
		emptyBlkBytes := [][]byte{}
		require.NoError(te.Ancestors(dummyCtx, nodeID, responseReqID, emptyBlkBytes))
		require.Nil(pushedBlks)               // blocks from wrong NodeID are not pushed to VM
		require.Equal(reqBlkFirst, issuedBlk) // check that VM controls next block ID to be requested
	}
	{
		// success
		nodeID := engCfg.Validators.GetValidatorIDs(engCfg.Ctx.SubnetID)[1]
		responseReqID++ // previous consumed by empty Ancestor response case

		require.NoError(te.Ancestors(dummyCtx, nodeID, responseReqID, blkBytes))
		require.Equal(blkBytes, pushedBlks)    // blocks are pushed to VM
		require.Equal(reqBlkSecond, issuedBlk) // check that VM controls next block ID to be requested
	}
}

type fullVM struct {
	*block.TestVM
	*block.TestStateSyncableVM
}

func setupBlockBackfillingTests(t *testing.T) (Config, *fullVM, *common.SenderTest, error) {
	engCfg := DefaultConfigs()

	var (
		vm = &fullVM{
			TestVM: &block.TestVM{
				TestVM: common.TestVM{
					T: t,
				},
			},
			TestStateSyncableVM: &block.TestStateSyncableVM{
				T: t,
			},
		}
		sender = &common.SenderTest{
			T: t,
		}
	)
	engCfg.VM = vm
	engCfg.Sender = sender

	lastAcceptedBlk := &snowman.TestBlock{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return lastAcceptedBlk.ID(), nil
	}

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case lastAcceptedBlk.ID():
			return lastAcceptedBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	// add at least a peer to be reached out for blocks
	vals := validators.NewManager()
	engCfg.Validators = vals
	vdr1 := ids.GenerateTestNodeID()
	vdr2 := ids.GenerateTestNodeID()
	errs := wrappers.Errs{}
	errs.Add(
		vals.AddStaker(engCfg.Ctx.SubnetID, vdr1, nil, ids.Empty, 1),
		vals.AddStaker(engCfg.Ctx.SubnetID, vdr2, nil, ids.Empty, 1),
	)

	return engCfg, vm, sender, errs.Err
}
