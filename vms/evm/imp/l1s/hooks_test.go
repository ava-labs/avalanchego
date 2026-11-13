// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/acp226"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/paramstest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/gaspricemanager"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/hook"
	"github.com/ava-labs/avalanchego/vms/evm/imp/blackhole"
	"github.com/ava-labs/avalanchego/vms/evm/imp/saeconfig"

	legacyparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	saeparams "github.com/ava-labs/avalanchego/vms/evm/params"
)

func TestBuildHeaderIglooOverride(t *testing.T) {
	networkUpgrades := upgradetest.GetConfig(upgradetest.Igloo)
	iglooTime := networkUpgrades.IglooTime.Add(time.Hour)
	snowCtx := newSnowCtx(t, networkUpgrades)
	chainCfg := legacyparams.Copy(paramstest.ForkToChainConfig[upgradetest.Igloo])
	legacyparams.GetExtra(&chainCfg).Override(&extras.NetworkUpgrades{
		IglooTimestamp: new(uint64(iglooTime.Unix())), //#nosec G115 -- known positive test timestamp
	})
	parent := &types.Header{
		Number: new(big.Int),
		Time:   uint64(iglooTime.Add(-10 * time.Second).Unix()), //#nosec G115 -- known positive test timestamp
	}

	tests := []struct {
		name    string
		now     time.Time
		wantErr error
	}{
		{
			name:    "before_override",
			now:     iglooTime.Add(-time.Second),
			wantErr: errIglooUnactivated,
		},
		{
			name: "at_override",
			now:  iglooTime,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			points := newHooks(
				snowCtx,
				&chainCfg,
				func() time.Time { return test.now },
				saeconfig.Desired{},
				nil,
				common.Address{},
				nil,
			)
			header, err := points.builder.BuildHeader(parent)
			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr, "builder.BuildHeader() at %s", test.now)
				return
			}
			require.NoError(t, err, "builder.BuildHeader() at %s", test.now)
			require.Equal(t, uint64(test.now.Unix()), header.Time, "builder.BuildHeader() at %s", test.now) //#nosec G115 -- known positive test timestamp
		})
	}
}

func TestGasConfigAfterGenesisCanonicalTarget(t *testing.T) {
	tests := []struct {
		name         string
		storedTarget gas.Gas
		want         gas.Gas
	}{
		{
			name:         "exactly_representable",
			storedTarget: 3_000_000,
			want:         3_000_000,
		},
		{
			name:         "rounded_up",
			storedTarget: 1_000_000_000,
			want:         1_000_000_006,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			chainCfg := legacyparams.Copy(paramstest.ForkToChainConfig[upgradetest.Igloo])
			config := &commontype.GasPriceConfig{
				TargetGas:    uint64(test.storedTarget),
				MinGasPrice:  1,
				TimeToDouble: 60,
			}
			legacyparams.GetExtra(&chainCfg).GenesisPrecompiles = extras.Precompiles{
				gaspricemanager.ConfigKey: gaspricemanager.NewConfig(
					new(uint64(0)),
					nil,
					nil,
					nil,
					config,
				),
			}
			genesis := &types.Header{
				Number: new(big.Int),
				Time:   saeparams.TauSeconds,
			}

			got, _ := newHooks(nil, &chainCfg, nil, saeconfig.Desired{}, nil, common.Address{}, nil).GasConfigAfter(genesis)
			require.Equal(t, test.want, got, "hooks.GasConfigAfter() for stored target %d", test.storedTarget)
		})
	}
}

