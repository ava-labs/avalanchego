// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp/warptest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	corethparams "github.com/ava-labs/avalanchego/graft/coreth/params"
	corethwarp "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	avalanchewarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	ethereum "github.com/ava-labs/libevm"
	ethparams "github.com/ava-labs/libevm/params"
)

// useCorethPrecompiles swaps every contract in the SAE precompile set, and
// the warp predicater, for coreth's implementations. The set, the hooks, and
// the header plumbing stay SAE's, so the only difference between the two SUTs
// is the contract and predicater code.
//
// Blind spot: because the set stays SAE's, the control arm shares SAE's
// Active list, so this test cannot detect a divergence in the EIP-2929 warm
// list. That list is pinned against coreth's in [TestNewPrecompiles].
func useCorethPrecompiles(cfg *ethparams.ChainConfig) {
	extra := corethparams.GetExtra(cfg)
	set := extra.Precompiles

	// A copy of the chain config without the set resolves precompiles the
	// way coreth does: built-in tables plus the warp module activated at
	// Durango.
	legacyEth := *cfg
	legacyExtra := *extra
	legacyExtra.Precompiles = nil
	durango := utils.TimeToNewUint64(extra.SnowCtx.NetworkUpgrades.DurangoTime)
	legacyExtra.UpgradeConfig = extras.UpgradeConfig{
		PrecompileUpgrades: []extras.PrecompileUpgrade{{Config: corethwarp.NewDefaultConfig(durango)}},
	}
	legacyCfg := corethparams.WithExtra(&legacyEth, &legacyExtra)

	// Any post-Helicon timestamp yields Granite rules.
	rules := corethparams.RulesExtra(*corethparams.GetRulesExtra(
		legacyCfg.Rules(big.NewInt(1), corethparams.IsMergeTODO, *utils.TimeToNewUint64(extra.SnowCtx.NetworkUpgrades.HeliconTime)+1),
	))
	for addr := range set.Contracts {
		c, ok := rules.PrecompileOverride(addr)
		if !ok {
			panic("coreth has no precompile at " + addr.Hex())
		}
		set.Contracts[addr] = c
	}
	set.Predicaters[warp.ContractAddress] = corethwarp.NewDefaultConfig(durango)
}

type diffCase struct {
	name       string
	to         func(contracts map[vm.OpCode]common.Address) common.Address
	data       []byte
	msg        *avalanchewarp.Message // carried as a predicate when non-nil
	extraPreds []*avalanchewarp.Message
}

