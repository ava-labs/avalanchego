// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"math"
	"math/big"
	"slices"
	"time"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/holiman/uint256"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp/warptest"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"

	corethwarp "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	evmdatabase "github.com/ava-labs/avalanchego/vms/evm/database"
	avalanchewarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
	ethparams "github.com/ava-labs/libevm/params"
)

func (mm *modelMachine) actions() map[string]func(*rapid.T) {
	drawFrom := func(rt *rapid.T) int {
		return rapid.IntRange(0, len(mm.addrs)-1).Draw(rt, "from")
	}
	return map[string]func(*rapid.T){
		"buildBlock":   mm.buildBlock,
		"advanceClock": mm.advanceClock,
		"settle":       mm.settle,
		"issueTransfer": func(rt *rapid.T) {
			mm.modelCore.issueTransfer(rt, mm.ctx, mm.sut, drawFrom(rt))
		},
		"issueDeploy": func(rt *rapid.T) {
			mm.modelCore.issueDeploy(rt, mm.ctx, mm.sut, drawFrom(rt))
		},
		"issueStore": func(rt *rapid.T) {
			mm.modelCore.issueStore(rt, mm.ctx, mm.sut, drawFrom(rt))
		},
		"issueRevert": func(rt *rapid.T) {
			mm.modelCore.issueRevert(rt, mm.ctx, mm.sut, drawFrom(rt))
		},
		"issueWarpSend": func(rt *rapid.T) {
			mm.modelCore.issueWarpSend(rt, mm.ctx, mm.sut, drawFrom(rt))
		},
		"issueWarpReceive": func(rt *rapid.T) {
			mm.modelCore.issueWarpReceive(rt, mm.ctx, mm.sut, drawFrom(rt))
		},
		"provisionUTXO": func(rt *rapid.T) {
			mm.modelCore.provisionUTXO(rt, mm.sut)
		},
		"issueImport": func(rt *rapid.T) {
			mm.modelCore.issueImport(rt, mm.ctx, mm.sut, []*SUT{mm.sut}, mm.atomicWallet)
		},
		"issueExport": func(rt *rapid.T) {
			mm.modelCore.issueExport(rt, mm.ctx, mm.sut, mm.atomicWallet)
		},
		"restart": mm.restart,
		"":        mm.check,
	}
}

const maxPendingPerAccount = 8 // stay well under the pool's 16 account slots

func (c *modelCore) pendingCount(addr common.Address) int {
	n := 0
	for _, it := range c.m.pendingEth {
		if it.from == addr {
			n++
		}
	}
	return n
}

// canAfford reports whether addr's spendable balance still covers a
// worst-case pool cost of 1_000_000*txGasFeeCap, the fixed Gas every
// fixed-cost action (issueDeploy, issueStore, issueRevert, issueWarpSend,
// issueWarpReceive) sends at. Without this guard, a legitimately drained
// account (reachable within a run via high-fraction transfers) would see the
// pool correctly reject admission with ErrInsufficientFunds — not a VM bug,
// just an action that shouldn't have been attempted. Takes no gas parameter:
// every call site passes the same 1_000_000 (see the actions above), so a
// parameter would only ever have one value in practice (unparam).
func (c *modelCore) canAfford(addr common.Address) bool {
	return c.spendable(addr).CmpUint64(1_000_000*txGasFeeCap) >= 0
}

// issueTransfer sends a randomized value transfer from account fromIdx to a
// drawn recipient via sut, occasionally (1-in-10) drawing a value guaranteed
// to exceed the sender's spendable balance to exercise the pool's
// insufficient-funds rejection path.
//
//nolint:revive // context-as-argument: rt, not ctx, is this file's leading-parameter convention (mirrors *testing.T)
func (c *modelCore) issueTransfer(rt *rapid.T, ctx context.Context, sut *SUT, fromIdx int) {
	toIdx := rapid.IntRange(0, len(c.addrs)-1).Draw(rt, "to")
	from, to := c.addrs[fromIdx], c.addrs[toIdx]
	if c.pendingCount(from) >= maxPendingPerAccount {
		return
	}

	spendable := c.spendable(from)
	insufficient := rapid.IntRange(0, 9).Draw(rt, "insufficient") == 0

	var value *uint256.Int
	if insufficient {
		// 2*balance + 1 ETH guarantees pool-admission rejection regardless of
		// concurrently pending txs.
		value = new(uint256.Int).Add(
			new(uint256.Int).Lsh(c.m.balances[from], 1),
			uint256.MustFromDecimal("1000000000000000000"),
		)
	} else {
		// A random fraction of at most spendable/4, so several transfers can
		// stack without draining the account mid-flight.
		frac := rapid.Uint64().Draw(rt, "valueFrac")
		value = new(uint256.Int).Rsh(new(uint256.Int).Mul(spendable, uint256.NewInt(frac)), 66)
	}

	data := &types.DynamicFeeTx{
		To:        &to,
		Gas:       ethparams.TxGas,
		GasFeeCap: big.NewInt(txGasFeeCap),
		Value:     value.ToBig(),
	}
	ethTx := c.wallet.SetNonceAndSign(c.tb, fromIdx, data)
	err := sut.ethclient.SendTransaction(ctx, ethTx)

	if insufficient {
		if diff := testerr.Diff(err, testerr.Contains(core.ErrInsufficientFunds.Error())); diff != "" {
			rt.Fatalf("SendTransaction(overdrawn transfer) error (-want +got):\n%s", diff)
		}
		c.wallet.DecrementNonce(c.tb, fromIdx) // reuse the burned nonce
		return
	}
	require.NoErrorf(rt, err, "SendTransaction(transfer of %s)", value)
	c.trackPending(ethTx, &issuedTx{
		kind:  kindTransfer,
		from:  from,
		to:    to,
		value: value,
		cost:  new(uint256.Int).Add(value, uint256.NewInt(ethparams.TxGas*txGasFeeCap)),
	})
}

