// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestRejectBlock(t *testing.T) {
	type test struct {
		name         string
		newBlockFunc func() (block.Block, error)
		rejectFunc   func(*rejector, block.Block) error
	}

	tests := []test{
		{
			name: "proposal block",
			newBlockFunc: func() (block.Block, error) {
				return block.NewBanffProposalBlock(
					time.Now(),
					ids.GenerateTestID(),
					1,
					&txs.Tx{
						Unsigned: &txs.AddDelegatorTx{
							// Without the line below, this function will error.
							DelegationRewardsOwner: &secp256k1fx.OutputOwners{},
						},
						Creds: []verify.Verifiable{},
					},
					[]*txs.Tx{},
				)
			},
			rejectFunc: func(r *rejector, b block.Block) error {
				return r.BanffProposalBlock(b.(*block.BanffProposalBlock))
			},
		},
		{
			name: "atomic block",
			newBlockFunc: func() (block.Block, error) {
				return block.NewApricotAtomicBlock(
					ids.GenerateTestID(),
					1,
					&txs.Tx{
						Unsigned: &txs.AddDelegatorTx{
							// Without the line below, this function will error.
							DelegationRewardsOwner: &secp256k1fx.OutputOwners{},
						},
						Creds: []verify.Verifiable{},
					},
				)
			},
			rejectFunc: func(r *rejector, b block.Block) error {
				return r.ApricotAtomicBlock(b.(*block.ApricotAtomicBlock))
			},
		},
		{
			name: "standard block",
			newBlockFunc: func() (block.Block, error) {
				return block.NewBanffStandardBlock(
					time.Now(),
					ids.GenerateTestID(),
					1,
					[]*txs.Tx{
						{
							Unsigned: &txs.AddDelegatorTx{
								// Without the line below, this function will error.
								DelegationRewardsOwner: &secp256k1fx.OutputOwners{},
							},
							Creds: []verify.Verifiable{},
						},
					},
				)
			},
			rejectFunc: func(r *rejector, b block.Block) error {
				return r.BanffStandardBlock(b.(*block.BanffStandardBlock))
			},
		},
		{
			name: "commit",
			newBlockFunc: func() (block.Block, error) {
				return block.NewBanffCommitBlock(time.Now(), ids.GenerateTestID() /*parent*/, 1 /*height*/)
			},
			rejectFunc: func(r *rejector, blk block.Block) error {
				return r.BanffCommitBlock(blk.(*block.BanffCommitBlock))
			},
		},
		{
			name: "abort",
			newBlockFunc: func() (block.Block, error) {
				return block.NewBanffAbortBlock(time.Now(), ids.GenerateTestID() /*parent*/, 1 /*height*/)
			},
			rejectFunc: func(r *rejector, blk block.Block) error {
				return r.BanffAbortBlock(blk.(*block.BanffAbortBlock))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			blk, err := tt.newBlockFunc()
			require.NoError(err)

			mempool, err := mempool.New("", prometheus.NewRegistry())
			require.NoError(err)
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
				addTxsToMempool: true,
			}

			require.NoError(tt.rejectFunc(rejector, blk))
			// Make sure block and its parent are removed from the state map.
			require.NotContains(rejector.blkIDToState, blk.ID())
		})
	}
}
