// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestRejectBlock(t *testing.T) {
	type test struct {
		name         string
		newBlockFunc func() (blocks.Block, error)
		rejectFunc   func(*rejector, blocks.Block) error
	}

	tests := []test{
		{
			name: "proposal block",
			newBlockFunc: func() (blocks.Block, error) {
				return blocks.NewProposalBlock(
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
			rejectFunc: func(r *rejector, b blocks.Block) error {
				return r.ProposalBlock(b.(*blocks.ProposalBlock))
			},
		},
		{
			name: "atomic block",
			newBlockFunc: func() (blocks.Block, error) {
				return blocks.NewAtomicBlock(
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
			rejectFunc: func(r *rejector, b blocks.Block) error {
				return r.AtomicBlock(b.(*blocks.AtomicBlock))
			},
		},
		{
			name: "standard block",
			newBlockFunc: func() (blocks.Block, error) {
				return blocks.NewStandardBlock(
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
			rejectFunc: func(r *rejector, b blocks.Block) error {
				return r.StandardBlock(b.(*blocks.StandardBlock))
			},
		},
		{
			name: "commit",
			newBlockFunc: func() (blocks.Block, error) {
				return blocks.NewCommitBlock(
					ids.GenerateTestID(),
					1,
				)
			},
			rejectFunc: func(r *rejector, blk blocks.Block) error {
				return r.CommitBlock(blk.(*blocks.CommitBlock))
			},
		},
		{
			name: "abort",
			newBlockFunc: func() (blocks.Block, error) {
				return blocks.NewAbortBlock(
					ids.GenerateTestID(),
					1,
				)
			},
			rejectFunc: func(r *rejector, blk blocks.Block) error {
				return r.AbortBlock(blk.(*blocks.AbortBlock))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			blk, err := tt.newBlockFunc()
			require.NoError(err)

			mempool := mempool.NewMockMempool(ctrl)
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
					blkIDToState: blkIDToState,
					Mempool:      mempool,
					state:        state,
				},
			}

			// Set expected calls on dependencies.
			for _, tx := range blk.Txs() {
				mempool.EXPECT().Add(tx).Return(nil).Times(1)
			}
			gomock.InOrder(
				state.EXPECT().AddStatelessBlock(blk, choices.Rejected).Times(1),
				state.EXPECT().Commit().Return(nil).Times(1),
			)

			err = tt.rejectFunc(rejector, blk)
			require.NoError(err)
			// Make sure block and its parent are removed from the state map.
			require.NotContains(rejector.blkIDToState, blk.ID())
		})
	}
}
