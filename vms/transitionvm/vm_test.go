// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"errors"
	"net/http"
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
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"

	smblock "github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var (
	_ Chain                     = (*fakeVM)(nil)
	_ snowman.Block             = (*fakeBlock)(nil)
	_ smblock.WithVerifyContext = (*fakeBlock)(nil)
)

// blockInterval is how much fakeVM advances time per built block.
const blockInterval = time.Second

// fakeState is chain state shared by the pre- and post-transition fakeVMs.
type fakeState struct {
	lastAccepted *fakeBlock
	// blocks contains all accepted blocks
	blocks map[ids.ID]*fakeBlock
	// parsable contains all built blocks
	parsable map[ids.ID]*snowmantest.Block
}

func newFakeState() *fakeState {
	genesis := &fakeBlock{Block: snowmantest.Genesis}
	return &fakeState{
		lastAccepted: genesis,
		blocks: map[ids.ID]*fakeBlock{
			genesis.ID(): genesis,
		},
		parsable: map[ids.ID]*snowmantest.Block{
			genesis.ID(): snowmantest.Genesis,
		},
	}
}

// fakeBlock is held in its VM's memory once verified and persisted to shared
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

func (*fakeBlock) ShouldVerifyWithContext(context.Context) (bool, error) {
	return true, nil
}

func (b *fakeBlock) VerifyWithContext(ctx context.Context, _ *smblock.Context) error {
	return b.Verify(ctx)
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
	// appSender is captured in Initialize for sendAppRequest.
	appSender common.AppSender
	// connected holds the peers currently connected to this VM.
	connected map[ids.NodeID]*version.Application
	// consensusState is the last state passed to SetState.
	consensusState snow.State
	// preference is the last ID passed to SetPreference.
	preference ids.ID
	// chainCtx is the context captured in Initialize.
	chainCtx *snow.Context
	// handlers is returned by CreateHandlers.
	handlers map[string]http.Handler
	// events feeds WaitForEvent, which otherwise blocks until canceled.
	events chan common.Message
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
		connected:       make(map[ids.NodeID]*version.Application),
		events:          make(chan common.Message, 1),
	}
}

func (vm *fakeVM) Initialize(_ context.Context, chainCtx *snow.Context, _ database.Database, _, _, _ []byte, _ []*common.Fx, appSender common.AppSender) error {
	vm.chainCtx = chainCtx
	vm.tip = vm.state.lastAccepted
	vm.appSender = appSender
	return nil
}

// sendAppRequest sends an app request via the AppSender captured in Initialize.
func (vm *fakeVM) sendAppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return vm.appSender.SendAppRequest(ctx, set.Of(nodeID), requestID, nil)
}

func (vm *fakeVM) Connected(_ context.Context, nodeID ids.NodeID, v *version.Application) error {
	vm.connected[nodeID] = v
	return nil
}

func (vm *fakeVM) Disconnected(_ context.Context, nodeID ids.NodeID) error {
	delete(vm.connected, nodeID)
	return nil
}

func (vm *fakeVM) SetState(_ context.Context, state snow.State) error {
	vm.consensusState = state
	return nil
}

func (vm *fakeVM) SetPreference(_ context.Context, blkID ids.ID) error {
	vm.preference = blkID
	return nil
}

func (vm *fakeVM) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	return vm.handlers, nil
}

