// Copyright (C) 2024, Ava Labs, Inv. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/vms/sdk/chainindex"
)

var (
	_ ConcreteBlock                                  = (*TestBlock)(nil)
	_ ConcreteVM[*TestBlock, *TestBlock, *TestBlock] = (*TestChain)(nil)
)

var errVerifyInvalidBlock = errors.New("verified invalid block")

const (
	testVersion    = "v0.0.1"
	blockStringerF = "(BlockID = %s, ParentID = %s, Timestamp = %d, Height = %d, RandomData = %x, Invalid = %t)"
)

type TestBlock struct {
	PrntID ids.ID `json:"parentID"`
	Tmstmp int64  `json:"timestamp"`
	Hght   uint64 `json:"height"`
	// RandomData is used to uniquify blocks given there's no state or application data
	// included in the tests otherwise.
	RandomData []byte `json:"randomData"`

	// Invalid marks a block that should return an error during execution.
	// This should make it easy to construct a block that should fail execution.
	Invalid bool `json:"invalid"`

	BlockContext *block.Context `json:"pChainHeight"`

	outputPopulated   bool
	acceptedPopulated bool
}

func NewTestBlockFromParent(parent *TestBlock) *TestBlock {
	return &TestBlock{
		PrntID:     parent.GetID(),
		Tmstmp:     parent.GetTimestamp() + 1,
		Hght:       parent.GetHeight() + 1,
		RandomData: utils.RandomBytes(32),
	}
}

func NewTestBlockFromParentWithContext(parent *TestBlock, ctx *block.Context) *TestBlock {
	blk := NewTestBlockFromParent(parent)
	blk.BlockContext = ctx
	return blk
}

func (t *TestBlock) GetID() ids.ID {
	return hashing.ComputeHash256Array(t.GetBytes())
}

func (t *TestBlock) GetParent() ids.ID {
	return t.PrntID
}

func (t *TestBlock) GetTimestamp() int64 {
	return t.Tmstmp
}

func (t *TestBlock) GetBytes() []byte {
	b, err := json.Marshal(t)
	if err != nil {
		panic(err)
	}
	return b
}

func (t *TestBlock) GetHeight() uint64 {
	return t.Hght
}

func (t *TestBlock) GetContext() *block.Context {
	return t.BlockContext
}

func (t *TestBlock) String() string {
	return fmt.Sprintf(blockStringerF, t.GetID(), t.PrntID, t.Tmstmp, t.Hght, t.RandomData, t.Invalid)
}

func NewTestBlockFromBytes(b []byte) (*TestBlock, error) {
	blk := &TestBlock{}
	if err := json.Unmarshal(b, blk); err != nil {
		return nil, err
	}
	return blk, nil
}

type TestChain struct {
	t                     *testing.T
	require               *require.Assertions
	initLastAcceptedBlock *TestBlock
}

func NewTestChain(
	t *testing.T,
	require *require.Assertions,
	initLastAcceptedBlock *TestBlock,
) *TestChain {
	return &TestChain{
		t:                     t,
		require:               require,
		initLastAcceptedBlock: initLastAcceptedBlock,
	}
}

func (t *TestChain) Initialize(
	ctx context.Context,
	chainInput ChainInput,
	_ *VM[*TestBlock, *TestBlock, *TestBlock],
) (ChainIndex[*TestBlock], *TestBlock, *TestBlock, bool, error) {
	chainIndex, err := chainindex.New[*TestBlock](chainInput.SnowCtx.Log, prometheus.NewRegistry(), chainindex.NewDefaultConfig(), t, memdb.New())
	if err != nil {
		return nil, nil, nil, false, err
	}
	if err := chainIndex.UpdateLastAccepted(ctx, t.initLastAcceptedBlock); err != nil {
		return nil, nil, nil, false, err
	}
	return chainIndex, t.initLastAcceptedBlock, t.initLastAcceptedBlock, t.initLastAcceptedBlock.acceptedPopulated, nil
}

func (*TestChain) SetConsensusIndex(_ *ConsensusIndex[*TestBlock, *TestBlock, *TestBlock]) {}

func (t *TestChain) BuildBlock(_ context.Context, blkContext *block.Context, parent *TestBlock) (*TestBlock, *TestBlock, error) {
	t.require.True(parent.outputPopulated)
	builtBlock := NewTestBlockFromParentWithContext(parent, blkContext)
	builtBlock.outputPopulated = true
	return builtBlock, builtBlock, nil
}

func (*TestChain) ParseBlock(_ context.Context, bytes []byte) (*TestBlock, error) {
	return NewTestBlockFromBytes(bytes)
}

func (t *TestChain) VerifyBlock(_ context.Context, parent *TestBlock, block *TestBlock) (*TestBlock, error) {
	// The parent must have been executed before we execute the block
	t.require.True(parent.outputPopulated)
	if block.Invalid {
		return nil, fmt.Errorf("%w: %s", errVerifyInvalidBlock, block)
	}

	// A block should only be executed once
	t.require.False(block.outputPopulated)
	block.outputPopulated = true

	return block, nil
}

func (t *TestChain) AcceptBlock(_ context.Context, acceptedParent *TestBlock, verifiedBlock *TestBlock) (*TestBlock, error) {
	// This block must be executed before calling accept
	t.require.True(acceptedParent.outputPopulated)
	t.require.True(acceptedParent.acceptedPopulated)
	t.require.True(verifiedBlock.outputPopulated)

	// The block should only be accepted once
	t.require.False(verifiedBlock.acceptedPopulated)
	verifiedBlock.acceptedPopulated = true

	return verifiedBlock, nil
}

