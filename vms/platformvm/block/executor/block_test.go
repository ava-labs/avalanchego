// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/uptime/uptimemock"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/statetest"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
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
				state := statetest.New(t, statetest.Config{})
				uptimes := uptimemock.NewCalculator(ctrl)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   snowtest.Context(t, snowtest.PChainID),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Internal{
							UptimePercentage: 0,
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
				state := statetest.New(t, statetest.Config{})
				uptimes := uptimemock.NewCalculator(ctrl)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   snowtest.Context(t, snowtest.PChainID),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Internal{
							UptimePercentage: 0,
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
				uptimes := uptimemock.NewCalculator(ctrl)

				state := statetest.New(t, statetest.Config{})

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   snowtest.Context(t, snowtest.PChainID),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Internal{
							UptimePercentage: 0,
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
			name: "banff proposal block; error fetching staker tx; db closed",
			blkF: func(ctrl *gomock.Controller) *Block {
				stakerTxID := ids.GenerateTestID()

				db := memdb.New()
				state := statetest.New(t, statetest.Config{
					DB: db,
				})
				require.NoError(t, db.Close())

				uptimes := uptimemock.NewCalculator(ctrl)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   snowtest.Context(t, snowtest.PChainID),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Internal{
							UptimePercentage: 0,
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
					TxID:     stakerTxID,
					Unsigned: &txs.CreateChainTx{},
				}

				state := statetest.New(t, statetest.Config{})
				state.AddTx(stakerTx, status.Committed)
				uptimes := uptimemock.NewCalculator(ctrl)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   snowtest.Context(t, snowtest.PChainID),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Internal{
							UptimePercentage: 0,
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
						TxID: stakerTxID,
					}
				)

				state := statetest.New(t, statetest.Config{})
				state.AddTx(stakerTx, status.Committed)
				uptimes := uptimemock.NewCalculator(ctrl)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   snowtest.Context(t, snowtest.PChainID),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Internal{
							UptimePercentage: 0,
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
						TxID: stakerTxID,
					}
					primaryNetworkValidatorStartTime = time.Now()
					staker                           = &state.Staker{
						StartTime: primaryNetworkValidatorStartTime,
						NodeID:    nodeID,
					}
				)

				state := statetest.New(t, statetest.Config{})
				state.AddTx(stakerTx, status.Committed)
				require.NoError(t, state.PutCurrentValidator(staker))

				uptimes := uptimemock.NewCalculator(ctrl)
				uptimes.EXPECT().CalculateUptimePercentFrom(nodeID, primaryNetworkValidatorStartTime).Return(0.0, database.ErrNotFound)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   snowtest.Context(t, snowtest.PChainID),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Internal{
							UptimePercentage: 0,
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
						TxID: stakerTxID,
					}
					primaryNetworkValidatorStartTime = time.Now()
					staker                           = &state.Staker{
						StartTime: primaryNetworkValidatorStartTime,
						NodeID:    nodeID,
					}
				)
				uptimes := uptimemock.NewCalculator(ctrl)

				state := statetest.New(t, statetest.Config{})
				state.AddTx(stakerTx, status.Committed)

				require.NoError(t, state.PutCurrentValidator(staker))

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   snowtest.Context(t, snowtest.PChainID),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Internal{
							UptimePercentage: 0,
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
						TxID: stakerTxID,
					}
					primaryNetworkValidatorStartTime = time.Now()
					staker                           = &state.Staker{
						StartTime: primaryNetworkValidatorStartTime,
						NodeID:    nodeID,
					}
					transformSubnetTx = &txs.Tx{
						Unsigned: &txs.TransformSubnetTx{
							UptimeRequirement: .2 * reward.PercentDenominator,
							Subnet:            subnetID,
						},
					}
				)

				uptimes := uptimemock.NewCalculator(ctrl)
				uptimes.EXPECT().CalculateUptimePercentFrom(nodeID, primaryNetworkValidatorStartTime).Return(.5, nil)

				state := statetest.New(t, statetest.Config{})
				state.AddTx(stakerTx, status.Committed)
				require.NoError(t, state.PutCurrentValidator(staker))

				state.AddSubnetTransformation(transformSubnetTx)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   snowtest.Context(t, snowtest.PChainID),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Internal{
							UptimePercentage: .8,
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
					subnetID   = constants.PrimaryNetworkID
					stakerTx   = &txs.Tx{
						Unsigned: &txs.AddPermissionlessValidatorTx{
							Validator: txs.Validator{
								NodeID: nodeID,
							},
							Subnet: subnetID,
						},
						TxID: stakerTxID,
					}
					primaryNetworkValidatorStartTime = time.Now()
					staker                           = &state.Staker{
						StartTime: primaryNetworkValidatorStartTime,
						NodeID:    nodeID,
						SubnetID:  subnetID,
					}
					transformSubnetTx = &txs.Tx{
						Unsigned: &txs.TransformSubnetTx{
							UptimeRequirement: .6 * reward.PercentDenominator,
						},
					}
				)

				state := statetest.New(t, statetest.Config{})
				state.AddTx(stakerTx, status.Committed)
				require.NoError(t, state.PutCurrentValidator(staker))

				state.AddSubnetTransformation(transformSubnetTx)
				uptimes := uptimemock.NewCalculator(ctrl)
				uptimes.EXPECT().CalculateUptimePercentFrom(nodeID, primaryNetworkValidatorStartTime).Return(.5, nil)
				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   snowtest.Context(t, snowtest.PChainID),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Internal{
							UptimePercentage: .8,
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

// TestBlockOptionsACP267UptimeRequirement verifies that ACP-267 raises the
// Primary Network uptime requirement to 90% for validations that start at or
// after Helicon activates.
func TestBlockOptionsACP267UptimeRequirement(t *testing.T) {
	heliconTime := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)

	type test struct {
		name      string
		fork      upgradetest.Fork
		startTime time.Time
		uptime    float64
		want      block.Block
	}

	const uptimeWindow = 1000 * time.Second
	newBlock := func(t *testing.T, tt test) *Block {
		t.Helper()

		var (
			nodeID     = ids.GenerateTestNodeID()
			stakerTxID = ids.GenerateTestID()
			stakerTx   = &txs.Tx{
				Unsigned: &txs.AddPermissionlessValidatorTx{
					Validator: txs.Validator{NodeID: nodeID},
					Subnet:    constants.PrimaryNetworkID,
				},
				TxID: stakerTxID,
			}
			staker = &state.Staker{
				StartTime: tt.startTime,
				NodeID:    nodeID,
				SubnetID:  constants.PrimaryNetworkID,
			}
			now = tt.startTime.Add(uptimeWindow)
		)

		chainState := statetest.New(t, statetest.Config{})
		chainState.AddTx(stakerTx, status.Committed)
		require.NoError(t, chainState.PutCurrentValidator(staker))

		clk := &mockable.Clock{}
		clk.Set(now)
		uptimeState := uptime.NewTestState()
		uptimeState.AddNode(nodeID, tt.startTime)
		require.NoError(t, uptimeState.SetUptime(
			nodeID,
			time.Duration(tt.uptime*float64(uptimeWindow)),
			now,
		))

		ctx := snowtest.Context(t, snowtest.PChainID)

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
			manager: &manager{
				backend: &backend{
					state: chainState,
					ctx:   ctx,
				},
				txExecutorBackend: &executor.Backend{
					Config: &config.Internal{
						UptimePercentage: .8,
						UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(
							tt.fork,
							heliconTime,
						),
					},
					Uptimes: uptime.NewManager(uptimeState, clk),
				},
			},
		}
	}

	tests := []test{
		{
			name:      "pre_helicon",
			fork:      upgradetest.Helicon,
			startTime: heliconTime.Add(-time.Second),
			uptime:    .85,
			want:      &block.BanffCommitBlock{},
		},
		{
			name:      "at_helicon",
			fork:      upgradetest.Helicon,
			startTime: heliconTime,
			uptime:    .85,
			want:      &block.BanffAbortBlock{},
		},
		{
			name:      "post_helicon",
			fork:      upgradetest.Helicon,
			startTime: heliconTime.Add(time.Hour),
			uptime:    .9,
			want:      &block.BanffCommitBlock{},
		},
		{
			name:      "without_helicon",
			fork:      upgradetest.Granite,
			startTime: heliconTime.Add(time.Hour),
			uptime:    .85,
			want:      &block.BanffCommitBlock{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blk := newBlock(t, tt)
			options, err := blk.Options(t.Context())
			require.NoError(t, err)
			require.IsType(t, tt.want, options[0].(*Block).Block)
		})
	}
}
