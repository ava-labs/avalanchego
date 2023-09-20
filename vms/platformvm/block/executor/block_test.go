// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

func TestStatus(t *testing.T) {
	type test struct {
		name           string
		blockF         func(*gomock.Controller) *Block
		expectedStatus choices.Status
	}

	tests := []test{
		{
			name: "last accepted",
			blockF: func(ctrl *gomock.Controller) *Block {
				blkID := ids.GenerateTestID()
				statelessBlk := block.NewMockBlock(ctrl)
				statelessBlk.EXPECT().ID().Return(blkID)

				manager := &manager{
					backend: &backend{
						lastAccepted: blkID,
					},
				}

				return &Block{
					Block:   statelessBlk,
					manager: manager,
				}
			},
			expectedStatus: choices.Accepted,
		},
		{
			name: "processing",
			blockF: func(ctrl *gomock.Controller) *Block {
				blkID := ids.GenerateTestID()
				statelessBlk := block.NewMockBlock(ctrl)
				statelessBlk.EXPECT().ID().Return(blkID)

				manager := &manager{
					backend: &backend{
						blkIDToState: map[ids.ID]*blockState{
							blkID: {},
						},
					},
				}
				return &Block{
					Block:   statelessBlk,
					manager: manager,
				}
			},
			expectedStatus: choices.Processing,
		},
		{
			name: "in database",
			blockF: func(ctrl *gomock.Controller) *Block {
				blkID := ids.GenerateTestID()
				statelessBlk := block.NewMockBlock(ctrl)
				statelessBlk.EXPECT().ID().Return(blkID)

				state := state.NewMockState(ctrl)
				state.EXPECT().GetStatelessBlock(blkID).Return(statelessBlk, nil)

				manager := &manager{
					backend: &backend{
						state: state,
					},
				}
				return &Block{
					Block:   statelessBlk,
					manager: manager,
				}
			},
			expectedStatus: choices.Accepted,
		},
		{
			name: "not in map or database",
			blockF: func(ctrl *gomock.Controller) *Block {
				blkID := ids.GenerateTestID()
				statelessBlk := block.NewMockBlock(ctrl)
				statelessBlk.EXPECT().ID().Return(blkID)

				state := state.NewMockState(ctrl)
				state.EXPECT().GetStatelessBlock(blkID).Return(nil, database.ErrNotFound)

				manager := &manager{
					backend: &backend{
						state: state,
					},
				}
				return &Block{
					Block:   statelessBlk,
					manager: manager,
				}
			},
			expectedStatus: choices.Processing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			blk := tt.blockF(ctrl)
			require.Equal(t, tt.expectedStatus, blk.Status())
		})
	}
}

func TestBlockOptions(t *testing.T) {
	type test struct {
		name                   string
		blkF                   func() *Block
		expectedPreferenceType block.Block
		expectedErr            error
	}

	tests := []test{
		{
			name: "apricot proposal block; commit preferred",
			blkF: func() *Block {
				innerBlk := &block.ApricotProposalBlock{}
				blkID := innerBlk.ID()

				manager := &manager{
					backend: &backend{
						blkIDToState: map[ids.ID]*blockState{
							blkID: {
								proposalBlockState: proposalBlockState{
									initiallyPreferCommit: true,
								},
							},
						},
					},
				}

				return &Block{
					Block:   innerBlk,
					manager: manager,
				}
			},
			expectedPreferenceType: &block.ApricotCommitBlock{},
		},
		{
			name: "apricot proposal block; abort preferred",
			blkF: func() *Block {
				innerBlk := &block.ApricotProposalBlock{}
				blkID := innerBlk.ID()

				manager := &manager{
					backend: &backend{
						blkIDToState: map[ids.ID]*blockState{
							blkID: {},
						},
					},
				}

				return &Block{
					Block:   innerBlk,
					manager: manager,
				}
			},
			expectedPreferenceType: &block.ApricotAbortBlock{},
		},
		{
			name: "banff proposal block; commit preferred",
			blkF: func() *Block {
				innerBlk := &block.BanffProposalBlock{}
				blkID := innerBlk.ID()

				manager := &manager{
					backend: &backend{
						blkIDToState: map[ids.ID]*blockState{
							blkID: {
								proposalBlockState: proposalBlockState{
									initiallyPreferCommit: true,
								},
							},
						},
					},
				}

				return &Block{
					Block:   innerBlk,
					manager: manager,
				}
			},
			expectedPreferenceType: &block.BanffCommitBlock{},
		},
		{
			name: "banff proposal block; abort preferred",
			blkF: func() *Block {
				innerBlk := &block.BanffProposalBlock{}
				blkID := innerBlk.ID()

				manager := &manager{
					backend: &backend{
						blkIDToState: map[ids.ID]*blockState{
							blkID: {},
						},
					},
				}

				return &Block{
					Block:   innerBlk,
					manager: manager,
				}
			},
			expectedPreferenceType: &block.BanffAbortBlock{},
		},
		{
			name: "non oracle block",
			blkF: func() *Block {
				return &Block{
					Block:   &block.BanffStandardBlock{},
					manager: &manager{},
				}
			},
			expectedErr: snowman.ErrNotOracle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			blk := tt.blkF()
			options, err := blk.Options(context.Background())
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			require.IsType(tt.expectedPreferenceType, options[0].(*Block).Block)
		})
	}
}
