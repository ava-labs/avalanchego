// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/fork"
	"github.com/ava-labs/avalanchego/fork/forktest"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/proposervm/acp181"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer/proposermock"

	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

var testForkTime = snowmantest.GenesisTimestamp.Add(time.Hour)

// enableTestFork turns on fork mode for an already-initialized proVM.
func enableTestFork(t *testing.T, proVM *VM, nodeIDs ...ids.NodeID) *fork.Status {
	cfg := forktest.NewConfig(t, testForkTime, nodeIDs...)
	status := fork.NewStatus(cfg, time.Now)
	proVM.Fork = cfg
	proVM.ForkStatus = status
	require.NoError(t, proVM.initFork(), "initFork()")
	return status
}

// buildAcceptedParent builds and accepts the first post-fork block at [ts].
func buildAcceptedParent(t *testing.T, coreVM *fullVM, proVM *VM, ts time.Time) (*postForkBlock, *snowmantest.Block) {
	innerParent := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return innerParent, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case innerParent.ID():
			return innerParent, nil
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, innerParent.Bytes()):
			return innerParent, nil
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}

	proVM.Set(ts)
	parent, err := proVM.BuildBlock(t.Context())
	require.NoError(t, err, "BuildBlock(parent)")
	require.NoError(t, parent.Verify(t.Context()), "parent.Verify()")
	require.NoError(t, parent.Accept(t.Context()), "parent.Accept()")
	require.NoError(t, proVM.SetPreference(t.Context(), parent.ID()), "SetPreference(parent)")

	postFork, ok := parent.(*postForkBlock)
	require.True(t, ok, "parent type")
	return postFork, innerParent
}

// makeChild crafts a child of [parent] at [ts], signed by this test's staking
// key if [signed].
func makeChild(t *testing.T, coreVM *fullVM, proVM *VM, parent *postForkBlock, innerParent *snowmantest.Block, ts time.Time, signed bool) snowman.Block {
	innerChild := snowmantest.BuildChild(innerParent)
	innerChild.TimestampV = ts
	parseParent := coreVM.ParseBlockF
	coreVM.ParseBlockF = func(ctx context.Context, b []byte) (snowman.Block, error) {
		if bytes.Equal(b, innerChild.Bytes()) {
			return innerChild, nil
		}
		return parseParent(ctx, b)
	}

	pChainHeight := parent.PChainHeight()
	epoch := acp181.NewEpoch(proVM.Upgrades, pChainHeight, parent.PChainEpoch(), parent.Timestamp(), ts)
	var (
		stateless statelessblock.SignedBlock
		err       error
	)
	if signed {
		stateless, err = statelessblock.Build(parent.ID(), ts, pChainHeight, epoch, pTestCert, innerChild.Bytes(), proVM.ctx.ChainID, pTestSigner)
	} else {
		stateless, err = statelessblock.BuildUnsigned(parent.ID(), ts, pChainHeight, epoch, innerChild.Bytes())
	}
	require.NoError(t, err, "build stateless child")

	child, err := proVM.ParseBlock(t.Context(), stateless.Bytes())
	require.NoError(t, err, "ParseBlock(child)")
	return child
}

func TestForkBlockVerification(t *testing.T) {
	other := ids.GenerateTestNodeID()
	tests := []struct {
		name        string
		state       snow.State
		childTime   time.Time
		signed      bool
		selfIsForkV bool
		wantErr     error
	}{
		{name: "pre-fork unsigned while bootstrapping", state: snow.Bootstrapping, childTime: testForkTime.Add(-time.Second)},
		{name: "post-fork unsigned while bootstrapping", state: snow.Bootstrapping, childTime: testForkTime, wantErr: errForkBlockUnsigned},
		{name: "post-fork wrong proposer while bootstrapping", state: snow.Bootstrapping, childTime: testForkTime, signed: true, wantErr: errUnexpectedProposer},
		{name: "post-fork fork proposer while bootstrapping", state: snow.Bootstrapping, childTime: testForkTime, signed: true, selfIsForkV: true},
		{name: "post-fork unsigned in normal op", state: snow.NormalOp, childTime: testForkTime, selfIsForkV: true, wantErr: errForkBlockUnsigned},
		{name: "post-fork wrong proposer in normal op", state: snow.NormalOp, childTime: testForkTime, signed: true, wantErr: errUnexpectedProposer},
		{name: "post-fork fork proposer in normal op", state: snow.NormalOp, childTime: testForkTime, signed: true, selfIsForkV: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
			defer func() {
				require.NoError(t, proVM.Shutdown(t.Context()), "Shutdown()")
			}()

			forkNode := other
			if tt.selfIsForkV {
				forkNode = proVM.ctx.NodeID
			}
			enableTestFork(t, proVM, forkNode)

			parent, innerParent := buildAcceptedParent(t, coreVM, proVM, testForkTime.Add(-10*time.Second))
			proVM.Set(tt.childTime)
			child := makeChild(t, coreVM, proVM, parent, innerParent, tt.childTime, tt.signed)

			proVM.consensusState = tt.state
			err := child.Verify(t.Context())
			require.ErrorIs(t, err, tt.wantErr, "child.Verify()")
		})
	}
}