// advanceToBuildable moves the mock clock to the preference's earliest
// buildable time. MUST run before anything that reaches WaitForEvent: the
// VM's waitUntil computes its sleep from vm.now() once and then sleeps in
// real time, so a lagging mock clock stalls the test for real.
func (mm *modelMachine) advanceToBuildable() {
	earliest := earliestBuildTime(mm.sut.VM.VM.GetPreference())
	if mm.clock.Now().Before(earliest) {
		mm.clock.Set(earliest)
	}
}

// issueMinimalTransfer funds a block when nothing is pending (the VM refuses
// empty blocks): a zero-value self-transfer from the richest account, issued
// via sut. Returns the chosen account index so the networked machine can pin
// the account to the issuing node.
//
//nolint:revive // context-as-argument: see issueTransfer
func (c *modelCore) issueMinimalTransfer(rt *rapid.T, ctx context.Context, sut *SUT) int {
	richest := c.addrs[0]
	richestIdx := 0
	for i, addr := range c.addrs {
		if c.m.balances[addr].Cmp(c.m.balances[richest]) > 0 {
			richest, richestIdx = addr, i
		}
	}
	data := &types.DynamicFeeTx{
		To:        &richest,
		Gas:       ethparams.TxGas,
		GasFeeCap: big.NewInt(txGasFeeCap),
	}
	ethTx := c.wallet.SetNonceAndSign(c.tb, richestIdx, data)
	require.NoErrorf(rt, sut.ethclient.SendTransaction(ctx, ethTx), "SendTransaction(minimal transfer)")
	c.trackPending(ethTx, &issuedTx{
		kind:  kindTransfer,
		from:  richest,
		to:    richest,
		value: new(uint256.Int),
		cost:  uint256.NewInt(ethparams.TxGas * txGasFeeCap),
	})
	return richestIdx
}

// nextNonce is the account's next nonce: executed nonce + in-flight txs.
func (c *modelCore) nextNonce(addr common.Address) uint64 {
	n := c.m.nonces[addr]
	for _, it := range c.m.pendingEth {
		if it.from == addr {
			n++
		}
	}
	return n
}

//nolint:revive // context-as-argument: see issueTransfer
func (c *modelCore) issueDeploy(rt *rapid.T, ctx context.Context, sut *SUT, fromIdx int) {
	from := c.addrs[fromIdx]
	if c.pendingCount(from) >= maxPendingPerAccount {
		return
	}
	if !c.canAfford(from) {
		return // can't cover worst-case pool cost; a rejection here would be correct VM behavior
	}
	deployKind := rapid.SampledFrom([]txKind{kindStore, kindRevert}).Draw(rt, "fixture")
	runtime := storeRuntime(c.tb)
	if deployKind == kindRevert {
		runtime = reverterRuntime(c.tb)
	}
	predicted := crypto.CreateAddress(from, c.nextNonce(from))
	data := &types.DynamicFeeTx{
		Gas:       1_000_000,
		GasFeeCap: big.NewInt(txGasFeeCap),
		Data:      deployCode(c.tb, runtime),
	}
	ethTx := c.wallet.SetNonceAndSign(c.tb, fromIdx, data)
	require.NoErrorf(rt, sut.ethclient.SendTransaction(ctx, ethTx), "SendTransaction(deploy)")
	c.trackPending(ethTx, &issuedTx{
		kind:       kindDeploy,
		from:       from,
		deployKind: deployKind,
		contract:   predicted,
		value:      new(uint256.Int),
		cost:       uint256.NewInt(1_000_000 * txGasFeeCap),
	})
}

// deployedContracts returns addresses of model contracts of the given kind.
func (c *modelCore) deployedContracts(kind txKind) []common.Address {
	var out []common.Address
	for addr, cs := range c.m.contracts {
		if cs.kind == kind {
			out = append(out, addr)
		}
	}
	slices.SortFunc(out, func(a, b common.Address) int { return bytes.Compare(a[:], b[:]) }) // deterministic draw order
	return out
}

//nolint:revive // context-as-argument: see issueTransfer
func (c *modelCore) issueStore(rt *rapid.T, ctx context.Context, sut *SUT, fromIdx int) {
	targets := c.deployedContracts(kindStore)
	if len(targets) == 0 {
		c.issueDeploy(rt, ctx, sut, fromIdx)
		return
	}
	from := c.addrs[fromIdx]
	if c.pendingCount(from) >= maxPendingPerAccount {
		return
	}
	if !c.canAfford(from) {
		return
	}
	contract := rapid.SampledFrom(targets).Draw(rt, "contract")
	key := common.BigToHash(new(big.Int).SetUint64(rapid.Uint64Range(0, 15).Draw(rt, "slot")))
	val := common.BigToHash(new(big.Int).SetUint64(rapid.Uint64().Draw(rt, "value")))
	data := &types.DynamicFeeTx{
		To:        &contract,
		Gas:       1_000_000,
		GasFeeCap: big.NewInt(txGasFeeCap),
		Data:      slices.Concat(key[:], val[:]),
	}
	ethTx := c.wallet.SetNonceAndSign(c.tb, fromIdx, data)
	require.NoErrorf(rt, sut.ethclient.SendTransaction(ctx, ethTx), "SendTransaction(store)")
	c.trackPending(ethTx, &issuedTx{
		kind:     kindStore,
		from:     from,
		contract: contract,
		key:      key,
		val:      val,
		value:    new(uint256.Int),
		cost:     uint256.NewInt(1_000_000 * txGasFeeCap),
	})
}

