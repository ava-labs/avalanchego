// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"

	avajson "github.com/ava-labs/avalanchego/utils/json"
)

// jsonRequester serves reply over a JSON round trip, as the HTTP requester
// would, and records the wire bytes.
type jsonRequester struct {
	reply    any
	wireJSON []byte
}

func (r *jsonRequester) SendRequest(_ context.Context, _ string, _ interface{}, reply interface{}, _ ...rpc.Option) error {
	b, err := json.Marshal(r.reply)
	if err != nil {
		return err
	}
	r.wireJSON = b
	return json.Unmarshal(b, reply)
}

func TestClientGetCurrentValidators(t *testing.T) {
	staker := api.Staker{
		TxID:      ids.Empty,
		StartTime: 1,
		EndTime:   2,
		Weight:    3,
		NodeID:    ids.EmptyNodeID,
	}
	clientStaker := ClientStaker{
		TxID:      ids.Empty,
		StartTime: 1,
		EndTime:   2,
		Weight:    3,
		NodeID:    ids.EmptyNodeID,
	}

	tests := []struct {
		name         string
		validator    api.PermissionlessValidator
		expectedJSON string
		expected     ClientPermissionlessValidator
	}{
		{
			name: "not auto-renewed",
			validator: api.PermissionlessValidator{
				Staker:        staker,
				DelegationFee: 2,
			},
			expectedJSON: `{
				"txID": "11111111111111111111111111111111LpoYY",
				"startTime": "1",
				"endTime": "2",
				"weight": "3",
				"nodeID": "NodeID-111111111111111111116DBWJs",
				"delegationFee": "2.0000"
			}`,
			expected: ClientPermissionlessValidator{
				ClientStaker:  clientStaker,
				DelegationFee: 2,
			},
		},
		{
			name: "auto-renewed with restaked rewards",
			validator: api.PermissionlessValidator{
				Staker:        staker,
				DelegationFee: 2,
				AutoRenewedConfig: &api.AutoRenewedConfig{
					ValidatorAuthority:        &api.Owner{Threshold: 1},
					NextPeriod:                1209600,
					AutoCompoundRewardShares:  1_000_000,
					RestakedValidationRewards: new(avajson.Uint64(7_000)),
					RestakedDelegateeRewards:  new(avajson.Uint64(3_000)),
				},
			},
			expectedJSON: `{
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
			expected: ClientPermissionlessValidator{
				ClientStaker:  clientStaker,
				DelegationFee: 2,
				AutoRenewedConfig: &ClientAutoRenewedConfig{
					ValidatorAuthority:        &ClientOwner{Threshold: 1, Addresses: []ids.ShortID{}},
					NextPeriod:                1209600,
					AutoCompoundRewardShares:  1_000_000,
					RestakedValidationRewards: new(uint64(7_000)),
					RestakedDelegateeRewards:  new(uint64(3_000)),
				},
			},
		},
		{
			name: "auto-renewed with zero restaked rewards",
			validator: api.PermissionlessValidator{
				Staker:        staker,
				DelegationFee: 2,
				AutoRenewedConfig: &api.AutoRenewedConfig{
					ValidatorAuthority:        &api.Owner{Threshold: 1},
					NextPeriod:                1209600,
					AutoCompoundRewardShares:  1_000_000,
					RestakedValidationRewards: new(avajson.Uint64),
					RestakedDelegateeRewards:  new(avajson.Uint64),
				},
			},
			expectedJSON: `{
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
			expected: ClientPermissionlessValidator{
				ClientStaker:  clientStaker,
				DelegationFee: 2,
				AutoRenewedConfig: &ClientAutoRenewedConfig{
					ValidatorAuthority:        &ClientOwner{Threshold: 1, Addresses: []ids.ShortID{}},
					NextPeriod:                1209600,
					AutoCompoundRewardShares:  1_000_000,
					RestakedValidationRewards: new(uint64),
					RestakedDelegateeRewards:  new(uint64),
				},
			},
		},
		{
			name: "auto-renewed without restaked rewards reported",
			validator: api.PermissionlessValidator{
				Staker:        staker,
				DelegationFee: 2,
				AutoRenewedConfig: &api.AutoRenewedConfig{
					ValidatorAuthority:       &api.Owner{Threshold: 1},
					NextPeriod:               1209600,
					AutoCompoundRewardShares: 1_000_000,
				},
			},
			expectedJSON: `{
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
			expected: ClientPermissionlessValidator{
				ClientStaker:  clientStaker,
				DelegationFee: 2,
				AutoRenewedConfig: &ClientAutoRenewedConfig{
					ValidatorAuthority:       &ClientOwner{Threshold: 1, Addresses: []ids.ShortID{}},
					NextPeriod:               1209600,
					AutoCompoundRewardShares: 1_000_000,
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			requester := &jsonRequester{
				reply: GetCurrentValidatorsReply{
					Validators: []any{test.validator},
				},
			}
			c := &Client{Requester: requester}

			validators, err := c.GetCurrentValidators(t.Context(), ids.Empty, nil)
			require.NoError(err)
			require.JSONEq(fmt.Sprintf(`{"validators": [%s]}`, test.expectedJSON), string(requester.wireJSON))
			require.Equal([]ClientPermissionlessValidator{test.expected}, validators)
		})
	}
}
