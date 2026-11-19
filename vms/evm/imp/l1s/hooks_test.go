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
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/paramstest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/vms/evm/gastime"
	"github.com/ava-labs/avalanchego/vms/evm/hook"
	"github.com/ava-labs/avalanchego/vms/evm/imp/blackhole"
	"github.com/ava-labs/avalanchego/vms/evm/imp/saeconfig"

	legacyparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
)

func TestBuildHeaderIglooOverride(t *testing.T) {
	networkUpgrades := upgradetest.GetConfig(upgrade.Igloo)
	iglooTime := networkUpgrades.IglooTime.Add(time.Hour)
	snowCtx := newSnowCtx(t, networkUpgrades)
	chainCfg := legacyparams.Copy(paramstest.ForkToChainConfig[upgrade.Igloo])
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

// TestBlockRebuilderFromOverridesValidatorCoinbase asserts that, when builders
// choose the coinbase, a rebuilt block MUST keep the received block's coinbase.
func TestBlockRebuilderFromOverridesValidatorCoinbase(t *testing.T) {
	const (
		parentTimeMS = uint64(2_000_000_000_000)
		nowMS        = parentTimeMS + 5_000 // beyond the initial minimum delay
	)
	var (
		builderCoinbase   = common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		rebuilderCoinbase = common.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	)

	chainCfg := legacyparams.Copy(paramstest.ForkToChainConfig[upgrade.Igloo])
	legacyparams.GetExtra(&chainCfg).AllowFeeRecipients = true

	parent := &types.Header{Number: big.NewInt(1), Time: parentTimeMS / 1000}
	customtypes.SetHeaderExtra(parent, &customtypes.HeaderExtra{
		TimeMilliseconds: new(parentTimeMS),
		MinDelayExcess:   new(acp226.InitialDelayExcess),
	})
	tx := types.NewTx(&types.DynamicFeeTx{Gas: 21_000, Value: big.NewInt(0)})
	settledRules := chainCfg.Rules(big.NewInt(0), true /*isMerge*/, parent.Time)
	// Any marker works, as long as both sides use it.
	settledMarker := hook.Settled{Height: 1, GasUnix: parent.Time, GasNumerator: 1, Excess: 1}

	// The state is nil because no precompile that BuildBlock reads is enabled.
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

// TestBlockRebuildRejectsForgedCoinbase asserts that, when the chain
// determines the coinbase, a block with any other coinbase MUST NOT rebuild to
// the same hash.
func TestBlockRebuildRejectsForgedCoinbase(t *testing.T) {
	const (
		parentTimeMS = uint64(2_000_000_000_000)
		nowMS        = parentTimeMS + 5_000
	)
	forgedCoinbase := common.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	chainCfg := legacyparams.Copy(paramstest.ForkToChainConfig[upgrade.Igloo])

	parent := &types.Header{Number: big.NewInt(1), Time: parentTimeMS / 1000}
	customtypes.SetHeaderExtra(parent, &customtypes.HeaderExtra{
		TimeMilliseconds: new(parentTimeMS),
		MinDelayExcess:   new(acp226.InitialDelayExcess),
	})
	tx := types.NewTx(&types.DynamicFeeTx{Gas: 21_000, Value: big.NewInt(0)})
	settledRules := chainCfg.Rules(big.NewInt(0), true /*isMerge*/, parent.Time)
	settledMarker := hook.Settled{Height: 1, GasUnix: parent.Time, GasNumerator: 1, Excess: 1}

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

	rebuilderPts := newHooks(nil, &chainCfg, nil, saeconfig.Desired{}, nil, forgedCoinbase, nil)
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

// TestBuildBlockStampsDefaultGasConfig asserts that a block built while
// gaspricemanager is disabled MUST carry the default gas config, because header
// fields can't be removed once populated.
func TestBuildBlockStampsDefaultGasConfig(t *testing.T) {
	const (
		parentTimeMS = uint64(2_000_000_000_000)
		nowMS        = parentTimeMS + 5_000 // beyond the initial minimum delay
	)
	chainCfg := legacyparams.Copy(paramstest.ForkToChainConfig[upgrade.Igloo])
	parent := &types.Header{Number: big.NewInt(1), Time: parentTimeMS / 1000}
	customtypes.SetHeaderExtra(parent, &customtypes.HeaderExtra{
		TimeMilliseconds: new(parentTimeMS),
		MinDelayExcess:   new(acp226.InitialDelayExcess),
	})
	b := newHooks(
		nil, &chainCfg,
		func() time.Time { return time.UnixMilli(int64(nowMS)) },
		saeconfig.Desired{}, nil, common.Address{}, nil,
	).builder

	header, err := b.BuildHeader(parent)
	require.NoError(t, err, "BuildHeader()")
	tx := types.NewTx(&types.DynamicFeeTx{Gas: 21_000, Value: big.NewInt(0)})
	settledRules := chainCfg.Rules(big.NewInt(0), true /*isMerge*/, parent.Time)
	block, err := b.BuildBlock(header, nil, []*types.Transaction{tx}, nil, hook.Settled{}, settledRules, nil)
	require.NoError(t, err, "BuildBlock()")

	he := customtypes.GetHeaderExtra(block.Header())
	require.Equal(t, new(uint64(gastime.DefaultTargetToExcessScaling)), he.TargetToExcessScaling, "BuildBlock() TargetToExcessScaling")
	require.Equal(t, new(uint64(gastime.DefaultMinPrice)), he.MinPrice, "BuildBlock() MinPrice")
}
