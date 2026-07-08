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

// SUT is an initialized transition VM whose pre- and post-transition fakeVMs
// share one [fakeState].
type SUT struct {
	*VM
	ctx  *snow.Context
	pre  *fakeVM
	post *fakeVM
}

// BuildVerifyAccept builds, verifies, and accepts a block. Accepting one at the
// transition time triggers the transition.
func (s *SUT) BuildVerifyAccept(t *testing.T, ctx context.Context, mode contextMode) {
	t.Helper()

	blk, err := s.BuildBlock(ctx)
	require.NoErrorf(t, err, "%T.BuildBlock()", s)
	require.NoErrorf(t, verifyBlock(ctx, blk, mode), "verifyBlock(%T, %s)", s, mode)
	require.NoErrorf(t, blk.Accept(ctx), "%T.Accept()", blk)
}

// contextMode selects whether an operation is performed with a
// [smblock.Context].
type contextMode string

const (
	noContext   contextMode = "NoContext"
	withContext contextMode = "WithContext"
)

var contextModes = []contextMode{noContext, withContext}

var errShouldVerifyWithoutContext = errors.New("unexpectedly should verify without context")

// verifyBlock verifies blk according to mode. The withContext mode requires
// blk to report that it should be verified with a context.
func verifyBlock(ctx context.Context, blk snowman.Block, mode contextMode) error {
	if mode == noContext {
		return blk.Verify(ctx)
	}
	bwc := blk.(smblock.WithVerifyContext)
	should, err := bwc.ShouldVerifyWithContext(ctx)
	if err != nil {
		return err
	}
	if !should {
		return errShouldVerifyWithoutContext
	}
	return bwc.VerifyWithContext(ctx, nil)
}

// setPreference sets vm's preference to blkID according to mode.
func setPreference(ctx context.Context, vm *VM, blkID ids.ID, mode contextMode) error {
	if mode == noContext {
		return vm.SetPreference(ctx, blkID)
	}
	return vm.SetPreferenceWithContext(ctx, blkID, nil)
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
		PreFactory:      fakeFactory{vm: pre},
		PostFactory:     fakeFactory{vm: post},
		TransitionTime:  cfg.transitionTime,
		APIDrainTimeout: 100 * time.Millisecond,
	}
	ctx := snowtest.Context(t, snowtest.CChainID)
	intf, err := factory.New(ctx.Log)
	require.NoErrorf(t, err, "%T.New()", factory)
	vm := intf.(*VM)

	appSender := &enginetest.Sender{
		T: t,
		SendAppRequestF: func(context.Context, set.Set[ids.NodeID], uint32, []byte) error {
			return nil
		},
	}
	require.NoErrorf(t, vm.Initialize(
		t.Context(),
		ctx,
		cfg.db,
		nil, // genesisBytes
		nil, // upgradeBytes
		nil, // configBytes
		nil, // fxs
		appSender,
	), "%T.Initialize()", vm)
	return &SUT{
		VM:   vm,
		ctx:  ctx,
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

// requireVersion asserts that the VM's version matches the expected value.
func (s *SUT) requireVersion(t *testing.T, want string) {
	t.Helper()
	ctx := t.Context()

	got, err := s.Version(ctx)
	require.NoErrorf(t, err, "%T.Version()", s)
	require.Equalf(t, want, got, "%T.Version()", s)
}

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

// fakeVM is a minimal in-memory [Chain] backed by a shared [fakeState].
type fakeVM struct {
	*blocktest.VM
	*blocktest.StateSyncableVM
	initialized bool

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
	// preference is the last ID passed to SetPreference or
	// SetPreferenceWithContext.
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
		StateSyncableVM: &blocktest.StateSyncableVM{T: t},
		name:            name,
		state:           state,
		verified:        make(map[ids.ID]*fakeBlock),
		connected:       make(map[ids.NodeID]*version.Application),
		events:          make(chan common.Message, 1),
	}
}

func (vm *fakeVM) Initialize(
	_ context.Context,
	chainCtx *snow.Context,
	_ database.Database,
	_ []byte,
	_ []byte,
	_ []byte,
	_ []*common.Fx,
	appSender common.AppSender,
) error {
	if vm.initialized {
		return errors.New("duplicate initialization")
	}

	vm.chainCtx = chainCtx
	vm.tip = vm.state.lastAccepted
	vm.appSender = appSender

	vm.initialized = true
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

func (vm *fakeVM) SetPreferenceWithContext(_ context.Context, blkID ids.ID, _ *smblock.Context) error {
	vm.preference = blkID
	return nil
}

func (vm *fakeVM) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	return vm.handlers, nil
}

