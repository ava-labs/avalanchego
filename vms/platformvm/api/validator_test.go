// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"

	avajson "github.com/ava-labs/avalanchego/utils/json"
)

func TestPermissionlessValidatorMarshalJSON(t *testing.T) {
	staker := Staker{
		TxID:      ids.Empty,
		StartTime: 1,
		EndTime:   2,
		Weight:    3,
		NodeID:    ids.EmptyNodeID,
	}

	tests := []struct {
		name      string
		validator PermissionlessValidator
		expected  string
	}{
		{
			name: "not auto-renewed",
			validator: PermissionlessValidator{
				Staker:        staker,
				DelegationFee: 2,
			},
			expected: `{
				"txID": "11111111111111111111111111111111LpoYY",
				"startTime": "1",
				"endTime": "2",
				"weight": "3",
				"nodeID": "NodeID-111111111111111111116DBWJs",
				"delegationFee": "2.0000"
			}`,
		},
		{
			name: "auto-renewed with restaked rewards",
			validator: PermissionlessValidator{
				Staker:        staker,
				DelegationFee: 2,
				AutoRenewedConfig: &AutoRenewedConfig{
					ValidatorAuthority:        &Owner{Threshold: 1},
					NextPeriod:                1209600,
					AutoCompoundRewardShares:  1_000_000,
					RestakedValidationRewards: new(avajson.Uint64(7_000)),
					RestakedDelegateeRewards:  new(avajson.Uint64(3_000)),
				},
			},
			expected: `{
				"txID": "11111111111111111111111111111111LpoYY",
				"startTime": "1",
				"endTime": "2",
				"weight": "3",
				"nodeID": "NodeID-111111111111111111116DBWJs",
				"delegationFee": "2.0000",
				"validatorAuthority": {"locktime": "0", "threshold": "1", "addresses": null},
				"nextPeriod": "1209600",
				"autoCompoundRewardShares": "1000000",
				"restakedValidationRewards": "7000",
				"restakedDelegateeRewards": "3000"
			}`,
		},
		{
			name: "auto-renewed with stake increases",
			validator: PermissionlessValidator{
				Staker:        staker,
				DelegationFee: 2,
				AutoRenewedConfig: &AutoRenewedConfig{
					ValidatorAuthority:        &Owner{Threshold: 1},
					NextPeriod:                1209600,
					AutoCompoundRewardShares:  1_000_000,
					RestakedValidationRewards: new(avajson.Uint64(7_000)),
					RestakedDelegateeRewards:  new(avajson.Uint64(3_000)),
					PendingStakeIncrease:      new(avajson.Uint64(2_000)),
					PendingStakeIncreases: &[]StakeIncrease{
						{
							TxID:   ids.Empty,
							Amount: 2_000,
						},
					},
					AppliedStakeIncrease: new(avajson.Uint64(5_000)),
					PendingStakeDecrease: new(avajson.Uint64(1_000)),
					PendingStakeDecreases: &[]StakeDecrease{
						{
							TxID:   ids.Empty,
							Amount: 1_000,
						},
					},
					WithdrawnStake: new(avajson.Uint64(4_000)),
				},
			},
			expected: `{
				"txID": "11111111111111111111111111111111LpoYY",
				"startTime": "1",
				"endTime": "2",
				"weight": "3",
				"nodeID": "NodeID-111111111111111111116DBWJs",
				"delegationFee": "2.0000",
				"validatorAuthority": {"locktime": "0", "threshold": "1", "addresses": null},
				"nextPeriod": "1209600",
				"autoCompoundRewardShares": "1000000",
				"restakedValidationRewards": "7000",
				"restakedDelegateeRewards": "3000",
				"pendingStakeIncrease": "2000",
				"pendingStakeIncreases": [{"txID": "11111111111111111111111111111111LpoYY", "amount": "2000"}],
				"appliedStakeIncrease": "5000",
				"pendingStakeDecrease": "1000",
				"pendingStakeDecreases": [{"txID": "11111111111111111111111111111111LpoYY", "amount": "1000"}],
				"withdrawnStake": "4000"
			}`,
		},
		{
			name: "auto-renewed with zero restaked rewards",
			validator: PermissionlessValidator{
				Staker:        staker,
				DelegationFee: 2,
				AutoRenewedConfig: &AutoRenewedConfig{
					ValidatorAuthority:        &Owner{Threshold: 1},
					NextPeriod:                1209600,
					AutoCompoundRewardShares:  1_000_000,
					RestakedValidationRewards: new(avajson.Uint64),
					RestakedDelegateeRewards:  new(avajson.Uint64),
				},
			},
			expected: `{
				"txID": "11111111111111111111111111111111LpoYY",
				"startTime": "1",
				"endTime": "2",
				"weight": "3",
				"nodeID": "NodeID-111111111111111111116DBWJs",
				"delegationFee": "2.0000",
				"validatorAuthority": {"locktime": "0", "threshold": "1", "addresses": null},
				"nextPeriod": "1209600",
				"autoCompoundRewardShares": "1000000",
				"restakedValidationRewards": "0",
				"restakedDelegateeRewards": "0"
			}`,
		},
		{
			name: "auto-renewed without restaked rewards reported",
			validator: PermissionlessValidator{
				Staker:        staker,
				DelegationFee: 2,
				AutoRenewedConfig: &AutoRenewedConfig{
					ValidatorAuthority:       &Owner{Threshold: 1},
					NextPeriod:               1209600,
					AutoCompoundRewardShares: 1_000_000,
				},
			},
			expected: `{
				"txID": "11111111111111111111111111111111LpoYY",
				"startTime": "1",
				"endTime": "2",
				"weight": "3",
				"nodeID": "NodeID-111111111111111111116DBWJs",
				"delegationFee": "2.0000",
				"validatorAuthority": {"locktime": "0", "threshold": "1", "addresses": null},
				"nextPeriod": "1209600",
				"autoCompoundRewardShares": "1000000"
			}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			validatorJSON, err := json.Marshal(test.validator)
			require.NoError(err)
			require.JSONEq(test.expected, string(validatorJSON))

			var parsedValidator PermissionlessValidator
			require.NoError(json.Unmarshal(validatorJSON, &parsedValidator))
			require.Equal(test.validator, parsedValidator)
		})
	}
}
