// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/libevm/libevm/options"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
)

var (
	_ Chain         = (*fakeVM)(nil)
	_ snowman.Block = (*fakeBlock)(nil)
)

// blockInterval is the amount of time fakeVM advances per built block.
const blockInterval = time.Second

// fakeState is the in-memory chain state shared by the pre- and post-transition
// fakeVMs.
type fakeState struct {
	lastAccepted *fakeBlock
	blocks       map[ids.ID]*fakeBlock
}

func newFakeState() *fakeState {
	genesis := &fakeBlock{Block: snowmantest.Genesis}
	return &fakeState{
		lastAccepted: genesis,
		blocks: map[ids.ID]*fakeBlock{
			genesis.ID(): genesis,
		},
	}
}

// fakeBlock lives in its VM's memory once verified and is persisted to shared
// state once accepted.
type fakeBlock struct {
	*snowmantest.Block
	vm *fakeVM
}

func (b *fakeBlock) Verify(ctx context.Context) error {
	if err := b.Block.Verify(ctx); err != nil {
		return err
	}
	b.vm.verified[b.ID()] = b
	return nil
}

func (b *fakeBlock) Accept(ctx context.Context) error {
	if err := b.Block.Accept(ctx); err != nil {
		return err
	}
	b.vm.state.lastAccepted = b
	b.vm.state.blocks[b.ID()] = b
	return nil
}

// fakeVM is a minimal in-memory [Chain] backed by a shared fakeState.
type fakeVM struct {
	*blocktest.VM
	*blocktest.SetPreferenceVM
	*blocktest.StateSyncableVM

	name  string
	state *fakeState
	// verified holds blocks verified but not yet accepted.
	verified map[ids.ID]*fakeBlock
	// tip is this VM's local building head.
	tip *fakeBlock
}

func newFakeVM(t *testing.T, name string, state *fakeState) *fakeVM {
	return &fakeVM{
		VM: &blocktest.VM{
			VM: enginetest.VM{T: t},
		},
		SetPreferenceVM: &blocktest.SetPreferenceVM{T: t},
		StateSyncableVM: &blocktest.StateSyncableVM{T: t},
		name:            name,
		state:           state,
		verified:        make(map[ids.ID]*fakeBlock),
	}
}

func (vm *fakeVM) Initialize(context.Context, *snow.Context, database.Database, []byte, []byte, []byte, []*common.Fx, common.AppSender) error {
	vm.tip = vm.state.lastAccepted
	return nil
}

func (vm *fakeVM) Version(context.Context) (string, error) {
	return vm.name, nil
}

func (vm *fakeVM) LastAccepted(context.Context) (ids.ID, error) {
	return vm.state.lastAccepted.ID(), nil
}

func (vm *fakeVM) GetBlock(_ context.Context, blkID ids.ID) (snowman.Block, error) {
	if blk, ok := vm.state.blocks[blkID]; ok {
		return blk, nil
	}
	if blk, ok := vm.verified[blkID]; ok {
		return blk, nil
	}
	return nil, database.ErrNotFound
}

// BuildBlock returns a child of the tip, advanced one blockInterval.
func (vm *fakeVM) BuildBlock(context.Context) (snowman.Block, error) {
	child := snowmantest.BuildChild(vm.tip.Block)
	child.TimestampV = vm.tip.Timestamp().Add(blockInterval)
	blk := &fakeBlock{Block: child, vm: vm}
	vm.tip = blk
	return blk, nil
}

func (*fakeVM) BuildBlockWithContext(context.Context, *block.Context) (snowman.Block, error) {
	return nil, errors.New("unexpectedly called BuildBlockWithContext")
}

// SUT is the system under test: an initialized transition VM whose pre- and
// post-transition fakeVMs ("pre" and "post") share one fakeState.
type SUT struct {
	*VM
}

type sutConfig struct {
	blocksUntilTransition int
}

// A sutOption overrides a default used by [newSUT].
type sutOption = options.Option[sutConfig]

// withBlocksUntilTransition sets how many blocks must be built on genesis
// before one reaches the transition time. Accepting the nth triggers it.
func withBlocksUntilTransition(n int) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.blocksUntilTransition = n
	})
}

func newSUT(t *testing.T, opts ...sutOption) *SUT {
	t.Helper()

	cfg := options.ApplyTo(&sutConfig{
		blocksUntilTransition: 1,
	}, opts...)

	state := newFakeState()
	timeUntilTransition := time.Duration(cfg.blocksUntilTransition) * blockInterval
	vm := &VM{
		preTransitionChain:  newFakeVM(t, "pre", state),
		postTransitionChain: newFakeVM(t, "post", state),
		transitionTime:      snowmantest.GenesisTimestamp.Add(timeUntilTransition),
	}
	vm.current = &current{chain: vm.preTransitionChain}

	require.NoError(t, vm.Initialize(
		t.Context(),
		snowtest.Context(t, snowtest.CChainID),
		memdb.New(),
		nil, // genesisBytes
		nil, // upgradeBytes
		nil, // configBytes
		nil, // fxs
		nil, // appSender
	))
	return &SUT{VM: vm}
}

// TestInitiallyTransitioned verifies that a VM whose transition time is already
// reached at genesis routes calls to the post-transition chain.
func TestInitiallyTransitioned(t *testing.T) {
	sut := newSUT(t, withBlocksUntilTransition(0))
	ctx := t.Context()

	version, err := sut.Version(ctx)
	require.NoError(t, err)
	require.Equal(t, "post", version)
}

// TestTransition verifies that accepting a block which reaches the transition
// time switches routing from the pre- to the post-transition chain.
func TestTransition(t *testing.T) {
	sut := newSUT(t)
	ctx := t.Context()

	version, err := sut.Version(ctx)
	require.NoError(t, err)
	require.Equal(t, "pre", version)

	blk, err := sut.BuildBlock(ctx)
	require.NoError(t, err)
	require.NoError(t, blk.Verify(ctx))
	require.NoError(t, blk.Accept(ctx)) // triggers the transition

	version, err = sut.Version(ctx)
	require.NoError(t, err)
	require.Equal(t, "post", version)
}