//nolint:revive // context-as-argument: see issueTransfer
func (c *modelCore) issueRevert(rt *rapid.T, ctx context.Context, sut *SUT, fromIdx int) {
	targets := c.deployedContracts(kindRevert)
	if len(targets) == 0 {
		c.issueDeploy(rt, ctx, sut, fromIdx)
		return
	}
	from := c.addrs[fromIdx]
	if c.pendingCount(from) >= maxPendingPerAccount {
		return
	}
	if !c.canAfford(from) {
		return
	}
	contract := rapid.SampledFrom(targets).Draw(rt, "contract")
	arg := rapid.Uint64().Draw(rt, "arg")
	wantStatus := types.ReceiptStatusSuccessful
	if arg%2 == 1 {
		wantStatus = types.ReceiptStatusFailed
	}
	argHash := common.BigToHash(new(big.Int).SetUint64(arg))
	data := &types.DynamicFeeTx{
		To:        &contract,
		Gas:       1_000_000,
		GasFeeCap: big.NewInt(txGasFeeCap),
		Data:      argHash[:],
	}
	ethTx := c.wallet.SetNonceAndSign(c.tb, fromIdx, data)
	require.NoErrorf(rt, sut.ethclient.SendTransaction(ctx, ethTx), "SendTransaction(revert probe)")
	c.trackPending(ethTx, &issuedTx{
		kind:       kindRevert,
		from:       from,
		contract:   contract,
		wantStatus: wantStatus,
		value:      new(uint256.Int),
		cost:       uint256.NewInt(1_000_000 * txGasFeeCap),
	})
}

// issueWarpSend calls the warp precompile's sendWarpMessage, which the
// executed block turns into a signable unsigned warp message (asserted in
// applyTxEffects).
//
//nolint:revive // context-as-argument: see issueTransfer
func (c *modelCore) issueWarpSend(rt *rapid.T, ctx context.Context, sut *SUT, fromIdx int) {
	from := c.addrs[fromIdx]
	if c.pendingCount(from) >= maxPendingPerAccount {
		return
	}
	if !c.canAfford(from) {
		return
	}
	payload := rapid.SliceOfN(rapid.Byte(), 1, 100).Draw(rt, "payload")
	callData, err := corethwarp.PackSendWarpMessage(payload)
	require.NoErrorf(rt, err, "PackSendWarpMessage(%d bytes)", len(payload))
	data := &types.DynamicFeeTx{
		To:        &corethwarp.ContractAddress,
		Gas:       1_000_000,
		GasFeeCap: big.NewInt(txGasFeeCap),
		Data:      callData,
	}
	ethTx := c.wallet.SetNonceAndSign(c.tb, fromIdx, data)
	require.NoErrorf(rt, sut.ethclient.SendTransaction(ctx, ethTx), "SendTransaction(warp send)")
	c.trackPending(ethTx, &issuedTx{
		kind:    kindWarpSend,
		from:    from,
		payload: payload,
		value:   new(uint256.Int),
		cost:    uint256.NewInt(1_000_000 * txGasFeeCap),
	})
}

// issueWarpReceive delivers a warp message (1-in-4 mis-signed) to the model's
// warpLoggerAddr fixture, which forwards to getVerifiedWarpMessage and logs
// the ABI-encoded output for applyTxEffects to compare.
//
//nolint:revive // context-as-argument: see issueTransfer
func (c *modelCore) issueWarpReceive(rt *rapid.T, ctx context.Context, sut *SUT, fromIdx int) {
	from := c.addrs[fromIdx]
	if c.pendingCount(from) >= maxPendingPerAccount {
		return
	}
	if !c.canAfford(from) {
		return
	}
	var (
		sourceAddress = common.Address(rapid.SliceOfN(rapid.Byte(), 20, 20).Draw(rt, "sourceAddress"))
		payload       = rapid.SliceOfN(rapid.Byte(), 1, 100).Draw(rt, "payload")
		unsigned      = sut.newAddressedCallMessage(c.tb, sourceAddress[:], payload)
		valid         = rapid.IntRange(0, 3).Draw(rt, "misSigned") != 0 // 1 in 4 mis-signed
	)
	var msg *avalanchewarp.Message
	if valid {
		msg = c.vdrs.Sign(c.tb, unsigned)
	} else {
		msg = warptest.IncorrectlySign(c.tb, unsigned)
	}

	callData, err := corethwarp.PackGetVerifiedWarpMessage(0)
	require.NoErrorf(rt, err, "PackGetVerifiedWarpMessage(0)")

	want := corethwarp.GetVerifiedWarpMessageOutput{Valid: false}
	if valid {
		want = corethwarp.GetVerifiedWarpMessageOutput{
			Message: corethwarp.WarpMessage{
				SourceChainID:       common.Hash(sut.ctx.ChainID),
				OriginSenderAddress: sourceAddress,
				Payload:             payload,
			},
			Valid: true,
		}
	}
	wantLogData, err := corethwarp.PackGetVerifiedWarpMessageOutput(want)
	require.NoErrorf(rt, err, "PackGetVerifiedWarpMessageOutput()")

	data := &types.DynamicFeeTx{
		To:         &warpLoggerAddr,
		Gas:        1_000_000,
		GasFeeCap:  big.NewInt(txGasFeeCap),
		Data:       callData,
		AccessList: warpAccessList(msg),
	}
	ethTx := c.wallet.SetNonceAndSign(c.tb, fromIdx, data)
	require.NoErrorf(rt, sut.ethclient.SendTransaction(ctx, ethTx), "SendTransaction(warp receive)")
	c.trackPending(ethTx, &issuedTx{
		kind:        kindWarpReceive,
		from:        from,
		wantLogData: wantLogData,
		value:       new(uint256.Int),
		cost:        uint256.NewInt(1_000_000 * txGasFeeCap),
	})
}

