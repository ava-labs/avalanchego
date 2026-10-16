// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cparams

import (
	"errors"
	"maps"
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/nativeasset"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap3"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/vms/evm/acp176"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"

	ethparams "github.com/ava-labs/libevm/params"
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
			r := RulesExtra{
				AvalancheRules: AvalancheRules{IsHelicon: tt.isHelicon},
			}
			assert.Equalf(t, tt.want, r.MinimumGasConsumption(tt.limit), "%T.MinimumGasConsumption(%d)", r, tt.limit)
		})
	}
}

func TestPredicateBytesFromExtra(t *testing.T) {
	tests := []struct {
		name     string
		rules    AvalancheRules
		extra    []byte
		expected []byte
	}{
		{
			name:     "empty_extra",
			extra:    nil,
			expected: nil,
		},
		{
			name:     "too_short",
			extra:    make([]byte, ap3.WindowSize-1),
			expected: nil,
		},
		{
			name:     "empty_predicate",
			extra:    make([]byte, ap3.WindowSize),
			expected: nil,
		},
		{
			name: "non_empty_predicate",
			extra: []byte{
				ap3.WindowSize: 5,
			},
			expected: []byte{5},
		},
		{
			name: "fortuna_empty_extra",
			rules: AvalancheRules{
				IsFortuna: true,
			},
			extra:    nil,
			expected: nil,
		},
		{
			name: "fortuna_too_short",
			rules: AvalancheRules{
				IsFortuna: true,
			},
			extra:    make([]byte, acp176.StateSize-1),
			expected: nil,
		},
		{
			name: "fortuna_empty_predicate",
			rules: AvalancheRules{
				IsFortuna: true,
			},
			extra:    make([]byte, acp176.StateSize),
			expected: nil,
		},
		{
			name: "fortuna_non_empty_predicate",
			rules: AvalancheRules{
				IsFortuna: true,
			},
			extra: []byte{
				acp176.StateSize: 5,
			},
			expected: []byte{5},
		},
		{
			name: "helicon_empty_predicate",
			rules: AvalancheRules{
				IsHelicon: true,
			},
			extra:    nil,
			expected: nil,
		},
		{
			name: "helicon_non_empty_predicate",
			rules: AvalancheRules{
				IsHelicon: true,
			},
			extra:    []byte{5},
			expected: []byte{5},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := predicateBytesFromExtra(test.rules, test.extra)
			require.Equal(t, test.expected, got)
		})
	}
}

