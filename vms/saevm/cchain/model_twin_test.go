// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"cmp"
	"maps"
	"math/big"
	"slices"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
	ethparams "github.com/ava-labs/libevm/params"
)

// scriptStep is one deterministic step of a twin-run script. Steps carry
// pre-drawn values so both runs execute identically.
type scriptStep struct {
	kind      string // "transfer", "build", "advance", "settle"
	fromIdx   int
	toIdx     int
	valueFrac uint64
	advance   time.Duration
}

func genScript(numAccounts int) *rapid.Generator[[]scriptStep] {
	step := rapid.Custom(func(rt *rapid.T) scriptStep {
		s := scriptStep{
			kind: rapid.SampledFrom([]string{"transfer", "transfer", "build", "advance", "settle"}).Draw(rt, "kind"),
		}
		switch s.kind {
		case "transfer":
			s.fromIdx = rapid.IntRange(0, numAccounts-1).Draw(rt, "from")
			s.toIdx = rapid.IntRange(0, numAccounts-1).Draw(rt, "to")
			s.valueFrac = rapid.Uint64().Draw(rt, "valueFrac")
		case "advance":
			s.advance = time.Duration(rapid.Int64Range(int64(time.Millisecond), int64(2*saeparams.Tau)).Draw(rt, "advance"))
		}
		return s
	})
	return rapid.SliceOfN(step, 4, 15)
}

// runScript executes script on a fresh machine, restarting after step
// restartAfter (-1 for never). Pending txs at the restart are re-issued so
// both runs process the same tx set. Returns the final observable state.
func runScript(t *testing.T, rt *rapid.T, cfg runConfig, script []scriptStep, restartAfter int) (root common.Hash, height uint64, balances map[common.Address]*uint256.Int) {
	mm := newModelMachine(t, rt, cfg)
	defer mm.tb.close()

	doTransfer := func(s scriptStep) {
		from := mm.addrs[s.fromIdx]
		if mm.pendingCount(from) >= maxPendingPerAccount {
			return
		}
		value := new(uint256.Int).Rsh(new(uint256.Int).Mul(mm.spendable(from), uint256.NewInt(s.valueFrac)), 66)
		to := mm.addrs[s.toIdx]
		data := &types.DynamicFeeTx{
			To:        &to,
			Gas:       ethparams.TxGas,
			GasFeeCap: big.NewInt(txGasFeeCap),
			Value:     value.ToBig(),
		}
		ethTx := mm.wallet.SetNonceAndSign(mm.tb, s.fromIdx, data)
		require.NoErrorf(rt, mm.sut.ethclient.SendTransaction(mm.ctx, ethTx), "twin SendTransaction")
		mm.trackPending(ethTx, &issuedTx{
			kind: kindTransfer, from: from, to: to, value: value,
			cost: new(uint256.Int).Add(value, uint256.NewInt(ethparams.TxGas*txGasFeeCap)),
		})
	}

	for i, s := range script {
		switch s.kind {
		case "transfer":
			doTransfer(s)
		case "build":
			if len(mm.m.pendingEth) == 0 {
				mm.issueMinimalTransfer(rt, mm.ctx, mm.sut)
			}
			mm.applyBlock(rt, mm.buildVerifyAcceptExecute(rt))
		case "advance":
			mm.clock.Advance(s.advance)
		case "settle":
			mm.settle(rt)
		}
		if i == restartAfter {
			// Snapshot the pending txs' (from, to, value, nonce) quadruples,
			// restart (which drops the pool and rewinds nonces), then
			// re-issue them verbatim so both runs process the same tx set.
			// The re-issue order must preserve each sender's original
			// per-account nonce order: SetNonceAndSign assigns nonces
			// sequentially per account, so reissuing a sender's txs out of
			// their original relative order would reassign which nonce
			// carries which value — a balance-equivalent (additive
			// transfers commute) but byte-different block. Sort by
			// (fromIdx, nonce) rather than value to keep it deterministic
			// and faithful to the original issue order; nonce comes off the
			// already-signed tx rather than out of mm.pendingEth, since
			// mm.pendingEthTxs/mm.m.pendingEth don't otherwise retain it.
			type pendingTx struct {
				fromIdx int
				to      common.Address
				value   *uint256.Int
				nonce   uint64
			}
			var pending []pendingTx
			for _, ethTx := range mm.pendingEthTxs {
				it, ok := mm.m.pendingEth[ethTx.Hash()]
				require.Truef(rt, ok, "pendingEthTxs entry %s missing from pendingEth", ethTx.Hash())
				pending = append(pending, pendingTx{slices.Index(mm.addrs, it.from), it.to, it.value, ethTx.Nonce()})
			}
			slices.SortFunc(pending, func(a, b pendingTx) int {
				return cmp.Or(cmp.Compare(a.fromIdx, b.fromIdx), cmp.Compare(a.nonce, b.nonce))
			})
			mm.restart(rt)
			for _, p := range pending {
				from := mm.addrs[p.fromIdx]
				data := &types.DynamicFeeTx{
					To:        &p.to,
					Gas:       ethparams.TxGas,
					GasFeeCap: big.NewInt(txGasFeeCap),
					Value:     p.value.ToBig(),
				}
				ethTx := mm.wallet.SetNonceAndSign(mm.tb, p.fromIdx, data)
				require.NoErrorf(rt, mm.sut.ethclient.SendTransaction(mm.ctx, ethTx), "twin re-issue after restart")
				mm.trackPending(ethTx, &issuedTx{
					kind: kindTransfer, from: from, to: p.to, value: p.value,
					cost: new(uint256.Int).Add(p.value, uint256.NewInt(ethparams.TxGas*txGasFeeCap)),
				})
			}
		}
	}
	// Flush anything still pending, so both runs end at the same height
	// regardless of where the restart fell. A single final block is not
	// enough: worst-case block building may legally include only a subset
	// of the pending txs (ApplyTx failures silently exclude txs from a
	// block; errEmptyBlock only fires when NONE are includable), so under
	// partial inclusion the two runs could otherwise end at different
	// heights/balances. Loop until the pool drains, settling between
	// iterations so unsettled credits from the prior block become
	// includable.
	const maxFlushIterations = 10
	for iter := 0; len(mm.m.pendingEth) > 0; iter++ {
		require.Lessf(rt, iter, maxFlushIterations, "final flush did not drain pendingEth within %d iterations", maxFlushIterations)
		mm.applyBlock(rt, mm.buildVerifyAcceptExecute(rt))
		mm.settle(rt)
	}
	mm.check(rt)

	if mm.m.lastAccepted != nil {
		root = mm.m.lastAccepted.PostExecutionStateRoot()
	}
	return root, mm.m.lastAcceptedHeight, maps.Clone(mm.m.balances)
}