type TestConsensusEngine struct {
	t       *testing.T
	require *require.Assertions
	rand    *rand.Rand
	chain   *TestChain
	vm      *SnowVM[*TestBlock, *TestBlock, *TestBlock]

	lastAccepted *Block[*TestBlock, *TestBlock, *TestBlock]
	preferred    *Block[*TestBlock, *TestBlock, *TestBlock]
	verified     map[ids.ID]*Block[*TestBlock, *TestBlock, *TestBlock]
	children     map[ids.ID]set.Set[ids.ID]
	accepted     []*Block[*TestBlock, *TestBlock, *TestBlock]
}

func NewTestConsensusEngine(t *testing.T, initLastAcceptedBlock *TestBlock) *TestConsensusEngine {
	rand := rand.New(rand.NewSource(0)) //nolint:gosec
	return NewTestConsensusEngineWithRand(t, rand, initLastAcceptedBlock)
}

func NewTestConsensusEngineWithRand(t *testing.T, rand *rand.Rand, initLastAcceptedBlock *TestBlock) *TestConsensusEngine {
	r := require.New(t)
	ctx := context.Background()
	chain := NewTestChain(t, r, initLastAcceptedBlock)
	vm := NewSnowVM[*TestBlock, *TestBlock, *TestBlock](testVersion, chain)
	toEngine := make(chan common.Message, 1)
	ce := &TestConsensusEngine{
		t:        t,
		require:  r,
		rand:     rand,
		chain:    chain,
		vm:       vm,
		verified: make(map[ids.ID]*Block[*TestBlock, *TestBlock, *TestBlock]),
		children: make(map[ids.ID]set.Set[ids.ID]),
	}
	snowCtx := snowtest.Context(t, ids.GenerateTestID())
	snowCtx.ChainDataDir = t.TempDir()
	config := map[string]interface{}{
		SnowVMConfigKey: VMConfig{
			AcceptedBlockWindowCache: 2,
		},
	}
	configBytes, err := json.Marshal(config)
	r.NoError(err)
	r.NoError(vm.Initialize(ctx, snowCtx, nil, nil, configBytes, nil, toEngine, nil, &enginetest.Sender{T: t}))
	ce.lastAccepted = vm.LastAcceptedBlock(ctx)
	ce.preferred = ce.lastAccepted
	t.Cleanup(func() {
		r.NoError(vm.Shutdown(ctx))
	})
	return ce
}

// BuildBlock copies the expected behavior of the consensus engine when building a block
// and assumes the VM always builds a correct block.
func (ce *TestConsensusEngine) BuildBlock(ctx context.Context) (*Block[*TestBlock, *TestBlock, *TestBlock], bool) {
	preferredID := ce.preferred.ID()
	blk, err := ce.vm.VM.BuildBlock(ctx)

	ce.require.NoError(err)
	ce.require.Equal(preferredID, blk.Parent())
	// Skip if we built a block identical to one we've already verified
	if _, ok := ce.verified[blk.ID()]; ok {
		return blk, false
	}

	ce.verifyValidBlock(ctx, blk)
	ce.require.NoError(blk.Verify(ctx))
	ce.verified[blk.ID()] = blk

	// Note: there is technically a case in the engine where building a block can enable issuance of
	// pending blocks that are missing an ancestor. We ignore this edge case for simplicity here.
	ce.require.NoError(ce.vm.SetPreference(ctx, blk.ID()))
	ce.preferred = blk
	return blk, true
}

func (ce *TestConsensusEngine) verifyValidBlock(ctx context.Context, blk *Block[*TestBlock, *TestBlock, *TestBlock]) {
	ce.require.NoError(blk.Verify(ctx))
	ce.verified[blk.ID()] = blk

	children, ok := ce.children[blk.Parent()]
	if !ok {
		children = set.NewSet[ids.ID](1)
		ce.children[blk.Parent()] = children
	}
	children.Add(blk.ID())
}

func (ce *TestConsensusEngine) selectRandomVerifiedBlock() (*Block[*TestBlock, *TestBlock, *TestBlock], bool) {
	for _, blk := range ce.verified {
		return blk, true
	}
	return nil, false
}

// getLastAcceptedToBlk returns the chain of blocks in the range (lastAcceptedBlk, blk]
// If lastAcceptedBlk == blk, this returns an empty chain
// Assumes that blk and its ancestors tracing back to lastAcceptedBlk are in verified
func (ce *TestConsensusEngine) getLastAcceptedToBlk(_ context.Context, blk *Block[*TestBlock, *TestBlock, *TestBlock]) []*Block[*TestBlock, *TestBlock, *TestBlock] {
	if blk.ID() == ce.lastAccepted.ID() {
		return nil
	}

	chain := make([]*Block[*TestBlock, *TestBlock, *TestBlock], 0)
	for {
		// Add the block to the chain and check for the next block
		chain = append(chain, blk)
		if parentBlk, ok := ce.verified[blk.Parent()]; ok {
			blk = parentBlk
			continue
		}

		if blk.Parent() == ce.lastAccepted.ID() {
			break
		}
		ce.require.FailNow("could not find parent tracing to last accepted block")
	}
	slices.Reverse(chain)
	return chain
}

