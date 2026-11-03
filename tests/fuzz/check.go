// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

// A view is what one node reports about the actors at one height.
//
// UTXOs are kept as codec bytes. Parsed outputs differ from wallet-built ones
// in unexported fields, so only the bytes compare reliably.
type view struct {
	utxos map[ids.ID][]byte
	l1    map[ids.ID]uint64 // validation ID to balance
	// The validator fee state: the chain time in Unix seconds, the price per
	// second, and the excess.
	time, price, excess uint64
}

// check fails the test if any invariant is broken:
//
//   - each honest node's accepted chain is a prefix of the others'
//     (checkAgreement), and its height index agrees with it
//     (checkHeightIndex);
//   - the nodes at the model's height report the same state, and exactly the
//     UTXOs the model holds, plus stake returns, rewards, and refunds the
//     model expects;
//   - each accepted tx has the effect that its type guarantees (postcondition);
//   - no two accepted txs spend one input;
//   - no accepted block holds a tx type that an upgrade disables at its
//     timestamp;
//   - the fee of each accepted tx charges for its whole size;
//   - each accepted tx burns at least its fee at the minimum gas price;
//   - each reward tx comes at its staker's end time;
//   - a staker's stake is back by the check after the block that removes it;
//   - the actors' P-Chain value changed by exactly what accepted txs, rewards,
//     and refunds moved, and their L1 and X-Chain value did not grow beyond it;
//   - each L1 validator that the model follows has the balance that the
//     continuous fee leaves it (checkL1Balances);
//   - no validator's accrued delegatee reward falls within a cycle or reaches
//     the supply;
//   - delegations never take a validator above the maximum stake;
//   - no more L1 validators are active than the validator fee capacity;
//   - no honest node lost its connection to another (checkLinks);
//   - honest nodes gossip txs as the gossip protocol promises (checkGossip);
//   - at the first check, each node lists the other honest nodes with the
//     ACPs their configs support (checkHandshakes);
//   - each judged tx type commits some of its fair attempts (checkCommits).
//
// The model learns every accepted tx from the blocks that the leading node
// accepted (scan), not from the ops, and an op's tx is decided there or by
// the nodes' statuses (decide), not by the op. The state checks read a node only while
// its accepted height is the model's (readState); a node behind or ahead is
// not read, and the check's event names it. If no node stays at the model's
// height, the state checks wait for a later check, and what moved carries
// over.
//
// The check's event logs the blocks it read, what the model moved to another
// lifecycle state, the UTXOs it explained, and the actors' value. A failed
// invariant also writes a check_failed event.
func (r *runner) check(t *rapid.T) {
	const checkTimeout = time.Minute

	_, r.ev = r.events.Start(t.Context(), "check")
	r.ev.Set("step", r.step)
	defer r.endEvent()

	ctx, done := deadline(t.Context(), t, fmt.Sprintf("check after op %d", r.step), checkTimeout)
	defer done()

	lines := r.flushNodeEvents(t)
	r.checkNodeEvents(t, lines)
	r.checkCommits(t)
	// Links stay up (checkLinks), so each handshake is judged once.
	if r.step == 0 {
		r.checkHandshakes(t)
	}
	r.checkAgreement(t)
	r.checkHeightIndex(ctx, t)

	// The validators before the blocks this check scans, which bound their
	// rewards (checkRewards).
	listed := r.primaryValidators
	states := r.readState(ctx, t)
	r.checkProgress(ctx, t, r.decide(ctx, t), lines)
	r.ev.Set("height", r.height)
	if len(states) == 0 {
		r.ev.Set("skipped", fmt.Sprintf("no node stayed at height %d", r.height))
		t.Logf("check after op %d: no node stayed at height %d, so the state checks wait", r.step, r.height)
		return
	}

	ref := states[0]
	for _, s := range states[1:] {
		if diff := diffViews(ref.view, s.view); diff != "" {
			r.fail(t, "state agreement", fmt.Errorf("nodes %d and %d disagree at height %d: %s", ref.node, s.node, r.height, diff))
		}
	}
	r.applySync(t, ref.sync)

	rewards, err := r.reconcile(t, ref.view.utxos)
	if err != nil {
		r.fail(t, "model", err)
	}
	r.checkRewards(t, states, listed, rewards)
	if err := checkL1Balances(r.l1Accounts, ref.view, r.l1Price); err != nil {
		r.fail(t, "l1 balance", err)
	}
	r.l1Price = ref.view.price
	if err := overdueReturns(r.returns, r.stakers.left); err != nil {
		r.fail(t, "stake return", err)
	}
	r.stakers.left.Clear()

	r.checkValue(ctx, t, ref.view)
}

// checkAgreement fails t unless each honest node's accepted P-Chain chain,
// as its consensus accept events give it, is a prefix of the others'
// (agreement).
func (r *runner) checkAgreement(t *rapid.T) {
	for i, accepted := range r.chains {
		if err := r.agreement.add(i, accepted()); err != nil {
			r.fail(t, "block agreement", err)
		}
	}
}

var errHeightIndex = errors.New("a node's height index disagrees with its accepted chain")

