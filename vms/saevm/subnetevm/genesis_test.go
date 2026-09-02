// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	subnetevmparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
)

func TestParseGenesisNetworkUpgradeOverrides(t *testing.T) {
	genesis := newTestGenesis(upgradetest.Etna, saetest.NewUNSAFEKeyChain(t, 1))
	genesisBytes, err := json.Marshal(genesis)
	require.NoError(t, err, "json.Marshal(genesis)")

	wantEtnaTimestamp := uint64(upgrade.InitiallyActiveTime.Unix() + 2)
	upgradeBytes, err := json.Marshal(extras.UpgradeConfig{
		NetworkUpgradeOverrides: &extras.NetworkUpgrades{
			EtnaTimestamp: utils.PointerTo(wantEtnaTimestamp),
		},
	})
	require.NoError(t, err, "json.Marshal(upgradeConfig)")

	got, err := parseGenesis(
		newSnowCtx(t, upgradetest.GetConfig(upgradetest.Etna)),
		genesisBytes,
		upgradeBytes,
	)
	require.NoError(t, err, "parseGenesis()")
	require.Equal(
		t,
		wantEtnaTimestamp,
		*subnetevmparams.GetExtra(got.Config).EtnaTimestamp,
		"parseGenesis() EtnaTimestamp",
	)
}