// remoteChains are the chains the harness simulates the far side of. Every
// SUT in a run shares the same chain IDs, so any node's ctx works.
func (*modelCore) remoteChains(sut *SUT) []ids.ID {
	return []ids.ID{sut.ctx.XChainID, constants.PlatformChainID}
}

// provisionUTXO writes ONE freshly drawn shared-memory UTXO, as the remote
// chain, into EVERY given SUT's shared memory — each SUT owns an independent
// atomic.Memory, and the remote chain's state is a single global truth the
// harness must mirror into all of them. Recorded once in the shared model.
func (c *modelCore) provisionUTXO(rt *rapid.T, suts ...*SUT) {
	ownerIdx := rapid.IntRange(0, len(c.atomicKeys)-1).Draw(rt, "owner")
	chain := rapid.SampledFrom(c.remoteChains(suts[0])).Draw(rt, "remoteChain")
	amount := rapid.Uint64Range(2, 1_000_000_000).Draw(rt, "amountNAVAX")
	utxo := txtest.NewUTXO(amount, suts[0].ctx.AVAXAssetID, c.atomicKeys[ownerIdx].Address())
	for _, s := range suts {
		s.addUTXOs(c.tb, s.ctx.ChainID, chain, utxo)
	}
	c.m.availableUTXOs[chain] = append(c.m.availableUTXOs[chain], &provisionedUTXO{
		utxo: utxo, ownerIdx: ownerIdx, amount: amount,
	})
}

// hasPendingImport reports whether ownerIdx already has an unconfirmed
// import in flight against chain. newImportTx consumes whatever the shared
// memory backend currently reports as spendable for the owner on that
// chain — a live RPC read with no notion of "reserved by a pending tx" — so
// issuing a second import for the same (owner, chain) before the first is
// executed would silently sweep up the first import's UTXOs too, crediting
// more than either issuance recorded. One in-flight import per (owner,
// chain) avoids that race; see the report for detail.
func (m *model) hasPendingImport(ownerIdx int, chain ids.ID) bool {
	for _, exp := range m.pendingAtomic {
		if exp.isImport && exp.senderIdx == ownerIdx && exp.remoteChain == chain {
			return true
		}
	}
	return false
}

// issueImport picks a (remoteChain, owner) pair with available UTXOs and
// issues an import consuming ALL of that owner's spendable UTXOs from that
// chain, crediting a random eth account. all is the provisioning fan-out used
// when there are no candidates: provisionUTXO writes the fresh UTXO into
// every SUT in all, not just sut.
//
//nolint:revive // context-as-argument: see issueTransfer
func (c *modelCore) issueImport(rt *rapid.T, ctx context.Context, sut *SUT, all []*SUT, walletFor func(int) *wallet) {
	// Pick a (chain, owner) pair with available UTXOs; newImportTx consumes
	// ALL of the owner's spendable UTXOs from that chain. Skip (owner, chain)
	// pairs with an import already in flight (see hasPendingImport).
	type candidate struct {
		chain    ids.ID
		ownerIdx int
		total    uint64
		utxos    []*provisionedUTXO
	}
	var cands []candidate
	for _, chain := range c.remoteChains(sut) {
		perOwner := map[int]*candidate{}
		for _, p := range c.m.availableUTXOs[chain] {
			if c.m.hasPendingImport(p.ownerIdx, chain) {
				continue
			}
			c2, ok := perOwner[p.ownerIdx]
			if !ok {
				c2 = &candidate{chain: chain, ownerIdx: p.ownerIdx}
				perOwner[p.ownerIdx] = c2
			}
			c2.total += p.amount
			c2.utxos = append(c2.utxos, p)
		}
		for _, idx := range slices.Sorted(maps.Keys(perOwner)) {
			cands = append(cands, *perOwner[idx])
		}
	}
	if len(cands) == 0 {
		c.provisionUTXO(rt, all...)
		return
	}
	c2 := cands[rapid.IntRange(0, len(cands)-1).Draw(rt, "candidate")]
	// fee must be >= 1: the atomic op's worst-case GasFeeCap is derived from
	// the fee alone (gasPrice(fee, gas)), and the block base fee has a floor
	// of 1 wei (dynamic.PriceExponent.Price()) that it can never go below, so
	// a zero fee produces a GasFeeCap of 0 that can never clear the base fee
	// — the op would sit in the pool forever, wedging the builder.
	fee := rapid.Uint64Range(1, c2.total-1).Draw(rt, "fee")
	toIdx := rapid.IntRange(0, len(c.addrs)-1).Draw(rt, "to")
	to := c.addrs[toIdx]

	signed := walletFor(c2.ownerIdx).newImportTx(ctx, c.tb, c2.chain, to, fee)
	require.NoErrorf(rt, sut.IssueTx(ctx, signed), "%T.IssueTx(import)", sut.Client)
	c.pendingAtomicTxs = append(c.pendingAtomicTxs, signed)

	credit := tx.ScaleAVAX(c2.total - fee)
	c.m.pendingAtomic[signed.ID()] = &atomicExpectation{
		isImport:    true,
		senderIdx:   c2.ownerIdx,
		to:          to,
		creditWei:   &credit,
		remoteChain: c2.chain,
		consumed:    c2.utxos,
	}
	// Remove the consumed UTXOs from availability immediately: the pool
	// reserves them.
	kept := c.m.availableUTXOs[c2.chain][:0]
	for _, p := range c.m.availableUTXOs[c2.chain] {
		if p.ownerIdx != c2.ownerIdx {
			kept = append(kept, p)
		}
	}
	c.m.availableUTXOs[c2.chain] = kept
}