// checkHeightIndex fails t unless, on each honest node, the P-Chain block
// that GetBlockByHeight gives at the node's accepted height is the one inside
// the proposervm block that its consensus accepted there, which the node
// serves to the proposer's peer. The scan reads only the leading node's
// index, and agreement only accept events, so this ties every node's index
// to its chain. It reads one height per node per check.
func (r *runner) checkHeightIndex(ctx context.Context, t *rapid.T) {
	for i, c := range r.clients {
		h, err := c.GetHeight(ctx)
		require.NoError(t, err)

		// Consensus reports a block before the VM accepts it, so the accept
		// events read after getHeight reach h.
		accepted := r.chains[i]()
		h = min(h, uint64(len(accepted)))
		if h == 0 {
			continue
		}

		b, err := c.GetBlockByHeight(ctx, h)
		require.NoError(t, err)
		indexed, err := platform.ParseBlock(platform.Codec, b)
		require.NoError(t, err, "parse node %d's block at height %d", i, h)
		blk, err := r.proposer.parent(ctx, i, accepted[h-1])
		require.NoError(t, err)

		if indexed.ID() != blk.inner.ID() {
			r.fail(t, "height index", fmt.Errorf("%w: node %d indexes P-Chain block %s at height %d, and its consensus accepted %s there, which holds %s", errHeightIndex, i, indexed.ID(), h, accepted[h-1], blk.inner.ID()))
		}
	}
}

// A nodeState is what one node reported at the model's height.
type nodeState struct {
	node int
	view view
	// sync holds what the node lists of the entities that the chain ends
	// with no tx. Only the first node read reports it.
	sync syncState
	// rewardUTXOs holds, by the ID that each decided reward pays under, the
	// actors' UTXOs that the node's reward index lists (checkRewardUTXOs).
	rewardUTXOs map[ids.ID][][]byte
}

// readState scans the blocks that the leading node accepted (scan), and reads
// each node whose accepted height is then the model's (readAt). The chain can
// move on during the reads, as reward blocks of short periods make it several
// times a second, so it tries a few times. It returns no state if no node
// stayed at the model's height in any try.
func (r *runner) readState(ctx context.Context, t *rapid.T) []nodeState {
	const tries = 5

	for range tries {
		r.scan(ctx, t)
		judged, skipped := r.queue.judgeable()

		var (
			states []nodeState
			unread []string
		)
		for i := range r.clients {
			s, ok := r.readAt(ctx, t, i, len(states) == 0, judged)
			if !ok {
				unread = append(unread, fmt.Sprintf("node %d", i))
				continue
			}
			states = append(states, s)
		}
		if len(states) == 0 {
			continue
		}

		r.ev.Set("unread", unread)
		r.ev.Set("postconditions", len(judged))
		r.ev.Set("postconditions_skipped", len(skipped))
		r.queue.clear()
		return states
	}

	return nil
}

// readAt reads node i's view at the model's height, and judges there the
// postconditions of judged and the reward options decided since the last
// check (checkOption). If first, it also reads what the node lists of the
// entities that the chain ends with no tx (readSync). It reports false if the
// node was not at the model's height through every read (atHeight): a
// failure found then counts for nothing.
func (r *runner) readAt(ctx context.Context, t *rapid.T, i int, first bool, judged []acceptedTx) (nodeState, bool) {
	c := r.clients[i]
	s := nodeState{node: i, rewardUTXOs: make(map[ids.ID][][]byte)}
	height := func() (uint64, error) { return c.GetHeight(ctx) }
	held, stayed, err := atHeight(r.height, height, func(ht *heldT) {
		s.view = r.read(ctx, ht, c)
		if first {
			s.sync = r.readSync(ctx, ht, c)
		}
		for _, a := range judged {
			p := &postcondition{ctx: ctx, r: r, t: ht, nodes: []int{i}, txID: a.tx.ID(), accepted: a}
			require.NoError(ht, a.tx.Unsigned.Visit(p))
		}
		for _, d := range r.stakers.decided {
			p := &postcondition{ctx: ctx, r: r, t: ht, nodes: []int{i}, txID: d.tx.ID(), option: &d}
			if err := d.tx.Unsigned.Visit(p); err != nil {
				ht.fail("reward option", err)
			}

			payee := d.payee()
			indexed, err := c.GetRewardUTXOs(ctx, &api.GetTxArgs{TxID: payee})
			require.NoError(ht, err)
			s.rewardUTXOs[payee] = r.ownedUTXOs(ht, indexed)
		}
	})
	require.NoError(t, err)
	if !stayed {
		return nodeState{}, false
	}
	if held.err != nil {
		r.fail(t, held.check, fmt.Errorf("node %d at height %d: %w", i, r.height, held.err))
	}

	return s, true
}

// checkRewards judges each reward proposal that an option block decided in
// the blocks since the last check: what it paid against listed, the primary
// network validators at the last check (checkRewardAmount), and the reward UTXOs
// that each node in states indexes against rewards, those that reconcile
// found by the ID they pay under (checkRewardUTXOs). readAt judged each
// option itself.
func (r *runner) checkRewards(
	t *rapid.T,
	states []nodeState,
	listed []platformvm.ClientPermissionlessValidator,
	rewards map[ids.ID][]*avax.UTXO,
) {
	for _, d := range r.stakers.decided {
		payee := d.payee()
		if err := checkRewardAmount(d, listed, r.stakers.delegated, sum(t, rewards[payee])); err != nil {
			r.fail(t, "reward amount", err)
		}

		found := make([][]byte, len(rewards[payee]))
		for i, u := range rewards[payee] {
			found[i] = utxoBytes(t, u)
		}
		for _, s := range states {
			if err := checkRewardUTXOs(payee, s.rewardUTXOs[payee], found); err != nil {
				r.fail(t, "reward utxos", fmt.Errorf("node %d, reward tx %s: %w", s.node, d.tx.ID(), err))
			}
		}
	}
	r.stakers.decided = nil
	r.stakers.delegated.Clear()
}

// ownedUTXOs returns, as the codec writes them, the UTXOs among utxos, codec
// bytes from a node, whose owners include an actor.
func (r *runner) ownedUTXOs(t require.TestingT, utxos [][]byte) [][]byte {
	var owned [][]byte
	for _, b := range utxos {
		if u := parseUTXO(t, b); r.owned(u.Out) {
			owned = append(owned, utxoBytes(t, u))
		}
	}

	return owned
}