// acceptChain should mimic the accept behavior of acceptPreferredChild in the Snow consensus engine
// Ref. https://github.com/ava-labs/avalanchego/blob/f6a5c1cd9e0fce911fb2367d1e69b8bb9af1fceb/snow/consensus/snowman/topological.go#L578
func (ce *TestConsensusEngine) acceptChain(ctx context.Context, chain []*Block[*TestBlock, *TestBlock, *TestBlock]) {
	for _, blk := range chain {
		_, ok := ce.verified[blk.ID()]
		ce.require.True(ok)

		parent := ce.lastAccepted
		ce.require.Equal(parent.ID(), blk.Parent())

		ce.require.NoError(blk.Accept(ctx))
		delete(ce.verified, blk.ID())
		ce.lastAccepted = blk
		ce.accepted = append(ce.accepted, blk)

		children := ce.children[parent.ID()]
		children.Remove(blk.ID())
		delete(ce.children, parent.ID())

		ce.rejectTransitively(ctx, children)
	}
}

func (ce *TestConsensusEngine) rejectTransitively(ctx context.Context, toReject set.Set[ids.ID]) {
	for child := range toReject {
		childBlk, ok := ce.verified[child]
		ce.require.True(ok)
		ce.require.NoError(childBlk.Reject(ctx))
		delete(ce.verified, child)

		ce.rejectTransitively(ctx, ce.children[child])
	}
}

func (ce *TestConsensusEngine) AcceptPreferredChain(ctx context.Context) (*Block[*TestBlock, *TestBlock, *TestBlock], bool) {
	preferredChain := ce.getLastAcceptedToBlk(ctx, ce.preferred)

	if len(preferredChain) == 0 {
		return nil, false
	}

	ce.acceptChain(ctx, preferredChain)
	return preferredChain[len(preferredChain)-1], true
}

func (ce *TestConsensusEngine) GetAcceptedBlock(ctx context.Context) {
	if len(ce.accepted) == 0 {
		return
	}

	selectedBlk := ce.accepted[ce.rand.Intn(len(ce.accepted))]
	retrievedBlk, err := ce.vm.GetBlock(ctx, selectedBlk.ID())
	ce.require.NoError(err)
	ce.require.Equal(retrievedBlk.ID(), selectedBlk.ID())

	retrievedBlkID, err := ce.vm.GetBlockIDAtHeight(ctx, selectedBlk.Height())
	ce.require.NoError(err)
	ce.require.Equal(selectedBlk.ID(), retrievedBlkID)

	retrievedBlk, err = ce.vm.GetBlockByHeight(ctx, selectedBlk.GetHeight())
	ce.require.NoError(err)
	ce.require.Equal(retrievedBlk.ID(), selectedBlk.ID())
}

func (ce *TestConsensusEngine) ParseFutureBlock(ctx context.Context) {
	tBlk := &TestBlock{
		PrntID: ids.GenerateTestID(),
		Tmstmp: math.MaxInt64,
		Hght:   math.MaxUint64,
	}
	blk, err := ce.vm.ParseBlock(ctx, tBlk.GetBytes())
	ce.require.NoError(err)
	ce.require.Equal(tBlk.GetID(), blk.ID())
	ce.require.Equal(tBlk.GetParent(), blk.Parent())
	ce.require.Equal(time.UnixMilli(tBlk.GetTimestamp()), blk.Timestamp())
	ce.require.Equal(tBlk.GetHeight(), blk.Height())
}

func (ce *TestConsensusEngine) ParseAndVerifyNewBlock(ctx context.Context, parent *Block[*TestBlock, *TestBlock, *TestBlock]) *Block[*TestBlock, *TestBlock, *TestBlock] {
	newBlk := NewTestBlockFromParent(parent.Input)
	parsedBlk, err := ce.vm.VM.ParseBlock(ctx, newBlk.GetBytes())
	ce.require.NoError(err)
	ce.require.Equal(newBlk.GetID(), parsedBlk.ID())
	ce.verifyValidBlock(ctx, parsedBlk)
	return parsedBlk
}

func (ce *TestConsensusEngine) ParseAndVerifyNewRandomBlock(ctx context.Context) *Block[*TestBlock, *TestBlock, *TestBlock] {
	blk, ok := ce.selectRandomVerifiedBlock()
	if !ok {
		blk = ce.lastAccepted
	}

	return ce.ParseAndVerifyNewBlock(ctx, blk)
}

func (ce *TestConsensusEngine) ParseVerifiedBlk(ctx context.Context) (*Block[*TestBlock, *TestBlock, *TestBlock], bool) {
	blk, ok := ce.selectRandomVerifiedBlock()
	if !ok {
		return nil, false
	}

	parsedBlk, err := ce.vm.ParseBlock(ctx, blk.Bytes())
	ce.require.NoError(err)
	ce.require.Equal(blk.ID(), parsedBlk.ID())
	ce.require.Equal(blk, parsedBlk)
	return blk, true
}

func (ce *TestConsensusEngine) ParseAndVerifyInvalidBlock(ctx context.Context) {
	blk, ok := ce.selectRandomVerifiedBlock()
	if !ok {
		blk = ce.lastAccepted
	}

	newBlk := NewTestBlockFromParent(blk.Input)
	newBlk.Invalid = true
	parsedBlk, err := ce.vm.ParseBlock(ctx, newBlk.GetBytes())
	ce.require.NoError(err)
	ce.require.Equal(newBlk.GetID(), parsedBlk.ID())
	ce.require.ErrorIs(parsedBlk.Verify(ctx), errVerifyInvalidBlock)
	_, ok = ce.verified[parsedBlk.ID()]
	ce.require.False(ok)
}