// TestPrecompilesMatchCoreth executes the same transactions on a SUT using
// SAE's precompiles and on a SUT using coreth's, and requires identical
// receipts, logs, header predicate results, and post-execution state roots.
func TestPrecompilesMatchCoreth(t *testing.T) {
	var (
		// Each SUT gets its own wallet so that both track nonces from zero.
		// Keys are deterministic, so both wallets sign identical transactions
		// from the same, identically funded account.
		saeWallet    = saetest.NewUNSAFEWallet(t, 1, types.LatestSigner(saetest.ChainConfig()))
		corethWallet = saetest.NewUNSAFEWallet(t, 1, types.LatestSigner(saetest.ChainConfig()))
		vdrs         = warptest.NewValidators(t, warptest.WithMinimum(2))
		// One forwarding contract per call opcode, all targeting warp.
		forwarders = map[vm.OpCode]common.Address{
			vm.CALL:         {'c', 'a', 'l', 'l'},
			vm.CALLCODE:     {'c', 'o', 'd', 'e'},
			vm.DELEGATECALL: {'d', 'l', 'g', 't'},
			vm.STATICCALL:   {'s', 't', 'a', 't'},
		}
		// Forwarders targeting a deprecated native-asset address.
		deprecatedCall     = common.Address{'d', 'e', 'p', 'c'}
		deprecatedDelegate = common.Address{'d', 'e', 'p', 'd'}
		p256Call           = common.Address{'p', '2', '5', '6'}
	)

	newDiffSUT := func(t *testing.T, opts ...sutOption) (context.Context, *SUT) {
		t.Helper()
		base := []sutOption{
			withMaxAllocFor(saeWallet.Addresses()...),
			withValidators(vdrs),
			withAccount(deprecatedCall, types.Account{Code: callAndLogCode(t, vm.CALL, nativeAssetCallAddr)}),
			withAccount(deprecatedDelegate, types.Account{Code: callAndLogCode(t, vm.DELEGATECALL, nativeAssetCallAddr)}),
			withAccount(p256Call, types.Account{Code: callAndLogCode(t, vm.CALL, p256VerifyAddress)}),
		}
		for op, addr := range forwarders {
			base = append(base, withAccount(addr, types.Account{Code: callAndLogCode(t, op, warp.ContractAddress)}))
		}
		return newSUT(t, append(base, opts...)...)
	}

	ctxSAE, sae := newDiffSUT(t)
	ctxCoreth, coreth := newDiffSUT(t, withCorethPrecompiles())

	must := func(b []byte, err error) []byte {
		t.Helper()
		require.NoError(t, err)
		return b
	}
	// Both SUTs share chain and network IDs, so one signed message serves both.
	addressedMsg := sae.newAddressedCallMessage(t, common.Address{1, 2, 3}.Bytes(), []byte{4, 5, 6})
	hashMsg := sae.newHashMessage(t, ids.GenerateTestID())

	warpTo := func(op vm.OpCode) func(map[vm.OpCode]common.Address) common.Address {
		return func(m map[vm.OpCode]common.Address) common.Address { return m[op] }
	}
	fixed := func(addr common.Address) func(map[vm.OpCode]common.Address) common.Address {
		return func(map[vm.OpCode]common.Address) common.Address { return addr }
	}

	cases := []diffCase{
		{name: "send_direct", to: fixed(warp.ContractAddress), data: must(warp.PackSendWarpMessage([]byte("hello")))},
		{name: "send_via_call", to: warpTo(vm.CALL), data: must(warp.PackSendWarpMessage([]byte("hello")))},
		{name: "send_via_callcode", to: warpTo(vm.CALLCODE), data: must(warp.PackSendWarpMessage([]byte("hello")))},
		{name: "send_via_delegatecall", to: warpTo(vm.DELEGATECALL), data: must(warp.PackSendWarpMessage([]byte("hello")))},
		{name: "send_via_staticcall", to: warpTo(vm.STATICCALL), data: must(warp.PackSendWarpMessage([]byte("hello")))},
		{name: "get_blockchain_id", to: warpTo(vm.CALL), data: must(warp.PackGetBlockchainID())},
		{name: "get_message_valid", to: warpTo(vm.CALL), data: must(warp.PackGetVerifiedWarpMessage(0)), msg: vdrs.Sign(t, addressedMsg)},
		{name: "get_message_invalid_signature", to: warpTo(vm.CALL), data: must(warp.PackGetVerifiedWarpMessage(0)), msg: warptest.IncorrectlySign(t, addressedMsg)},
		{name: "get_message_missing_index", to: warpTo(vm.CALL), data: must(warp.PackGetVerifiedWarpMessage(1)), msg: vdrs.Sign(t, addressedMsg)},
		{name: "get_message_wrong_payload", to: warpTo(vm.CALL), data: must(warp.PackGetVerifiedWarpMessage(0)), msg: vdrs.Sign(t, hashMsg)},
		{name: "get_message_second_of_two", to: warpTo(vm.CALL), data: must(warp.PackGetVerifiedWarpMessage(1)), msg: warptest.IncorrectlySign(t, addressedMsg), extraPreds: []*avalanchewarp.Message{vdrs.Sign(t, addressedMsg)}},
		{name: "staticcall_get_message", to: warpTo(vm.STATICCALL), data: must(warp.PackGetVerifiedWarpMessage(0)), msg: vdrs.Sign(t, addressedMsg)},
		{name: "get_hash_valid", to: warpTo(vm.CALL), data: must(warp.PackGetVerifiedWarpBlockHash(0)), msg: vdrs.Sign(t, hashMsg)},
		{name: "get_hash_invalid_signature", to: warpTo(vm.CALL), data: must(warp.PackGetVerifiedWarpBlockHash(0)), msg: warptest.IncorrectlySign(t, hashMsg)},
		{name: "unknown_selector", to: warpTo(vm.CALL), data: []byte{0xde, 0xad, 0xbe, 0xef, 0x00}},
		{name: "short_input", to: warpTo(vm.CALL), data: []byte{0x01, 0x02}},
		{name: "selector_only_get_message", to: warpTo(vm.CALL), data: warp.ABI.Methods["getVerifiedWarpMessage"].ID},
		{name: "deprecated_call", to: fixed(deprecatedCall), data: []byte{1, 2, 3, 4}},
		{name: "deprecated_delegatecall", to: fixed(deprecatedDelegate), data: []byte{1, 2, 3, 4}},
		{name: "p256_garbage", to: fixed(p256Call), data: make([]byte, 160)},
	}

	type outcome struct {
		status  uint64
		gasUsed uint64
		logs    []*types.Log
		extra   []byte
		root    common.Hash
	}
	accessList := func(c diffCase) types.AccessList {
		msgs := append([]*avalanchewarp.Message{}, c.extraPreds...)
		if c.msg != nil {
			msgs = append([]*avalanchewarp.Message{c.msg}, msgs...)
		}
		return warpAccessList(msgs...)
	}
	// SAE charges at least gasLimit/Lambda for every transaction, which would
	// hide any precompile gas difference below that floor. Each case therefore
	// runs with coreth's own gas estimate as its limit, so that the receipts'
	// gas used reflects actual execution on both arms. Cases whose inner call
	// consumes all forwarded gas estimate at the block gas limit; they are
	// capped, and use (nearly) all of the capped limit regardless.
	const maxGas = 1_000_000
	estimateGas := func(ctx context.Context, t *testing.T, c diffCase) uint64 {
		t.Helper()
		to := c.to(forwarders)
		gas, err := coreth.ethclient.EstimateGas(ctx, ethereum.CallMsg{
			From:       corethWallet.Addresses()[0],
			To:         &to,
			GasFeeCap:  big.NewInt(1),
			Data:       c.data,
			AccessList: accessList(c),
		})
		require.NoErrorf(t, err, "%T.EstimateGas(%s)", coreth.ethclient, c.name)
		return min(gas, maxGas)
	}
	run := func(ctx context.Context, t *testing.T, sut *SUT, wallet *saetest.Wallet, gas uint64, c diffCase) outcome {
		t.Helper()
		to := c.to(forwarders)
		tx := wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			To:         &to,
			Gas:        gas,
			GasFeeCap:  big.NewInt(1),
			Data:       c.data,
			AccessList: accessList(c),
		})
		require.NoErrorf(t, sut.ethclient.SendTransaction(ctx, tx), "%T.SendTransaction(%s)", sut.ethclient, c.name)
		sut.waitForPendingEthTxs(ctx, t, tx)
		blk := sut.runConsensusLoop(ctx, t, withBlockContext(&block.Context{}))
		receipts := blk.Receipts()
		require.Lenf(t, receipts, 1, "%T.Receipts() for %s", blk, c.name)
		for _, l := range receipts[0].Logs {
			// Block hash and tx index are plumbing, not precompile behaviour.
			l.BlockHash, l.TxHash, l.TxIndex, l.Index = common.Hash{}, common.Hash{}, 0, 0
		}
		return outcome{
			status:  receipts[0].Status,
			gasUsed: receipts[0].GasUsed,
			logs:    receipts[0].Logs,
			extra:   blk.Header().Extra,
			root:    blk.PostExecutionStateRoot(),
		}
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gas := estimateGas(ctxCoreth, t, c)
			want := run(ctxCoreth, t, coreth, corethWallet, gas, c)
			got := run(ctxSAE, t, sae, saeWallet, gas, c)
			require.Equal(t, want.status, got.status, "receipt status")
			require.Equal(t, want.gasUsed, got.gasUsed, "gas used")
			require.Equal(t, want.logs, got.logs, "logs")
			require.Equal(t, want.extra, got.extra, "header predicate results")
			require.Equal(t, want.root, got.root, "post-execution state root")
		})
	}

	// The control arm must really be running coreth's code, or the test
	// compared SAE against itself. The predicater types are distinct and
	// comparable; the contract values are both libevm closures and are not,
	// so only their presence is checked.
	saeSet := corethparams.GetExtra(sae.chainConfig).Precompiles
	corethSet := corethparams.GetExtra(coreth.chainConfig).Precompiles
	require.IsType(t, warp.Predicater{}, saeSet.Predicaters[warp.ContractAddress], "SAE arm predicater")
	require.IsType(t, &corethwarp.Config{}, corethSet.Predicaters[warp.ContractAddress], "control arm predicater")
	require.NotNil(t, corethSet.Contracts[warp.ContractAddress], "control arm warp contract")
}