// sum returns the total amount of utxos.
func sum(t require.TestingT, utxos []*avax.UTXO) uint64 {
	var total uint64
	for _, u := range utxos {
		total += amount(t, u)
	}

	return total
}

// finish checks the nodes' events once more after the last op, once the
// tx pulls and pushes in flight have ended, so the checks judge them too. It
// scans the blocks and decides the pending txs once more, counts the txs
// still pending as undecided, and then judges the commits, so the last
// check's decisions and the commits since count too.
func (r *runner) finish(t *rapid.T) {
	const (
		// A request ends within the request timeout, at most a second on the
		// test network (nodes.go setTestDefaults).
		settle        = time.Second
		finishTimeout = time.Minute
	)

	time.Sleep(settle)
	_, r.ev = r.events.Start(t.Context(), "check")
	r.ev.Set("step", r.step)
	defer r.endEvent()

	ctx, done := deadline(t.Context(), t, "the final check", finishTimeout)
	defer done()

	r.checkNodeEvents(t, r.flushNodeEvents(t))
	r.scan(ctx, t)
	r.decide(ctx, t)
	for _, p := range slices.Clone(r.pending) {
		r.resolve(t, p, undecided, "")
	}
	r.checkCommits(t)
}

// scan applies to the model the blocks above its height that the node with
// the most accepted blocks accepted, in height order (scanBlock). Agreement
// (checkAgreement) holds every other node to the same blocks, as far as it
// got.
func (r *runner) scan(ctx context.Context, t *rapid.T) {
	var top uint64
	for i, c := range r.clients {
		h, err := c.GetHeight(ctx)
		require.NoError(t, err)
		if h > top {
			top, r.leader = h, i
		}
	}

	for h := r.height + 1; h <= top; h++ {
		b, err := r.clients[r.leader].GetBlockByHeight(ctx, h)
		require.NoError(t, err)

		blk, err := platform.ParseBlock(platform.Codec, b)
		require.NoError(t, err, "parse block at height %d", h)
		r.scanBlock(ctx, t, h, blk)
	}
}

// scanBlock checks that blk, accepted at height h, spends no input twice,
// charges each tx for its size, burns each tx's fee, and rewards stakers on
// time. It applies its txs to the model, adds the value they move to
// r.flows, and queues their postconditions.
func (r *runner) scanBlock(ctx context.Context, t *rapid.T, h uint64, blk platform.Block) {
	r.ev.Append("scanned", scannedBlock{Block: blk.ID().String(), Height: h, Txs: logTxs(blk.Txs())})
	if b, ok := blk.(platform.BanffBlock); ok {
		r.blockTime = uint64(b.Timestamp().Unix())
	}
	if err := checkSpentOnce(r.spent, blk); err != nil {
		r.fail(t, "spent once", fmt.Errorf("block at height %d: %w", h, err))
	}
	if err := r.stakers.scan(blk); err != nil {
		r.fail(t, "reward time", fmt.Errorf("block at height %d: %w", h, err))
	}
	for _, tx := range blk.Txs() {
		if err := checkBandwidth(tx); err != nil {
			r.fail(t, "fee bandwidth", fmt.Errorf("block at height %d: %w", h, err))
		}
		if err := checkFee(r.fees, r.avaxAssetID, tx); err != nil {
			r.fail(t, "fee", fmt.Errorf("block at height %d: %w", h, err))
		}
		if err := r.account(ctx, tx); err != nil {
			r.fail(t, "model", fmt.Errorf("account for tx %s at height %d: %w", tx.ID(), h, err))
		}
		if err := r.learn(ctx, tx); err != nil {
			r.fail(t, "model", fmt.Errorf("apply tx %s at height %d: %w", tx.ID(), h, err))
		}
		r.accepted = append(r.accepted, tx)
		r.acceptedIDs.Add(tx.ID())
		r.seeTx(tx)
		r.queue.add(tx, h, blk)
		r.resolveCommitted(t, tx.ID())
	}
	r.height = h
}

var errSpentTwice = errors.New("two accepted txs spend one input")

// checkSpentOnce records in spent the input IDs that the txs of blk consume,
// each mapped to the tx that consumes it. It returns errSpentTwice if an input
// is already in spent.
//
// A proposal tx counts as spending even when its block is aborted: both the
// commit and the abort branch consume its inputs
// (vms/platformvm/txs/executor/proposal_tx_executor.go:191,206). The decision
// txs of a Banff proposal block run before the branch
// (vms/platformvm/block/executor/verifier.go:81-91), so they spend either
// way.
func checkSpentOnce(spent map[ids.ID]ids.ID, blk platform.Block) error {
	for _, tx := range blk.Txs() {
		txID := tx.ID()
		for in := range tx.InputIDs() {
			if first, ok := spent[in]; ok {
				return fmt.Errorf("%w: tx %s and tx %s spend input %s", errSpentTwice, first, txID, in)
			}
			spent[in] = txID
		}
	}

	return nil
}