// pendingAtomicCount counts in-flight cross-chain txs for one atomic key.
func (m *model) pendingAtomicCount(senderIdx int) int {
	n := 0
	for _, exp := range m.pendingAtomic {
		if exp.senderIdx == senderIdx {
			n++
		}
	}
	return n
}

// unscaleAVAXTruncateCapped converts an aAVAX amount back into nAVAX,
// rounding down and clamping to math.MaxUint64. tx.UnscaleAVAXTruncate does
// not exist, so the scale is computed once via tx.ScaleAVAX(1) and divided
// out with uint256.Int.Div.
func unscaleAVAXTruncateCapped(wei *uint256.Int) uint64 {
	scale := tx.ScaleAVAX(1)
	q := new(uint256.Int).Div(wei, &scale)
	if q.IsUint64() {
		return q.Uint64()
	}
	return math.MaxUint64
}

// issueExport issues an export of at most a quarter of the sender's
// spendable nAVAX, provided the sender has no other in-flight atomic tx (one
// in-flight atomic tx per key keeps nonces simple).
//
//nolint:revive // context-as-argument: see issueTransfer
func (c *modelCore) issueExport(rt *rapid.T, ctx context.Context, sut *SUT, walletFor func(int) *wallet) {
	senderIdx := rapid.IntRange(0, len(c.atomicKeys)-1).Draw(rt, "sender")
	sender := c.atomicAddrs[senderIdx]
	if c.m.pendingAtomicCount(senderIdx) > 0 {
		return // one in-flight atomic tx per key keeps nonces simple
	}
	chain := rapid.SampledFrom(c.remoteChains(sut)).Draw(rt, "remoteChain")

	// Export at most a quarter of the sender's spendable nAVAX.
	spendableNAVAX := unscaleAVAXTruncateCapped(c.spendable(sender))
	if spendableNAVAX < 8 {
		return
	}
	amount := rapid.Uint64Range(1, spendableNAVAX/4).Draw(rt, "amountNAVAX")
	// fee must be >= 1, for the same reason as issueImport's fee: a zero fee
	// yields a worst-case GasFeeCap of 0, which can never clear the block's
	// 1-wei price floor and would wedge the builder forever.
	fee := rapid.Uint64Range(1, 1_000).Draw(rt, "fee")

	signed, export := walletFor(senderIdx).newExportTx(
		c.tb, chain, fee,
		txtest.NewTransferOutput(amount, c.atomicKeys[senderIdx].Address()),
	)
	require.NoErrorf(rt, sut.IssueTx(ctx, signed), "%T.IssueTx(export)", sut.Client)
	c.pendingAtomicTxs = append(c.pendingAtomicTxs, signed)

	debit := tx.ScaleAVAX(amount + fee)
	c.m.pendingAtomic[signed.ID()] = &atomicExpectation{
		senderIdx:   senderIdx,
		debitWei:    &debit,
		remoteChain: chain,
		exported:    txtest.ExportedUTXOs(signed.ID(), export),
	}
}

// restart shuts the VM down and reopens it on the same persisted state, as a
// node restart would. The model is NOT reset: everything it predicts must
// still hold. In-memory pool contents are expected to drop, so issued-but-
// unaccepted txs return to "unissued" (nonces rewound).
func (mm *modelMachine) restart(rt *rapid.T) {
	require.NoErrorf(rt, mm.sut.Shutdown(mm.ctx), "%T.Shutdown()", mm.sut.VM)

	if mm.cfg.kv == kvLevelDB {
		// The true production restart: close and reopen the store.
		require.NoErrorf(rt, mm.db.Close(), "leveldb Close() on restart")
		db, err := leveldb.New(mm.dbDir, nil, logging.NoLog{}, prometheus.NewRegistry())
		require.NoErrorf(rt, err, "leveldb.New(%q) on restart", mm.dbDir)
		mm.db = db
	}

	// Pool contents are gone: rewind wallet nonces for dropped eth txs and
	// forget them; forget dropped atomic txs likewise.
	for h, it := range mm.m.pendingEth {
		for i, addr := range mm.addrs {
			if addr == it.from {
				mm.wallet.DecrementNonce(mm.tb, i)
			}
		}
		mm.m.pendingCost[it.from].Sub(mm.m.pendingCost[it.from], it.cost)
		delete(mm.m.pendingEth, h)
	}
	for id, exp := range mm.m.pendingAtomic {
		if exp.isImport {
			// The reserved UTXOs become available again.
			mm.m.availableUTXOs[exp.remoteChain] = append(mm.m.availableUTXOs[exp.remoteChain], exp.consumed...)
		}
		delete(mm.m.pendingAtomic, id)
	}
	mm.pendingEthTxs = nil
	mm.pendingAtomicTxs = nil

	mm.openSUT() // atomic wallet nonces are restored from the model here
}