// WaitForEvent returns the next event's message, or blocks until canceled.
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
	// Blocks are shared across the VMs through the state, but we want each
	// parsed copy to be isolated, like in production, so we store a copy.
	parsable := *child
	vm.state.parsable[child.ID()] = &parsable
	blk := &fakeBlock{Block: child, vm: vm}
	vm.tip = blk
	return blk, nil
}

func (*fakeVM) BuildBlockWithContext(context.Context, *smblock.Context) (snowman.Block, error) {
	return nil, errors.New("unexpectedly called BuildBlockWithContext")
}

// TestInitiallyTransitioned verifies a VM already past its transition time at
// genesis routes to the post-transition chain.
func TestInitiallyTransitioned(t *testing.T) {
	sut := newSUT(t, withBlocksUntilTransition(0))
	sut.requireVersion(t, "post")
}

// TestTransition verifies accepting a block at the transition time switches
// from the pre- to the post-transition chain.
func TestTransition(t *testing.T) {
	sut := newSUT(t)
	ctx := t.Context()

	sut.requireVersion(t, "pre")
	sut.BuildVerifyAccept(t, ctx, noContext) // triggers the transition
	sut.requireVersion(t, "post")
}

// TestTransitionSkipsPreference verifies the transition doesn't set the
// post-transition chain's preference if the preference wasn't set before the
// transition.
func TestTransitionSkipsPreference(t *testing.T) {
	for _, mode := range contextModes {
		t.Run(string(mode), func(t *testing.T) {
			sut := newSUT(t)
			ctx := t.Context()

			sut.BuildVerifyAccept(t, ctx, mode) // triggers the transition
			require.Zerof(t, sut.post.preference, "%T.preference", sut.post)
		})
	}
}

// TestTransitionSetsPreference verifies the transition sets the post-transition
// chain's preference if the preference was set before the transition.
func TestTransitionSetsPreference(t *testing.T) {
	for _, mode := range contextModes {
		t.Run(string(mode), func(t *testing.T) {
			sut := newSUT(t)
			ctx := t.Context()

			lastAcceptedID, err := sut.LastAccepted(ctx)
			require.NoErrorf(t, err, "%T.LastAccepted()", sut)
			require.NoErrorf(t, setPreference(ctx, sut.VM, lastAcceptedID, mode), "setPreference(%s)", mode)

			sut.BuildVerifyAccept(t, ctx, noContext) // triggers the transition
			require.Equalf(t, sut.post.state.lastAccepted.ID(), sut.post.preference, "%T.preference", sut.post)
		})
	}
}

// TestInitializeIsolatesContext verifies the post-transition chain gets its own
// context and metrics gatherer while sharing the chain values.
func TestInitializeIsolatesContext(t *testing.T) {
	sut := newSUT(t)
	ctx := t.Context()

	sut.BuildVerifyAccept(t, ctx, noContext) // triggers the transition

	require.NotSamef(t, sut.pre.chainCtx, sut.post.chainCtx, "%T.chainCtx", sut.post)
	require.NotSamef(t, sut.pre.chainCtx.Metrics, sut.post.chainCtx.Metrics, "%T.chainCtx.Metrics", sut.post)

	sut.ctx.Metrics = nil
	sut.pre.chainCtx.Metrics = nil
	sut.post.chainCtx.Metrics = nil
	require.Equalf(t, sut.ctx, sut.pre.chainCtx, "%T.chainCtx", sut.pre)
	require.Equalf(t, sut.ctx, sut.post.chainCtx, "%T.chainCtx", sut.post)
}

// TestRestart verifies the VM resumes on the correct chain after a restart,
// before and after the transition.
func TestRestart(t *testing.T) {
	// Two blocks to transition, so we can restart while still pre-transition.
	sut := newSUT(t, withBlocksUntilTransition(2))
	ctx := t.Context()

	sut.BuildVerifyAccept(t, ctx, noContext)

	sut = sut.restart(t)
	sut.requireVersion(t, "pre")

	sut.BuildVerifyAccept(t, ctx, noContext) // triggers the transition
	sut.requireVersion(t, "post")

	sut = sut.restart(t)
	sut.requireVersion(t, "post")
}

// TestRestartWithoutTransitionMarker verifies a restart after accepting the
// transition block, but before writing the transition marker, re-runs the
// transition during initialization.
func TestRestartWithoutTransitionMarker(t *testing.T) {
	sut := newSUT(t)
	ctx := t.Context()

	sut.BuildVerifyAccept(t, ctx, noContext) // triggers the transition

	// Model a crash after accepting the transition block but before writing
	// the transition marker.
	require.NoErrorf(t, sut.db.Delete(transitionedKey), "%T.Delete()", sut.db)

	sut = sut.restart(t)
	sut.requireVersion(t, "post")
}