func (ce *TestConsensusEngine) SwapRandomPreference(ctx context.Context) (*Block[*TestBlock, *TestBlock, *TestBlock], *Block[*TestBlock, *TestBlock, *TestBlock], bool) {
	selectedBlk, ok := ce.selectRandomVerifiedBlock()
	if !ok {
		selectedBlk = ce.lastAccepted
	}
	oldPreference := ce.preferred
	newPreference := selectedBlk
	changed := ce.preferred.ID() != selectedBlk.ID()
	ce.preferred = selectedBlk
	ce.require.NoError(ce.vm.SetPreference(ctx, selectedBlk.ID()))
	return oldPreference, newPreference, changed
}

func (ce *TestConsensusEngine) SetPreference(ctx context.Context, blkID ids.ID) (*Block[*TestBlock, *TestBlock, *TestBlock], *Block[*TestBlock, *TestBlock, *TestBlock], bool) {
	selectedBlk, ok := ce.verified[blkID]
	if !ok && ce.lastAccepted.ID() == blkID {
		selectedBlk = ce.lastAccepted
		ok = true
	}
	ce.require.True(ok)

	oldPreference := ce.preferred
	newPreference := selectedBlk
	changed := ce.preferred.ID() != selectedBlk.ID()
	ce.preferred = selectedBlk
	ce.require.NoError(ce.vm.SetPreference(ctx, selectedBlk.ID()))
	return oldPreference, newPreference, changed
}

func (ce *TestConsensusEngine) AcceptNonPreferredBlock(ctx context.Context) {
	preferredChain := ce.getLastAcceptedToBlk(ctx, ce.preferred)
	preferredSet := set.NewSet[ids.ID](len(preferredChain))
	for _, blk := range preferredChain {
		preferredSet.Add(blk.ID())
	}

	var selectedBlk *Block[*TestBlock, *TestBlock, *TestBlock]
	for _, blk := range ce.verified {
		if !preferredSet.Contains(blk.ID()) {
			selectedBlk = blk
			break
		}
	}
	if selectedBlk == nil {
		ce.t.Log("no non-preferred block to accept")
		return
	}

	nonPreferredChain := ce.getLastAcceptedToBlk(ctx, selectedBlk)
	ce.acceptChain(ctx, nonPreferredChain)
	_, _, changedPref := ce.SetPreference(ctx, nonPreferredChain[len(nonPreferredChain)-1].ID())
	ce.require.True(changedPref)
}

func (ce *TestConsensusEngine) GetVerifiedBlock(ctx context.Context) (*Block[*TestBlock, *TestBlock, *TestBlock], bool) {
	selectedBlk, ok := ce.selectRandomVerifiedBlock()
	if !ok {
		return nil, false
	}

	blk, err := ce.vm.GetBlock(ctx, selectedBlk.ID())
	ce.require.NoError(err)
	ce.require.Equal(blk, selectedBlk)
	return selectedBlk, true
}

func (ce *TestConsensusEngine) GetLastAcceptedBlock(ctx context.Context) {
	blk, err := ce.vm.GetBlock(ctx, ce.lastAccepted.ID())
	ce.require.NoError(err)
	ce.require.Equal(blk.ID(), ce.lastAccepted.ID())
	ce.require.Equal(blk, ce.lastAccepted)
}

func (ce *TestConsensusEngine) StartStateSync(ctx context.Context, target *TestBlock) {
	ce.require.NoError(ce.vm.StartStateSync(ctx, target))
}

func (ce *TestConsensusEngine) FinishStateSync(ctx context.Context, blk *Block[*TestBlock, *TestBlock, *TestBlock]) {
	ce.vm.snowCtx.Lock.Lock()
	defer ce.vm.snowCtx.Lock.Unlock()

	blk.Input.outputPopulated = true
	blk.Input.acceptedPopulated = true
	blk.setAccepted(blk.Input, blk.Input)
	ce.require.NoError(ce.vm.FinishStateSync(ctx, blk.Input, blk.Output, blk.Accepted))
}

type step int

const (
	buildBlock step = iota
	acceptPreferredChain
	getAcceptedBlock
	parseFutureBlock
	parseAndVerifyNewRandomBlock
	parseVerifiedBlock
	parseAndVerifyInvalidBlock
	swapRandomPreference
	acceptNonPreferredBlock
	getVerifiedBlock
	getLastAcceptedBlock
	maxStepValue
)

func (s step) String() string {
	switch s {
	case buildBlock:
		return "buildBlock"
	case acceptPreferredChain:
		return "acceptPreferredChain"
	case getAcceptedBlock:
		return "getAcceptedBlock"
	case parseFutureBlock:
		return "parseFutureBlock"
	case parseAndVerifyNewRandomBlock:
		return "parseAndVerifyNewRandomBlock"
	case parseVerifiedBlock:
		return "parseVerifiedBlock"
	case parseAndVerifyInvalidBlock:
		return "parseAndVerifyInvalidBlock"
	case swapRandomPreference:
		return "swapRandomPreference"
	case acceptNonPreferredBlock:
		return "acceptNonPreferredBlock"
	case getVerifiedBlock:
		return "getVerifiedBlock"
	case getLastAcceptedBlock:
		return "getLastAcceptedBlock"
	default:
		panic("invalid step")
	}
}