func TestConstructRulesExtra(t *testing.T) {
	t.Run("nil_config", func(t *testing.T) {
		require.Zero(t, constructRulesExtra(nil, nil, nil, nil, false, 0))
	})

	t.Run("avalanche_rules", func(t *testing.T) {
		c := &ChainConfig{
			NetworkUpgrades: NetworkUpgrades{
				ApricotPhase1Timestamp: utils.PointerTo[uint64](0),
				DurangoTimestamp:       utils.PointerTo[uint64](10),
			},
		}
		for _, timestamp := range []uint64{0, 10} {
			got := constructRulesExtra(nil, nil, c, nil, false, timestamp)
			require.Equalf(t, c.Rules(timestamp), got.AvalancheRules, "AvalancheRules at %d", timestamp)
		}
	})

	var (
		enableA = warp.NewConfig(utils.PointerTo[uint64](10), 0, false)
		disable = &warp.Config{
			Upgrade: precompileconfig.Upgrade{
				BlockTimestamp: utils.PointerTo[uint64](20),
				Disable:        true,
			},
		}
		enableB = warp.NewConfig(utils.PointerTo[uint64](20), 80, false)
		enableC = warp.NewConfig(utils.PointerTo[uint64](30), 90, true)
		never   = warp.NewConfig(nil, 0, false)
	)
	tests := []struct {
		name      string
		upgrades  []*warp.Config
		timestamp uint64
		want      *warp.Config // nil if warp should be inactive
	}{
		{
			name:      "no_upgrades",
			timestamp: 100,
		},
		{
			name:      "before_enabled",
			upgrades:  []*warp.Config{enableA},
			timestamp: 9,
		},
		{
			name:      "at_enabled",
			upgrades:  []*warp.Config{enableA},
			timestamp: 10,
			want:      enableA,
		},
		{
			name:      "before_disabled",
			upgrades:  []*warp.Config{enableA, disable},
			timestamp: 19,
			want:      enableA,
		},
		{
			name:      "at_disabled",
			upgrades:  []*warp.Config{enableA, disable},
			timestamp: 20,
		},
		{
			name:      "before_reconfigured",
			upgrades:  []*warp.Config{enableA, enableB},
			timestamp: 19,
			want:      enableA,
		},
		{
			name:      "after_reconfigured",
			upgrades:  []*warp.Config{enableA, enableB},
			timestamp: 25,
			want:      enableB,
		},
		{
			name:      "before_reenabled",
			upgrades:  []*warp.Config{enableA, disable, enableC},
			timestamp: 29,
		},
		{
			name:      "at_reenabled",
			upgrades:  []*warp.Config{enableA, disable, enableC},
			timestamp: 30,
			want:      enableC,
		},
		{
			name:      "never_scheduled",
			upgrades:  []*warp.Config{never},
			timestamp: 100,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &ChainConfig{}
			for _, u := range tt.upgrades {
				c.PrecompileUpgrades = append(c.PrecompileUpgrades, PrecompileUpgrade{Config: u})
			}

			got := constructRulesExtra(nil, nil, c, nil, false, tt.timestamp)
			if tt.want == nil {
				require.Empty(t, got.precompiles, "precompiles")
				require.Empty(t, got.Predicaters, "Predicaters")
				require.False(t, got.HasPredicate(warp.ContractAddress), "HasPredicate()")
				return
			}
			require.Len(t, got.precompiles, 1, "precompiles")
			require.Same(t, tt.want, got.precompiles[warp.ContractAddress], "precompiles[warp]")
			require.Len(t, got.Predicaters, 1, "Predicaters")
			require.Same(t, tt.want, got.Predicaters[warp.ContractAddress], "Predicaters[warp]")
			require.True(t, got.HasPredicate(warp.ContractAddress), "HasPredicate()")
		})
	}
}

func TestBuiltinPrecompiles(t *testing.T) {
	var (
		ap1     = AvalancheRules{IsApricotPhase1: true}
		ap2     = ap1
		pre6    AvalancheRules
		ap6     AvalancheRules
		banff   AvalancheRules
		granite AvalancheRules
	)
	ap2.IsApricotPhase2 = true
	pre6 = ap2
	pre6.IsApricotPhasePre6 = true
	ap6 = pre6
	ap6.IsApricotPhase6 = true
	banff = ap6
	banff.IsBanff = true
	banff.IsDurango = true
	banff.IsFortuna = true
	granite = banff
	granite.IsGranite = true
	granite.IsHelicon = true

	tests := []struct {
		name  string
		rules AvalancheRules
		want  map[common.Address]vm.PrecompiledContract
	}{
		{name: "launch"},
		{name: "ApricotPhase1", rules: ap1},
		{name: "ApricotPhase2", rules: ap2, want: precompiledContractsApricotPhase2},
		{name: "ApricotPhasePre6", rules: pre6, want: precompiledContractsApricotPhasePre6},
		{name: "ApricotPhase6", rules: ap6, want: precompiledContractsApricotPhase6},
		{name: "Banff", rules: banff, want: precompiledContractsBanff},
		{name: "Granite", rules: granite, want: precompiledContractsGranite},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := RulesExtra{AvalancheRules: tt.rules}
			require.Equal(t,
				reflect.ValueOf(tt.want).Pointer(),
				reflect.ValueOf(r.currentPrecompiles()).Pointer(),
				"currentPrecompiles() returned the wrong set",
			)

			existing := []common.Address{{0xff}}
			wantAddrs := slices.AppendSeq(slices.Clone(existing), maps.Keys(tt.want))
			require.ElementsMatch(t, wantAddrs, r.ActivePrecompiles(existing), "ActivePrecompiles()")

			for addr := range tt.want {
				p, ok := r.PrecompileOverride(addr)
				require.Truef(t, ok, "PrecompileOverride(%s)", addr)
				require.NotNilf(t, p, "PrecompileOverride(%s)", addr)
			}
			for _, addr := range []common.Address{{0xff}, warp.ContractAddress} {
				_, ok := r.PrecompileOverride(addr)
				require.Falsef(t, ok, "PrecompileOverride(%s)", addr)
			}
		})
	}

	t.Run("expected_addresses", func(t *testing.T) {
		nativeAssets := []common.Address{
			nativeasset.GenesisContractAddr,
			nativeasset.NativeAssetBalanceAddr,
			nativeasset.NativeAssetCallAddr,
		}
		for _, m := range []map[common.Address]vm.PrecompiledContract{
			precompiledContractsApricotPhase2,
			precompiledContractsApricotPhasePre6,
			precompiledContractsApricotPhase6,
			precompiledContractsBanff,
		} {
			require.ElementsMatch(t, nativeAssets, slices.Collect(maps.Keys(m)))
		}
		require.ElementsMatch(t,
			append(nativeAssets, p256VerifyAddress),
			slices.Collect(maps.Keys(precompiledContractsGranite)),
		)
	})
}