// read returns what c reports about the actors: their UTXOs, and the
// balances of the L1 validators that the model follows.
func (r *runner) read(ctx context.Context, t require.TestingT, c *platformvm.Client) view {
	store := common.NewUTXOs()
	require.NoError(t, primary.AddAllUTXOs(ctx, store, c, platform.Codec, constants.PlatformChainID, constants.PlatformChainID, r.actors))

	utxos, err := store.UTXOs(ctx, constants.PlatformChainID, constants.PlatformChainID)
	require.NoError(t, err)

	v := view{
		utxos: make(map[ids.ID][]byte, len(utxos)),
		l1:    make(map[ids.ID]uint64, len(r.l1Subnet)),
	}
	for _, u := range utxos {
		v.utxos[u.InputID()] = utxoBytes(t, u)
	}
	for validationID := range r.l1Subnet {
		vdr, _, err := c.GetL1Validator(ctx, validationID)
		require.NoError(t, err)

		v.l1[validationID] = vdr.Balance
	}
	excess, price, at, err := c.GetValidatorFeeState(ctx)
	require.NoError(t, err)
	v.time, v.price, v.excess = uint64(at.Unix()), uint64(price), uint64(excess)

	return v
}

// errUnexplained means a node holds an actor UTXO that the model neither holds
// nor expects.
var errUnexplained = errors.New("UTXO is not in the model and is not a stake return, a reward, or a refund")

// reconcile matches the nodes' UTXOs against the model. Every model UTXO must
// exist unchanged. Every other UTXO must be a stake return, a reward, or a
// refund that the model expects, or reconcile returns [errUnexplained].
// Explained UTXOs join the model, so the wallet can spend them, and rewards and
// refunds join r.flows. It returns the rewards found, by the ID they pay under.
func (r *runner) reconcile(t *rapid.T, utxos map[ids.ID][]byte) (map[ids.ID][]*avax.UTXO, error) {
	ctx := t.Context()
	model, err := r.utxos.UTXOs(ctx, constants.PlatformChainID, constants.PlatformChainID)
	require.NoError(t, err)

	known := make(map[ids.ID]struct{}, len(model))
	for _, u := range model {
		id := u.InputID()
		known[id] = struct{}{}
		if want := utxoBytes(t, u); !bytes.Equal(want, utxos[id]) {
			return nil, fmt.Errorf("nodes disagree with the model on UTXO %s: nodes hold %s, model holds %s", id, describeUTXO(utxos[id]), describeUTXO(want))
		}
	}

	rewards := make(map[ids.ID][]*avax.UTXO)
	for id, b := range utxos {
		if _, ok := known[id]; ok {
			continue
		}

		u := parseUTXO(t, b)
		first, isReward := r.rewards[u.TxID]
		refund, isRefund := r.refunds[id]
		var kind string
		switch want, isReturn := r.returns[id]; {
		case isReturn:
			if !bytes.Equal(utxoBytes(t, want), b) {
				return nil, fmt.Errorf("stake return %s is %s, but the stake was %+v", id, describeUTXO(b), want.Out)
			}
			delete(r.returns, id)
			kind = "stake return"
		case isReward && u.OutputIndex >= first:
			rewards[u.TxID] = append(rewards[u.TxID], u)
			r.flows.Rewards += amount(t, u)
			kind = "reward"
		case isRefund:
			if got := amount(t, u); got != refund {
				return nil, fmt.Errorf("refund %s is %d, but the validator had %d left (checkL1Balances)", id, got, refund)
			}
			delete(r.refunds, id)
			r.flows.Refunds += amount(t, u)
			kind = "refund"
		default:
			return nil, fmt.Errorf("%w: %s: %s", errUnexplained, id, describeUTXO(b))
		}
		t.Logf("found %s %s of %d", kind, id, amount(t, u))
		r.ev.Append("found", found{Kind: kind, UTXO: id.String(), Amount: amount(t, u)})
		require.NoError(t, r.utxos.AddUTXO(ctx, constants.PlatformChainID, constants.PlatformChainID, u))
	}

	return rewards, nil
}

// checkValue fails the test unless the actors' value changed as r.flows, the
// value that accepted txs, rewards, and refunds moved since the last check,
// explains (checkConservation). Value is UTXOs, L1 validator balances, stake
// not yet returned, X-Chain balances, and atomic value in flight to the
// P-Chain. v is a node's view at the model's height. If the X-Chain does not
// hold still for its reads (readX), the check waits for a later one, and the
// flows carry over.
func (r *runner) checkValue(ctx context.Context, t *rapid.T, v view) {
	x, ok := r.readX(ctx, t)
	if !ok {
		r.ev.Set("value_skipped", "the X-Chain accepted blocks during every read")
		return
	}

	parts := valueParts{XChain: x}
	for _, b := range v.utxos {
		parts.UTXOs += amount(t, parseUTXO(t, b))
	}
	for validationID := range r.l1Subnet {
		parts.L1Balances += v.l1[validationID]
	}
	for _, u := range r.returns {
		parts.StakeReturns += amount(t, u)
	}
	inFlight, err := r.utxos.UTXOs(ctx, r.xChainID, constants.PlatformChainID)
	require.NoError(t, err)
	for _, u := range inFlight {
		parts.InFlight += amount(t, u)
	}
	r.ev.Set("value", parts)
	r.ev.Set("flows", r.flows)

	if err := checkConservation(r.value, parts, r.flows); err != nil {
		r.fail(t, "conservation", err)
	}
	r.value = &parts
	r.flows = flows{}
}