// TestBlockRebuilderFromOverridesValidatorCoinbase: in operator-chosen
// Coinbase branches the rebuilt block MUST carry the BUILDER's Coinbase
// from the received header, else differing local `Config.FeeRecipient`
// would cause hash mismatches in [VM.VerifyBlock].
func TestBlockRebuilderFromOverridesValidatorCoinbase(t *testing.T) {
	const (
		parentTimeMS = uint64(2_000_000_000_000) // well past SubnetEVM activation
		nowMS        = parentTimeMS + 5_000      // > InitialDelayExcess (~2s)
	)
	var (
		builderCoinbase   = common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		rebuilderCoinbase = common.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	)

	chainCfg := legacyparams.Copy(paramstest.ForkToChainConfig[upgradetest.Igloo])
	legacyparams.GetExtra(&chainCfg).AllowFeeRecipients = true // route through customCoinbase branch in resolveCoinbase

	parent := &types.Header{Number: big.NewInt(1), Time: parentTimeMS / 1000}
	customtypes.SetHeaderExtra(parent, &customtypes.HeaderExtra{
		TimeMilliseconds: new(parentTimeMS),
		MinDelayExcess:   new(acp226.InitialDelayExcess),
	})
	tx := types.NewTx(&types.DynamicFeeTx{Gas: 21_000, Value: big.NewInt(0)})
	settledRules := chainCfg.Rules(big.NewInt(0), true /*isMerge*/, parent.Time)
	// Any non-zero marker works; it only needs to match on both sides for the
	// hash-equality assertion.
	settledMarker := hook.Settled{Height: 1, GasUnix: parent.Time, GasNumerator: 1, Excess: 1}

	// Builder side: stamp `builderCoinbase` into a real block. The worst-case
	// state reader is nil because neither rewardmanager nor gaspricemanager
	// is enabled, so BuildBlock never reads it.
	builderPts := newHooks(
		nil, &chainCfg,
		func() time.Time { return time.UnixMilli(int64(nowMS)) },
		saeconfig.Desired{}, nil, builderCoinbase, nil,
	)
	builderHdr, err := builderPts.builder.BuildHeader(parent)
	require.NoError(t, err)
	builderBlock, err := builderPts.builder.BuildBlock(
		builderHdr, nil, []*types.Transaction{tx}, nil, settledMarker, settledRules, nil,
	)
	require.NoError(t, err)
	require.Equal(t, builderCoinbase, builderBlock.Header().Coinbase,
		"sanity: builder must stamp its own Coinbase")

	// Rebuilder side: a DIFFERENT node (rebuilderCoinbase != builderCoinbase)
	// rebuilds builderBlock. Its rebuilt block must carry builderCoinbase.
	rebuilderPts := newHooks(nil, &chainCfg, nil, saeconfig.Desired{}, nil, rebuilderCoinbase, nil)
	rebuilder, err := rebuilderPts.BlockRebuilderFrom(builderBlock)
	require.NoError(t, err)
	rebuiltHdr, err := rebuilder.BuildHeader(parent)
	require.NoError(t, err)
	rebuilt, err := rebuilder.BuildBlock(
		rebuiltHdr, nil, []*types.Transaction{tx}, nil, settledMarker, settledRules, nil,
	)
	require.NoError(t, err)
	require.Equal(t, builderCoinbase, rebuilt.Header().Coinbase,
		"rebuilt block MUST carry the builder's Coinbase from the received header")
	require.Equal(t, builderBlock.Hash(), rebuilt.Hash())
}

// TestBuildBlockWarnsOnBurnedFees asserts that building a block that burns fees
// the chain allows this node to claim MUST warn, and that rebuilding such a
// block MUST NOT.
func TestBuildBlockWarnsOnBurnedFees(t *testing.T) {
	const (
		parentTimeMS = uint64(2_000_000_000_000)
		nowMS        = parentTimeMS + 5_000
	)

	chainCfg := legacyparams.Copy(paramstest.ForkToChainConfig[upgradetest.Igloo])
	legacyparams.GetExtra(&chainCfg).AllowFeeRecipients = true

	parent := &types.Header{Number: big.NewInt(1), Time: parentTimeMS / 1000}
	customtypes.SetHeaderExtra(parent, &customtypes.HeaderExtra{
		TimeMilliseconds: new(parentTimeMS),
		MinDelayExcess:   new(acp226.InitialDelayExcess),
	})
	tx := types.NewTx(&types.DynamicFeeTx{Gas: 21_000, Value: big.NewInt(0)})
	settledRules := chainCfg.Rules(big.NewInt(0), true /*isMerge*/, parent.Time)
	settled := hook.Settled{Height: 1, GasUnix: parent.Time, GasNumerator: 1, Excess: 1}

	log := loggingtest.NewRecorder(logging.Warn)
	h := newHooks(
		&snow.Context{Log: log}, &chainCfg,
		func() time.Time { return time.UnixMilli(int64(nowMS)) },
		saeconfig.Desired{}, nil, blackhole.Address, nil,
	)
	hdr, err := h.builder.BuildHeader(parent)
	require.NoError(t, err)
	built, err := h.builder.BuildBlock(hdr, nil, []*types.Transaction{tx}, nil, settled, settledRules, nil)
	require.NoError(t, err)
	want := []*loggingtest.Record{{
		Level: logging.Warn,
		Msg:   `burning fees of a block built by this node, although the chain allows claiming them; set "fee-recipient" to claim them`,
	}}
	require.Equal(t, want, log.Records, "BuildBlock() logs")

	rebuilder, err := h.BlockRebuilderFrom(built)
	require.NoError(t, err)
	rebuiltHdr, err := rebuilder.BuildHeader(parent)
	require.NoError(t, err)
	_, err = rebuilder.BuildBlock(rebuiltHdr, nil, []*types.Transaction{tx}, nil, settled, settledRules, nil)
	require.NoError(t, err)
	require.Equal(t, want, log.Records, "rebuilt BuildBlock() logs")
}