func (ce *TestConsensusEngine) Step(ctx context.Context, s step) {
	switch s {
	case buildBlock:
		ce.BuildBlock(ctx)
	case acceptPreferredChain:
		ce.AcceptPreferredChain(ctx)
	case getAcceptedBlock:
		ce.GetAcceptedBlock(ctx)
	case parseFutureBlock:
		ce.ParseFutureBlock(ctx)
	case parseAndVerifyNewRandomBlock:
		ce.ParseAndVerifyNewRandomBlock(ctx)
	case parseVerifiedBlock:
		ce.ParseVerifiedBlk(ctx)
	case parseAndVerifyInvalidBlock:
		ce.ParseAndVerifyInvalidBlock(ctx)
	case swapRandomPreference:
		ce.SwapRandomPreference(ctx)
	case acceptNonPreferredBlock:
		ce.AcceptNonPreferredBlock(ctx)
	case getVerifiedBlock:
		ce.GetVerifiedBlock(ctx)
	case getLastAcceptedBlock:
		ce.GetLastAcceptedBlock(ctx)
	default:
		// No such step, leave to fuzzer to realize this.
	}
}

func TestBuildAndAcceptBlock(t *testing.T) {
	ctx := context.Background()

	ce := NewTestConsensusEngine(t, &TestBlock{outputPopulated: true, acceptedPopulated: true})

	blk1, ok := ce.BuildBlock(ctx)
	ce.require.True(ok)
	ce.require.Equal(uint64(1), blk1.Height())
	ce.require.Equal(uint64(1), ce.preferred.Height())

	acceptedTip, ok := ce.AcceptPreferredChain(ctx)
	ce.require.True(ok)
	ce.require.Equal(acceptedTip.ID(), blk1.ID())

	ce.require.Empty(ce.verified)
}

func TestBuildAndAcceptChain(t *testing.T) {
	ctx := context.Background()

	ce := NewTestConsensusEngine(t, &TestBlock{outputPopulated: true, acceptedPopulated: true})

	blk1, ok := ce.BuildBlock(ctx)
	ce.require.True(ok)
	ce.require.Equal(uint64(1), blk1.Height())
	blk2, ok := ce.BuildBlock(ctx)
	ce.require.True(ok)
	ce.require.Equal(uint64(2), blk2.Height())
	ce.require.Equal(blk2.ID(), ce.preferred.ID())
	acceptedTip, ok := ce.AcceptPreferredChain(ctx)
	ce.require.True(ok)
	ce.require.Equal(acceptedTip.ID(), blk2.ID())

	ce.require.Empty(ce.verified)
}

func TestParseAndAcceptBlock(t *testing.T) {
	ctx := context.Background()

	ce := NewTestConsensusEngine(t, &TestBlock{outputPopulated: true, acceptedPopulated: true})

	blk1 := ce.ParseAndVerifyNewRandomBlock(ctx)
	ce.require.Equal(uint64(1), blk1.Height())

	oldPref, newPref, changed := ce.SetPreference(ctx, blk1.ID())
	ce.require.True(changed)
	ce.require.Equal(ce.lastAccepted, oldPref)
	ce.require.Equal(blk1, newPref)

	acceptedTip, ok := ce.AcceptPreferredChain(ctx)
	ce.require.True(ok)
	ce.require.Equal(acceptedTip.ID(), blk1.ID())

	ce.require.Empty(ce.verified)
}

func TestParseAndAcceptChain(t *testing.T) {
	ctx := context.Background()

	ce := NewTestConsensusEngine(t, &TestBlock{outputPopulated: true, acceptedPopulated: true})

	blk0 := ce.lastAccepted
	blk1 := ce.ParseAndVerifyNewRandomBlock(ctx)
	ce.require.Equal(uint64(1), blk1.Height())

	oldPref, newPref, changed := ce.SetPreference(ctx, blk1.ID())
	ce.require.True(changed)
	ce.require.Equal(blk0, oldPref)
	ce.require.Equal(blk1, newPref)
	ce.require.Equal(uint64(1), ce.preferred.Height())

	blk2 := ce.ParseAndVerifyNewRandomBlock(ctx)
	ce.require.Equal(uint64(2), blk2.Height())

	oldPref, newPref, changed = ce.SetPreference(ctx, blk2.ID())
	ce.require.True(changed)
	ce.require.Equal(blk1, oldPref)
	ce.require.Equal(blk2, newPref)

	acceptedTip, ok := ce.AcceptPreferredChain(ctx)
	ce.require.True(ok)
	ce.require.Equal(acceptedTip.ID(), blk2.ID())

	ce.require.Empty(ce.verified)
}

func TestBuild_Parse_Get_Accept(t *testing.T) {
	ctx := context.Background()

	ce := NewTestConsensusEngine(t, &TestBlock{outputPopulated: true, acceptedPopulated: true})

	blk1, ok := ce.BuildBlock(ctx)
	ce.require.True(ok)

	parsedBlk, ok := ce.ParseVerifiedBlk(ctx)
	ce.require.True(ok)
	ce.require.Equal(blk1.ID(), parsedBlk.ID())

	gotBlk, ok := ce.GetVerifiedBlock(ctx)
	ce.require.True(ok)
	ce.require.Equal(blk1.ID(), gotBlk.ID())

	acceptedTip, ok := ce.AcceptPreferredChain(ctx)
	ce.require.True(ok)
	ce.require.Equal(acceptedTip.ID(), blk1.ID())

	ce.require.Empty(ce.verified)
}

