// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/uptime/uptimemock"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var (
	zeroRequiredUptimeConfig = genesis.UptimeRequirementConfig{
		DefaultRequiredUptimePercentage: 0,
	}
	eightyRequiredUptimeConfig = genesis.UptimeRequirementConfig{
		DefaultRequiredUptimePercentage: .8,
	}
)

func TestBlockOptions(t *testing.T) {
	type test struct {
		name                   string
		blkF                   func(*gomock.Controller) *Block
		expectedPreferenceType block.Block
	}

	tests := []test{
		{
			name: "apricot proposal block; commit preferred",
			blkF: func(ctrl *gomock.Controller) *Block {
				state := state.NewMockState(ctrl)

				uptimes := uptimemock.NewCalculator(ctrl)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   snowtest.Context(t, snowtest.PChainID),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Internal{
							UptimeRequirementConfig: zeroRequiredUptimeConfig,
						},
						Uptimes: uptimes,
					},
				}

				return &Block{
					Block:   &block.ApricotProposalBlock{},
					manager: manager,
				}
			},
			expectedPreferenceType: &block.ApricotCommitBlock{},
		},
		{
			name: "banff proposal block; invalid proposal tx",
			blkF: func(ctrl *gomock.Controller) *Block {
				state := state.NewMockState(ctrl)

				uptimes := uptimemock.NewCalculator(ctrl)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   snowtest.Context(t, snowtest.PChainID),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Internal{
							UptimeRequirementConfig: zeroRequiredUptimeConfig,
						},
						Uptimes: uptimes,
					},
				}

				return &Block{
					Block: &block.BanffProposalBlock{
						ApricotProposalBlock: block.ApricotProposalBlock{
							Tx: &txs.Tx{
								Unsigned: &txs.CreateChainTx{},
							},
						},
					},
					manager: manager,
				}
			},
			expectedPreferenceType: &block.BanffCommitBlock{},
		},
		{
			name: "banff proposal block; missing tx",
			blkF: func(ctrl *gomock.Controller) *Block {
				stakerTxID := ids.GenerateTestID()

				state := state.NewMockState(ctrl)
				state.EXPECT().GetTx(stakerTxID).Return(nil, status.Unknown, database.ErrNotFound)

				uptimes := uptimemock.NewCalculator(ctrl)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   snowtest.Context(t, snowtest.PChainID),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Internal{
							UptimeRequirementConfig: zeroRequiredUptimeConfig,
						},
						Uptimes: uptimes,
					},
				}

				return &Block{
					Block: &block.BanffProposalBlock{
						ApricotProposalBlock: block.ApricotProposalBlock{
							Tx: &txs.Tx{
								Unsigned: &txs.RewardValidatorTx{
									TxID: stakerTxID,
								},
							},
						},
					},
					manager: manager,
				}
			},
			expectedPreferenceType: &block.BanffCommitBlock{},
		},
		{
			name: "banff proposal block; error fetching staker tx",
			blkF: func(ctrl *gomock.Controller) *Block {
				stakerTxID := ids.GenerateTestID()

				state := state.NewMockState(ctrl)
				state.EXPECT().GetTx(stakerTxID).Return(nil, status.Unknown, database.ErrClosed)

				uptimes := uptimemock.NewCalculator(ctrl)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   snowtest.Context(t, snowtest.PChainID),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Internal{
							UptimeRequirementConfig: zeroRequiredUptimeConfig,
						},
						Uptimes: uptimes,
					},
				}

				return &Block{
					Block: &block.BanffProposalBlock{
						ApricotProposalBlock: block.ApricotProposalBlock{
							Tx: &txs.Tx{
								Unsigned: &txs.RewardValidatorTx{
									TxID: stakerTxID,
								},
							},
						},
					},
					manager: manager,
				}
			},
			expectedPreferenceType: &block.BanffCommitBlock{},
		},
		{
			name: "banff proposal block; unexpected staker tx type",
			blkF: func(ctrl *gomock.Controller) *Block {
				stakerTxID := ids.GenerateTestID()
				stakerTx := &txs.Tx{
					Unsigned: &txs.CreateChainTx{},
				}

				state := state.NewMockState(ctrl)
				state.EXPECT().GetTx(stakerTxID).Return(stakerTx, status.Committed, nil)

				uptimes := uptimemock.NewCalculator(ctrl)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   snowtest.Context(t, snowtest.PChainID),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Internal{
							UptimeRequirementConfig: zeroRequiredUptimeConfig,
						},
						Uptimes: uptimes,
					},
				}

				return &Block{
					Block: &block.BanffProposalBlock{
						ApricotProposalBlock: block.ApricotProposalBlock{
							Tx: &txs.Tx{
								Unsigned: &txs.RewardValidatorTx{
									TxID: stakerTxID,
								},
							},
						},
					},
					manager: manager,
				}
			},
			expectedPreferenceType: &block.BanffCommitBlock{},
		},
		{
			name: "banff proposal block; missing primary network validator",
			blkF: func(ctrl *gomock.Controller) *Block {
				var (
					stakerTxID = ids.GenerateTestID()
					nodeID     = ids.GenerateTestNodeID()
					subnetID   = ids.GenerateTestID()
					stakerTx   = &txs.Tx{
						Unsigned: &txs.AddPermissionlessValidatorTx{
							Validator: txs.Validator{
								NodeID: nodeID,
							},
							Subnet: subnetID,
						},
					}
				)

				state := state.NewMockState(ctrl)
				state.EXPECT().GetTx(stakerTxID).Return(stakerTx, status.Committed, nil)
				state.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, nodeID).Return(nil, database.ErrNotFound)

				uptimes := uptimemock.NewCalculator(ctrl)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   snowtest.Context(t, snowtest.PChainID),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Internal{
							UptimeRequirementConfig: zeroRequiredUptimeConfig,
						},
						Uptimes: uptimes,
					},
				}

				return &Block{
					Block: &block.BanffProposalBlock{
						ApricotProposalBlock: block.ApricotProposalBlock{
							Tx: &txs.Tx{
								Unsigned: &txs.RewardValidatorTx{
									TxID: stakerTxID,
								},
							},
						},
					},
					manager: manager,
				}
			},
			expectedPreferenceType: &block.BanffCommitBlock{},
		},
		{
			name: "banff proposal block; failed calculating primary network uptime",
			blkF: func(ctrl *gomock.Controller) *Block {
				var (
					stakerTxID = ids.GenerateTestID()
					nodeID     = ids.GenerateTestNodeID()
					subnetID   = constants.PrimaryNetworkID
					stakerTx   = &txs.Tx{
						Unsigned: &txs.AddPermissionlessValidatorTx{
							Validator: txs.Validator{
								NodeID: nodeID,
							},
							Subnet: subnetID,
						},
					}
					primaryNetworkValidatorStartTime = time.Now()
					staker                           = &state.Staker{
						StartTime: primaryNetworkValidatorStartTime,
					}
				)

				state := state.NewMockState(ctrl)
				state.EXPECT().GetTx(stakerTxID).Return(stakerTx, status.Committed, nil)
				state.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, nodeID).Return(staker, nil)

				uptimes := uptimemock.NewCalculator(ctrl)
				uptimes.EXPECT().CalculateUptimePercentFrom(nodeID, primaryNetworkValidatorStartTime).Return(0.0, database.ErrNotFound)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   snowtest.Context(t, snowtest.PChainID),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Internal{
							UptimeRequirementConfig: zeroRequiredUptimeConfig,
						},
						Uptimes: uptimes,
					},
				}

				return &Block{
					Block: &block.BanffProposalBlock{
						ApricotProposalBlock: block.ApricotProposalBlock{
							Tx: &txs.Tx{
								Unsigned: &txs.RewardValidatorTx{
									TxID: stakerTxID,
								},
							},
						},
					},
					manager: manager,
				}
			},
			expectedPreferenceType: &block.BanffCommitBlock{},
		},
		{
			name: "banff proposal block; failed fetching subnet transformation",
			blkF: func(ctrl *gomock.Controller) *Block {
				var (
					stakerTxID = ids.GenerateTestID()
					nodeID     = ids.GenerateTestNodeID()
					subnetID   = ids.GenerateTestID()
					stakerTx   = &txs.Tx{
						Unsigned: &txs.AddPermissionlessValidatorTx{
							Validator: txs.Validator{
								NodeID: nodeID,
							},
							Subnet: subnetID,
						},
					}
					primaryNetworkValidatorStartTime = time.Now()
					staker                           = &state.Staker{
						StartTime: primaryNetworkValidatorStartTime,
					}
				)

				state := state.NewMockState(ctrl)
				state.EXPECT().GetTx(stakerTxID).Return(stakerTx, status.Committed, nil)
				state.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, nodeID).Return(staker, nil)
				state.EXPECT().GetSubnetTransformation(subnetID).Return(nil, database.ErrNotFound)

				uptimes := uptimemock.NewCalculator(ctrl)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   snowtest.Context(t, snowtest.PChainID),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Internal{
							UptimeRequirementConfig: zeroRequiredUptimeConfig,
						},
						Uptimes: uptimes,
					},
				}

				return &Block{
					Block: &block.BanffProposalBlock{
						ApricotProposalBlock: block.ApricotProposalBlock{
							Tx: &txs.Tx{
								Unsigned: &txs.RewardValidatorTx{
									TxID: stakerTxID,
								},
							},
						},
					},
					manager: manager,
				}
			},
			expectedPreferenceType: &block.BanffCommitBlock{},
		},
		{
			name: "banff proposal block; prefers commit",
			blkF: func(ctrl *gomock.Controller) *Block {
				var (
					stakerTxID = ids.GenerateTestID()
					nodeID     = ids.GenerateTestNodeID()
					subnetID   = ids.GenerateTestID()
					stakerTx   = &txs.Tx{
						Unsigned: &txs.AddPermissionlessValidatorTx{
							Validator: txs.Validator{
								NodeID: nodeID,
							},
							Subnet: subnetID,
						},
					}
					primaryNetworkValidatorStartTime = time.Now()
					staker                           = &state.Staker{
						StartTime: primaryNetworkValidatorStartTime,
					}
					transformSubnetTx = &txs.Tx{
						Unsigned: &txs.TransformSubnetTx{
							UptimeRequirement: .2 * reward.PercentDenominator,
						},
					}
				)

				state := state.NewMockState(ctrl)
				state.EXPECT().GetTx(stakerTxID).Return(stakerTx, status.Committed, nil)
				state.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, nodeID).Return(staker, nil)
				state.EXPECT().GetSubnetTransformation(subnetID).Return(transformSubnetTx, nil)

				uptimes := uptimemock.NewCalculator(ctrl)
				uptimes.EXPECT().CalculateUptimePercentFrom(nodeID, primaryNetworkValidatorStartTime).Return(.5, nil)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   snowtest.Context(t, snowtest.PChainID),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Internal{
							UptimeRequirementConfig: eightyRequiredUptimeConfig,
						},
						Uptimes: uptimes,
					},
				}

				return &Block{
					Block: &block.BanffProposalBlock{
						ApricotProposalBlock: block.ApricotProposalBlock{
							Tx: &txs.Tx{
								Unsigned: &txs.RewardValidatorTx{
									TxID: stakerTxID,
								},
							},
						},
					},
					manager: manager,
				}
			},
			expectedPreferenceType: &block.BanffCommitBlock{},
		},
		{
			name: "banff proposal block; prefers abort",
			blkF: func(ctrl *gomock.Controller) *Block {
				var (
					stakerTxID = ids.GenerateTestID()
					nodeID     = ids.GenerateTestNodeID()
					subnetID   = ids.GenerateTestID()
					stakerTx   = &txs.Tx{
						Unsigned: &txs.AddPermissionlessValidatorTx{
							Validator: txs.Validator{
								NodeID: nodeID,
							},
							Subnet: subnetID,
						},
					}
					primaryNetworkValidatorStartTime = time.Now()
					staker                           = &state.Staker{
						StartTime: primaryNetworkValidatorStartTime,
					}
					transformSubnetTx = &txs.Tx{
						Unsigned: &txs.TransformSubnetTx{
							UptimeRequirement: .6 * reward.PercentDenominator,
						},
					}
				)

				state := state.NewMockState(ctrl)
				state.EXPECT().GetTx(stakerTxID).Return(stakerTx, status.Committed, nil)
				state.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, nodeID).Return(staker, nil)
				state.EXPECT().GetSubnetTransformation(subnetID).Return(transformSubnetTx, nil)

				uptimes := uptimemock.NewCalculator(ctrl)
				uptimes.EXPECT().CalculateUptimePercentFrom(nodeID, primaryNetworkValidatorStartTime).Return(.5, nil)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   snowtest.Context(t, snowtest.PChainID),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Internal{
							UptimeRequirementConfig: eightyRequiredUptimeConfig,
						},
						Uptimes: uptimes,
					},
				}

				return &Block{
					Block: &block.BanffProposalBlock{
						ApricotProposalBlock: block.ApricotProposalBlock{
							Tx: &txs.Tx{
								Unsigned: &txs.RewardValidatorTx{
									TxID: stakerTxID,
								},
							},
						},
					},
					manager: manager,
				}
			},
			expectedPreferenceType: &block.BanffAbortBlock{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			require := require.New(t)

			blk := tt.blkF(ctrl)
			options, err := blk.Options(t.Context())
			require.NoError(err)
			require.IsType(tt.expectedPreferenceType, options[0].(*Block).Block)
		})
	}
}
