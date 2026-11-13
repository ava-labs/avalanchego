// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cparams

import (
	"math"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/libevm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/evm/imp/cchain/warp/warptest"
	"github.com/ava-labs/avalanchego/vms/evm/imp/warp/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	avalanchewarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

func TestRules_MinimumGasConsumption(t *testing.T) {
	tests := []struct {
		name      string
		isHelicon bool
		limit     uint64
		want      uint64
	}{
		{"pre_fork_noop", false, math.MaxUint64, 0},
		{"post_fork_zero", true, 0, 0},
		{"post_fork_odd_rounds_up", true, 3, 2},
		{"post_fork_tx_gas", true, 21_000, 10_500},
		{"post_fork_max_no_overflow", true, math.MaxUint64, 1 << 63},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Rules{IsHelicon: tt.isHelicon}
			assert.Equalf(t, tt.want, r.MinimumGasConsumption(tt.limit), "%T.MinimumGasConsumption(%d)", r, tt.limit)
		})
	}
}

func TestRules_AccessListGas(t *testing.T) {
	const numSigners = 2
	addressedCall, err := payload.NewAddressedCall(nil, []byte{1, 2, 3})
	require.NoError(t, err, "payload.NewAddressedCall()")
	unsigned, err := avalanchewarp.NewUnsignedMessage(constants.UnitTestID, ids.GenerateTestID(), addressedCall.Bytes())
	require.NoError(t, err, "warp.NewUnsignedMessage()")
	msg := warptest.NewValidators(t, warptest.WithMinimum(numSigners)).Sign(t, unsigned)

	// A standard tuple costs 2_400 + 2*1_900 = 6_200. The Warp message has 2
	// signers and packs into 6 chunks.
	accessList := libevm.AccessList{
		{Address: common.Address{1}, StorageKeys: []common.Hash{{1}, {2}}},
		{Address: message.ContractAddress, StorageKeys: message.Pack(msg.Bytes())},
	}

	tests := []struct {
		name         string
		rules        Rules
		want         uint64
		wantOverride bool
	}{
		{
			name: "pre_durango_not_overridden",
		},
		{
			name:         "durango",
			rules:        Rules{IsDurango: true},
			want:         226_400, // 6_200 + 200_000 + 6*3_200 + 2*500
			wantOverride: true,
		},
		{
			name:         "granite",
			rules:        Rules{IsDurango: true, IsGranite: true},
			want:         134_772, // 6_200 + 125_000 + 6*512 + 2*250
			wantOverride: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotOverride, err := tt.rules.AccessListGas(accessList)
			require.NoErrorf(t, err, "%T.AccessListGas()", tt.rules)
			assert.Equalf(t, tt.wantOverride, gotOverride, "%T.AccessListGas() override", tt.rules)
			assert.Equalf(t, tt.want, got, "%T.AccessListGas() gas", tt.rules)
		})
	}
}