// readX returns the actors' X-Chain balance on node 0, and adds to the model
// the atomic UTXOs that node 0's shared memory holds for them from the
// X-Chain, which an export made and an ImportTx spends. It reads both while
// node 0's X-Chain stays at one height: an export accepted between the two
// reads would count once in neither or twice. A UTXO that an accepted tx
// spent is not added back: node 0 may be behind the block that spent it. It
// reports false if the X-Chain moved during every try.
//
// The actors hold value on the X-Chain too, and value in flight between the
// chains: the atomic UTXOs that the X-Chain exported to them and that no
// accepted ImportTx has spent. Counting all three makes a cross-chain move
// conserve the total instead of looking like minting.
func (r *runner) readX(ctx context.Context, t *rapid.T) (uint64, bool) {
	const tries = 5

	for range tries {
		before, err := r.xClient.GetHeight(ctx)
		require.NoError(t, err)

		atomic := common.NewUTXOs()
		require.NoError(t, primary.AddAllUTXOs(ctx, atomic, r.clients[0], platform.Codec, r.xChainID, constants.PlatformChainID, r.actors))
		var balance uint64
		for _, addr := range r.actors {
			reply, err := r.xClient.GetBalance(ctx, addr, r.avaxAssetID.String(), false)
			require.NoError(t, err)
			balance += uint64(reply.Balance)
		}

		after, err := r.xClient.GetHeight(ctx)
		require.NoError(t, err)
		if after != before {
			continue
		}

		utxos, err := atomic.UTXOs(ctx, r.xChainID, constants.PlatformChainID)
		require.NoError(t, err)
		for _, u := range utxos {
			if _, ok := r.spent[u.InputID()]; !ok {
				require.NoError(t, r.utxos.AddUTXO(ctx, r.xChainID, constants.PlatformChainID, u))
			}
		}
		return balance, true
	}

	return 0, false
}

var (
	errMinted = errors.New("actors' value grew by more than accepted txs, rewards, and refunds explain")
	errLost   = errors.New("actors' value fell by more than accepted txs moved out of it")
)

// flows is the value that moved into and out of the actors' accounts since
// the last conservation check. Accepted txs move what their fields say
// (account); rewards and refunds are the UTXOs that reconcile found.
type flows struct {
	Spent    uint64 `json:"spent"`    // inputs that spent model UTXOs on the P-Chain
	Received uint64 `json:"received"` // outputs and stake paid to the actors
	Imported uint64 `json:"imported"` // inputs that spent atomic UTXOs from the X-Chain
	ToL1     uint64 `json:"to_l1"`    // balances given to the L1 validators the model follows
	Rewards  uint64 `json:"rewards"`
	Refunds  uint64 `json:"refunds"`
}

// checkConservation compares after, the actors' value now, with before, the
// value at the last check, and f, what moved between them. A nil before is the
// first check, which has nothing to compare.
//
// The P-Chain part, UTXOs and stake not yet returned, changes by exactly f:
// it gains what txs pay the actors, rewards, and refunds, and loses what txs
// spend, as the inputs declare it. Nothing else moves it, and the fx refuses
// an input whose declared amount differs from its UTXO's
// (vms/secp256k1fx/fx.go:162). Growth mints AVAX (errMinted); a loss takes
// more than the tx says it burns (errLost), which the fee check, reading the
// declared inputs, would miss. A tx that only moves the actors' value moves
// out of it exactly what it burns.
//
// The L1 and X-Chain parts may lose more than f says, so they are checked
// only for growth. The continuous fee drains each active L1 balance, which
// checkL1Balances judges per validator, and a refund pays only what is left.
// The runner does not scan the X-Chain's blocks, so it does not know the fees
// that its exports burn there.
func checkConservation(before *valueParts, after valueParts, f flows) error {
	if before == nil {
		return nil
	}

	inP := before.UTXOs + before.StakeReturns + f.Received + f.Rewards + f.Refunds
	switch outP := after.UTXOs + after.StakeReturns + f.Spent; {
	case outP > inP:
		return fmt.Errorf("%w: P-Chain value is %d more than %+v explains: %s", errMinted, outP-inP, f, growth(*before, after))
	case outP < inP:
		return fmt.Errorf("%w: P-Chain value is %d less than %+v explains: %s", errLost, inP-outP, f, growth(*before, after))
	}
	if after.L1Balances+f.Refunds > before.L1Balances+f.ToL1 {
		return fmt.Errorf("%w: L1 balances grew from %d to %d with %d given and %d refunded", errMinted, before.L1Balances, after.L1Balances, f.ToL1, f.Refunds)
	}
	if afterX, beforeX := after.XChain+after.InFlight+f.Imported, before.XChain+before.InFlight; afterX > beforeX {
		return fmt.Errorf("%w: X-Chain value and value in flight grew by %d, with %d imported: %s", errMinted, afterX-beforeX, f.Imported, growth(*before, after))
	}

	return nil
}

// account adds to r.flows the value that tx, accepted, moves into and out of
// the actors' accounts, read from its fields (valueOf). An input counts if
// the model holds its UTXO, so account must run before learn spends them. An
// output counts if it pays an actor, as learn keeps it. Value that leaves the
// actors, such as an export to the X-Chain, counts in neither.
func (r *runner) account(ctx context.Context, tx *platform.Tx) error {
	v, err := valueOf(tx.Unsigned)
	if err != nil {
		return err
	}

	for _, in := range v.ins {
		switch id := in.InputID(); {
		case r.holds(ctx, constants.PlatformChainID, id):
			r.flows.Spent += in.In.Amount()
		case r.holds(ctx, r.xChainID, id):
			r.flows.Imported += in.In.Amount()
		}
	}
	for _, out := range v.outs {
		if r.owned(out.Out) {
			r.flows.Received += out.Output().Amount()
		}
	}
	switch utx := tx.Unsigned.(type) {
	case *platform.ConvertSubnetToL1Tx:
		// learnL1Validators follows every validator that a conversion creates.
		r.flows.ToL1 += v.l1
	case *platform.IncreaseL1ValidatorBalanceTx:
		if _, ok := r.l1Subnet[utx.ValidationID]; ok {
			r.flows.ToL1 += v.l1
		}
	}

	return nil
}

// holds reports whether the model holds the UTXO id that sourceChainID sent to
// the P-Chain.
func (r *runner) holds(ctx context.Context, sourceChainID, id ids.ID) bool {
	_, err := r.utxos.GetUTXO(ctx, sourceChainID, constants.PlatformChainID, id)

	return err == nil
}

