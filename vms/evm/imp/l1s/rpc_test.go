// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/deployerallowlist"
)

func withUpgrade(tb testing.TB, u extras.UpgradeConfig) sutOption {
	tb.Helper()
	b, err := json.Marshal(u)
	require.NoErrorf(tb, err, "json.Marshal(%T)", u)
	return options.Func[sutConfig](func(c *sutConfig) {
		c.upgrade = b
	})
}

func TestGetChainConfig(t *testing.T) {
	const (
		enableTime  uint64 = 2_000_000_000
		disableTime uint64 = 2_000_000_100
	)
	upgrades := extras.UpgradeConfig{
		PrecompileUpgrades: []extras.PrecompileUpgrade{
			{Config: deployerallowlist.NewConfig(new(enableTime), []common.Address{{1}}, nil, nil)},
			{Config: deployerallowlist.NewDisableConfig(new(disableTime))},
		},
		StateUpgrades: []extras.StateUpgrade{{
			BlockTimestamp: new(enableTime),
			StateUpgradeAccounts: map[common.Address]extras.StateUpgradeAccount{
				{2}: {BalanceChange: (*math.HexOrDecimal256)(big.NewInt(42))},
			},
		}},
	}

	ctx, sut := newSUT(t, withUpgrade(t, upgrades))

	var got struct {
		Upgrades extras.UpgradeConfig `json:"upgrades"`
	}
	client := sut.EthClient().RPC
	require.NoErrorf(t, client.CallContext(ctx, &got, "eth_getChainConfig"), "%T.CallContext(eth_getChainConfig)", client)
	require.Equal(t, upgrades, got.Upgrades, "eth_getChainConfig upgrades")
}
