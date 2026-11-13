// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"math"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/libevm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/imp/warp/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

func TestRulesExtra_MinimumGasConsumption(t *testing.T) {
	tests := []struct {
		name    string
		isIgloo bool
		limit   uint64
		want    uint64
	}{
		{"pre_fork_noop", false, math.MaxUint64, 0},
		{"post_fork_zero", true, 0, 0},
		{"post_fork_odd_rounds_up", true, 3, 2},
		{"post_fork_tx_gas", true, 21_000, 10_500},
		{"post_fork_max_no_overflow", true, math.MaxUint64, 1 << 63},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := RulesExtra(extras.Rules{AvalancheRules: extras.AvalancheRules{IsIgloo: tt.isIgloo}})
			assert.Equalf(t, tt.want, r.MinimumGasConsumption(tt.limit), "%T.MinimumGasConsumption(%d)", r, tt.limit)
		})
	}
}

func TestRulesExtra_AccessListGas(t *testing.T) {
	config := WithExtra(
		&ChainConfig{},
		&extras.ChainConfig{
			UpgradeConfig: extras.UpgradeConfig{
				PrecompileUpgrades: []extras.PrecompileUpgrade{
					{Config: warp.NewDefaultConfig(utils.PointerTo[uint64](1))},
					{Config: warp.NewDisableConfig(utils.PointerTo[uint64](2))},
				},
			},
		},
	)

	addressedCall, err := payload.NewAddressedCall([]byte{1}, []byte{2})
	require.NoError(t, err, "payload.NewAddressedCall()")
	unsignedMsg, err := avalancheWarp.NewUnsignedMessage(constants.UnitTestID, ids.Empty, addressedCall.Bytes())
	require.NoError(t, err, "avalancheWarp.NewUnsignedMessage()")
	// The signature is not valid, but gas only depends on the number of signers.
	msg, err := avalancheWarp.NewMessage(unsignedMsg, &avalancheWarp.BitSetSignature{
		Signers: set.NewBits(0, 1).Bytes(),
	})
	require.NoError(t, err, "avalancheWarp.NewMessage()")
	accessList := libevm.AccessList{{
		Address:     warp.ContractAddress,
		StorageKeys: message.Pack(msg.Bytes()),
	}}

	tests := []struct {
		name         string
		time         uint64
		wantGas      uint64
		wantOverride bool
	}{
		{
			name:         "enabled",
			time:         1,
			wantGas:      220_200, // 200_000 + 6 chunks * 3_200 + 2 signers * 500
			wantOverride: true,
		},
		{
			// libevm charges the standard per-tuple cost instead.
			name: "disabled",
			time: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := config.Rules(common.Big0, IsMergeTODO, tt.time)
			gotGas, gotOverride, err := RulesExtra(*GetRulesExtra(rules)).AccessListGas(accessList)
			require.NoError(t, err, "AccessListGas()")
			require.Equal(t, tt.wantGas, gotGas, "AccessListGas() gas")
			require.Equal(t, tt.wantOverride, gotOverride, "AccessListGas() override")
		})
	}
}