// learn applies an accepted tx to the model. The model keeps the UTXOs whose
// owners include an actor, the stake each staker gets back, and the IDs that
// rewards and refunds are paid under. Amounts come from the tx or from the
// nodes; learn never decides one. learn also records in the registries each
// entity that the tx adds or removes, whichever op or path the tx came from.
func (r *runner) learn(ctx context.Context, tx *platform.Tx) error {
	txID := tx.ID()
	switch utx := tx.Unsigned.(type) {
	case *platform.AdvanceTimeTx:
		return nil
	case *platform.RewardValidatorTx:
		// It pays under the staker's ID, after the staker's outputs and stake.
		if first, ok := r.rewardIndex[utx.TxID]; ok {
			r.rewards[utx.TxID] = first
		}
		return nil
	case *platform.RewardAutoRenewedValidatorTx:
		// It pays under its own ID, because the staker keeps its ID across
		// cycles (proposal_tx_executor.go mintRewards).
		r.rewards[txID] = 0
		return nil
	case *platform.CreateSubnetTx:
		r.subnets.put(txID)
	case *platform.ConvertSubnetToL1Tx:
		r.converted.Add(utx.Subnet)
		if err := r.learnL1Validators(ctx, utx); err != nil {
			return err
		}
	case *platform.AddPermissionlessValidatorTx:
		if utx.Subnet == constants.PrimaryNetworkID {
			r.validators.put(utx.NodeID())
		}
	case *platform.AddAutoRenewedValidatorTx:
		r.autoRenew.put(txID)
	case *platform.AddSubnetValidatorTx:
		if nodeIDs := r.subnetValidators[utx.Subnet]; !slices.Contains(nodeIDs, utx.NodeID()) {
			r.subnetValidators[utx.Subnet] = append(nodeIDs, utx.NodeID())
		}
	case *platform.RemoveSubnetValidatorTx:
		r.subnetValidators[utx.Subnet] = slices.DeleteFunc(r.subnetValidators[utx.Subnet], func(nodeID ids.NodeID) bool {
			return nodeID == utx.NodeID
		})
	case *platform.IncreaseL1ValidatorBalanceTx:
		if a, ok := r.l1Accounts[utx.ValidationID]; ok {
			a = a.drain(r.blockTime, r.l1Price)
			a.balance += utx.Balance
			r.l1Accounts[utx.ValidationID] = a
		}
	case *platform.DisableL1ValidatorTx:
		// The refund is what the balance has left at the block's time. A
		// validator that ran out refunds nothing. An increase can activate
		// the validator again, so its account stays, empty.
		if a, ok := r.l1Accounts[utx.ValidationID]; ok {
			refund := avax.UTXOID{TxID: txID, OutputIndex: uint32(len(utx.Outs))}
			r.refunds[refund.InputID()] = a.drain(r.blockTime, r.l1Price).balance
			r.l1Accounts[utx.ValidationID] = l1Account{at: r.blockTime}
			r.l1.transition(utx.ValidationID, removed)
		}
	}

	if err := r.backend.AcceptTx(ctx, tx); err != nil {
		return err
	}

	// AcceptTx keeps every output, but the nodes report only the actors'.
	for _, u := range tx.UTXOs() {
		if !r.owned(u.Out) {
			if err := r.utxos.RemoveUTXO(ctx, constants.PlatformChainID, constants.PlatformChainID, u.InputID()); err != nil {
				return err
			}
		}
	}

	if s, ok := tx.Unsigned.(platform.PermissionlessStaker); ok {
		r.expectStakeReturn(txID, s.Outputs(), s.Stake())
	}

	return nil
}

// expectStakeReturn records the UTXOs that an actor gets back when a staker
// ends, and where the staker's rewards start.
func (r *runner) expectStakeReturn(txID ids.ID, outs, stake []*avax.TransferableOutput) {
	for i, out := range stake {
		if !r.owned(out.Out) {
			continue
		}

		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{TxID: txID, OutputIndex: uint32(len(outs) + i)},
			Asset:  out.Asset,
			Out:    out.Output(),
		}
		r.returns[utxo.InputID()] = utxo
	}
	r.rewardIndex[txID] = uint32(len(outs) + len(stake))
}

// owned reports whether out's owners include an actor, which is when the
// nodes list it among the actors' UTXOs.
func (r *runner) owned(out any) bool {
	a, ok := out.(avax.Addressable)
	if !ok {
		return false
	}

	for _, b := range a.Addresses() {
		addr, err := ids.ToShortID(b)
		if err == nil && slices.Contains(r.actors, addr) {
			return true
		}
	}

	return false
}

func utxoBytes(t require.TestingT, u *avax.UTXO) []byte {
	b, err := platform.Codec.Marshal(platform.CodecVersion, u)
	require.NoError(t, err)
	return b
}

func parseUTXO(t require.TestingT, b []byte) *avax.UTXO {
	var u avax.UTXO
	_, err := platform.Codec.Unmarshal(b, &u)
	require.NoError(t, err)
	return &u
}

func amount(t require.TestingT, u *avax.UTXO) uint64 {
	out, ok := u.Out.(avax.Amounter)
	require.True(t, ok, "UTXO %s has no amount", u.InputID())
	return out.Amount()
}

