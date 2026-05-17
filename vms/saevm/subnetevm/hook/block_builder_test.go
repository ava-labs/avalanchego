// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hook

import (
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/constants"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/paramstest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"

	subnetevmparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
)

// TestMain registers libevm extras required by [subnetevmparams.GetExtra]
// and [customtypes.GetHeaderExtra] used in this package's tests.
func TestMain(m *testing.M) {
	core.RegisterExtras()
	subnetevmparams.RegisterExtras()
	customtypes.Register()
	os.Exit(m.Run())
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

	chainCfg := subnetevmparams.Copy(paramstest.ForkToChainConfig[upgradetest.Helicon])
	subnetevmparams.GetExtra(&chainCfg).AllowFeeRecipients = true // route through customCoinbase branch in resolveCoinbase

	parent := &types.Header{Number: big.NewInt(1), Time: parentTimeMS / 1000}
	customtypes.SetHeaderExtra(parent, &customtypes.HeaderExtra{
		TimeMilliseconds: utils.PointerTo(parentTimeMS),
		MinDelayExcess:   utils.PointerTo(acp226.InitialDelayExcess),
	})
	tx := types.NewTx(&types.DynamicFeeTx{Gas: 21_000, Value: big.NewInt(0)})
	settled := &types.Header{Number: big.NewInt(0), Time: parent.Time}

	// Builder side: stamp `builderCoinbase` into a real block.
	builderPts := NewPoints(
		nil, nil, &chainCfg,
		func() time.Time { return time.UnixMilli(int64(nowMS)) },
		nil, nil, nil, builderCoinbase,
	)
	builderHdr, err := builderPts.blockBuilder.BuildHeader(parent)
	require.NoError(t, err)
	builderBlock, err := builderPts.blockBuilder.BuildBlock(
		builderHdr, nil, nil, []*types.Transaction{tx}, nil, nil, settled,
	)
	require.NoError(t, err)
	require.Equal(t, builderCoinbase, builderBlock.Header().Coinbase,
		"sanity: builder must stamp its own Coinbase")

	// Rebuilder side: a DIFFERENT node (rebuilderCoinbase != builderCoinbase)
	// rebuilds builderBlock. Its rebuilt block must carry builderCoinbase.
	rebuilderPts := NewPoints(nil, nil, &chainCfg, nil, nil, nil, nil, rebuilderCoinbase)
	rebuilder, err := rebuilderPts.BlockRebuilderFrom(builderBlock)
	require.NoError(t, err)
	rebuiltHdr, err := rebuilder.BuildHeader(parent)
	require.NoError(t, err)
	rebuilt, err := rebuilder.BuildBlock(
		rebuiltHdr, nil, nil, []*types.Transaction{tx}, nil, nil, settled,
	)
	require.NoError(t, err)
	require.Equal(t, builderCoinbase, rebuilt.Header().Coinbase,
		"rebuilt block MUST carry the builder's Coinbase from the received header")
	require.Equal(t, builderBlock.Hash(), rebuilt.Hash())
}

// TestBlockRebuildRejectsForgedCoinbase covers the dual of
// [TestBlockRebuilderFromOverridesValidatorCoinbase]: in DETERMINISTIC
// branches of [resolveCoinbase] (where the chain pins a unique correct
// Coinbase per block), a builder that ships a different `Coinbase` MUST
// produce a block whose rebuilt twin has a DIFFERENT hash, so SAE's
// `errHashMismatch` fires in [VM.VerifyBlock]. This is what makes the
// pinned branches enforceable and not just advisory.
//
// Two pinned branches:
//   - `!AllowFeeRecipients && rewardmanager not enabled` => MUST be BlackholeAddr.
//   - `rewardmanager enabled && stored allowFeeRecipients == false` =>
//     MUST be the stored reward address. (Not exercised here; it requires
//     a `worstcaseState` with the precompile slot pre-populated, which
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

	chainCfg := subnetevmparams.Copy(paramstest.ForkToChainConfig[upgradetest.Helicon])
	// Vanilla Helicon: AllowFeeRecipients=false, no rewardmanager precompile
	// => deterministic branch in resolveCoinbase MUST stamp BlackholeAddr.

	parent := &types.Header{Number: big.NewInt(1), Time: parentTimeMS / 1000}
	customtypes.SetHeaderExtra(parent, &customtypes.HeaderExtra{
		TimeMilliseconds: utils.PointerTo(parentTimeMS),
		MinDelayExcess:   utils.PointerTo(acp226.InitialDelayExcess),
	})
	tx := types.NewTx(&types.DynamicFeeTx{Gas: 21_000, Value: big.NewInt(0)})
	settled := &types.Header{Number: big.NewInt(0), Time: parent.Time}

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
		TimeMilliseconds: utils.PointerTo(nowMS),
		MinDelayExcess:   utils.PointerTo(acp226.InitialDelayExcess),
	})
	forgedBlock := types.NewBlockWithHeader(forgedHdr)

	rebuilderPts := NewPoints(nil, nil, &chainCfg, nil, nil, nil, nil, forgedCoinbase /* same as builder; doesn't matter */)
	rebuilder, err := rebuilderPts.BlockRebuilderFrom(forgedBlock)
	require.NoError(t, err)
	rebuiltHdr, err := rebuilder.BuildHeader(parent)
	require.NoError(t, err)
	rebuilt, err := rebuilder.BuildBlock(
		rebuiltHdr, nil, nil, []*types.Transaction{tx}, nil, nil, settled,
	)
	require.NoError(t, err)

	require.Equal(t, constants.BlackholeAddr, rebuilt.Header().Coinbase,
		"deterministic branch MUST stamp BlackholeAddr regardless of received Coinbase")
	require.NotEqual(t, forgedCoinbase, rebuilt.Header().Coinbase,
		"rebuilder MUST NOT echo the forged Coinbase")
	require.NotEqual(t, forgedBlock.Hash(), rebuilt.Hash(),
		"rebuilt hash MUST diverge from forged block's hash; this is what triggers errHashMismatch in VerifyBlock")
}