// WaitForEvent returns the next events message, or blocks until canceled.
func (vm *fakeVM) WaitForEvent(ctx context.Context) (common.Message, error) {
	select {
	case msg := <-vm.events:
		return msg, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func (vm *fakeVM) Version(context.Context) (string, error) {
	return vm.name, nil
}

func (vm *fakeVM) LastAccepted(context.Context) (ids.ID, error) {
	return vm.state.lastAccepted.ID(), nil
}

func (vm *fakeVM) GetBlock(_ context.Context, blkID ids.ID) (snowman.Block, error) {
	if blk, ok := vm.verified[blkID]; ok {
		return blk, nil
	}
	if blk, ok := vm.state.blocks[blkID]; ok {
		return blk, nil
	}
	return nil, database.ErrNotFound
}

// ParseBlock reconstructs a block from its bytes. The reconstructed block is
// owned by this VM.
func (vm *fakeVM) ParseBlock(_ context.Context, b []byte) (snowman.Block, error) {
	// [snowmantest.BuildChild] sets the bytes to the block ID.
	blkID, err := ids.ToID(b)
	if err != nil {
		return nil, err
	}
	blk, ok := vm.state.parsable[blkID]
	if !ok {
		return nil, database.ErrNotFound
	}
	return &fakeBlock{Block: blk, vm: vm}, nil
}

// BuildBlock returns a child of the tip, advancing the timestamp by
// [blockInterval].
func (vm *fakeVM) BuildBlock(context.Context) (snowman.Block, error) {
	child := snowmantest.BuildChild(vm.tip.Block)
	child.TimestampV = vm.tip.Timestamp().Add(blockInterval)
	vm.state.parsable[child.ID()] = child
	blk := &fakeBlock{Block: child, vm: vm}
	vm.tip = blk
	return blk, nil
}

func (*fakeVM) BuildBlockWithContext(context.Context, *smblock.Context) (snowman.Block, error) {
	return nil, errors.New("unexpectedly called BuildBlockWithContext")
}

// SUT is an initialized transition VM whose pre- and post-transition fakeVMs
// share one fakeState.
type SUT struct {
	*VM
	pre  *fakeVM
	post *fakeVM
}

// BuildVerifyAccept builds, verifies, and accepts a block. Accepting one at the
// transition time triggers the transition.
func (s *SUT) BuildVerifyAccept(t *testing.T, ctx context.Context, mode verifyMode) {
	t.Helper()

	blk, err := s.BuildBlock(ctx)
	require.NoError(t, err)
	require.NoError(t, verifyBlock(ctx, blk, mode))
	require.NoError(t, blk.Accept(ctx))
}

// verifyMode selects whether a block is verified with a [smblock.Context].
type verifyMode int

const (
	verifyNoContext verifyMode = iota
	verifyWithContext
)

var verifyModes = []verifyMode{verifyNoContext, verifyWithContext}

func (m verifyMode) String() string {
	if m == verifyWithContext {
		return "VerifyWithContext"
	}
	return "Verify"
}

// verifyBlock verifies blk according to mode.
func verifyBlock(ctx context.Context, blk snowman.Block, mode verifyMode) error {
	if mode == verifyNoContext {
		return blk.Verify(ctx)
	}
	bwc := blk.(smblock.WithVerifyContext)
	return bwc.VerifyWithContext(ctx, nil)
}

type sutConfig struct {
	db             database.Database
	state          *fakeState
	transitionTime time.Time
}

// A sutOption overrides a default used by [newSUT].
type sutOption = options.Option[sutConfig]

// withBlocksUntilTransition sets how many blocks after genesis reach the
// transition time; accepting the nth triggers it.
func withBlocksUntilTransition(n int) sutOption {
	return withTransitionTime(snowmantest.GenesisTimestamp.Add(time.Duration(n) * blockInterval))
}

// withDatabase sets the database the VM is initialized with.
func withDatabase(db database.Database) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.db = db
	})
}

// withState sets the chain state shared by the fakeVMs.
func withState(state *fakeState) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.state = state
	})
}

// withTransitionTime sets the time at which the VM transitions.
func withTransitionTime(transitionTime time.Time) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.transitionTime = transitionTime
	})
}

