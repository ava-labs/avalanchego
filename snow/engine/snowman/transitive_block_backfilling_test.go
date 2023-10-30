// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/stretchr/testify/require"
)

type fullVM struct {
	*block.TestVM
	*block.TestStateSyncableVM
}

func TestGetAncestorsRequestIssuedIfStateSyncVMEnabledBlockBackfilling(t *testing.T) {
	require := require.New(t)
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
		sender = engCfg.Sender.(*common.SenderTest)
	)
	engCfg.VM = vm

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
	require.NoError(vals.AddStaker(engCfg.Ctx.SubnetID, vdr, nil, ids.Empty, 1))

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
	require.NoError(te.Start(dummyCtx, uint32(reqNum)))
	require.Equal(reqBlk, issuedBlkID)
}
