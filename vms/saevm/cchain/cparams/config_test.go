// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cparams

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/arr4n/shed/testerr"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/snow"

	ethparams "github.com/ava-labs/libevm/params"
)

func TestCheckConfigForkOrder(t *testing.T) {
	tests := []struct {
		name     string
		upgrades NetworkUpgrades
		wantErr  testerr.Want
	}{
		{
			name: "none_scheduled",
		},
		{
			name: "ascending_with_trailing_nils",
			upgrades: NetworkUpgrades{
				ApricotPhase1Timestamp: utils.PointerTo[uint64](0),
				ApricotPhase2Timestamp: utils.PointerTo[uint64](1),
				ApricotPhase3Timestamp: utils.PointerTo[uint64](2),
			},
		},
		{
			name: "equal_timestamps",
			upgrades: NetworkUpgrades{
				ApricotPhase1Timestamp: utils.PointerTo[uint64](5),
				ApricotPhase2Timestamp: utils.PointerTo[uint64](5),
			},
		},
		{
			name: "gap",
			upgrades: NetworkUpgrades{
				ApricotPhase1Timestamp: utils.PointerTo[uint64](0),
				ApricotPhase3Timestamp: utils.PointerTo[uint64](10),
			},
			wantErr: testerr.Contains("Apricot Phase 2 not enabled, but Apricot Phase 3 enabled at timestamp 10"),
		},
		{
			name: "first_fork_missing",
			upgrades: NetworkUpgrades{
				ApricotPhase2Timestamp: utils.PointerTo[uint64](0),
			},
			wantErr: testerr.Contains("Apricot Phase 1 not enabled, but Apricot Phase 2 enabled at timestamp 0"),
		},
		{
			name: "descending",
			upgrades: NetworkUpgrades{
				ApricotPhase1Timestamp: utils.PointerTo[uint64](10),
				ApricotPhase2Timestamp: utils.PointerTo[uint64](5),
			},
			wantErr: testerr.Contains("Apricot Phase 1 enabled at timestamp 10, but Apricot Phase 2 enabled at timestamp 5"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := WithExtra(&ethparams.ChainConfig{}, &ChainConfig{NetworkUpgrades: tt.upgrades})
			if diff := testerr.Diff(c.CheckConfigForkOrder(), tt.wantErr); diff != "" {
				t.Errorf("%T.CheckConfigForkOrder() %s", c, diff)
			}
		})
	}

	t.Run("nil_extra", func(t *testing.T) {
		c := &ethparams.ChainConfig{}
		require.NoErrorf(t, c.CheckConfigForkOrder(), "%T.CheckConfigForkOrder()", c)
	})
}

