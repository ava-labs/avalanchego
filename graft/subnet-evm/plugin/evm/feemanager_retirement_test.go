// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/feemanager/feemanagertest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
)

// TestLegacyFeeManagerHeliconRetirement drives the canonical
// [feemanagertest.RetirementCases] table through legacy
// `vm.Initialize`. Case definitions and JSON encoders live in
// [feemanagertest]; this test owns the loop, the VM-specific init,
// and asserts the post-init `GenesisPrecompiles` and
// `PrecompileUpgrades` match each case's expected shape (covers
// genesis normalization + synthetic-disable injection end-to-end).
func TestLegacyFeeManagerHeliconRetirement(t *testing.T) {
	helicon := uint64(upgradetest.GetConfig(upgradetest.Helicon).HeliconTime.Unix())
	base := params.TestSubnetEVMChainConfig

	for _, tc := range feemanagertest.RetirementCases(helicon) {
		t.Run(tc.Name, func(t *testing.T) {
			tvm, err := tryNewVM(t, testVMConfig{
				fork:        utils.PointerTo(upgradetest.Helicon),
				genesisJSON: string(feemanagertest.EncodeGenesisJSON(t, base, tc)),
				upgradeJSON: string(feemanagertest.EncodeUpgradeBytesJSON(t, tc)),
			})
			require.ErrorIs(t, err, tc.WantErr)
			if err != nil {
				return
			}

			extra := params.GetExtra(tvm.vm.ChainConfig())
			require.Equal(t, tc.WantGenesisPrecompiles, extra.GenesisPrecompiles,
				"post-Initialize GenesisPrecompiles mismatch")
			require.Equal(t, tc.WantPrecompileUpgrades, extra.PrecompileUpgrades,
				"post-Initialize PrecompileUpgrades mismatch")
		})
	}
}