// TestBlockRebuildRejectsForgedCoinbase covers the dual of
// [TestBlockRebuilderFromOverridesValidatorCoinbase]: in DETERMINISTIC
// branches of [resolveCoinbase] (where the chain pins a unique correct
// Coinbase per block), a builder that ships a different `Coinbase` MUST
// produce a block whose rebuilt twin has a DIFFERENT hash, so
// [sae.ErrHashMismatch] fires in [VM.VerifyBlock]. This is what makes the
// pinned branches enforceable and not just advisory.
//
// Two pinned branches:
//   - `!AllowFeeRecipients && rewardmanager not enabled` => MUST be BlackholeAddr.
//   - `rewardmanager enabled && stored allowFeeRecipients == false` =>
//     MUST be the stored reward address. (Not exercised here; it requires
//     a settled state with the rewardmanager slot populated, which
//     belongs to the integration tests.)
//
// We simulate the malicious builder by stamping a non-deterministic
// `Coinbase` into the received block's header BEFORE handing it to
// `BlockRebuilderFrom`. The rebuilder's `BuildBlock` then ignores that
// override and stamps the deterministic value. We assert both the
// resolved address and the resulting hash differ.
func TestBlockRebuildRejectsForgedCoinbase(t *testing.T) {
	const (
		parentTimeMS = uint64(2_000_000_000_000)
		nowMS        = parentTimeMS + 5_000
	)
	forgedCoinbase := common.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	chainCfg := legacyparams.Copy(paramstest.ForkToChainConfig[upgradetest.Igloo])
	// Vanilla Igloo: AllowFeeRecipients=false, no rewardmanager precompile
	// => deterministic branch in resolveCoinbase MUST stamp BlackholeAddr.

	parent := &types.Header{Number: big.NewInt(1), Time: parentTimeMS / 1000}
	customtypes.SetHeaderExtra(parent, &customtypes.HeaderExtra{
		TimeMilliseconds: new(parentTimeMS),
		MinDelayExcess:   new(acp226.InitialDelayExcess),
	})
	tx := types.NewTx(&types.DynamicFeeTx{Gas: 21_000, Value: big.NewInt(0)})
	settledRules := chainCfg.Rules(big.NewInt(0), true /*isMerge*/, parent.Time)
	settledMarker := hook.Settled{Height: 1, GasUnix: parent.Time, GasNumerator: 1, Excess: 1}

	// "Builder" forges by skipping resolveCoinbase entirely and stamping
	// `forgedCoinbase` directly into the header that will be served as the
	// received block. We don't need to call this side's `BuildBlock`; we
	// just need a `*types.Block` whose Coinbase is the forged value and
	// whose extras match what `BlockRebuilderFrom` reads.
	forgedHdr := &types.Header{
		Coinbase: forgedCoinbase,
		Time:     nowMS / 1000,
		Number:   new(big.Int).Add(parent.Number, common.Big1),
	}
	customtypes.SetHeaderExtra(forgedHdr, &customtypes.HeaderExtra{
		TimeMilliseconds: new(nowMS),
		MinDelayExcess:   new(acp226.InitialDelayExcess),
	})
	forgedBlock := types.NewBlockWithHeader(forgedHdr)

	rebuilderPts := newHooks(nil, &chainCfg, nil, saeconfig.Desired{}, nil, forgedCoinbase /* same as builder; doesn't matter */, nil)
	rebuilder, err := rebuilderPts.BlockRebuilderFrom(forgedBlock)
	require.NoError(t, err)
	rebuiltHdr, err := rebuilder.BuildHeader(parent)
	require.NoError(t, err)
	rebuilt, err := rebuilder.BuildBlock(
		rebuiltHdr, nil, []*types.Transaction{tx}, nil, settledMarker, settledRules, nil,
	)
	require.NoError(t, err)

	require.Equal(t, blackhole.Address, rebuilt.Header().Coinbase,
		"deterministic branch MUST stamp BlackholeAddr regardless of received Coinbase")
	require.NotEqual(t, forgedCoinbase, rebuilt.Header().Coinbase,
		"rebuilder MUST NOT echo the forged Coinbase")
	require.NotEqual(t, forgedBlock.Hash(), rebuilt.Hash(),
		"rebuilt hash MUST diverge from forged block's hash; this is what triggers sae.ErrHashMismatch in VerifyBlock")
}

func TestVerifyBlockSyntaxGasConfigGroup(t *testing.T) {
	one := new(uint64(1))
	tests := []struct {
		name    string
		extra   *customtypes.HeaderExtra
		wantErr error
	}{
		{name: "absent", extra: &customtypes.HeaderExtra{}},
		{
			name: "one_of_three",
			extra: &customtypes.HeaderExtra{
				GasConfigTargetToExcessScaling: one,
			},
			wantErr: errPartialGasConfig,
		},
		{
			name: "two_of_three",
			extra: &customtypes.HeaderExtra{
				GasConfigTargetToExcessScaling: one,
				GasConfigMinGasPrice:           one,
			},
			wantErr: errPartialGasConfig,
		},
		{
			name: "complete",
			extra: &customtypes.HeaderExtra{
				GasConfigTargetToExcessScaling: one,
				GasConfigMinGasPrice:           one,
				GasConfigStaticPricing:         one,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			header := customtypes.WithHeaderExtra(&types.Header{}, test.extra)
			err := (&hooks{}).VerifyBlockSyntax(types.NewBlockWithHeader(header))
			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr, "hooks.VerifyBlockSyntax()")
				return
			}
			require.NoError(t, err, "hooks.VerifyBlockSyntax()")
		})
	}
}