// learnL1Validators puts the L1 validators that a ConvertSubnetToL1Tx
// created. Their validation IDs come from the nodes, so the model does not
// copy how the chain derives them. The scan reads the tx's block from the
// leading node, which is at or past it, and no op removes an L1 validator, so
// it lists every one.
func (r *runner) learnL1Validators(ctx context.Context, utx *platform.ConvertSubnetToL1Tx) error {
	nodeIDs := make([]ids.NodeID, len(utx.Validators))
	for i, vdr := range utx.Validators {
		nodeID, err := ids.ToNodeID(vdr.NodeID)
		if err != nil {
			return err
		}
		nodeIDs[i] = nodeID
	}

	listed, err := r.clients[r.leader].GetCurrentValidators(ctx, utx.Subnet, nodeIDs)
	if err != nil {
		return err
	}
	r.see(listed)

	validationIDs := make(map[ids.NodeID]ids.ID, len(listed))
	for _, v := range listed {
		if v.ValidationID != nil {
			validationIDs[v.NodeID] = *v.ValidationID
		}
	}
	for i, vdr := range utx.Validators {
		vID, ok := validationIDs[nodeIDs[i]]
		if !ok {
			return fmt.Errorf("node %d does not list L1 validator %s of subnet %s", r.leader, nodeIDs[i], utx.Subnet)
		}
		r.l1.put(vID)
		r.l1Subnet[vID] = utx.Subnet
		r.l1Accounts[vID] = l1Account{balance: vdr.Balance, at: r.blockTime}
	}

	return nil
}

// A syncState is what one node lists, at the model's height, of the entities
// that the chain ends with no tx (readSync).
type syncState struct {
	// primary holds the current primary network validators, sorted by node
	// ID.
	primary []platformvm.ClientPermissionlessValidator
	// subnets holds, by subnet, the node IDs of the model's subnet
	// validators that the node still lists.
	subnets map[ids.ID]set.Set[ids.NodeID]
	// l1 holds the balance of each active validator of the L1s that the
	// model follows, by validation ID, and capacity the validator fee
	// capacity.
	l1       map[ids.ID]uint64
	capacity gas.Gas
	supply   uint64
}

// readSync reads from c the current validators of the primary network, of
// each subnet that the model holds validators on, and of each L1 that it
// follows, the validator fee capacity, and the supply. It adds their IDs to
// the seen sets.
func (r *runner) readSync(ctx context.Context, t require.TestingT, c *platformvm.Client) syncState {
	s := syncState{
		primary: r.currentValidators(ctx, t, c, constants.PrimaryNetworkID, nil),
		subnets: make(map[ids.ID]set.Set[ids.NodeID]),
		l1:      make(map[ids.ID]uint64),
	}

	// Sorted, so the seen sets fill in the same order on every run.
	for _, subnetID := range slices.SortedFunc(maps.Keys(r.subnetValidators), ids.ID.Compare) {
		if nodeIDs := r.subnetValidators[subnetID]; len(nodeIDs) > 0 {
			var listed set.Set[ids.NodeID]
			for _, v := range r.currentValidators(ctx, t, c, subnetID, nodeIDs) {
				listed.Add(v.NodeID)
			}
			s.subnets[subnetID] = listed
		}
	}

	if len(r.l1Subnet) > 0 {
		feeConfig, err := c.GetValidatorFeeConfig(ctx)
		require.NoError(t, err)
		s.capacity = feeConfig.Capacity

		var read set.Set[ids.ID] // subnets already read
		for _, validationID := range slices.SortedFunc(maps.Keys(r.l1Subnet), ids.ID.Compare) {
			subnetID := r.l1Subnet[validationID]
			if read.Contains(subnetID) {
				continue
			}
			read.Add(subnetID)

			for _, v := range r.currentValidators(ctx, t, c, subnetID, nil) {
				if v.ValidationID != nil && v.Balance != nil {
					s.l1[*v.ValidationID] = *v.Balance
				}
			}
		}
	}

	supply, _, err := c.GetCurrentSupply(ctx, constants.PrimaryNetworkID)
	require.NoError(t, err)
	s.supply = supply

	return s
}

// applySync moves each registry entry to the lifecycle state that s, one
// node's listing at the model's height, gives it, because the chain ends
// stakers and L1 validators with no tx. It keeps the primary network
// validators in r.primaryValidators and gives them to r.stakers.sync. It also
// checks, on s, the accrued delegatee rewards (checkAccruals), the maximum
// stake (checkMaxStake), and the L1 capacity (checkL1Capacity): one listing
// at one height is one state.
func (r *runner) applySync(t *rapid.T, s syncState) {
	r.primaryValidators = s.primary
	r.stakers.sync(s.primary)

	nodeIDs := make(map[ids.NodeID]struct{}, len(s.primary))
	txIDs := make(map[ids.ID]struct{}, len(s.primary))
	for _, v := range s.primary {
		nodeIDs[v.NodeID] = struct{}{}
		txIDs[v.TxID] = struct{}{}
	}
	for _, nodeID := range r.validators.keys(active) {
		if _, ok := nodeIDs[nodeID]; !ok {
			r.validators.transition(nodeID, ended)
			r.ev.Append("transitions", transition{Kind: "validator", ID: nodeID.String(), To: ended.String()})
		}
	}
	// An online staker's peers go once its validation ended, or once its tx
	// did not commit and is no longer pending.
	waiting := r.pendingStakers()
	for nodeID, peers := range r.online {
		if _, ok := nodeIDs[nodeID]; !ok && !waiting.Contains(nodeID) {
			require.NoError(t, closePeers(t.Context(), peers))
			delete(r.online, nodeID)
		}
	}
	// An auto-renewed validator keeps its tx ID across periods. A node ID
	// alone would miss one that ended while another tx added its node ID.
	for _, txID := range r.autoRenew.keys(active) {
		if _, ok := txIDs[txID]; !ok {
			r.autoRenew.transition(txID, ended)
			r.ev.Append("transitions", transition{Kind: "auto-renewed validator", ID: txID.String(), To: ended.String()})
		}
	}

	for subnetID, listed := range s.subnets {
		r.subnetValidators[subnetID] = slices.DeleteFunc(r.subnetValidators[subnetID], func(nodeID ids.NodeID) bool {
			if listed.Contains(nodeID) {
				return false
			}

			r.ev.Append("transitions", transition{
				Kind:   "subnet validator",
				ID:     nodeID.String(),
				Subnet: subnetID.String(),
				To:     ended.String(),
			})
			return true
		})
	}

	if len(r.l1Subnet) > 0 {
		if err := checkL1Capacity(s.l1, s.capacity); err != nil {
			r.fail(t, "l1 capacity", fmt.Errorf("at height %d: %w", r.height, err))
		}
	}
	for _, validationID := range slices.SortedFunc(maps.Keys(r.l1Subnet), ids.ID.Compare) {
		state, _ := r.l1.state(validationID)
		to := state
		switch {
		case s.l1[validationID] > 0:
			to = active
		case state == active:
			to = ended
		}
		if to != state {
			r.l1.transition(validationID, to)
			r.ev.Append("transitions", transition{Kind: "l1 validator", ID: validationID.String(), To: to.String()})
		}
	}

	var err error
	r.accruals, err = checkAccruals(r.accruals, s.primary, s.supply)
	if err != nil {
		r.fail(t, "delegatee reward", err)
	}
	if err := checkMaxStake(s.primary, maxValidatorStake); err != nil {
		r.fail(t, "max stake", err)
	}
}

