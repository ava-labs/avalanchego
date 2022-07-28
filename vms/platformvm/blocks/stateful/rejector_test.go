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
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestRejectBlock(t *testing.T) {
	type test struct {
		name         string
		newBlockFunc func() (stateless.Block, error)
		rejectFunc   func(*rejector, stateless.Block) error
	}

	tests := []test{
		{
			name: "proposal block",
			newBlockFunc: func() (stateless.Block, error) {
				return stateless.NewProposalBlock(
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
			},
			rejectFunc: func(r *rejector, b stateless.Block) error {
				return r.ProposalBlock(b.(*stateless.ProposalBlock))
			},
		},
		{
			name: "atomic block",
			newBlockFunc: func() (stateless.Block, error) {
				return stateless.NewAtomicBlock(
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
			},
			rejectFunc: func(r *rejector, b stateless.Block) error {
				return r.AtomicBlock(b.(*stateless.AtomicBlock))
			},
		},
		{
			name: "standard block",
			newBlockFunc: func() (stateless.Block, error) {
				return stateless.NewStandardBlock(
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
			},
			rejectFunc: func(r *rejector, b stateless.Block) error {
				return r.StandardBlock(b.(*stateless.StandardBlock))
			},
		},
		{
			name: "commit",
			newBlockFunc: func() (stateless.Block, error) {
				return stateless.NewCommitBlock(
					ids.GenerateTestID(),
					1,
				)
			},
			rejectFunc: func(r *rejector, blk stateless.Block) error {
				return r.CommitBlock(blk.(*stateless.CommitBlock))
			},
		},
		{
			name: "abort",
			newBlockFunc: func() (stateless.Block, error) {
				return stateless.NewAbortBlock(
					ids.GenerateTestID(),
					1,
				)
			},
			rejectFunc: func(r *rejector, blk stateless.Block) error {
				return r.AbortBlock(blk.(*stateless.AbortBlock))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert := assert.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			blk, err := tt.newBlockFunc()
			assert.NoError(err)

			mempool := mempool.NewMockMempool(ctrl)
			stateVersions := state.NewMockVersions(ctrl)
			state := state.NewMockState(ctrl)
			blkIDToState := map[ids.ID]*blockState{
				blk.Parent(): nil,
				blk.ID():     nil,
			}
			rejector := &rejector{
				backend: &backend{
					ctx: &snow.Context{
						Log: logging.NoLog{},
					},
					blkIDToState:  blkIDToState,
					Mempool:       mempool,
					stateVersions: stateVersions,
					state:         state,
				},
			}

			// Set expected calls on dependencies.
			for _, tx := range blk.BlockTxs() {
				mempool.EXPECT().Add(tx).Return(nil).Times(1)
			}
			gomock.InOrder(
				stateVersions.EXPECT().DeleteState(blk.ID()).Times(1),
				state.EXPECT().AddStatelessBlock(blk, choices.Rejected).Times(1),
				state.EXPECT().Commit().Return(nil).Times(1),
			)

			err = tt.rejectFunc(rejector, blk)
			assert.NoError(err)
			// Make sure block and its parent are removed from the state map.
			assert.NotContains(rejector.blkIDToState, blk.ID())
		})
	}
}
