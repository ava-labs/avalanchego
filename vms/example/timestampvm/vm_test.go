// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timestampvm

import (
	"context"
	"testing"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/version"
	"github.com/stretchr/testify/require"
)

var blockchainID = ids.ID{1, 2, 3}

// require that after initialization, the vm has the state we expect
func TestGenesis(t *testing.T) {
	require := require.New(t)
	ctx := context.TODO()
	// Initialize the vm
	vm, _, _, err := newTestVM()
	require.NoError(err)
	// Verify that the db is initialized
	ok, err := vm.state.IsInitialized()
	require.NoError(err)
	require.True(ok)

	// Get lastAccepted
	lastAccepted, err := vm.LastAccepted(ctx)
	require.NoError(err)
	require.NotEqual(ids.Empty, lastAccepted)

	// Verify that getBlock returns the genesis block, and the genesis block
	// is the type we expect
	genesisBlock, err := vm.getBlock(lastAccepted) // genesisBlock as snowman.Block
	require.NoError(err)

	// Verify that the genesis block has the data we expect
	require.Equal(ids.Empty, genesisBlock.Parent())
	require.Equal([32]byte{0, 0, 0, 0, 0}, genesisBlock.Data())
}

func TestHappyPath(t *testing.T) {
	require := require.New(t)
	ctx := context.TODO()

	// Initialize the vm
	vm, snowCtx, msgChan, err := newTestVM()
	require.NoError(err)

	lastAcceptedID, err := vm.LastAccepted(ctx)
	require.NoError(err)
	genesisBlock, err := vm.getBlock(lastAcceptedID)
	require.NoError(err)

	// in an actual execution, the engine would set the preference
	require.NoError(vm.SetPreference(ctx, genesisBlock.ID()))

	snowCtx.Lock.Lock()
	vm.proposeBlock([DataLen]byte{0, 0, 0, 0, 1}) // propose a value
	snowCtx.Lock.Unlock()

	select { // require there is a pending tx message to the engine
	case msg := <-msgChan:
		require.Equal(common.PendingTxs, msg)
	default:
		require.FailNow("should have been pendingTxs message on channel")
	}

	// build the block
	snowCtx.Lock.Lock()
	snowmanBlock2, err := vm.BuildBlock(ctx)
	require.NoError(err)

	require.NoError(snowmanBlock2.Verify(ctx))
	require.NoError(snowmanBlock2.Accept(ctx))
	require.NoError(vm.SetPreference(ctx, snowmanBlock2.ID()))

	lastAcceptedID, err = vm.LastAccepted(ctx)
	require.NoError(err)

	// Should be the block we just accepted
	block2, err := vm.getBlock(lastAcceptedID)
	require.NoError(err)

	// require the block we accepted has the data we expect
	require.Equal(genesisBlock.ID(), block2.Parent())
	require.Equal([DataLen]byte{0, 0, 0, 0, 1}, block2.Data())
	require.Equal(snowmanBlock2.ID(), block2.ID())
	require.NoError(block2.Verify(ctx))

	vm.proposeBlock([DataLen]byte{0, 0, 0, 0, 2}) // propose a block
	snowCtx.Lock.Unlock()

	select { // verify there is a pending tx message to the engine
	case msg := <-msgChan:
		require.Equal(common.PendingTxs, msg)
	default:
		require.FailNow("should have been pendingTxs message on channel")
	}

	snowCtx.Lock.Lock()

	// build the block
	snowmanBlock3, err := vm.BuildBlock(ctx)
	require.NoError(err)
	require.NoError(snowmanBlock3.Verify(ctx))
	require.NoError(snowmanBlock3.Accept(ctx))
	require.NoError(vm.SetPreference(ctx, snowmanBlock3.ID()))

	lastAcceptedID, err = vm.LastAccepted(ctx)
	require.NoError(err)
	// The block we just accepted
	block3, err := vm.getBlock(lastAcceptedID)
	require.NoError(err)

	// require the block we accepted has the data we expect
	require.Equal(snowmanBlock2.ID(), block3.Parent())
	require.Equal([DataLen]byte{0, 0, 0, 0, 2}, block3.Data())
	require.Equal(snowmanBlock3.ID(), block3.ID())
	require.NoError(block3.Verify(ctx))

	// Next, check the blocks we added are there
	block2FromState, err := vm.getBlock(block2.ID())
	require.NoError(err)
	require.Equal(block2.ID(), block2FromState.ID())

	block3FromState, err := vm.getBlock(snowmanBlock3.ID())
	require.NoError(err)
	require.Equal(snowmanBlock3.ID(), block3FromState.ID())

	snowCtx.Lock.Unlock()
}

func TestService(t *testing.T) {
	// Initialize the vm
	require := require.New(t)
	// Initialize the vm
	vm, _, _, err := newTestVM()
	require.NoError(err)
	service := Service{vm}
	require.NoError(service.GetBlock(nil, &GetBlockArgs{}, &GetBlockReply{}))
}

func TestSetState(t *testing.T) {
	// Initialize the vm
	require := require.New(t)
	ctx := context.TODO()
	// Initialize the vm
	vm, _, _, err := newTestVM()
	require.NoError(err)
	// bootstrapping
	require.NoError(vm.SetState(ctx, snow.Bootstrapping))
	require.False(vm.bootstrapped.Get())
	// bootstrapped
	require.NoError(vm.SetState(ctx, snow.NormalOp))
	require.True(vm.bootstrapped.Get())
	// unknown
	unknownState := snow.State(99)
	require.ErrorIs(vm.SetState(ctx, unknownState), snow.ErrUnknownState)
}

func newTestVM() (*VM, *snow.Context, chan common.Message, error) {
	dbManager := manager.NewMemDB(&version.Semantic{
		Major: 1,
		Minor: 0,
		Patch: 0,
	})
	msgChan := make(chan common.Message, 1)
	vm := &VM{}
	snowCtx := snow.DefaultContextTest()
	snowCtx.ChainID = blockchainID
	err := vm.Initialize(context.TODO(), snowCtx, dbManager, []byte{0, 0, 0, 0, 0}, nil, nil, msgChan, nil, nil)
	return vm, snowCtx, msgChan, err
}
