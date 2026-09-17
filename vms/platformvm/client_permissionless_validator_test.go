// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"

	avajson "github.com/ava-labs/avalanchego/utils/json"
	api "github.com/ava-labs/avalanchego/vms/platformvm/api"
)

// Test the JSON field names of AutoRenewedConfig, which are public API.
// The struct is embedded by pointer, so its fields are promoted, not nested.
func TestAutoRenewedConfigWireNames(t *testing.T) {
	require := require.New(t)

	cfg := api.AutoRenewedConfig{
		NextPeriod:                avajson.Uint64(1),
		AutoCompoundRewardShares:  avajson.Uint32(2),
		RestakedValidationRewards: ptrTo(avajson.Uint64(3)),
		RestakedDelegateeRewards:  ptrTo(avajson.Uint64(4)),
	}

	b, err := json.Marshal(cfg)
	require.NoError(err)

	// Values are quoted because avajson.Uint64 marshals as a string.
	require.JSONEq(`{
		"validatorAuthority": null,
		"nextPeriod": "1",
		"autoCompoundRewardShares": "2",
		"restakedValidationRewards": "3",
		"restakedDelegateeRewards": "4"
	}`, string(b))

	// A zero total must still be emitted: omitempty on a pointer drops only
	// nil, so "restaked nothing yet" stays distinguishable from "not reported".
	zero, err := json.Marshal(api.AutoRenewedConfig{
		RestakedValidationRewards: ptrTo(avajson.Uint64(0)),
		RestakedDelegateeRewards:  ptrTo(avajson.Uint64(0)),
	})
	require.NoError(err)
	require.Contains(string(zero), `"restakedValidationRewards":"0"`)
	require.Contains(string(zero), `"restakedDelegateeRewards":"0"`)

	// A node that does not report them omits the keys entirely.
	absent, err := json.Marshal(api.AutoRenewedConfig{})
	require.NoError(err)
	require.NotContains(string(absent), "restakedValidationRewards")
	require.NotContains(string(absent), "restakedDelegateeRewards")
}

// Test the full wire path: marshal, unmarshal, convert to client. A wrong
// JSON tag passes a struct-level assertion but fails here.
func TestGetClientPermissionlessValidatorsAutoRenewedRoundTrip(t *testing.T) {
	const (
		restakedValidation = uint64(7_000)
		restakedDelegatee  = uint64(3_000)
		stakedAmount       = uint64(2_000_000_000_000)
	)

	require := require.New(t)

	nodeID := ids.GenerateTestNodeID()
	apiValidator := api.PermissionlessValidator{
		Staker: api.Staker{
			TxID:   ids.GenerateTestID(),
			NodeID: nodeID,
			Weight: avajson.Uint64(stakedAmount + restakedValidation + restakedDelegatee),
		},
		DelegationFee: avajson.Float32(2),
		AutoRenewedConfig: &api.AutoRenewedConfig{
			ValidatorAuthority:        &api.Owner{Threshold: avajson.Uint32(1)},
			NextPeriod:                avajson.Uint64(1209600),
			AutoCompoundRewardShares:  avajson.Uint32(1_000_000),
			RestakedValidationRewards: ptrTo(avajson.Uint64(restakedValidation)),
			RestakedDelegateeRewards:  ptrTo(avajson.Uint64(restakedDelegatee)),
		},
	}

	clientValidators, err := getClientPermissionlessValidators([]interface{}{apiValidator})
	require.NoError(err)
	require.Len(clientValidators, 1)

	gotCfg := clientValidators[0].AutoRenewedConfig
	require.NotNil(gotCfg)
	require.NotNil(gotCfg.RestakedValidationRewards)
	require.NotNil(gotCfg.RestakedDelegateeRewards)
	require.Equal(restakedValidation, *gotCfg.RestakedValidationRewards)
	require.Equal(restakedDelegatee, *gotCfg.RestakedDelegateeRewards)
	require.Equal(uint64(1209600), gotCfg.NextPeriod)
	require.Equal(uint32(1_000_000), gotCfg.AutoCompoundRewardShares)

	require.Equal(
		clientValidators[0].Weight-stakedAmount,
		*gotCfg.RestakedValidationRewards+*gotCfg.RestakedDelegateeRewards,
	)
}

// Test version skew: a node predating these fields omits them, and the
// client must surface nil rather than zero.
func TestGetClientPermissionlessValidatorsAutoRenewedOlderNode(t *testing.T) {
	require := require.New(t)

	apiValidator := api.PermissionlessValidator{
		Staker: api.Staker{
			TxID:   ids.GenerateTestID(),
			NodeID: ids.GenerateTestNodeID(),
		},
		AutoRenewedConfig: &api.AutoRenewedConfig{
			ValidatorAuthority: &api.Owner{Threshold: avajson.Uint32(1)},
			NextPeriod:         avajson.Uint64(1209600),
			// RestakedValidationRewards and RestakedDelegateeRewards unset.
		},
	}

	clientValidators, err := getClientPermissionlessValidators([]interface{}{apiValidator})
	require.NoError(err)
	require.Len(clientValidators, 1)

	gotCfg := clientValidators[0].AutoRenewedConfig
	require.NotNil(gotCfg)
	require.Nil(gotCfg.RestakedValidationRewards)
	require.Nil(gotCfg.RestakedDelegateeRewards)
}

// Test an absent config converts to nil, not a zero-valued struct.
func TestAPIAutoRenewedConfigToClientNil(t *testing.T) {
	got, err := apiAutoRenewedConfigToClient(nil)
	require.NoError(t, err)
	require.Nil(t, got)
}

func ptrTo[T any](v T) *T { return &v }