func TestForkBuildAndRecordForkPoint(t *testing.T) {
	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(t, proVM.Shutdown(t.Context()), "Shutdown()")
	}()
	status := enableTestFork(t, proVM, proVM.ctx.NodeID)

	parent, innerParent := buildAcceptedParent(t, coreVM, proVM, testForkTime.Add(-10*time.Second))

	innerChild := snowmantest.BuildChild(innerParent)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return innerChild, nil
	}
	proVM.Set(testForkTime)
	child, err := proVM.BuildBlock(t.Context())
	require.NoError(t, err, "BuildBlock(child)")
	postForkChild, ok := child.(*postForkBlock)
	require.True(t, ok, "child type")
	require.Equal(t, proVM.ctx.NodeID, postForkChild.Proposer(), "child proposer")

	require.NoError(t, child.Verify(t.Context()), "child.Verify()")
	require.NoError(t, child.Accept(t.Context()), "child.Accept()")

	want := fork.ForkPoint{BlockID: parent.ID(), Height: parent.Height()}
	require.Equal(t, map[string]fork.ForkPoint{proVM.ctx.ChainID.String(): want}, status.Report().ForkPoints, "fork points after accept")

	// A restart reloads the fork point from the database.
	restarted := fork.NewStatus(proVM.Fork, time.Now)
	proVM.ForkStatus = restarted
	proVM.forkPointKnown = false
	require.NoError(t, proVM.initFork(), "initFork() after restart")
	require.Equal(t, map[string]fork.ForkPoint{proVM.ctx.ChainID.String(): want}, restarted.Report().ForkPoints, "fork points after restart")
}

func TestForkBuildRejectedForNonForkValidator(t *testing.T) {
	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(t, proVM.Shutdown(t.Context()), "Shutdown()")
	}()
	enableTestFork(t, proVM, ids.GenerateTestNodeID())

	_, innerParent := buildAcceptedParent(t, coreVM, proVM, testForkTime.Add(-10*time.Second))
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return snowmantest.BuildChild(innerParent), nil
	}
	proVM.Set(testForkTime)
	_, err := proVM.BuildBlock(t.Context())
	require.ErrorIs(t, err, errUnexpectedProposer, "BuildBlock() by a non-fork validator")
}

func TestPostDurangoSlotTimeAcrossFork(t *testing.T) {
	self := ids.GenerateTestNodeID()
	tests := []struct {
		name            string
		forkSet         []ids.NodeID
		parentTime      time.Time
		normalDelay     time.Duration // returned by the source windower; 0 means not called
		wantTime        time.Time
		wantAtOrAfterTo time.Time // if non-zero, assert result >= this instead of equality
	}{
		{
			name:        "source slot before T wins",
			forkSet:     []ids.NodeID{self},
			parentTime:  testForkTime.Add(-12 * time.Second),
			normalDelay: 5 * time.Second,
			wantTime:    testForkTime.Add(-7 * time.Second),
		},
		{
			name:        "fork slot straddling T is clamped to T",
			forkSet:     []ids.NodeID{self},
			parentTime:  testForkTime.Add(-12 * time.Second),
			normalDelay: 30 * time.Second,
			wantTime:    testForkTime,
		},
		{
			name:       "parent at or after T uses the fork windower only",
			forkSet:    []ids.NodeID{self},
			parentTime: testForkTime,
			wantTime:   testForkTime,
		},
		{
			name:            "not a fork validator",
			forkSet:         []ids.NodeID{ids.GenerateTestNodeID()},
			parentTime:      testForkTime.Add(-12 * time.Second),
			normalDelay:     30 * time.Second,
			wantAtOrAfterTo: testForkTime,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			windower := proposermock.NewWindower(ctrl)
			if tt.normalDelay != 0 {
				windower.EXPECT().MinDelayForProposer(gomock.Any(), gomock.Any(), gomock.Any(), self, gomock.Any()).Return(tt.normalDelay, nil)
			}

			cfg := forktest.NewConfig(t, testForkTime, tt.forkSet...)
			vm := &VM{
				Config:   Config{Fork: cfg},
				ctx:      &snow.Context{NodeID: self, Log: logging.NoLog{}},
				Windower: windower,
			}
			vm.forkWindower = proposer.New(fork.NewStaticState(cfg), ids.Empty, ids.Empty, logging.NoLog{})

			slot := proposer.TimeToSlot(tt.parentTime, tt.parentTime)
			got, err := vm.getPostDurangoSlotTime(t.Context(), 10, 5, slot, tt.parentTime)
			require.NoError(t, err, "getPostDurangoSlotTime()")
			if !tt.wantAtOrAfterTo.IsZero() {
				require.False(t, got.Before(tt.wantAtOrAfterTo), "getPostDurangoSlotTime() = %s, want >= %s", got, tt.wantAtOrAfterTo)
				return
			}
			require.Equal(t, tt.wantTime, got, "getPostDurangoSlotTime()")
		})
	}
}
