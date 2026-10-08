// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"math"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/libevm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/nativeasset"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/vms/evm/precompile"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
)

func TestRulesExtra_MinimumGasConsumption(t *testing.T) {
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
			r := RulesExtra(extras.Rules{
				AvalancheRules: extras.AvalancheRules{IsHelicon: tt.isHelicon},
			})
			assert.Equalf(t, tt.want, r.MinimumGasConsumption(tt.limit), "%T.MinimumGasConsumption(%d)", r, tt.limit)
		})
	}
}

// stubContract is a comparable [libevm.PrecompiledContract]. Its id is
// reported as its gas so that distinct stubs are distinguishable.
type stubContract struct{ id uint64 }

func (c stubContract) RequiredGas([]byte) uint64 { return c.id }
func (stubContract) Run([]byte) ([]byte, error)  { return nil, nil }

type stubPredicater struct{}

func (stubPredicater) PredicateGas(predicate.Predicate, precompileconfig.Rules) (uint64, error) {
	return 0, nil
}

func (stubPredicater) VerifyPredicate(*precompileconfig.PredicateContext, predicate.Predicate) error {
	return nil
}

// warpAddress is coreth's module-registry warp address, used here only to
// assert that the registry is bypassed when a set is present.
var warpAddress = common.HexToAddress("0x0200000000000000000000000000000000000005")

func TestRulesExtra_PrecompileSet(t *testing.T) {
	var (
		inSet = common.Address{0xaa}
		other = common.Address{0xbb}
		set   = &precompile.Set{
			Contracts: map[common.Address]libevm.PrecompiledContract{inSet: stubContract{1}},
			Active:    []common.Address{inSet},
		}
		r = RulesExtra(extras.Rules{
			AvalancheRules: extras.AvalancheRules{IsGranite: true},
			PrecompileSet:  set,
		})
	)

	got, ok := r.PrecompileOverride(inSet)
	require.True(t, ok, "PrecompileOverride(address in set)")
	require.Equal(t, stubContract{1}, got, "PrecompileOverride(address in set)")

	_, ok = r.PrecompileOverride(other)
	require.False(t, ok, "PrecompileOverride(unknown address)")

	_, ok = r.PrecompileOverride(nativeasset.NativeAssetCallAddr)
	require.False(t, ok, "PrecompileOverride(built-in table address) must be bypassed by the set")

	_, ok = r.PrecompileOverride(warpAddress)
	require.False(t, ok, "PrecompileOverride(module registry address) must be bypassed by the set")

	require.Equal(t,
		[]common.Address{inSet, other},
		r.ActivePrecompiles([]common.Address{other}),
		"ActivePrecompiles(existing)",
	)
}

func TestRulesExtra_PrecompileSetNil(t *testing.T) {
	r := RulesExtra(extras.Rules{
		AvalancheRules: extras.AvalancheRules{IsGranite: true},
	})

	_, ok := r.PrecompileOverride(nativeasset.NativeAssetCallAddr)
	require.True(t, ok, "PrecompileOverride(built-in table address) without a set")

	// This pins the EIP-2929 warm list that SAE's set must reproduce.
	want := []common.Address{
		nativeasset.GenesisContractAddr,
		nativeasset.NativeAssetBalanceAddr,
		nativeasset.NativeAssetCallAddr,
		P256VerifyAddress,
	}
	require.ElementsMatch(t, want, r.ActivePrecompiles(nil), "ActivePrecompiles(nil) under Granite")
}

func TestConstructRulesExtra_PrecompileSet(t *testing.T) {
	addr := common.Address{0xaa}
	set := &precompile.Set{
		Contracts:   map[common.Address]libevm.PrecompiledContract{addr: stubContract{1}},
		Predicaters: map[common.Address]precompileconfig.Predicater{addr: stubPredicater{}},
	}
	cEx := &extras.ChainConfig{
		NetworkUpgrades: extras.TestHeliconChainConfig.NetworkUpgrades,
		Precompiles:     set,
	}

	rules := constructRulesExtra(nil, nil, cEx, big.NewInt(0), true, 0)
	require.Same(t, set, rules.PrecompileSet, "constructRulesExtra().PrecompileSet")
	require.Equal(t, set.Predicaters, rules.Predicaters, "constructRulesExtra().Predicaters")
	require.Empty(t, rules.Precompiles, "constructRulesExtra().Precompiles")
	require.Empty(t, rules.AccepterPrecompiles, "constructRulesExtra().AccepterPrecompiles")
	require.True(t, rules.IsHelicon, "constructRulesExtra().IsHelicon")
}

func TestConstructRulesExtra_PrecompileSetPreHelicon(t *testing.T) {
	addr := common.Address{0xaa}
	cEx := &extras.ChainConfig{
		NetworkUpgrades: extras.TestGraniteChainConfig.NetworkUpgrades,
		Precompiles: &precompile.Set{
			Contracts:   map[common.Address]libevm.PrecompiledContract{addr: stubContract{1}},
			Predicaters: map[common.Address]precompileconfig.Predicater{addr: stubPredicater{}},
		},
	}

	rules := constructRulesExtra(nil, nil, cEx, big.NewInt(0), true, 0)
	require.False(t, rules.IsHelicon, "constructRulesExtra().IsHelicon")
	require.Nil(t, rules.PrecompileSet, "constructRulesExtra().PrecompileSet before Helicon")
	// Before Helicon the set is ignored and the module loop allocates these.
	// The set path would carry the set's predicaters, so the NotContains
	// assertion below is what proves this branch ignores them.
	require.NotNil(t, rules.Precompiles, "constructRulesExtra().Precompiles")
	require.NotNil(t, rules.Predicaters, "constructRulesExtra().Predicaters")
	require.NotNil(t, rules.AccepterPrecompiles, "constructRulesExtra().AccepterPrecompiles")
	require.NotContains(t, rules.Predicaters, addr, "constructRulesExtra().Predicaters must not come from the set")

	_, ok := rules.PrecompileOverride(nativeasset.NativeAssetCallAddr)
	require.True(t, ok, "PrecompileOverride(built-in table address) before Helicon")
	_, ok = rules.PrecompileOverride(addr)
	require.False(t, ok, "PrecompileOverride(set address) before Helicon")
}
