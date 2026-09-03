// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/feemanager"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
)

func TestLegacyFeeManagerRetirement(t *testing.T) {
	helicon := uint64(upgradetest.GetConfig(upgradetest.Helicon).HeliconTime.Unix()) // #nosec G115 -- known positive test timestamp
	chainConfig := params.Copy(params.TestSubnetEVMChainConfig)
	extra := params.GetExtra(&chainConfig)
	extra.GenesisPrecompiles = extras.Precompiles{
		feemanager.ConfigKey: feemanager.NewConfig(utils.PointerTo[uint64](0), nil, nil, nil, nil),
	}
	genesis := &core.Genesis{
		Config:     &chainConfig,
		Difficulty: big.NewInt(0),
		GasLimit:   8_000_000,
		Alloc:      types.GenesisAlloc{},
	}
	genesisJSON, err := json.Marshal(genesis)
	require.NoError(t, err, "json.Marshal(genesis)")

	tvm, err := tryNewVM(t, testVMConfig{
		fork:        utils.PointerTo(upgradetest.Helicon),
		genesisJSON: string(genesisJSON),
	})
	require.NoError(t, err, "tryNewVM()")

	got := params.GetExtra(tvm.vm.ChainConfig()).PrecompileUpgrades
	want := []extras.PrecompileUpgrade{
		{Config: feemanager.NewDisableConfig(utils.PointerTo(helicon))},
	}
	require.Equal(t, want, got, "feeManager retirement upgrades")
}
