// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/snow/snowtest"
)

// Genesis files commonly set subnetEVMTimestamp to 0 (or omit it), which MUST
// default to activating every upgrade prior to Durango at genesis.
func TestParseGenesisDefaultsSubnetEVMTimestamp(t *testing.T) {
	for _, config := range []string{
		`{"chainId":99999,"subnetEVMTimestamp":0}`,
		`{"chainId":99999}`,
	} {
		t.Run(config, func(t *testing.T) {
			genesis := []byte(`{"config":` + config + `,"alloc":{},"gasLimit":"0x7A1200","difficulty":"0x0"}`)
			ctx := snowtest.Context(t, snowtest.CChainID)

			g, err := parseGenesis(ctx, genesis, nil, "")
			require.NoError(t, err)

			rules := params.GetExtra(g.Config).GetAvalancheRules(0)
			require.True(t, rules.IsApricotPhase1)
			require.True(t, rules.IsCortina)
		})
	}
}