// TestChainConfigJSON guards the JSON encoding of [ChainConfig], which is
// persisted to disk as part of [ethparams.ChainConfig] and MUST NOT change.
// The registered extras are encoded at the root of the libevm config.
func TestChainConfigJSON(t *testing.T) {
	tests := []struct {
		name   string
		config ChainConfig
		json   string
	}{
		{
			name: "empty",
			json: `{"chainId": 1}`,
		},
		{
			name: "all_upgrades",
			config: ChainConfig{
				NetworkUpgrades: NetworkUpgrades{
					ApricotPhase1Timestamp:     utils.PointerTo[uint64](1),
					ApricotPhase2Timestamp:     utils.PointerTo[uint64](2),
					ApricotPhase3Timestamp:     utils.PointerTo[uint64](3),
					ApricotPhase4Timestamp:     utils.PointerTo[uint64](4),
					ApricotPhase5Timestamp:     utils.PointerTo[uint64](5),
					ApricotPhasePre6Timestamp:  utils.PointerTo[uint64](6),
					ApricotPhase6Timestamp:     utils.PointerTo[uint64](7),
					ApricotPhasePost6Timestamp: utils.PointerTo[uint64](8),
					BanffTimestamp:             utils.PointerTo[uint64](9),
					CortinaTimestamp:           utils.PointerTo[uint64](10),
					DurangoTimestamp:           utils.PointerTo[uint64](11),
					EtnaTimestamp:              utils.PointerTo[uint64](12),
					FortunaTimestamp:           utils.PointerTo[uint64](13),
					GraniteTimestamp:           utils.PointerTo[uint64](14),
					HeliconTimestamp:           utils.PointerTo[uint64](15),
					IglooTimestamp:             utils.PointerTo[uint64](16),
				},
			},
			json: `{
				"chainId": 1,
				"apricotPhase1BlockTimestamp": 1,
				"apricotPhase2BlockTimestamp": 2,
				"apricotPhase3BlockTimestamp": 3,
				"apricotPhase4BlockTimestamp": 4,
				"apricotPhase5BlockTimestamp": 5,
				"apricotPhasePre6BlockTimestamp": 6,
				"apricotPhase6BlockTimestamp": 7,
				"apricotPhasePost6BlockTimestamp": 8,
				"banffBlockTimestamp": 9,
				"cortinaBlockTimestamp": 10,
				"durangoBlockTimestamp": 11,
				"etnaTimestamp": 12,
				"fortunaTimestamp": 13,
				"graniteTimestamp": 14,
				"heliconTimestamp": 15,
				"iglooTimestamp": 16
			}`,
		},
		{
			name: "unscheduled_upgrades_omitted",
			config: ChainConfig{
				NetworkUpgrades: NetworkUpgrades{
					ApricotPhase1Timestamp: utils.PointerTo[uint64](0),
					DurangoTimestamp:       utils.PointerTo[uint64](100),
				},
			},
			json: `{
				"chainId": 1,
				"apricotPhase1BlockTimestamp": 0,
				"durangoBlockTimestamp": 100
			}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("marshal", func(t *testing.T) {
				// Fields that are not part of the encoding MUST be ignored.
				config := tt.config
				config.PrecompileUpgrades = []PrecompileUpgrade{
					{Config: warp.NewDefaultConfig(utils.PointerTo[uint64](0))},
				}
				config.SnowCtx = &snow.Context{}

				// ChainID is required.
				ethCfg := WithExtra(&ethparams.ChainConfig{ChainID: big.NewInt(1)}, &config)
				got, err := json.Marshal(ethCfg)
				require.NoError(t, err)
				require.JSONEq(t, tt.json, string(got))
			})

			t.Run("unmarshal", func(t *testing.T) {
				var got ethparams.ChainConfig
				require.NoError(t, json.Unmarshal([]byte(tt.json), &got))
				require.Equal(t, big.NewInt(1), got.ChainID, "ChainID")
				require.Equal(t, &tt.config, GetExtra(&got), "GetExtra()")
			})
		})
	}
}

func TestCheckConfigCompatible(t *testing.T) {
	ap5 := &ChainConfig{
		NetworkUpgrades: NetworkUpgrades{
			ApricotPhase5Timestamp: utils.PointerTo[uint64](0),
		},
	}
	tests := []struct {
		name    string
		stored  *ChainConfig
		new     *ethparams.ChainConfig
		wantErr *ethparams.ConfigCompatError
	}{
		{
			name: "nil_stored",
			new:  &ethparams.ChainConfig{},
		},
		{
			name:   "compatible",
			stored: ap5,
			new:    WithExtra(&ethparams.ChainConfig{}, ap5),
		},
		{
			name:   "incompatible",
			stored: ap5,
			new:    WithExtra(&ethparams.ChainConfig{}, &ChainConfig{}),
			wantErr: &ethparams.ConfigCompatError{
				What:       "Apricot Phase 5 fork block timestamp",
				StoredTime: utils.PointerTo[uint64](0),
			},
		},
		{
			name:   "new_without_extra",
			stored: ap5,
			new:    &ethparams.ChainConfig{},
			wantErr: &ethparams.ConfigCompatError{
				What:       "ChainConfig.Hooks() is not a non-nil *cparams.ChainConfig, got *cparams.ChainConfig",
				StoredTime: utils.PointerTo[uint64](0),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.stored.CheckConfigCompatible(tt.new, nil, 100)
			require.Equal(t, tt.wantErr, got)
		})
	}
}