func newSUT(t *testing.T, opts ...sutOption) *SUT {
	t.Helper()

	cfg := options.ApplyTo(&sutConfig{
		db:             memdb.New(),
		state:          newFakeState(),
		transitionTime: snowmantest.GenesisTimestamp.Add(blockInterval),
	}, opts...)

	pre := newFakeVM(t, "pre", cfg.state)
	post := newFakeVM(t, "post", cfg.state)
	factory := &Factory{
		PreFactory:     fakeFactory{vm: pre},
		PostFactory:    fakeFactory{vm: post},
		TransitionTime: cfg.transitionTime,
	}
	ctx := snowtest.Context(t, snowtest.CChainID)
	intf, err := factory.New(ctx.Log)
	require.NoError(t, err)
	vm := intf.(*VM)

	appSender := &enginetest.Sender{
		T: t,
		SendAppRequestF: func(context.Context, set.Set[ids.NodeID], uint32, []byte) error {
			return nil
		},
	}
	require.NoError(t, vm.Initialize(
		t.Context(),
		ctx,
		cfg.db,
		nil, // genesisBytes
		nil, // upgradeBytes
		nil, // configBytes
		nil, // fxs
		appSender,
	))
	return &SUT{
		VM:   vm,
		pre:  pre,
		post: post,
	}
}

// restart rebuilds the VM against the same database and chain state, modeling a
// node restart.
func (s *SUT) restart(t *testing.T) *SUT {
	t.Helper()
	return newSUT(t,
		withDatabase(s.db),
		withState(s.pre.state),
		withTransitionTime(s.transitionTime),
	)
}

// TestInitiallyTransitioned verifies a VM already past its transition time at
// genesis routes to the post-transition chain.
func TestInitiallyTransitioned(t *testing.T) {
	sut := newSUT(t, withBlocksUntilTransition(0))
	ctx := t.Context()

	version, err := sut.Version(ctx)
	require.NoError(t, err)
	require.Equal(t, "post", version)
}

// TestTransition verifies accepting a block at the transition time switches
// from the pre- to the post-transition chain.
func TestTransition(t *testing.T) {
	sut := newSUT(t)
	ctx := t.Context()

	version, err := sut.Version(ctx)
	require.NoError(t, err)
	require.Equal(t, "pre", version)

	sut.BuildVerifyAccept(t, ctx, verifyNoContext) // triggers the transition

	version, err = sut.Version(ctx)
	require.NoError(t, err)
	require.Equal(t, "post", version)
}

// TestTransitionSetsPreference verifies the transition sets the post-transition
// chain's preference to the last accepted block.
func TestTransitionSetsPreference(t *testing.T) {
	sut := newSUT(t)
	ctx := t.Context()

	sut.BuildVerifyAccept(t, ctx, verifyNoContext) // triggers the transition

	require.Equal(t, sut.post.state.lastAccepted.ID(), sut.post.preference)
}

// TestInitializeIsolatesContext verifies the post-transition chain gets its own
// context and metrics gatherer while sharing the chain values.
func TestInitializeIsolatesContext(t *testing.T) {
	sut := newSUT(t)
	ctx := t.Context()

	sut.BuildVerifyAccept(t, ctx, verifyNoContext) // triggers the transition

	require.NotSame(t, sut.pre.chainCtx, sut.post.chainCtx)
	require.NotSame(t, sut.pre.chainCtx.Metrics, sut.post.chainCtx.Metrics)
	require.Equal(t, sut.pre.chainCtx.ChainID, sut.post.chainCtx.ChainID)
}

// TestRestart verifies the VM resumes on the correct chain after a restart,
// before and after the transition.
func TestRestart(t *testing.T) {
	// Two blocks to transition, so we can restart while still pre-transition.
	sut := newSUT(t, withBlocksUntilTransition(2))
	ctx := t.Context()

	sut.BuildVerifyAccept(t, ctx, verifyNoContext)

	sut = sut.restart(t)
	version, err := sut.Version(ctx)
	require.NoError(t, err)
	require.Equal(t, "pre", version)

	sut.BuildVerifyAccept(t, ctx, verifyNoContext) // triggers the transition
	version, err = sut.Version(ctx)
	require.NoError(t, err)
	require.Equal(t, "post", version)

	sut = sut.restart(t)
	version, err = sut.Version(ctx)
	require.NoError(t, err)
	require.Equal(t, "post", version)
}
