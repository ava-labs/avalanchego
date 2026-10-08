// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/nativeasset"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp"

	corethparams "github.com/ava-labs/avalanchego/graft/coreth/params"
	corethwarp "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	evmutils "github.com/ava-labs/avalanchego/graft/evm/utils"
)

func TestNewPrecompiles(t *testing.T) {
	ctx := snowtest.Context(t, snowtest.CChainID)
	set := newPrecompiles(ctx)
	require.NoError(t, set.Verify(), "Verify()")

	// Addresses are pinned against coreth's, which remain the source of truth
	// until coreth is removed.
	require.Equal(t, nativeasset.GenesisContractAddr, nativeAssetGenesisAddr, "nativeAssetGenesisAddr")
	require.Equal(t, nativeasset.NativeAssetBalanceAddr, nativeAssetBalanceAddr, "nativeAssetBalanceAddr")
	require.Equal(t, nativeasset.NativeAssetCallAddr, nativeAssetCallAddr, "nativeAssetCallAddr")
	require.Equal(t, corethparams.P256VerifyAddress, p256VerifyAddress, "p256VerifyAddress")

	wantContracts := []common.Address{
		nativeAssetGenesisAddr, nativeAssetBalanceAddr, nativeAssetCallAddr, p256VerifyAddress, warp.ContractAddress,
	}
	for _, addr := range wantContracts {
		_, ok := set.Contract(addr)
		require.Truef(t, ok, "Contract(%s)", addr)
	}
	require.Len(t, set.Contracts, len(wantContracts), "Contracts")

	p256, _ := set.Contract(p256VerifyAddress)
	require.IsType(t, &vm.P256Verify{}, p256, "P256 contract is libevm's")

	require.Equal(t, map[common.Address]bool{warp.ContractAddress: true}, predicaterAddresses(set.Predicaters), "Predicaters")

	// The EIP-2929 warm list excludes warp, as coreth's did.
	require.ElementsMatch(t,
		[]common.Address{nativeAssetGenesisAddr, nativeAssetBalanceAddr, nativeAssetCallAddr, p256VerifyAddress},
		set.Active,
		"Active",
	)
	// Cross-pinned against coreth's Granite warm list, since the differential
	// test shares SAE's Active on both arms and so cannot detect a mismatch.
	require.ElementsMatch(t,
		corethparams.RulesExtra(extras.Rules{AvalancheRules: extras.AvalancheRules{IsGranite: true}}).ActivePrecompiles(nil),
		set.Active,
		"Active matches coreth's Granite warm list",
	)
}

func predicaterAddresses[V any](m map[common.Address]V) map[common.Address]bool {
	out := make(map[common.Address]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

func TestParseGenesisSetsPrecompiles(t *testing.T) {
	ctx := snowtest.Context(t, snowtest.CChainID)
	g, err := parseGenesis(ctx, []byte(`{"config":{"chainId":43112},"gasLimit":"0x0","difficulty":"0x0","alloc":{}}`))
	require.NoError(t, err, "parseGenesis()")

	extra := corethparams.GetExtra(g.Config)
	require.NotNil(t, extra.Precompiles, "Precompiles on parsed chain config")
	require.NoError(t, extra.Precompiles.Verify(), "Precompiles.Verify()")

	// The Durango warp upgrade remains for pre-Helicon replay.
	require.Len(t, extra.PrecompileUpgrades, 1, "PrecompileUpgrades")
	warpConfig, ok := extra.PrecompileUpgrades[0].Config.(*corethwarp.Config)
	require.Truef(t, ok, "PrecompileUpgrades[0].Config is %T, want *corethwarp.Config", extra.PrecompileUpgrades[0].Config)
	require.Equal(t, evmutils.TimeToNewUint64(ctx.NetworkUpgrades.DurangoTime), warpConfig.Timestamp(), "warp upgrade Timestamp()")
}
