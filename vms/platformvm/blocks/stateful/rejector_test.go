// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/golang/mock/gomock"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func TestRejectorVisitProposalBlock(t *testing.T) {
	assert := assert.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

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

	mempool := mempool.NewMockMempool(ctrl)
	stateVersions := state.NewMockVersions(ctrl)
	state := state.NewMockState(ctrl)
	rejector := &rejector{
		backend: &backend{
			ctx: &snow.Context{
				Log: logging.NoLog{},
			},
			Mempool:       mempool,
			stateVersions: stateVersions,
			state:         state,
		},
	}

	// Set expected calls on dependencies.
	gomock.InOrder(
		mempool.EXPECT().Add(blk.Tx).Return(nil).Times(1),
		stateVersions.EXPECT().DeleteState(blk.ID()).Times(1),
		state.EXPECT().AddStatelessBlock(blk, choices.Rejected).Times(1),
		state.EXPECT().Commit().Return(nil).Times(1),
	)

	err = rejector.VisitProposalBlock(blk)
	assert.NoError(err)
	assert.NotContains(rejector.blkIDToState, blk.ID())
}

func TestRejectorVisitAtomicBlock(t *testing.T) {
	assert := assert.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

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

	metrics := metrics.Metrics{}
	err = metrics.Initialize("", prometheus.NewRegistry(), ids.Set{})
	assert.NoError(err)

	mempool := mempool.NewMockMempool(ctrl)
	stateVersions := state.NewMockVersions(ctrl)
	state := state.NewMockState(ctrl)
	rejector := &rejector{
		backend: &backend{
			ctx: &snow.Context{
				Log: logging.NoLog{},
			},
			Mempool:       mempool,
			stateVersions: stateVersions,
			state:         state,
		},
	}

	// Set expected calls on dependencies.
	gomock.InOrder(
		mempool.EXPECT().Add(blk.Tx).Return(nil).Times(1),
		stateVersions.EXPECT().DeleteState(blk.ID()).Times(1),
		state.EXPECT().AddStatelessBlock(blk, choices.Rejected).Times(1),
		state.EXPECT().Commit().Return(nil).Times(1),
	)

	err = rejector.VisitAtomicBlock(blk)
	assert.NoError(err)
	assert.NotContains(rejector.blkIDToState, blk.ID())
}

func TestRejectorVisitStandardBlock(t *testing.T) {
	assert := assert.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	blk, err := stateless.NewStandardBlock(
		ids.GenerateTestID(),
		1,
		[]*txs.Tx{
			{
				Unsigned: &txs.AddDelegatorTx{
					// Without the line below, this function will error.
					RewardsOwner: &secp256k1fx.OutputOwners{},
				},
				Creds: []verify.Verifiable{},
			},
		},
	)
	assert.NoError(err)

	metrics := metrics.Metrics{}
	err = metrics.Initialize("", prometheus.NewRegistry(), ids.Set{})
	assert.NoError(err)

	mempool := mempool.NewMockMempool(ctrl)
	stateVersions := state.NewMockVersions(ctrl)
	state := state.NewMockState(ctrl)
	rejector := &rejector{
		backend: &backend{
			ctx: &snow.Context{
				Log: logging.NoLog{},
			},
			Mempool:       mempool,
			stateVersions: stateVersions,
			state:         state,
		},
	}

	// Set expected calls on dependencies.
	for _, tx := range blk.Txs {
		mempool.EXPECT().Add(tx).Return(nil).Times(1)
	}

	gomock.InOrder(
		stateVersions.EXPECT().DeleteState(blk.ID()).Times(1),
		state.EXPECT().AddStatelessBlock(blk, choices.Rejected).Times(1),
		state.EXPECT().Commit().Return(nil).Times(1),
	)

	err = rejector.VisitStandardBlock(blk)
	assert.NoError(err)
	assert.NotContains(rejector.blkIDToState, blk.ID())
}

func TestRejectorVisitOptionBlock(t *testing.T) {
	type test struct {
		name       string
		newBlkFunc func() (stateless.Block, error)
		rejectFunc func(*rejector, interface{}) error
	}

	tests := []test{
		{
			name: "commit",
			newBlkFunc: func() (stateless.Block, error) {
				return stateless.NewCommitBlock(
					ids.GenerateTestID(),
					1,
				)
			},
			rejectFunc: func(r *rejector, blk interface{}) error {
				return r.VisitCommitBlock(blk.(*stateless.CommitBlock))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert := assert.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			blk, err := tt.newBlkFunc()
			assert.NoError(err)

			metrics := metrics.Metrics{}
			err = metrics.Initialize("", prometheus.NewRegistry(), ids.Set{})
			assert.NoError(err)

			mempool := mempool.NewMockMempool(ctrl)
			stateVersions := state.NewMockVersions(ctrl)
			state := state.NewMockState(ctrl)
			rejector := &rejector{
				backend: &backend{
					ctx: &snow.Context{
						Log: logging.NoLog{},
					},
					Mempool:       mempool,
					stateVersions: stateVersions,
					state:         state,
				},
			}

			// Set expected calls on dependencies.
			gomock.InOrder(
				stateVersions.EXPECT().DeleteState(blk.ID()).Times(1),
				state.EXPECT().AddStatelessBlock(blk, choices.Rejected).Times(1),
				state.EXPECT().Commit().Return(nil).Times(1),
			)

			err = tt.rejectFunc(rejector, blk)
			assert.NoError(err)
		})
	}
}
