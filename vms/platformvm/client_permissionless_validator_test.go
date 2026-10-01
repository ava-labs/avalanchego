// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"

	avajson "github.com/ava-labs/avalanchego/utils/json"
)

type mockClient struct {
	reply GetCurrentValidatorsReply
}

func (mc *mockClient) SendRequest(_ context.Context, _ string, _ interface{}, replyIntf interface{}, _ ...rpc.Option) error {
	reply := replyIntf.(*GetCurrentValidatorsReply)
	*reply = mc.reply
	return nil
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
		name      string
		validator api.PermissionlessValidator
		expected  ClientPermissionlessValidator
	}{
		{
			name: "not auto-renewed",
			validator: api.PermissionlessValidator{
				Staker:        staker,
				DelegationFee: 2,
			},
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

			c := &Client{
				Requester: &mockClient{
					reply: GetCurrentValidatorsReply{
						Validators: []any{test.validator},
					},
				},
			}

			validators, err := c.GetCurrentValidators(t.Context(), ids.Empty, nil)
			require.NoError(err)
			require.Equal([]ClientPermissionlessValidator{test.expected}, validators)
		})
	}
}