// TestModelRestartEquivalence verifies a run with a restart injected reaches
// exactly the state of the same run without one.
//
// runScript deliberately does not return lastAccepted block IDs: they are NOT
// compared. Three independent findings during development made a
// byte-identical block hash unreachable here, regardless of any
// restart-specific bug:
//
//  1. Atomic-key accounts are seeded with a fresh secp256k1 key per
//     newModelMachine call (txtest.NewKey draws from crypto/rand.Reader, not
//     from rt) and their addresses are baked directly into genesis alloc
//     (baseOptions). genScript never issues import/export, so those accounts
//     are untested here but their address would otherwise make genesis -
//     and so every downstream block hash - differ between runA and runB
//     regardless of the restart under test. cfg.numAtomicKeys is forced to 0
//     below to eliminate this (state roots would differ too, not just IDs).
//  2. The restart-reissue path in runScript re-issues pending txs in each
//     sender's original nonce order (not by value, which was the original
//     bug here): SetNonceAndSign assigns nonces sequentially per account, so
//     reissuing a sender's txs out of their original relative order would
//     reassign which nonce carries which value — a balance-equivalent
//     (additive transfers commute) but byte-different block. The snapshot
//     is taken from mm.pendingEthTxs (issue order) and sorted by (fromIdx,
//     nonce) — nonce read off the already-signed tx — rather than by value,
//     so it is faithful to the original issue order and deterministic
//     across runs. This eliminates the reissue-ordering artifact, but does
//     NOT make the property hold: see (3).
//  3. The real, confirmed remaining cause: for transactions from different
//     senders with equal effective gas tip (all model transfers use the
//     same fixed txGasFeeCap), TransactionsByPriority
//     (vms/saevm/txgossip/priority.go:47-61) tie-breaks by `a.Time.Compare
//     (b.Time)`, where Time is the real wall-clock time the tx pool
//     admitted the tx — NOT the mocked model clock. runA and runB are two
//     independent VM instances; identical script replay does not guarantee
//     identical relative wall-clock arrival order for two different
//     senders' txs (real RPC/pool-admission latency jitter between the two
//     instances), so a block containing pending txs from ≥2 senders at
//     equal tip can legally order them differently between runs — a
//     different, byte-different block for the same final state. Observed
//     directly with -race -rapid.checks=300 after fixing (2): the very
//     FIRST accepted block (height 1) already diverged in per-sender tx
//     order between runA and runB — before either instance's restart step
//     had run at all (see task-12-report.md's "Final-review fix wave"
//     section for the sender/tx_index logs) — which rules out the reissue
//     path as the cause and confirms this is intrinsic to running two
//     separate VM instances against the same script, independent of
//     restart.
//
// Height, post-execution state root (which subsumes balances, nonces, and
// contract storage), and a direct balances comparison are kept as the
// equivalence property; the lastAccepted ID comparison is intentionally not
// restored (see (3)).
func TestModelRestartEquivalence(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		cfg := genRunConfig().Draw(rt, "runConfig")
		cfg.numAtomicKeys = 0                                        // see (1) above
		script := genScript(int(cfg.numAccounts)).Draw(rt, "script") //#nosec G115 -- numAccounts is bounded [2,6] by genRunConfig
		restartAfter := rapid.IntRange(0, len(script)-1).Draw(rt, "restartAfter")

		rootA, heightA, balancesA := runScript(t, rt, cfg, script, -1)
		rootB, heightB, balancesB := runScript(t, rt, cfg, script, restartAfter)

		require.Equalf(rt, heightA, heightB, "twin-run final height")
		require.Equalf(rt, rootA, rootB, "twin-run post-execution state root")
		require.Equalf(rt, balancesA, balancesB, "twin-run final balances")
	})
}