func TestBuild_ParseAndExtendPreferredChain_Accept(t *testing.T) {
	ctx := context.Background()

	ce := NewTestConsensusEngine(t, &TestBlock{outputPopulated: true, acceptedPopulated: true})

	blk1, ok := ce.BuildBlock(ctx)
	ce.require.True(ok)

	blk2 := ce.ParseAndVerifyNewBlock(ctx, blk1)
	ce.require.Equal(uint64(2), blk2.Height())
	ce.SetPreference(ctx, blk2.ID())

	acceptedTip, ok := ce.AcceptPreferredChain(ctx)
	ce.require.True(ok)
	ce.require.Equal(acceptedTip.ID(), blk2.ID())

	ce.require.Empty(ce.verified)
}

func TestConflictingChains(t *testing.T) {
	ctx := context.Background()

	ce := NewTestConsensusEngine(t, &TestBlock{outputPopulated: true, acceptedPopulated: true})
	genesis := ce.lastAccepted

	builtBlk1, ok := ce.BuildBlock(ctx)
	ce.require.True(ok)
	ce.require.Equal(uint64(1), builtBlk1.Height())

	builtBlk2, ok := ce.BuildBlock(ctx)
	ce.require.True(ok)
	ce.require.Equal(uint64(2), builtBlk2.Height())

	parsedBlk1 := ce.ParseAndVerifyNewBlock(ctx, genesis)
	ce.require.Equal(uint64(1), parsedBlk1.Height())
	parsedBlk2 := ce.ParseAndVerifyNewBlock(ctx, parsedBlk1)
	ce.require.Equal(uint64(2), parsedBlk2.Height())
	ce.SetPreference(ctx, parsedBlk2.ID())

	acceptedTip, ok := ce.AcceptPreferredChain(ctx)
	ce.require.True(ok)
	ce.require.Equal(acceptedTip.ID(), parsedBlk2.ID())

	ce.require.Empty(ce.verified)
}

func TestBuildBlockWithContext(t *testing.T) {
	tests := []struct {
		name          string
		buildContext  *block.Context
		verifyContext *block.Context
		expectedErr   error
	}{
		{
			name: "build = nil, verify = nil",
		},
		{
			name:          "build = 1, verify = nil",
			verifyContext: &block.Context{PChainHeight: 1},
			expectedErr:   errMismatchedPChainContext,
		},
		{
			name:          "build = 1, verify = 2",
			buildContext:  &block.Context{PChainHeight: 1},
			verifyContext: &block.Context{PChainHeight: 2},
			expectedErr:   errMismatchedPChainContext,
		},
		{
			name:          "build = 1, verify = 1",
			buildContext:  &block.Context{PChainHeight: 1},
			verifyContext: &block.Context{PChainHeight: 1},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()

			ce := NewTestConsensusEngine(t, &TestBlock{outputPopulated: true, acceptedPopulated: true})

			blk, err := ce.vm.VM.BuildBlockWithContext(ctx, test.buildContext)
			ce.require.NoError(err)
			ce.require.Equal(test.buildContext, blk.Input.GetContext())

			ce.require.ErrorIs(blk.VerifyWithContext(ctx, test.verifyContext), test.expectedErr)
		})
	}
}

func TestVerifyBlockWithContext(t *testing.T) {
	tests := []struct {
		name            string
		suppliedContext *block.Context
		verifyContext   *block.Context
		expectedErr     error
	}{
		{
			name: "build = nil, verify = nil",
		},
		{
			name:          "build = 1, verify = nil",
			verifyContext: &block.Context{PChainHeight: 1},
			expectedErr:   errMismatchedPChainContext,
		},
		{
			name:            "build = 1, verify = 2",
			suppliedContext: &block.Context{PChainHeight: 1},
			verifyContext:   &block.Context{PChainHeight: 2},
			expectedErr:     errMismatchedPChainContext,
		},
		{
			name:            "build = 1, verify = 1",
			suppliedContext: &block.Context{PChainHeight: 1},
			verifyContext:   &block.Context{PChainHeight: 1},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()

			ce := NewTestConsensusEngine(t, &TestBlock{outputPopulated: true, acceptedPopulated: true})

			testBlk := NewTestBlockFromParentWithContext(ce.lastAccepted.Input, test.suppliedContext)

			blk, err := ce.vm.VM.ParseBlock(ctx, testBlk.GetBytes())
			ce.require.NoError(err)
			ce.require.Equal(test.suppliedContext, blk.Input.GetContext())

			ce.require.ErrorIs(blk.VerifyWithContext(ctx, test.verifyContext), test.expectedErr)
		})
	}
}

