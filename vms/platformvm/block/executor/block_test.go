// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/statetest"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/types"
)

var _ uptime.Calculator = testUptimeCalculator{}

// testUptimeCalculator is a local [uptime.Calculator] for tests that returns a
// fixed uptime percentage (or error), avoiding the need for a generated mock.
type testUptimeCalculator struct {
	percent float64
	err     error
}

func (c testUptimeCalculator) CalculateUptime(ids.NodeID) (time.Duration, time.Time, error) {
	return 0, time.Time{}, c.err
}

func (c testUptimeCalculator) CalculateUptimePercent(ids.NodeID) (float64, error) {
	return c.percent, c.err
}

func (c testUptimeCalculator) CalculateUptimePercentFrom(ids.NodeID, time.Time) (float64, error) {
	return c.percent, c.err
}

func TestBlockOptions(t *testing.T) {
	type test struct {
		name                   string
		blkF                   func() *Block
		expectedPreferenceType block.Block
	}

	tests := []test{
		{
			name: "apricot proposal block; commit preferred",
			blkF: func() *Block {
				state := statetest.New(t, statetest.Config{})
				uptimes := testUptimeCalculator{}

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
			blkF: func() *Block {
				state := statetest.New(t, statetest.Config{})
				uptimes := testUptimeCalculator{}

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
			blkF: func() *Block {
				stakerTxID := ids.GenerateTestID()
				uptimes := testUptimeCalculator{}

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
			blkF: func() *Block {
				stakerTxID := ids.GenerateTestID()

				db := memdb.New()
				state := statetest.New(t, statetest.Config{
					DB: db,
				})
				require.NoError(t, db.Close())

				uptimes := testUptimeCalculator{}

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
			blkF: func() *Block {
				stakerTxID := ids.GenerateTestID()
				stakerTx := &txs.Tx{
					TxID:     stakerTxID,
					Unsigned: &txs.CreateChainTx{},
				}

				state := statetest.New(t, statetest.Config{})
				state.AddTx(stakerTx, status.Committed)
				uptimes := testUptimeCalculator{}

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
			blkF: func() *Block {
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
				uptimes := testUptimeCalculator{}

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
			blkF: func() *Block {
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
					primaryNetworkValidatorStartTime = genesistest.DefaultValidatorStartTime
					staker                           = &state.Staker{
						StartTime: primaryNetworkValidatorStartTime,
						NodeID:    nodeID,
					}
				)

				state := statetest.New(t, statetest.Config{})
				state.AddTx(stakerTx, status.Committed)
				require.NoError(t, state.PutCurrentValidator(staker))

				uptimes := testUptimeCalculator{err: database.ErrNotFound}

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
			blkF: func() *Block {
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
					primaryNetworkValidatorStartTime = genesistest.DefaultValidatorStartTime
					staker                           = &state.Staker{
						StartTime: primaryNetworkValidatorStartTime,
						NodeID:    nodeID,
					}
				)
				uptimes := testUptimeCalculator{}

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
			blkF: func() *Block {
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
					primaryNetworkValidatorStartTime = genesistest.DefaultValidatorStartTime
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

				uptimes := testUptimeCalculator{percent: .5}

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
			blkF: func() *Block {
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
					primaryNetworkValidatorStartTime = genesistest.DefaultValidatorStartTime
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
				uptimes := testUptimeCalculator{percent: .5}
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
		{
			name: "banff proposal block; reward auto-renewed validator; sufficient uptime; prefer commit",
			blkF: func() *Block {
				var (
					stakerTxID = ids.GenerateTestID()
					nodeID     = ids.GenerateTestNodeID()
					stakerTx   = &txs.Tx{
						Unsigned: &txs.AddAutoRenewedValidatorTx{
							ValidatorNodeID: types.JSONByteSlice(nodeID.Bytes()),
						},
						TxID: stakerTxID,
					}
					primaryNetworkValidatorStartTime = genesistest.DefaultValidatorStartTime
					staker                           = &state.Staker{
						StartTime: primaryNetworkValidatorStartTime,
						NodeID:    nodeID,
					}
				)

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
							UptimePercentage: .8,
						},
						Uptimes: testUptimeCalculator{percent: .9},
					},
				}

				return &Block{
					Block: &block.BanffProposalBlock{
						ApricotProposalBlock: block.ApricotProposalBlock{
							Tx: &txs.Tx{
								Unsigned: &txs.RewardAutoRenewedValidatorTx{
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
			name: "banff proposal block; reward auto-renewed validator; insufficient uptime; prefer abort",
			blkF: func() *Block {
				var (
					stakerTxID = ids.GenerateTestID()
					nodeID     = ids.GenerateTestNodeID()
					stakerTx   = &txs.Tx{
						Unsigned: &txs.AddAutoRenewedValidatorTx{
							ValidatorNodeID: types.JSONByteSlice(nodeID.Bytes()),
						},
						TxID: stakerTxID,
					}
					primaryNetworkValidatorStartTime = genesistest.DefaultValidatorStartTime
					staker                           = &state.Staker{
						StartTime: primaryNetworkValidatorStartTime,
						NodeID:    nodeID,
					}
				)

				state := statetest.New(t, statetest.Config{})
				state.AddTx(stakerTx, status.Committed)
				require.NoError(t, state.PutCurrentValidator(staker))

				uptimes := testUptimeCalculator{percent: .5}

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
								Unsigned: &txs.RewardAutoRenewedValidatorTx{
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
			require := require.New(t)

			blk := tt.blkF()
			options, err := blk.Options(t.Context())
			require.NoError(err)
			require.IsType(tt.expectedPreferenceType, options[0].(*Block).Block)
		})
	}
}
