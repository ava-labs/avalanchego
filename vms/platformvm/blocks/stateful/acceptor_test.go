// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/window"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/golang/mock/gomock"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func TestAcceptorVisitProposalBlock(t *testing.T) {
	assert := assert.New(t)

	blk, err := stateless.NewProposalBlock(
		ids.GenerateTestID(),
		1,
		&txs.Tx{
			Unsigned: &txs.AddDelegatorTx{
				// Without the line below, this function will error.
				RewardsOwner: &secp256k1fx.OutputOwners{},
			},
			Creds: []verify.Verifiable{},
		},
	)
	assert.NoError(err)

	metrics := metrics.Metrics{}
	err = metrics.Initialize("", prometheus.NewRegistry(), ids.Set{})
	assert.NoError(err)

	acceptor := &acceptor{
		backend: &backend{
			ctx: &snow.Context{
				Log: logging.NoLog{},
			},
		},
		metrics:          metrics,
		recentlyAccepted: nil,
	}

	err = acceptor.VisitProposalBlock(blk)
	assert.NoError(err)

	assert.Equal(blk.ID(), acceptor.backend.lastAccepted)
}

func TestAcceptorVisitAtomicBlock(t *testing.T) {
	assert := assert.New(t)

	metrics := metrics.Metrics{}
	err := metrics.Initialize("", prometheus.NewRegistry(), ids.Set{})
	assert.NoError(err)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	s := state.NewMockState(ctrl)
	sharedMemory := atomic.NewMockSharedMemory(ctrl)

	acceptor := &acceptor{
		backend: &backend{
			blkIDToState: make(map[ids.ID]*blockState),
			state:        s,
			ctx: &snow.Context{
				Log:          logging.NoLog{},
				SharedMemory: sharedMemory,
			},
		},
		metrics: metrics,
		recentlyAccepted: window.New(window.Config{
			Clock:   &mockable.Clock{},
			MaxSize: 1,
			TTL:     time.Hour,
		}),
	}

	blk, err := stateless.NewAtomicBlock(
		ids.GenerateTestID(),
		1,
		&txs.Tx{
			Unsigned: &txs.AddDelegatorTx{
				// Without the line below, this function will error.
				RewardsOwner: &secp256k1fx.OutputOwners{},
			},
			Creds: []verify.Verifiable{},
		},
	)
	assert.NoError(err)

	// Set expected calls on the state.
	// We should error after [commonAccept] is called.
	s.EXPECT().SetLastAccepted(blk.ID()).Times(1)
	s.EXPECT().SetHeight(blk.Height()).Times(1)
	s.EXPECT().AddStatelessBlock(blk, choices.Accepted).Times(1)

	err = acceptor.VisitAtomicBlock(blk)
	assert.Error(err, "should fail because the block isn't in the state map")

	// Set [blk]'s state in the map as though it had been verified.
	onAcceptState := state.NewMockDiff(ctrl)
	childID := ids.GenerateTestID()
	atomicRequests := map[ids.ID]*atomic.Requests{ids.GenerateTestID(): nil}
	acceptor.backend.blkIDToState[blk.ID()] = &blockState{
		onAcceptState:  onAcceptState,
		children:       []ids.ID{childID},
		atomicRequests: atomicRequests,
	}
	// Give [blk] a child.
	childOnAcceptState := state.NewMockDiff(ctrl)
	childOnAbortState := state.NewMockDiff(ctrl)
	childOnCommitState := state.NewMockDiff(ctrl)
	childState := &blockState{
		onAcceptState: childOnAcceptState,
		proposalBlockState: proposalBlockState{
			onAbortState:  childOnAbortState,
			onCommitState: childOnCommitState,
		},
	}
	acceptor.backend.blkIDToState[childID] = childState

	// Set expected calls on dependencies.
	s.EXPECT().SetLastAccepted(blk.ID()).Times(1)
	s.EXPECT().SetHeight(blk.Height()).Times(1)
	s.EXPECT().AddStatelessBlock(blk, choices.Accepted).Times(1)
	batch := database.NewMockBatch(ctrl)
	s.EXPECT().CommitBatch().Return(batch, nil).Times(1)
	s.EXPECT().Abort().Times(1)
	onAcceptState.EXPECT().Apply(s).Times(1)
	sharedMemory.EXPECT().Apply(atomicRequests, batch).Return(nil).Times(1)
	childOnAcceptState.EXPECT().Apply(s).Times(1)
	childOnAbortState.EXPECT().SetBase(gomock.Any()).Times(1)
	childOnCommitState.EXPECT().SetBase(gomock.Any()).Times(1)

	err = acceptor.VisitAtomicBlock(blk)
	assert.NoError(err)
}