func TestPrecompileOverrideDynamic(t *testing.T) {
	r := RulesExtra{
		precompiles: map[common.Address]precompileconfig.Config{
			warp.ContractAddress: warp.NewDefaultConfig(utils.PointerTo[uint64](0)),
		},
	}
	p, ok := r.PrecompileOverride(warp.ContractAddress)
	require.True(t, ok, "PrecompileOverride(warp) when enabled")
	require.NotNil(t, p, "PrecompileOverride(warp) when enabled")

	// Not a registered module, so no contract can be constructed.
	unregistered := common.Address{0xff}
	r.precompiles[unregistered] = warp.NewDefaultConfig(utils.PointerTo[uint64](0))
	_, ok = r.PrecompileOverride(unregistered)
	require.False(t, ok, "PrecompileOverride() of unregistered module")
}

type fixedGasPredicater struct {
	gas uint64
	err error
}

func (p fixedGasPredicater) PredicateGas(predicate.Predicate, precompileconfig.Rules) (uint64, error) {
	return p.gas, p.err
}

func (fixedGasPredicater) VerifyPredicate(*precompileconfig.PredicateContext, predicate.Predicate) error {
	return nil
}

func TestAccessListGas(t *testing.T) {
	var (
		predicaterAddr = common.Address{0x01}
		otherAddr      = common.Address{0x02}
		accessList     = libevm.AccessList{
			{Address: predicaterAddr, StorageKeys: []common.Hash{{}}},
			{Address: otherAddr, StorageKeys: []common.Hash{{}, {}}},
		}
		errTest = errors.New("test error")
	)
	tests := []struct {
		name         string
		predicaters  map[common.Address]precompileconfig.Predicater
		wantGas      uint64
		wantOverride bool
		wantErr      error
	}{
		{
			name: "no_predicaters",
		},
		{
			name: "predicater",
			predicaters: map[common.Address]precompileconfig.Predicater{
				predicaterAddr: fixedGasPredicater{gas: 1234},
			},
			wantGas:      1234 + ethparams.TxAccessListAddressGas + 2*ethparams.TxAccessListStorageKeyGas,
			wantOverride: true,
		},
		{
			name: "predicater_error",
			predicaters: map[common.Address]precompileconfig.Predicater{
				predicaterAddr: fixedGasPredicater{err: errTest},
			},
			wantOverride: true,
			wantErr:      errTest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := RulesExtra{Predicaters: tt.predicaters}
			gas, override, err := r.AccessListGas(accessList)
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.wantOverride, override, "override")
			require.Equal(t, tt.wantGas, gas, "gas")
		})
	}
}