func (c *modelCore) trackPending(ethTx *types.Transaction, it *issuedTx) {
	c.m.pendingEth[ethTx.Hash()] = it
	c.m.pendingCost[it.from].Add(c.m.pendingCost[it.from], it.cost)
	c.pendingEthTxs = append(c.pendingEthTxs, ethTx)
}

// spendable is the model's view of what addr can still commit to new txs.
func (c *modelCore) spendable(addr common.Address) *uint256.Int {
	s := new(uint256.Int).Set(c.m.balances[addr])
	if pc := c.m.pendingCost[addr]; pc != nil && s.Cmp(pc) >= 0 {
		s.Sub(s, pc)
	} else if pc != nil {
		s.Clear()
	}
	return s
}

func (mm *modelMachine) buildBlock(rt *rapid.T) {
	if len(mm.m.pendingEth) == 0 && len(mm.m.pendingAtomic) == 0 {
		mm.issueMinimalTransfer(rt, mm.ctx, mm.sut)
	}
	blk := mm.buildVerifyAcceptExecute(rt)
	mm.applyBlock(rt, blk)
}

// buildVerifyAcceptExecute drives one full consensus round and waits for
// execution, so LastExecutedState reflects the new block for the model
// comparison. BuildBlock retries a bounded number of times, recovering from
// errEmptyBlock (settle the last accepted block) and sae.ErrExecutionLagging
// (wait for execution to catch up); any other error is immediately fatal.
func (mm *modelMachine) buildVerifyAcceptExecute(rt *rapid.T) *blocks.Block {
	blockCtx := &block.Context{}
	lastAccepted, err := mm.sut.LastAccepted(mm.ctx)
	require.NoErrorf(rt, err, "%T.LastAccepted()", mm.sut.VM)
	require.NoErrorf(rt, mm.sut.SetPreference(mm.ctx, lastAccepted, blockCtx), "%T.SetPreference()", mm.sut.VM)

	mm.advanceToBuildable()
	mm.sut.waitForPendingEthTxs(mm.ctx, mm.tb, mm.pendingEthTxs...)
	mm.sut.waitForPendingTxs(mm.ctx, mm.tb)

	const maxBuildAttempts = 5
	var blk *blocks.Block
	for attempt := 1; ; attempt++ {
		var err error
		blk, err = mm.sut.BuildBlock(mm.ctx, blockCtx)
		if err == nil {
			break
		}
		require.Lessf(rt, attempt, maxBuildAttempts, "BuildBlock never recovered after %d attempts: %v", attempt, err)
		switch {
		case errors.Is(err, errEmptyBlock):
			// Worst-case block building validates spendability against the
			// last-SETTLED state (ACP-194), so txs spending executed-but-
			// unsettled credits are admitted to the pool yet unincludable,
			// and the hook refuses an empty block. Settling the last
			// accepted block makes those credits spendable; bounded retries
			// keep a genuinely wedged builder fatal.
			require.NotNilf(rt, mm.m.lastAccepted, "%T.BuildBlock() returned errEmptyBlock with nothing accepted to settle", mm.sut.VM)
			mm.settle(rt)
		case errors.Is(err, sae.ErrExecutionLagging):
			// A wall-clock jump can leave the executor's gas-time behind the
			// settlement target implied by the requested block time (the
			// "GC stall" scenario): let execution catch up.
			if mm.m.lastAccepted != nil {
				require.NoErrorf(rt, mm.m.lastAccepted.WaitUntilExecuted(mm.ctx), "%T.WaitUntilExecuted() during lag recovery", mm.m.lastAccepted)
			}
		default:
			require.NoErrorf(rt, err, "%T.BuildBlock() attempt %d: want errors.Is(err, errEmptyBlock) or errors.Is(err, sae.ErrExecutionLagging)", mm.sut.VM, attempt)
		}
		mm.advanceToBuildable()
	}
	require.NoErrorf(rt, mm.sut.VerifyBlock(mm.ctx, blockCtx, blk), "%T.VerifyBlock()", mm.sut.VM)
	require.NoErrorf(rt, mm.sut.AcceptBlock(mm.ctx, blk), "%T.AcceptBlock()", mm.sut.VM)
	require.NoErrorf(rt, blk.WaitUntilExecuted(mm.ctx), "%T.WaitUntilExecuted()", blk)
	return blk
}

// applyAtomicBlockEffects advances the model by blk's atomic (cross-chain)
// txs, waiting for sut's pool to evict each included tx and asserting the
// import-consumed UTXOs are gone from sut's shared memory.
//
//nolint:revive // context-as-argument: see issueTransfer
func (c *modelCore) applyAtomicBlockEffects(rt *rapid.T, ctx context.Context, sut *SUT, blk *blocks.Block) {
	m := c.m
	atxs := blockTxs(c.tb, blk)
	for _, atx := range atxs {
		exp, ok := m.pendingAtomic[atx.ID()]
		require.Truef(rt, ok, "block %d contains unexpected atomic tx %s", blk.NumberU64(), atx.ID())
		delete(m.pendingAtomic, atx.ID())
		sut.waitForTxPoolStateUpdate(ctx, c.tb, atx)

		sender := c.atomicAddrs[exp.senderIdx]
		if exp.isImport {
			m.balances[exp.to].Add(m.balances[exp.to], exp.creditWei)
			consumed := utxosOf(exp.consumed)
			m.consumedUTXOs[exp.remoteChain] = append(m.consumedUTXOs[exp.remoteChain], consumed...)
			sut.assertUTXOsMissing(c.tb, sut.ctx.ChainID, exp.remoteChain, consumed...)
		} else {
			m.balances[sender].Sub(m.balances[sender], exp.debitWei)
			m.nonces[sender]++
			m.exportedUTXOs[exp.remoteChain] = append(m.exportedUTXOs[exp.remoteChain], exp.exported...)
		}
	}

	// Drop included txs from the machine's wait-list, mirroring pendingEthTxs.
	included := make(map[ids.ID]bool, len(atxs))
	for _, atx := range atxs {
		included[atx.ID()] = true
	}
	kept := c.pendingAtomicTxs[:0]
	for _, t := range c.pendingAtomicTxs {
		if !included[t.ID()] {
			kept = append(kept, t)
		}
	}
	c.pendingAtomicTxs = kept
}