var errL1Capacity = errors.New("more L1 validators are active than the validator fee capacity")

// checkL1Capacity returns errL1Capacity if more of balances, by validation
// ID, are above 0 than capacity, the validator fee config's capacity.
//
// The API reports a balance only for an active validator, one with an end
// fee (vms/platformvm/service.go, EndAccumulatedFee). A tx that would make a
// validator active fails while capacity validators already are
// (txs/executor/standard_tx_executor.go, errMaxNumActiveValidators), so at
// any accepted height at most capacity are active.
func checkL1Capacity(balances map[ids.ID]uint64, capacity gas.Gas) error {
	var active gas.Gas
	for _, b := range balances {
		if b > 0 {
			active++
		}
	}
	if active > capacity {
		return fmt.Errorf("%w: %d are active, but the capacity is %d", errL1Capacity, active, capacity)
	}

	return nil
}

// currentValidators returns c's current validators of subnetID, limited to
// nodeIDs unless it is empty, sorted by node ID: the API promises no order,
// and a replay must draw the same one. It adds their IDs to the seen sets.
func (r *runner) currentValidators(
	ctx context.Context,
	t require.TestingT,
	c *platformvm.Client,
	subnetID ids.ID,
	nodeIDs []ids.NodeID,
) []platformvm.ClientPermissionlessValidator {
	vdrs, err := c.GetCurrentValidators(ctx, subnetID, nodeIDs)
	require.NoError(t, err)

	slices.SortFunc(vdrs, func(x, y platformvm.ClientPermissionlessValidator) int {
		return x.NodeID.Compare(y.NodeID)
	})
	r.see(vdrs)

	return vdrs
}

// seeTx adds to the pools every value in an accepted tx: its own ID, the IDs
// of the UTXOs it spends and creates, and every value in its fields and
// credentials, keyed by field path (mutate.go walk).
func (r *runner) seeTx(tx *platform.Tx) {
	r.seenIDs.all.add(tx.ID())
	spent := tx.InputIDs().List()
	utils.Sort(spent) // a set has no order, and the seen order must not vary
	r.seenIDs.all.add(spent...)
	for _, u := range tx.UTXOs() {
		r.seenIDs.all.add(u.InputID())
	}

	name := txName(tx.Unsigned)
	walk(reflect.ValueOf(tx.Unsigned), name, name, serialized, r.observe)
	creds := name + ".Creds"
	walk(reflect.ValueOf(tx.Creds), creds, creds, serialized, r.observe)
}

// see adds to the pools every value in v, an API reply, that exported fields,
// pointers, interfaces, and lists reach. Paths start with v's type, such as
// "[]platformvm.ClientPermissionlessValidator", so a reply's values are
// known values for every field and historical ones for none.
func (r *runner) see(v any) {
	name := reflect.TypeOf(v).String()
	walk(reflect.ValueOf(v), name, name, reflect.StructField.IsExported, r.observe)
}

var errBandwidth = errors.New("fee bandwidth differs from tx size")

// checkBandwidth returns errBandwidth unless the bandwidth that the fee charges
// for tx equals its signed size. The fee package states this for every tx type
// it supports (vms/platformvm/txs/fee/complexity_test.go:51), and skips the
// others with fee.ErrUnsupportedTx.
//
// The fee counts each credential from its input's signer count, not from the
// credential. An accepted tx still matches: the fx rejects a credential whose
// signature count differs (vms/secp256k1fx/fx.go:179), and the executor
// rejects extra credentials (vms/platformvm/utxo/verifier.go:126). Since Etna,
// block verification rejects a tx whose complexity is an error
// (vms/platformvm/block/executor/verifier.go:538).
func checkBandwidth(tx *platform.Tx) error {
	complexity, err := fee.TxComplexity(tx.Unsigned)
	if errors.Is(err, fee.ErrUnsupportedTx) {
		return nil
	}
	if err != nil {
		return err
	}

	if got, want := complexity[gas.Bandwidth], uint64(len(tx.Bytes())); got != want {
		return fmt.Errorf("%w: tx %s has %d bytes, but its fee charges for %d", errBandwidth, tx.ID(), want, got)
	}

	return nil
}
