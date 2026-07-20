// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"bytes"
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
	return map[string]func(*rapid.T){
		"buildBlock":       mm.buildBlock,
		"advanceClock":     mm.advanceClock,
		"settle":           mm.settle,
		"issueTransfer":    mm.issueTransfer,
		"issueDeploy":      mm.issueDeploy,
		"issueStore":       mm.issueStore,
		"issueRevert":      mm.issueRevert,
		"issueWarpSend":    mm.issueWarpSend,
		"issueWarpReceive": mm.issueWarpReceive,
		"provisionUTXO":    mm.provisionUTXO,
		"issueImport":      mm.issueImport,
		"issueExport":      mm.issueExport,
		"restart":          mm.restart,
		"":                 mm.check,
	}
}

const maxPendingPerAccount = 8 // stay well under the pool's 16 account slots

func (mm *modelMachine) pendingCount(addr common.Address) int {
	n := 0
	for _, it := range mm.m.pendingEth {
		if it.from == addr {
			n++
		}
	}
	return n
}

// issueTransfer sends a randomized value transfer between two model accounts,
// occasionally (1-in-10) drawing a value guaranteed to exceed the sender's
// spendable balance to exercise the pool's insufficient-funds rejection path.
func (mm *modelMachine) issueTransfer(rt *rapid.T) {
	fromIdx := rapid.IntRange(0, len(mm.addrs)-1).Draw(rt, "from")
	toIdx := rapid.IntRange(0, len(mm.addrs)-1).Draw(rt, "to")
	from, to := mm.addrs[fromIdx], mm.addrs[toIdx]
	if mm.pendingCount(from) >= maxPendingPerAccount {
		return
	}

	spendable := mm.spendable(from)
	insufficient := rapid.IntRange(0, 9).Draw(rt, "insufficient") == 0

	var value *uint256.Int
	if insufficient {
		// 2*balance + 1 ETH guarantees pool-admission rejection regardless of
		// concurrently pending txs.
		value = new(uint256.Int).Add(
			new(uint256.Int).Lsh(mm.m.balances[from], 1),
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
	ethTx := mm.wallet.SetNonceAndSign(mm.tb, fromIdx, data)
	err := mm.sut.ethclient.SendTransaction(mm.ctx, ethTx)

	if insufficient {
		if diff := testerr.Diff(err, testerr.Contains(core.ErrInsufficientFunds.Error())); diff != "" {
			rt.Fatalf("SendTransaction(overdrawn transfer) error (-want +got):\n%s", diff)
		}
		mm.wallet.DecrementNonce(mm.tb, fromIdx) // reuse the burned nonce
		return
	}
	require.NoErrorf(rt, err, "SendTransaction(transfer of %s)", value)
	mm.trackPending(ethTx, &issuedTx{
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
// empty blocks): a zero-value self-transfer from the richest account.
func (mm *modelMachine) issueMinimalTransfer(rt *rapid.T) {
	richest := mm.addrs[0]
	richestIdx := 0
	for i, addr := range mm.addrs {
		if mm.m.balances[addr].Cmp(mm.m.balances[richest]) > 0 {
			richest, richestIdx = addr, i
		}
	}
	data := &types.DynamicFeeTx{
		To:        &richest,
		Gas:       ethparams.TxGas,
		GasFeeCap: big.NewInt(txGasFeeCap),
	}
	ethTx := mm.wallet.SetNonceAndSign(mm.tb, richestIdx, data)
	require.NoErrorf(rt, mm.sut.ethclient.SendTransaction(mm.ctx, ethTx), "SendTransaction(minimal transfer)")
	mm.trackPending(ethTx, &issuedTx{
		kind:  kindTransfer,
		from:  richest,
		to:    richest,
		value: new(uint256.Int),
		cost:  uint256.NewInt(ethparams.TxGas * txGasFeeCap),
	})
}

// nextNonce is the account's next nonce: executed nonce + in-flight txs.
func (mm *modelMachine) nextNonce(addr common.Address) uint64 {
	n := mm.m.nonces[addr]
	for _, it := range mm.m.pendingEth {
		if it.from == addr {
			n++
		}
	}
	return n
}

func (mm *modelMachine) issueDeploy(rt *rapid.T) {
	fromIdx := rapid.IntRange(0, len(mm.addrs)-1).Draw(rt, "from")
	from := mm.addrs[fromIdx]
	if mm.pendingCount(from) >= maxPendingPerAccount {
		return
	}
	deployKind := rapid.SampledFrom([]txKind{kindStore, kindRevert}).Draw(rt, "fixture")
	runtime := storeRuntime(mm.tb)
	if deployKind == kindRevert {
		runtime = reverterRuntime(mm.tb)
	}
	predicted := crypto.CreateAddress(from, mm.nextNonce(from))
	data := &types.DynamicFeeTx{
		Gas:       1_000_000,
		GasFeeCap: big.NewInt(txGasFeeCap),
		Data:      deployCode(mm.tb, runtime),
	}
	ethTx := mm.wallet.SetNonceAndSign(mm.tb, fromIdx, data)
	require.NoErrorf(rt, mm.sut.ethclient.SendTransaction(mm.ctx, ethTx), "SendTransaction(deploy)")
	mm.trackPending(ethTx, &issuedTx{
		kind:       kindDeploy,
		from:       from,
		deployKind: deployKind,
		contract:   predicted,
		value:      new(uint256.Int),
		cost:       uint256.NewInt(1_000_000 * txGasFeeCap),
	})
}

// deployedContracts returns addresses of model contracts of the given kind.
func (mm *modelMachine) deployedContracts(kind txKind) []common.Address {
	var out []common.Address
	for addr, cs := range mm.m.contracts {
		if cs.kind == kind {
			out = append(out, addr)
		}
	}
	slices.SortFunc(out, func(a, b common.Address) int { return bytes.Compare(a[:], b[:]) }) // deterministic draw order
	return out
}

func (mm *modelMachine) issueStore(rt *rapid.T) {
	targets := mm.deployedContracts(kindStore)
	if len(targets) == 0 {
		mm.issueDeploy(rt)
		return
	}
	fromIdx := rapid.IntRange(0, len(mm.addrs)-1).Draw(rt, "from")
	from := mm.addrs[fromIdx]
	if mm.pendingCount(from) >= maxPendingPerAccount {
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
	ethTx := mm.wallet.SetNonceAndSign(mm.tb, fromIdx, data)
	require.NoErrorf(rt, mm.sut.ethclient.SendTransaction(mm.ctx, ethTx), "SendTransaction(store)")
	mm.trackPending(ethTx, &issuedTx{
		kind:     kindStore,
		from:     from,
		contract: contract,
		key:      key,
		val:      val,
		value:    new(uint256.Int),
		cost:     uint256.NewInt(1_000_000 * txGasFeeCap),
	})
}

func (mm *modelMachine) issueRevert(rt *rapid.T) {
	targets := mm.deployedContracts(kindRevert)
	if len(targets) == 0 {
		mm.issueDeploy(rt)
		return
	}
	fromIdx := rapid.IntRange(0, len(mm.addrs)-1).Draw(rt, "from")
	from := mm.addrs[fromIdx]
	if mm.pendingCount(from) >= maxPendingPerAccount {
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
	ethTx := mm.wallet.SetNonceAndSign(mm.tb, fromIdx, data)
	require.NoErrorf(rt, mm.sut.ethclient.SendTransaction(mm.ctx, ethTx), "SendTransaction(revert probe)")
	mm.trackPending(ethTx, &issuedTx{
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
func (mm *modelMachine) issueWarpSend(rt *rapid.T) {
	fromIdx := rapid.IntRange(0, len(mm.addrs)-1).Draw(rt, "from")
	from := mm.addrs[fromIdx]
	if mm.pendingCount(from) >= maxPendingPerAccount {
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
	ethTx := mm.wallet.SetNonceAndSign(mm.tb, fromIdx, data)
	require.NoErrorf(rt, mm.sut.ethclient.SendTransaction(mm.ctx, ethTx), "SendTransaction(warp send)")
	mm.trackPending(ethTx, &issuedTx{
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
func (mm *modelMachine) issueWarpReceive(rt *rapid.T) {
	fromIdx := rapid.IntRange(0, len(mm.addrs)-1).Draw(rt, "from")
	from := mm.addrs[fromIdx]
	if mm.pendingCount(from) >= maxPendingPerAccount {
		return
	}
	var (
		sourceAddress = common.Address(rapid.SliceOfN(rapid.Byte(), 20, 20).Draw(rt, "sourceAddress"))
		payload       = rapid.SliceOfN(rapid.Byte(), 1, 100).Draw(rt, "payload")
		unsigned      = mm.sut.newAddressedCallMessage(mm.tb, sourceAddress[:], payload)
		valid         = rapid.IntRange(0, 3).Draw(rt, "misSigned") != 0 // 1 in 4 mis-signed
	)
	var msg *avalanchewarp.Message
	if valid {
		msg = mm.vdrs.Sign(mm.tb, unsigned)
	} else {
		msg = warptest.IncorrectlySign(mm.tb, unsigned)
	}

	callData, err := corethwarp.PackGetVerifiedWarpMessage(0)
	require.NoErrorf(rt, err, "PackGetVerifiedWarpMessage(0)")

	want := corethwarp.GetVerifiedWarpMessageOutput{Valid: false}
	if valid {
		want = corethwarp.GetVerifiedWarpMessageOutput{
			Message: corethwarp.WarpMessage{
				SourceChainID:       common.Hash(mm.sut.ctx.ChainID),
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
	ethTx := mm.wallet.SetNonceAndSign(mm.tb, fromIdx, data)
	require.NoErrorf(rt, mm.sut.ethclient.SendTransaction(mm.ctx, ethTx), "SendTransaction(warp receive)")
	mm.trackPending(ethTx, &issuedTx{
		kind:        kindWarpReceive,
		from:        from,
		wantLogData: wantLogData,
		value:       new(uint256.Int),
		cost:        uint256.NewInt(1_000_000 * txGasFeeCap),
	})
}

// remoteChains are the chains the harness simulates the far side of.
func (mm *modelMachine) remoteChains() []ids.ID {
	return []ids.ID{mm.sut.ctx.XChainID, constants.PlatformChainID}
}

// provisionUTXO writes a fresh shared-memory UTXO as the remote chain,
// spendable by a randomly chosen atomic key via a future import.
func (mm *modelMachine) provisionUTXO(rt *rapid.T) {
	ownerIdx := rapid.IntRange(0, len(mm.atomicKeys)-1).Draw(rt, "owner")
	chain := rapid.SampledFrom(mm.remoteChains()).Draw(rt, "remoteChain")
	amount := rapid.Uint64Range(2, 1_000_000_000).Draw(rt, "amountNAVAX")
	utxo := txtest.NewUTXO(amount, mm.sut.ctx.AVAXAssetID, mm.atomicKeys[ownerIdx].Address())
	mm.sut.addUTXOs(mm.tb, mm.sut.ctx.ChainID, chain, utxo)
	mm.m.availableUTXOs[chain] = append(mm.m.availableUTXOs[chain], &provisionedUTXO{
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
// chain, crediting a random eth account.
func (mm *modelMachine) issueImport(rt *rapid.T) {
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
	for _, chain := range mm.remoteChains() {
		perOwner := map[int]*candidate{}
		for _, p := range mm.m.availableUTXOs[chain] {
			if mm.m.hasPendingImport(p.ownerIdx, chain) {
				continue
			}
			c, ok := perOwner[p.ownerIdx]
			if !ok {
				c = &candidate{chain: chain, ownerIdx: p.ownerIdx}
				perOwner[p.ownerIdx] = c
			}
			c.total += p.amount
			c.utxos = append(c.utxos, p)
		}
		for _, idx := range slices.Sorted(maps.Keys(perOwner)) {
			cands = append(cands, *perOwner[idx])
		}
	}
	if len(cands) == 0 {
		mm.provisionUTXO(rt)
		return
	}
	c := cands[rapid.IntRange(0, len(cands)-1).Draw(rt, "candidate")]
	// fee must be >= 1: the atomic op's worst-case GasFeeCap is derived from
	// the fee alone (gasPrice(fee, gas)), and the block base fee has a floor
	// of 1 wei (dynamic.PriceExponent.Price()) that it can never go below, so
	// a zero fee produces a GasFeeCap of 0 that can never clear the base fee
	// — the op would sit in the pool forever, wedging the builder.
	fee := rapid.Uint64Range(1, c.total-1).Draw(rt, "fee")
	toIdx := rapid.IntRange(0, len(mm.addrs)-1).Draw(rt, "to")
	to := mm.addrs[toIdx]

	signed, _ := mm.atomicWallets[c.ownerIdx].newImportTx(mm.ctx, mm.tb, c.chain, to, fee)
	require.NoErrorf(rt, mm.sut.IssueTx(mm.ctx, signed), "%T.IssueTx(import)", mm.sut.Client)

	credit := tx.ScaleAVAX(c.total - fee)
	mm.m.pendingAtomic[signed.ID()] = &atomicExpectation{
		isImport:    true,
		senderIdx:   c.ownerIdx,
		to:          to,
		creditWei:   &credit,
		remoteChain: c.chain,
		consumed:    c.utxos,
	}
	// Remove the consumed UTXOs from availability immediately: the pool
	// reserves them.
	kept := mm.m.availableUTXOs[c.chain][:0]
	for _, p := range mm.m.availableUTXOs[c.chain] {
		if p.ownerIdx != c.ownerIdx {
			kept = append(kept, p)
		}
	}
	mm.m.availableUTXOs[c.chain] = kept
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
func (mm *modelMachine) issueExport(rt *rapid.T) {
	senderIdx := rapid.IntRange(0, len(mm.atomicKeys)-1).Draw(rt, "sender")
	sender := mm.atomicAddrs[senderIdx]
	if mm.m.pendingAtomicCount(senderIdx) > 0 {
		return // one in-flight atomic tx per key keeps nonces simple
	}
	chain := rapid.SampledFrom(mm.remoteChains()).Draw(rt, "remoteChain")

	// Export at most a quarter of the sender's spendable nAVAX.
	spendableNAVAX := unscaleAVAXTruncateCapped(mm.spendable(sender))
	if spendableNAVAX < 8 {
		return
	}
	amount := rapid.Uint64Range(1, spendableNAVAX/4).Draw(rt, "amountNAVAX")
	// fee must be >= 1, for the same reason as issueImport's fee: a zero fee
	// yields a worst-case GasFeeCap of 0, which can never clear the block's
	// 1-wei price floor and would wedge the builder forever.
	fee := rapid.Uint64Range(1, 1_000).Draw(rt, "fee")

	signed, export := mm.atomicWallets[senderIdx].newExportTx(
		mm.tb, chain, fee,
		txtest.NewTransferOutput(amount, mm.atomicKeys[senderIdx].Address()),
	)
	require.NoErrorf(rt, mm.sut.IssueTx(mm.ctx, signed), "%T.IssueTx(export)", mm.sut.Client)

	debit := tx.ScaleAVAX(amount + fee)
	mm.m.pendingAtomic[signed.ID()] = &atomicExpectation{
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

	mm.openSUT() // atomic wallet nonces are restored from the model here
}

func (mm *modelMachine) trackPending(ethTx *types.Transaction, it *issuedTx) {
	mm.m.pendingEth[ethTx.Hash()] = it
	mm.m.pendingCost[it.from].Add(mm.m.pendingCost[it.from], it.cost)
	mm.pendingEthTxs = append(mm.pendingEthTxs, ethTx)
}

// spendable is the model's view of what addr can still commit to new txs.
func (mm *modelMachine) spendable(addr common.Address) *uint256.Int {
	s := new(uint256.Int).Set(mm.m.balances[addr])
	if pc := mm.m.pendingCost[addr]; pc != nil && s.Cmp(pc) >= 0 {
		s.Sub(s, pc)
	} else if pc != nil {
		s.Clear()
	}
	return s
}

func (mm *modelMachine) buildBlock(rt *rapid.T) {
	if len(mm.m.pendingEth) == 0 && len(mm.m.pendingAtomic) == 0 {
		mm.issueMinimalTransfer(rt)
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

// applyBlock advances the model by one accepted block: dynamic-parameter
// ramps, height, and per-tx effects reconciled against receipts.
func (mm *modelMachine) applyBlock(rt *rapid.T, blk *blocks.Block) {
	m := mm.m

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

		mm.applyTxEffects(rt, it, r)
	}

	// Atomic (cross-chain) tx effects, driven by what the block actually
	// included.
	for _, atx := range blockTxs(mm.tb, blk) {
		exp, ok := m.pendingAtomic[atx.ID()]
		require.Truef(rt, ok, "block %d contains unexpected atomic tx %s", blk.NumberU64(), atx.ID())
		delete(m.pendingAtomic, atx.ID())
		mm.sut.waitForTxPoolStateUpdate(mm.ctx, mm.tb, atx)

		sender := mm.atomicAddrs[exp.senderIdx]
		if exp.isImport {
			m.balances[exp.to].Add(m.balances[exp.to], exp.creditWei)
			consumed := utxosOf(exp.consumed)
			m.consumedUTXOs[exp.remoteChain] = append(m.consumedUTXOs[exp.remoteChain], consumed...)
			mm.sut.assertUTXOsMissing(mm.tb, mm.sut.ctx.ChainID, exp.remoteChain, consumed...)
		} else {
			m.balances[sender].Sub(m.balances[sender], exp.debitWei)
			m.nonces[sender]++
			m.exportedUTXOs[exp.remoteChain] = append(m.exportedUTXOs[exp.remoteChain], exp.exported...)
		}
	}

	// Drop pending txs from the machine's wait-list once included.
	included := make(map[common.Hash]bool, len(blk.Transactions()))
	for _, ethTx := range blk.Transactions() {
		included[ethTx.Hash()] = true
	}
	kept := mm.pendingEthTxs[:0]
	for _, ethTx := range mm.pendingEthTxs {
		if !included[ethTx.Hash()] {
			kept = append(kept, ethTx)
		}
	}
	mm.pendingEthTxs = kept
}

// applyTxEffects applies kind-specific model updates for an included tx.
func (mm *modelMachine) applyTxEffects(rt *rapid.T, it *issuedTx, r *types.Receipt) {
	switch it.kind {
	case kindTransfer:
		require.Equalf(rt, types.ReceiptStatusSuccessful, r.Status, "transfer receipt status")
		mm.m.balances[it.from].Sub(mm.m.balances[it.from], it.value)
		mm.m.balances[it.to].Add(mm.m.balances[it.to], it.value)
	case kindDeploy:
		require.Equalf(rt, types.ReceiptStatusSuccessful, r.Status, "deploy receipt status")
		require.Equalf(rt, it.contract, r.ContractAddress, "CREATE address prediction")
		mm.m.contracts[it.contract] = &contractState{kind: it.deployKind, storage: make(map[common.Hash]common.Hash)}
	case kindStore:
		require.Equalf(rt, types.ReceiptStatusSuccessful, r.Status, "store receipt status")
		require.Lenf(rt, r.Logs, 1, "store tx log count")
		require.Equalf(rt, []common.Hash{it.val}, r.Logs[0].Topics, "store log topic")
		require.Equalf(rt, it.contract, r.Logs[0].Address, "store log address")
		mm.m.contracts[it.contract].storage[it.key] = it.val
	case kindRevert:
		require.Equalf(rt, it.wantStatus, r.Status, "reverter receipt status")
	case kindWarpSend:
		require.Equalf(rt, types.ReceiptStatusSuccessful, r.Status, "warp send receipt status")
		// The message must now be signable: the block is accepted and executed.
		sent := mm.sut.newAddressedCallMessage(mm.tb, it.from.Bytes(), it.payload)
		mm.sut.signAndVerifyWarpMessage(mm.ctx, mm.tb, sent)
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

// check is the rapid invariant action, run around every other action.
func (mm *modelMachine) check(rt *rapid.T) {
	got, err := mm.sut.LastAccepted(mm.ctx)
	require.NoErrorf(rt, err, "%T.LastAccepted()", mm.sut.VM)
	require.Equalf(rt, mm.m.lastAcceptedID, got, "last accepted ID")

	state, err := mm.sut.LastExecutedState()
	require.NoErrorf(rt, err, "%T.LastExecutedState()", mm.sut.VM)
	for addr, want := range mm.m.balances {
		require.Equalf(rt, *want, *state.GetBalance(addr), "balance of %s", addr)
		require.Equalf(rt, mm.m.nonces[addr], state.GetNonce(addr), "nonce of %s", addr)
	}
	for contract, cs := range mm.m.contracts {
		for key, want := range cs.storage {
			require.Equalf(rt, want, state.GetState(contract, key), "storage %s[%s]", contract, key)
		}
	}
	mm.checkRawdbPointers(rt)

	// Shared-memory agreement: exported UTXOs are readable by the remote
	// chain, consumed UTXOs are gone. This also covers restarts, since it
	// reads directly from shared memory rather than in-memory bookkeeping.
	for _, chain := range mm.remoteChains() {
		if exported := mm.m.exportedUTXOs[chain]; len(exported) > 0 {
			mm.sut.assertUTXOsExist(mm.tb, chain, mm.sut.ctx.ChainID, exported...)
		}
		if consumed := mm.m.consumedUTXOs[chain]; len(consumed) > 0 {
			mm.sut.assertUTXOsMissing(mm.tb, mm.sut.ctx.ChainID, chain, consumed...)
		}
	}
}

// checkRawdbPointers spot-checks invariants.md pointer discipline on the
// persisted chain: Finalized (settled) ≤ Head (executed) ≤ HeadFast
// (accepted) along the canonical chain.
func (mm *modelMachine) checkRawdbPointers(rt *rapid.T) {
	ethDB := rawdb.NewDatabase(evmdatabase.New(prefixdb.NewNested(ethDBPrefix, prefixdb.New([]byte("chain"), mm.db))))

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