// applyBlock advances the model by one accepted block: the shared eth-side
// core, plus the shared atomic (cross-chain) reconciliation.
func (mm *modelMachine) applyBlock(rt *rapid.T, blk *blocks.Block) {
	mm.modelCore.applyBlock(rt, mm.ctx, mm.sut, blk)
	mm.modelCore.applyAtomicBlockEffects(rt, mm.ctx, mm.sut, blk)
}

// applyBlock advances the model by one accepted block: dynamic-parameter
// ramps, height, and per-tx effects reconciled against receipts.
//
//nolint:revive // context-as-argument: see modelCore.issueTransfer
func (c *modelCore) applyBlock(rt *rapid.T, ctx context.Context, sut *SUT, blk *blocks.Block) {
	m := c.m

	// Dynamic-parameter ramp: an independent recomputation of Toward.
	m.target = m.target.Toward(m.desiredTarget)
	m.price = m.price.Toward(m.desiredPrice)
	m.delay = m.delay.Toward(m.desiredDelay)
	he := customtypes.GetHeaderExtra(blk.Header())
	require.NotNilf(rt, he.TargetExponent, "block %d TargetExponent extra", blk.NumberU64())
	require.Equalf(rt, m.target, *he.TargetExponent, "block %d TargetExponent ramp", blk.NumberU64())
	require.NotNilf(rt, he.MinPriceExponent, "block %d MinPriceExponent extra", blk.NumberU64())
	require.Equalf(rt, m.price, *he.MinPriceExponent, "block %d MinPriceExponent ramp", blk.NumberU64())
	require.NotNilf(rt, he.MinDelayExcess, "block %d MinDelayExcess extra", blk.NumberU64())
	require.Equalf(rt, m.delay, dynamic.DelayExponent(*he.MinDelayExcess), "block %d MinDelayExcess ramp", blk.NumberU64())

	// Heights.
	require.Equalf(rt, m.lastAcceptedHeight+1, blk.NumberU64(), "accepted block height")
	m.lastAccepted = blk
	m.lastAcceptedID = blk.ID()
	m.lastAcceptedHeight = blk.NumberU64()

	// Gas-time monotonicity (compare via AsTime — stable across the
	// gastime/proxytime API surface).
	gt := blk.ExecutedByGasTime()
	if m.lastGasTime != nil {
		require.Falsef(rt, gt.AsTime().Before(m.lastGasTime.AsTime()), "ExecutedByGasTime went backward at block %d", blk.NumberU64())
	}
	m.lastGasTime = gt

	// Per-tx effects, driven by what the block actually included.
	receipts := make(map[common.Hash]*types.Receipt, len(blk.Receipts()))
	for _, r := range blk.Receipts() {
		receipts[r.TxHash] = r
	}
	for _, ethTx := range blk.Transactions() {
		it, ok := m.pendingEth[ethTx.Hash()]
		require.Truef(rt, ok, "block %d contains unexpected tx %s", blk.NumberU64(), ethTx.Hash())
		delete(m.pendingEth, ethTx.Hash())
		m.pendingCost[it.from].Sub(m.pendingCost[it.from], it.cost)

		r, ok := receipts[ethTx.Hash()]
		require.Truef(rt, ok, "missing receipt for tx %s", ethTx.Hash())

		// Gas reconciliation: deduct actual gas charged.
		price, overflow := uint256.FromBig(r.EffectiveGasPrice)
		require.Falsef(rt, overflow, "EffectiveGasPrice overflows uint256")
		fee := new(uint256.Int).Mul(uint256.NewInt(r.GasUsed), price)
		m.balances[it.from].Sub(m.balances[it.from], fee)
		m.nonces[it.from]++

		c.applyTxEffects(rt, ctx, sut, it, r)
	}

	// Drop pending txs from the machine's wait-list once included.
	included := make(map[common.Hash]bool, len(blk.Transactions()))
	for _, ethTx := range blk.Transactions() {
		included[ethTx.Hash()] = true
	}
	kept := c.pendingEthTxs[:0]
	for _, ethTx := range c.pendingEthTxs {
		if !included[ethTx.Hash()] {
			kept = append(kept, ethTx)
		}
	}
	c.pendingEthTxs = kept
}