func TestDynamicStateSyncTransition_NoPending(t *testing.T) {
	ctx := context.Background()

	// Create consensus engine in dynamic state sync mode.
	ce := NewTestConsensusEngine(t, &TestBlock{})
	ce.StartStateSync(ctx, ce.lastAccepted.Input)

	// Parse and verify a new block, which should be a pass through.
	blk1 := ce.ParseAndVerifyNewRandomBlock(ctx)
	ce.SetPreference(ctx, blk1.ID())
	ce.require.Equal(uint64(1), blk1.Height())

	acceptedTip, ok := ce.AcceptPreferredChain(ctx)
	ce.require.True(ok)
	ce.require.Equal(acceptedTip.ID(), blk1.ID())

	// Tip should not be verified/accepted, since it was handled prior
	// to the VM being marked ready.
	ce.require.False(acceptedTip.verified)
	ce.require.False(acceptedTip.accepted)

	// Mark the VM ready and fully populate the last accepted block.
	ce.FinishStateSync(ctx, acceptedTip)

	ce.ParseAndVerifyInvalidBlock(ctx)

	blk2 := ce.ParseAndVerifyNewRandomBlock(ctx)
	ce.require.Equal(uint64(2), blk2.Height())
	ce.SetPreference(ctx, blk2.ID())

	ce.ParseAndVerifyInvalidBlock(ctx)
	updatedAcceptedTip, ok := ce.AcceptPreferredChain(ctx)
	ce.require.True(ok)
	ce.require.Equal(updatedAcceptedTip.ID(), blk2.ID())
}

func TestDynamicStateSyncTransition_PendingTree_AcceptSingleBlock(t *testing.T) {
	ctx := context.Background()

	// Create consensus engine in dynamic state sync mode.
	ce := NewTestConsensusEngine(t, &TestBlock{})
	ce.StartStateSync(ctx, ce.lastAccepted.Input)

	parent := ce.lastAccepted
	// Parse and verify a new block, which should be a pass through.
	blk1 := ce.ParseAndVerifyNewBlock(ctx, parent)
	ce.SetPreference(ctx, blk1.ID())
	ce.require.Equal(uint64(1), blk1.Height())

	ce.FinishStateSync(ctx, ce.lastAccepted)

	acceptedTip, ok := ce.AcceptPreferredChain(ctx)
	ce.require.True(ok)
	ce.require.Equal(acceptedTip.ID(), blk1.ID())
}

func TestDynamicStateSyncTransition_PendingTree_AcceptChain(t *testing.T) {
	ctx := context.Background()

	ce := NewTestConsensusEngine(t, &TestBlock{})
	ce.StartStateSync(ctx, ce.lastAccepted.Input)

	parent := ce.lastAccepted
	// Parse and verify a new block, which should be a pass through.
	blk1 := ce.ParseAndVerifyNewBlock(ctx, parent)
	ce.SetPreference(ctx, blk1.ID())
	ce.require.Equal(uint64(1), blk1.Height())

	blk2 := ce.ParseAndVerifyNewBlock(ctx, blk1)
	ce.SetPreference(ctx, blk2.ID())
	ce.require.Equal(uint64(2), blk2.Height())

	ce.FinishStateSync(ctx, ce.lastAccepted)

	acceptedTip, ok := ce.AcceptPreferredChain(ctx)
	ce.require.True(ok)
	ce.require.Equal(acceptedTip.ID(), blk2.ID())
}

func TestDynamicStateSyncTransition_PendingTree_VerifySingleBlock(t *testing.T) {
	ctx := context.Background()

	ce := NewTestConsensusEngine(t, &TestBlock{})
	ce.StartStateSync(ctx, ce.lastAccepted.Input)

	parent := ce.lastAccepted
	// Parse and verify a new block, which should be a pass through.
	blk1 := ce.ParseAndVerifyNewBlock(ctx, parent)
	ce.SetPreference(ctx, blk1.ID())
	ce.require.Equal(uint64(1), blk1.Height())

	ce.FinishStateSync(ctx, ce.lastAccepted)

	blk2 := ce.ParseAndVerifyNewBlock(ctx, blk1)
	ce.SetPreference(ctx, blk2.ID())

	acceptedTip, ok := ce.AcceptPreferredChain(ctx)
	ce.require.True(ok)
	ce.require.Equal(acceptedTip.ID(), blk2.ID())
}

func TestDynamicStateSyncTransition_PendingTree_VerifyChain(t *testing.T) {
	ctx := context.Background()

	ce := NewTestConsensusEngine(t, &TestBlock{})
	ce.StartStateSync(ctx, ce.lastAccepted.Input)

	parent := ce.lastAccepted
	// Parse and verify a new block, which should be a pass through.
	blk1 := ce.ParseAndVerifyNewBlock(ctx, parent)
	ce.SetPreference(ctx, blk1.ID())
	ce.require.Equal(uint64(1), blk1.Height())

	ce.FinishStateSync(ctx, ce.lastAccepted)

	blk2 := ce.ParseAndVerifyNewBlock(ctx, blk1)
	ce.SetPreference(ctx, blk2.ID())

	blk3 := ce.ParseAndVerifyNewBlock(ctx, blk2)
	ce.SetPreference(ctx, blk3.ID())

	acceptedTip, ok := ce.AcceptPreferredChain(ctx)
	ce.require.True(ok)
	ce.require.Equal(acceptedTip.ID(), blk3.ID())
}

