// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cparams_test

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/arr4n/shed/testerr"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/cparams"

	corethparams "github.com/ava-labs/avalanchego/graft/coreth/params"
	ethparams "github.com/ava-labs/libevm/params"
)

func TestMain(m *testing.M) {
	corethparams.RegisterExtras()
	os.Exit(m.Run())
}

func TestCheckConfigForkOrder(t *testing.T) {
	tests := []struct {
		name     string
		upgrades cparams.ChainConfig
		wantErr  testerr.Want
	}{
		{
			name: "none_scheduled",
		},
		{
			name: "ascending_with_trailing_nils",
			upgrades: cparams.ChainConfig{
				ApricotPhase1Timestamp: new(uint64),
				ApricotPhase2Timestamp: new(uint64(1)),
				ApricotPhase3Timestamp: new(uint64(2)),
			},
		},
		{
			name: "equal_timestamps",
			upgrades: cparams.ChainConfig{
				ApricotPhase1Timestamp: new(uint64(5)),
				ApricotPhase2Timestamp: new(uint64(5)),
			},
		},
		{
			name: "gap",
			upgrades: cparams.ChainConfig{
				ApricotPhase1Timestamp: new(uint64),
				ApricotPhase3Timestamp: new(uint64(10)),
			},
			wantErr: testerr.Contains("Apricot Phase 2 not enabled"),
		},
		{
			name: "first_fork_missing",
			upgrades: cparams.ChainConfig{
				ApricotPhase2Timestamp: new(uint64),
			},
			wantErr: testerr.Contains("Apricot Phase 1 not enabled"),
		},
		{
			name: "descending",
			upgrades: cparams.ChainConfig{
				ApricotPhase1Timestamp: new(uint64(10)),
				ApricotPhase2Timestamp: new(uint64(5)),
			},
			wantErr: testerr.Contains("Apricot Phase 1 enabled at timestamp 10, but Apricot Phase 2 enabled at timestamp 5"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := corethparams.WithExtra(
				&ethparams.ChainConfig{},
				&tt.upgrades,
			)
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

// TODO(alarso16): delete this once test package is unnecessary.
func ptrEqual[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

// Tests [cparams.ChainConfig.CheckConfigCompatible] through
// [ethparams.ChainConfig.CheckCompatible]. All eth fork handling is tested
// [ethparams] and is independent of Avalanche forks.
func TestCheckCompatible(t *testing.T) {
	withUpgrades := func(n cparams.ChainConfig) *ethparams.ChainConfig {
		return corethparams.WithExtra(&ethparams.ChainConfig{}, &n)
	}

	// checks for non-nil [ethparams.ConfigCompatError] with matching timestamps
	compatErr := func(storedTime, newTime *uint64) testerr.Want {
		return testerr.As(func(got *ethparams.ConfigCompatError) string {
			if ptrEqual(got.StoredTime, storedTime) && ptrEqual(got.NewTime, newTime) {
				return ""
			}
			return fmt.Sprintf("%T with stored time %v and new time %v", got, deref(storedTime), deref(newTime))
		})
	}

	const headTime = 100
	tests := []struct {
		name    string
		stored  *ethparams.ChainConfig
		new     *ethparams.ChainConfig
		wantErr testerr.Want
	}{
		{
			name:   "identical",
			stored: withUpgrades(cparams.ChainConfig{DurangoTimestamp: new(uint64(50))}),
			new:    withUpgrades(cparams.ChainConfig{DurangoTimestamp: new(uint64(50))}),
		},
		{
			name:   "future_fork_rescheduled",
			stored: withUpgrades(cparams.ChainConfig{DurangoTimestamp: new(uint64(200))}),
			new:    withUpgrades(cparams.ChainConfig{DurangoTimestamp: new(uint64(300))}),
		},
		{
			name:   "future_fork_unscheduled",
			stored: withUpgrades(cparams.ChainConfig{DurangoTimestamp: new(uint64(200))}),
			new:    withUpgrades(cparams.ChainConfig{}),
		},
		{
			name:    "active_fork_rescheduled",
			stored:  withUpgrades(cparams.ChainConfig{DurangoTimestamp: new(uint64(50))}),
			new:     withUpgrades(cparams.ChainConfig{DurangoTimestamp: new(uint64(60))}),
			wantErr: compatErr(new(uint64(50)), new(uint64(60))),
		},
		{
			name:    "active_fork_unscheduled",
			stored:  withUpgrades(cparams.ChainConfig{ApricotPhase5Timestamp: new(uint64)}),
			new:     withUpgrades(cparams.ChainConfig{}),
			wantErr: compatErr(new(uint64), nil),
		},
		{
			name:    "fork_scheduled_in_past",
			stored:  withUpgrades(cparams.ChainConfig{}),
			new:     withUpgrades(cparams.ChainConfig{DurangoTimestamp: new(uint64(50))}),
			wantErr: compatErr(nil, new(uint64(50))),
		},
		{
			name:    "fork_scheduled_at_head",
			stored:  withUpgrades(cparams.ChainConfig{}),
			new:     withUpgrades(cparams.ChainConfig{DurangoTimestamp: new(uint64(100))}),
			wantErr: compatErr(nil, new(uint64(100))),
		},
		{
			name: "first_incompatible_fork_reported",
			stored: withUpgrades(cparams.ChainConfig{
				ApricotPhase1Timestamp: new(uint64(10)),
				DurangoTimestamp:       new(uint64(10)),
			}),
			new: withUpgrades(cparams.ChainConfig{
				ApricotPhase1Timestamp: new(uint64(20)),
				DurangoTimestamp:       new(uint64(30)),
			}),
			wantErr: compatErr(new(uint64(10)), new(uint64(20))),
		},
		{
			name:   "stored_without_extra",
			stored: &ethparams.ChainConfig{},
			new:    &ethparams.ChainConfig{},
		},
		{
			name:    "new_without_extra",
			stored:  withUpgrades(cparams.ChainConfig{ApricotPhase5Timestamp: new(uint64)}),
			new:     &ethparams.ChainConfig{},
			wantErr: compatErr(new(uint64), nil),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error // avoid typed-nil footgun
			if e := tt.stored.CheckCompatible(tt.new, 0 /*block number*/, headTime); e != nil {
				err = e
			}
			if diff := testerr.Diff(err, tt.wantErr); diff != "" {
				t.Errorf("%T.CheckCompatible() %s", tt.stored, diff)
			}
		})
	}
}

// TestChainConfigJSON guards the JSON encoding of [cparams.ChainConfig].
func TestChainConfigJSON(t *testing.T) {
	tests := []struct {
		name   string
		config cparams.ChainConfig
		json   string
	}{
		{
			name: "empty",
			json: `{"chainId": 1}`,
		},
		{
			name: "all_upgrades",
			config: cparams.ChainConfig{
				ApricotPhase1Timestamp:     new(uint64(1)),
				ApricotPhase2Timestamp:     new(uint64(2)),
				ApricotPhase3Timestamp:     new(uint64(3)),
				ApricotPhase4Timestamp:     new(uint64(4)),
				ApricotPhase5Timestamp:     new(uint64(5)),
				ApricotPhasePre6Timestamp:  new(uint64(6)),
				ApricotPhase6Timestamp:     new(uint64(7)),
				ApricotPhasePost6Timestamp: new(uint64(8)),
				BanffTimestamp:             new(uint64(9)),
				CortinaTimestamp:           new(uint64(10)),
				DurangoTimestamp:           new(uint64(11)),
				EtnaTimestamp:              new(uint64(12)),
				FortunaTimestamp:           new(uint64(13)),
				GraniteTimestamp:           new(uint64(14)),
				HeliconTimestamp:           new(uint64(15)),
				IglooTimestamp:             new(uint64(16)),
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
			config: cparams.ChainConfig{
				ApricotPhase1Timestamp: new(uint64),
				DurangoTimestamp:       new(uint64(100)),
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
				config.SnowCtx = snowtest.Context(t, snowtest.CChainID)
				config.PrecompileConfigs = []precompileconfig.Config{
					warp.NewDefaultConfig(new(uint64)),
				}

				// ChainID is required.
				ethCfg := corethparams.WithExtra(&ethparams.ChainConfig{ChainID: big.NewInt(1)}, &config)
				got, err := json.Marshal(ethCfg)
				require.NoError(t, err)
				require.JSONEq(t, tt.json, string(got))
			})

			t.Run("unmarshal", func(t *testing.T) {
				var got ethparams.ChainConfig
				require.NoError(t, json.Unmarshal([]byte(tt.json), &got))
				require.Equal(t, big.NewInt(1), got.ChainID, "ChainID")
				require.Equal(t, &tt.config, corethparams.GetExtra(&got), "corethparams.GetExtra()")
			})
		})
	}
}

// TestIsForked tests fork activation through [cparams.ChainConfig.IsApricotPhase1];
// every `Is<Fork>` method shares the same activation logic.
func TestIsForked(t *testing.T) {
	tests := []struct {
		name     string
		ts       *uint64
		head     uint64
		isForked bool
	}{
		{
			name:     "nil fork at 0",
			ts:       nil,
			head:     0,
			isForked: false,
		},
		{
			name:     "nil fork at non-zero",
			ts:       nil,
			head:     100,
			isForked: false,
		},
		{
			name:     "zero fork at genesis",
			ts:       new(uint64),
			head:     0,
			isForked: true,
		},
		{
			name:     "pre fork timestamp",
			ts:       new(uint64(100)),
			head:     50,
			isForked: false,
		},
		{
			name:     "at fork timestamp",
			ts:       new(uint64(100)),
			head:     100,
			isForked: true,
		},
		{
			name:     "post fork timestamp",
			ts:       new(uint64(100)),
			head:     150,
			isForked: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &cparams.ChainConfig{ApricotPhase1Timestamp: tt.ts}
			require.Equalf(t, tt.isForked, c.IsApricotPhase1(tt.head), "%T.IsApricotPhase1(%d)", c, tt.head)
		})
	}
}