// applyTxEffects applies kind-specific model updates for an included tx.
//
//nolint:revive // context-as-argument: see modelCore.issueTransfer
func (c *modelCore) applyTxEffects(rt *rapid.T, ctx context.Context, sut *SUT, it *issuedTx, r *types.Receipt) {
	switch it.kind {
	case kindTransfer:
		require.Equalf(rt, types.ReceiptStatusSuccessful, r.Status, "transfer receipt status")
		c.m.balances[it.from].Sub(c.m.balances[it.from], it.value)
		c.m.balances[it.to].Add(c.m.balances[it.to], it.value)
	case kindDeploy:
		require.Equalf(rt, types.ReceiptStatusSuccessful, r.Status, "deploy receipt status")
		require.Equalf(rt, it.contract, r.ContractAddress, "CREATE address prediction")
		c.m.contracts[it.contract] = &contractState{kind: it.deployKind, storage: make(map[common.Hash]common.Hash)}
	case kindStore:
		require.Equalf(rt, types.ReceiptStatusSuccessful, r.Status, "store receipt status")
		require.Lenf(rt, r.Logs, 1, "store tx log count")
		require.Equalf(rt, []common.Hash{it.val}, r.Logs[0].Topics, "store log topic")
		require.Equalf(rt, it.contract, r.Logs[0].Address, "store log address")
		c.m.contracts[it.contract].storage[it.key] = it.val
	case kindRevert:
		require.Equalf(rt, it.wantStatus, r.Status, "reverter receipt status")
	case kindWarpSend:
		require.Equalf(rt, types.ReceiptStatusSuccessful, r.Status, "warp send receipt status")
		// The message must now be signable: the block is accepted and executed.
		sent := sut.newAddressedCallMessage(c.tb, it.from.Bytes(), it.payload)
		sut.signAndVerifyWarpMessage(ctx, c.tb, sent)
	case kindWarpReceive:
		require.Equalf(rt, types.ReceiptStatusSuccessful, r.Status, "warp receive receipt status")
		require.Lenf(rt, r.Logs, 1, "warp receive log count")
		require.Equalf(rt, it.wantLogData, r.Logs[0].Data, "warp precompile output log data")
	}
}

func (mm *modelMachine) advanceClock(rt *rapid.T) {
	var d time.Duration
	if rapid.IntRange(0, 9).Draw(rt, "isStall") == 0 {
		// Rare multi-Tau jump: the "GC stall" / slow-processing scenario.
		d = time.Duration(rapid.Int64Range(int64(saeparams.Tau), int64(10*saeparams.Tau)).Draw(rt, "stall"))
	} else {
		d = time.Duration(rapid.Int64Range(int64(time.Millisecond), int64(2*time.Second)).Draw(rt, "tick"))
	}
	mm.clock.Advance(d)
}

func (mm *modelMachine) settle(_ *rapid.T) {
	if mm.m.lastAccepted == nil {
		return
	}
	mm.clock.AdvanceToSettle(mm.ctx, mm.tb, mm.m.lastAccepted)
}

// checkState compares the model's predicted chain state against one SUT.
//
//nolint:revive // context-as-argument: see modelCore.issueTransfer
func (c *modelCore) checkState(rt *rapid.T, ctx context.Context, sut *SUT, db database.Database) {
	got, err := sut.LastAccepted(ctx)
	require.NoErrorf(rt, err, "%T.LastAccepted()", sut.VM)
	require.Equalf(rt, c.m.lastAcceptedID, got, "last accepted ID")

	state, err := sut.LastExecutedState()
	require.NoErrorf(rt, err, "%T.LastExecutedState()", sut.VM)
	for addr, want := range c.m.balances {
		require.Equalf(rt, *want, *state.GetBalance(addr), "balance of %s", addr)
		require.Equalf(rt, c.m.nonces[addr], state.GetNonce(addr), "nonce of %s", addr)
	}
	for contract, cs := range c.m.contracts {
		for key, want := range cs.storage {
			require.Equalf(rt, want, state.GetState(contract, key), "storage %s[%s]", contract, key)
		}
	}
	checkRawdbPointers(rt, db)
}

// checkSharedMemory asserts sut's shared memory agrees with the model:
// exported UTXOs are readable by the remote chain, consumed UTXOs are gone.
// Reads shared memory directly, so it also covers restarts.
func (c *modelCore) checkSharedMemory(sut *SUT) {
	for _, chain := range c.remoteChains(sut) {
		if exported := c.m.exportedUTXOs[chain]; len(exported) > 0 {
			sut.assertUTXOsExist(c.tb, chain, sut.ctx.ChainID, exported...)
		}
		if consumed := c.m.consumedUTXOs[chain]; len(consumed) > 0 {
			sut.assertUTXOsMissing(c.tb, sut.ctx.ChainID, chain, consumed...)
		}
	}
}

// check is the rapid invariant action, run around every other action.
func (mm *modelMachine) check(rt *rapid.T) {
	mm.checkState(rt, mm.ctx, mm.sut, mm.db)
	mm.checkSharedMemory(mm.sut)
}

// checkRawdbPointers spot-checks invariants.md pointer discipline on the
// persisted chain: Finalized (settled) ≤ Head (executed) ≤ HeadFast
// (accepted) along the canonical chain.
func checkRawdbPointers(rt *rapid.T, db database.Database) {
	ethDB := rawdb.NewDatabase(evmdatabase.New(prefixdb.NewNested(ethDBPrefix, prefixdb.New([]byte("chain"), db))))

	heightOf := func(name string, hash common.Hash) uint64 {
		if hash == (common.Hash{}) {
			return 0 // pointer not written yet (genesis-only chain)
		}
		n := rawdb.ReadHeaderNumber(ethDB, hash)
		require.NotNilf(rt, n, "rawdb header number for %s pointer %s", name, hash)
		return *n
	}
	finalized := heightOf("finalized", rawdb.ReadFinalizedBlockHash(ethDB))
	head := heightOf("head", rawdb.ReadHeadBlockHash(ethDB))
	headFast := heightOf("head-fast", rawdb.ReadHeadFastBlockHash(ethDB))
	require.LessOrEqualf(rt, finalized, head, "rawdb Finalized (settled) vs Head (executed)")
	require.LessOrEqualf(rt, head, headFast, "rawdb Head (executed) vs HeadFast (accepted)")
}
