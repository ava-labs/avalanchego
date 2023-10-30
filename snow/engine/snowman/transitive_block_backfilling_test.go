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
	vdr := ids.GenerateTestNodeID()
	err := vals.AddStaker(engCfg.Ctx.SubnetID, vdr, nil, ids.Empty, 1)

	return engCfg, vm, sender, err
}