func TestDynamicStateSyncTransition_PendingTree_VerifyBlockWithInvalidAncestor(t *testing.T) {
	ctx := context.Background()

	ce := NewTestConsensusEngine(t, &TestBlock{})
	ce.StartStateSync(ctx, ce.lastAccepted.Input)

	// Check health - should be unhealthy during state sync
	details, err := ce.vm.HealthCheck(ctx)
	ce.require.ErrorIs(err, errVMNotReady)
	ce.require.Equal(map[string]interface{}{
		vmReadinessHealthChecker: false,
	}, details)

	parent := ce.lastAccepted
	invalidTestBlock1 := NewTestBlockFromParent(parent.Input)
	invalidTestBlock1.Invalid = true

	parsedBlk1, err := ce.vm.VM.ParseBlock(ctx, invalidTestBlock1.GetBytes())
	ce.require.NoError(err)
	ce.verifyValidBlock(ctx, parsedBlk1)

	invalidTestBlock2 := NewTestBlockFromParent(invalidTestBlock1)
	invalidTestBlock2.Invalid = true

	parsedBlk2, err := ce.vm.VM.ParseBlock(ctx, invalidTestBlock2.GetBytes())
	ce.require.NoError(err)
	ce.verifyValidBlock(ctx, parsedBlk2)

	ce.FinishStateSync(ctx, ce.lastAccepted)

	// Check health - should be unhealthy due to unresolved blocks
	details, err = ce.vm.HealthCheck(ctx)
	ce.require.ErrorIs(err, errUnresolvedBlocks)
	ce.require.Equal(map[string]any{
		vmReadinessHealthChecker:      true,
		unresolvedBlocksHealthChecker: 2,
	}, details)

	// Construct a new child of the invalid block at depth 1 marked as processing
	invalidatedChildTestBlock1 := NewTestBlockFromParent(invalidTestBlock1)
	invalidatedChildBlock1, err := ce.vm.ParseBlock(ctx, invalidatedChildTestBlock1.GetBytes())
	ce.require.NoError(err)

	invalidatedChildBlock1Err := invalidatedChildBlock1.Verify(ctx)
	ce.require.ErrorIs(invalidatedChildBlock1Err, errParentFailedVerification)

	// Construct a new child of the invalid block at depth 2 marked as processing
	// This tests that if a parent block fails verification, a re-processing child
	// will also fail verification after transitioning out of state sync.
	invalidatedChildTestBlock2 := NewTestBlockFromParent(invalidTestBlock2)
	invalidatedChildBlock2, err := ce.vm.ParseBlock(ctx, invalidatedChildTestBlock2.GetBytes())
	ce.require.NoError(err)

	invalidatedChildBlk2 := invalidatedChildBlock2.Verify(ctx)
	ce.require.ErrorIs(invalidatedChildBlk2, errParentFailedVerification)

	// Accept a new block to reject the invalid chain
	// Note: consensus only rejects blocks after accepting a conflict, so we
	// mimic this behavior here.
	validBlk1 := ce.ParseAndVerifyNewBlock(ctx, ce.lastAccepted)
	ce.SetPreference(ctx, validBlk1.ID())

	acceptedTip, ok := ce.AcceptPreferredChain(ctx)
	ce.require.True(ok)
	ce.require.Equal(acceptedTip.ID(), validBlk1.ID())

	details, err = ce.vm.HealthCheck(ctx)
	ce.require.NoError(err)
	ce.require.Equal(map[string]any{
		vmReadinessHealthChecker:      true,
		unresolvedBlocksHealthChecker: 0,
	}, details)
}

func TestDynamicStateSync_FinishOnAcceptedAncestor(t *testing.T) {
	ctx := context.Background()

	// Create consensus engine in dynamic state sync mode.
	ce := NewTestConsensusEngine(t, &TestBlock{})
	ce.StartStateSync(ctx, ce.lastAccepted.Input)

	notReadyLastAccepted := ce.lastAccepted

	// Parse and verify a new block, which should be a pass through.
	blk1 := ce.ParseAndVerifyNewRandomBlock(ctx)
	ce.SetPreference(ctx, blk1.ID())
	ce.require.Equal(uint64(1), blk1.Height())

	acceptedTip, ok := ce.AcceptPreferredChain(ctx)
	ce.require.True(ok)
	ce.require.Equal(acceptedTip.ID(), blk1.ID())

	// Tip should not be verified/accepted, since it was handled prior
	// to the VM being marked ready.
	ce.require.False(acceptedTip.verified)
	ce.require.False(acceptedTip.accepted)

	// Mark the VM ready and set the last accepted block to an ancestor
	// of the current last accepted block
	ce.FinishStateSync(ctx, notReadyLastAccepted)

	ce.ParseAndVerifyInvalidBlock(ctx)

	blk2 := ce.ParseAndVerifyNewRandomBlock(ctx)
	ce.require.Equal(uint64(2), blk2.Height())
	ce.SetPreference(ctx, blk2.ID())

	ce.ParseAndVerifyInvalidBlock(ctx)
	updatedAcceptedTip, ok := ce.AcceptPreferredChain(ctx)
	ce.require.True(ok)
	ce.require.Equal(updatedAcceptedTip.ID(), blk2.ID())
}

func FuzzSnowVM(f *testing.F) {
	for i := byte(0); i < 100; i++ {
		randomSteps := hashing.ComputeHash256([]byte{i})
		f.Add(int64(i), randomSteps)
	}

	maxFuzzSteps := 50
	// Cap the number of steps to take by using byte as the type
	f.Fuzz(func(t *testing.T, randSource int64, byteSteps []byte) {
		rand := rand.New(rand.NewSource(randSource)) //nolint:gosec

		ctx := context.Background()
		ce := NewTestConsensusEngineWithRand(t, rand, &TestBlock{outputPopulated: true, acceptedPopulated: true})

		byteSteps = byteSteps[:min(maxFuzzSteps, len(byteSteps))]
		for _, byteStep := range byteSteps {
			selectedStep := step(byteStep % byte(maxStepValue))
			t.Logf("Step: %s", selectedStep)
			ce.Step(ctx, selectedStep)
		}
	})
}
