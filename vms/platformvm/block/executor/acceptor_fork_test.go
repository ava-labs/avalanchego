// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/chains/atomic/atomicmock"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/statetest"
	"github.com/ava-labs/avalanchego/vms/platformvm/validators"
)

func TestRecordForkHeight(t *testing.T) {
	forkTime := time.Unix(1_800_000_000, 0)

	standard := func(t *testing.T, ts time.Time, height uint64) platform.Block {
		blk, err := platform.NewBanffStandardBlock(ts, ids.GenerateTestID(), height, nil)
		require.NoError(t, err, "NewBanffStandardBlock()")
		return blk
	}

	tests := []struct {
		name       string
		forkTime   time.Time
		blocks     []platform.Block
		wantHeight uint64
		wantOK     bool
	}{
		{name: "fork mode off", blocks: []platform.Block{standard(t, forkTime, 5)}},
		{name: "before fork time", forkTime: forkTime, blocks: []platform.Block{standard(t, forkTime.Add(-time.Second), 5)}},
		{name: "at fork time", forkTime: forkTime, blocks: []platform.Block{standard(t, forkTime, 5)}, wantHeight: 5, wantOK: true},
		{
			name:     "first one wins",
			forkTime: forkTime,
			blocks: []platform.Block{
				standard(t, forkTime.Add(-time.Second), 4),
				standard(t, forkTime, 5),
				standard(t, forkTime.Add(time.Second), 6),
			},
			wantHeight: 5,
			wantOK:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := statetest.New(t, statetest.Config{})
			var reported []uint64
			a := &acceptor{
				backend: &backend{
					state: s,
					ctx:   &snow.Context{Log: logging.NoLog{}},
				},
				forkTime:     tt.forkTime,
				onForkHeight: func(h uint64) { reported = append(reported, h) },
			}
			for _, blk := range tt.blocks {
				a.recordForkHeight(blk)
			}

			height, ok := s.GetForkHeight()
			require.Equal(t, tt.wantOK, ok, "GetForkHeight() ok")
			require.Equal(t, tt.wantHeight, height, "GetForkHeight()")
			if tt.wantOK {
				require.Equal(t, []uint64{tt.wantHeight}, reported, "onForkHeight calls")
			} else {
				require.Empty(t, reported, "onForkHeight calls")
			}
		})
	}
}

// TestRecordForkHeightThroughOptionBlock accepts a proposal block and its
// commit block, both timestamped at the fork time, through the acceptor's
// public path. H_fork must be the proposal's height: the proposal is accepted
// (commonAccept) before its option, in the same commit.
func TestRecordForkHeightThroughOptionBlock(t *testing.T) {
	forkTime := time.Unix(1_800_000_000, 0)
	ctrl := gomock.NewController(t)

	db := memdb.New()
	s := statetest.New(t, statetest.Config{DB: db})
	sharedMemory := atomicmock.NewSharedMemory(ctrl)
	sharedMemory.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ map[ids.ID]*atomic.Requests, batches ...database.Batch) error {
			for _, b := range batches {
				if err := b.Write(); err != nil {
					return err
				}
			}
			return nil
		},
	).Times(1)

	var reported []uint64
	a := &acceptor{
		backend: &backend{
			lastAccepted: s.GetLastAccepted(),
			blkIDToState: make(map[ids.ID]*blockState),
			state:        s,
			ctx: &snow.Context{
				Log:          logging.NoLog{},
				SharedMemory: sharedMemory,
			},
		},
		metrics:      metrics.Noop,
		validators:   validators.NewManager(config.Internal{}, s, metrics.Noop, new(mockable.Clock)),
		forkTime:     forkTime,
		onForkHeight: func(h uint64) { reported = append(reported, h) },
	}

	proposalTx := &platform.Tx{
		Unsigned: &platform.RewardValidatorTx{TxID: ids.GenerateTestID()},
	}
	require.NoError(t, proposalTx.Initialize(platform.Codec), "proposalTx.Initialize()")

	const proposalHeight = 1
	proposal, err := platform.NewBanffProposalBlock(forkTime, s.GetLastAccepted(), proposalHeight, proposalTx, nil)
	require.NoError(t, err, "NewBanffProposalBlock()")
	commit, err := platform.NewBanffCommitBlock(forkTime, proposal.ID(), proposalHeight+1)
	require.NoError(t, err, "NewBanffCommitBlock()")

	onCommitState, err := state.NewDiffOn(s, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(t, err, "NewDiffOn(onCommit)")
	onAbortState, err := state.NewDiffOn(s, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(t, err, "NewDiffOn(onAbort)")
	a.blkIDToState[proposal.ID()] = &blockState{
		proposalBlockState: proposalBlockState{
			onCommitState: onCommitState,
			onAbortState:  onAbortState,
		},
		statelessBlock: proposal,
		timestamp:      forkTime,
		atomicRequests: make(map[ids.ID]*atomic.Requests),
		metrics:        metrics.Block{Block: proposal},
	}
	a.blkIDToState[commit.ID()] = &blockState{
		statelessBlock: commit,
		onAcceptState:  onCommitState,
		timestamp:      forkTime,
		metrics:        metrics.Block{Block: commit},
	}

	require.NoError(t, a.BanffProposalBlock(proposal), "BanffProposalBlock()")
	require.Empty(t, reported, "onForkHeight calls after proposal accept")

	require.NoError(t, a.BanffCommitBlock(commit), "BanffCommitBlock()")
	height, ok := s.GetForkHeight()
	require.True(t, ok, "GetForkHeight() ok")
	require.Equal(t, uint64(proposalHeight), height, "GetForkHeight()")
	require.Equal(t, []uint64{proposalHeight}, reported, "onForkHeight calls")
	require.Equal(t, commit.ID(), s.GetLastAccepted(), "GetLastAccepted()")

	require.NoError(t, s.Close(), "Close()")
	reloaded := statetest.New(t, statetest.Config{DB: db})
	height, ok = reloaded.GetForkHeight()
	require.True(t, ok, "GetForkHeight() after reload ok")
	require.Equal(t, uint64(proposalHeight), height, "GetForkHeight() after reload")
	require.Equal(t, commit.ID(), reloaded.GetLastAccepted(), "GetLastAccepted() after reload")
}
